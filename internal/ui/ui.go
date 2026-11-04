// Package ui is the Bubble Tea front end.
package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

type entry struct {
	it         ytdlp.Item
	isPlaylist bool
	tr         store.Track // listening state, copied in by Model.decorate
	dl         bool        // downloaded for offline playback
}

func (e entry) FilterValue() string { return e.it.Title }
func (e entry) Title() string {
	if e.tr.Played {
		return "✓ " + e.it.Title
	}
	return e.it.Title
}
func (e entry) Description() string {
	if e.isPlaylist {
		return "playlist"
	}
	d := fmt.Sprintf("%s · %s", e.it.Channel, fmtDur(e.it.Duration))
	if !e.tr.Played && e.tr.Pos > resumeMin {
		d += " · resume " + fmtDur(e.tr.Pos)
	}
	if e.dl {
		d += " · ↓ downloaded"
	}
	return d
}

// resumeMin is how far in (seconds) a track must be before we resume it.
const resumeMin = 5

// saveEvery is how many ticks (500ms each) pass between periodic saves.
const saveEvery = 10

// restartThreshold is how far into a track (seconds) "previous" restarts it
// rather than going back to the prior queue item.
const restartThreshold = 3

// lookaheadAfter is how far into a track (seconds) we wait before resolving the
// next queued track's stream, so the lookup doesn't compete with starting up.
const lookaheadAfter = 2

// hoverDelay is how long the cursor must rest on a video before we resolve its
// stream speculatively. It keeps scrolling through a list from triggering a
// yt-dlp run per row.
const hoverDelay = 300 * time.Millisecond

// resolvedTTL is how long a pre-resolved stream URL is trusted. YouTube's
// expire after hours; this is deliberately well inside that.
const resolvedTTL = time.Hour

// finishedWithin marks a track played when this close to the end (seconds).
const finishedWithin = 15

type loadedMsg struct {
	items      []ytdlp.Item
	isPlaylist bool
	title      string
	err        error
}
type tickMsg time.Time
type endedMsg struct{ eof bool }

// hoverMsg fires hoverDelay after the selection moved onto video id.
type hoverMsg struct{ id string }

// resolvedMsg carries the result of a background lookahead resolve.
type resolvedMsg struct {
	id, stream string
	err        error
}

// resolvedURL is one entry of the lookahead cache.
type resolvedURL struct {
	stream  string
	at      time.Time
	pending bool // lookup in flight
	failed  bool // lookup failed; don't retry, just load normally
}

// usable reports whether the cached stream can be handed to the player.
func (r resolvedURL) usable(now time.Time) bool {
	return !r.pending && !r.failed && r.stream != "" && now.Sub(r.at) < resolvedTTL
}

func waitEnded(p Player) tea.Cmd {
	return func() tea.Msg { return endedMsg{eof: <-p.Done()} }
}

type Model struct {
	yt    Source
	pl    Player
	db    *store.Store
	state map[string]store.Track // in-memory mirror of the tracks table
	dl    dlState
	// resolved caches stream URLs found ahead of time, by video ID.
	resolved map[string]resolvedURL
	hoverID  string // the video the cursor was last seen on, for debouncing
	ticks    int
	list     list.Model
	input    textinput.Model
	typing   bool
	status   string
	w, h     int

	queue []ytdlp.Item // upcoming and current items
	qpos  int          // index of the current item in queue; -1 when nothing is playing

	restored    bool // a saved queue was loaded and nothing has played yet
	showQueue   bool
	browseItems []list.Item // list contents saved while the queue view is open
	browseTitle string
}

type savedQueue struct {
	Items []ytdlp.Item
	Pos   int
}

func New(p Player, db *store.Store, src Source, opts Options) Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "eartube"
	l.SetShowHelp(false)
	in := textinput.New()
	in.Prompt = "search: "
	m := Model{yt: src, pl: p, db: db, list: l, input: in, qpos: -1,
		state:    map[string]store.Track{},
		resolved: map[string]resolvedURL{},
		dl:       newDLState(opts, db),
		status:   "/ search · p playlists · enter play"}
	if st, err := db.All(); err == nil {
		m.state = st
	}
	var sq savedQueue
	if ok, _ := db.LoadJSON("queue", &sq); ok && len(sq.Items) > 0 && sq.Pos < len(sq.Items) {
		m.queue, m.qpos, m.restored = sq.Items, sq.Pos, true
		m.status = "queue restored — space to resume, tab to view"
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.fetchPlaylists(), tick(), waitEnded(m.pl)}
	if m.restored {
		// Space will resume the restored queue; have its stream ready by then.
		if cur := m.current(); cur != nil {
			cmds = append(cmds, m.resolveCmd(*cur))
		}
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) fetchPlaylists() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		items, err := m.yt.Playlists(ctx)
		return loadedMsg{items: items, isPlaylist: true, title: "Your playlists", err: err}
	}
}

func (m Model) fetchSearch(q string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		items, err := m.yt.Search(ctx, q, 25)
		return loadedMsg{items: items, title: "Search: " + q, err: err}
	}
}

func (m Model) fetchPlaylist(e entry) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		items, err := m.yt.Playlist(ctx, e.it.URL)
		return loadedMsg{items: items, title: e.it.Title, err: err}
	}
}

// Update handles a message, then starts a lookahead resolve if one is due.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd := m.update(msg)
	mm := nm.(Model)
	if la := mm.lookahead(); la != nil {
		cmd = tea.Batch(cmd, la)
	}
	if w := mm.watchSelection(); w != nil {
		cmd = tea.Batch(cmd, w)
	}
	return mm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case resolvedMsg:
		m.resolved[msg.id] = resolvedURL{stream: msg.stream, at: time.Now(), failed: msg.err != nil}
		return m, nil

	case dlDoneMsg:
		return m, m.onDownloadDone(msg)

	case hoverMsg:
		// Only resolve if the cursor is still resting on that video.
		if e, ok := m.list.SelectedItem().(entry); ok && !e.isPlaylist && e.it.ID == msg.id {
			return m, m.resolveCmd(e.it)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.list.SetSize(m.w, m.h-5) // status line, now-playing, and a 2-line help footer
		return m, nil

	case tickMsg:
		if m.ticks++; m.ticks%saveEvery == 0 {
			m.saveProgress()
			m.saveQueue()
		}
		return m, tick()

	case endedMsg:
		if msg.eof && m.current() != nil {
			m.setPlayed(m.current().ID, true)
		}
		return m, tea.Batch(m.advance(), m.refreshList(), waitEnded(m.pl))

	case loadedMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		es := make([]list.Item, len(msg.items))
		for i, it := range msg.items {
			es[i] = entry{it: it, isPlaylist: msg.isPlaylist, tr: m.state[it.ID], dl: m.hasDownload(it.ID)}
		}
		m.showQueue = false
		m.list.Title = msg.title
		m.status = fmt.Sprintf("%d items", len(es))
		if m.restored {
			m.status += " · queue restored — space to resume, tab to view"
		}
		return m, m.list.SetItems(es)

	case tea.KeyMsg:
		if m.typing {
			switch msg.String() {
			case "enter":
				m.typing = false
				m.input.Blur()
				q := m.input.Value()
				m.status = "searching…"
				return m, m.fetchSearch(q)
			case "esc":
				m.typing = false
				m.input.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q":
			m.cancelDownloads()
			m.saveProgress()
			m.saveQueue()
			return m, tea.Quit
		case "/":
			m.typing = true
			m.input.SetValue("")
			return m, m.input.Focus()
		case "p":
			m.status = "loading playlists…"
			return m, m.fetchPlaylists()
		case "enter":
			e, ok := m.list.SelectedItem().(entry)
			if !ok {
				return m, nil
			}
			if e.isPlaylist {
				m.status = "loading playlist…"
				return m, m.fetchPlaylist(e)
			}
			m.saveProgress() // before the queue changes under qpos
			if m.showQueue {
				m.playAt(m.list.Index())
				return m, m.refreshList()
			}
			// Play from here and queue the rest of the list after it.
			m.queue = m.queue[:0]
			for _, li := range m.list.Items()[m.list.Index():] {
				m.queue = append(m.queue, li.(entry).it)
			}
			m.playAt(0)
			return m, m.refreshList()
		case "a":
			if e, ok := m.list.SelectedItem().(entry); ok && !e.isPlaylist && !m.showQueue {
				m.queue = append(m.queue, e.it)
				m.status = fmt.Sprintf("queued (%d in queue)", len(m.queue))
				m.startIfIdle()
			}
			return m, nil
		case "A":
			if !m.showQueue {
				for _, li := range m.list.Items() {
					if e := li.(entry); !e.isPlaylist {
						m.queue = append(m.queue, e.it)
					}
				}
				m.status = fmt.Sprintf("queued all (%d in queue)", len(m.queue))
				m.startIfIdle()
			}
			return m, nil
		case "n":
			m.saveProgress()
			return m, tea.Batch(m.advance(), m.refreshList())
		case "b":
			return m, m.previous()
		case "K", "J":
			if m.showQueue {
				d := -1
				if msg.String() == "J" {
					d = 1
				}
				m.moveQueueItem(d)
			}
			return m, nil
		case "d":
			e, ok := m.list.SelectedItem().(entry)
			switch {
			case !ok:
			case e.isPlaylist:
				m.status = "open the playlist first: d downloads one video, D the whole list"
			default:
				return m, m.requestDownload([]ytdlp.Item{e.it})
			}
			return m, nil
		case "D":
			var items []ytdlp.Item
			for _, li := range m.list.Items() {
				if e := li.(entry); !e.isPlaylist {
					items = append(items, e.it)
				}
			}
			return m, m.requestDownload(items)
		case "X":
			if e, ok := m.list.SelectedItem().(entry); ok && !e.isPlaylist {
				if m.hasDownload(e.it.ID) {
					m.removeDownload(e.it.ID)
					m.status = "removed download: " + e.it.Title
					return m, m.refreshList()
				}
				m.status = "not downloaded: " + e.it.Title
			}
			return m, nil
		case "m":
			if e, ok := m.list.SelectedItem().(entry); ok && !e.isPlaylist {
				m.setPlayed(e.it.ID, !e.tr.Played)
				return m, m.refreshList()
			}
			return m, nil
		case "x":
			if m.showQueue && m.list.Index() != m.qpos {
				i := m.list.Index()
				m.queue = append(m.queue[:i], m.queue[i+1:]...)
				if i < m.qpos {
					m.qpos--
				}
				return m, m.refreshQueueView()
			}
			return m, nil
		case "tab":
			return m, m.toggleQueue()
		case " ":
			if m.pl.Status().Idle && m.qpos >= 0 && m.qpos < len(m.queue) {
				m.playAt(m.qpos) // resume a restored queue
				return m, m.refreshList()
			}
			m.pl.TogglePause()
			return m, nil
		case "left":
			m.pl.Seek(-10)
			return m, nil
		case "right":
			m.pl.Seek(10)
			return m, nil
		case "[":
			m.pl.AddSpeed(-0.25)
			return m, nil
		case "]":
			m.pl.AddSpeed(0.25)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// current is the queue item being played, or nil.
func (m *Model) current() *ytdlp.Item {
	if m.qpos < 0 || m.qpos >= len(m.queue) {
		return nil
	}
	return &m.queue[m.qpos]
}

// playAt starts queue[i], resuming where we left off unless it was played.
func (m *Model) playAt(i int) {
	if i < 0 || i >= len(m.queue) {
		return
	}
	m.qpos, m.restored = i, false
	it := m.queue[i]
	start := 0.0
	if tr := m.state[it.ID]; !tr.Played && tr.Pos > resumeMin {
		start = tr.Pos - 2 // back up a couple of seconds for context
	}
	m.loadTrack(it, start)
	m.status = "playing: " + it.Title
}

// loadTrack starts it, preferring a downloaded file, then a pre-resolved
// stream, then the normal lookup.
func (m *Model) loadTrack(it ytdlp.Item, start float64) {
	if path, ok := m.localFile(it.ID); ok {
		m.touchDownload(it.ID)
		m.pl.LoadFile(it.URL, path, start)
		return
	}
	if r := m.resolved[it.ID]; r.usable(time.Now()) {
		delete(m.resolved, it.ID) // single use; a later replay resolves afresh
		m.pl.LoadResolved(it.URL, r.stream, start)
		return
	}
	m.pl.Load(it.URL, start)
}

// lookahead resolves the next queued track's stream in the background once the
// current one is well underway, so auto-advance and "next" start instantly.
func (m *Model) lookahead() tea.Cmd {
	if m.qpos < 0 || m.qpos+1 >= len(m.queue) {
		return nil
	}
	if st := m.pl.Status(); st.Idle || st.Pos < lookaheadAfter {
		return nil
	}
	return m.resolveCmd(m.queue[m.qpos+1])
}

// watchSelection notices the cursor landing on a new video and, after it has
// rested there for hoverDelay, asks for that video to be resolved (hoverMsg).
func (m *Model) watchSelection() tea.Cmd {
	e, ok := m.list.SelectedItem().(entry)
	if !ok || e.isPlaylist {
		m.hoverID = ""
		return nil
	}
	if e.it.ID == m.hoverID {
		return nil
	}
	m.hoverID = e.it.ID
	id := e.it.ID
	return tea.Tick(hoverDelay, func(time.Time) tea.Msg { return hoverMsg{id: id} })
}

// resolveCmd starts a background stream lookup for it, unless one is already
// cached, in flight, or has already failed. Videos that need a sign-in fail
// here, which just means they load the normal way.
func (m *Model) resolveCmd(it ytdlp.Item) tea.Cmd {
	if m.hasDownload(it.ID) {
		return nil // plays from disk; nothing to look up
	}
	now := time.Now()
	if r, ok := m.resolved[it.ID]; ok && (r.pending || r.failed || now.Sub(r.at) < resolvedTTL) {
		return nil
	}
	for id, r := range m.resolved { // drop expired entries so the cache stays small
		if !r.pending && now.Sub(r.at) >= resolvedTTL {
			delete(m.resolved, id)
		}
	}
	m.resolved[it.ID] = resolvedURL{pending: true, at: now}
	src := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stream, err := src.Resolve(ctx, it.URL)
		return resolvedMsg{id: it.ID, stream: stream, err: err}
	}
}

// saveProgress stores the current position, or marks the track played if it
// is nearly finished. It must run before the queue or qpos change.
func (m *Model) saveProgress() {
	it, st := m.current(), m.pl.Status()
	if it == nil || st.Idle || st.Pos <= 0 || st.Duration <= 0 {
		return
	}
	if st.Duration-st.Pos < finishedWithin {
		m.setPlayed(it.ID, true)
		return
	}
	if err := m.db.SavePos(it.ID, st.Pos); err != nil {
		m.status = "save failed: " + err.Error()
		return
	}
	tr := m.state[it.ID]
	tr.Pos = st.Pos
	m.state[it.ID] = tr
}

func (m *Model) setPlayed(id string, played bool) {
	if err := m.db.SetPlayed(id, played); err != nil {
		m.status = "save failed: " + err.Error()
		return
	}
	m.state[id] = store.Track{Played: played}
	if played && m.dl.opts.DeletePlayed && m.hasDownload(id) {
		m.removeDownload(id)
	}
}

func (m *Model) saveQueue() {
	_ = m.db.SaveJSON("queue", savedQueue{Items: m.queue, Pos: m.qpos})
}

// decorate refreshes each entry's listening state from m.state.
func (m *Model) decorate(items []list.Item) []list.Item {
	out := make([]list.Item, len(items))
	for i, li := range items {
		e := li.(entry)
		e.tr = m.state[e.it.ID]
		e.dl = m.hasDownload(e.it.ID)
		out[i] = e
	}
	return out
}

// refreshList redraws the visible list after state changes, keeping the cursor.
func (m *Model) refreshList() tea.Cmd {
	idx := m.list.Index()
	cmd := m.list.SetItems(m.decorate(m.list.Items()))
	m.list.Select(idx)
	return cmd
}

// startIfIdle begins playback when something is queued but nothing is playing.
func (m *Model) startIfIdle() {
	if m.qpos < 0 && len(m.queue) > 0 {
		m.playAt(0)
	}
}

// advance moves to the next queued item, or stops at the end of the queue.
func (m *Model) advance() tea.Cmd {
	if m.qpos+1 < len(m.queue) {
		m.playAt(m.qpos + 1)
	} else {
		m.status = "end of queue"
	}
	return m.refreshQueueView()
}

// previous restarts the current track if we are a few seconds in (or at the
// head of the queue); otherwise it goes back one item.
func (m *Model) previous() tea.Cmd {
	cur := m.current()
	if cur == nil {
		m.status = "nothing to go back to"
		return nil
	}
	if m.pl.Status().Pos > restartThreshold || m.qpos == 0 {
		m.loadTrack(*cur, 0)
		if err := m.db.SavePos(cur.ID, 0); err == nil {
			tr := m.state[cur.ID]
			tr.Pos = 0
			m.state[cur.ID] = tr
		}
		m.restored = false
		m.status = "restarted: " + cur.Title
	} else {
		m.saveProgress()
		m.playAt(m.qpos - 1)
	}
	return tea.Batch(m.refreshQueueView(), m.refreshList())
}

// moveQueueItem swaps the selected queue item with its neighbour (d = -1 up,
// +1 down) and keeps the cursor and the "current track" marker on the right
// items.
func (m *Model) moveQueueItem(d int) {
	i, j := m.list.Index(), m.list.Index()+d
	if j < 0 || j >= len(m.queue) {
		return
	}
	m.queue[i], m.queue[j] = m.queue[j], m.queue[i]
	switch m.qpos {
	case i:
		m.qpos = j
	case j:
		m.qpos = i
	}
	m.list.SetItems(m.queueItems())
	m.list.Select(j)
	m.saveQueue()
}

func (m *Model) queueItems() []list.Item {
	items := make([]list.Item, len(m.queue))
	for i, it := range m.queue {
		items[i] = entry{it: it, tr: m.state[it.ID], dl: m.hasDownload(it.ID)}
	}
	return items
}

func (m *Model) refreshQueueView() tea.Cmd {
	if !m.showQueue {
		return nil
	}
	idx := m.list.Index()
	cmd := m.list.SetItems(m.queueItems())
	m.list.Select(idx)
	return cmd
}

func (m *Model) toggleQueue() tea.Cmd {
	if m.showQueue {
		m.showQueue = false
		m.list.Title = m.browseTitle
		return m.list.SetItems(m.decorate(m.browseItems))
	}
	m.showQueue = true
	m.browseItems, m.browseTitle = m.list.Items(), m.list.Title
	m.list.Title = "Queue"
	cmd := m.list.SetItems(m.queueItems())
	if m.qpos >= 0 {
		m.list.Select(m.qpos)
	}
	return cmd
}

var dim = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

func (m Model) View() string {
	top := m.status
	if m.typing {
		top = m.input.View()
	}
	st := m.pl.Status()
	np := "idle"
	if !st.Idle {
		icon := "▶"
		if st.Paused {
			icon = "⏸"
		}
		np = fmt.Sprintf("%s %s / %s  %.2fx", icon, fmtDur(st.Pos), fmtDur(st.Duration), st.Speed)
		if m.qpos >= 0 && m.qpos < len(m.queue) {
			np += "  " + m.queue[m.qpos].Title
			if left := len(m.queue) - m.qpos - 1; left > 0 {
				np += fmt.Sprintf("  (+%d queued)", left)
			}
		}
	}
	if d := m.downloadStatus(); d != "" {
		np += "   " + d
	}
	help := "enter play · m played · a/A queue · tab queue view · x remove · K/J move · n next · b back · space pause · ←/→ seek · [ ] speed · / search · p playlists · q quit"
	if m.dl.opts.Downloads {
		help = "d/D download · X remove download · " + help
	}
	return top + "\n" + m.list.View() + "\n" + np + "\n" + dim.Width(max(m.w, 20)).Render(help)
}

func fmtDur(s float64) string {
	if s <= 0 {
		return "--:--"
	}
	t := int(s)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}
