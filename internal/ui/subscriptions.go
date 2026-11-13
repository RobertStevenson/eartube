package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// subsTab is which of the two subscriptions feeds the list shows. YouTube
// keeps Shorts in a feed of their own, without upload dates, so they cannot be
// merged into one list by time; they get a tab instead.
type subsTab int

const (
	noSubs subsTab = iota
	subsVideos
	subsShorts
)

// defaultSubscriptionsCount matches config.DefaultSubscriptionsCount, for a
// caller that leaves Options.SubscriptionsCount unset.
const defaultSubscriptionsCount = 50

func (t subsTab) title() string {
	if t == subsShorts {
		return "Subscriptions: Shorts"
	}
	return "Subscriptions: videos"
}

// hint names the keys that matter in the view, for the status line.
func (t subsTab) hint() string {
	if t == subsShorts {
		return "f videos · r refresh"
	}
	return "f Shorts · r refresh"
}

// openSubscriptions loads the newest uploads from your subscriptions afresh:
// the videos, or the Shorts when the videos are already showing, so f switches
// between the two. Nothing in either list is saved.
func (m *Model) openSubscriptions() tea.Cmd {
	tab := subsVideos
	if m.subs == subsVideos && !m.showQueue {
		tab = subsShorts
	}
	return m.fetchSubscriptions(tab)
}

// refreshSubscriptions reloads the feed the list shows.
func (m *Model) refreshSubscriptions() tea.Cmd {
	if m.subs == noSubs || m.showQueue {
		m.status = "r refreshes your subscriptions: press f to open them"
		return nil
	}
	return m.fetchSubscriptions(m.subs)
}

func (m *Model) fetchSubscriptions(tab subsTab) tea.Cmd {
	n := m.opts.SubscriptionsCount
	if n <= 0 {
		n = defaultSubscriptionsCount
	}
	m.status = "loading subscriptions…"
	src := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		items, err := src.Subscriptions(ctx, tab == subsShorts, n)
		return loadedMsg{items: items, title: tab.title(), subs: tab, err: err}
	}
}
