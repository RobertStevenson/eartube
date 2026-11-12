package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// fakeArchiver serves canned details and records which videos were looked up.
type fakeArchiver struct {
	mu    sync.Mutex
	calls []string
	err   map[string]error
	hold  chan struct{} // if set, lookups block until it is closed
}

func newFakeArchiver() *fakeArchiver { return &fakeArchiver{err: map[string]error{}} }

func (f *fakeArchiver) Fetch(ctx context.Context, it ytdlp.Item) (store.Video, error) {
	f.mu.Lock()
	f.calls = append(f.calls, it.ID)
	hold, err := f.hold, f.err[it.ID]
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return store.Video{}, ctx.Err()
		}
	}
	if err != nil {
		return store.Video{}, err
	}
	return store.Video{ID: it.ID, Title: it.Title, Channel: it.Channel, Duration: it.Duration,
		UploadDate:  "2021-06-01",
		Description: "The full story of " + it.Title + ".\nSecond paragraph.",
		Tags:        []string{"tag-" + it.ID}, Thumbnail: "/thumbs/" + it.ID + ".jpg"}, nil
}

func (f *fakeArchiver) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeArchiver) count(id string) int {
	n := 0
	for _, c := range f.callList() {
		if c == id {
			n++
		}
	}
	return n
}

func archOptions(f *fakeArchiver) Options { return Options{Archiver: f} }

var podcasts = store.SavedPlaylist{ID: "PL1", Title: "Podcasts", URL: "https://yt/pl1"}

// savingDB is a database in which the given playlists (default: Podcasts)
// are chosen for saving, as pressing s on them would.
func savingDB(t *testing.T, pls ...store.SavedPlaylist) *store.Store {
	t.Helper()
	db := newDB(t)
	if len(pls) == 0 {
		pls = []store.SavedPlaylist{podcasts}
	}
	for _, p := range pls {
		if err := db.SetPlaylistSaved(p, true); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func (a *app) video(id string) (store.Video, bool) {
	a.t.Helper()
	v, ok, err := a.db.Video(id)
	if err != nil {
		a.t.Fatal(err)
	}
	return v, ok
}

func (a *app) hasDetails(id string) bool { v, _ := a.video(id); return v.HasDetails() }

// gone is how yt-dlp lists a video YouTube no longer serves.
func gone(id, placeholder string) ytdlp.Item {
	return ytdlp.Item{ID: id, Title: placeholder, URL: "https://yt/" + id, Unavailable: true}
}

func TestOpeningAPlaylistSavesItsVideosDetails(t *testing.T) {
	f := newFakeArchiver()
	a := startWith(t, defaultSource(), savingDB(t), archOptions(f))
	a.openPlaylist()
	a.eventually("details saved for every video", func() bool {
		return a.hasDetails("a") && a.hasDetails("b") && a.hasDetails("c")
	})
	v, _ := a.video("b")
	if v.Title != "Episode B" || v.Description == "" || v.Thumbnail != "/thumbs/b.jpg" ||
		len(v.Playlists) != 1 || v.Playlists[0] != "Podcasts" {
		t.Errorf("saved %+v", v)
	}

	// Opening it again looks nothing up: details are saved once.
	a.press("p")
	a.waitFor("Your playlists")
	a.openPlaylist()
	a.press("down") // a later key whose effect we can see: the earlier ones were handled
	a.press("i")
	a.waitFor("The full story of Episode B.")
	if n := f.count("b"); n != 1 {
		t.Errorf("b was looked up %d times, want 1", n)
	}
	a.quit()
}

func TestSearchResultsAreNotSaved(t *testing.T) {
	f := newFakeArchiver()
	a := startWith(t, defaultSource(), savingDB(t), archOptions(f))
	a.waitFor("Podcasts")
	a.press("/")
	a.typeText("x")
	a.press("enter")
	a.waitFor("Search One")
	a.press("i")
	a.waitFor("Nothing saved: only videos in playlists you save are")
	if _, ok := a.video("s1"); ok || len(f.callList()) > 0 {
		t.Errorf("a search result was saved or looked up: %v", f.callList())
	}
	a.quit()
}

// The whole point: a video saved earlier and since removed from YouTube is
// still identified, with everything that was saved.
func TestARemovedVideoShowsWhatWasSaved(t *testing.T) {
	db := savingDB(t)
	f := newFakeArchiver()
	src := defaultSource()
	a := startWith(t, src, db, archOptions(f))
	a.openPlaylist()
	a.eventually("details saved", func() bool { return a.hasDetails("b") })
	a.quit()

	src.byURL["https://yt/pl1"] = []ytdlp.Item{vid("a", "Episode A"), gone("b", "[Private video]"), vid("c", "Episode C")}
	a = startWith(t, src, db, archOptions(f))
	a.waitFor("Podcasts")
	a.press("enter")
	a.waitFor("⊘ Episode B")
	a.waitFor("unavailable · chan · 10:00 · i for saved details")
	if strings.Contains(a.screen(), "[Private video]") {
		t.Errorf("the placeholder title is shown instead of the saved one:\n%s", a.screen())
	}

	// enter on it cannot play it; it says what the video was instead.
	a.press("down", "enter")
	a.waitFor("This video is no longer on YouTube. This is what eartube saved.")
	a.waitFor("The full story of Episode B.")
	a.waitFor("Uploaded:     2021-06-01")
	a.waitFor("/thumbs/b.jpg")
	a.waitFor("unavailable since")
	if l := a.pl.loaded(); len(l) != 0 {
		t.Errorf("tried to play a removed video: %+v", l)
	}
	v, _ := a.video("b")
	if v.GoneAt.IsZero() {
		t.Error("not recorded as gone")
	}
	a.quit()
}

func TestAVideoGoneBeforeItWasSeenSaysNothingWasSaved(t *testing.T) {
	src := defaultSource()
	src.byURL["https://yt/pl1"] = []ytdlp.Item{gone("z", "[Deleted video]"), vid("a", "Episode A")}
	f := newFakeArchiver()
	a := startWith(t, src, savingDB(t), archOptions(f))
	a.waitFor("Podcasts")
	a.press("enter")
	a.waitFor("⊘ [Deleted video]")
	a.waitFor("nothing was saved before it went")
	a.press("i")
	a.waitFor("was already gone the first")
	a.waitFor("https://www.youtube.com/watch?v=z")
	a.waitFor("Playlists: Podcasts")
	a.quit()
	if f.count("z") != 0 {
		t.Error("looked up a video that is already gone")
	}
}

func TestRemovedVideosAreNotQueuedOrDownloaded(t *testing.T) {
	src := defaultSource()
	src.byURL["https://yt/pl1"] = []ytdlp.Item{gone("z", "[Private video]"), vid("a", "Episode A"), vid("b", "Episode B")}
	dl := newFakeDownloader(t)
	opts := dlOptions(dl)
	opts.Archiver = newFakeArchiver()
	a := startWith(t, src, savingDB(t), opts)
	a.waitFor("Podcasts")
	a.press("enter")
	a.waitFor("⊘ [Private video]")
	a.press("a")
	a.waitFor("cannot be queued")
	a.press("d")
	a.waitFor("cannot be downloaded")
	a.press("A")
	a.waitFor("playing: Episode A") // A starts playback when idle
	a.inspect(func(m *Model) {
		if len(m.queue) != 2 {
			t.Errorf("queue holds %d items, want the 2 playable ones", len(m.queue))
		}
	})
	a.press("D")
	a.eventually("both playable videos downloaded", func() bool { return len(dl.callList()) == 2 })
	if c := dl.callList(); c[0] == "z" || c[1] == "z" {
		t.Errorf("downloaded a removed video: %v", c)
	}
	a.press("tab")
	a.waitFor("Queue")
	if strings.Contains(a.screen(), "Private video") {
		t.Errorf("the queue holds a removed video:\n%s", a.screen())
	}
	a.quit()
}

func TestSweepReadsEverySavedPlaylistWithoutOpeningThem(t *testing.T) {
	src := defaultSource()
	src.playlists = append(src.playlists, ytdlp.Item{ID: "PL2", Title: "Talks", URL: "https://yt/pl2"})
	src.byURL["https://yt/pl2"] = []ytdlp.Item{vid("t1", "Talk 1"), vid("a", "Episode A")}
	f := newFakeArchiver()
	db := savingDB(t, podcasts, store.SavedPlaylist{ID: "PL2", Title: "Talks", URL: "https://yt/pl2"})
	a := startWith(t, src, db, archOptions(f))
	a.waitFor("Podcasts")
	a.press("I")
	a.waitFor("saved details for every video in 2 playlist(s)")
	for _, id := range []string{"a", "b", "c", "t1"} {
		if !a.hasDetails(id) {
			t.Errorf("%s has no details after the sweep", id)
		}
	}
	if f.count("a") != 1 {
		t.Errorf("a is in two playlists but should be looked up once, got %d", f.count("a"))
	}
	if v, _ := a.video("a"); len(v.Playlists) != 2 {
		t.Errorf("a's playlists = %q", v.Playlists)
	}
	a.quit()
}

func TestAFailedLookupIsNotRetriedAndIsReported(t *testing.T) {
	f := newFakeArchiver()
	f.err["b"] = errors.New("sign in to confirm your age")
	a := startWith(t, defaultSource(), savingDB(t), archOptions(f))
	a.openPlaylist()
	a.eventually("the others saved", func() bool { return a.hasDetails("a") && a.hasDetails("c") })
	a.press("down", "i")
	a.waitFor("could not be looked up")
	a.waitFor("Channel:      chan") // what the listing showed is still saved
	a.press("esc", "up")            // back to row 1: the playlists list has only one row
	a.waitFor("Episode A")
	a.press("p")
	a.waitFor("Your playlists")
	a.openPlaylist()
	a.press("i")
	a.waitFor("Video details")
	if n := f.count("b"); n != 1 {
		t.Errorf("a failed lookup was retried in the same session: %d calls", n)
	}
	a.quit()
}

// Pressing i on a video whose lookup is still queued moves it to the front.
func TestDetailsViewJumpsTheQueueAndFillsInWhenTheLookupFinishes(t *testing.T) {
	f := newFakeArchiver()
	f.hold = make(chan struct{})
	a := startWith(t, defaultSource(), savingDB(t), archOptions(f))
	a.openPlaylist()
	a.eventually("the first lookup started", func() bool { return len(f.callList()) == 1 })
	a.waitFor("saving video details (3 left)")
	a.press("down", "down", "i")
	a.waitFor("looking up the full details…")
	a.waitFor("title, channel and length only")
	f.mu.Lock()
	close(f.hold)
	f.hold = nil
	f.mu.Unlock()
	a.waitFor("The full story of Episode C.") // the open view refreshes itself
	a.waitGone("looking up the full details…")
	if c := f.callList(); len(c) < 2 || c[1] != "c" {
		t.Errorf("lookup order = %v, want c second (it was asked for)", c)
	}
	a.quit()
}

func TestDetailsViewKeys(t *testing.T) {
	f := newFakeArchiver()
	a := startWith(t, defaultSource(), savingDB(t), archOptions(f))
	a.openPlaylist()
	a.eventually("details saved", func() bool { return a.hasDetails("a") })
	a.press("i")
	a.waitFor("Video details")
	a.press("a") // swallowed: must not queue the hidden list's video
	a.press(" ") // playback keys still work
	a.eventually("space reached the player", func() bool {
		a.pl.mu.Lock()
		defer a.pl.mu.Unlock()
		return a.pl.pauses == 1
	})
	a.press("i")
	a.waitGone("Video details")
	a.waitFor("Episode B")
	if l := a.pl.loaded(); len(l) != 0 {
		t.Errorf("a key reached the list behind the details view and played %+v", l)
	}
	a.inspect(func(m *Model) {
		if len(m.queue) != 0 {
			t.Errorf("a key reached the list behind the details view: queue = %v", m.queue)
		}
	})
	a.press("i")
	a.waitFor("Video details")
	a.press("esc")
	a.waitGone("Video details")
	a.quit()
}

func TestDetailsViewScrollsALongDescription(t *testing.T) {
	db := savingDB(t)
	long := strings.Repeat("line\n", 60) + "THE END"
	db.RecordPlaylist("PL1", "Podcasts", []store.Sighting{{ID: "a", Title: "Episode A", Channel: "chan"}}, timeZero)
	db.SaveDetails(store.Video{ID: "a", Description: long}, timeZero)
	a := startWith(t, defaultSource(), db, archOptions(newFakeArchiver()))
	a.openPlaylist()
	a.press("i")
	a.waitFor("↓ more below")
	if strings.Contains(a.screen(), "THE END") {
		t.Fatal("the whole description fits a 30-row terminal; the test needs a longer one")
	}
	a.press("G")
	a.waitFor("THE END")
	a.waitFor("↑ more above")
	if h := renderedHeight(a.screen(), 120); h > 30 {
		t.Errorf("the view is %d rows, taller than the 30-row terminal", h)
	}
	a.quit()
}

var timeZero = time.Unix(1_700_000_000, 0)
