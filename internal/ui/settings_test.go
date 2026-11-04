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

// fakeEditor stands in for the user's editor: it records being opened and can
// change the file the way saving in a real editor would.
type fakeEditor struct {
	mu    sync.Mutex
	calls []string
	edit  func(path string)
	err   error
}

func (f *fakeEditor) Edit(path string) tea.Cmd {
	return func() tea.Msg {
		f.mu.Lock()
		f.calls = append(f.calls, path)
		f.mu.Unlock()
		if f.edit != nil {
			f.edit(path)
		}
		return EditorDoneMsg{Err: f.err}
	}
}

func (f *fakeEditor) opened() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

var sampleRows = []config.Row{
	{Key: "browser", Value: "brave+gnomekeyring", Source: "config file"},
	{Key: "downloads", Value: "off", Source: "default"},
	{Key: "download_max_gb", Value: "5 GiB", Source: "default"},
	{Key: "delete_played", Value: "on", Source: "env EARTUBE_DELETE_PLAYED"},
	{Key: "download_dir", Value: "/data/eartube/downloads", Source: "flag --download-dir"},
}

// settingsOptions wires the settings view to a temp config path, the real
// EnsureFile, and a fake editor.
func settingsOptions(t *testing.T, ed *fakeEditor, validate func() error) (Options, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cfg", "config.toml")
	return Options{Settings: Settings{
		Path:     path,
		Rows:     sampleRows,
		Ensure:   func() (bool, error) { return config.EnsureFile(path) },
		Edit:     ed.Edit,
		Validate: validate,
	}}, path
}

func TestSettingsViewShowsEverySettingWithItsSource(t *testing.T) {
	ed := &fakeEditor{}
	o, path := settingsOptions(t, ed, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Settings")
	a.waitFor(path)
	for _, r := range sampleRows {
		a.waitFor(r.Key)
		a.waitFor(r.Value)
		a.waitFor(r.Source)
	}
	a.waitFor("restart eartube")
	a.waitFor("flag, environment")
	a.quit()
}

func TestSettingsViewOpensAndCloses(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Settings")
	a.waitGone("Podcasts")
	a.press("esc")
	a.waitFor("Podcasts")
	a.waitGone("download_max_gb")

	a.press("c") // c toggles it too
	a.waitFor("download_max_gb")
	a.press("c")
	a.waitGone("download_max_gb")
	a.quit()
}

func TestSettingsViewSaysWhenThereIsNoConfigFileYet(t *testing.T) {
	o, path := settingsOptions(t, &fakeEditor{}, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("not created yet: press e to create it")
	a.press("e")
	a.eventually("starter file created", func() bool { _, err := os.Stat(path); return err == nil })
	a.waitGone("not created yet")
	a.quit()
}

// --- editing -------------------------------------------------------------------

func TestEditCreatesTheStarterFileAndOpensTheEditor(t *testing.T) {
	ed := &fakeEditor{}
	o, path := settingsOptions(t, ed, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.eventually("editor opened", func() bool { return len(ed.opened()) == 1 })
	if ed.opened()[0] != path {
		t.Fatalf("opened %q, want %q", ed.opened()[0], path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != config.Template() {
		t.Fatalf("the starter file was not created from the template (err %v)", err)
	}
	a.waitFor("created with commented defaults")
	a.quit()
}

func TestEditNeverOverwritesAnExistingConfig(t *testing.T) {
	ed := &fakeEditor{}
	o, path := settingsOptions(t, ed, nil)
	mine := "# my notes\nbrowser = \"firefox\"\n"
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(mine), 0o644)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.eventually("editor opened", func() bool { return len(ed.opened()) == 1 })
	if got, _ := os.ReadFile(path); string(got) != mine {
		t.Fatalf("the user's config was changed:\n%s", got)
	}
	a.quit()
}

func TestSavingAChangeSaysToRestart(t *testing.T) {
	ed := &fakeEditor{edit: func(path string) {
		os.WriteFile(path, []byte("downloads = true\n# edited\n"), 0o644)
	}}
	o, _ := settingsOptions(t, ed, func() error { return nil })
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.waitFor("config saved: restart eartube to apply the changes")
	a.quit()
}

func TestClosingTheEditorWithoutChangesSaysSo(t *testing.T) {
	ed := &fakeEditor{} // the editor opens and closes without touching the file
	o, path := settingsOptions(t, ed, func() error { return nil })
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("downloads = true\n"), 0o644) // already exists, so it is not "newly created"
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.waitFor("config unchanged")
	a.quit()
}

func TestABrokenConfigIsReportedAtOnce(t *testing.T) {
	ed := &fakeEditor{edit: func(path string) { os.WriteFile(path, []byte("downlods = true\n"), 0o644) }}
	o, _ := settingsOptions(t, ed, func() error { return errors.New("unknown setting(s): downlods") })
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.waitFor("config has a problem, fix it before restarting")
	a.waitFor("downlods")
	a.quit()
}

func TestEditorFailureIsReported(t *testing.T) {
	ed := &fakeEditor{err: errors.New("$EDITOR is \"nope\", which is not installed")}
	o, _ := settingsOptions(t, ed, func() error { return nil })
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.waitFor("editor: $EDITOR is")
	a.quit()
}

func TestWhenTheFileCannotBeCreatedTheEditorIsNotOpened(t *testing.T) {
	ed := &fakeEditor{}
	o, _ := settingsOptions(t, ed, nil)
	o.Settings.Ensure = func() (bool, error) { return false, errors.New("permission denied") }
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c", "e")
	a.waitFor("could not create the config file: permission denied")
	if n := len(ed.opened()); n != 0 {
		t.Fatalf("editor opened %d time(s) although there is no file", n)
	}
	a.quit()
}

func TestEditingIsUnavailableWithoutAnEditorHook(t *testing.T) {
	a := start(t, defaultSource(), nil) // zero Options
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("The configuration is not available")
	a.press("e")
	a.waitFor("not available here")
	a.quit()
}

func TestEditKeyDoesNothingOutsideTheSettingsView(t *testing.T) {
	ed := &fakeEditor{}
	o, path := settingsOptions(t, ed, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("e") // not in settings
	a.press("c", "esc", "e")
	if n := len(ed.opened()); n != 0 {
		t.Fatalf("editor opened %d time(s) outside the settings view", n)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a config file was created without asking")
	}
	a.quit()
}

// --- keys ---------------------------------------------------------------------

func TestPlaybackKeysStillWorkWithSettingsOpen(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Settings")
	a.press(" ", "left", "right", "[", "]")
	a.eventually("controls forwarded", func() bool {
		a.pl.mu.Lock()
		defer a.pl.mu.Unlock()
		return a.pl.pauses == 1 && len(a.pl.seeks) == 2 && len(a.pl.speeds) == 2
	})
	a.waitFor("Settings") // and the view is still open
	a.quit()
}

func TestOtherKeysCannotActOnTheHiddenList(t *testing.T) {
	f := newFakeDownloader(t)
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	o.Downloads, o.Downloader = true, f
	a := startWith(t, defaultSource(), nil, o)
	a.openPlaylist() // a video list with a real selection underneath
	a.press("c")
	a.waitFor("Settings")
	a.press("enter", "a", "A", "d", "D", "X", "m", "o", "p", "tab", "down", "K", "J", "x", "/")
	a.waitFor("Settings") // still here
	a.press("esc")
	a.waitFor("Episode A")
	if n := len(a.pl.loaded()); n != 0 {
		t.Fatalf("%d track(s) started from the hidden list", n)
	}
	if n := len(f.callList()); n != 0 {
		t.Fatalf("%d download(s) started from the hidden list", n)
	}
	if strings.Contains(a.screen(), "✓") || strings.Contains(a.screen(), "↓ downloaded") {
		t.Fatalf("a hidden-list key took effect:\n%s", a.screen())
	}
	a.quit()
}

func TestQuitStillWorksWithSettingsOpen(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("c")
	a.waitFor("Settings")
	a.quit() // waits for the program to finish
}

func TestTypingCInTheSearchBoxDoesNotOpenSettings(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("/")
	a.typeText("cccc")
	a.waitFor("search: cccc")
	if strings.Contains(a.screen(), "Settings") {
		t.Fatalf("typing in the search box opened settings:\n%s", a.screen())
	}
	a.press("esc") // leave the search box, or the q in quit() would just be typed into it
	a.quit()
}

func TestHelpMentionsSettings(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("c settings")
	a.quit()
}

func TestSettingsViewFitsSmallTerminals(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	for _, size := range [][2]int{{200, 40}, {120, 30}, {80, 24}, {60, 16}} {
		w, h := size[0], size[1]
		m := New(newFakePlayer(), newDB(t), defaultSource(), o)
		nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
		if lines := strings.Count(nm.(Model).View(), "\n") + 1; lines > h {
			t.Errorf("settings at %dx%d is %d lines, taller than the terminal", w, h, lines)
		}
	}
}
