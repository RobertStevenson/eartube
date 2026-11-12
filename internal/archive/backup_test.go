package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
)

func TestBackupCopiesEverythingBeforeADelete(t *testing.T) {
	src := t.TempDir()
	thumb := filepath.Join(src, "a.jpg")
	os.WriteFile(thumb, []byte("jpeg bytes"), 0o644)
	vs := []store.Video{
		{ID: "a", Title: "Kept knowledge", Description: "what a was", Thumbnail: thumb, Playlists: []string{"Liked videos"}},
		{ID: "b", Title: "No picture"},
		{ID: "c", Title: "Picture already gone", Thumbnail: filepath.Join(src, "missing.jpg")},
	}
	dir := filepath.Join(t.TempDir(), "removed")
	folder, err := Backup(dir, "Liked videos / 2024!", vs, time.Date(2026, 11, 12, 9, 30, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "2026-11-12-093005-liked-videos-2024"); folder != want {
		t.Errorf("folder = %s, want %s", folder, want)
	}
	var got []map[string]any
	b, _ := os.ReadFile(filepath.Join(folder, "videos.json"))
	if err := json.Unmarshal(b, &got); err != nil || len(got) != 3 {
		t.Fatalf("videos.json: %d videos, %v\n%s", len(got), err, b)
	}
	copied := filepath.Join(folder, "thumbnails", "a.jpg")
	if got[0]["thumbnail_file"] != copied {
		t.Errorf("thumbnail_file = %v, want the copy %s", got[0]["thumbnail_file"], copied)
	}
	if _, ok := got[2]["thumbnail_file"]; ok {
		t.Errorf("a missing thumbnail should not be listed: %v", got[2])
	}
	if c, _ := os.ReadFile(copied); string(c) != "jpeg bytes" {
		t.Errorf("thumbnail copy holds %q", c)
	}
	if md, _ := os.ReadFile(filepath.Join(folder, "videos.md")); !strings.Contains(string(md), "what a was") {
		t.Errorf("videos.md lacks the description:\n%s", md)
	}
	if _, err := os.Stat(thumb); err != nil {
		t.Errorf("Backup must not remove the original: %v", err)
	}
	if err := RemoveThumbnails(vs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Errorf("original thumbnail still there: %v", err)
	}
}

func TestBackupFailsWhenItCannotWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	os.WriteFile(file, nil, 0o644)
	if _, err := Backup(file, "P", []store.Video{{ID: "a"}}, time.Now()); err == nil {
		t.Error("expected an error, so the caller does not delete anything")
	}
}
