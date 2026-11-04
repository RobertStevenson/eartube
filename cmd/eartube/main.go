package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobertStevenson/eartube/internal/config"
	"github.com/RobertStevenson/eartube/internal/download"
	"github.com/RobertStevenson/eartube/internal/player"
	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ui"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
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

	defaultDir := filepath.Join(filepath.Dir(path), "downloads")
	opts := ui.Options{
		Downloads:    cfg.Downloads,
		DeletePlayed: cfg.DeletePlayed,
		MaxBytes:     cfg.MaxBytes(),
		Settings: ui.Settings{
			Path:     cfg.Path,
			Rows:     cfg.Rows(defaultDir),
			Ensure:   func() (bool, error) { return config.EnsureFile(cfg.Path) },
			Edit:     ui.ExecEditor,
			Validate: func() error { return config.Check(config.OSEnv()) },
		},
	}
	if cfg.Downloads {
		dir := cfg.DownloadDir
		if dir == "" {
			dir = defaultDir
		}
		opts.Downloader = download.Fetcher{Dir: dir, Browser: browser}
	}

	model := ui.New(p, db, ytdlp.Client{Browser: browser}, opts)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
