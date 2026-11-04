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
}

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
	open    bool
	before  fileStamp // the file as it was when the editor was opened
	created bool      // the editor was opened on a freshly created starter file
}

// settingsKey handles keys while the settings view is relevant. handled=false
// means the key goes on to normal handling, which is how playback keys keep
// working with the view open.
func (m *Model) settingsKey(key string) (handled bool, cmd tea.Cmd) {
	if !m.set.open {
		if key == "c" {
			m.set.open = true
			m.status = "settings: e edits the config file, esc closes"
			return true, nil
		}
		return false, nil
	}
	switch key {
	case "esc", "c":
		m.set.open = false
		m.status = ""
		return true, nil
	case "e":
		return true, m.editConfig()
	case "ctrl+c", "q", " ", "left", "right", "[", "]", "n", "b":
		return false, nil
	}
	return true, nil // anything else would act on the hidden list
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
	return strings.Join(lines, "\n")
}
