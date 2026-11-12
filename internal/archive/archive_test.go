package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

func thumbServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("JPEGDATA"))
		case "/page.html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		case "/big.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(make([]byte, maxThumbBytes+1))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchSavesDetailsAndTheFirstThumbnailThatWorks(t *testing.T) {
	srv := thumbServer(t)
	dir := t.TempDir()
	f := Fetcher{ThumbDir: dir, Lookup: func(_ context.Context, url string) (ytdlp.Details, error) {
		if url != "https://yt/v1" {
			t.Errorf("looked up %q", url)
		}
		return ytdlp.Details{ID: "v1", Title: "T", Channel: "C", ChannelURL: "https://yt/@c", Duration: 61,
			UploadDate: "2024-01-02", Description: "D", Tags: []string{"a"},
			Thumbnails: []string{srv.URL + "/missing.jpg", srv.URL + "/page.html", srv.URL + "/big.jpg", srv.URL + "/good.jpg"}}, nil
	}}
	v, err := f.Fetch(context.Background(), ytdlp.Item{ID: "v1", URL: "https://yt/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != "v1" || v.Title != "T" || v.Channel != "C" || v.ChannelURL != "https://yt/@c" || v.Duration != 61 ||
		v.UploadDate != "2024-01-02" || v.Description != "D" || len(v.Tags) != 1 {
		t.Errorf("got %+v", v)
	}
	want := filepath.Join(dir, "v1.jpg")
	if v.Thumbnail != want {
		t.Fatalf("thumbnail = %q, want %q", v.Thumbnail, want)
	}
	if st, err := os.Stat(want); err != nil || st.Mode().Perm() != 0o644 {
		t.Errorf("thumbnail file mode: %v %v, want 0644 like eartube's other files", st.Mode(), err)
	}
	if b, _ := os.ReadFile(want); string(b) != "JPEGDATA" {
		t.Errorf("thumbnail file holds %d bytes, want the 8-byte good.jpg", len(b))
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(left) > 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestFetchKeepsTheDetailsWhenNoThumbnailCanBeSaved(t *testing.T) {
	srv := thumbServer(t)
	f := Fetcher{ThumbDir: t.TempDir(), Lookup: func(context.Context, string) (ytdlp.Details, error) {
		return ytdlp.Details{ID: "v1", Title: "T", Thumbnails: []string{srv.URL + "/missing.jpg"}}, nil
	}}
	v, err := f.Fetch(context.Background(), ytdlp.Item{ID: "v1"})
	if err != nil || v.Title != "T" || v.Thumbnail != "" {
		t.Errorf("v=%+v err=%v", v, err)
	}
}

func TestFetchReportsLookupFailuresAndUnsafeIDs(t *testing.T) {
	boom := errors.New("Private video")
	f := Fetcher{ThumbDir: t.TempDir(), Lookup: func(context.Context, string) (ytdlp.Details, error) {
		return ytdlp.Details{}, boom
	}}
	if _, err := f.Fetch(context.Background(), ytdlp.Item{ID: "v1"}); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	if _, err := f.Fetch(context.Background(), ytdlp.Item{ID: "../x"}); err == nil {
		t.Error("an id with a path in it must be refused")
	}
}

func sample() []store.Video {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	return []store.Video{
		{ID: "gone1", Title: "Lost Talk", Channel: "Speaker", UploadDate: "2019-05-04", Duration: 3725,
			Description: "Line one.\n\nLine three.", Tags: []string{"talk", "clojure"}, Thumbnail: "/thumbs/gone1.jpg",
			Playlists: []string{"Clojure!", "Reference"}, FirstSeen: t0, DetailsAt: t0, GoneAt: t0.Add(240 * time.Hour)},
		{ID: "gone2", Playlists: []string{"Inbox"}, FirstSeen: t0, GoneAt: t0},
		{ID: "ok1", Title: "Still Here", Channel: "Chan", Duration: 90, FirstSeen: t0},
	}
}

func TestExportJSON(t *testing.T) {
	var b bytes.Buffer
	if err := Export(&b, sample(), "json", false); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b.String())
	}
	if len(got) != 3 {
		t.Fatalf("got %d videos", len(got))
	}
	g := got[0]
	for k, want := range map[string]any{
		"id": "gone1", "url": "https://www.youtube.com/watch?v=gone1", "title": "Lost Talk",
		"upload_date": "2019-05-04", "duration_seconds": 3725.0, "description": "Line one.\n\nLine three.",
		"thumbnail_file": "/thumbs/gone1.jpg", "available": false, "unavailable_since": "2026-11-11",
		"details_saved": "2026-11-01",
	} {
		if g[k] != want {
			t.Errorf("%s = %#v, want %#v", k, g[k], want)
		}
	}
	if got[2]["available"] != true || got[2]["unavailable_since"] != nil {
		t.Errorf("available video exported as %v", got[2])
	}

	b.Reset()
	Export(&b, sample(), "json", true)
	json.Unmarshal(b.Bytes(), &got)
	if len(got) != 2 {
		t.Errorf("only-unavailable export has %d videos, want 2", len(got))
	}
}

func TestExportMarkdown(t *testing.T) {
	var b bytes.Buffer
	if err := Export(&b, sample(), "markdown", true); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{
		"# eartube archive\n\n2 videos, 2 no longer available.\n",
		"## Lost Talk\n",
		"- **Status:** unavailable since 2026-11-11",
		"- **Uploaded:** 2019-05-04\n",
		"- **Length:** 1:02:05\n",
		"- **Playlists:** Clojure!, Reference\n",
		"> Line one.\n>\n> Line three.\n",
		"## (title never saved)\n",
		"- **Link:** https://www.youtube.com/watch?v=gone2\n",
		"- **Saved:** only its link and playlists: it was already gone when first seen\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Still Here") {
		t.Error("only-unavailable export includes an available video")
	}
}

func TestExportRejectsAnUnknownFormat(t *testing.T) {
	if err := Export(&bytes.Buffer{}, nil, "xml", false); err == nil || !strings.Contains(err.Error(), "json or markdown") {
		t.Errorf("err = %v", err)
	}
}
