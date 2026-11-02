package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

func TestSelectedVideoIsResolvedAfterTheCursorRests(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.openPlaylist() // cursor lands on Episode A
	a.eventually("A resolved in the background", func() bool { return a.resolvedReady("a") })

	a.press("enter") // by now the stream is ready, so the first track starts from it
	a.waitFor("playing: Episode A")
	first := a.pl.loaded()[0]
	if first.url != "https://yt/a" || first.stream != "direct://https://yt/a" {
		t.Fatalf("first load = %+v, want a via its pre-resolved stream", first)
	}
	a.quit()
}

func TestScrollingPastVideosDoesNotResolveThem(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.openPlaylist()
	a.press("down", "down") // A -> B -> C well inside the rest delay
	a.eventually("C resolved once the cursor rests", func() bool { return a.resolvedReady("c") })
	time.Sleep(hoverDelay + 200*time.Millisecond)
	for _, id := range []string{"a", "b"} {
		if n := src.resolves.count("https://yt/" + id); n != 0 {
			t.Errorf("%s was resolved %d time(s) although the cursor only passed over it", id, n)
		}
	}
	a.quit()
}

func TestPlaylistsAreNotResolved(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.waitFor("Podcasts") // the cursor rests on a playlist, not a video
	time.Sleep(hoverDelay + 300*time.Millisecond)
	if n := src.resolves.total(); n != 0 {
		t.Fatalf("resolved %d item(s); playlists must be skipped", n)
	}
	a.quit()
}

func TestSearchResultsAreResolvedWhenTheyAppear(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.waitFor("Podcasts")
	a.press("/")
	a.typeText("golang")
	a.press("enter")
	a.waitFor("Search One")
	a.eventually("first result resolved", func() bool { return a.resolvedReady("s1") })
	if n := src.resolves.count("https://yt/s2"); n != 0 {
		t.Fatalf("the second result was resolved without the cursor reaching it (%d)", n)
	}
	a.quit()
}

func TestHoverResolveIsNotRepeated(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.openPlaylist()
	a.eventually("A resolved", func() bool { return a.resolvedReady("a") })
	a.press("down", "up") // leave and come back
	time.Sleep(hoverDelay + 300*time.Millisecond)
	if n := src.resolves.count("https://yt/a"); n != 1 {
		t.Fatalf("A resolved %d times, want 1 (cached)", n)
	}
	a.quit()
}

func TestRestoredQueueIsResolvedAtLaunch(t *testing.T) {
	db := newDB(t)
	a := start(t, defaultSource(), db)
	playFirst(a)
	a.pl.setStatus(playingStatus(50))
	a.quit()

	src := defaultSource()
	b := start(t, src, db) // a fresh launch with the saved queue
	b.waitFor("queue restored")
	b.eventually("current track resolved before the user asks", func() bool { return b.resolvedReady("a") })
	b.press(" ")
	b.waitFor("playing: Episode A")
	ld := b.pl.loaded()
	if len(ld) != 1 || ld[0].stream != "direct://https://yt/a" || ld[0].start != 48 {
		t.Fatalf("loads = %+v, want the pre-resolved stream resuming at 48", ld)
	}
	b.quit()
}

func TestHoverFailureFallsBackToANormalLoad(t *testing.T) {
	src := defaultSource()
	src.resolveErr = errNeedsSignIn
	a := start(t, src, nil)
	a.openPlaylist()
	a.eventually("failure recorded", func() bool {
		failed := false
		a.inspect(func(m *Model) { failed = m.resolved["a"].failed })
		return failed
	})
	a.press("enter")
	a.waitFor("playing: Episode A")
	if first := a.pl.loaded()[0]; first.url != "https://yt/a" || first.stream != "" {
		t.Fatalf("first load = %+v, want a normal load", first)
	}
	a.quit()
}

// Scrolling steadily through a long list must resolve only where the cursor
// finally stops. Each row's rest timer fires while the cursor is further down,
// and those stale timers must be ignored.
func TestSteadyScrollingResolvesOnlyWhereItStops(t *testing.T) {
	src := defaultSource()
	long := []ytdlp.Item{vid("a", "Episode A")} // openPlaylist waits for this title
	for i := 1; i <= 8; i++ {
		long = append(long, vid(fmt.Sprintf("v%d", i), fmt.Sprintf("Video %d", i)))
	}
	src.byURL["https://yt/pl1"] = long
	a := start(t, src, nil)
	a.openPlaylist()
	for i := 0; i < 8; i++ { // 8 steps, 120ms apart: never rests for hoverDelay
		a.press("down")
		time.Sleep(120 * time.Millisecond)
	}
	a.eventually("last row resolved once the cursor stops", func() bool { return a.resolvedReady("v8") })
	time.Sleep(hoverDelay + 200*time.Millisecond)
	passed := []string{"a"}
	for i := 1; i < 8; i++ {
		passed = append(passed, fmt.Sprintf("v%d", i))
	}
	for _, id := range passed {
		if n := src.resolves.count("https://yt/" + id); n != 0 {
			t.Errorf("%s was resolved (%d) although the cursor never rested on it", id, n)
		}
	}
	a.quit()
}
