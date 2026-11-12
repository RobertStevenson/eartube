// Package archive saves the details of videos in your playlists (title,
// channel, description, upload date, tags and a thumbnail image), so eartube
// can still say what a video was after YouTube stops serving it, and exports
// what was saved.
package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// Fetcher looks up a video's details and saves its thumbnail into ThumbDir.
type Fetcher struct {
	// Lookup returns the details; ytdlp.Client.Details in the real program.
	Lookup   func(ctx context.Context, url string) (ytdlp.Details, error)
	ThumbDir string
	HTTP     *http.Client // nil means http.DefaultClient
}

// validID keeps video IDs safe to use as file names.
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// maxThumbBytes guards against saving something that is not a thumbnail.
const maxThumbBytes = 5 << 20

// Fetch returns everything worth saving about it. The thumbnail is best
// effort: if no image can be saved, the details are still returned.
func (f Fetcher) Fetch(ctx context.Context, it ytdlp.Item) (store.Video, error) {
	if !validID.MatchString(it.ID) {
		return store.Video{}, fmt.Errorf("unsafe video id %q", it.ID)
	}
	d, err := f.Lookup(ctx, it.URL)
	if err != nil {
		return store.Video{}, err
	}
	v := store.Video{ID: it.ID, Title: d.Title, Channel: d.Channel, ChannelURL: d.ChannelURL,
		Duration: d.Duration, UploadDate: d.UploadDate, Description: d.Description, Tags: d.Tags}
	for _, u := range d.Thumbnails {
		if p, err := f.saveThumb(ctx, it.ID, u); err == nil {
			v.Thumbnail = p
			break
		} else if ctx.Err() != nil {
			return store.Video{}, ctx.Err()
		}
	}
	return v, nil
}

func (f Fetcher) saveThumb(ctx context.Context, id, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	c := f.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("thumbnail: %s", resp.Status)
	}
	ext := imageExt(resp.Header.Get("Content-Type"))
	if ext == "" {
		return "", fmt.Errorf("thumbnail: not an image (%q)", resp.Header.Get("Content-Type"))
	}
	if err := os.MkdirAll(f.ThumbDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(f.ThumbDir, id+".*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxThumbBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if n > maxThumbBytes {
		return "", errors.New("thumbnail: too large")
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { // CreateTemp makes it 0600
		return "", err
	}
	path := filepath.Join(f.ThumbDir, id+ext)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func imageExt(contentType string) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	switch mt {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	}
	return ""
}

// DateFormat is how dates are shown and exported.
const DateFormat = "2006-01-02"

// FmtDuration shows seconds as h:mm:ss or m:ss; empty if unknown.
func FmtDuration(s float64) string {
	if s <= 0 {
		return ""
	}
	t := int(s)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// URL is the video's YouTube page.
func URL(id string) string { return "https://www.youtube.com/watch?v=" + id }

// oneLine collapses whitespace so a value cannot break a Markdown line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
