package download

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"eartube-progress  42.3%", 42.3, true},
		{"eartube-progress 100.0%", 100, true},
		{"eartube-progress 0.0%", 0, true},
		{"eartube-progress 7%", 7, true},
		{"eartube-progress NA", 0, false},
		{"42.3%", 0, false}, // a bare percentage is not ours
		{"/home/u/x/abc.m4a", 0, false},
		{"", 0, false},
		{"[download] 5%", 0, false},
	}
	for _, c := range cases {
		if got, ok := parseProgress(c.in); got != c.want || ok != c.ok {
			t.Errorf("parseProgress(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFetchRejectsUnsafeIDs(t *testing.T) {
	f := Fetcher{Dir: t.TempDir()}
	for _, id := range []string{"", "../escape", "a/b", "a b", "x;rm -rf", "..", strings.Repeat("a", 65)} {
		if _, err := f.Fetch(context.Background(), id, "https://example.invalid", nil); err == nil {
			t.Errorf("id %q was accepted", id)
		}
	}
}

func TestArgs(t *testing.T) {
	f := Fetcher{Dir: "/data/dl"}
	plain := f.args("abc123", "https://y/v", "")
	for _, want := range []string{"--no-playlist", "-f", "bestaudio/best", "/data/dl/abc123.%(ext)s", "https://y/v"} {
		if !slices.Contains(plain, want) {
			t.Errorf("args missing %q: %v", want, plain)
		}
	}
	if slices.Contains(plain, "--cookies-from-browser") {
		t.Error("the first attempt must not use cookies")
	}
	cookies := f.args("abc123", "https://y/v", "brave+gnomekeyring")
	i := slices.Index(cookies, "--cookies-from-browser")
	if i < 0 || cookies[i+1] != "brave+gnomekeyring" {
		t.Errorf("cookie attempt args wrong: %v", cookies)
	}
	if cookies[len(cookies)-1] != "https://y/v" {
		t.Error("the URL must come last")
	}
}

func TestFinish(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("abc.m4a", 1234)

	t.Run("uses the printed path", func(t *testing.T) {
		got, err := finish(dir, "abc", good)
		if err != nil || got.Path != good || got.Bytes != 1234 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("falls back to the directory when nothing was printed", func(t *testing.T) {
		got, err := finish(dir, "abc", "")
		if err != nil || got.Path != good {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("ignores a printed path outside the download directory", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "abc.m4a")
		os.WriteFile(outside, []byte("x"), 0o644)
		got, err := finish(dir, "abc", outside)
		if err != nil || got.Path != good {
			t.Fatalf("got %+v, %v; must not trust a path outside the dir", got, err)
		}
	})
	t.Run("never mistakes a partial file for a finished one", func(t *testing.T) {
		d2 := t.TempDir()
		os.WriteFile(filepath.Join(d2, "xyz.m4a.part"), []byte("partial"), 0o644)
		os.WriteFile(filepath.Join(d2, "xyz.ytdl"), []byte("state"), 0o644)
		if got, err := finish(d2, "xyz", filepath.Join(d2, "xyz.m4a.part")); err == nil {
			t.Fatalf("accepted a partial download: %+v", got)
		}
	})
	t.Run("an empty file is not a download", func(t *testing.T) {
		d3 := t.TempDir()
		os.WriteFile(filepath.Join(d3, "e.m4a"), nil, 0o644)
		if _, err := finish(d3, "e", filepath.Join(d3, "e.m4a")); err == nil {
			t.Fatal("accepted an empty file")
		}
	})
	t.Run("a different video's file is not matched", func(t *testing.T) {
		if _, err := finish(dir, "other", ""); err == nil {
			t.Fatal("matched a file belonging to another id")
		}
	})
}
