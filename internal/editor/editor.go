// Package editor finds the user's text editor and builds the command to open a file.
package editor

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// fallbacks are tried, in order, when neither $VISUAL nor $EDITOR is set.
var fallbacks = []string{"nano", "vim", "vi"}

// Command returns the command that opens path in the user's editor: $VISUAL,
// then $EDITOR, then the first of nano, vim and vi that is installed.
//
// The variable may carry arguments ("code --wait"), split on whitespace; quoting
// inside it is not interpreted. lookupEnv and lookPath are injected for testing.
func Command(path string, lookupEnv func(string) (string, bool), lookPath func(string) (string, error)) (*exec.Cmd, error) {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		v, _ := lookupEnv(name)
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		bin, err := lookPath(fields[0])
		if err != nil {
			return nil, fmt.Errorf("$%s is %q, which is not installed or not on your PATH", name, fields[0])
		}
		return exec.Command(bin, append(fields[1:], path)...), nil
	}
	for _, name := range fallbacks {
		if bin, err := lookPath(name); err == nil {
			return exec.Command(bin, path), nil
		}
	}
	return nil, errors.New("no editor found: set $EDITOR (or $VISUAL) to your editor, e.g. export EDITOR=nano")
}
