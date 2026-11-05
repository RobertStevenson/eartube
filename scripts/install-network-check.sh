#!/usr/bin/env bash
# Installs (or removes) a weekly systemd *user* timer that runs network-check.sh.
#
#   scripts/install-network-check.sh               install and start the timer
#   scripts/install-network-check.sh --browser brave+gnomekeyring
#   scripts/install-network-check.sh --uninstall   stop and remove it (logs are kept)
#
# It only writes under ~/.config/systemd/user and ~/.local/share/eartube, and
# needs no root. Re-running it updates the installed copy of the script.
set -eu

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
unit_dir="$HOME/.config/systemd/user"
app_dir="$HOME/.local/share/eartube"
installed_script="$app_dir/network-check.sh"
service="eartube-network-check.service"
timer="eartube-network-check.timer"

mode=install
browser=""
browser_given=0
while [ $# -gt 0 ]; do
  case "$1" in
    --uninstall) mode=uninstall ;;
    --browser) shift; browser="${1:-}"; browser_given=1 ;;
    -h|--help) sed -n '2,10p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift || true # --browser as the last argument leaves nothing to shift
done

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl not found: this needs systemd." >&2
  exit 1
fi

if [ "$mode" = uninstall ]; then
  systemctl --user disable --now "$timer" >/dev/null 2>&1 || true
  rm -f "$unit_dir/$service" "$unit_dir/$timer" "$installed_script"
  systemctl --user daemon-reload
  echo "Removed the weekly network check. Its log and results in"
  echo "${XDG_STATE_HOME:-$HOME/.local/state}/eartube were kept."
  exit 0
fi

# Use the same browser eartube is configured with unless told otherwise.
if [ "$browser_given" -eq 0 ]; then
  cfg="${XDG_CONFIG_HOME:-$HOME/.config}/eartube/config.toml"
  if [ -f "$cfg" ]; then
    browser="$(sed -n 's/^[[:space:]]*browser[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$cfg" | head -1)"
  fi
fi

# A systemd user service does not get your shell's PATH, and Go is often a
# version-manager shim. Resolve where the real tools are now and fix them in.
tool_dirs=""
add_dir() { case ":$tool_dirs:" in *":$1:"*) ;; *) tool_dirs="${tool_dirs:+$tool_dirs:}$1" ;; esac; }
if command -v go >/dev/null 2>&1; then
  goroot="$(go env GOROOT 2>/dev/null || true)"
  if [ -n "$goroot" ] && [ -x "$goroot/bin/go" ]; then add_dir "$goroot/bin"; else add_dir "$(dirname "$(command -v go)")"; fi
else
  echo "warning: go was not found on PATH; the check will fail until it is." >&2
fi
for tool in git yt-dlp mpv curl notify-send; do
  if command -v "$tool" >/dev/null 2>&1; then add_dir "$(dirname "$(command -v "$tool")")"; fi
done
service_path="$tool_dirs:/usr/local/bin:/usr/bin:/bin"

mkdir -p "$unit_dir" "$app_dir"
install -m 0755 "$here/network-check.sh" "$installed_script"

cat >"$unit_dir/$service" <<UNIT
[Unit]
Description=Check that eartube still works against real YouTube
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=$installed_script
Environment=PATH=$service_path
Environment=EARTUBE_BROWSER=$browser
TimeoutStartSec=20min
Nice=10
UNIT

cat >"$unit_dir/$timer" <<UNIT
[Unit]
Description=Weekly eartube network check

[Timer]
OnCalendar=Mon *-*-* 09:30:00
RandomizedDelaySec=20min
Persistent=true

[Install]
WantedBy=timers.target
UNIT

systemctl --user daemon-reload
systemctl --user enable --now "$timer"

echo "Installed. The check runs weekly (Mondays about 09:30, or at the next login if the"
echo "machine was off) and shows a notification only if something is wrong."
echo
echo "  browser used for signed-in tests: ${browser:-<none: those tests are skipped>}"
echo "  run it now:     systemctl --user start $service"
echo "  see the result: cat ${XDG_STATE_HOME:-$HOME/.local/state}/eartube/last-result"
echo "  full log:       ${XDG_STATE_HOME:-$HOME/.local/state}/eartube/network-check.log"
echo "  next run:       systemctl --user list-timers $timer"
echo "  remove it:      $here/install-network-check.sh --uninstall"
