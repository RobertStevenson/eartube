package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobertStevenson/eartube/internal/store"
)

// library puts three downloads in a database, oldest-played first in time.
func library(t *testing.T, f *fakeDownloader) *store.Store {
	db := newDB(t)
	now := time.Now()
	f.addWithMeta(t, db, "x1", "Oldest Talk", "Old Channel", 600, 1000, now.Add(-10*time.Hour))
	f.addWithMeta(t, db, "x2", "Middle Talk", "Mid Channel", 3725, 2000, now.Add(-5*time.Hour))
	f.addWithMeta(t, db, "x3", "Newest Talk", "New Channel", 90, 3000, now.Add(-1*time.Hour))
	return db
}

func TestDownloadedViewListsMostRecentlyPlayedFirst(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Downloaded · 3 videos")
	s := a.screen()
	n, m, o := strings.Index(s, "Newest Talk"), strings.Index(s, "Middle Talk"), strings.Index(s, "Oldest Talk")
	if n < 0 || m < 0 || o < 0 || !(n < m && m < o) {
		t.Fatalf("want newest, middle, oldest top to bottom; got positions %d %d %d:\n%s", n, m, o, s)
	}
	a.quit()
}

func TestDownloadedViewShowsDetailsAndTotalSize(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Downloaded · 3 videos · 6000 B") // 1000 + 2000 + 3000 bytes
	a.waitFor("Mid Channel · 1:02:05")
	a.waitFor("↓ downloaded")
	a.quit()
}

func TestDownloadedViewWorksWithoutANetwork(t *testing.T) {
	f := newFakeDownloader(t)
	src := defaultSource()
	src.err = errBoom // YouTube is unreachable: nothing else will load
	a := startWith(t, src, library(t, f), dlOptions(f))
	a.waitFor("offline? press o for your downloaded videos")
	a.press("o")
	a.waitFor("Newest Talk")
	a.press("enter")
	a.waitFor("playing: Newest Talk")
	first := a.pl.loaded()[0]
	if first.file == "" || first.url != "https://www.youtube.com/watch?v=x3" {
		t.Fatalf("first load = %+v, want the downloaded file for x3", first)
	}
	a.quit()
}

func TestOfflineHintIsNotShownWithoutDownloads(t *testing.T) {
	src := defaultSource()
	src.err = errBoom
	a := start(t, src, nil)
	a.waitFor("boom")
	if s := a.screen(); strings.Contains(s, "offline?") {
		t.Fatalf("offered downloads that do not exist:\n%s", s)
	}
	a.quit()
}

func TestDownloadedViewWhenEmpty(t *testing.T) {
	on := startWith(t, defaultSource(), nil, dlOptions(newFakeDownloader(t)))
	on.waitFor("Podcasts")
	on.press("o")
	on.waitFor("no downloads yet: press d on a video")
	if strings.Contains(on.screen(), "after setting downloads") {
		t.Error("told a user with downloads on to turn them on")
	}
	on.quit()

	off := start(t, defaultSource(), nil)
	off.waitFor("Podcasts")
	off.press("o")
	off.waitFor("after setting downloads = true")
	off.quit()
}

func TestDownloadedViewStillWorksWhenDownloadsAreTurnedOff(t *testing.T) {
	f := newFakeDownloader(t)
	a := start(t, defaultSource(), library(t, f)) // zero Options: feature off
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Downloaded · 3 videos")
	a.quit()
}

func TestEnterInTheDownloadedViewQueuesTheRest(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Newest Talk")
	a.press("down", "enter") // Middle Talk, with Oldest Talk still to come
	a.waitFor("playing: Middle Talk")
	a.waitFor("+1 queued")
	a.quit()
}

func TestRemovingFromTheDownloadedViewDropsItAtOnce(t *testing.T) {
	f := newFakeDownloader(t)
	db := library(t, f)
	path := db2path(t, db, "x3")
	a := startWith(t, defaultSource(), db, dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Newest Talk")
	a.press("X")              // the cursor is on the newest
	a.waitGone("New Channel") // (the status line still names the title, so wait on the row's channel)
	a.waitFor("Downloaded · 2 videos")
	if fileExists(path) {
		t.Fatal("the file is still on disk")
	}
	a.quit()
}

func db2path(t *testing.T, db *store.Store, id string) string {
	t.Helper()
	all, err := db.Downloads()
	if err != nil || all[id].Path == "" {
		t.Fatalf("no download %s: %v", id, err)
	}
	return all[id].Path
}

func TestPlayedDownloadsLeaveTheViewWhenDeletePlayedIsOn(t *testing.T) {
	f := newFakeDownloader(t)
	o := dlOptions(f)
	o.DeletePlayed = true
	a := startWith(t, defaultSource(), library(t, f), o)
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Newest Talk")
	a.press("enter")
	a.waitFor("playing: Newest Talk")
	a.pl.ended <- true // finishes; delete_played removes it
	a.waitGone("New Channel")
	a.waitFor("Downloaded · 2 videos")
	a.quit()
}

func TestViewKeepsTheCursorOnTheSameRowAfterARemoval(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Newest Talk")
	a.press("down", "X") // remove Middle Talk; the cursor should land on Oldest Talk
	a.waitGone("Mid Channel")
	a.press("enter")
	a.waitFor("playing: Oldest Talk")
	a.quit()
}

func TestLeavingTheDownloadedView(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Downloaded · 3 videos")
	a.press("/")
	a.typeText("anything")
	a.press("enter")
	a.waitFor("Search One")
	a.press("X") // on a search result this must be a harmless no-op, not "delete everything"
	a.waitFor("not downloaded")
	a.press("m") // any list refresh must keep showing the search results...
	a.waitFor("✓ Search One")
	if strings.Contains(a.screen(), "Downloaded ·") {
		t.Fatalf("the Downloaded view took the list back after a search:\n%s", a.screen())
	}
	if got, _ := a.db.Downloads(); len(got) != 3 {
		t.Fatalf("downloads = %d, want all 3 untouched", len(got))
	}
	a.quit()
}

func TestQueueViewRoundTripKeepsTheDownloadedView(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Downloaded · 3 videos")
	a.press("tab")
	a.waitFor("Queue")
	a.press("tab")
	a.waitFor("Downloaded · 3 videos")
	a.waitFor("Oldest Talk")
	a.quit()
}

// --- metadata -------------------------------------------------------------------

func TestADownloadRemembersItsTitleChannelAndDuration(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("d")
	a.waitDownloaded("a")
	got := a.downloads()["a"]
	if got.Title != "Episode A" || got.Channel != "chan" || got.Duration != 600 {
		t.Fatalf("recorded %+v, want title, channel and duration kept for offline listing", got)
	}
	a.quit()
}

func TestOldDownloadsLearnTheirTitlesWhenNextSeen(t *testing.T) {
	db := newDB(t)
	f := newFakeDownloader(t)
	f.addExisting(t, db, "a", 100, time.Now()) // like a v0.4.0 download: no metadata
	a := startWith(t, defaultSource(), db, dlOptions(f))
	a.waitFor("Podcasts")
	a.press("o")
	a.waitFor("Downloaded · 1 video")
	a.waitFor("a ") // untitled: shown by id for now
	a.press("p")    // reload playlists, then open the one containing Episode A
	a.waitFor("Podcasts")
	a.press("enter")
	a.waitFor("Episode A")
	a.eventually("title learned and saved", func() bool { return a.downloads()["a"].Title == "Episode A" })
	a.press("o")
	a.waitFor("Downloaded · 1 video")
	a.waitFor("Episode A")
	a.quit()
}

// --- footer ---------------------------------------------------------------------

// The help footer wraps on narrow terminals; the list must shrink to leave room
// so nothing is pushed off the top of the screen.
func TestEverythingFitsOnNarrowTerminals(t *testing.T) {
	for _, opts := range []Options{{}, {Downloads: true}} {
		for _, size := range [][2]int{{200, 40}, {120, 30}, {100, 24}, {80, 24}, {60, 20}} {
			w, h := size[0], size[1]
			m := New(newFakePlayer(), newDB(t), defaultSource(), opts)
			nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			lines := renderedHeight(nm.(Model).View(), w)
			if lines > h {
				t.Errorf("downloads=%v at %dx%d: view is %d lines, taller than the %d-line terminal",
					opts.Downloads, w, h, lines, h)
			}
		}
	}
}
