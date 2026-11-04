package ui

import (
	"context"

	"github.com/RobertStevenson/eartube/internal/download"
	"github.com/RobertStevenson/eartube/internal/player"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// Player is the playback engine the UI controls. *player.Player implements it;
// tests substitute a fake.
type Player interface {
	Load(url string, start float64)
	// LoadResolved starts a video from an already-resolved stream URL, falling
	// back to url if that stream has gone stale.
	LoadResolved(url, stream string, start float64)
	// LoadFile plays a downloaded file, falling back to url if it is unplayable.
	LoadFile(url, path string, start float64)
	TogglePause()
	Seek(sec float64)
	AddSpeed(delta float64)
	Status() player.Status
	// Done delivers a value when a track finishes: true for EOF, false for error.
	Done() <-chan bool
}

// Source finds videos. ytdlp.Client implements it; tests substitute a fake.
type Source interface {
	Search(ctx context.Context, query string, n int) ([]ytdlp.Item, error)
	Playlists(ctx context.Context) ([]ytdlp.Item, error)
	Playlist(ctx context.Context, url string) ([]ytdlp.Item, error)
	// Resolve returns a direct stream URL for a video page, for lookahead.
	Resolve(ctx context.Context, url string) (string, error)
}

// Downloader fetches a video's audio for offline playback.
// download.Fetcher implements it; tests substitute a fake.
type Downloader interface {
	Fetch(ctx context.Context, id, url string, progress func(pct float64)) (download.Result, error)
}
