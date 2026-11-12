package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobertStevenson/eartube/internal/archive"
	"github.com/RobertStevenson/eartube/internal/config"
	"github.com/RobertStevenson/eartube/internal/download"
	"github.com/RobertStevenson/eartube/internal/player"
	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ui"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "archive" {
		os.Exit(runArchive(os.Args[2:]))
	}
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

	yt := ytdlp.Client{Browser: browser}
	defaultDir := filepath.Join(filepath.Dir(path), "downloads")
	opts := ui.Options{
		Downloads:    cfg.Downloads,
		DeletePlayed: cfg.DeletePlayed,
		MaxBytes:     cfg.MaxBytes(),
		Archiver: archive.Fetcher{Lookup: yt.Details,
			ThumbDir: filepath.Join(filepath.Dir(path), "thumbnails")},
		ArchivePause: time.Second,
		Settings: ui.Settings{
			Path:       cfg.Path,
			Rows:       cfg.Rows(defaultDir),
			Ensure:     func() (bool, error) { return config.EnsureFile(cfg.Path) },
			Edit:       ui.ExecEditor,
			Validate:   func() error { return config.Check(config.OSEnv()) },
			Missing:    func() ([]string, error) { return config.MissingInFile(cfg.Path) },
			AddMissing: func() ([]string, error) { return config.AddMissing(cfg.Path) },
		},
	}
	if cfg.Downloads {
		dir := cfg.DownloadDir
		if dir == "" {
			dir = defaultDir
		}
		opts.Downloader = download.Fetcher{Dir: dir, Browser: browser}
	}

	model := ui.New(p, db, yt, opts)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// runArchive is "eartube archive ...": it reads the saved video details and
// needs neither mpv nor the network.
func runArchive(args []string) int {
	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "data dir:", err)
		return 1
	}
	db, err := store.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		return 1
	}
	defer db.Close()
	switch err := archive.Run(args, db, os.Stdout, os.Stderr); {
	case errors.Is(err, flag.ErrHelp):
		return 0
	case err != nil:
		fmt.Fprintln(os.Stderr, "eartube archive:", err)
		return 2
	}
	return 0
}
