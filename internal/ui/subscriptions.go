package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultSubscriptionsCount matches config.DefaultSubscriptionsCount, for a
// caller that leaves Options.SubscriptionsCount unset.
const defaultSubscriptionsCount = 50

// subsTitle names the subscriptions view.
const subsTitle = "Subscriptions"

// openSubscriptions loads the newest uploads from your subscriptions afresh.
// Nothing in the list is saved.
func (m *Model) openSubscriptions() tea.Cmd { return m.fetchSubscriptions() }

// refreshSubscriptions reloads the subscriptions list while it is showing.
func (m *Model) refreshSubscriptions() tea.Cmd {
	if !m.inSubs || m.showQueue {
		m.status = "r refreshes your subscriptions: press f to open them"
		return nil
	}
	return m.fetchSubscriptions()
}

func (m *Model) fetchSubscriptions() tea.Cmd {
	n := m.opts.SubscriptionsCount
	if n <= 0 {
		n = defaultSubscriptionsCount
	}
	m.status = "loading subscriptions…"
	src := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		items, err := src.Subscriptions(ctx, n)
		return loadedMsg{items: items, title: subsTitle, subs: true, err: err}
	}
}
