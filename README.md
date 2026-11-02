# eartube

An audio-only YouTube player for the terminal. It treats YouTube like a
podcast app: browse your playlists or search, queue things up, and listen
without ever loading the video. Position and played state are remembered, so
you can pick up where you left off.

Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).
It shells out to `yt-dlp` to find videos and drives a headless `mpv` to play
the audio.

## Requirements

- Go 1.22 or newer
- [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) (keep it up to date; YouTube
  changes break old versions)
- [`mpv`](https://mpv.io)
- A Chromium-based browser signed in to YouTube, for access to your private
  playlists

On Arch / Omarchy:

```sh
sudo pacman -S go mpv yt-dlp
```

## Build and run

```sh
make run     # run from source
make build   # builds bin/eartube
```

## Keys

| Key       | Action                                              |
|-----------|-----------------------------------------------------|
| `enter`   | Play the selected video and queue the rest of the list after it; on a playlist, open it |
| `a` / `A` | Add the selected video / every video in the list to the queue |
| `n`       | Next in queue                                       |
| `tab`     | Switch between the browsing list and the queue      |
| `x`       | Remove the selected item (queue view)               |
| `m`       | Toggle played on the selected video                 |
| `space`   | Pause / resume (also resumes a restored queue)      |
| `←` / `→` | Seek -10s / +10s                                    |
| `[` / `]` | Slower / faster by 0.25x                            |
| `/`       | Search (`enter` to run, `esc` to cancel)            |
| `p`       | Your playlists (includes Watch Later and Liked videos) |
| `q`       | Quit (saves position and queue)                     |

Lists also support Bubble Tea's usual navigation (arrows, `j`/`k`,
page up/down).

## How it works

- **Search** runs `yt-dlp ytsearchN:<query>`. No API key or quota.
- **Playlists** are read with `yt-dlp --cookies-from-browser`, using your
  browser's YouTube login. This is read-only; eartube never modifies your
  playlists.
- **Playback** is a long-lived `mpv --no-video --idle` controlled over its JSON
  IPC socket. Stream URLs are resolved by mpv when a track starts, so they
  never go stale in a queue.
- **State** is kept in SQLite at `~/.local/share/eartube/eartube.db`
  (`$XDG_DATA_HOME/eartube/` if set): resume positions, played flags, and the
  queue. Positions save about every 5 seconds, when you change track, and on
  quit. A track that finishes, or that you leave within 15 seconds of the end,
  is marked played. Played tracks do not resume.

## Configuring the browser

The browser is a constant in `cmd/eartube/main.go`:

```go
const browser = "brave+gnomekeyring"
```

The value is passed straight to yt-dlp's `--cookies-from-browser`, so
`firefox`, `chromium`, `chrome`, and so on all work. The `+gnomekeyring` part
is there because on Omarchy yt-dlp otherwise picks the wrong keyring and
cannot decrypt Chromium-family cookies (you will see
`cannot decrypt v11 cookies` and a 401 on the playlists page). Drop the
suffix if your setup does not need it. Making this a flag or config file is a
good first contribution.

## Known limitations

- Reading cookies from your browser is the only supported sign-in. If you are
  not signed in, search still works but playlists will fail.
- yt-dlp's playlist and cookie handling is unofficial and can break when
  YouTube changes; the usual fix is `yt-dlp -U` (or your package manager).
- A track you have marked played will not resume, even if you listen to part
  of it again. Press `m` to unmark it.
- No previous-track key or queue reordering yet.

## Layout

```
cmd/eartube/       entry point
internal/ytdlp/    search and playlist listing via yt-dlp
internal/player/   mpv process and IPC
internal/store/    SQLite persistence
internal/ui/       Bubble Tea model and views
```

## Tests

```sh
go test ./...          # fast, offline: no mpv, yt-dlp, or network needed
go test -race ./...
```

The UI tests (`internal/ui`) run the real Bubble Tea program under
[`teatest`](https://github.com/charmbracelet/x/tree/main/exp/teatest) with a
fake player and fake yt-dlp behind the `Player` and `Source` interfaces
(`internal/ui/deps.go`), and a real SQLite file in a temp directory. Tests
send keys (`a.press("enter", "down")`), wait for text on screen
(`a.waitFor("playing: Episode A")`), and then assert on the fake player and
the database. See `internal/ui/harness_test.go` for the helpers.

The `player` and `ytdlp` packages talk to real mpv and yt-dlp and are not
covered by tests yet.
