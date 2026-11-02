// Package ytdlp wraps the yt-dlp binary for search and playlist listing.
package ytdlp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Item is a video or playlist entry.
type Item struct {
	ID       string
	Title    string
	Channel  string
	Duration float64 // seconds; 0 if unknown
	URL      string
}

// ErrNoBrowser is returned when your playlists are requested but no browser
// is configured to supply YouTube cookies.
var ErrNoBrowser = errors.New("your playlists need a signed-in browser: set --browser, EARTUBE_BROWSER, or browser in the config file (search still works)")

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

func (c Client) run(ctx context.Context, target string) ([]Item, error) {
	args := []string{"--flat-playlist", "-J", "--no-warnings"}
	if c.Browser != "" {
		args = append(args, "--cookies-from-browser", c.Browser)
	}
	args = append(args, target)

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
		items = append(items, Item{ID: e.ID, Title: e.Title, Channel: ch, Duration: e.Duration, URL: url})
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
