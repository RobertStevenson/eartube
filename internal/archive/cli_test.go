package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
)

func testDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seeded(t *testing.T) *store.Store {
	db := testDB(t)
	t0 := time.Unix(1_700_000_000, 0)
	db.RecordPlaylist("PL1", "Podcasts", []store.Sighting{
		{ID: "a", Title: "Kept", Channel: "C"},
		{ID: "b", Title: "Will go", Channel: "C"},
	}, t0)
	db.SaveDetails(store.Video{ID: "b", Description: "what b was"}, t0)
	db.RecordPlaylist("PL1", "Podcasts", []store.Sighting{{ID: "b", Gone: true}, {ID: "z", Gone: true}}, t0.Add(time.Hour))
	return db
}

func run(t *testing.T, db *store.Store, args ...string) (stdout, stderr string, err error) {
	var o, e bytes.Buffer
	err = Run(args, db, &o, &e)
	return o.String(), e.String(), err
}

func TestSummary(t *testing.T) {
	out, _, err := run(t, seeded(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"3 videos saved from your playlists, 1 with full details.",
		"2 are no longer on YouTube; eartube had saved 1 of those before they went.",
		"eartube archive export",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = run(t, testDB(t))
	if !strings.Contains(out, "No videos saved yet") {
		t.Errorf("empty summary = %q", out)
	}
}

func TestExportToStdoutAndFile(t *testing.T) {
	db := seeded(t)
	out, _, err := run(t, db, "export", "--unavailable")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 {
		t.Fatalf("got %d videos, err %v:\n%s", len(got), err, out)
	}

	path := filepath.Join(t.TempDir(), "archive.md")
	if err := os.WriteFile(path, []byte("old contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := run(t, db, "export", "--format", "markdown", "--output", path)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" || !strings.Contains(errOut, "wrote "+path) {
		t.Errorf("stdout=%q stderr=%q", out, errOut)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "## Will go") || !strings.Contains(string(b), "> what b was") {
		t.Errorf("file holds:\n%s", b)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".eartube-export-*")); len(left) > 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestBadArgumentsAreErrorsAndLeaveFilesAlone(t *testing.T) {
	db := seeded(t)
	path := filepath.Join(t.TempDir(), "keep.json")
	os.WriteFile(path, []byte("precious"), 0o644)
	for _, args := range [][]string{
		{"exprot"},
		{"export", "--format", "xml", "--output", path},
		{"export", "extra"},
		{"export", "--nope"},
	} {
		if _, _, err := run(t, db, args...); err == nil {
			t.Errorf("%q: expected an error", args)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "precious" {
		t.Errorf("a failed export overwrote the file with %q", b)
	}
	if _, errOut, err := run(t, db, "-h"); !errors.Is(err, flag.ErrHelp) || !strings.Contains(errOut, "-unavailable") {
		t.Errorf("help: err=%v stderr=%q", err, errOut)
	}
}
