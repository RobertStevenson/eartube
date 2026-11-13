package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
		"browser":             browser,
		"subscriptions_count": fmt.Sprintf("%d videos, %d Shorts", c.SubscriptionsCount, c.SubscriptionsCount),
		"downloads":           onOff(c.Downloads),
		"download_max_gb":     max,
		"delete_played":       onOff(c.DeletePlayed),
		"download_dir":        dir,
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

// settingDoc is one setting's documentation in the config file: an explanatory
// comment and a commented-out example at the default. Template is built from
// these, and the same blocks are appended for settings an older file lacks.
type settingDoc struct {
	Key     string
	Comment string // each line without the leading "# "
	Example string // the commented-out assignment, without the leading "# "
}

// settingDocs must list every config key, in SettingKeys order; a test enforces it.
var settingDocs = []settingDoc{
	{"browser", `Browser to read your YouTube login from, as a yt-dlp --cookies-from-browser
spec: brave, firefox, chromium, chrome, and so on. Needed to see your own
playlists; search works without it. On Omarchy, Chromium-family browsers also
need the keyring named, for example "brave+gnomekeyring".`, `browser = ""`},
	{"subscriptions_count", `How many of the newest uploads from your subscriptions to list (press f),
for videos and again for Shorts. Needs the browser setting. Nothing in the
list is saved.`, `subscriptions_count = 50`},
	{"downloads", `Offline downloads. Off by default, and always manual: press d on a video.`, `downloads = false`},
	{"download_max_gb", `Cap on the total size of downloaded audio, in GiB. When a download pushes past
it, the least recently played files are deleted first (never anything queued).
0 means unlimited.`, `download_max_gb = 5`},
	{"delete_played", `Delete a download as soon as its track is marked played.`, `delete_played = false`},
	{"download_dir", `Where downloads are kept. A leading ~/ is expanded. Default: the "downloads"
folder next to eartube's database (~/.local/share/eartube/downloads).`, `download_dir = ""`},
}

func (d settingDoc) block() string {
	var b strings.Builder
	for _, line := range strings.Split(d.Comment, "\n") {
		b.WriteString("# " + line + "\n")
	}
	b.WriteString("# " + d.Example + "\n")
	return b.String()
}

const templateHeader = `# eartube configuration.
#
# Restart eartube after changing this file. The same settings can also be given
# as flags (run "eartube -h") or EARTUBE_* environment variables, which take
# precedence over this file. Uncomment a line to change that setting.
`

// Template is the starter config file: every setting, commented out at its
// default, so the file is valid as is and each line is easy to enable.
func Template() string {
	var b strings.Builder
	b.WriteString(templateHeader)
	for _, d := range settingDocs {
		b.WriteString("\n" + d.block())
	}
	return b.String()
}

// mentions reports whether the file text mentions key, either as a setting or
// as a commented-out example.
func mentions(content, key string) bool {
	return regexp.MustCompile(`(?m)^\s*#?\s*` + regexp.QuoteMeta(key) + `\s*=`).MatchString(content)
}

// MissingKeys returns the settings that content does not mention at all, active
// or commented out, in SettingKeys order. A deliberately deleted example counts
// as missing; callers decide whether to keep asking.
func MissingKeys(content string) []string {
	var missing []string
	for _, k := range SettingKeys {
		if !mentions(content, k) {
			missing = append(missing, k)
		}
	}
	return missing
}

// MissingInFile is MissingKeys for the file at path. A file that does not exist
// has nothing to add to, so it reports no missing settings.
func MissingInFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return MissingKeys(string(data)), nil
}

// AddMissing appends the documentation blocks for settings the file does not
// mention, commented out at their defaults. It only ever appends: nothing that
// is already in the file is changed or moved. It reports the keys it added.
func AddMissing(path string) (added []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	added = MissingKeys(string(data))
	if len(added) == 0 {
		return nil, nil
	}
	// Exactly one blank line between what is already there and what we add,
	// whatever the file happens to end with.
	sep := "\n\n"
	switch text := string(data); {
	case len(text) == 0, strings.HasSuffix(text, "\n\n"):
		sep = ""
	case strings.HasSuffix(text, "\n"):
		sep = "\n"
	}
	var b strings.Builder
	b.WriteString(sep + "# Added by eartube: settings newer than this file, commented out at their defaults.\n")
	for _, k := range added {
		for _, d := range settingDocs {
			if d.Key == k {
				b.WriteString("\n" + d.block())
			}
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return nil, err
	}
	return added, f.Close()
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
