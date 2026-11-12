# eartube

An audio-only YouTube player for the terminal. It treats YouTube like a
podcast app: browse your playlists or search, queue things up, and listen
without ever loading the video. Position and played state are remembered, so
you can pick up where you left off.

It is built for the kind of video where the picture adds nothing, such as
talks, interviews, lectures, and single-host commentary.

- Search YouTube and browse your own playlists, including Watch Later and
  Liked videos (read-only; eartube never changes your account)
- Queue with auto-advance; the queue survives restarts
- Resume where you left off, with played / unplayed tracking
- Optional offline downloads (off by default, manual only), with a size cap
- Remembers every video in your playlists (title, channel, description, upload
  date, tags and a thumbnail), so when one is made private or deleted you can
  still see what it was; export it all as JSON or Markdown
- Speed control, seeking, and a text-only interface

Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).
It shells out to `yt-dlp` to find videos and drives a headless `mpv` to play
the audio. This is an early, personal project, so expect rough edges.

## Install

You need [`mpv`](https://mpv.io) and [`yt-dlp`](https://github.com/yt-dlp/yt-dlp)
on your `PATH`. Keep yt-dlp up to date, because YouTube changes break old
versions. On Arch / Omarchy:

```sh
sudo pacman -S mpv yt-dlp
```

Then install eartube (needs Go 1.26 or newer to build):

```sh
go install github.com/RobertStevenson/eartube/cmd/eartube@latest
```

Or build from a checkout:

```sh
git clone https://github.com/RobertStevenson/eartube
cd eartube
make build        # builds bin/eartube
```

What changed in each version, and anything to know when upgrading, is in
[CHANGELOG.md](CHANGELOG.md).

## Quick start

```sh
eartube                         # search works straight away
eartube --browser firefox       # also load your playlists
```

To see your own playlists, eartube needs a browser that is signed in to
YouTube; see [Configuration](#configuration) to make that permanent. Press `/`
to search, `p` for your playlists, and `enter` to play.

## Development

```sh
make run                          # run from source
make run ARGS="--browser brave"   # with flags
make test                         # fast, offline tests
```

## Keys

Press `?` inside eartube for the full key reference, grouped by purpose. It
scrolls (`↑`/`↓`, `pgup`/`pgdn`, `home`/`end`) when your terminal is too short
to show it all, and playback keys keep working while it is open. The bottom
line of the screen shows just the few keys you need most.

| Key       | Action                                              |
|-----------|-----------------------------------------------------|
| `enter`   | Play the selected video and queue the rest of the list after it; on a playlist, open it; on a removed video, show what it was |
| `a` / `A` | Add the selected video / every video in the list to the queue |
| `n`       | Next in queue                                       |
| `b`       | Back: restart the track if you are more than 3s in, otherwise the previous queue item |
| `tab`     | Switch between the browsing list and the queue      |
| `x`       | Remove the selected item (queue view)               |
| `K` / `J` | Move the selected item up / down (queue view)       |
| `m`       | Toggle played on the selected video                 |
| `d`       | Download the selected video's audio (needs `downloads = true`) |
| `D`       | Download every video in the current list            |
| `X`       | Delete the selected video's download                |
| `o`       | Open the **Downloaded** view: everything you have saved, no network needed |
| `i`       | Saved details of the selected video: description, upload date, tags, and more, even after YouTube removed it |
| `I`       | Read every one of your playlists and save the details of all their videos |
| `space`   | Pause / resume (also resumes a restored queue)      |
| `←` / `→` | Seek -10s / +10s                                    |
| `[` / `]` | Slower / faster by 0.25x                            |
| `/`       | Search (`enter` to run, `esc` to cancel)            |
| `p`       | Your playlists (includes Watch Later and Liked videos) |
| `?`       | Show every key (this table, inside the app)         |
| `c`       | Settings: see every setting and where it came from; `e` edits the config file |
| `q`       | Quit (saves position and queue)                     |

Lists also support Bubble Tea's usual navigation (arrows, `j`/`k`,
page up/down).

## How it works

- **Search** runs `yt-dlp ytsearchN:<query>`. No API key or quota.
- **Playlists** are read with `yt-dlp --cookies-from-browser`, using your
  browser's YouTube login. This is read-only; eartube never modifies your
  playlists.
- **Playback** is a long-lived `mpv --no-video --idle` controlled over its JSON
  IPC socket. Unless you download a video yourself, nothing is saved: mpv
  streams the audio as it plays. Starting a track tries, cheapest first:
  1. a file you downloaded (instant, and works offline);
  2. a stream URL already found in the background (see lookahead, below);
  3. yt-dlp **without** cookies (about 3 seconds);
  4. yt-dlp **with** your browser's cookies, only if the earlier ones fail, for
     videos that need a sign-in (private, age-restricted, members-only).

  Decrypting browser cookies costs several seconds on every use, so they are a
  last resort rather than the default.
- **Lookahead:** eartube resolves streams in the background so playback can
  start almost instantly (about 0.4s in testing, against about 3s cold). It
  does this for:
  - the **next queued track**, a couple of seconds into the current one, so
    auto-advance and `n` are instant;
  - the **video under the cursor**, once the cursor has rested on it for 300ms,
    so pressing `enter` is usually instant too. Scrolling past rows does not
    trigger lookups;
  - the **current track of a restored queue**, at launch, so `space` is instant.

  Videos that need a sign-in cannot be looked ahead without cookies, so they
  load the normal way. Resolved streams are used once and trusted for an hour.
  The cost is a few extra yt-dlp lookups in the background for videos you
  point at but never play.
- **Offline downloads** are off unless you turn them on (see
  [Offline downloads](#offline-downloads)). Nothing is ever downloaded
  automatically.
- **State** is kept in SQLite at `~/.local/share/eartube/eartube.db`
  (`$XDG_DATA_HOME/eartube/` if set): resume positions, played flags, the
  queue, and the [saved video details](#saved-video-details). Positions save about every 5 seconds, when you change track, and on
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

### The settings view

Press `c` inside eartube to see every setting, its current value, and **where
that value came from** (default, config file, an `EARTUBE_*` variable, or a
flag), plus the path of your config file. Playback keys keep working while it
is open; `esc` or `c` closes it.

Press `e` there to edit the config file in your editor. If the file does not
exist yet, eartube first creates it from a starter template with every setting
listed and commented out at its default, so you only uncomment what you want to
change. An existing file is never overwritten.

- The editor is `$VISUAL`, then `$EDITOR`, then the first of `nano`, `vim`,
  `vi` that is installed. Arguments are allowed (`EDITOR="code --wait"`); quotes
  inside the variable are not interpreted. A graphical editor must be told to
  wait (such as `code --wait`), or eartube thinks you closed it at once.
- When the editor closes, eartube checks the file and tells you straight away if
  something is wrong (a misspelled setting, a wrong type), instead of failing the
  next time you start it.
- Settings are read once at startup, so **restart eartube after changing
  them**. The settings view is read-only; editing happens in your editor.

#### Settings added in newer versions

When eartube gains a setting, your existing config file does not know about it.
If the file does not mention a setting at all (neither as a line nor as a
commented-out example), eartube says so at launch ("N newer setting(s) missing
from your config file: press `c`") and lists them in the settings view. Then:

- `u` **adds them** to the end of the file as commented-out examples at their
  defaults, each with its explanation. It only ever appends: nothing already in
  the file is changed, moved or reformatted, so your own comments and layout are
  safe, and no setting changes value.
- `i` **stops offering** those settings. That choice is remembered, and a setting
  added in a later version is offered again.

It is only ever an offer; eartube never edits your file unless you press `u`.

**Omarchy / Linux note:** on Omarchy yt-dlp can pick the wrong keyring and fail
to decrypt Chromium-family cookies (you will see `cannot decrypt v11 cookies`
and a 401 on the playlists page). Naming the keyring fixes it, which is the
`+gnomekeyring` suffix above.

## Offline downloads

For listening without a connection, or just to start instantly. It is **off by
default** and **manual**: eartube only downloads what you tell it to.

```toml
downloads = true          # turn on d / D / X
download_max_gb = 5       # cap on total size in GiB; 0 = unlimited (default 5)
delete_played = false     # delete a download once its track is marked played
download_dir = "~/Music/eartube"   # default: ~/.local/share/eartube/downloads
```

Each also has an environment variable (`EARTUBE_DOWNLOADS`,
`EARTUBE_DOWNLOAD_MAX_GB`, `EARTUBE_DELETE_PLAYED`, `EARTUBE_DOWNLOAD_DIR`) and
a flag (`--downloads`, `--download-max-gb`, `--delete-played`,
`--download-dir`), with the usual precedence.

- Press `d` on a video to download its audio, or `D` to download everything in
  the current list (a playlist, search results, or the queue view). Downloads
  run one at a time in the background, with progress shown next to the
  now-playing line. `X` deletes a download. Downloaded videos are marked
  `↓ downloaded`.
- Audio only, in whatever format YouTube serves (usually Opus or AAC), roughly
  50 to 60 MB per hour.
- A downloaded track plays from disk with no network and starts in well under
  a second (about 60 milliseconds in testing). If the file is missing or
  damaged, eartube quietly streams it instead.
- **The cap:** when a download pushes the total over `download_max_gb`, the
  *least recently played* downloads are deleted until it fits. Anything in your
  queue, and anything in the batch currently downloading, is never deleted. If
  that leaves nothing to delete, the rest of the batch is skipped and you are
  told, rather than eartube deleting what it just fetched.
- **`delete_played`** removes a download as soon as its track is marked played,
  whether it finished playing or you pressed `m`. Un-marking never deletes.
- Turning `downloads` off does not delete or hide anything you already have;
  those files still play. The cap is only enforced after a download.
- **The Downloaded view** (`o`) lists everything you have saved, most recently
  played first, with title, channel, duration and the total size. It is built
  from local data only, so it works with no network at all: press `o`, pick
  something, and `enter` plays it and queues the rest of the list. `m`, `a`,
  `A` and `X` work as in any list, and a removed download disappears from the
  view immediately. If your playlists fail to load and you have downloads,
  the status line tells you to press `o`.
- Search and your playlists still need a connection.
- Downloads made with v0.4.0 did not record titles. They appear in the
  Downloaded view by video ID until you next see them in a playlist or search
  result, at which point eartube fills in the title.

Downloading from YouTube generally goes against its terms of service. This
feature is for personal use and is your responsibility.

## Saved video details

Videos in playlists get made private or deleted, and YouTube then shows only
"[Private video]" or "[Deleted video]", with no hint of what you had saved.
eartube keeps its own record so you can still tell.

- **When a playlist is opened**, every video in it is remembered at once: title,
  channel and length, which come free with the listing. Then, one video at a
  time in the background, eartube looks up the rest: the full description,
  upload date, channel link, tags, and a 480x360 thumbnail image (about 30 KB).
  Progress shows next to the now-playing line. Each video is looked up only
  once; opening the playlist again costs nothing.
- **`I` saves everything** without opening each playlist: it reads all your
  playlists, including Watch Later and Liked videos, and queues lookups for
  every video not yet saved. A lookup takes about 2 seconds plus a 1-second
  pause between them to stay gentle on YouTube, so a thousand videos take
  roughly 50 minutes. Quitting stops it; pressing `I` again later carries on
  where it left off.
- **A removed video** appears in its playlist as `⊘` and its saved title,
  marked *unavailable*. `enter` or `i` shows everything saved, including the
  date it was first seen gone. It cannot be played, queued or downloaded.
- **`i` works on any video** in any list. For a video in one of your playlists
  whose details are not saved yet, it moves that video to the front of the
  lookup queue, and the view fills in when the lookup finishes.
- Only videos in your playlists are saved, not search results. Nothing saved
  is ever deleted, even if a video later leaves the playlist.
- Thumbnails are saved in `~/.local/share/eartube/thumbnails/`.

### Exporting

```sh
eartube archive                                   # how much is saved
eartube archive export > archive.json             # everything, as JSON
eartube archive export --format markdown --unavailable --output gone.md
```

`--unavailable` keeps only the videos that are no longer on YouTube. The JSON
has one object per video (`id`, `url`, `title`, `channel`, `upload_date`,
`duration_seconds`, `description`, `tags`, `thumbnail_file`, `playlists`,
`available`, `unavailable_since`, ...). Export needs neither mpv nor a network.

A video that was already gone the first time eartube saw it cannot be
identified: all that is known is its link and the playlists it is in.

## Known limitations

- A video that cannot be played at all (deleted, region-blocked) is tried up
  to three times before eartube gives up on it and moves on, so it takes a
  little longer to skip than a working one takes to start.
- Videos that need a sign-in start slower (about 10 seconds): they fail the
  cookie-free attempts first, then pay the cookie cost.
- If you press `enter` the instant the cursor lands on a video, before its
  300ms rest and lookup have finished, that one still takes about 3 seconds.
- Reading cookies from your browser is the only supported sign-in. With no
  browser configured, or when you are not signed in, search still works but
  playlists will fail.
- yt-dlp's playlist and cookie handling is unofficial and can break when
  YouTube changes; the usual fix is `yt-dlp -U` (or your package manager).
- A track you have marked played will not resume, even if you listen to part
  of it again. Press `m` to unmark it.

## Layout

```
cmd/eartube/       entry point
internal/config/    flags, environment, config file, and the starter template
internal/editor/    finds your editor ($VISUAL, $EDITOR, nano, vim, vi)
internal/ytdlp/    search, playlist listing and video details via yt-dlp
internal/player/   mpv process and IPC
internal/download/ yt-dlp audio downloads and the size-cap eviction rule
internal/archive/  saved video details: lookup, thumbnails, and export
internal/store/    SQLite persistence
scripts/           the weekly network check and its installer (tested like Go code)
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

### Releasing

When you tag a release, add its section to [CHANGELOG.md](CHANGELOG.md)
(`## vX.Y.Z - YYYY-MM-DD`). `go test ./scripts` fails if a `vX.Y.Z` tag has no
section, and if the changelog lists an older version that was never tagged. A
section for a release you are still preparing is fine. The check reads this
repository's tags, so it runs on a normal clone and is skipped where the tags
are not fetched (such as CI's shallow checkout).

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
  and error signals, the fallback stages, and shutdown. Network tests play a
  short public YouTube video, check that a normal play does not pay the cookie
  cost, that a pre-resolved stream starts fast, and (with `EARTUBE_BROWSER`)
  that the cookie fallback really works.
- `internal/download` downloads a real short video, checks progress reporting,
  failure cleanup and cancellation, and one test plays the downloaded file in
  real mpv. The eviction rule that decides what to delete is pure and tested
  exhaustively without any network.
- `internal/ytdlp` runs real searches and a public playlist, and checks that
  failures surface as errors. The signed-in playlists test only runs when
  `EARTUBE_BROWSER` is set, and skips otherwise.
- yt-dlp output parsing is unit-tested offline (`parse_test.go`).


### Weekly network check

YouTube and yt-dlp change often, so a feature that works today can break without
any change to this code. To catch that early, a small systemd user timer can run
the network tests every week on your own machine and show a desktop notification
**only if something fails**:

```sh
make install-network-check        # or: scripts/install-network-check.sh
make uninstall-network-check      # remove it again (your logs are kept)
```

- It runs Mondays at about 09:30, or at your next login if the machine was off.
- It tests a **fresh clone of the published `main`**, never your working copy, so
  an edit in progress cannot cause a false alarm. It uses the yt-dlp and mpv
  installed on your machine, which is what you actually use, and the browser from
  your eartube config for the signed-in tests.
- Being offline is not a failure: the run is recorded as skipped and nothing pops
  up. If YouTube asks for a sign-in (its bot check), you are told that, because it
  says nothing about the code.
- Results: `~/.local/state/eartube/last-result` (one line), `network-check.log`
  (history), and `last-failure.log` (the full output of the latest failed run).
  Run it now with `systemctl --user start eartube-network-check.service`.

This is a local timer rather than a GitHub Actions schedule because YouTube
blocks GitHub's servers with a bot check, so the tests cannot run there.
## License

[MIT](LICENSE) © 2026 Robert Stevenson
