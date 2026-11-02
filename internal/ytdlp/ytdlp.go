// Package ytdlp wraps the yt-dlp binary for search and playlist listing.
package ytdlp

import (
	"context"
	"encoding/json"
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
	return c.run(ctx, "https://www.youtube.com/feed/playlists")
}

// Playlist lists the videos in a playlist URL.
func (c Client) Playlist(ctx context.Context, url string) ([]Item, error) {
	return c.run(ctx, url)
}
