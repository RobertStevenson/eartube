// Package ytdlp wraps the yt-dlp binary for search, playlist and subscription listing, and
// looking up a video's details.
package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Item is a video or playlist entry.
type Item struct {
	ID       string
	Title    string
	Channel  string
	Duration float64 // seconds; 0 if unknown
	URL      string
	// Unavailable is set for a playlist entry YouTube no longer serves
	// (private, deleted): yt-dlp lists it with a placeholder title.
	Unavailable bool `json:",omitempty"`
}

// Short reports whether it is a YouTube Short, which yt-dlp lists by its
// /shorts/ link.
func (it Item) Short() bool { return strings.Contains(it.URL, "/shorts/") }

// placeholder matches the titles yt-dlp gives unavailable playlist entries,
// such as "[Private video]" and "[Deleted video]". Real titles can start with
// a bracket too ("[4K] ..."), so the whole title must match, and the entry
// must also have no channel.
var placeholder = regexp.MustCompile(`^\[[A-Za-z ]+ video\]$`)

// ErrNoBrowser is returned when your playlists are requested but no browser
// is configured to supply YouTube cookies.
var ErrNoBrowser = errors.New("your playlists and subscriptions need a signed-in browser: set --browser, EARTUBE_BROWSER, or browser in the config file (search still works)")

type Client struct {
	// Browser to read cookies from (e.g. "brave"). Empty disables cookies.
	Browser string
}

type dump struct {
	Entries []struct {
		ID       string  `json:"id"`
		Title    string  `json:"title"`
		Channel  string  `json:"channel"`
		Uploader string  `json:"uploader"`
		Duration float64 `json:"duration"`
		URL      string  `json:"url"`
	} `json:"entries"`
}

func (c Client) run(ctx context.Context, target string, extra ...string) ([]Item, error) {
	args := []string{"--flat-playlist", "-J", "--no-warnings"}
	if c.Browser != "" {
		args = append(args, "--cookies-from-browser", c.Browser)
	}
	args = append(append(args, extra...), target)

	out, err := exec.CommandContext(ctx, "yt-dlp", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp failed (try updating yt-dlp): %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return parse(out)
}

// parse converts yt-dlp's flat-playlist JSON into Items.
func parse(out []byte) ([]Item, error) {
	var d dump
	if err := json.Unmarshal(out, &d); err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(d.Entries))
	for _, e := range d.Entries {
		if e.ID == "" && e.URL == "" {
			continue // yt-dlp emits null entries for unavailable videos
		}
		ch := e.Channel
		if ch == "" {
			ch = e.Uploader
		}
		url := e.URL
		if url == "" {
			url = "https://www.youtube.com/watch?v=" + e.ID
		}
		gone := ch == "" && placeholder.MatchString(e.Title)
		items = append(items, Item{ID: e.ID, Title: e.Title, Channel: ch, Duration: e.Duration, URL: url, Unavailable: gone})
	}
	return items, nil
}

// Search returns up to n video results for query.
func (c Client) Search(ctx context.Context, query string, n int) ([]Item, error) {
	return c.run(ctx, fmt.Sprintf("ytsearch%d:%s", n, query))
}

// Playlists lists the signed-in user's playlists (requires cookies).
func (c Client) Playlists(ctx context.Context) ([]Item, error) {
	if c.Browser == "" {
		return nil, ErrNoBrowser
	}
	return c.run(ctx, "https://www.youtube.com/feed/playlists")
}

// subscriptionsURL is the signed-in user's feed of new uploads from the
// channels they subscribe to. Its Shorts are a separate feed, under /shorts.
const subscriptionsURL = "https://www.youtube.com/feed/subscriptions"

// Subscriptions lists the newest n uploads from the signed-in user's
// subscriptions (requires cookies), newest first: videos, or with shorts set,
// Shorts. YouTube gives neither feed upload dates, and Shorts come without a
// channel or length, and sometimes without a title.
func (c Client) Subscriptions(ctx context.Context, shorts bool, n int) ([]Item, error) {
	if c.Browser == "" {
		return nil, ErrNoBrowser
	}
	target := subscriptionsURL
	if shorts {
		target += "/shorts"
	}
	return c.run(ctx, target, "--playlist-end", strconv.Itoa(n))
}

// Playlist lists the videos in a playlist URL.
func (c Client) Playlist(ctx context.Context, url string) ([]Item, error) {
	return c.run(ctx, url)
}

// Resolve returns a direct audio stream URL for a video page, so playback can
// start without mpv running yt-dlp again. It deliberately ignores c.Browser:
// decrypting cookies costs seconds, and videos that need a sign-in simply fail
// here, which callers treat as "load it the normal way".
func (c Client) Resolve(ctx context.Context, url string) (string, error) {
	out, err := exec.CommandContext(ctx, "yt-dlp", "-g", "-f", "bestaudio/best",
		"--no-playlist", "--no-warnings", url).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("resolve failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return parseResolve(out)
}

// parseResolve accepts exactly one URL. Several lines means yt-dlp chose
// separate video and audio streams, which can't be handed to mpv as one file.
func parseResolve(out []byte) (string, error) {
	var urls []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			urls = append(urls, l)
		}
	}
	if len(urls) != 1 || !strings.HasPrefix(urls[0], "http") {
		return "", fmt.Errorf("expected one stream URL, got %d line(s)", len(urls))
	}
	return urls[0], nil
}

// Details is the full description of one video, as saved in the archive.
type Details struct {
	ID          string
	Title       string
	Channel     string
	ChannelURL  string
	Duration    float64
	UploadDate  string // YYYY-MM-DD; empty if unknown
	Description string
	Tags        []string
	// Thumbnails are image URLs to try in order, smallest suitable first.
	Thumbnails []string
}

type videoJSON struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Channel     string   `json:"channel"`
	Uploader    string   `json:"uploader"`
	ChannelURL  string   `json:"channel_url"`
	UploaderURL string   `json:"uploader_url"`
	Duration    float64  `json:"duration"`
	UploadDate  string   `json:"upload_date"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Thumbnail   string   `json:"thumbnail"`
	Thumbnails  []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"thumbnails"`
}

// Details looks up a video's full details (description, upload date, tags,
// thumbnail). Like downloads it tries without cookies first, since decrypting
// them costs seconds, and retries with them only if that fails.
func (c Client) Details(ctx context.Context, url string) (Details, error) {
	d, err := c.details(ctx, url, "")
	if err != nil && c.Browser != "" && ctx.Err() == nil {
		var err2 error
		if d, err2 = c.details(ctx, url, c.Browser); err2 == nil {
			return d, nil
		}
		return Details{}, fmt.Errorf("%w (retried with cookies: %v)", err, err2)
	}
	return d, err
}

func (c Client) details(ctx context.Context, url, browser string) (Details, error) {
	args := []string{"-J", "--skip-download", "--no-playlist", "--no-warnings"}
	if browser != "" {
		args = append(args, "--cookies-from-browser", browser)
	}
	out, err := exec.CommandContext(ctx, "yt-dlp", append(args, url)...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return Details{}, fmt.Errorf("details failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return Details{}, err
	}
	return parseDetails(out)
}

// parseDetails converts yt-dlp's single-video JSON into Details.
func parseDetails(out []byte) (Details, error) {
	var v videoJSON
	if err := json.Unmarshal(out, &v); err != nil {
		return Details{}, err
	}
	if v.ID == "" {
		return Details{}, errors.New("details: yt-dlp returned no video id")
	}
	d := Details{ID: v.ID, Title: v.Title, Channel: v.Channel, ChannelURL: v.ChannelURL,
		Duration: v.Duration, Description: v.Description, Tags: v.Tags}
	if d.Channel == "" {
		d.Channel = v.Uploader
	}
	if d.ChannelURL == "" {
		d.ChannelURL = v.UploaderURL
	}
	if u := v.UploadDate; len(u) == 8 {
		d.UploadDate = u[:4] + "-" + u[4:6] + "-" + u[6:]
	}
	// hqdefault.jpg (480x360, a few tens of KB) is plenty to remember a video
	// by, and its URL is stable. yt-dlp's own pick is often a large WebP.
	d.Thumbnails = []string{"https://i.ytimg.com/vi/" + v.ID + "/hqdefault.jpg"}
	if v.Thumbnail != "" {
		d.Thumbnails = append(d.Thumbnails, v.Thumbnail)
	}
	return d, nil
}
