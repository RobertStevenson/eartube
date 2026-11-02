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
make run                          # run from source
make run ARGS="--browser brave"   # with flags
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

## Configuration

eartube reads your YouTube login from a browser's cookies (via yt-dlp). Without
one, search works but your playlists do not. Pick a browser by any of these,
highest precedence first:

| How                  | Example                                   |
|----------------------|-------------------------------------------|
| Flag                 | `eartube --browser brave+gnomekeyring`    |
| Environment variable | `EARTUBE_BROWSER=firefox eartube`         |
| Config file          | `~/.config/eartube/config.toml`           |

The config file (`$XDG_CONFIG_HOME/eartube/config.toml`) is TOML:

```toml
browser = "brave+gnomekeyring"
```

The value is passed straight to yt-dlp's `--cookies-from-browser`, so
`brave`, `firefox`, `chromium`, `chrome`, and so on all work. An empty value
(`--browser=`) turns cookies off, which overrides a file or environment
setting. Unknown keys in the config file are an error, so typos do not pass
silently. `eartube -h` prints the flags and the config path in use.

**Omarchy / Linux note:** on Omarchy yt-dlp can pick the wrong keyring and fail
to decrypt Chromium-family cookies (you will see `cannot decrypt v11 cookies`
and a 401 on the playlists page). Naming the keyring fixes it, which is the
`+gnomekeyring` suffix above.

## Known limitations

- Reading cookies from your browser is the only supported sign-in. With no
  browser configured, or when you are not signed in, search still works but
  playlists will fail.
- yt-dlp's playlist and cookie handling is unofficial and can break when
  YouTube changes; the usual fix is `yt-dlp -U` (or your package manager).
- A track you have marked played will not resume, even if you listen to part
  of it again. Press `m` to unmark it.
- No previous-track key or queue reordering yet.

## Layout

```
cmd/eartube/       entry point
internal/config/    flags, environment, and config file
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

### Integration tests

The `player` and `ytdlp` packages are tested against the real mpv and yt-dlp.
These sit behind the `integration` build tag so the default run stays offline:

```sh
make test-integration                 # or: go test -tags integration ./...
go test -tags integration -short ./...    # skip the tests that hit YouTube
EARTUBE_BROWSER=brave+gnomekeyring make test-integration   # also test signed-in playlists
```

- `internal/player` drives a silent mpv (`--ao=null`) using small generated
  WAV files, covering status, seek, pause, speed, resume offsets, end-of-file
  and error signals, and shutdown. One test plays a short public YouTube video.
- `internal/ytdlp` runs real searches and a public playlist, and checks that
  failures surface as errors. The signed-in playlists test only runs when
  `EARTUBE_BROWSER` is set, and skips otherwise.
- yt-dlp output parsing is unit-tested offline (`parse_test.go`).
