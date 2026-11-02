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
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	// Browser is a yt-dlp --cookies-from-browser spec, e.g. "brave" or
	// "brave+gnomekeyring". Empty means no cookies (search only).
	Browser string `toml:"browser"`
}

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
	var cfg Config

	// 1. Config file (lowest precedence). A missing file is fine.
	path := Path(env)
	data, err := env.ReadFile(path)
	switch {
	case err == nil:
		md, err := toml.Decode(string(data), &cfg)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
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
	if v, ok := env.LookupEnv("EARTUBE_BROWSER"); ok {
		cfg.Browser = v
	}

	// 3. Flags (highest). Visit tells us whether a flag was actually passed, so
	// an explicit --browser= can turn cookies off even if the file sets one.
	fs := flag.NewFlagSet("eartube", flag.ContinueOnError)
	fs.SetOutput(out)
	browser := fs.String("browser", "", "browser to read YouTube cookies from, as a yt-dlp spec (e.g. brave, firefox, brave+gnomekeyring); empty disables cookies")
	fs.Usage = func() {
		fmt.Fprintf(out, "Usage: eartube [flags]\n\nConfig file: %s\nEnvironment: EARTUBE_BROWSER\n\nFlags:\n", path)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "browser" {
			cfg.Browser = *browser
		}
	})
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return cfg, nil
}
