package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"eartube/internal/player"
	"eartube/internal/ui"
)

func main() {
	p, err := player.Start("brave")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to start mpv:", err)
		os.Exit(1)
	}
	defer p.Close()

	if _, err := tea.NewProgram(ui.New(p, "brave"), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
