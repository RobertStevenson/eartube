package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"eartube/internal/player"
	"eartube/internal/ui"
)

// Brave on Omarchy: yt-dlp auto-detects the wrong keyring, so name it explicitly.
const browser = "brave+gnomekeyring"

func main() {
	p, err := player.Start(browser)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to start mpv:", err)
		os.Exit(1)
	}
	defer p.Close()

	if _, err := tea.NewProgram(ui.New(p, browser), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
