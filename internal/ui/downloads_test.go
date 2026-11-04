package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
)

func dlOptions(f *fakeDownloader) Options { return Options{Downloads: true, Downloader: f} }

// downloads reads what the database says is downloaded.
func (a *app) downloads() map[string]store.Download {
	a.t.Helper()
	d, err := a.db.Downloads()
	if err != nil {
		a.t.Fatal(err)
	}
	return d
}

func (a *app) waitDownloaded(id string) {
	a.t.Helper()
	a.eventually("download of "+id+" recorded", func() bool { _, ok := a.downloads()[id]; return ok })
}

func TestDownloadsAreOffByDefault(t *testing.T) {
	a := start(t, defaultSource(), nil) // zero Options
	a.openPlaylist()
	a.press("d")
	a.waitFor("downloads are off")
	a.quit()
}

func TestDownloadSelectedVideo(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitFor("downloaded: Episode A")
	a.waitFor("↓ downloaded")
	dl := a.downloads()["a"]
	if dl.Bytes != 100 || !fileExists(dl.Path) {
		t.Fatalf("download = %+v, want a 100-byte file on disk", dl)
	}
	if got := f.callList(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("fetched %v, want just a", got)
	}
	a.quit()
}

func TestOnlyTheSelectedVideoIsDownloaded(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("down", "d") // Episode B
	a.waitDownloaded("b")
	if got := f.callList(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("fetched %v, want just b", got)
	}
	a.quit()
}

func TestPlaylistsCannotBeDownloadedDirectly(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.waitFor("Podcasts")
	a.press("d")
	a.waitFor("open the playlist first")
	if len(f.callList()) != 0 {
		t.Fatal("nothing should have been fetched")
	}
	a.quit()
}

func TestAlreadyDownloadedIsSkipped(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	a.press("d")
	a.waitFor("already downloaded")
	if n := len(f.callList()); n != 1 {
		t.Fatalf("fetched %d times, want 1", n)
	}
	a.quit()
}

func TestDownloadAllRunsOneAtATime(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("D")
	a.waitFor("downloaded 3 videos")
	got := f.callList()
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("fetched %v, want a b c in order", got)
	}
	if f.peak != 1 {
		t.Fatalf("up to %d downloads ran at once, want strictly one at a time", f.peak)
	}
	a.quit()
}

func TestDownloadProgressIsShown(t *testing.T) {
	f := newFakeDownloader(t)
	f.hold = make(chan struct{})
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitFor("⇩ 40% Episode A")
	close(f.hold)
	a.waitFor("downloaded: Episode A")
	a.waitGone("⇩")
	a.quit()
}

func TestFailedDownloadIsReported(t *testing.T) {
	f := newFakeDownloader(t)
	f.err["a"] = errors.New("HTTP 403")
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitFor("download failed: Episode A")
	a.waitFor("HTTP 403")
	if _, ok := a.downloads()["a"]; ok {
		t.Fatal("a failed download must not be recorded")
	}
	a.quit()
}

func TestOneFailureDoesNotStopTheBatch(t *testing.T) {
	f := newFakeDownloader(t)
	f.err["a"] = errors.New("boom")
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("D")
	a.waitDownloaded("b")
	a.waitDownloaded("c")
	if _, ok := a.downloads()["a"]; ok {
		t.Fatal("the failed one must not be recorded")
	}
	a.quit()
}

// --- playback ---------------------------------------------------------------

func TestDownloadedTrackPlaysFromTheFile(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	a.press("enter")
	a.waitFor("playing: Episode A")
	first := a.pl.loaded()[0]
	if first.file != a.downloads()["a"].Path || first.stream != "" {
		t.Fatalf("first load = %+v, want the downloaded file", first)
	}
	a.quit()
}

func TestDownloadedTrackIsNotResolved(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("down", "d")
	a.waitDownloaded("b")
	var cmdWasNil bool
	a.inspect(func(m *Model) { cmdWasNil = m.resolveCmd(vid("b", "Episode B")) == nil })
	if !cmdWasNil {
		t.Fatal("a downloaded track must not be looked up on the network")
	}
	a.quit()
}

func TestDownloadedTrackStillResumes(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	f.addExisting(t, db, "a", 100, time.Now())
	db.SavePos("a", 120)
	a := startWith(t, defaultSource(), db, dlOptions(f))
	playFirst(a)
	if first := a.pl.loaded()[0]; first.file == "" || first.start != 118 {
		t.Fatalf("first load = %+v, want the file, resuming at 118", first)
	}
	a.quit()
}

func TestPlayingADownloadMarksItRecentlyUsed(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	old := time.Now().Add(-48 * time.Hour)
	f.addExisting(t, db, "a", 100, old)
	a := startWith(t, defaultSource(), db, dlOptions(f))
	playFirst(a)
	a.eventually("last-used moved forward", func() bool { return a.downloads()["a"].LastUsed.After(old.Add(time.Hour)) })
	a.quit()
}

func TestDownloadsSurviveARestart(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), db, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	a.quit()

	b := startWith(t, defaultSource(), db, dlOptions(f))
	b.openPlaylist()
	b.waitFor("↓ downloaded")
	b.press("enter")
	b.waitFor("playing: Episode A")
	if first := b.pl.loaded()[0]; first.file == "" {
		t.Fatalf("first load = %+v, want the file from the previous run", first)
	}
	b.quit()
}

func TestDownloadWorksEvenWhenFeatureIsOffForExistingFiles(t *testing.T) {
	// Turning downloads off must not strand files you already have.
	db := newDB(t)
	f := newFakeDownloader(t)
	f.addExisting(t, db, "a", 100, time.Now())
	a := start(t, defaultSource(), db) // zero Options: downloads off
	playFirst(a)
	if first := a.pl.loaded()[0]; first.file == "" {
		t.Fatalf("first load = %+v, want the existing file to still play", first)
	}
	a.quit()
}

func TestVanishedFileFallsBackAndIsForgotten(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	path := f.addExisting(t, db, "a", 100, time.Now())
	a := startWith(t, defaultSource(), db, dlOptions(f))
	a.openPlaylist()
	a.waitFor("↓ downloaded")
	if err := os.Remove(path); err != nil { // deleted behind the app's back
		t.Fatal(err)
	}
	a.press("enter")
	a.waitFor("playing: Episode A")
	if first := a.pl.loaded()[0]; first.file != "" {
		t.Fatalf("first load = %+v, must not try a missing file", first)
	}
	a.eventually("row forgotten", func() bool { _, ok := a.downloads()["a"]; return !ok })
	a.quit()
}

func TestStartupForgetsFilesThatNoLongerExist(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	path := f.addExisting(t, db, "a", 100, time.Now())
	os.Remove(path)
	a := startWith(t, defaultSource(), db, dlOptions(f))
	a.openPlaylist()
	if _, ok := a.downloads()["a"]; ok {
		t.Fatal("a row for a missing file survived startup")
	}
	a.quit()
}

// --- removing ----------------------------------------------------------------

func TestRemoveDownloadKey(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	path := a.downloads()["a"].Path
	a.press("X")
	a.waitFor("removed download: Episode A")
	a.waitGone("↓ downloaded")
	if fileExists(path) {
		t.Fatal("the file is still on disk")
	}
	if _, ok := a.downloads()["a"]; ok {
		t.Fatal("the row is still in the database")
	}
	a.quit()
}

func TestRemoveDownloadForSomethingNotDownloaded(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("X")
	a.waitFor("not downloaded")
	a.quit()
}

func TestDeletePlayedRemovesTheDownload(t *testing.T) {
	f := newFakeDownloader(t)
	o := dlOptions(f)
	o.DeletePlayed = true
	a := startWith(t, defaultSource(), nil, o)
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	path := a.downloads()["a"].Path
	a.press("enter")
	a.waitFor("playing: Episode A")
	a.pl.ended <- true // plays to the end
	a.eventually("download deleted after playing", func() bool { return !fileExists(path) })
	a.eventually("row forgotten", func() bool { _, ok := a.downloads()["a"]; return !ok })
	a.quit()
}

func TestDownloadIsKeptAfterPlayingWhenDeletePlayedIsOff(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f)) // DeletePlayed false
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	path := a.downloads()["a"].Path
	a.press("enter")
	a.waitFor("playing: Episode A")
	a.pl.ended <- true
	a.waitFor("playing: Episode B")
	if !fileExists(path) {
		t.Fatal("the download was deleted although delete_played is off")
	}
	a.quit()
}

func TestMarkingPlayedAlsoDeletesWhenEnabled(t *testing.T) {
	f := newFakeDownloader(t)
	o := dlOptions(f)
	o.DeletePlayed = true
	a := startWith(t, defaultSource(), nil, o)
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	path := a.downloads()["a"].Path
	a.press("m")
	a.eventually("download deleted", func() bool { return !fileExists(path) })
	a.quit()
}

func TestUnmarkingPlayedDoesNotDeleteAnything(t *testing.T) {
	f := newFakeDownloader(t)
	o := dlOptions(f)
	o.DeletePlayed = true
	a := startWith(t, defaultSource(), nil, o)
	a.openPlaylist()
	a.press("m") // played (nothing downloaded yet)
	a.waitFor("✓ Episode A")
	a.press("d")
	a.waitDownloaded("a")
	path := a.downloads()["a"].Path
	a.press("m") // back to unplayed
	a.waitGone("✓ Episode A")
	if !fileExists(path) {
		t.Fatal("marking unplayed must never delete a download")
	}
	a.quit()
}

// --- the size cap -------------------------------------------------------------

func TestCapEvictsTheLeastRecentlyPlayed(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	oldest := f.addExisting(t, db, "old1", 100, time.Now().Add(-10*time.Hour))
	newer := f.addExisting(t, db, "old2", 100, time.Now().Add(-1*time.Hour))
	o := dlOptions(f)
	o.MaxBytes = 250
	a := startWith(t, defaultSource(), db, o)
	a.openPlaylist()
	a.press("d") // +100 = 300 > 250
	a.waitFor("deleted 1 old download")
	if fileExists(oldest) {
		t.Error("the least recently played file should have been deleted")
	}
	if !fileExists(newer) {
		t.Error("the more recently played file must be kept")
	}
	got := a.downloads()
	if _, ok := got["old1"]; ok {
		t.Error("old1 still in the database")
	}
	if _, ok := got["a"]; !ok || got["old2"].ID == "" {
		t.Errorf("expected a and old2 to remain, got %v", got)
	}
	a.quit()
}

func TestCapNeverDeletesQueuedTracks(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	queued := f.addExisting(t, db, "b", 100, time.Now().Add(-100*time.Hour)) // oldest of all, but queued
	spare := f.addExisting(t, db, "zz", 100, time.Now().Add(-1*time.Hour))
	o := dlOptions(f)
	o.MaxBytes = 250
	a := startWith(t, defaultSource(), db, o)
	playFirst(a) // queue is A, B, C
	a.press("/")
	a.typeText("anything")
	a.press("enter")
	a.waitFor("Search One")
	a.press("d") // a new download pushes the total to 300
	a.waitFor("deleted 1 old download")
	if !fileExists(queued) {
		t.Error("a queued track's download was deleted")
	}
	if fileExists(spare) {
		t.Error("the unprotected file should have been deleted instead")
	}
	a.quit()
}

func TestCapStopsABatchInsteadOfDeletingItsOwnFiles(t *testing.T) {
	f := newFakeDownloader(t)
	o := dlOptions(f)
	o.MaxBytes = 150 // room for one 100-byte file, not two
	a := startWith(t, defaultSource(), nil, o)
	a.openPlaylist()
	a.press("D")
	a.waitFor("download cap reached")
	a.waitFor("skipped 1 more")
	got := f.callList()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("fetched %v, want a and b only; c must be skipped", got)
	}
	for _, id := range []string{"a", "b"} {
		if !fileExists(a.downloads()[id].Path) {
			t.Errorf("%s was deleted; a batch must not evict its own files", id)
		}
	}
	a.quit()
}

func TestUnlimitedCapKeepsEverything(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	old := f.addExisting(t, db, "old", 1000, time.Now().Add(-time.Hour))
	a := startWith(t, defaultSource(), db, dlOptions(f)) // MaxBytes 0 = unlimited
	a.openPlaylist()
	a.press("D")
	a.waitFor("downloaded 3 videos")
	if !fileExists(old) {
		t.Fatal("nothing should be evicted when there is no cap")
	}
	a.quit()
}

// --- shutdown -----------------------------------------------------------------

func TestQuitCancelsADownloadInFlight(t *testing.T) {
	f := newFakeDownloader(t)
	f.hold = make(chan struct{}) // never released: only cancellation can end it
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitFor("⇩ 40%")
	a.quit()
	a.eventually("the fetch saw its context cancelled", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.saw) == 1 && f.saw[0] == "a"
	})
	if _, ok := a.downloads()["a"]; ok {
		t.Fatal("a cancelled download must not be recorded")
	}
}

func TestQuitDropsQueuedDownloads(t *testing.T) {
	f := newFakeDownloader(t)
	f.hold = make(chan struct{})
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("D")
	a.waitFor("⇩ 40% Episode A (+2)")
	a.quit()
	time.Sleep(300 * time.Millisecond)
	if got := f.callList(); len(got) != 1 {
		t.Fatalf("fetched %v after quitting; the queued ones must not start", got)
	}
}

func TestHelpMentionsDownloadKeysOnlyWhenEnabled(t *testing.T) {
	on := startWith(t, defaultSource(), nil, dlOptions(newFakeDownloader(t)))
	on.waitFor("Podcasts")
	on.waitFor("d/D download")
	on.quit()

	off := start(t, defaultSource(), nil)
	off.waitFor("Podcasts")
	if s := off.screen(); strings.Contains(s, "d/D download") {
		t.Fatalf("download keys advertised although downloads are off:\n%s", s)
	}
	off.quit()
}
