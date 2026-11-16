package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// f opens the newest videos from your subscriptions, with no Shorts tab, and
// pressing it again loads the list afresh.
func TestSubscriptionsShowNewVideos(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.waitFor("Podcasts")

	a.press("f")
	a.waitFor("Subscriptions")
	a.waitFor("New Video One")
	a.waitFor("2 items · r refresh")
	if s := a.screen(); strings.Contains(s, "Shorts") {
		t.Fatalf("the subscriptions view still mentions Shorts:\n%s", s)
	}

	src.subs.set([]ytdlp.Item{vid("v0", "Just Uploaded")})
	a.press("f")
	a.waitFor("Just Uploaded")
	a.waitGone("New Video One")
	if got := src.subs.callList(); !reflect.DeepEqual(got, []int{50, 50}) {
		t.Fatalf("feed requests = %v, want two of 50", got)
	}
	a.quit()
}

func TestASubscriptionVideoPlays(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("f")
	a.waitFor("New Video One")
	a.press("enter")
	a.eventually("the video to load", func() bool {
		l := a.pl.loaded()
		return len(l) == 1 && l[0].url == "https://yt/v1"
	})
	a.quit()
}

// r reloads the feed on show, picks up new uploads, and starts again at the top.
func TestSubscriptionsRefresh(t *testing.T) {
	src := defaultSource()
	a := start(t, src, nil)
	a.waitFor("Podcasts")
	a.press("f")
	a.waitFor("New Video Two")
	a.press("down")
	a.eventually("the cursor to move down", func() bool {
		var i int
		a.inspect(func(m *Model) { i = m.list.Index() })
		return i == 1
	})

	src.subs.set([]ytdlp.Item{vid("v0", "Just Uploaded"), vid("v1", "New Video One")})
	a.press("r")
	a.waitFor("Just Uploaded")
	a.waitGone("New Video Two")
	var i int
	a.inspect(func(m *Model) { i = m.list.Index() })
	if i != 0 {
		t.Errorf("cursor at %d after a refresh, want the newest (0)", i)
	}
	if got := src.subs.callList(); len(got) != 2 {
		t.Fatalf("feed requests = %v, want two", got)
	}
	a.quit()
}

// r does nothing outside the subscriptions view but say how to get there: not
// in your playlists, not in the queue, not in the Downloaded view.
func TestRefreshOnlyWorksInSubscriptions(t *testing.T) {
	src := defaultSource()
	f := newFakeDownloader(t)
	a := startWith(t, src, library(t, f), dlOptions(f))
	a.waitFor("Podcasts")
	a.press("r")
	a.waitFor("r refreshes your subscriptions: press f to open them")

	a.press("f")
	a.waitFor("New Video One")
	a.press("tab")
	a.waitFor("Queue")
	a.press("r")
	a.waitFor("r refreshes your subscriptions: press f to open them")
	a.press("tab")
	a.waitFor("New Video One")

	a.press("p")
	a.waitFor("Your playlists")
	a.press("r")
	a.waitFor("r refreshes your subscriptions: press f to open them")

	a.press("f")
	a.waitFor("New Video One")
	a.press("o")
	a.waitFor("downloaded: enter plays")
	a.press("r")
	a.waitFor("r refreshes your subscriptions: press f to open them")
	if got := src.subs.callList(); len(got) != 2 {
		t.Fatalf("feed requests = %v, want just the two from f", got)
	}
	a.quit()
}

func TestSubscriptionsCountComesFromOptions(t *testing.T) {
	src := defaultSource()
	a := startWith(t, src, nil, Options{SubscriptionsCount: 7})
	a.waitFor("Podcasts")
	a.press("f")
	a.waitFor("New Video One")
	if got := src.subs.callList(); !reflect.DeepEqual(got, []int{7}) {
		t.Fatalf("feed requests = %v, want [7]", got)
	}
	a.quit()
}

// Nothing from the subscriptions feed is saved: not as a video in the
// archive, and no details are looked up, even when playlists are being saved.
func TestSubscriptionsAreNotSaved(t *testing.T) {
	f := newFakeArchiver()
	a := startWith(t, defaultSource(), savingDB(t, podcasts), archOptions(f))
	a.waitFor("Podcasts")
	a.press("f")
	a.waitFor("New Video One")
	a.press("i") // details of an unsaved video must not start a lookup
	a.waitFor("details: ↑/↓ scroll, esc closes")
	a.press("esc")
	a.press("r")
	a.waitFor("2 items · r refresh")
	for _, id := range []string{"v1", "v2"} {
		if _, ok := a.video(id); ok {
			t.Errorf("%s was saved to the archive", id)
		}
	}
	if calls := f.callList(); len(calls) > 0 {
		t.Errorf("details looked up for %v", calls)
	}
	a.quit()
}

func TestSubscriptionsErrorIsShown(t *testing.T) {
	src := defaultSource()
	src.subs.err = errors.New("your playlists and subscriptions need a signed-in browser")
	a := start(t, src, nil)
	a.waitFor("Podcasts")
	a.press("f")
	a.waitFor("error: your playlists and subscriptions need a signed-in browser")
	a.waitFor("Your playlists") // the list it failed over is left alone
	a.quit()
}
