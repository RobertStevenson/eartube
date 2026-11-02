package ui

import (
	"testing"

	"github.com/RobertStevenson/eartube/internal/player"
)

// playFirst opens the playlist and plays Episode A (queueing B and C).
func playFirst(a *app) {
	a.openPlaylist()
	a.press("enter")
	a.waitFor("playing: Episode A")
}

func TestQuitSavesPosition(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.pl.setStatus(player.Status{Pos: 120, Duration: 600, Speed: 1})
	a.quit()
	if got := a.track("a"); got.Pos != 120 || got.Played {
		t.Fatalf("track a = %+v, want pos 120 unplayed", got)
	}
}

func TestPlayingResumesSavedPosition(t *testing.T) {
	db := newDB(t)
	db.SavePos("a", 120)
	a := start(t, defaultSource(), db)
	playFirst(a)
	if ld := a.pl.loaded(); len(ld) != 1 || ld[0].start != 118 {
		t.Fatalf("loads = %+v, want start 118 (saved 120 minus 2s)", ld)
	}
	a.quit()
}

func TestPlayedTrackRestartsFromBeginning(t *testing.T) {
	db := newDB(t)
	db.SetPlayed("a", true)
	db.SavePos("a", 120) // stale position must be ignored for played tracks
	a := start(t, defaultSource(), db)
	playFirst(a)
	if ld := a.pl.loaded(); len(ld) != 1 || ld[0].start != 0 {
		t.Fatalf("loads = %+v, want start 0", ld)
	}
	a.quit()
}

func TestNearEndOnQuitMarksPlayed(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.pl.setStatus(player.Status{Pos: 595, Duration: 600, Speed: 1})
	a.quit()
	if got := a.track("a"); !got.Played {
		t.Fatalf("track a = %+v, want played", got)
	}
}

func TestAutoAdvanceMarksPlayed(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.pl.ended <- true
	a.eventually("advanced to B", func() bool {
		l := a.pl.loaded()
		return len(l) == 2 && l[1].url == "https://yt/b"
	})
	a.waitFor("playing: Episode B")
	a.waitFor("✓ Episode A") // the visible list reflects the new played state
	if !a.track("a").Played {
		t.Fatal("a should be persisted as played")
	}
	a.quit()
}

func TestPlaybackErrorAdvancesWithoutMarkingPlayed(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.pl.ended <- false
	a.eventually("advanced to B", func() bool { return len(a.pl.loaded()) == 2 })
	a.quit()
	if a.track("a").Played {
		t.Fatal("a failed to play and must not be marked played")
	}
}

func TestEndOfQueueStops(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.openPlaylist()
	a.press("down", "down", "enter") // only C
	a.waitFor("playing: Episode C")
	a.pl.ended <- true
	a.waitFor("end of queue")
	if n := len(a.pl.loaded()); n != 1 {
		t.Fatalf("loads = %d, want 1", n)
	}
	a.quit()
}

func TestMarkPlayedToggle(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.openPlaylist()
	a.press("m")
	a.waitFor("✓ Episode A")
	if !a.track("a").Played {
		t.Fatal("not persisted")
	}
	a.press("m")
	a.waitGone("✓ Episode A")
	if a.track("a").Played {
		t.Fatal("still persisted as played")
	}
	a.quit()
}

func TestSkipNext(t *testing.T) {
	a := start(t, defaultSource(), nil)
	playFirst(a)
	a.pl.setStatus(player.Status{Pos: 90, Duration: 600, Speed: 1})
	a.press("n")
	a.waitFor("playing: Episode B")
	if got := a.track("a"); got.Pos != 90 {
		t.Fatalf("skipping should save a's position, got %+v", got)
	}
	a.quit()
}

func TestQueueViewAndRemove(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.openPlaylist()
	a.press("a", "down", "a") // queue A (starts playing) and B
	a.waitFor("queued (2 in queue)")
	a.press("tab")
	a.waitFor("Queue")
	a.press("down", "x") // remove B
	a.waitGone("Episode B")
	a.press("tab") // back to the playlist
	a.waitFor("Episode B")
	a.quit()
}

func TestQueueRestoredAfterRestart(t *testing.T) {
	db := newDB(t)
	a := start(t, defaultSource(), db)
	playFirst(a)
	a.pl.setStatus(player.Status{Pos: 50, Duration: 600, Speed: 1})
	a.quit()

	b := start(t, defaultSource(), db) // same database, fresh program
	b.waitFor("Podcasts")              // startup load finished...
	b.waitFor("queue restored")        // ...and must not have hidden the hint
	b.press(" ")                       // idle player + restored queue => resume it
	b.waitFor("playing: Episode A")
	if ld := b.pl.loaded(); len(ld) != 1 || ld[0].url != "https://yt/a" || ld[0].start != 48 {
		t.Fatalf("loads = %+v, want episode A at 48", ld)
	}
	b.quit()
}
