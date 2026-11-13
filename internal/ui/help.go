package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// keyDoc documents one key binding. keys are the exact strings Bubble Tea
// reports (" " for space), so a test can compare them with the code.
type keyDoc struct {
	keys  []string
	label string // how the key is shown
	desc  string
}

type keyGroup struct {
	title          string
	needsDownloads bool // marked as off when downloads are disabled
	docs           []keyDoc
}

// keyGroups is the full key reference shown by "?". Every key the program
// handles must appear here: TestEveryKeyIsDocumented reads the source to enforce it.
var keyGroups = []keyGroup{
	{title: "Playing", docs: []keyDoc{
		{[]string{"enter"}, "enter", "play the selected video and queue the rest of the list"},
		{[]string{" "}, "space", "pause or resume (also resumes a restored queue)"},
		{[]string{"left", "right"}, "← / →", "seek back / forward 10 seconds"},
		{[]string{"[", "]"}, "[ / ]", "slower / faster by 0.25x"},
		{[]string{"n"}, "n", "next in the queue"},
		{[]string{"b"}, "b", "back: restart after 3s, otherwise the previous item"},
	}},
	{title: "Queue", docs: []keyDoc{
		{[]string{"a", "A"}, "a / A", "add the selected video / every video in the list"},
		{[]string{"tab"}, "tab", "switch between the list and the queue"},
		{[]string{"x"}, "x", "remove the selected item (queue view)"},
		{[]string{"K", "J"}, "K / J", "move the selected item up / down (queue view)"},
	}},
	{title: "Finding things", docs: []keyDoc{
		{[]string{"/"}, "/", "search YouTube (enter runs it, esc cancels)"},
		{[]string{"p"}, "p", "your playlists, including Watch Later and Liked"},
		{[]string{"f"}, "f", "new from your subscriptions; again to switch videos / Shorts"},
		{[]string{"r"}, "r", "(in subscriptions) refresh"},
		{[]string{"o"}, "o", "downloaded videos (works with no network)"},
		{[]string{"m"}, "m", "mark the selected video played / unplayed"},
	}},
	{title: "Downloads", needsDownloads: true, docs: []keyDoc{
		{[]string{"d"}, "d", "download the selected video's audio"},
		{[]string{"D"}, "D", "download every video in the list"},
		{[]string{"X"}, "X", "delete the selected video's download"},
	}},
	{title: "Saved details", docs: []keyDoc{
		{[]string{"i"}, "i", "what is saved about the selected video, even if it was removed"},
		{[]string{"s"}, "s", "save the selected playlist's video details, or stop"},
		{[]string{"I"}, "I", "re-read the saved playlists: new videos, removed ones"},
		{[]string{"y", "n"}, "y / n", "(in a question) yes / no"},
	}},
	{title: "Moving around", docs: []keyDoc{
		{[]string{"up", "down", "j", "k"}, "↑ ↓ j k", "move (scroll, in help and details)"},
		{[]string{"pgup", "pgdown"}, "pgup pgdn", "page up / down"},
		{[]string{"g", "G", "home", "end"}, "g / G", "jump to the first / last item"},
	}},
	{title: "Settings and help", docs: []keyDoc{
		{[]string{"c"}, "c", "settings: where each setting comes from"},
		{[]string{"e"}, "e", "(in settings) edit the config file in your editor"},
		{[]string{"u"}, "u", "(in settings) add missing settings to the file, as comments"},
		{[]string{"i"}, "i", "(in settings) stop offering to add missing settings"},
		{[]string{"?"}, "?", "this help"},
		{[]string{"esc"}, "esc", "close settings, help or details; cancel a search or question"},
	}},
	{title: "General", docs: []keyDoc{
		{[]string{"q", "ctrl+c"}, "q", "quit"},
	}},
}

type helpState struct {
	open   bool
	offset int // first visible content line
}

var bold = lipgloss.NewStyle().Bold(true)

// helpKey handles keys for the help screen. As with settings, playback keys pass
// through and everything else is swallowed so it cannot act on the hidden list.
func (m *Model) helpKey(key string) (handled bool, cmd tea.Cmd) {
	if !m.help.open {
		switch key {
		case "?":
			m.help.open, m.help.offset = true, 0
			m.status = "help: ↑/↓ scroll, esc closes"
			return true, nil
		}
		return false, nil
	}
	page := max(m.helpWindow()-1, 1)
	switch key {
	case "esc", "?":
		m.help.open = false
		m.status = ""
		return true, nil
	case "up", "k":
		m.help.offset--
	case "down", "j":
		m.help.offset++
	case "pgup":
		m.help.offset -= page
	case "pgdown":
		m.help.offset += page
	case "home", "g":
		m.help.offset = 0
	case "end", "G":
		m.help.offset = 1 << 30
	case "ctrl+c", "q", " ", "left", "right", "[", "]", "n", "b":
		return false, nil
	default:
		return true, nil
	}
	m.help.offset = min(max(m.help.offset, 0), m.helpMaxOffset())
	return true, nil
}

// helpLines is the reference as text lines, one per binding plus group titles.
func (m Model) helpLines() []string {
	var lines []string
	for i, g := range keyGroups {
		if i > 0 {
			lines = append(lines, "")
		}
		title := g.title
		if g.needsDownloads && !m.opts.Downloads {
			title += "  (off: set downloads = true to enable)"
		}
		lines = append(lines, bold.Render(title))
		for _, d := range g.docs {
			lines = append(lines, fmt.Sprintf("  %-10s %s", d.label, d.desc))
		}
	}
	return lines
}

// bodyHeight is the room for the main area: the terminal minus the status line,
// the now-playing line and the footer.
func (m Model) bodyHeight() int { return m.h - 3 - m.helpHeight() }

// helpWindow is how many reference lines fit (the panel keeps a header line
// and a scroll-hint line). 0 means the size is not known yet: show everything.
func (m Model) helpWindow() int {
	if m.h <= 0 {
		return 0
	}
	return max(m.bodyHeight()-2, 1)
}

func (m Model) helpMaxOffset() int {
	w := m.helpWindow()
	if w == 0 {
		return 0
	}
	return max(len(m.helpLines())-w, 0)
}

// helpPanel renders the reference, scrolled and clipped to fit.
func (m Model) helpPanel() string {
	lines := m.helpLines()
	win, off := m.helpWindow(), min(m.help.offset, m.helpMaxOffset())
	hint := ""
	if win > 0 && len(lines) > win {
		switch {
		case off > 0 && off < m.helpMaxOffset():
			hint = "↑ ↓ more"
		case off > 0:
			hint = "↑ more above"
		default:
			hint = "↓ more below"
		}
		lines = lines[off : off+win]
	}
	out := []string{bold.Render("Keys") + dim.Render("   ↑/↓ scroll · esc closes")}
	out = append(out, lines...)
	if hint != "" {
		out = append(out, dim.Render(hint))
	}
	return clipWidth(strings.Join(out, "\n"), m.w)
}

// clipWidth truncates every line to w columns, so a panel can never wrap and
// push the layout off screen. w <= 0 (size not known yet) leaves it alone.
func clipWidth(s string, w int) string {
	if w <= 0 {
		return s
	}
	st := lipgloss.NewStyle().MaxWidth(w)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = st.Render(l)
	}
	return strings.Join(lines, "\n")
}
