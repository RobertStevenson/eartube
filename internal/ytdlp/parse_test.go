package ytdlp

import "testing"

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
