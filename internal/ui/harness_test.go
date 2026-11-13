package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/RobertStevenson/eartube/internal/download"
	"github.com/RobertStevenson/eartube/internal/player"
	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// fakePlayer records what the UI asks of it and lets tests script playback.
type fakePlayer struct {
	mu     sync.Mutex
	loads  []load
	pauses int
	seeks  []float64
	speeds []float64
	st     player.Status
	ended  chan bool
}

type load struct {
	url    string
	stream string // set when the UI used a pre-resolved stream URL
	file   string // set when the UI played a downloaded file
	start  float64
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{ended: make(chan bool, 1), st: player.Status{Idle: true, Speed: 1}}
}

func (f *fakePlayer) Load(url string, start float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, load{url: url, start: start})
	f.st = player.Status{Speed: f.st.Speed, Pos: start}
}
func (f *fakePlayer) LoadResolved(url, stream string, start float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, load{url: url, stream: stream, start: start})
	f.st = player.Status{Speed: f.st.Speed, Pos: start}
}
func (f *fakePlayer) LoadFile(url, path string, start float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, load{url: url, file: path, start: start})
	f.st = player.Status{Speed: f.st.Speed, Pos: start}
}
func (f *fakePlayer) TogglePause() { f.mu.Lock(); f.pauses++; f.mu.Unlock() }
func (f *fakePlayer) Seek(s float64) {
	f.mu.Lock()
	f.seeks = append(f.seeks, s)
	f.mu.Unlock()
}
func (f *fakePlayer) AddSpeed(d float64) {
	f.mu.Lock()
	f.speeds = append(f.speeds, d)
	f.mu.Unlock()
}
func (f *fakePlayer) Status() player.Status { f.mu.Lock(); defer f.mu.Unlock(); return f.st }
func (f *fakePlayer) Done() <-chan bool     { return f.ended }

// setStatus scripts what mpv would report (position, duration, ...).
func (f *fakePlayer) setStatus(s player.Status) { f.mu.Lock(); f.st = s; f.mu.Unlock() }
func (f *fakePlayer) loaded() []load {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]load(nil), f.loads...)
}

// fakeSource serves canned results.
type fakeSource struct {
	playlists  []ytdlp.Item
	byURL      map[string][]ytdlp.Item
	search     []ytdlp.Item
	err        error
	resolveErr error
	resolves   *resolveLog // shared, since fakeSource is passed by value
	subs       *subsFeed   // shared, so a test can change the feeds between loads
}

// subsFeed is the fake subscriptions feeds, and a record of each request.
type subsFeed struct {
	mu             sync.Mutex
	videos, shorts []ytdlp.Item
	err            error
	calls          []subsCall
}

type subsCall struct {
	shorts bool
	n      int
}

func (f *subsFeed) set(videos, shorts []ytdlp.Item) {
	f.mu.Lock()
	f.videos, f.shorts = videos, shorts
	f.mu.Unlock()
}

func (f *subsFeed) callList() []subsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]subsCall(nil), f.calls...)
}

func (s fakeSource) Subscriptions(_ context.Context, shorts bool, n int) ([]ytdlp.Item, error) {
	f := s.subs
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, subsCall{shorts, n})
	items := f.videos
	if shorts {
		items = f.shorts
	}
	return items, f.err
}

// resolveLog records which pages the UI asked to resolve ahead of time.
type resolveLog struct {
	mu   sync.Mutex
	urls []string
}

func (l *resolveLog) add(u string) { l.mu.Lock(); l.urls = append(l.urls, u); l.mu.Unlock() }
func (l *resolveLog) count(u string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, x := range l.urls {
		if x == u {
			n++
		}
	}
	return n
}
func (l *resolveLog) total() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.urls) }

func (s fakeSource) Resolve(_ context.Context, url string) (string, error) {
	s.resolves.add(url)
	if s.resolveErr != nil {
		return "", s.resolveErr
	}
	return "direct://" + url, nil
}

func (s fakeSource) Search(context.Context, string, int) ([]ytdlp.Item, error) {
	return s.search, s.err
}
func (s fakeSource) Playlists(context.Context) ([]ytdlp.Item, error) {
	return s.playlists, s.err
}
func (s fakeSource) Playlist(_ context.Context, url string) ([]ytdlp.Item, error) {
	return s.byURL[url], s.err
}

func vid(id, title string) ytdlp.Item {
	return ytdlp.Item{ID: id, Title: title, Channel: "chan", Duration: 600, URL: "https://yt/" + id}
}

// screenProbe wraps the model and remembers the latest View() it rendered. The
// terminal output stream is a series of diffs (unchanged lines are not
// redrawn), so asserting on the model's own view is far less brittle.
type screenProbe struct {
	inner Model
	mu    *sync.Mutex
	last  *string
	model *Model // latest model, for tests that inspect internal state
}

func (p screenProbe) Init() tea.Cmd { return p.inner.Init() }
func (p screenProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.mu.Lock() // the test goroutine may inspect the model concurrently
	defer p.mu.Unlock()
	m, cmd := p.inner.Update(msg)
	p.inner = m.(Model)
	*p.model = p.inner
	return p, cmd
}
func (p screenProbe) View() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.inner.View()
	*p.last = v
	return v
}

type screenReader struct {
	mu   *sync.Mutex
	last *string
}

func (r screenReader) get() string { r.mu.Lock(); defer r.mu.Unlock(); return *r.last }

// app bundles a running program with its fakes and real database.
type app struct {
	t     *testing.T
	tm    *teatest.TestModel
	view  screenReader
	mu    *sync.Mutex
	model *Model
	pl    *fakePlayer
	db    *store.Store
	src   fakeSource
}

func defaultSource() fakeSource {
	return fakeSource{
		playlists: []ytdlp.Item{{ID: "PL1", Title: "Podcasts", URL: "https://yt/pl1"}},
		byURL: map[string][]ytdlp.Item{
			"https://yt/pl1": {vid("a", "Episode A"), vid("b", "Episode B"), vid("c", "Episode C")},
		},
		search:   []ytdlp.Item{vid("s1", "Search One"), vid("s2", "Search Two")},
		resolves: &resolveLog{},
		subs: &subsFeed{
			videos: []ytdlp.Item{vid("v1", "New Video One"), vid("v2", "New Video Two")},
			shorts: []ytdlp.Item{
				{ID: "sh1", URL: "https://www.youtube.com/shorts/sh1"}, // the feed often has no title
				{ID: "sh2", Title: "A Titled Short", URL: "https://www.youtube.com/shorts/sh2"},
			},
		},
	}
}

// start launches the real Bubble Tea program against fakes. Pass a db to share
// state across restarts; nil creates a fresh one.
func start(t *testing.T, src fakeSource, db *store.Store) *app {
	t.Helper()
	return startWith(t, src, db, Options{})
}

// startWith is start with explicit options (downloads, caps, ...).
func startWith(t *testing.T, src fakeSource, db *store.Store, opts Options) *app {
	t.Helper()
	if db == nil {
		var err error
		db, err = store.Open(filepath.Join(t.TempDir(), "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
	}
	pl := newFakePlayer()
	var mu sync.Mutex
	var last string
	var snapshot Model
	probe := screenProbe{inner: New(pl, db, src, opts), mu: &mu, last: &last, model: &snapshot}
	tm := teatest.NewTestModel(t, probe, teatest.WithInitialTermSize(120, 30))
	return &app{t: t, tm: tm, pl: pl, db: db, src: src, view: screenReader{&mu, &last}, mu: &mu, model: &snapshot}
}

var keys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "tab": tea.KeyTab, "esc": tea.KeyEsc,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
}

// press sends named keys ("enter", "down") or single characters ("a", " ").
func (a *app) press(names ...string) {
	a.t.Helper()
	for _, n := range names {
		if kt, ok := keys[n]; ok {
			a.tm.Send(tea.KeyMsg{Type: kt})
		} else {
			a.tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(n)})
		}
	}
}

// typeText types literal text into a focused input.
func (a *app) typeText(s string) { a.tm.Type(s) }

// screen returns the most recently rendered view.
func (a *app) screen() string { return a.view.get() }

// waitFor blocks until the current screen contains text.
func (a *app) waitFor(text string) {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(a.screen(), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.t.Fatalf("timed out waiting for %q on screen. Screen:\n%s", text, a.screen())
}

// waitGone blocks until text is no longer on screen.
func (a *app) waitGone(text string) {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !strings.Contains(a.screen(), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.t.Fatalf("%q still on screen after 5s. Screen:\n%s", text, a.screen())
}

// eventually polls a condition that is not visible on screen (fake or db state).
func (a *app) eventually(desc string, cond func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.t.Fatalf("timed out waiting for: %s", desc)
}

// quit presses q and waits for the program to exit, so saves have happened.
func (a *app) quit() {
	a.t.Helper()
	a.press("q")
	a.tm.WaitFinished(a.t, teatest.WithFinalTimeout(5*time.Second))
}

// openPlaylist waits for the playlist list and opens the first playlist.
func (a *app) openPlaylist() {
	a.t.Helper()
	a.waitFor("Podcasts")
	a.press("enter")
	a.waitFor("Episode A")
}

type boomErr struct{}

func (boomErr) Error() string { return "boom" }

var errBoom error = boomErr{}

func newDB(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// track reads one track's persisted state.
func (a *app) track(id string) store.Track {
	a.t.Helper()
	all, err := a.db.All()
	if err != nil {
		a.t.Fatal(err)
	}
	return all[id]
}

// inspect runs fn on the program's latest model while it is not being updated.
func (a *app) inspect(fn func(m *Model)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a.model)
}

// resolvedReady reports whether a usable pre-resolved stream is cached for id.
func (a *app) resolvedReady(id string) bool {
	ok := false
	a.inspect(func(m *Model) { ok = m.resolved[id].usable(time.Now()) })
	return ok
}

var errNeedsSignIn = errors.New("sign in to confirm you're not a bot")

// playingStatus is a fake mpv status for a track that is well underway.
func playingStatus(pos float64) player.Status {
	return player.Status{Pos: pos, Duration: 600, Speed: 1}
}

// fakeDownloader writes real files so the UI's disk handling is exercised.
type fakeDownloader struct {
	dir   string
	mu    sync.Mutex
	calls []string         // video IDs, in the order they were requested
	size  map[string]int64 // bytes to write per ID (default 100)
	err   map[string]error
	hold  chan struct{} // if set, downloads block (reporting 40%) until closed
	live  int           // downloads in flight right now
	peak  int           // most ever in flight at once
	saw   []string      // IDs whose download was cancelled by the app
}

func newFakeDownloader(t *testing.T) *fakeDownloader {
	return &fakeDownloader{dir: t.TempDir(), size: map[string]int64{}, err: map[string]error{}}
}

func (f *fakeDownloader) Fetch(ctx context.Context, id, url string, progress func(float64)) (download.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.live++
	if f.live > f.peak {
		f.peak = f.live
	}
	hold, err, n := f.hold, f.err[id], f.size[id]
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.live--; f.mu.Unlock() }()

	if hold != nil {
		progress(40)
		select {
		case <-hold:
		case <-ctx.Done():
			f.mu.Lock()
			f.saw = append(f.saw, id)
			f.mu.Unlock()
			return download.Result{}, ctx.Err()
		}
	}
	if err != nil {
		return download.Result{}, err
	}
	if n == 0 {
		n = 100
	}
	path := filepath.Join(f.dir, id+".m4a")
	if werr := os.WriteFile(path, make([]byte, n), 0o644); werr != nil {
		return download.Result{}, werr
	}
	progress(100)
	return download.Result{Path: path, Bytes: n}, nil
}

func (f *fakeDownloader) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// addExisting puts a file on disk and in the database, as if downloaded earlier.
func (f *fakeDownloader) addExisting(t *testing.T, db *store.Store, id string, bytes int64, lastUsed time.Time) string {
	t.Helper()
	path := filepath.Join(f.dir, id+".m4a")
	if err := os.WriteFile(path, make([]byte, bytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDownload(store.Download{ID: id, Path: path, Bytes: bytes, LastUsed: lastUsed}); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// addWithMeta is addExisting for a download that also remembers its title,
// channel and duration, as downloads made since v0.5 do.
func (f *fakeDownloader) addWithMeta(t *testing.T, db *store.Store, id, title, channel string, duration float64, bytes int64, lastUsed time.Time) string {
	t.Helper()
	path := f.addExisting(t, db, id, bytes, lastUsed)
	if err := db.SetDownloadMeta(id, title, channel, duration); err != nil {
		t.Fatal(err)
	}
	return path
}

// renderedHeight is how many terminal rows a view really occupies at width w,
// counting the wrapping a terminal would apply to over-long lines.
func renderedHeight(view string, w int) int {
	return lipgloss.Height(lipgloss.NewStyle().Width(w).Render(view))
}
