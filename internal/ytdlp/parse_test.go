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

// Shapes copied from a real playlist listing (yt-dlp 2026.08.19): unavailable
// videos keep their id but get a placeholder title and no channel or length.
// A real title may also start with a bracket.
func TestParseMarksUnavailableEntries(t *testing.T) {
	in := []byte(`{"entries": [
		{"id": "_KRwAzDH_uM", "title": "[4K] Cedar Point & Lake Erie Railroad Steam Train", "channel": "Dynamo Productions", "duration": 871,
		 "url": "https://www.youtube.com/watch?v=_KRwAzDH_uM"},
		{"id": "CeBGxKcV2CQ", "title": "[Deleted video]", "channel": null, "duration": null,
		 "url": "https://www.youtube.com/watch?v=CeBGxKcV2CQ"},
		{"id": "cwGMdiWQ55g", "title": "[Private video]", "channel": null, "duration": null,
		 "url": "https://www.youtube.com/watch?v=cwGMdiWQ55g"},
		{"id": "x1", "title": "[Private video]", "channel": "Someone",
		 "url": "https://www.youtube.com/watch?v=x1"}
	]}`)
	got, err := parse(in)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"_KRwAzDH_uM": false, "CeBGxKcV2CQ": true, "cwGMdiWQ55g": true, "x1": false}
	if len(got) != len(want) {
		t.Fatalf("got %d items: %+v", len(got), got)
	}
	for _, it := range got {
		if it.Unavailable != want[it.ID] {
			t.Errorf("%s (%q): unavailable = %v, want %v", it.ID, it.Title, it.Unavailable, want[it.ID])
		}
	}
}

// Trimmed from real `yt-dlp -J --skip-download` output for one video.
func TestParseDetails(t *testing.T) {
	in := []byte(`{"id": "_KRwAzDH_uM", "title": "[4K] Cedar Point & Lake Erie Railroad Steam Train",
		"channel": "Dynamo Productions", "uploader": "Dynamo Productions",
		"channel_url": "https://www.youtube.com/channel/UCUavkI6ArT44SRiAtnoN45Q",
		"uploader_url": "https://www.youtube.com/@DynamoProductions-trains",
		"duration": 870, "upload_date": "20230503",
		"description": "The Cedar Point & Lake Erie Railroad operates...\nSecond line.",
		"tags": ["Cedar Point", "steam train"],
		"thumbnail": "https://i.ytimg.com/vi_webp/_KRwAzDH_uM/maxresdefault.webp",
		"thumbnails": [{"url": "https://i.ytimg.com/vi/_KRwAzDH_uM/mqdefault.jpg", "width": 320}],
		"formats": [{"format_id": "251"}]}`)
	d, err := parseDetails(in)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "_KRwAzDH_uM" || d.Channel != "Dynamo Productions" || d.Duration != 870 ||
		d.ChannelURL != "https://www.youtube.com/channel/UCUavkI6ArT44SRiAtnoN45Q" ||
		d.Description != "The Cedar Point & Lake Erie Railroad operates...\nSecond line." {
		t.Errorf("got %+v", d)
	}
	if d.UploadDate != "2023-05-03" {
		t.Errorf("upload date = %q, want 2023-05-03", d.UploadDate)
	}
	if len(d.Tags) != 2 || d.Tags[1] != "steam train" {
		t.Errorf("tags = %q", d.Tags)
	}
	want := []string{"https://i.ytimg.com/vi/_KRwAzDH_uM/hqdefault.jpg", "https://i.ytimg.com/vi_webp/_KRwAzDH_uM/maxresdefault.webp"}
	if len(d.Thumbnails) != 2 || d.Thumbnails[0] != want[0] || d.Thumbnails[1] != want[1] {
		t.Errorf("thumbnails = %q, want %q", d.Thumbnails, want)
	}
}

func TestParseDetailsFallsBackToUploader(t *testing.T) {
	d, err := parseDetails([]byte(`{"id": "v", "uploader": "Up", "uploader_url": "https://yt/@up", "upload_date": "2024"}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Channel != "Up" || d.ChannelURL != "https://yt/@up" || d.UploadDate != "" {
		t.Errorf("got %+v", d)
	}
	if _, err := parseDetails([]byte(`{}`)); err == nil {
		t.Error("a result with no id should be an error")
	}
}
