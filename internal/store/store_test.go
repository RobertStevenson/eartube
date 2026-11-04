package store

import (
	"database/sql"
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

func TestDownloadMetadataRoundTrips(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Unix(1_700_000_000, 0)
	s.AddDownload(Download{ID: "a", Path: "/d/a.webm", Bytes: 5, LastUsed: at,
		Title: "A talk about \"quotes\" and ünïcode", Channel: "Some Channel", Duration: 3725.5})
	got, _ := s.Downloads()
	a := got["a"]
	if a.Title != "A talk about \"quotes\" and ünïcode" || a.Channel != "Some Channel" || a.Duration != 3725.5 {
		t.Fatalf("metadata lost: %+v", a)
	}
}

func TestSetDownloadMetaBackfills(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.AddDownload(Download{ID: "a", Path: "/d/a", Bytes: 5, LastUsed: time.Unix(1, 0)}) // no metadata yet
	if err := s.SetDownloadMeta("a", "Now known", "Chan", 60); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDownloadMeta("nope", "x", "y", 1); err != nil { // unknown id: no error, no row
		t.Fatal(err)
	}
	got, _ := s.Downloads()
	if len(got) != 1 || got["a"].Title != "Now known" || got["a"].Duration != 60 {
		t.Fatalf("got %+v", got)
	}
}

// A database written by v0.4.0 has a downloads table without the metadata
// columns. Opening it must upgrade it in place and keep the existing rows.
func TestOpenUpgradesAV040Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`
		CREATE TABLE downloads (id TEXT PRIMARY KEY, path TEXT NOT NULL, bytes INTEGER NOT NULL, last_used INTEGER NOT NULL);
		INSERT INTO downloads VALUES ('old1', '/d/old1.webm', 1234, 1700000000);`)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("could not open a v0.4.0 database: %v", err)
	}
	got, err := s.Downloads()
	if err != nil {
		t.Fatal(err)
	}
	if d := got["old1"]; d.Bytes != 1234 || d.Path != "/d/old1.webm" || d.Title != "" {
		t.Fatalf("old row not preserved: %+v", d)
	}
	if err := s.AddDownload(Download{ID: "new", Path: "/d/new", Bytes: 1, LastUsed: time.Unix(2, 0), Title: "T"}); err != nil {
		t.Fatalf("new columns unusable after upgrade: %v", err)
	}
	s.Close()

	s, err = Open(path) // opening an already-upgraded database must be a no-op
	if err != nil {
		t.Fatalf("second open failed: %v", err)
	}
	defer s.Close()
	if got, _ := s.Downloads(); len(got) != 2 || got["new"].Title != "T" {
		t.Fatalf("after reopen: %+v", got)
	}
}
