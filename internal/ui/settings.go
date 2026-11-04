package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobertStevenson/eartube/internal/config"
	"github.com/RobertStevenson/eartube/internal/editor"
)

// Settings describes the configuration for the in-app settings view, and the
// hooks it needs. Hooks are injected so tests never touch a real editor.
type Settings struct {
	Path string       // the config file
	Rows []config.Row // effective settings and where each came from

	// Ensure creates the starter config file if it is missing.
	Ensure func() (created bool, err error)
	// Edit opens the file in an editor; the returned command must yield an
	// EditorDoneMsg when the editor closes. ExecEditor is the real one.
	Edit func(path string) tea.Cmd
	// Validate reports problems in the config as it is now on disk.
	Validate func() error
	// Missing lists settings the config file does not mention at all; nil
	// disables the offer to add them.
	Missing func() ([]string, error)
	// AddMissing appends commented examples for those settings to the file and
	// reports which it added. It must never change existing content.
	AddMissing func() ([]string, error)
}

// dismissedKey is where "don't offer these again" is remembered.
const dismissedKey = "config_hint_dismissed"

// EditorDoneMsg is delivered when the editor closes.
type EditorDoneMsg struct{ Err error }

// ExecEditor suspends the interface, runs the user's editor on path, and
// resumes. It is the production value for Settings.Edit.
func ExecEditor(path string) tea.Cmd {
	cmd, err := editor.Command(path, os.LookupEnv, exec.LookPath)
	if err != nil {
		return func() tea.Msg { return EditorDoneMsg{Err: err} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return EditorDoneMsg{Err: err} })
}

type fileStamp struct {
	mod  time.Time
	size int64
	ok   bool
}

func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mod: st.ModTime(), size: st.Size(), ok: true}
}

// settingsState is the view's own state.
type settingsState struct {
	offer   []string // settings missing from the file that the user has not waved away
	hint    bool     // the launch-time hint is still showing; cleared once settings is opened
	open    bool
	before  fileStamp // the file as it was when the editor was opened
	created bool      // the editor was opened on a freshly created starter file
}

// settingsKey handles keys while the settings view is relevant. handled=false
// means the key goes on to normal handling, which is how playback keys keep
// working with the view open.
func (m *Model) settingsKey(key string) (handled bool, cmd tea.Cmd) {
	if !m.set.open {
		switch key {
		case "c":
			m.set.open, m.help.open = true, false // only one panel at a time
			m.set.hint = false
			m.refreshOffer()
			m.status = "settings: e edits the config file, esc closes"
			return true, nil
		}
		return false, nil
	}
	switch key {
	case "?":
		m.set.open, m.help.open, m.help.offset = false, true, 0
		m.status = "help: ↑/↓ scroll, esc closes"
		return true, nil
	case "esc", "c":
		m.set.open = false
		m.status = ""
		return true, nil
	case "e":
		return true, m.editConfig()
	case "u":
		m.addMissingSettings()
		return true, nil
	case "i":
		m.dismissOffer()
		return true, nil
	case "ctrl+c", "q", " ", "left", "right", "[", "]", "n", "b":
		return false, nil
	}
	return true, nil // anything else would act on the hidden list
}

// refreshOffer recomputes which missing settings to offer: those the config
// file does not mention and the user has not asked us to stop mentioning.
func (m *Model) refreshOffer() {
	m.set.offer = nil
	s := m.opts.Settings
	if s.Missing == nil {
		return
	}
	missing, err := s.Missing()
	if err != nil || len(missing) == 0 {
		return
	}
	var dismissed []string
	_, _ = m.db.LoadJSON(dismissedKey, &dismissed)
	skip := map[string]bool{}
	for _, k := range dismissed {
		skip[k] = true
	}
	for _, k := range missing {
		if !skip[k] {
			m.set.offer = append(m.set.offer, k)
		}
	}
}

func (m *Model) addMissingSettings() {
	s := m.opts.Settings
	if s.AddMissing == nil || len(m.set.offer) == 0 {
		return
	}
	added, err := s.AddMissing()
	if err != nil {
		m.status = "could not update the config file: " + err.Error()
		return
	}
	m.status = fmt.Sprintf("added %d commented setting(s) to your config file, nothing else changed: %s",
		len(added), strings.Join(added, ", "))
	m.set.hint = false
	m.refreshOffer()
}

// dismissOffer remembers the currently offered settings so we stop mentioning
// them. A setting added to eartube later is a new key and is offered again.
func (m *Model) dismissOffer() {
	if len(m.set.offer) == 0 {
		return
	}
	var dismissed []string
	_, _ = m.db.LoadJSON(dismissedKey, &dismissed)
	dismissed = append(dismissed, m.set.offer...)
	if err := m.db.SaveJSON(dismissedKey, dismissed); err != nil {
		m.status = "could not remember that: " + err.Error()
		return
	}
	m.status = "won't offer to add " + strings.Join(m.set.offer, ", ") + " again"
	m.set.hint = false
	m.refreshOffer()
}

func (m *Model) editConfig() tea.Cmd {
	s := m.opts.Settings
	if s.Edit == nil || s.Path == "" {
		m.status = "editing the config file is not available here"
		return nil
	}
	m.set.created = false
	if s.Ensure != nil {
		created, err := s.Ensure()
		if err != nil {
			m.status = "could not create the config file: " + err.Error()
			return nil
		}
		m.set.created = created
	}
	m.set.before = stampOf(s.Path)
	m.status = "opening your editor…"
	return s.Edit(s.Path)
}

// editorClosed reports what happened, including problems in the edited file.
func (m *Model) editorClosed(msg EditorDoneMsg) {
	defer m.refreshOffer() // the editor may have added (or removed) settings
	s := m.opts.Settings
	if msg.Err != nil {
		m.status = "editor: " + msg.Err.Error()
		return
	}
	if s.Validate != nil {
		if err := s.Validate(); err != nil {
			m.status = "config has a problem, fix it before restarting: " + err.Error()
			return
		}
	}
	changed := stampOf(s.Path) != m.set.before
	switch {
	case changed:
		m.status = "config saved: restart eartube to apply the changes"
	case m.set.created:
		m.status = "config file created with commented defaults, unchanged: nothing to apply"
	default:
		m.status = "config unchanged"
	}
}

// settingsPanel renders the view, trimmed to at most height lines.
func (m Model) settingsPanel(height int) string {
	s := m.opts.Settings
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("Settings")
	w("")
	if s.Path == "" {
		w("The configuration is not available.")
	} else {
		w("Config file  %s", s.Path)
		if !stampOf(s.Path).ok {
			w("             (not created yet: press e to create it with commented defaults)")
		}
	}
	if len(m.set.offer) > 0 {
		w("")
		w("Not in your config file yet: %s", strings.Join(m.set.offer, ", "))
		w("  u  add them as commented examples (nothing already in the file is changed)")
		w("  i  don't offer these again")
	}
	w("")
	kw, vw := 0, 0
	for _, r := range s.Rows {
		kw, vw = max(kw, len(r.Key)), max(vw, len(r.Value))
	}
	for _, r := range s.Rows {
		w("  %-*s  %-*s  %s", kw, r.Key, vw, r.Value, dim.Render(r.Source))
	}
	w("")
	w("Where a value comes from, highest first: flag, environment (EARTUBE_*),")
	w("config file, default. Changes take effect after you restart eartube.")
	w("")
	w("e  edit the config file in your editor        esc  close")

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return clipWidth(strings.Join(lines, "\n"), m.w)
}
