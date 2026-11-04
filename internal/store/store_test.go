package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePos("a", 42.5); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlayed("b", true); err != nil {
		t.Fatal(err)
	}
	// Played clears the resume position; SavePos leaves the flag alone.
	s.SavePos("b", 10)
	s.SetPlayed("a", true)
	s.SavePos("a", 7)
	type q struct{ N int }
	if err := s.SaveJSON("queue", q{3}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path) // reopen: data must persist
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	all, _ := s.All()
	if a := all["a"]; !a.Played || a.Pos != 7 {
		t.Errorf("a = %+v", a)
	}
	if b := all["b"]; !b.Played || b.Pos != 10 {
		t.Errorf("b = %+v", b)
	}
	var got q
	if ok, err := s.LoadJSON("queue", &got); !ok || err != nil || got.N != 3 {
		t.Errorf("queue: ok=%v err=%v got=%+v", ok, err, got)
	}
	if ok, _ := s.LoadJSON("missing", &got); ok {
		t.Error("missing key reported present")
	}
}

func TestDownloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Unix(1_700_000_000, 0)
	for _, d := range []Download{
		{ID: "a", Path: "/d/a.m4a", Bytes: 100, LastUsed: t0},
		{ID: "b", Path: "/d/b.webm", Bytes: 250, LastUsed: t0.Add(time.Hour)},
	} {
		if err := s.AddDownload(d); err != nil {
			t.Fatal(err)
		}
	}
	// Re-adding replaces; touching only moves last_used; removing forgets.
	s.AddDownload(Download{ID: "a", Path: "/d/a2.m4a", Bytes: 120, LastUsed: t0})
	s.TouchDownload("b", t0.Add(2*time.Hour))
	s.RemoveDownload("nope") // removing something unknown is not an error
	s.Close()

	s, _ = Open(path) // persists across reopen
	defer s.Close()
	got, err := s.Downloads()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d downloads, want 2: %+v", len(got), got)
	}
	if a := got["a"]; a.Path != "/d/a2.m4a" || a.Bytes != 120 {
		t.Errorf("a = %+v, want the replaced row", a)
	}
	if b := got["b"]; b.Bytes != 250 || !b.LastUsed.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("b = %+v, want last_used moved forward", b)
	}
	if err := s.RemoveDownload("a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Downloads(); len(got) != 1 || got["b"].ID != "b" {
		t.Errorf("after remove: %+v", got)
	}
}
