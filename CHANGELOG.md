# Changelog

What changed in each release of eartube, newest first. Each version is a git tag
you can install with `go install github.com/RobertStevenson/eartube/cmd/eartube@vX.Y.Z`.

Timings are from the author's own machine and connection, so yours will vary.

## v0.7.0 - 2026-11-12

eartube now remembers the videos in your playlists, so one that is made private or deleted can still be identified.

### Added

- **Saved video details.** Choose which playlists to save with `s` in your playlists; none are saved until you do, so a catch-all such as Liked videos is left alone. Every video in a saved playlist is remembered, and each one's full details are looked up in the background, once: description, upload date, channel link, tags, and a 480x360 thumbnail image saved to disk. Opening a saved playlist, or `I` for all of them, saves new videos and notices removed ones.
- **Stopping a saved playlist** (`s` again) asks whether to delete the videos saved only from it. Yes writes a copy (JSON, Markdown and thumbnails) to `~/.local/share/eartube/removed/` first, and deletes nothing if the copy fails; no keeps them.
- **Removed videos are identified.** A video YouTube now lists as "[Private video]" or "[Deleted video]" shows as `⊘` with the title eartube saved, and is no longer played, queued or downloaded by mistake. `enter` or `i` shows everything saved about it, and when it was first seen gone.
- **Details view (`i`)** for any video: what is saved, with a scrollable description. On a video whose lookup is still waiting, it jumps the queue.
- **`eartube archive`** says which playlists are saved and how much, and `eartube archive export` writes it all out as JSON or Markdown (`--format`, `--unavailable` for just the removed ones, `--output`).

### Upgrading from v0.6.0

Nothing to do: the database gains its new tables on first launch. Videos that were already gone before this version first sees them cannot be identified; only their link and playlist are recorded.

## v0.6.0 - 2026-11-05

A help screen, an offer to add newer settings to your config file, and an optional weekly self-check.

### Added

- **Help (`?`)**: a scrollable key reference grouped by purpose (playing, queue, finding things, downloads, moving around, settings and help). It scrolls on short terminals with "more" hints, shows downloads as off when they are, keeps playback keys working while open, and swaps with the settings panel via `c` and `?`.
- **Offer to add newer settings.** When your config file does not mention a setting at all (neither as a line nor as a commented-out example), eartube says so at launch and lists the settings in the settings view (`c`). Press `u` to append commented examples for just those settings, or `i` to stop offering them (remembered; a setting added in a later version is offered again). It only ever appends: nothing already in your file is changed, moved or reformatted, and no setting changes value.
- **Weekly network check** (optional, for people running from a checkout): `make install-network-check` installs a systemd user timer that tests a fresh clone of the published `main` against real YouTube, with your own yt-dlp and mpv and your configured browser, and shows a desktop notification only if something fails. Offline runs are skipped quietly, and a YouTube sign-in demand is reported as its own thing. It runs on your machine because YouTube blocks GitHub's servers. `make uninstall-network-check` removes it.

### Changed

- The key-help footer shrinks to the handful of keys you need most plus `? all keys`, so it fits on one line at normal widths.
- The help and settings panels are clipped to the terminal width, so they can never wrap and push the screen off.

## v0.5.0 - 2026-11-04

A settings view, and a way to browse your downloads offline.

### Added

- **Settings (`c`)** shows every setting, its current value, and where that value came from: default, config file, an `EARTUBE_*` variable, or a flag. It also shows the path of your config file. Playback keys keep working while it is open; `esc` closes it.
- **Edit the config file in your editor (`e`, in settings).** The editor is `$VISUAL`, then `$EDITOR`, then `nano`, `vim` or `vi`. If there is no config file yet, it is first created from a starter template that lists every setting commented out at its default; an existing file is never overwritten. When the editor closes, the file is checked and any mistake (a misspelled setting, a wrong type) is reported straight away instead of at the next launch. Settings are read at startup, so restart eartube after changing them. A graphical editor needs a wait flag, for example `EDITOR="code --wait"`.
- **Downloaded view (`o`)** lists everything you have downloaded, most recently played first, with title, channel, duration and total size. It is built from local data, so it works with no network: pick something and `enter` plays it and queues the rest. If your playlists fail to load and you have downloads, the status line points you to it.

### Changed

- Downloads now record their title, channel and duration, so they can be listed offline.
- The key-help footer's reserved height now follows how it wraps, so the list fits on narrow terminals.

### Upgrading from v0.4.0

Nothing to do. The database upgrades itself on first launch and keeps your downloads. Downloads made with v0.4.0 did not record titles, so they appear by video ID until you next see them in a playlist or search result, when their titles are filled in.

## v0.4.0 - 2026-11-04

Optional offline downloads, and a faster first track.

### Added

- **Offline downloads**, off by default and always manual (turn on with `downloads = true` or `--downloads`). `d` downloads the selected video's audio, `D` downloads every video in the current list, and `X` deletes a download. Downloads run one at a time in the background with progress beside the now-playing line, and downloaded videos are marked `↓ downloaded`.
- A downloaded track **plays from disk**, with no network, and starts almost instantly (about 60 milliseconds). If the file is missing or damaged, it quietly streams instead.
- **Size cap** (`download_max_gb`, default 5, `0` for unlimited): when a download pushes past it, the least recently played files are deleted first. Anything in your queue, and anything in the batch being downloaded, is never deleted. If that leaves nothing to delete, the rest of the batch is skipped and you are told.
- **`delete_played`** (default off) removes a download once its track is marked played; un-marking never deletes. **`download_dir`** chooses where files go (default `~/.local/share/eartube/downloads`). Each setting also has an `EARTUBE_*` variable and a flag.

### Changed

- **Faster first track.** The wait on the first track is mostly YouTube lookup time that cannot be shortened, so eartube now does it earlier: it resolves the video under your cursor once the cursor has rested on it for 300 ms, and the current track of a restored queue at launch. Pressing `enter` or `space` then usually starts in about 0.4 seconds. Scrolling past rows does not trigger lookups; the cost is a few extra background lookups for videos you point at but never play.

### Notes

- Downloading from YouTube generally goes against its terms of service. It is for personal use and is your responsibility.
- A video that needs a sign-in still starts slowly (about 10 seconds), and downloading one takes the same slow route, once.

## v0.3.0 - 2026-11-02

Much faster playback start.

### Changed

- Starting a track went from about **10 seconds** to about **3 seconds**, and the next queued track starts in about **0.4 seconds**.
- **Browser cookies are no longer used on every play.** Decrypting them cost about 6.5 seconds each time. A play now tries, cheapest first: a stream address found in the background, then yt-dlp without cookies, then yt-dlp with your cookies. The cookie step only runs for videos that need a sign-in (private, age-restricted, members-only), so those are the only ones that stay slow.
- **Lookahead.** A couple of seconds into a track, eartube works out the stream for the next queued track in the background, so auto-advance and `n` start almost instantly.
- mpv now asks for audio only, so it never opens a video stream.

### Notes

- A video that cannot be played at all (deleted, region-blocked) is now tried up to three ways before eartube gives up on it, so it takes a little longer to be skipped.
- No settings need changing.

## v0.2.0 - 2026-11-02

Two queue controls that were missing.

### Added

- **Previous track (`b`)**: more than 3 seconds into a track it restarts it, like a podcast app; otherwise it goes back one item in the queue, resuming wherever you left off there. At the front of the queue it restarts the current track.
- **Queue reordering (`K` / `J`)** in the queue view (`tab`) moves the selected item up or down. The track that is playing keeps playing and stays marked as current wherever you move it, and the new order is saved and used by `n`.

### Changed

- The key-help footer wraps onto reserved lines, so a long list of keys can no longer push the top of the screen off.

## v0.1.0 - 2026-11-02

The first release: an audio-only YouTube player for the terminal that treats YouTube like a podcast app.

### Added

- **Search YouTube** and **browse your own playlists**, including Watch Later and Liked videos. Playlists are read-only and use your browser's YouTube login through yt-dlp, so eartube never changes your account.
- **Audio-only playback** through mpv, with pause, seeking, and speed control.
- **A play queue** that advances on its own and is restored when you restart.
- **Remembers your place**: where you stopped in each video, and which ones you have finished.
- **A configurable browser** with `--browser`, `EARTUBE_BROWSER`, or `~/.config/eartube/config.toml`.
- MIT licence; unit tests, a driver that runs the real interface against fakes, tests against real mpv and yt-dlp, and CI on GitHub Actions.

### Notes

- With no browser configured, search works but your playlists do not.
- With a browser configured, every play re-reads that browser's cookies, which makes starting a track slow (about 10 seconds). Fixed in v0.3.0.
- Needs [mpv](https://mpv.io) and [yt-dlp](https://github.com/yt-dlp/yt-dlp) on your `PATH`, and Go 1.26 or newer to build.

[v0.7.0]: https://github.com/RobertStevenson/eartube/compare/v0.6.0...v0.7.0
[v0.6.0]: https://github.com/RobertStevenson/eartube/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/RobertStevenson/eartube/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/RobertStevenson/eartube/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/RobertStevenson/eartube/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/RobertStevenson/eartube/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/RobertStevenson/eartube/releases/tag/v0.1.0
