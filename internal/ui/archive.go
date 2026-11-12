package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobertStevenson/eartube/internal/archive"
	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// The archive: every video seen in one of your playlists is remembered, and
// its full details are looked up once in the background, so a video that later
// goes private or is deleted can still be identified (i).

// archState tracks the background lookups of full details.
type archState struct {
	queue   []ytdlp.Item // waiting, one lookup at a time
	active  *ytdlp.Item
	pending map[string]bool // queued or active
	failed  map[string]bool // failed this session; not retried until restart
	ctx     context.Context
	cancel  context.CancelFunc

	// The sweep through every playlist (I).
	sweeping  bool
	toRead    []ytdlp.Item // playlists still to be listed
	read      int          // playlists listed so far
	readFails int
}

func newArchState() archState {
	ctx, cancel := context.WithCancel(context.Background())
	return archState{pending: map[string]bool{}, failed: map[string]bool{}, ctx: ctx, cancel: cancel}
}

// infoState is the details view (i).
type infoState struct {
	open   bool
	offset int
	it     ytdlp.Item  // the video as the list showed it
	v      store.Video // what is saved
	saved  bool        // anything is saved about it
}

type archDoneMsg struct {
	item ytdlp.Item
	v    store.Video
	err  error
}

// sweepListMsg carries your playlists, the first step of the sweep (I).
type sweepListMsg struct {
	playlists []ytdlp.Item
	err       error
}

// sweepPlaylistMsg carries one playlist's videos during the sweep.
type sweepPlaylistMsg struct {
	pl    ytdlp.Item
	items []ytdlp.Item
	err   error
}

// recordPlaylist saves what a playlist listing showed, queues full-detail
// lookups for videos that have none yet, and returns the items with each
// unavailable video's saved title, channel and length filled in.
func (m *Model) recordPlaylist(pl ytdlp.Item, items []ytdlp.Item) ([]ytdlp.Item, tea.Cmd) {
	seen := make([]store.Sighting, 0, len(items))
	ids := make([]string, 0, len(items))
	for _, it := range items {
		if it.ID == "" {
			continue
		}
		s := store.Sighting{ID: it.ID, Gone: it.Unavailable}
		if !it.Unavailable {
			s.Title, s.Channel, s.Duration = it.Title, it.Channel, it.Duration
		}
		seen = append(seen, s)
		ids = append(ids, it.ID)
	}
	if err := m.db.RecordPlaylist(pl.ID, pl.Title, seen, time.Now()); err != nil {
		m.status = "could not save video details: " + err.Error()
		return items, nil
	}
	saved, err := m.db.Videos(ids)
	if err != nil {
		m.status = "could not read saved details: " + err.Error()
		return items, nil
	}
	out := make([]ytdlp.Item, len(items))
	var need []ytdlp.Item
	for i, it := range items {
		v, ok := saved[it.ID]
		switch {
		case it.Unavailable && ok && v.Title != "":
			it.Title, it.Channel, it.Duration = v.Title, v.Channel, v.Duration
		case !it.Unavailable && (!ok || !v.HasDetails()):
			need = append(need, it)
		}
		out[i] = it
	}
	return out, m.queueDetails(need, false)
}

// queueDetails adds videos to the lookup queue (at the front if urgent).
// Videos already queued, or that failed this session, are skipped.
func (m *Model) queueDetails(items []ytdlp.Item, urgent bool) tea.Cmd {
	if m.opts.Archiver == nil {
		return nil
	}
	var add []ytdlp.Item
	for _, it := range items {
		if it.Unavailable || m.arch.failed[it.ID] {
			continue
		}
		if m.arch.pending[it.ID] {
			if urgent && (m.arch.active == nil || m.arch.active.ID != it.ID) {
				m.dropQueued(it.ID) // re-added at the front below
			} else {
				continue
			}
		}
		add = append(add, it)
		m.arch.pending[it.ID] = true
	}
	if urgent {
		m.arch.queue = append(add, m.arch.queue...)
	} else {
		m.arch.queue = append(m.arch.queue, add...)
	}
	return m.startNextDetails()
}

func (m *Model) dropQueued(id string) {
	q := m.arch.queue[:0]
	for _, it := range m.arch.queue {
		if it.ID != id {
			q = append(q, it)
		}
	}
	m.arch.queue = q
}

// startNextDetails starts the next lookup, after a short pause so a long
// sweep stays gentle on YouTube.
func (m *Model) startNextDetails() tea.Cmd {
	if m.arch.active != nil || len(m.arch.queue) == 0 {
		return nil
	}
	it := m.arch.queue[0]
	m.arch.queue = m.arch.queue[1:]
	m.arch.active = &it
	a, ctx, pause := m.opts.Archiver, m.arch.ctx, m.opts.ArchivePause
	return func() tea.Msg {
		if pause > 0 {
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				return archDoneMsg{item: it, err: ctx.Err()}
			}
		}
		lctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		v, err := a.Fetch(lctx, it)
		return archDoneMsg{item: it, v: v, err: err}
	}
}

func (m *Model) onDetailsDone(msg archDoneMsg) tea.Cmd {
	m.arch.active = nil
	delete(m.arch.pending, msg.item.ID)
	if msg.err != nil {
		if m.arch.ctx.Err() != nil {
			return nil // quitting
		}
		m.arch.failed[msg.item.ID] = true
	} else if err := m.db.SaveDetails(msg.v, time.Now()); err != nil {
		m.status = "could not save video details: " + err.Error()
	}
	if m.info.open && m.info.it.ID == msg.item.ID {
		m.loadInfo(m.info.it)
	}
	m.sweepStatus()
	return m.startNextDetails()
}

// archiveStatus is the short note for the now-playing line.
func (m Model) archiveStatus() string {
	if m.arch.active == nil {
		return ""
	}
	return fmt.Sprintf("saving video details (%d left)", len(m.arch.queue)+1)
}

// --- the sweep through every playlist (I) -----------------------------------

func (m *Model) startSweep() tea.Cmd {
	if m.opts.Archiver == nil {
		m.status = "saving video details is not available"
		return nil
	}
	if m.arch.sweeping {
		m.status = "already reading your playlists"
		return nil
	}
	m.arch.sweeping, m.arch.read, m.arch.readFails, m.arch.toRead = true, 0, 0, nil
	m.status = "reading your playlists to save every video's details…"
	src := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		pls, err := src.Playlists(ctx)
		return sweepListMsg{playlists: pls, err: err}
	}
}

func (m *Model) onSweepList(msg sweepListMsg) tea.Cmd {
	if msg.err != nil {
		m.arch.sweeping = false
		m.status = "could not list your playlists: " + msg.err.Error()
		return nil
	}
	m.arch.toRead = msg.playlists
	return m.readNextPlaylist()
}

func (m *Model) readNextPlaylist() tea.Cmd {
	if len(m.arch.toRead) == 0 {
		m.arch.sweeping = false
		m.sweepStatus()
		return nil
	}
	pl := m.arch.toRead[0]
	m.arch.toRead = m.arch.toRead[1:]
	m.sweepStatus()
	src := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		items, err := src.Playlist(ctx, pl.URL)
		return sweepPlaylistMsg{pl: pl, items: items, err: err}
	}
}

func (m *Model) onSweepPlaylist(msg sweepPlaylistMsg) tea.Cmd {
	m.arch.read++
	var cmd tea.Cmd
	if msg.err != nil {
		m.arch.readFails++
	} else {
		_, cmd = m.recordPlaylist(msg.pl, msg.items)
	}
	return tea.Batch(cmd, m.readNextPlaylist())
}

// sweepStatus reports the sweep's progress in the status line while it runs,
// and once when it and the lookups it queued are finished.
func (m *Model) sweepStatus() {
	switch {
	case m.arch.sweeping:
		m.status = fmt.Sprintf("reading your playlists: %d of %d", m.arch.read, m.arch.read+len(m.arch.toRead)+1)
	case m.arch.read > 0 && m.arch.active == nil && len(m.arch.queue) == 0:
		m.status = fmt.Sprintf("saved details for every video in %d playlist(s)", m.arch.read-m.arch.readFails)
		if m.arch.readFails > 0 {
			m.status += fmt.Sprintf(" (%d could not be read)", m.arch.readFails)
		}
		if n := len(m.arch.failed); n > 0 {
			m.status += fmt.Sprintf(" · %d video(s) could not be looked up", n)
		}
		m.arch.read = 0
	case m.arch.read > 0:
		m.status = fmt.Sprintf("read %d playlist(s); saving video details in the background", m.arch.read)
	}
}

// --- the details view (i) ---------------------------------------------------

// openInfo shows what is saved about the selected video.
func (m *Model) openInfo() tea.Cmd {
	e, ok := m.list.SelectedItem().(entry)
	if !ok || e.isPlaylist {
		m.status = "i shows the saved details of a video: select one first"
		return nil
	}
	m.help.open, m.set.open = false, false
	m.info = infoState{open: true}
	m.loadInfo(e.it)
	m.status = "details: ↑/↓ scroll, esc closes"
	if !e.it.Unavailable && !m.info.v.HasDetails() && m.inArchive() {
		return m.queueDetails([]ytdlp.Item{e.it}, true)
	}
	return nil
}

// inArchive reports whether the selected video was seen in a playlist (so
// its details are wanted), as opposed to only in search results.
func (m *Model) inArchive() bool { return m.info.saved && len(m.info.v.Playlists) > 0 }

func (m *Model) loadInfo(it ytdlp.Item) {
	m.info.it = it
	v, ok, err := m.db.Video(it.ID)
	if err != nil {
		m.status = "could not read saved details: " + err.Error()
	}
	m.info.v, m.info.saved = v, ok
}

// infoKey handles keys while the details view is open. Playback keys pass
// through; everything else is swallowed so it cannot act on the hidden list.
func (m *Model) infoKey(key string) (handled bool, cmd tea.Cmd) {
	if !m.info.open {
		return false, nil
	}
	page := max(m.helpWindow()-1, 1)
	switch key {
	case "esc", "i":
		m.info.open = false
		m.status = ""
		return true, nil
	case "up", "k":
		m.info.offset--
	case "down", "j":
		m.info.offset++
	case "pgup":
		m.info.offset -= page
	case "pgdown":
		m.info.offset += page
	case "home", "g":
		m.info.offset = 0
	case "end", "G":
		m.info.offset = 1 << 30
	case "ctrl+c", "q", " ", "left", "right", "[", "]", "n", "b":
		return false, nil
	default:
		return true, nil
	}
	m.info.offset = min(max(m.info.offset, 0), m.infoMaxOffset())
	return true, nil
}

// infoLines is the details view's content, wrapped to the terminal width.
func (m Model) infoLines() []string {
	it, v := m.info.it, m.info.v
	title := v.Title
	if title == "" {
		title = it.Title
	}
	var lines []string
	add := func(s string) { lines = append(lines, s) }
	add(bold.Render(title))
	switch {
	case it.Unavailable && !m.info.saved || it.Unavailable && v.Title == "":
		add("")
		add("YouTube no longer serves this video, and it was already gone the first")
		add("time eartube saw it, so its title and details were never saved.")
		add("")
		add("Link: " + archive.URL(it.ID))
		if len(v.Playlists) > 0 {
			add("Playlists: " + strings.Join(v.Playlists, ", "))
		}
		return m.wrap(lines)
	case !m.info.saved:
		add("")
		add("Nothing saved: only videos in your playlists are saved.")
		if it.Channel != "" {
			add("Channel: " + it.Channel)
		}
		add("Link: " + archive.URL(it.ID))
		return m.wrap(lines)
	}
	if it.Unavailable {
		add("This video is no longer on YouTube. This is what eartube saved.")
	}
	add("")
	for _, l := range archive.Lines(v) {
		lines = append(lines, m.labelled(l[0], l[1])...)
	}
	if m.arch.pending[it.ID] {
		add("")
		add(dim.Render("looking up the full details…"))
	} else if m.arch.failed[it.ID] {
		add("")
		add(dim.Render("the full details could not be looked up; eartube will try again next time"))
	}
	if v.Description != "" {
		add("")
		add(bold.Render("Description"))
		for _, l := range strings.Split(strings.TrimRight(v.Description, "\n"), "\n") {
			add(l)
		}
	}
	return m.wrap(lines)
}

// labelWidth is the column where values start in the details view.
const labelWidth = 14

// labelled renders "Label:  value", wrapping the value so continuation lines
// stay under the value rather than running back under the label.
func (m Model) labelled(label, value string) []string {
	if m.w <= labelWidth+10 {
		return []string{fmt.Sprintf("%-*s%s", labelWidth, label+":", value)}
	}
	wrapped := strings.Split(lipgloss.NewStyle().Width(m.w-labelWidth).Render(value), "\n")
	out := make([]string, len(wrapped))
	for i, w := range wrapped {
		prefix := strings.Repeat(" ", labelWidth)
		if i == 0 {
			prefix = fmt.Sprintf("%-*s", labelWidth, label+":")
		}
		out[i] = prefix + strings.TrimRight(w, " ")
	}
	return out
}

// wrap breaks long lines at the terminal width (unknown width: no wrapping).
func (m Model) wrap(lines []string) []string {
	if m.w <= 0 {
		return lines
	}
	st := lipgloss.NewStyle().Width(m.w)
	var out []string
	for _, l := range lines {
		if l == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(st.Render(l), "\n")...)
	}
	return out
}

func (m Model) infoMaxOffset() int {
	w := m.helpWindow()
	if w == 0 {
		return 0
	}
	return max(len(m.infoLines())-w, 0)
}

// infoPanel renders the details view, scrolled and clipped to fit.
func (m Model) infoPanel() string {
	lines := m.infoLines()
	win, off := m.helpWindow(), min(m.info.offset, m.infoMaxOffset())
	hint := ""
	if win > 0 && len(lines) > win {
		switch {
		case off > 0 && off < m.infoMaxOffset():
			hint = "↑ ↓ more"
		case off > 0:
			hint = "↑ more above"
		default:
			hint = "↓ more below"
		}
		lines = lines[off : off+win]
	}
	out := []string{bold.Render("Video details") + dim.Render("   ↑/↓ scroll · esc closes")}
	out = append(out, lines...)
	if hint != "" {
		out = append(out, dim.Render(hint))
	}
	return clipWidth(strings.Join(out, "\n"), m.w)
}
