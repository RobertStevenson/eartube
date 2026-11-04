package editor

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// installed fakes a PATH containing exactly these programs.
func installed(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, have := range names {
			if n == have {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestCommand(t *testing.T) {
	const file = "/home/u/.config/eartube/config.toml"
	cases := []struct {
		name string
		env  map[string]string
		path []string // programs on PATH
		want []string // expected argv
	}{
		{"VISUAL wins over EDITOR", map[string]string{"VISUAL": "vim", "EDITOR": "nano"}, []string{"vim", "nano"}, []string{"/usr/bin/vim", file}},
		{"EDITOR when there is no VISUAL", map[string]string{"EDITOR": "nano"}, []string{"nano"}, []string{"/usr/bin/nano", file}},
		{"an empty VISUAL is ignored", map[string]string{"VISUAL": "", "EDITOR": "nano"}, []string{"nano"}, []string{"/usr/bin/nano", file}},
		{"a blank VISUAL is ignored", map[string]string{"VISUAL": "   ", "EDITOR": "nano"}, []string{"nano"}, []string{"/usr/bin/nano", file}},
		{"arguments are kept, and the file goes last", map[string]string{"EDITOR": "code --wait --new-window"}, []string{"code"},
			[]string{"/usr/bin/code", "--wait", "--new-window", file}},
		{"a path with spaces is one argument", map[string]string{"EDITOR": "nano"}, []string{"nano"}, []string{"/usr/bin/nano", file}},
		{"falls back to nano first", nil, []string{"vi", "nano", "vim"}, []string{"/usr/bin/nano", file}},
		{"then vim", nil, []string{"vi", "vim"}, []string{"/usr/bin/vim", file}},
		{"then vi", nil, []string{"vi"}, []string{"/usr/bin/vi", file}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, err := Command(file, envOf(c.env), installed(c.path...))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cmd.Args[1:], c.want[1:]) || cmd.Path != c.want[0] {
				t.Errorf("got %v (%s), want %v", cmd.Args, cmd.Path, c.want)
			}
		})
	}
}

func TestACommandIsNotInterpretedByAShell(t *testing.T) {
	// A hostile or odd value must stay data: no shell is involved, so ';' is just an argument.
	cmd, err := Command("/f", envOf(map[string]string{"EDITOR": "nano; rm -rf ~"}), installed("nano;"))
	if err != nil {
		t.Skip("not found on PATH under that name, which is also safe:", err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "rm") || cmd.Path != "/usr/bin/nano;" {
		t.Fatalf("unexpected command %v", cmd.Args)
	}
}

func TestErrors(t *testing.T) {
	t.Run("an EDITOR that is not installed is named in the error", func(t *testing.T) {
		_, err := Command("/f", envOf(map[string]string{"EDITOR": "emacs-not-here"}), installed("nano"))
		if err == nil || !strings.Contains(err.Error(), "$EDITOR") || !strings.Contains(err.Error(), "emacs-not-here") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a bad VISUAL is reported rather than silently using EDITOR", func(t *testing.T) {
		_, err := Command("/f", envOf(map[string]string{"VISUAL": "gone", "EDITOR": "nano"}), installed("nano"))
		if err == nil || !strings.Contains(err.Error(), "$VISUAL") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("nothing installed says how to fix it", func(t *testing.T) {
		_, err := Command("/f", envOf(nil), installed())
		if err == nil || !strings.Contains(err.Error(), "EDITOR") {
			t.Fatalf("err = %v", err)
		}
	})
}
