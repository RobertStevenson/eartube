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
	for _, want := range []string{defaultFile, "EARTUBE_BROWSER", "-browser"} {
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
