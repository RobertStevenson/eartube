package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Video is what eartube has saved about a video seen in one of your playlists,
// so it can still say what the video was after YouTube stops serving it.
type Video struct {
	ID          string
	Title       string
	Channel     string
	ChannelURL  string
	Duration    float64 // seconds; 0 if unknown
	UploadDate  string  // YYYY-MM-DD; empty if unknown
	Description string
	Tags        []string
	Thumbnail   string // path of the saved image; empty if none
	Playlists   []string
	FirstSeen   time.Time
	// DetailsAt is when the full details (description, date, tags, thumbnail)
	// were saved. Zero means only what the playlist listing gave.
	DetailsAt time.Time
	// GoneAt is when the video was first seen unavailable (private, deleted).
	// Zero means it was playable the last time it was listed.
	GoneAt time.Time
}

// HasDetails reports whether the full details have been saved.
func (v Video) HasDetails() bool { return !v.DetailsAt.IsZero() }

const videosSchema = `
	CREATE TABLE IF NOT EXISTS videos (
		id          TEXT PRIMARY KEY,
		title       TEXT    NOT NULL DEFAULT '',
		channel     TEXT    NOT NULL DEFAULT '',
		channel_url TEXT    NOT NULL DEFAULT '',
		duration    REAL    NOT NULL DEFAULT 0,
		upload_date TEXT    NOT NULL DEFAULT '',
		description TEXT    NOT NULL DEFAULT '',
		tags        TEXT    NOT NULL DEFAULT '[]',
		thumbnail   TEXT    NOT NULL DEFAULT '',
		first_seen  INTEGER NOT NULL DEFAULT 0,
		details_at  INTEGER NOT NULL DEFAULT 0,
		gone_at     INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS video_playlists (
		video_id       TEXT NOT NULL,
		playlist_id    TEXT NOT NULL,
		playlist_title TEXT NOT NULL,
		PRIMARY KEY (video_id, playlist_id)
	);`

// Sighting is one video as a playlist listing showed it.
type Sighting struct {
	ID       string
	Title    string
	Channel  string
	Duration float64
	Gone     bool // listed as private, deleted or otherwise unavailable
}

// RecordPlaylist saves what a playlist listing showed. Playable videos have
// their title, channel and duration saved (never blanked out) and are marked
// available. Unavailable ones keep everything saved earlier and get GoneAt set
// the first time; one never seen before is still recorded, so its id and
// playlist are known. Saved details are never deleted.
func (s *Store) RecordPlaylist(playlistID, playlistTitle string, seen []Sighting, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, v := range seen {
		if v.Gone {
			_, err = tx.Exec(`
				INSERT INTO videos (id, first_seen, gone_at) VALUES (?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET gone_at = CASE WHEN gone_at = 0 THEN excluded.gone_at ELSE gone_at END`,
				v.ID, at.Unix(), at.Unix())
		} else {
			_, err = tx.Exec(`
				INSERT INTO videos (id, title, channel, duration, first_seen) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET
				    title    = CASE WHEN excluded.title    != '' THEN excluded.title    ELSE title    END,
				    channel  = CASE WHEN excluded.channel  != '' THEN excluded.channel  ELSE channel  END,
				    duration = CASE WHEN excluded.duration >  0  THEN excluded.duration ELSE duration END,
				    gone_at  = 0`,
				v.ID, v.Title, v.Channel, v.Duration, at.Unix())
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`
			INSERT INTO video_playlists (video_id, playlist_id, playlist_title) VALUES (?, ?, ?)
			ON CONFLICT(video_id, playlist_id) DO UPDATE SET playlist_title = excluded.playlist_title`,
			v.ID, playlistID, playlistTitle); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveDetails stores a video's full details. Empty fields do not overwrite
// what was saved before.
func (s *Store) SaveDetails(v Video, at time.Time) error {
	tags, err := json.Marshal(v.Tags)
	if err != nil {
		return err
	}
	if v.Tags == nil {
		tags = []byte("[]")
	}
	_, err = s.db.Exec(`
		INSERT INTO videos (id, title, channel, channel_url, duration, upload_date, description, tags, thumbnail, first_seen, details_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		    title       = CASE WHEN excluded.title       != '' THEN excluded.title       ELSE title       END,
		    channel     = CASE WHEN excluded.channel     != '' THEN excluded.channel     ELSE channel     END,
		    channel_url = CASE WHEN excluded.channel_url != '' THEN excluded.channel_url ELSE channel_url END,
		    duration    = CASE WHEN excluded.duration    >  0  THEN excluded.duration    ELSE duration    END,
		    upload_date = CASE WHEN excluded.upload_date != '' THEN excluded.upload_date ELSE upload_date END,
		    description = CASE WHEN excluded.description != '' THEN excluded.description ELSE description END,
		    tags        = CASE WHEN excluded.tags     != '[]'  THEN excluded.tags        ELSE tags        END,
		    thumbnail   = CASE WHEN excluded.thumbnail   != '' THEN excluded.thumbnail   ELSE thumbnail   END,
		    details_at  = excluded.details_at`,
		v.ID, v.Title, v.Channel, v.ChannelURL, v.Duration, v.UploadDate, v.Description,
		string(tags), v.Thumbnail, at.Unix(), at.Unix())
	return err
}

const videoCols = `v.id, v.title, v.channel, v.channel_url, v.duration, v.upload_date, v.description,
	v.tags, v.thumbnail, v.first_seen, v.details_at, v.gone_at,
	(SELECT json_group_array(playlist_title) FROM
	    (SELECT playlist_title FROM video_playlists p WHERE p.video_id = v.id ORDER BY playlist_title))`

func scanVideo(sc interface{ Scan(...any) error }) (Video, error) {
	var v Video
	var tags, pls string
	var first, details, gone int64
	if err := sc.Scan(&v.ID, &v.Title, &v.Channel, &v.ChannelURL, &v.Duration, &v.UploadDate,
		&v.Description, &tags, &v.Thumbnail, &first, &details, &gone, &pls); err != nil {
		return Video{}, err
	}
	_ = json.Unmarshal([]byte(tags), &v.Tags)
	_ = json.Unmarshal([]byte(pls), &v.Playlists)
	if len(v.Playlists) == 0 {
		v.Playlists = nil
	}
	v.FirstSeen = unixOrZero(first)
	v.DetailsAt = unixOrZero(details)
	v.GoneAt = unixOrZero(gone)
	return v, nil
}

func unixOrZero(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// Video returns what is saved about one video.
func (s *Store) Video(id string) (Video, bool, error) {
	v, err := scanVideo(s.db.QueryRow(`SELECT `+videoCols+` FROM videos v WHERE v.id = ?`, id))
	if err == sql.ErrNoRows {
		return Video{}, false, nil
	}
	if err != nil {
		return Video{}, false, err
	}
	return v, true, nil
}

// Videos returns what is saved about the given ids; unknown ids are absent.
func (s *Store) Videos(ids []string) (map[string]Video, error) {
	out := map[string]Video{}
	for _, id := range ids {
		v, ok, err := s.Video(id)
		if err != nil {
			return nil, err
		}
		if ok {
			out[id] = v
		}
	}
	return out, nil
}

// AllVideos returns everything saved, unavailable videos first, then by
// channel and title.
func (s *Store) AllVideos() ([]Video, error) {
	rows, err := s.db.Query(`SELECT ` + videoCols + ` FROM videos v
		ORDER BY v.gone_at = 0, v.channel COLLATE NOCASE, v.title COLLATE NOCASE, v.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SavedPlaylist is a playlist whose videos' details are being saved. None are
// by default: you choose them, so a catch-all such as Liked videos is not.
type SavedPlaylist struct {
	ID, Title, URL string
}

const savedPlaylistsSchema = `
	CREATE TABLE IF NOT EXISTS saved_playlists (
		id    TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		url   TEXT NOT NULL
	);`

// SetPlaylistSaved starts or stops saving a playlist's video details. Starting
// again only refreshes its title and link. Stopping deletes nothing: see
// VideosOnlyIn and ForgetPlaylist.
func (s *Store) SetPlaylistSaved(p SavedPlaylist, on bool) error {
	if !on {
		_, err := s.db.Exec(`DELETE FROM saved_playlists WHERE id = ?`, p.ID)
		return err
	}
	_, err := s.db.Exec(`
		INSERT INTO saved_playlists (id, title, url) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET title = excluded.title, url = excluded.url`,
		p.ID, p.Title, p.URL)
	return err
}

// SavedPlaylists returns the playlists being saved, by title.
func (s *Store) SavedPlaylists() ([]SavedPlaylist, error) {
	rows, err := s.db.Query(`SELECT id, title, url FROM saved_playlists ORDER BY title COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedPlaylist
	for rows.Next() {
		var p SavedPlaylist
		if err := rows.Scan(&p.ID, &p.Title, &p.URL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlaylistIsSaved reports whether a playlist's video details are being saved.
func (s *Store) PlaylistIsSaved(id string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM saved_playlists WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// PlaylistVideoCounts returns how many saved videos each playlist holds,
// whether or not it is still being saved.
func (s *Store) PlaylistVideoCounts() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT playlist_id, count(*) FROM video_playlists GROUP BY playlist_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// VideosOnlyIn returns the saved videos that belong to this playlist and no
// other: what ForgetPlaylist would delete.
func (s *Store) VideosOnlyIn(playlistID string) ([]Video, error) {
	rows, err := s.db.Query(`SELECT `+videoCols+` FROM videos v
		WHERE EXISTS (SELECT 1 FROM video_playlists p WHERE p.video_id = v.id AND p.playlist_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM video_playlists p WHERE p.video_id = v.id AND p.playlist_id != ?)
		ORDER BY v.gone_at = 0, v.channel COLLATE NOCASE, v.title COLLATE NOCASE, v.id`,
		playlistID, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ForgetPlaylist removes a playlist from every saved video and deletes the
// videos left in no playlist. Videos also in another playlist are kept. It
// returns how many videos were deleted.
func (s *Store) ForgetPlaylist(playlistID string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM videos
		WHERE id IN (SELECT video_id FROM video_playlists WHERE playlist_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM video_playlists p WHERE p.video_id = videos.id AND p.playlist_id != ?)`,
		playlistID, playlistID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM video_playlists WHERE playlist_id = ?`, playlistID); err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}
