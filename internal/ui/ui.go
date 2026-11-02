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

	"eartube/internal/player"
	"eartube/internal/ytdlp"
)

type entry struct {
	it         ytdlp.Item
	isPlaylist bool
}

func (e entry) Title() string       { return e.it.Title }
func (e entry) FilterValue() string { return e.it.Title }
func (e entry) Description() string {
	if e.isPlaylist {
		return "playlist"
	}
	return fmt.Sprintf("%s · %s", e.it.Channel, fmtDur(e.it.Duration))
}

type loadedMsg struct {
	items      []ytdlp.Item
	isPlaylist bool
	title      string
	err        error
}
type tickMsg time.Time
type endedMsg struct{}

func waitEnded(p *player.Player) tea.Cmd {
	return func() tea.Msg {
		<-p.Ended
		return endedMsg{}
	}
}

type Model struct {
	yt     ytdlp.Client
	pl     *player.Player
	list   list.Model
	input  textinput.Model
	typing bool
	status string
	w, h   int

	queue []ytdlp.Item // upcoming and current items
	qpos  int          // index of the current item in queue; -1 when nothing is playing

	showQueue   bool
	browseItems []list.Item // list contents saved while the queue view is open
	browseTitle string
}

func New(p *player.Player, browser string) Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "eartube"
	l.SetShowHelp(false)
	in := textinput.New()
	in.Prompt = "search: "
	return Model{yt: ytdlp.Client{Browser: browser}, pl: p, list: l, input: in, qpos: -1,
		status: "/ search · p playlists · enter play"}
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
		return m, tick()

	case endedMsg:
		return m, tea.Batch(m.advance(), waitEnded(m.pl))

	case loadedMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		es := make([]list.Item, len(msg.items))
		for i, it := range msg.items {
			es[i] = entry{it: it, isPlaylist: msg.isPlaylist}
		}
		m.showQueue = false
		m.list.Title = msg.title
		m.status = fmt.Sprintf("%d items", len(es))
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
			if m.showQueue {
				m.playAt(m.list.Index())
				return m, nil
			}
			// Play from here and queue the rest of the list after it.
			m.queue = m.queue[:0]
			for _, li := range m.list.Items()[m.list.Index():] {
				m.queue = append(m.queue, li.(entry).it)
			}
			m.playAt(0)
			return m, nil
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
			return m, m.advance()
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

// playAt starts queue[i] and makes it the current item.
func (m *Model) playAt(i int) {
	if i < 0 || i >= len(m.queue) {
		return
	}
	m.qpos = i
	m.pl.Load(m.queue[i].URL)
	m.status = "playing: " + m.queue[i].Title
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
		items[i] = entry{it: it}
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
		return m.list.SetItems(m.browseItems)
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
	help := "enter play · a/A queue · tab queue view · x remove · n next · space pause · ←/→ seek · [ ] speed · / search · p playlists · q quit"
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
