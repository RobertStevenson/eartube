// Package config resolves settings from flags, environment, and a config file.
//
// Precedence, highest first: command-line flag, environment variable, config
// file, built-in default.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// SettingKeys are the config-file keys, in the order the settings view lists them.
var SettingKeys = []string{"browser", "subscriptions_count", "downloads", "download_max_gb", "delete_played", "download_dir"}

// DefaultDownloadMaxGB is the offline-storage cap when none is configured.
const DefaultDownloadMaxGB = 5

// DefaultSubscriptionsCount is how many uploads the subscriptions view lists
// when none is configured.
const DefaultSubscriptionsCount = 50

type Config struct {
	// Browser is a yt-dlp --cookies-from-browser spec, e.g. "brave" or
	// "brave+gnomekeyring". Empty means no cookies (search only).
	Browser string `toml:"browser"`

	// SubscriptionsCount is how many of the newest uploads the subscriptions
	// view (f) lists, separately for videos and for Shorts.
	SubscriptionsCount int `toml:"subscriptions_count"`

	// Downloads enables manual downloading of audio for offline playback.
	// Off by default; nothing is ever downloaded automatically.
	Downloads bool `toml:"downloads"`
	// DownloadMaxGB caps the total size of downloaded audio, in GiB. When a
	// download pushes past it, the least recently played files are deleted.
	// 0 means no limit.
	DownloadMaxGB float64 `toml:"download_max_gb"`
	// DeletePlayed deletes a downloaded file once its track is marked played.
	DeletePlayed bool `toml:"delete_played"`
	// DownloadDir is where downloads live. Empty means the default under the
	// data directory; a leading "~/" is expanded.
	DownloadDir string `toml:"download_dir"`

	// Where the settings came from, for the in-app settings view. Not part of
	// the file format.
	Path       string            `toml:"-"` // the config file's location
	FileExists bool              `toml:"-"`
	Sources    map[string]string `toml:"-"` // setting key -> "default", "config file", "env EARTUBE_X" or "flag --x"
}

// MaxBytes is the download cap in bytes, or 0 for unlimited.
func (c Config) MaxBytes() int64 { return int64(c.DownloadMaxGB * (1 << 30)) }

// Env abstracts the process environment so Load can be tested.
type Env struct {
	LookupEnv func(string) (string, bool)
	ReadFile  func(string) ([]byte, error)
}

// OSEnv is the real process environment.
func OSEnv() Env { return Env{LookupEnv: os.LookupEnv, ReadFile: os.ReadFile} }

// Path is where the config file lives: $XDG_CONFIG_HOME/eartube/config.toml,
// falling back to ~/.config/eartube/config.toml.
func Path(env Env) string {
	if x, ok := env.LookupEnv("XDG_CONFIG_HOME"); ok && x != "" {
		return filepath.Join(x, "eartube", "config.toml")
	}
	home, _ := env.LookupEnv("HOME")
	return filepath.Join(home, ".config", "eartube", "config.toml")
}

// Load resolves the configuration. Usage and flag errors are written to out.
// It returns flag.ErrHelp if -h/--help was requested.
func Load(args []string, env Env, out io.Writer) (Config, error) {
	cfg := Config{DownloadMaxGB: DefaultDownloadMaxGB, SubscriptionsCount: DefaultSubscriptionsCount, Sources: map[string]string{}}
	for _, k := range SettingKeys {
		cfg.Sources[k] = "default"
	}

	// 1. Config file (lowest precedence). A missing file is fine.
	path := Path(env)
	cfg.Path = path
	data, err := env.ReadFile(path)
	switch {
	case err == nil:
		cfg.FileExists = true
		md, err := toml.Decode(string(data), &cfg)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		for _, k := range SettingKeys {
			if md.IsDefined(k) {
				cfg.Sources[k] = "config file"
			}
		}
		if u := md.Undecoded(); len(u) > 0 {
			keys := make([]string, len(u))
			for i, k := range u {
				keys[i] = k.String()
			}
			return Config{}, fmt.Errorf("%s: unknown setting(s): %s", path, strings.Join(keys, ", "))
		}
	case !errors.Is(err, os.ErrNotExist):
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	// 2. Environment.
	if err := applyEnv(env, &cfg); err != nil {
		return Config{}, err
	}

	// 3. Flags (highest). Visit tells us which flags were actually passed, so
	// an explicit --browser= or --downloads=false can override the file.
	fs := flag.NewFlagSet("eartube", flag.ContinueOnError)
	fs.SetOutput(out)
	browser := fs.String("browser", "", "browser to read YouTube cookies from, as a yt-dlp spec (e.g. brave, firefox, brave+gnomekeyring); empty disables cookies")
	subsCount := fs.Int("subscriptions-count", DefaultSubscriptionsCount, "how many of the newest uploads from your subscriptions to list (f), for videos and for Shorts")
	downloads := fs.Bool("downloads", false, "enable downloading audio for offline playback (press d)")
	maxGB := fs.Float64("download-max-gb", DefaultDownloadMaxGB, "cap on total downloaded audio in GiB; oldest-played files are deleted first; 0 = unlimited")
	deletePlayed := fs.Bool("delete-played", false, "delete a downloaded file once its track is marked played")
	dir := fs.String("download-dir", "", "directory for downloads (default: under the data directory)")
	fs.Usage = func() {
		fmt.Fprintf(out, "Usage: eartube [flags]\n       eartube archive [export ...]   saved video details (eartube archive -h)\n\nConfig file: %s\nEnvironment: EARTUBE_BROWSER, EARTUBE_SUBSCRIPTIONS_COUNT, EARTUBE_DOWNLOADS,\n             EARTUBE_DOWNLOAD_MAX_GB, EARTUBE_DELETE_PLAYED, EARTUBE_DOWNLOAD_DIR\n\nFlags:\n", path)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "browser":
			cfg.Browser = *browser
			cfg.Sources["browser"] = "flag --browser"
		case "subscriptions-count":
			cfg.SubscriptionsCount = *subsCount
			cfg.Sources["subscriptions_count"] = "flag --subscriptions-count"
		case "downloads":
			cfg.Downloads = *downloads
			cfg.Sources["downloads"] = "flag --downloads"
		case "download-max-gb":
			cfg.DownloadMaxGB = *maxGB
			cfg.Sources["download_max_gb"] = "flag --download-max-gb"
		case "delete-played":
			cfg.DeletePlayed = *deletePlayed
			cfg.Sources["delete_played"] = "flag --delete-played"
		case "download-dir":
			cfg.DownloadDir = *dir
			cfg.Sources["download_dir"] = "flag --download-dir"
		}
	})
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	// 4. Validate and normalise the merged result.
	if cfg.DownloadMaxGB < 0 {
		return Config{}, fmt.Errorf("download_max_gb must be 0 (unlimited) or positive, got %v", cfg.DownloadMaxGB)
	}
	if cfg.SubscriptionsCount < 1 {
		return Config{}, fmt.Errorf("subscriptions_count must be at least 1, got %d", cfg.SubscriptionsCount)
	}
	if rest, ok := strings.CutPrefix(cfg.DownloadDir, "~/"); ok {
		home, _ := env.LookupEnv("HOME")
		cfg.DownloadDir = filepath.Join(home, rest)
	}
	return cfg, nil
}

func applyEnv(env Env, cfg *Config) error {
	from := func(key, name string) { cfg.Sources[key] = "env " + name }
	if v, ok := env.LookupEnv("EARTUBE_BROWSER"); ok {
		cfg.Browser = v
		from("browser", "EARTUBE_BROWSER")
	}
	if v, ok := env.LookupEnv("EARTUBE_DOWNLOAD_DIR"); ok {
		cfg.DownloadDir = v
		from("download_dir", "EARTUBE_DOWNLOAD_DIR")
	}
	for _, b := range []struct {
		name, key string
		dst       *bool
	}{
		{"EARTUBE_DOWNLOADS", "downloads", &cfg.Downloads},
		{"EARTUBE_DELETE_PLAYED", "delete_played", &cfg.DeletePlayed},
	} {
		if v, ok := env.LookupEnv(b.name); ok {
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s=%q: want true or false", b.name, v)
			}
			*b.dst = parsed
			from(b.key, b.name)
		}
	}
	if v, ok := env.LookupEnv("EARTUBE_SUBSCRIPTIONS_COUNT"); ok {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("EARTUBE_SUBSCRIPTIONS_COUNT=%q: want a whole number", v)
		}
		cfg.SubscriptionsCount = parsed
		from("subscriptions_count", "EARTUBE_SUBSCRIPTIONS_COUNT")
	}
	if v, ok := env.LookupEnv("EARTUBE_DOWNLOAD_MAX_GB"); ok {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("EARTUBE_DOWNLOAD_MAX_GB=%q: want a number", v)
		}
		cfg.DownloadMaxGB = parsed
		from("download_max_gb", "EARTUBE_DOWNLOAD_MAX_GB")
	}
	return nil
}
