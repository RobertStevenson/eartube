package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

var talks = store.SavedPlaylist{ID: "PL2", Title: "Talks", URL: "https://yt/pl2"}

// twoPlaylists has Podcasts (a, b, c) and Talks (t1, and a again).
func twoPlaylists() fakeSource {
	src := defaultSource()
	src.playlists = append(src.playlists, ytdlp.Item{ID: talks.ID, Title: talks.Title, URL: talks.URL})
	src.byURL[talks.URL] = []ytdlp.Item{vid("t1", "Talk 1"), vid("a", "Episode A")}
	return src
}

func (a *app) savedPlaylistIDs() []string {
	a.t.Helper()
	pls, err := a.db.SavedPlaylists()
	if err != nil {
		a.t.Fatal(err)
	}
	var ids []string
	for _, p := range pls {
		ids = append(ids, p.ID)
	}
	return ids
}

// Nothing is saved until you choose a playlist: a catch-all such as Liked
// videos must not be swept up by default.
func TestNoPlaylistIsSavedUntilYouChooseIt(t *testing.T) {
	f := newFakeArchiver()
	a := startWith(t, defaultSource(), nil, archOptions(f))
	a.openPlaylist()
	a.press("I")
	a.waitFor("no playlists are saved yet: press p, then s on each one to save")
	if _, ok := a.video("a"); ok || len(f.callList()) > 0 {
		t.Fatalf("an unchosen playlist was saved: lookups %v", f.callList())
	}
	a.press("s")
	a.waitFor("s chooses which playlists to save")

	a.press("p")
	a.waitFor("Your playlists")
	a.waitFor("Podcasts")
	a.press("s")
	a.waitFor("saved details for every video in 1 playlist(s)")
	for _, id := range []string{"a", "b", "c"} {
		if !a.hasDetails(id) {
			t.Errorf("%s has no details after choosing its playlist", id)
		}
	}
	a.waitFor("playlist · saving details (3 saved)")
	if got := a.savedPlaylistIDs(); len(got) != 1 || got[0] != "PL1" {
		t.Errorf("saved playlists = %v", got)
	}
	a.quit()
}

func TestStoppingAPlaylistAsksThenKeepsACopyAndDeletes(t *testing.T) {
	f := newFakeArchiver()
	db := savingDB(t, podcasts, talks)
	opts := archOptions(f)
	opts.RemovedDir = filepath.Join(t.TempDir(), "removed")
	a := startWith(t, twoPlaylists(), db, opts)
	a.waitFor("Podcasts")
	a.press("I")
	a.waitFor("saved details for every video in 2 playlist(s)")
	a.press("p")
	a.waitFor("playlist · saving details (3 saved)")

	// a is also in Talks, so only b and c would go.
	a.press("s")
	a.waitFor("stop saving Podcasts: also delete the 2 video(s) saved only from it?")
	a.press("enter", "a") // swallowed while the question is open
	a.press("esc")
	a.waitFor("still saving Podcasts")
	if strings.Contains(a.screen(), "Episode A") {
		t.Fatalf("enter opened the playlist while the question was open:\n%s", a.screen())
	}
	a.inspect(func(m *Model) {
		if len(m.queue) != 0 {
			t.Errorf("a reached the list while the question was open: %v", m.queue)
		}
	})
	if got := a.savedPlaylistIDs(); len(got) != 2 {
		t.Fatalf("esc should change nothing: saved = %v", got)
	}

	a.press("s")
	a.waitFor("also delete the 2 video(s)")
	a.press("y")
	a.waitFor("stopped saving Podcasts and deleted 2 video(s); a copy is in")
	for _, id := range []string{"b", "c"} {
		if _, ok := a.video(id); ok {
			t.Errorf("%s should be deleted", id)
		}
	}
	if v, ok := a.video("a"); !ok || len(v.Playlists) != 1 || v.Playlists[0] != "Talks" {
		t.Errorf("a should be kept, in Talks only: %+v", v)
	}
	if got := a.savedPlaylistIDs(); len(got) != 1 || got[0] != "PL2" {
		t.Errorf("saved playlists = %v, want only Talks", got)
	}
	a.waitFor("playlist · saving details (2 saved)") // Talks; Podcasts is plain again
	if strings.Contains(a.screen(), "not saving") {
		t.Errorf("Podcasts has nothing left but is still listed with saved videos:\n%s", a.screen())
	}

	// The copy holds exactly what was deleted.
	folders, _ := filepath.Glob(filepath.Join(opts.RemovedDir, "*-podcasts"))
	if len(folders) != 1 {
		t.Fatalf("copy folders = %v", folders)
	}
	b, err := os.ReadFile(filepath.Join(folders[0], "videos.json"))
	if err != nil {
		t.Fatal(err)
	}
	var copied []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	json.Unmarshal(b, &copied)
	var ids []string
	for _, v := range copied {
		ids = append(ids, v.ID)
		if v.Description == "" {
			t.Errorf("the copy of %s lacks its description", v.ID)
		}
	}
	sort.Strings(ids)
	if strings.Join(ids, ",") != "b,c" {
		t.Errorf("copy holds %v, want b and c", ids)
	}
	a.quit()
}

func TestStoppingAPlaylistCanKeepWhatWasSaved(t *testing.T) {
	f := newFakeArchiver()
	opts := archOptions(f)
	opts.RemovedDir = filepath.Join(t.TempDir(), "removed")
	a := startWith(t, defaultSource(), savingDB(t), opts)
	a.waitFor("Podcasts")
	a.press("I")
	a.waitFor("saved details for every video in 1 playlist(s)")
	a.press("p")
	a.waitFor("saving details (3 saved)")
	a.press("s")
	a.waitFor("also delete the 3 video(s)")
	a.press("n")
	a.waitFor("stopped saving Podcasts; what was saved from it is kept")
	a.waitFor("playlist · not saving · 3 saved earlier")
	if len(a.savedPlaylistIDs()) != 0 || !a.hasDetails("b") {
		t.Errorf("n should stop saving and keep the videos")
	}
	if _, err := os.Stat(opts.RemovedDir); !os.IsNotExist(err) {
		t.Errorf("keeping should write no copy: %v", err)
	}
	// Not saved now: re-reading changes nothing and looks nothing up.
	calls := len(f.callList())
	a.press("I")
	a.waitFor("no playlists are saved yet")
	a.press("enter")
	a.waitFor("Episode A")
	if len(f.callList()) != calls {
		t.Errorf("an unsaved playlist was looked up: %v", f.callList())
	}
	a.quit()
}

// If the copy cannot be written, nothing is deleted.
func TestNothingIsDeletedWithoutACopy(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	os.WriteFile(notADir, nil, 0o644)
	opts := archOptions(newFakeArchiver())
	opts.RemovedDir = notADir
	a := startWith(t, defaultSource(), savingDB(t), opts)
	a.waitFor("Podcasts")
	a.press("I")
	a.waitFor("saved details for every video in 1 playlist(s)")
	a.press("p")
	a.waitFor("saving details (3 saved)")
	a.press("s")
	a.waitFor("also delete the 3 video(s)")
	a.press("y")
	a.waitFor("nothing deleted, as a copy could not be kept first")
	if !a.hasDetails("b") || len(a.savedPlaylistIDs()) != 1 {
		t.Error("a failed copy must leave everything as it was")
	}
	a.quit()
}

// A lookup that finishes after its video was deleted must not bring it back.
func TestALookupFinishingAfterADeleteIsDropped(t *testing.T) {
	f := newFakeArchiver()
	f.hold = make(chan struct{})
	opts := archOptions(f)
	opts.RemovedDir = filepath.Join(t.TempDir(), "removed")
	a := startWith(t, defaultSource(), nil, opts)
	a.waitFor("Podcasts")
	a.press("s")
	a.eventually("the first lookup started", func() bool { return len(f.callList()) == 1 })
	a.waitFor("saving details (3 saved)")
	a.press("s")
	a.waitFor("also delete the 3 video(s)")
	a.press("y")
	a.waitFor("deleted 3 video(s)")
	f.mu.Lock()
	close(f.hold)
	f.hold = nil
	f.mu.Unlock()
	a.eventually("the held lookup finished", func() bool {
		var idle bool
		a.inspect(func(m *Model) { idle = m.arch.active == nil })
		return idle
	})
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := a.video(id); ok {
			t.Errorf("%s came back after its playlist was deleted", id)
		}
	}
	if s := a.screen(); !strings.Contains(s, "deleted 3 video(s)") {
		t.Errorf("the late lookup replaced the delete message:\n%s", s)
	}
	if n := len(f.callList()); n != 1 {
		t.Errorf("queued lookups ran after the delete: %v", f.callList())
	}
	a.quit()
}
