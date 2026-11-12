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

// The archive: every video in a playlist you chose to save (s, in your
// playlists) is remembered, and its full details are looked up once in the
// background, so a video that later goes private or is deleted can still be
// identified (i). No playlist is saved until you choose it.

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

	// forget is the open question after s on a saved playlist: also delete
	// the videos saved only from it? forgetQ is the question, shown in place
	// of the status line so nothing else can replace it while it is open.
	forget  *store.SavedPlaylist
	forgetQ string
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

// sweepPlaylistMsg carries one playlist's videos during the sweep.
type sweepPlaylistMsg struct {
	pl    ytdlp.Item
	items []ytdlp.Item
	err   error
}

// recordPlaylist saves what a playlist listing showed if you save that
// playlist, queues full-detail lookups for videos that have none yet, and
// returns the items with each unavailable video's saved title, channel and
// length filled in (from anything saved earlier, saved playlist or not).
func (m *Model) recordPlaylist(pl ytdlp.Item, items []ytdlp.Item) ([]ytdlp.Item, tea.Cmd) {
	saving, err := m.db.PlaylistIsSaved(pl.ID)
	if err != nil {
		m.status = "could not read which playlists are saved: " + err.Error()
		return items, nil
	}
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
	if saving {
		// Keeps the saved title and link current if the playlist was renamed.
		err := m.db.SetPlaylistSaved(store.SavedPlaylist{ID: pl.ID, Title: pl.Title, URL: pl.URL}, true)
		if err == nil {
			err = m.db.RecordPlaylist(pl.ID, pl.Title, seen, time.Now())
		}
		if err != nil {
			m.status = "could not save video details: " + err.Error()
			return items, nil
		}
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
		case saving && !it.Unavailable && (!ok || !v.HasDetails()):
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
	} else if _, kept, _ := m.db.Video(msg.item.ID); !kept {
		// Deleted (its playlist forgotten) while it was being looked up.
		_ = archive.RemoveThumbnails([]store.Video{msg.v})
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

// --- the sweep through the saved playlists (I) ------------------------------

func (m *Model) startSweep() tea.Cmd {
	if m.opts.Archiver == nil {
		m.status = "saving video details is not available"
		return nil
	}
	pls, err := m.db.SavedPlaylists()
	if err != nil {
		m.status = "could not read which playlists are saved: " + err.Error()
		return nil
	}
	if len(pls) == 0 {
		m.status = "no playlists are saved yet: press p, then s on each one to save"
		return nil
	}
	if m.arch.sweeping {
		m.status = "already reading your saved playlists"
		return nil
	}
	return m.sweep(pls)
}

// sweep reads playlists one after another, saving what each holds. A
// playlist added while a sweep runs joins the end of it.
func (m *Model) sweep(pls []store.SavedPlaylist) tea.Cmd {
	for _, p := range pls {
		m.arch.toRead = append(m.arch.toRead, ytdlp.Item{ID: p.ID, Title: p.Title, URL: p.URL})
	}
	if m.arch.sweeping {
		m.sweepStatus()
		return nil
	}
	m.arch.sweeping, m.arch.read, m.arch.readFails = true, 0, 0
	return m.readNextPlaylist()
}

// --- choosing which playlists to save (s) ------------------------------------

// toggleSaving starts saving the selected playlist, or asks before stopping.
func (m *Model) toggleSaving() tea.Cmd {
	e, ok := m.list.SelectedItem().(entry)
	switch {
	case !ok || !e.isPlaylist || m.showQueue:
		m.status = "s chooses which playlists to save: press p, then s on a playlist"
		return nil
	case m.opts.Archiver == nil:
		m.status = "saving video details is not available"
		return nil
	}
	pl := store.SavedPlaylist{ID: e.it.ID, Title: e.it.Title, URL: e.it.URL}
	if !e.saving {
		if err := m.db.SetPlaylistSaved(pl, true); err != nil {
			m.status = "could not save the choice: " + err.Error()
			return nil
		}
		return tea.Batch(m.sweep([]store.SavedPlaylist{pl}), m.refreshList())
	}
	only, err := m.db.VideosOnlyIn(pl.ID)
	if err != nil {
		m.status = "could not read saved details: " + err.Error()
		return nil
	}
	if len(only) == 0 {
		m.stopSaving(pl)
		m.status = "stopped saving " + pl.Title
		return m.refreshList()
	}
	m.arch.forget = &pl
	m.arch.forgetQ = fmt.Sprintf("stop saving %s: also delete the %d video(s) saved only from it? "+
		"y delete (a copy is kept) · n keep them · esc cancel", pl.Title, len(only))
	return nil
}

// stopSaving unmarks a playlist and drops it from a running sweep.
func (m *Model) stopSaving(pl store.SavedPlaylist) {
	if err := m.db.SetPlaylistSaved(pl, false); err != nil {
		m.status = "could not save the choice: " + err.Error()
	}
	left := m.arch.toRead[:0]
	for _, it := range m.arch.toRead {
		if it.ID != pl.ID {
			left = append(left, it)
		}
	}
	m.arch.toRead = left
}

// forgetKey answers the question asked by toggleSaving. Playback keys pass
// through; everything else is swallowed until it is answered.
func (m *Model) forgetKey(key string) (handled bool, cmd tea.Cmd) {
	pl := m.arch.forget
	if pl == nil {
		return false, nil
	}
	switch key {
	case "y":
		m.arch.forget = nil
		m.forgetPlaylist(*pl)
		return true, m.refreshList()
	case "n":
		m.arch.forget = nil
		m.stopSaving(*pl)
		m.status = "stopped saving " + pl.Title + "; what was saved from it is kept"
		return true, m.refreshList()
	case "esc":
		m.arch.forget = nil
		m.status = "still saving " + pl.Title
		return true, nil
	case "ctrl+c", "q", " ", "left", "right", "[", "]", "b":
		return false, nil
	}
	return true, nil
}

// forgetPlaylist stops saving a playlist and deletes the videos saved only
// from it, after writing a copy of them (with thumbnails) to RemovedDir. If
// the copy cannot be written, nothing is deleted.
func (m *Model) forgetPlaylist(pl store.SavedPlaylist) {
	only, err := m.db.VideosOnlyIn(pl.ID) // again: a lookup may have finished since the question
	if err == nil && m.opts.RemovedDir == "" {
		err = fmt.Errorf("no folder for the copy")
	}
	var folder string
	if err == nil {
		folder, err = archive.Backup(m.opts.RemovedDir, pl.Title, only, time.Now())
	}
	if err != nil {
		m.status = "nothing deleted, as a copy could not be kept first: " + err.Error()
		return
	}
	m.stopSaving(pl)
	n, err := m.db.ForgetPlaylist(pl.ID)
	if err != nil {
		m.status = "could not delete the saved details: " + err.Error()
		return
	}
	for _, v := range only {
		if m.arch.active == nil || m.arch.active.ID != v.ID {
			m.dropQueued(v.ID)
			delete(m.arch.pending, v.ID)
		}
	}
	_ = archive.RemoveThumbnails(only)
	if !m.arch.sweeping {
		m.arch.read = 0 // no "saved every video" report for what was just deleted
	}
	m.status = fmt.Sprintf("stopped saving %s and deleted %d video(s); a copy is in %s", pl.Title, n, folder)
}

// playlistMarks is which playlists are saved and how many saved videos each
// holds, for the playlists list.
func (m *Model) playlistMarks() (map[string]bool, map[string]int) {
	saving := map[string]bool{}
	if pls, err := m.db.SavedPlaylists(); err == nil {
		for _, p := range pls {
			saving[p.ID] = true
		}
	}
	counts, _ := m.db.PlaylistVideoCounts()
	return saving, counts
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
	// The playlists list shows how many videos each holds.
	return tea.Batch(cmd, m.refreshList(), m.readNextPlaylist())
}

// sweepStatus reports the sweep's progress in the status line while it runs,
// and once when it and the lookups it queued are finished.
func (m *Model) sweepStatus() {
	switch {
	case m.arch.sweeping:
		m.status = fmt.Sprintf("reading your saved playlists: %d of %d", m.arch.read, m.arch.read+len(m.arch.toRead)+1)
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
		add("Nothing saved: only videos in playlists you save are (press p, then s on a playlist).")
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
