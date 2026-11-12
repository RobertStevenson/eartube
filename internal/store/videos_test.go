package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func mustVideo(t *testing.T, s *Store, id string) Video {
	t.Helper()
	v, ok, err := s.Video(id)
	if err != nil || !ok {
		t.Fatalf("video %s: ok=%v err=%v", id, ok, err)
	}
	return v
}

func TestRecordPlaylistSavesBasicsAndMembership(t *testing.T) {
	s, _ := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	if err := s.RecordPlaylist("PL1", "Podcasts", []Sighting{
		{ID: "a", Title: "Episode A", Channel: "Chan", Duration: 600},
	}, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordPlaylist("WL", "Watch later", []Sighting{{ID: "a", Title: "Episode A", Channel: "Chan", Duration: 600}}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	v := mustVideo(t, s, "a")
	if v.Title != "Episode A" || v.Channel != "Chan" || v.Duration != 600 {
		t.Errorf("basics = %+v", v)
	}
	if !v.FirstSeen.Equal(t0) {
		t.Errorf("first seen = %v, want %v (the first sighting, not the latest)", v.FirstSeen, t0)
	}
	if want := []string{"Podcasts", "Watch later"}; !reflect.DeepEqual(v.Playlists, want) {
		t.Errorf("playlists = %q, want %q", v.Playlists, want)
	}
	if v.HasDetails() || !v.GoneAt.IsZero() {
		t.Errorf("a new video has no details and is not gone: %+v", v)
	}
}

// The point of the archive: once a video goes private, the listing shows a
// placeholder, and nothing saved earlier may be lost.
func TestAGoneVideoKeepsWhatWasSaved(t *testing.T) {
	s, path := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	s.RecordPlaylist("PL1", "Podcasts", []Sighting{{ID: "a", Title: "Episode A", Channel: "Chan", Duration: 600}}, t0)
	if err := s.SaveDetails(Video{ID: "a", Title: "Episode A", Channel: "Chan", ChannelURL: "https://yt/@chan",
		UploadDate: "2024-03-01", Description: "All about A.", Tags: []string{"x", "y"}, Thumbnail: "/t/a.jpg"}, t0); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(48 * time.Hour)
	if err := s.RecordPlaylist("PL1", "Podcasts", []Sighting{{ID: "a", Title: "[Private video]", Gone: true}}, t1); err != nil {
		t.Fatal(err)
	}
	// Seeing it gone again later must not move the date it went.
	s.RecordPlaylist("PL1", "Podcasts", []Sighting{{ID: "a", Gone: true}}, t1.Add(time.Hour))
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v := mustVideo(t, s, "a")
	if v.Title != "Episode A" || v.Description != "All about A." || v.UploadDate != "2024-03-01" ||
		v.ChannelURL != "https://yt/@chan" || v.Thumbnail != "/t/a.jpg" || !reflect.DeepEqual(v.Tags, []string{"x", "y"}) {
		t.Errorf("saved details were lost: %+v", v)
	}
	if !v.GoneAt.Equal(t1) {
		t.Errorf("gone at = %v, want %v", v.GoneAt, t1)
	}

	// Back again (made public): no longer gone.
	s.RecordPlaylist("PL1", "Podcasts", []Sighting{{ID: "a", Title: "Episode A", Channel: "Chan", Duration: 600}}, t1.Add(2*time.Hour))
	if v := mustVideo(t, s, "a"); !v.GoneAt.IsZero() {
		t.Errorf("a video playable again should not be marked gone: %v", v.GoneAt)
	}
}

func TestAVideoGoneBeforeItWasEverSeenIsStillRecorded(t *testing.T) {
	s, _ := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	s.RecordPlaylist("PL1", "Podcasts", []Sighting{{ID: "z", Title: "[Deleted video]", Gone: true}}, t0)
	v := mustVideo(t, s, "z")
	if v.Title != "" || !v.GoneAt.Equal(t0) || !reflect.DeepEqual(v.Playlists, []string{"Podcasts"}) {
		t.Errorf("got %+v", v)
	}
}

func TestSaveDetailsDoesNotBlankOutFields(t *testing.T) {
	s, _ := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	s.SaveDetails(Video{ID: "a", Title: "A", Description: "desc", Tags: []string{"t"}, Thumbnail: "/a.jpg"}, t0)
	s.SaveDetails(Video{ID: "a", Title: "A2"}, t0.Add(time.Hour))
	v := mustVideo(t, s, "a")
	if v.Title != "A2" || v.Description != "desc" || !reflect.DeepEqual(v.Tags, []string{"t"}) || v.Thumbnail != "/a.jpg" {
		t.Errorf("got %+v", v)
	}
	if !v.DetailsAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("details at = %v", v.DetailsAt)
	}
}

func TestVideosAndAllVideos(t *testing.T) {
	s, _ := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	s.RecordPlaylist("PL1", "P", []Sighting{
		{ID: "b", Title: "Bee", Channel: "Chan"},
		{ID: "a", Title: "Ant", Channel: "Chan"},
		{ID: "g", Gone: true},
	}, t0)
	got, err := s.Videos([]string{"a", "nope", "g"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a"].Title != "Ant" || got["g"].GoneAt.IsZero() {
		t.Errorf("Videos = %+v", got)
	}
	all, err := s.AllVideos()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, v := range all {
		ids = append(ids, v.ID)
	}
	if want := []string{"g", "a", "b"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("order = %v, want %v (gone first, then by channel and title)", ids, want)
	}
}

func TestSavedPlaylistsAreChosenAndNoneByDefault(t *testing.T) {
	s, path := openTemp(t)
	if got, err := s.SavedPlaylists(); err != nil || len(got) != 0 {
		t.Fatalf("a new database saves no playlists: %v %v", got, err)
	}
	s.SetPlaylistSaved(SavedPlaylist{ID: "PL2", Title: "Zoology", URL: "u2"}, true)
	s.SetPlaylistSaved(SavedPlaylist{ID: "PL1", Title: "Old name", URL: "u1"}, true)
	s.SetPlaylistSaved(SavedPlaylist{ID: "PL1", Title: "Algebra", URL: "u1"}, true) // renamed
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, _ := s.SavedPlaylists()
	want := []SavedPlaylist{{"PL1", "Algebra", "u1"}, {"PL2", "Zoology", "u2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("saved = %v, want %v", got, want)
	}
	s.SetPlaylistSaved(SavedPlaylist{ID: "PL2"}, false)
	if on, _ := s.PlaylistIsSaved("PL2"); on {
		t.Error("PL2 still saved after stopping")
	}
	if on, _ := s.PlaylistIsSaved("PL1"); !on {
		t.Error("PL1 should still be saved")
	}
}

// Forgetting a playlist deletes only the videos that were in no other playlist.
func TestForgetPlaylistKeepsVideosSharedWithAnotherPlaylist(t *testing.T) {
	s, _ := openTemp(t)
	t0 := time.Unix(1_700_000_000, 0)
	s.RecordPlaylist("LL", "Liked videos", []Sighting{
		{ID: "only", Title: "Only liked"}, {ID: "both", Title: "Liked and kept"}, {ID: "gone", Gone: true},
	}, t0)
	s.RecordPlaylist("PL1", "Reference", []Sighting{{ID: "both", Title: "Liked and kept"}, {ID: "ref", Title: "Ref"}}, t0)
	s.SaveDetails(Video{ID: "stray", Title: "no playlist"}, t0) // not in LL: must survive

	doomed, err := s.VideosOnlyIn("LL")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, v := range doomed {
		ids = append(ids, v.ID)
	}
	if want := []string{"gone", "only"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("only in LL = %v, want %v", ids, want)
	}
	counts, _ := s.PlaylistVideoCounts()
	if counts["LL"] != 3 || counts["PL1"] != 2 {
		t.Errorf("counts = %v", counts)
	}

	n, err := s.ForgetPlaylist("LL")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, err %v; want 2", n, err)
	}
	for _, id := range []string{"only", "gone"} {
		if _, ok, _ := s.Video(id); ok {
			t.Errorf("%s should be deleted", id)
		}
	}
	for _, id := range []string{"both", "ref", "stray"} {
		if _, ok, _ := s.Video(id); !ok {
			t.Errorf("%s should be kept", id)
		}
	}
	if v := mustVideo(t, s, "both"); !reflect.DeepEqual(v.Playlists, []string{"Reference"}) {
		t.Errorf("both is now in %v, want only Reference", v.Playlists)
	}
	if counts, _ := s.PlaylistVideoCounts(); counts["LL"] != 0 {
		t.Errorf("LL still has %d videos", counts["LL"])
	}
}
