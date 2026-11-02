package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"eartube/internal/config"
	"eartube/internal/player"
	"eartube/internal/store"
	"eartube/internal/ui"
	"eartube/internal/ytdlp"
)

func main() {
	cfg, err := config.Load(os.Args[1:], config.OSEnv(), os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eartube:", err)
		os.Exit(2)
	}
	browser := cfg.Browser

	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "data dir:", err)
		os.Exit(1)
	}
	db, err := store.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		os.Exit(1)
	}
	defer db.Close()

	p, err := player.Start(browser)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to start mpv:", err)
		os.Exit(1)
	}
	defer p.Close()

	if _, err := tea.NewProgram(ui.New(p, db, ytdlp.Client{Browser: browser}), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
