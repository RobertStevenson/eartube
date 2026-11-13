package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobertStevenson/eartube/internal/config"
)

// fakeConfigFile stands in for the config file's contents: which settings it
// lacks, and what happens when asked to add them.
type fakeConfigFile struct {
	mu      sync.Mutex
	missing []string
	addErr  error
	adds    int
}

func (f *fakeConfigFile) Missing() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.missing...), nil
}

func (f *fakeConfigFile) AddMissing() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return nil, f.addErr
	}
	f.adds++
	added := f.missing
	f.missing = nil
	return added, nil
}

func (f *fakeConfigFile) addCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.adds }
func (f *fakeConfigFile) set(keys ...string) {
	f.mu.Lock()
	f.missing = keys
	f.mu.Unlock()
}

func offerOptions(t *testing.T, cf *fakeConfigFile) Options {
	t.Helper()
	o, _ := settingsOptions(t, &fakeEditor{}, func() error { return nil })
	o.Settings.Missing, o.Settings.AddMissing = cf.Missing, cf.AddMissing
	return o
}

func TestLaunchHintSaysSettingsAreMissing(t *testing.T) {
	cf := &fakeConfigFile{missing: []string{"download_dir", "delete_played"}}
	a := startWith(t, defaultSource(), nil, offerOptions(t, cf))
	a.waitFor("2 newer setting(s) missing from your config file: press c")
	a.quit()
}

func TestNoHintWhenNothingIsMissing(t *testing.T) {
	a := startWith(t, defaultSource(), nil, offerOptions(t, &fakeConfigFile{}))
	a.waitFor("Podcasts")
	a.waitFor("1 items")
	if strings.Contains(a.screen(), "newer setting") {
		t.Fatalf("hinted although nothing is missing:\n%s", a.screen())
	}
	a.quit()
}

func TestSettingsViewListsWhatIsMissing(t *testing.T) {
	cf := &fakeConfigFile{missing: []string{"download_dir", "delete_played"}}
	a := startWith(t, defaultSource(), nil, offerOptions(t, cf))
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Not in your config file yet: download_dir, delete_played")
	a.waitFor("u  add them as commented examples")
	a.waitFor("i  don't offer these again")
	a.quit()
}

func TestUAddsThemAndTheOfferGoesAway(t *testing.T) {
	cf := &fakeConfigFile{missing: []string{"download_dir", "delete_played"}}
	a := startWith(t, defaultSource(), nil, offerOptions(t, cf))
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Not in your config file yet")
	a.press("u")
	a.waitFor("added 2 commented setting(s) to your config file, nothing else changed: download_dir, delete_played")
	a.waitGone("Not in your config file yet")
	if cf.addCount() != 1 {
		t.Fatalf("AddMissing ran %d times, want once", cf.addCount())
	}
	a.press("u")        // nothing left to add: a second press must be harmless
	a.press("esc", "c") // keys are handled in order, so once this has taken effect the u has been too
	a.waitFor("Config file")
	if cf.addCount() != 1 {
		t.Fatalf("AddMissing ran %d times; the second u had nothing to add", cf.addCount())
	}
	a.quit()
}

func TestAFailedAddIsReportedAndTheOfferStays(t *testing.T) {
	cf := &fakeConfigFile{missing: []string{"download_dir"}, addErr: errors.New("permission denied")}
	a := startWith(t, defaultSource(), nil, offerOptions(t, cf))
	a.waitFor("Podcasts")
	a.press("c", "u")
	a.waitFor("could not update the config file: permission denied")
	a.waitFor("Not in your config file yet: download_dir")
	a.quit()
}

func TestIStopsOfferingAndRemembersAcrossRestarts(t *testing.T) {
	db := newDB(t)
	cf := &fakeConfigFile{missing: []string{"download_dir", "delete_played"}}
	a := startWith(t, defaultSource(), db, offerOptions(t, cf))
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Not in your config file yet")
	a.press("i")
	a.waitFor("won't offer to add download_dir, delete_played again")
	a.waitGone("Not in your config file yet")
	if cf.addCount() != 0 {
		t.Fatal("dismissing must not touch the file")
	}
	a.quit()

	b := startWith(t, defaultSource(), db, offerOptions(t, cf)) // same database, fresh launch
	b.waitFor("1 items")
	if strings.Contains(b.screen(), "newer setting") {
		t.Fatalf("hinted again after being told not to:\n%s", b.screen())
	}
	b.press("c")
	b.waitFor("Config file")
	if strings.Contains(b.screen(), "Not in your config file yet") {
		t.Fatal("offered again after being told not to")
	}
	b.quit()
}

func TestDismissingOnlyCoversTheSettingsThatWereOffered(t *testing.T) {
	db := newDB(t)
	cf := &fakeConfigFile{missing: []string{"download_dir"}}
	a := startWith(t, defaultSource(), db, offerOptions(t, cf))
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Not in your config file yet: download_dir")
	a.press("i")
	a.waitGone("Not in your config file yet")
	a.quit()

	// A later release adds a setting the user has never seen: that one is offered.
	cf.set("download_dir", "brand_new_setting")
	b := startWith(t, defaultSource(), db, offerOptions(t, cf))
	b.waitFor("1 newer setting(s) missing from your config file: press c")
	b.press("c")
	b.waitFor("Not in your config file yet: brand_new_setting")
	if strings.Contains(b.screen(), "download_dir,") || strings.Contains(b.screen(), "yet: download_dir") {
		t.Fatalf("offered a setting that was already waved away:\n%s", b.screen())
	}
	b.quit()
}

func TestOpeningSettingsClearsTheLaunchHint(t *testing.T) {
	cf := &fakeConfigFile{missing: []string{"download_dir"}}
	a := startWith(t, defaultSource(), nil, offerOptions(t, cf))
	a.waitFor("1 newer setting(s) missing")
	a.press("c")
	a.waitFor("Config file")
	a.press("esc")
	a.press("p") // reload the list: the status line is rewritten
	a.waitFor("1 items")
	if strings.Contains(a.screen(), "newer setting") {
		t.Fatalf("the hint keeps nagging after settings was opened:\n%s", a.screen())
	}
	a.quit()
}

func TestAnEditorSessionRefreshesTheOffer(t *testing.T) {
	cf := &fakeConfigFile{missing: []string{"download_dir"}}
	ed := &fakeEditor{edit: func(string) { cf.set() }} // the user added the missing setting by hand
	o, _ := settingsOptions(t, ed, func() error { return nil })
	o.Settings.Missing, o.Settings.AddMissing = cf.Missing, cf.AddMissing
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Not in your config file yet: download_dir")
	a.press("e")
	a.waitGone("Not in your config file yet")
	a.quit()
}

func TestUAndIDoNothingWithoutAnOffer(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil) // no Missing/AddMissing hooks at all
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Config file")
	a.press("u", "i")
	a.waitFor("Config file") // still here, and nothing blew up
	if strings.Contains(a.screen(), "could not") {
		t.Fatalf("an error appeared for a no-op key:\n%s", a.screen())
	}
	a.quit()
}

func TestUAndIOutsideSettingsDoNothing(t *testing.T) {
	db := newDB(t)
	cf := &fakeConfigFile{missing: []string{"download_dir"}}
	a := startWith(t, defaultSource(), db, offerOptions(t, cf))
	a.openPlaylist()
	a.press("u", "i")
	a.press("c", "esc")
	if cf.addCount() != 0 {
		t.Fatal("u added settings from outside the settings view")
	}
	var dismissed []string
	if ok, _ := db.LoadJSON(dismissedKey, &dismissed); ok {
		t.Fatalf("i recorded a dismissal from outside the settings view: %v", dismissed)
	}
	a.quit()
}

func TestOfferNoticeFitsSmallTerminals(t *testing.T) {
	cf := &fakeConfigFile{missing: config.SettingKeys}
	o := offerOptions(t, cf)
	for _, size := range [][2]int{{200, 40}, {120, 30}, {80, 24}, {60, 16}} {
		w, h := size[0], size[1]
		m := New(newFakePlayer(), newDB(t), defaultSource(), o)
		nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
		if lines := renderedHeight(nm.(Model).View(), w); lines > h {
			t.Errorf("settings with the offer at %dx%d is %d rows, taller than the terminal", w, h, lines)
		}
	}
}

// The whole feature with the real file operations: the app's u key appends
// commented examples to a real file and leaves everything in it untouched.
func TestAddingToARealConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg", "config.toml")
	mine := "# my own note\nbrowser = \"brave+gnomekeyring\"\n"
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(mine), 0o644)

	o, _ := settingsOptions(t, &fakeEditor{}, func() error { return nil })
	o.Settings.Path = path
	o.Settings.Missing = func() ([]string, error) { return config.MissingInFile(path) }
	o.Settings.AddMissing = func() ([]string, error) { return config.AddMissing(path) }
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("5 newer setting(s) missing")
	a.press("c")
	a.waitFor("Not in your config file yet: subscriptions_count, downloads, download_max_gb, delete_played, download_dir")
	a.press("u")
	a.waitFor("added 5 commented setting(s)")
	a.waitGone("Not in your config file yet")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), mine) {
		t.Fatalf("the user's own content was altered:\n%s", got)
	}
	for _, k := range []string{"subscriptions_count", "downloads", "download_max_gb", "delete_played", "download_dir"} {
		if !strings.Contains(string(got), "# "+k+" = ") {
			t.Errorf("no commented example for %s was added", k)
		}
	}
	if m, _ := config.MissingInFile(path); len(m) != 0 {
		t.Errorf("still missing %v after adding", m)
	}
	a.quit()
}
