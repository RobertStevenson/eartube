# eartube: working notes

Audio-only YouTube player for the terminal, in Go with Bubble Tea. It shells out to
yt-dlp (search, playlists, downloads) and drives a headless mpv over its IPC socket.
MIT, public: github.com/RobertStevenson/eartube. `README.md` is the user documentation,
`CHANGELOG.md` the release notes; this file is for working on the code.

## Commands

- `make test`: `go test -race ./...`. Offline; about 25s uncached, instant when cached. Includes `scripts/`.
- `make test-integration`: real mpv and real YouTube (build tag `integration`). `-short`
  skips the network tests. `EARTUBE_BROWSER=<yt-dlp spec>` adds the signed-in ones.
- `make run ARGS="--browser brave"`, `make build`.
- CI (`.github/workflows/ci.yml`): Unit job (gofmt, `go mod tidy` leaves no diff, vet with
  and without the integration tag, `make test`) and an offline mpv Integration job.
- The weekly check against real YouTube is a **local systemd timer**
  (`make install-network-check`), not a GitHub schedule: YouTube's bot check blocks
  GitHub-hosted runners, so the network tests cannot run there. Do not re-add one.

## Layout

- `cmd/eartube`: wiring only.
- `internal/ui`: the Bubble Tea model. `ui.go` (model, Update, View), `downloads.go`,
  `settings.go`, `help.go`, `subscriptions.go` (the `f` feeds: videos and Shorts tabs,
  never archived), and `deps.go` (the interfaces the UI depends on).
- `internal/player`: mpv over IPC. `attempt.go` is the fallback chain for starting a
  track: downloaded file, then a pre-resolved stream, then yt-dlp without cookies, then
  yt-dlp with cookies. Cookies are the last resort because decrypting them costs seconds.
- `internal/ytdlp`, `internal/download` (`Fetcher`, and `Evict` for the size cap),
  `internal/store` (SQLite through a pure-Go driver), `internal/editor`.
- `internal/archive`: saved video details. `Fetcher` (yt-dlp details plus a thumbnail), the
  JSON/Markdown export, and `eartube archive` (dispatched in `main` before config is loaded).
  The UI side is `internal/ui/archive.go` (lookup queue, the `I` sweep, the `i` details view,
  and `s`: which playlists are saved, in the `saved_playlists` table; none by default).
- `internal/config`: precedence is flag, then `EARTUBE_*` variable, then file, then default.
  Records where each setting came from, owns the starter template, and appends missing
  settings to an existing file.
- `scripts/`: the weekly check, its installer, and Go tests for them, for `CHANGELOG.md`
  and for release tags.

## Rules the code relies on

- The UI talks to `Player`, `Source`, `Downloader`, `Archiver` and the `Settings` hooks through
  interfaces so tests can inject fakes. UI tests must never start real mpv, yt-dlp or an editor.
- A new setting goes in `Config`, `SettingKeys`, `settingDocs` (`describe.go`), the flag and
  environment handling, and the README. Tests fail if these drift apart.
- Every key the program handles must be in `keyGroups` (`help.go`). `TestEveryKeyIsDocumented`
  parses the source, so handle keys with `switch ... case "x":`, not `if key == "x"`.
- The user's config file is only ever appended to (`AddMissing`), never rewritten.
- Downloads are manual and off by default. Eviction never deletes anything queued or in the
  batch being downloaded.

## Testing

- UI tests run the real program under `teatest` with the harness in `internal/ui/harness_test.go`:
  `start`/`startWith`, `press`, `typeText`, `waitFor`/`waitGone` (they poll the model's current
  `View()`, not the terminal diff stream), `eventually`, `inspect`, `quit`. Fakes: `fakePlayer`,
  `fakeSource`, `fakeDownloader`, `fakeArchiver`, `fakeEditor`, `fakeConfigFile`. SQLite is real,
  in a temp dir.
- **Prove a new test can fail.** Break the code on purpose, check the right test fails, restore.
  Many tests passed first time and several were weak (asserting before an async key was
  processed, checking the wrong string, a double-press counter read too early).
- Keys are processed in order. To be sure an earlier key took effect, send a later one and wait
  for its visible effect. Never hold a fake's lock while the UI runs: `View()` reads it.
- Fit tests measure *wrapped* height (`renderedHeight`), not newlines.
- Checking the real binary in a pty: the fake terminal never answers the colour query, so the
  first screen takes about 5 seconds. Wait for a specific string such as `\d+ items`; plain
  "items" also matches "No items".

## Traps

- Go caches passing tests and cannot see external state. `tags_test.go` reads `.git/refs/tags`
  and `packed-refs` so the cache notices new tags.
- yt-dlp looks for browser profiles under `$XDG_CONFIG_HOME`. Pointing it at a temp dir in a
  real-YouTube check hides the profile (symlink the browser directory in instead).
- yt-dlp lists a removed playlist video with its id, a title of exactly `[Private video]` or
  `[Deleted video]`, and no channel. Real titles can start with `[` too (`[4K] ...`), so
  `Item.Unavailable` needs both the exact placeholder form and the missing channel.
- In a tmux check, send `Escape` on its own: followed at once by another key, it arrives as alt+key.
- yt-dlp with `--print` is quiet; progress needs `--progress`. The `download:` in a progress
  template is a selector, not output.
- mpv can ignore `quit` sent right after start; `Player.Close` kills it after 2 seconds.
- A GitHub run's summary can say success while a job never ran, and a job's `conclusion` can lag
  or be empty. Judge by each job's conclusion and steps.
- `gofmt` reformats files, so re-read the exact text before patching with search and replace.

## Releasing

1. Add a `## vX.Y.Z - YYYY-MM-DD` section to `CHANGELOG.md` (newest first) and its link at the
   bottom. Tests check the format and that every tag has a section.
2. Commit, push, and wait until **both** CI jobs have a real success conclusion.
3. Compare `git log vPREV..HEAD` with what the tag message will claim, then create an annotated
   tag (several `-m` paragraphs) and push it. Never move or force-push a published tag.
4. Releases page: `gh release create vX.Y.Z --verify-tag` with that version's changelog
   section, titled `vX.Y.Z: short description`.

## Style

- One commit per feature, message explaining why. Update the README, CHANGELOG entry and tests
  in the same change as the feature.
- Say what was and was not verified. Do not claim a result that was not observed.
