//go:build integration

package ytdlp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// These tests run the real yt-dlp against YouTube:
//
//	go test -tags integration ./internal/ytdlp
//
// Use -short to skip them. TestPlaylistsWithCookies and
// TestSubscriptionsWithCookies also need
// EARTUBE_BROWSER set to a yt-dlp browser spec, e.g. brave+gnomekeyring.

func needYtdlp(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestSearch(t *testing.T) {
	needYtdlp(t)
	items, err := Client{}.Search(ctx(t), "go bubbletea tutorial", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || len(items) > 3 {
		t.Fatalf("got %d items, want 1..3", len(items))
	}
	for _, it := range items {
		if it.ID == "" || it.Title == "" || it.Channel == "" || it.Duration <= 0 ||
			!strings.HasPrefix(it.URL, "https://www.youtube.com/watch?v=") {
			t.Errorf("incomplete item: %+v", it)
		}
	}
}

// Essence of linear algebra (3Blue1Brown): a long-lived public playlist.
const publicPlaylist = "https://www.youtube.com/playlist?list=PLZHQObOWTQDPD3MizzM2xVFitgF8hE_ab"

func TestPublicPlaylist(t *testing.T) {
	needYtdlp(t)
	items, err := Client{}.Playlist(ctx(t), publicPlaylist)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 10 {
		t.Fatalf("got %d items, want at least 10", len(items))
	}
	if !strings.Contains(items[0].Title, "Vectors") {
		t.Errorf("first item = %q, want the Vectors chapter", items[0].Title)
	}
}

func TestPlaylistsWithCookies(t *testing.T) {
	needYtdlp(t)
	browser := os.Getenv("EARTUBE_BROWSER")
	if browser == "" {
		t.Skip("set EARTUBE_BROWSER (e.g. brave+gnomekeyring) to test signed-in playlists")
	}
	items, err := Client{Browser: browser}.Playlists(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no playlists; is the browser signed in to YouTube?")
	}
	for _, it := range items {
		if it.Title == "" || !strings.Contains(it.URL, "list=") {
			t.Errorf("odd playlist entry: %+v", it)
		}
	}
}

func TestSubscriptionsWithCookies(t *testing.T) {
	needYtdlp(t)
	browser := os.Getenv("EARTUBE_BROWSER")
	if browser == "" {
		t.Skip("set EARTUBE_BROWSER (e.g. brave+gnomekeyring) to test the subscriptions feeds")
	}
	c := Client{Browser: browser}
	videos, err := c.Subscriptions(ctx(t), false, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) == 0 || len(videos) > 5 {
		t.Fatalf("got %d videos, want 1..5; is the browser signed in and subscribed to anything?", len(videos))
	}
	for _, it := range videos {
		if it.ID == "" || it.Title == "" || it.Channel == "" || it.Short() {
			t.Errorf("odd subscription video: %+v", it)
		}
	}
	shorts, err := c.Subscriptions(ctx(t), true, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(shorts) > 5 {
		t.Fatalf("got %d Shorts, want at most 5", len(shorts))
	}
	for _, it := range shorts {
		if it.ID == "" || !it.Short() || it.Unavailable {
			t.Errorf("odd subscription Short: %+v", it)
		}
	}
}

func TestFailureIsReportedAsError(t *testing.T) {
	needYtdlp(t)
	_, err := Client{Browser: "no-such-browser"}.Search(ctx(t), "x", 1)
	if err == nil || !strings.Contains(err.Error(), "yt-dlp failed") {
		t.Fatalf("err = %v, want a 'yt-dlp failed' error", err)
	}
}

func TestResolveReturnsOneStreamURL(t *testing.T) {
	needYtdlp(t)
	// Resolve ignores the browser on purpose, so even a Client with a bogus one must work.
	u, err := Client{Browser: "no-such-browser"}.Resolve(ctx(t), "https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "https://") || strings.Contains(u, "\n") {
		t.Fatalf("got %q, want a single https URL", u)
	}
}

func TestResolveFailsForUnavailableVideo(t *testing.T) {
	needYtdlp(t)
	if _, err := (Client{}).Resolve(ctx(t), "https://www.youtube.com/watch?v=aaaaaaaaaaa"); err == nil {
		t.Fatal("expected an error for a video that does not exist")
	}
}

// "Me at the zoo", YouTube's first video: public since 2005 and unlikely to go.
func TestDetailsOfARealVideo(t *testing.T) {
	needYtdlp(t)
	d, err := (Client{}).Details(ctx(t), "https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "jNQXAC9IVRw" || d.Title == "" || d.Channel == "" || d.Description == "" || d.Duration <= 0 {
		t.Errorf("got %+v", d)
	}
	if d.UploadDate != "2005-04-24" {
		t.Errorf("upload date = %q, want 2005-04-24", d.UploadDate)
	}
	if len(d.Thumbnails) == 0 || !strings.HasPrefix(d.Thumbnails[0], "https://") {
		t.Errorf("thumbnails = %q", d.Thumbnails)
	}
}

func TestDetailsFailsForUnavailableVideo(t *testing.T) {
	needYtdlp(t)
	if _, err := (Client{}).Details(ctx(t), "https://www.youtube.com/watch?v=aaaaaaaaaaa"); err == nil {
		t.Fatal("expected an error for a video that does not exist")
	}
}
