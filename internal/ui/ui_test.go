package ui

import (
	"testing"
)

func TestPlaylistsShownOnStart(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Your playlists")
	a.waitFor("Podcasts")
	a.quit()
}

func TestEnterPlaysAndQueuesRestOfList(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.openPlaylist()
	a.press("down", "enter") // Episode B
	a.waitFor("+1 queued")   // B playing, C still queued

	ld := a.pl.loaded()
	if len(ld) != 1 || ld[0].url != "https://yt/b" {
		t.Fatalf("loads = %+v, want only episode B", ld)
	}
	a.quit()
}

func TestSearchPlaysResult(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("/")
	a.typeText("golang")
	a.press("enter")
	a.waitFor("Search One")
	a.press("enter")
	a.eventually("search result loaded", func() bool {
		l := a.pl.loaded()
		return len(l) == 1 && l[0].url == "https://yt/s1"
	})
	a.quit()
}

func TestSearchEscapeCancels(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("/")
	a.typeText("nope")
	a.press("esc")
	a.press("p") // a normal key works again, so input is no longer focused
	a.waitFor("Your playlists")
	a.quit()
	if len(a.pl.loaded()) != 0 {
		t.Fatal("nothing should have played")
	}
}

func TestSourceErrorIsShown(t *testing.T) {
	src := defaultSource()
	src.err = errBoom
	a := start(t, src, nil)
	a.waitFor("boom")
	a.quit()
}

func TestPlayerControls(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press(" ", "left", "right", "[", "]")
	a.eventually("controls forwarded", func() bool {
		a.pl.mu.Lock()
		defer a.pl.mu.Unlock()
		return a.pl.pauses == 1 && len(a.pl.seeks) == 2 && len(a.pl.speeds) == 2
	})
	a.pl.mu.Lock()
	seeks, speeds := append([]float64(nil), a.pl.seeks...), append([]float64(nil), a.pl.speeds...)
	a.pl.mu.Unlock() // never hold the fake's lock while the UI is running: View() reads it
	if seeks[0] != -10 || seeks[1] != 10 || speeds[0] != -0.25 || speeds[1] != 0.25 {
		t.Fatalf("seeks=%v speeds=%v", seeks, speeds)
	}
	a.quit()
}
