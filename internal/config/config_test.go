package config

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// env builds a fake environment. files maps path -> contents.
func env(vars map[string]string, files map[string]string) Env {
	return Env{
		LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
		ReadFile: func(p string) ([]byte, error) {
			if c, ok := files[p]; ok {
				return []byte(c), nil
			}
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
		},
	}
}

const home = "/home/u"
const defaultFile = "/home/u/.config/eartube/config.toml"

func TestPrecedence(t *testing.T) {
	file := map[string]string{defaultFile: `browser = "fromfile"`}
	cases := []struct {
		name  string
		vars  map[string]string
		files map[string]string
		args  []string
		want  string
	}{
		{"default is no cookies", map[string]string{"HOME": home}, nil, nil, ""},
		{"file", map[string]string{"HOME": home}, file, nil, "fromfile"},
		{"env beats file", map[string]string{"HOME": home, "EARTUBE_BROWSER": "fromenv"}, file, nil, "fromenv"},
		{"flag beats env", map[string]string{"HOME": home, "EARTUBE_BROWSER": "fromenv"}, file, []string{"--browser", "fromflag"}, "fromflag"},
		{"flag=value form", map[string]string{"HOME": home}, file, []string{"-browser=brave+gnomekeyring"}, "brave+gnomekeyring"},
		{"empty flag disables cookies", map[string]string{"HOME": home, "EARTUBE_BROWSER": "fromenv"}, file, []string{"--browser="}, ""},
		{"empty env disables cookies", map[string]string{"HOME": home, "EARTUBE_BROWSER": ""}, file, nil, ""},
		{"XDG_CONFIG_HOME wins over HOME", map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/x"},
			map[string]string{"/x/eartube/config.toml": `browser = "xdg"`, defaultFile: `browser = "home"`}, nil, "xdg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Load(c.args, env(c.vars, c.files), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if got.Browser != c.want {
				t.Errorf("Browser = %q, want %q", got.Browser, c.want)
			}
		})
	}
}

func TestBadInputIsRejected(t *testing.T) {
	vars := map[string]string{"HOME": home}
	cases := []struct {
		name, file string
		args       []string
		wantErr    string
	}{
		{"unknown key (typo)", `browsr = "brave"`, nil, "browsr"},
		{"invalid toml", `browser = `, nil, defaultFile},
		{"wrong type", `browser = 3`, nil, defaultFile},
		{"unknown flag", "", []string{"--nope"}, "nope"},
		{"stray argument", "", []string{"extra"}, "extra"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{}
			if c.file != "" {
				files[defaultFile] = c.file
			}
			_, err := Load(c.args, env(vars, files), io.Discard)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

func TestHelpShowsConfigLocation(t *testing.T) {
	var out bytes.Buffer
	_, err := Load([]string{"-h"}, env(map[string]string{"HOME": home}, nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{defaultFile, "EARTUBE_BROWSER", "EARTUBE_DOWNLOADS", "-browser", "-downloads", "-download-max-gb", "-delete-played", "-download-dir"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestUnreadableFileIsAnError(t *testing.T) {
	e := Env{
		LookupEnv: func(k string) (string, bool) { return home, k == "HOME" },
		ReadFile:  func(string) ([]byte, error) { return nil, os.ErrPermission },
	}
	if _, err := Load(nil, e, io.Discard); err == nil {
		t.Fatal("a permission error must not be silently ignored")
	}
}

func TestDownloadDefaults(t *testing.T) {
	got, err := Load(nil, env(map[string]string{"HOME": home}, nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got.Downloads || got.DeletePlayed {
		t.Errorf("downloads and delete_played must default to off: %+v", got)
	}
	if got.DownloadMaxGB != 5 || got.MaxBytes() != 5<<30 {
		t.Errorf("cap = %v GB (%d bytes), want 5 GB", got.DownloadMaxGB, got.MaxBytes())
	}
	if got.DownloadDir != "" {
		t.Errorf("download dir = %q, want empty (default chosen later)", got.DownloadDir)
	}
}

func TestDownloadSettingsPrecedence(t *testing.T) {
	file := map[string]string{defaultFile: "downloads = true\ndownload_max_gb = 2\ndelete_played = true\ndownload_dir = \"/from/file\"\n"}
	vars := func(kv ...string) map[string]string {
		m := map[string]string{"HOME": home}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	cases := []struct {
		name  string
		vars  map[string]string
		files map[string]string
		args  []string
		check func(Config) string
	}{
		{"file sets everything", vars(), file, nil, func(c Config) string {
			if !c.Downloads || c.DownloadMaxGB != 2 || !c.DeletePlayed || c.DownloadDir != "/from/file" {
				return "file values not applied"
			}
			return ""
		}},
		{"env beats file", vars("EARTUBE_DOWNLOADS", "false", "EARTUBE_DOWNLOAD_MAX_GB", "7.5",
			"EARTUBE_DELETE_PLAYED", "0", "EARTUBE_DOWNLOAD_DIR", "/from/env"), file, nil, func(c Config) string {
			if c.Downloads || c.DownloadMaxGB != 7.5 || c.DeletePlayed || c.DownloadDir != "/from/env" {
				return "env values not applied"
			}
			return ""
		}},
		{"flags beat env", vars("EARTUBE_DOWNLOAD_MAX_GB", "7.5", "EARTUBE_DOWNLOADS", "false"), file,
			[]string{"--downloads", "--download-max-gb=1", "--delete-played=false", "--download-dir", "/flag"}, func(c Config) string {
				if !c.Downloads || c.DownloadMaxGB != 1 || c.DeletePlayed || c.DownloadDir != "/flag" {
					return "flag values not applied"
				}
				return ""
			}},
		{"--downloads=false overrides the file", vars(), file, []string{"--downloads=false"}, func(c Config) string {
			if c.Downloads {
				return "downloads still on"
			}
			return ""
		}},
		{"an unset flag leaves the file alone", vars(), file, []string{"--browser", "x"}, func(c Config) string {
			if !c.Downloads || c.DownloadMaxGB != 2 {
				return "unrelated flag clobbered file settings"
			}
			return ""
		}},
		{"zero cap means unlimited", vars(), nil, []string{"--download-max-gb=0"}, func(c Config) string {
			if c.DownloadMaxGB != 0 || c.MaxBytes() != 0 {
				return "cap should be 0 (unlimited)"
			}
			return ""
		}},
		{"tilde is expanded", vars(), map[string]string{defaultFile: `download_dir = "~/Music/eartube"`}, nil, func(c Config) string {
			if c.DownloadDir != "/home/u/Music/eartube" {
				return "tilde not expanded: " + c.DownloadDir
			}
			return ""
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Load(c.args, env(c.vars, c.files), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if msg := c.check(got); msg != "" {
				t.Errorf("%s: %+v", msg, got)
			}
		})
	}
}

func TestBadDownloadSettingsAreRejected(t *testing.T) {
	cases := []struct {
		name    string
		vars    map[string]string
		file    string
		args    []string
		wantErr string
	}{
		{"negative cap in file", nil, "download_max_gb = -1", nil, "download_max_gb"},
		{"negative cap on the flag", nil, "", []string{"--download-max-gb=-3"}, "download_max_gb"},
		{"cap is not a number", map[string]string{"EARTUBE_DOWNLOAD_MAX_GB": "lots"}, "", nil, "EARTUBE_DOWNLOAD_MAX_GB"},
		{"downloads is not a bool", map[string]string{"EARTUBE_DOWNLOADS": "maybe"}, "", nil, "EARTUBE_DOWNLOADS"},
		{"delete_played wrong type in file", nil, `delete_played = "yes"`, nil, defaultFile},
		{"typo in a download key", nil, "download_max_gbs = 5", nil, "download_max_gbs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vars := map[string]string{"HOME": home}
			for k, v := range c.vars {
				vars[k] = v
			}
			files := map[string]string{}
			if c.file != "" {
				files[defaultFile] = c.file
			}
			_, err := Load(c.args, env(vars, files), io.Discard)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, c.wantErr)
			}
		})
	}
}
