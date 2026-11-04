package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Row is one setting as the settings view shows it.
type Row struct {
	Key    string // the config-file key
	Value  string // human-readable current value
	Source string // "default", "config file", "env EARTUBE_X", or "flag --x"
}

// Rows describes the effective settings. defaultDownloadDir is shown when no
// directory was configured, so the view can say where downloads really go.
func (c Config) Rows(defaultDownloadDir string) []Row {
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	browser := c.Browser
	if browser == "" {
		browser = "(none: search only)"
	}
	max := fmt.Sprintf("%g GiB", c.DownloadMaxGB)
	if c.DownloadMaxGB == 0 {
		max = "unlimited"
	}
	dir := c.DownloadDir
	if dir == "" {
		dir = defaultDownloadDir
	}
	vals := map[string]string{
		"browser":         browser,
		"downloads":       onOff(c.Downloads),
		"download_max_gb": max,
		"delete_played":   onOff(c.DeletePlayed),
		"download_dir":    dir,
	}
	rows := make([]Row, 0, len(SettingKeys))
	for _, k := range SettingKeys {
		src := c.Sources[k]
		if src == "" {
			src = "default"
		}
		rows = append(rows, Row{Key: k, Value: vals[k], Source: src})
	}
	return rows
}

// Template is the starter config file: every setting, commented out at its
// default, so the file is valid as is and each line is easy to enable.
func Template() string {
	return `# eartube configuration.
#
# Restart eartube after changing this file. The same settings can also be given
# as flags (run "eartube -h") or EARTUBE_* environment variables, which take
# precedence over this file. Uncomment a line to change that setting.

# Browser to read your YouTube login from, as a yt-dlp --cookies-from-browser
# spec: brave, firefox, chromium, chrome, and so on. Needed to see your own
# playlists; search works without it. On Omarchy, Chromium-family browsers also
# need the keyring named, for example "brave+gnomekeyring".
# browser = ""

# Offline downloads. Off by default, and always manual: press d on a video.
# downloads = false

# Cap on the total size of downloaded audio, in GiB. When a download pushes past
# it, the least recently played files are deleted first (never anything queued).
# 0 means unlimited.
# download_max_gb = 5

# Delete a download as soon as its track is marked played.
# delete_played = false

# Where downloads are kept. A leading ~/ is expanded. Default: the "downloads"
# folder next to eartube's database (~/.local/share/eartube/downloads).
# download_dir = ""
`
}

// EnsureFile creates the config file from Template if it does not exist yet. It
// never overwrites an existing file. created reports whether it wrote one.
func EnsureFile(path string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(Template()); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// Check reports whether the current configuration loads: the file parses with
// no unknown keys, and every value (including environment overrides) is valid.
// Flags are not consulted, since they belong to a particular launch.
func Check(env Env) error {
	_, err := Load(nil, env, discard{})
	return err
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
