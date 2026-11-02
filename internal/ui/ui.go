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

type Model struct {
	yt     ytdlp.Client
	pl     *player.Player
	list   list.Model
	input  textinput.Model
	typing bool
	status string
	w, h   int
}

func New(p *player.Player, browser string) Model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "eartube"
	l.SetShowHelp(false)
	in := textinput.New()
	in.Prompt = "search: "
	return Model{yt: ytdlp.Client{Browser: browser}, pl: p, list: l, input: in,
		status: "/ search · p playlists · enter play"}
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.fetchPlaylists(), tick()) }

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

	case loadedMsg:
		if msg.err != nil {
			m.status = "error: " + msg.err.Error()
			return m, nil
		}
		es := make([]list.Item, len(msg.items))
		for i, it := range msg.items {
			es[i] = entry{it: it, isPlaylist: msg.isPlaylist}
		}
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
			if e, ok := m.list.SelectedItem().(entry); ok {
				if e.isPlaylist {
					m.status = "loading playlist…"
					return m, m.fetchPlaylist(e)
				}
				m.pl.Load(e.it.URL)
				m.status = "playing: " + e.it.Title
			}
			return m, nil
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
	}
	help := "space pause · ←/→ seek · [ ] speed · / search · p playlists · q quit"
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
