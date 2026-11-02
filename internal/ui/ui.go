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
	return d
}

// resumeMin is how far in (seconds) a track must be before we resume it.
const resumeMin = 5

// saveEvery is how many ticks (500ms each) pass between periodic saves.
const saveEvery = 10

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

func waitEnded(p Player) tea.Cmd {
	return func() tea.Msg { return endedMsg{eof: <-p.Done()} }
}

type Model struct {
	yt     Source
	pl     Player
	db     *store.Store
	state  map[string]store.Track // in-memory mirror of the tracks table
	ticks  int
	list   list.Model
	input  textinput.Model
	typing bool
	status string
	w, h   int

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

func New(p Player, db *store.Store, src Source) Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "eartube"
	l.SetShowHelp(false)
	in := textinput.New()
	in.Prompt = "search: "
	m := Model{yt: src, pl: p, db: db, list: l, input: in, qpos: -1,
		state:  map[string]store.Track{},
		status: "/ search · p playlists · enter play"}
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

func (m Model) Init() tea.Cmd { return tea.Batch(m.fetchPlaylists(), tick(), waitEnded(m.pl)) }

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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.list.SetSize(m.w, m.h-4)
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
			es[i] = entry{it: it, isPlaylist: msg.isPlaylist, tr: m.state[it.ID]}
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
	m.pl.Load(it.URL, start)
	m.status = "playing: " + it.Title
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

func (m *Model) queueItems() []list.Item {
	items := make([]list.Item, len(m.queue))
	for i, it := range m.queue {
		items[i] = entry{it: it, tr: m.state[it.ID]}
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
	help := "enter play · m played · a/A queue · tab queue view · x remove · n next · space pause · ←/→ seek · [ ] speed · / search · p playlists · q quit"
	return top + "\n" + m.list.View() + "\n" + np + "\n" + dim.Render(help)
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
