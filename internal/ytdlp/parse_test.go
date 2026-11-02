package ytdlp

import (
	"context"
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	in := []byte(`{"entries": [
		{"id": "v1", "title": "With channel", "channel": "Chan", "uploader": "Up", "duration": 61.5,
		 "url": "https://www.youtube.com/watch?v=v1"},
		{"id": "v2", "title": "Uploader fallback, no url", "uploader": "Up2", "duration": 10},
		{"id": "PL9", "title": "A playlist", "url": "https://www.youtube.com/playlist?list=PL9"},
		null
	]}`)
	got, err := parse(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{
		{ID: "v1", Title: "With channel", Channel: "Chan", Duration: 61.5, URL: "https://www.youtube.com/watch?v=v1"},
		{ID: "v2", Title: "Uploader fallback, no url", Channel: "Up2", Duration: 10, URL: "https://www.youtube.com/watch?v=v2"},
		{ID: "PL9", Title: "A playlist", URL: "https://www.youtube.com/playlist?list=PL9"},
	} // the null entry is skipped
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := parse([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestPlaylistsWithoutBrowserFailsFast(t *testing.T) {
	// No subprocess is started: this must fail before touching yt-dlp.
	_, err := Client{}.Playlists(context.Background())
	if !errors.Is(err, ErrNoBrowser) {
		t.Fatalf("err = %v, want ErrNoBrowser", err)
	}
}

func TestParseResolve(t *testing.T) {
	good := "https://rr1.googlevideo.com/videoplayback?expire=1&x=2\n"
	if got, err := parseResolve([]byte(good)); err != nil || got != "https://rr1.googlevideo.com/videoplayback?expire=1&x=2" {
		t.Errorf("got %q, %v", got, err)
	}
	for name, in := range map[string]string{
		"empty":           "",
		"blank lines":     "\n\n",
		"video and audio": "https://a/video\nhttps://a/audio\n",
		"not a url":       "ERROR: nope\n",
	} {
		if _, err := parseResolve([]byte(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
