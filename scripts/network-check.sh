#!/usr/bin/env bash
# Weekly check that eartube still works against real YouTube.
#
# It tests a fresh clone of the published main branch (never your working copy,
# so a half-finished edit cannot cause a false alarm) with the yt-dlp and mpv
# installed on this machine, which is exactly what you use. A desktop
# notification appears only when something is wrong. Everything it did is in
# the log. Installed and scheduled by scripts/install-network-check.sh.
#
# Exit status: 0 = passed or skipped (offline); 1 = something needs attention.
#
# The EARTUBE_CHECK_* variables replace individual steps; they exist so the
# script can be tested without a network, and are not needed in normal use.
set -u

state_dir="${EARTUBE_CHECK_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/eartube}"
repo_url="${EARTUBE_CHECK_REPO_URL:-https://github.com/RobertStevenson/eartube.git}"
branch="${EARTUBE_CHECK_BRANCH:-main}"
notifier="${EARTUBE_CHECK_NOTIFY:-notify-send}"
zoo="https://www.youtube.com/watch?v=jNQXAC9IVRw" # "Me at the zoo", 19s, public

mkdir -p "$state_dir"
log_file="$state_dir/network-check.log"
last_result="$state_dir/last-result"
last_failure="$state_dir/last-failure.log"

log() { printf '%s  %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$log_file"; }

# Keep the log from growing for ever: when it passes 200 KB, keep the newest 100 KB.
if [ -f "$log_file" ] && [ "$(wc -c <"$log_file")" -gt 200000 ]; then
  tail -c 100000 "$log_file" >"$log_file.tmp" && mv "$log_file.tmp" "$log_file"
fi

# notify TITLE BODY: a critical desktop notification. Missing or failing
# notifiers are logged, never fatal.
notify() {
  if command -v "$notifier" >/dev/null 2>&1; then
    "$notifier" -a eartube -u critical "$1" "$2" || log "notification failed"
  else
    log "notification unavailable ($notifier not found): $1: $2"
  fi
}

finish() { # finish STATUS MESSAGE EXITCODE
  printf '%s  %s  %s\n' "$(date '+%Y-%m-%d %H:%M')" "$1" "$2" >"$last_result"
  log "$1: $2"
  exit "$3"
}

log "---- network check starting"

# 1. No connection is not a failure of eartube: say so quietly and stop.
online_cmd="${EARTUBE_CHECK_ONLINE_CMD:-curl -fsS -m 15 -o /dev/null https://www.youtube.com/}"
if ! bash -c "$online_cmd" >/dev/null 2>&1; then
  finish "SKIPPED" "no connection to YouTube; nothing was tested" 0
fi

# 2. A fresh clone of the published branch.
work="$(mktemp -d)" || finish "ERROR" "could not create a temporary directory" 1
trap 'rm -rf "$work"' EXIT
if ! clone_out="$(git clone --quiet --depth 1 --branch "$branch" "$repo_url" "$work/eartube" 2>&1)"; then
  log "git clone failed: $clone_out"
  notify "eartube network check could not run" "Could not fetch $repo_url ($branch). See $log_file"
  finish "ERROR" "could not fetch the repository" 1
fi
sha="$(git -C "$work/eartube" rev-parse --short HEAD)"
versions="yt-dlp $(yt-dlp --version 2>/dev/null || echo '?'), $(mpv --version 2>/dev/null | head -1 | cut -d' ' -f1-2 || echo 'mpv ?')"
log "testing $branch@$sha with $versions"

# 3. Is YouTube asking this machine to prove it is not a bot? That is worth
# knowing, but it is not a bug in eartube, so report it as its own thing.
preflight_cmd="${EARTUBE_CHECK_PREFLIGHT_CMD:-yt-dlp -g -f bestaudio --no-warnings $zoo}"
if ! pre_out="$(bash -c "$preflight_cmd" 2>&1)"; then
  if printf '%s' "$pre_out" | grep -qiE "not a bot|sign in to confirm"; then
    notify "eartube network check: YouTube wants a sign-in" "YouTube is asking this machine to prove it is not a bot, so nothing was tested. This says nothing about the code."
    finish "BLOCKED" "YouTube asked for a sign-in (bot check); nothing was tested" 1
  fi
  log "preflight could not resolve the test video, running the tests anyway so the real failure is reported: $(printf '%s' "$pre_out" | tail -1)"
fi

# 4. The real tests, in the clone.
test_cmd="${EARTUBE_CHECK_TEST_CMD:-go test -tags integration -race -v -timeout 600s ./internal/ytdlp ./internal/download ./internal/player}"
out="$work/test-output.txt"
( cd "$work/eartube" && bash -c "$test_cmd" ) >"$out" 2>&1
code=$?

if [ "$code" -eq 0 ]; then
  finish "PASS" "$branch@$sha ($versions)" 0
fi

# 5. Something failed: keep the whole output and say which tests.
cp "$out" "$last_failure"
if [ "$code" -eq 127 ]; then
  # "command not found": almost always a tool that moved, e.g. Go after an upgrade.
  notify "eartube network check could not run" "A tool was not found (usually Go after an upgrade). Re-run scripts/install-network-check.sh to refresh the timer. Details: $last_failure"
  finish "ERROR" "a required tool was not found; re-run install-network-check.sh" 1
fi
failed="$(grep -E '^--- FAIL' "$out" | sed -E 's/^--- FAIL: ([^ ]+).*/\1/' | sort -u | head -5 | paste -sd, -)"
[ -n "$failed" ] || failed="the test run itself failed (see the log)"
notify "eartube network check FAILED" "$failed. $branch@$sha with $versions. Full output: $last_failure"
log "failing tests: $failed"
finish "FAIL" "$branch@$sha ($versions): $failed" 1
