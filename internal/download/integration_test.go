//go:build integration

package download

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Real yt-dlp against YouTube; skipped by -short.
//
//	go test -tags integration ./internal/download

func TestFetchRealVideo(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var last float64
	res, err := Fetcher{Dir: dir}.Fetch(ctx, "jNQXAC9IVRw", "https://www.youtube.com/watch?v=jNQXAC9IVRw",
		func(p float64) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(res.Path) != dir || filepath.Base(res.Path)[:11] != "jNQXAC9IVRw" {
		t.Errorf("path = %q, want a file named after the id inside %q", res.Path, dir)
	}
	if res.Bytes < 10_000 {
		t.Errorf("only %d bytes for a 19s video", res.Bytes)
	}
	if st, err := os.Stat(res.Path); err != nil || st.Size() != res.Bytes {
		t.Errorf("reported %d bytes, stat says %v (%v)", res.Bytes, st, err)
	}
	if last != 100 {
		t.Errorf("last progress = %v, want 100", last)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(left) != 0 {
		t.Errorf("left partial files behind: %v", left)
	}
	t.Logf("downloaded %s (%d bytes)", filepath.Base(res.Path), res.Bytes)
}

func TestFetchFailureLeavesNothingBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := (Fetcher{Dir: dir}).Fetch(ctx, "aaaaaaaaaaa", "https://www.youtube.com/watch?v=aaaaaaaaaaa", nil); err == nil {
		t.Fatal("expected an error for a video that does not exist")
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Errorf("failed download left files behind: %v", files)
	}
}

func TestFetchCanBeCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	began := time.Now()
	_, err := Fetcher{Dir: t.TempDir()}.Fetch(ctx, "jNQXAC9IVRw", "https://www.youtube.com/watch?v=jNQXAC9IVRw", nil)
	if err == nil {
		t.Fatal("a cancelled download must not succeed")
	}
	if time.Since(began) > 5*time.Second {
		t.Errorf("cancel took %v to take effect", time.Since(began))
	}
}
