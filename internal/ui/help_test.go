package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// caseKeys returns every string literal used as a case in the named functions
// of the given source files: the keys the program actually handles.
func caseKeys(t *testing.T, files map[string][]string) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for file, funcs := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, fn := range funcs {
			found := false
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Name.Name != fn {
					continue
				}
				found = true
				ast.Inspect(fd, func(n ast.Node) bool {
					if cc, ok := n.(*ast.CaseClause); ok {
						for _, e := range cc.List {
							if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								if s, err := strconv.Unquote(lit.Value); err == nil {
									keys[s] = true
								}
							}
						}
					}
					return true
				})
			}
			if !found {
				t.Fatalf("function %s not found in %s; update this test if it moved", fn, file)
			}
		}
	}
	return keys
}

func documentedKeys() map[string]bool {
	keys := map[string]bool{}
	for _, g := range keyGroups {
		for _, d := range g.docs {
			for _, k := range d.keys {
				keys[k] = true
			}
		}
	}
	return keys
}

func names(m map[string]bool) []string {
	var out []string
	for k := range m {
		if k == " " {
			k = "space"
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A key that works but is not in the help would be invisible to users, so the
// help must list exactly the keys the code handles.
func TestEveryKeyIsDocumented(t *testing.T) {
	code := caseKeys(t, map[string][]string{
		"ui.go":       {"update"},
		"settings.go": {"settingsKey"},
		"help.go":     {"helpKey"},
	})
	docs := documentedKeys()

	var undocumented, phantom []string
	for k := range code {
		if !docs[k] {
			undocumented = append(undocumented, k)
		}
	}
	for k := range docs {
		if !code[k] {
			phantom = append(phantom, k)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(phantom)
	if len(undocumented) > 0 {
		t.Errorf("keys handled in the code but missing from keyGroups (help.go): %q", undocumented)
	}
	if len(phantom) > 0 {
		t.Errorf("keys documented in keyGroups but not handled anywhere in the code: %q", phantom)
	}
	if len(code) < 20 {
		t.Errorf("only found %d keys (%v): the source scan is probably broken", len(code), names(code))
	}
}

func TestKeyDocsAreWellFormed(t *testing.T) {
	seen := map[string]string{}
	for _, g := range keyGroups {
		if g.title == "" || len(g.docs) == 0 {
			t.Errorf("group %+v is empty", g)
		}
		for _, d := range g.docs {
			if d.label == "" || d.desc == "" || len(d.keys) == 0 {
				t.Errorf("incomplete entry in %q: %+v", g.title, d)
			}
			if len(d.desc) > 64 {
				t.Errorf("%q description is %d chars; keep it short enough for a narrow terminal: %q", d.label, len(d.desc), d.desc)
			}
			for _, k := range d.keys {
				if prev, dup := seen[k]; dup {
					t.Errorf("key %q is documented twice (%q and %q)", k, prev, g.title)
				}
				seen[k] = g.title
			}
		}
	}
}

// --- behaviour -----------------------------------------------------------------

func TestQuestionMarkOpensTheKeyReference(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("?")
	a.waitFor("Keys")
	a.waitFor("Playing") // the first groups; the rest are a scroll away in this 30-row terminal
	a.waitFor("Queue")
	a.waitFor("pause or resume")
	a.waitFor("↓ more below")
	a.waitGone("Podcasts") // it replaces the list while open
	a.quit()
}

func TestHelpClosesWithEscOrQuestionMark(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("?")
	a.waitFor("Keys")
	a.press("esc")
	a.waitFor("Podcasts")
	a.waitGone("pause or resume")
	a.press("?")
	a.waitFor("pause or resume")
	a.press("?")
	a.waitFor("Podcasts")
	a.quit()
}

func TestHelpSaysWhenDownloadsAreOff(t *testing.T) {
	off := start(t, defaultSource(), nil)
	off.waitFor("Podcasts")
	off.press("?")
	off.waitFor("(off: set downloads = true to enable)")
	off.quit()

	on := startWith(t, defaultSource(), nil, dlOptions(newFakeDownloader(t)))
	on.waitFor("Podcasts")
	on.press("?")
	on.waitFor("Downloads")
	if strings.Contains(on.screen(), "(off:") {
		t.Fatalf("told a user with downloads on that they are off:\n%s", on.screen())
	}
	on.quit()
}

func TestTheFooterIsShortAndPointsToHelp(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("? all keys")
	a.waitFor("space pause")
	if strings.Contains(a.screen(), "K/J move") {
		t.Fatalf("the long key list is still in the footer:\n%s", a.screen())
	}
	var m Model
	a.inspect(func(mm *Model) { m = *mm })
	if h := m.helpHeight(); h != 1 {
		t.Errorf("footer takes %d lines at 120 columns, want 1", h)
	}
	a.quit()
}

func TestPlaybackKeysWorkWithHelpOpen(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("?")
	a.waitFor("Keys")
	a.press(" ", "left", "right", "[", "]")
	a.eventually("controls forwarded", func() bool {
		a.pl.mu.Lock()
		defer a.pl.mu.Unlock()
		return a.pl.pauses == 1 && len(a.pl.seeks) == 2 && len(a.pl.speeds) == 2
	})
	a.waitFor("Keys") // still open
	a.quit()
}

func TestOtherKeysCannotActOnTheListBehindHelp(t *testing.T) {
	f := newFakeDownloader(t)
	a := startWith(t, defaultSource(), nil, dlOptions(f))
	a.openPlaylist()
	a.press("?")
	a.waitFor("Keys")
	a.press("enter", "a", "A", "d", "D", "X", "m", "o", "p", "tab", "x", "K", "J", "/", "e")
	a.waitFor("Keys")
	a.press("esc")
	a.waitFor("Episode A")
	if n := len(a.pl.loaded()); n != 0 {
		t.Fatalf("%d track(s) started from the hidden list", n)
	}
	if n := len(f.callList()); n != 0 {
		t.Fatalf("%d download(s) started from the hidden list", n)
	}
	if s := a.screen(); strings.Contains(s, "✓") || strings.Contains(s, "↓ downloaded") {
		t.Fatalf("a hidden-list key took effect:\n%s", s)
	}
	a.quit()
}

func TestQuestionMarkInTheSearchBoxIsJustText(t *testing.T) {
	a := start(t, defaultSource(), nil)
	a.waitFor("Podcasts")
	a.press("/")
	a.typeText("why?")
	a.waitFor("search: why?")
	if strings.Contains(a.screen(), "pause or resume") {
		t.Fatal("typing ? in the search box opened the help")
	}
	a.press("esc")
	a.quit()
}

func TestHelpAndSettingsReplaceEachOther(t *testing.T) {
	o, _ := settingsOptions(t, &fakeEditor{}, nil)
	a := startWith(t, defaultSource(), nil, o)
	a.waitFor("Podcasts")
	a.press("?")
	a.waitFor("pause or resume")
	a.press("c") // from help straight to settings
	a.waitFor("Config file")
	a.waitGone("pause or resume")
	a.press("?") // and back
	a.waitFor("pause or resume")
	a.waitGone("Config file")
	a.press("esc")
	a.waitFor("Podcasts")
	a.quit()
}

// --- scrolling ------------------------------------------------------------------

// shortHelp is a model in a terminal too short for the whole reference.
func shortHelp(t *testing.T, w, h int) Model {
	t.Helper()
	m := New(newFakePlayer(), newDB(t), defaultSource(), Options{})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	return nm.(Model)
}

func press(m Model, keys ...tea.KeyMsg) Model {
	var nm tea.Model = m
	for _, k := range keys {
		nm, _ = nm.Update(k)
	}
	return nm.(Model)
}

func rn(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

func TestHelpScrollsWhenTheTerminalIsShort(t *testing.T) {
	m := shortHelp(t, 100, 14)
	top := m.View()
	if !strings.Contains(top, "Playing") || strings.Contains(top, "General") {
		t.Fatalf("a short terminal should show the start, not everything:\n%s", top)
	}
	if !strings.Contains(top, "↓ more below") {
		t.Errorf("no hint that there is more:\n%s", top)
	}

	m = press(m, tea.KeyMsg{Type: tea.KeyEnd})
	bottom := m.View()
	if !strings.Contains(bottom, "General") || strings.Contains(bottom, "↓ more below") {
		t.Fatalf("End should reach the last group with no 'more below' hint:\n%s", bottom)
	}
	if !strings.Contains(bottom, "↑ more above") {
		t.Errorf("no hint that there is more above:\n%s", bottom)
	}

	m = press(m, tea.KeyMsg{Type: tea.KeyHome})
	if v := m.View(); !strings.Contains(v, "Playing") {
		t.Fatalf("Home should return to the top:\n%s", v)
	}
}

func TestHelpScrollIsClampedAtBothEnds(t *testing.T) {
	m := shortHelp(t, 100, 14)
	m = press(m, tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyUp}, rn("k"))
	if m.help.offset != 0 {
		t.Errorf("scrolled above the top: offset %d", m.help.offset)
	}
	for i := 0; i < 80; i++ {
		m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.help.offset != m.helpMaxOffset() || m.help.offset == 0 {
		t.Errorf("scrolled past the end: offset %d, max %d", m.help.offset, m.helpMaxOffset())
	}
	if v := m.View(); !strings.Contains(v, "General") {
		t.Errorf("the last group should be visible at the bottom:\n%s", v)
	}
}

func TestPageKeysMoveByAScreen(t *testing.T) {
	m := shortHelp(t, 100, 14)
	m = press(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.help.offset < 3 {
		t.Errorf("page down moved only %d lines", m.help.offset)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.help.offset != 0 {
		t.Errorf("page up did not come back: offset %d", m.help.offset)
	}
}

func TestEveryGroupAndKeyIsShownWhenThereIsRoom(t *testing.T) {
	m := shortHelp(t, 100, 60)
	v := m.View()
	for _, g := range keyGroups {
		if !strings.Contains(v, g.title) {
			t.Errorf("group %q missing from the help", g.title)
		}
		for _, d := range g.docs {
			if !strings.Contains(v, d.desc) {
				t.Errorf("%q (%s) missing from the help", d.desc, g.title)
			}
		}
	}
	if !strings.Contains(v, "General") || strings.Contains(v, "more below") || strings.Contains(v, "more above") {
		t.Fatalf("a tall terminal should show the whole reference with no scroll hints:\n%s", v)
	}
}

func TestHelpReopensAtTheTop(t *testing.T) {
	m := shortHelp(t, 100, 14)
	m = press(m, tea.KeyMsg{Type: tea.KeyEnd}, tea.KeyMsg{Type: tea.KeyEsc}, rn("?"))
	if m.help.offset != 0 {
		t.Errorf("reopened at offset %d, want the top", m.help.offset)
	}
}

func TestHelpWithAnUnknownTerminalSizeShowsEverything(t *testing.T) {
	m := New(newFakePlayer(), newDB(t), defaultSource(), Options{}) // no WindowSizeMsg yet
	nm, _ := m.Update(rn("?"))
	if v := nm.(Model).View(); !strings.Contains(v, "General") {
		t.Fatalf("with no size known, show everything:\n%s", v)
	}
}

// Nothing may wrap or overflow at any size, however narrow or short.
func TestHelpFitsEveryTerminalSize(t *testing.T) {
	for _, down := range []bool{false, true} {
		for _, size := range [][2]int{{200, 50}, {120, 30}, {100, 24}, {80, 24}, {60, 20}, {50, 12}, {40, 10}} {
			w, h := size[0], size[1]
			m := New(newFakePlayer(), newDB(t), defaultSource(), Options{Downloads: down})
			nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			nm, _ = nm.Update(rn("?"))
			for _, scroll := range []tea.KeyMsg{{Type: tea.KeyHome}, {Type: tea.KeyEnd}} {
				nm, _ = nm.Update(scroll)
				if lines := renderedHeight(nm.(Model).View(), w); lines > h {
					t.Errorf("help (downloads=%v) at %dx%d is %d rows, taller than the terminal", down, w, h, lines)
				}
			}
		}
	}
}
