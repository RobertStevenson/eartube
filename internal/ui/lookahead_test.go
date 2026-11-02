package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/player"
)

// playing makes the fake report a track that is well underway.
func playing(a *app, pos float64) {
	a.pl.setStatus(player.Status{Pos: pos, Duration: 600, Speed: 1})
}

func TestNoLookaheadBeforePlaybackIsUnderway(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	playFirst(a)                        // fake reports position 0: still starting up
	time.Sleep(1200 * time.Millisecond) // a couple of ticks
	if n := src.resolves.total(); n != 0 {
		t.Fatalf("resolved %d tracks while the current one was still starting", n)
	}
	a.quit()
}

func TestLookaheadResolvesOnlyTheNextTrack(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	playFirst(a)
	playing(a, 5)
	a.eventually("next track resolved", func() bool { return a.resolvedReady("b") })
	if src.resolves.count("https://yt/c") != 0 {
		t.Fatal("must only look one track ahead")
	}
	a.quit()
}

func TestLookaheadDoesNotRepeat(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	playFirst(a)
	playing(a, 5)
	a.eventually("next track resolved", func() bool { return a.resolvedReady("b") })
	time.Sleep(1200 * time.Millisecond) // several more ticks
	if n := src.resolves.count("https://yt/b"); n != 1 {
		t.Fatalf("b resolved %d times, want exactly 1", n)
	}
	a.quit()
}

func TestNextUsesResolvedStream(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	playing(a, 5)
	a.eventually("next track resolved", func() bool { return a.resolvedReady("b") })
	a.press("n")
	a.waitFor("playing: Episode B")
	last := a.pl.loaded()[1]
	if last.url != "https://yt/b" || last.stream != "direct://https://yt/b" {
		t.Fatalf("second load = %+v, want b via its pre-resolved stream", last)
	}
	a.quit()
}

func TestAutoAdvanceUsesResolvedStream(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	playing(a, 5)
	a.eventually("next track resolved", func() bool { return a.resolvedReady("b") })
	a.pl.ended <- true
	a.waitFor("playing: Episode B")
	if last := a.pl.loaded()[1]; last.stream == "" {
		t.Fatalf("auto-advance did not use the pre-resolved stream: %+v", last)
	}
	a.quit()
}

func TestResolvedStreamStillResumesSavedPosition(t *testing.T) {
	db := newDB(t)
	db.SavePos("b", 300)
	a := start(t, defaultSource(), db)
	playFirst(a)
	playing(a, 5)
	a.eventually("next track resolved", func() bool { return a.resolvedReady("b") })
	a.press("n")
	a.waitFor("playing: Episode B")
	if last := a.pl.loaded()[1]; last.stream == "" || last.start != 298 {
		t.Fatalf("second load = %+v, want the stream, resuming at 298", last)
	}
	a.quit()
}

func TestResolveFailureFallsBackToNormalLoad(t *testing.T) {
	src := defaultSource()
	src.resolveErr = errors.New("needs sign-in")
	a := start(t, src, nil)
	playFirst(a)
	playing(a, 5)
	a.eventually("lookahead attempted", func() bool { return src.resolves.count("https://yt/b") == 1 })
	a.eventually("failure recorded", func() bool {
		failed := false
		a.inspect(func(m *Model) { failed = m.resolved["b"].failed })
		return failed
	})
	time.Sleep(1200 * time.Millisecond)
	if n := src.resolves.count("https://yt/b"); n != 1 {
		t.Fatalf("a failed resolve was retried %d times", n-1)
	}
	a.press("n")
	a.waitFor("playing: Episode B")
	if last := a.pl.loaded()[1]; last.url != "https://yt/b" || last.stream != "" {
		t.Fatalf("second load = %+v, want a normal load", last)
	}
	a.quit()
}

func TestResolvedStreamIsSingleUse(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	playing(a, 5)
	a.eventually("next track resolved", func() bool { return a.resolvedReady("b") })
	a.press("n")
	a.waitFor("playing: Episode B")
	a.press("b") // fresh at 0s, so this goes back to A
	a.waitFor("playing: Episode A")
	a.press("n") // and forward to B again: its old stream may be stale
	a.eventually("third load", func() bool { return len(a.pl.loaded()) == 4 })
	if last := a.pl.loaded()[3]; last.url != "https://yt/b" || last.stream != "" {
		t.Fatalf("loads = %+v, want the 4th to be a normal (non-stream) load of b", a.pl.loaded())
	}
	a.quit()
}

func TestResolvedURLExpiry(t *testing.T) {
	t0 := time.Now()
	fresh := resolvedURL{stream: "https://s", at: t0}
	for _, c := range []struct {
		name string
		r    resolvedURL
		now  time.Time
		want bool
	}{
		{"fresh", fresh, t0.Add(time.Minute), true},
		{"just inside TTL", fresh, t0.Add(resolvedTTL - time.Second), true},
		{"expired", fresh, t0.Add(resolvedTTL + time.Second), false},
		{"pending", resolvedURL{pending: true, at: t0}, t0, false},
		{"failed", resolvedURL{failed: true, stream: "https://s", at: t0}, t0, false},
		{"empty stream", resolvedURL{at: t0}, t0, false},
	} {
		if got := c.r.usable(c.now); got != c.want {
			t.Errorf("%s: usable = %v, want %v", c.name, got, c.want)
		}
	}
}
