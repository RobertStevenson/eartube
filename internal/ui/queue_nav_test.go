package ui

import (
	"testing"

	"github.com/RobertStevenson/eartube/internal/player"
)

// savedQueueOf reads the persisted queue (quit first so it has been written).
func savedQueueOf(t *testing.T, a *app) ([]string, int) {
	t.Helper()
	var sq savedQueue
	if ok, err := a.db.LoadJSON("queue", &sq); !ok || err != nil {
		t.Fatalf("no saved queue: ok=%v err=%v", ok, err)
	}
	ids := make([]string, len(sq.Items))
	for i, it := range sq.Items {
		ids[i] = it.ID
	}
	return ids, sq.Pos
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPreviousGoesBackWhenNearStart(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.press("n")
	a.waitFor("playing: Episode B")
	a.press("b") // B has barely started, so go back to A
	a.waitFor("playing: Episode A")

	urls := []string{}
	for _, l := range a.pl.loaded() {
		urls = append(urls, l.url)
	}
	if !equal(urls, []string{"https://yt/a", "https://yt/b", "https://yt/a"}) {
		t.Fatalf("loads = %v", urls)
	}
	a.quit()
}

func TestPreviousRestartsWhenPastThreshold(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.press("n")
	a.waitFor("playing: Episode B")
	a.pl.setStatus(player.Status{Pos: 30, Duration: 600, Speed: 1})
	a.press("b") // 30s in: restart B, don't leave it
	a.waitFor("restarted: Episode B")

	ld := a.pl.loaded()
	if len(ld) != 3 || ld[2].url != "https://yt/b" || ld[2].start != 0 {
		t.Fatalf("loads = %+v, want B restarted from 0", ld)
	}
	a.quit()
	if got := a.track("b"); got.Pos != 0 {
		t.Fatalf("b position = %v, want the saved position cleared", got.Pos)
	}
}

func TestPreviousAtQueueHeadRestarts(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.press("b")
	a.waitFor("restarted: Episode A")
	if ld := a.pl.loaded(); len(ld) != 2 || ld[1].url != "https://yt/a" {
		t.Fatalf("loads = %+v", ld)
	}
	a.quit()
}

func TestPreviousWithNothingPlaying(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("b")
	a.waitFor("nothing to go back to")
	if len(a.pl.loaded()) != 0 {
		t.Fatal("nothing should have loaded")
	}
	a.quit()
}

func TestPreviousResumesEarlierPartialTrack(t *testing.T) {
	db := newDB(t)
	a := start(t, defaultSource(), db)
	playFirst(a)
	a.pl.setStatus(player.Status{Pos: 200, Duration: 600, Speed: 1})
	a.press("n") // saves A at 200
	a.waitFor("playing: Episode B")
	a.press("b")
	a.waitFor("playing: Episode A")
	if ld := a.pl.loaded(); ld[2].url != "https://yt/a" || ld[2].start != 198 {
		t.Fatalf("loads = %+v, want A resumed at 198", ld)
	}
	a.quit()
}

// openQueue plays A (queueing B, C) and switches to the queue view.
func openQueue(a *app) {
	playFirst(a)
	a.press("tab")
	a.waitFor("Queue")
}

func TestReorderCurrentItemKeepsPlaying(t *testing.T) {
	a := start(t, defaultSource(), nil)
	openQueue(a)
	a.press("J") // move A (current) down: B, A, C
	a.press("J") // and again:            B, C, A
	a.quit()
	ids, pos := savedQueueOf(t, a)
	if !equal(ids, []string{"b", "c", "a"}) || pos != 2 {
		t.Fatalf("queue = %v pos = %d, want [b c a] pos 2 (still pointing at A)", ids, pos)
	}
	if n := len(a.pl.loaded()); n != 1 {
		t.Fatalf("reordering must not restart playback; loads = %d", n)
	}
}

func TestReorderOtherItemAdjustsCurrentMarker(t *testing.T) {
	a := start(t, defaultSource(), nil)
	openQueue(a)
	a.press("down", "down") // cursor on C
	a.press("K")            // A, C, B
	a.press("K")            // swaps with current A: C, A, B
	a.quit()
	ids, pos := savedQueueOf(t, a)
	if !equal(ids, []string{"c", "a", "b"}) || pos != 1 {
		t.Fatalf("queue = %v pos = %d, want [c a b] pos 1 (A still current)", ids, pos)
	}
}

func TestReorderChangesWhatPlaysNext(t *testing.T) {
	a := start(t, defaultSource(), nil)
	openQueue(a)
	a.press("down", "down", "K") // A, C, B
	a.press("n")
	a.waitFor("playing: Episode C")
	if ld := a.pl.loaded(); ld[len(ld)-1].url != "https://yt/c" {
		t.Fatalf("loads = %+v, want C next", ld)
	}
	a.quit()
}

func TestReorderStopsAtEnds(t *testing.T) {
	a := start(t, defaultSource(), nil)
	openQueue(a)
	a.press("K")                      // first item can't go up
	a.press("down", "down", "J", "J") // last item can't go down
	a.quit()
	ids, pos := savedQueueOf(t, a)
	if !equal(ids, []string{"a", "b", "c"}) || pos != 0 {
		t.Fatalf("queue = %v pos = %d, want unchanged", ids, pos)
	}
}

func TestReorderIgnoredOutsideQueueView(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.press("J", "K") // browsing the playlist: must not touch the queue
	a.quit()
	ids, _ := savedQueueOf(t, a)
	if !equal(ids, []string{"a", "b", "c"}) {
		t.Fatalf("queue = %v", ids)
	}
}
