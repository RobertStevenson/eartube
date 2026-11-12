package archive

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/RobertStevenson/eartube/internal/store"
)

const usage = `Usage:
  eartube archive                 how many videos are saved
  eartube archive export [flags]  write the saved details out

Export flags:
`

// Run implements "eartube archive ...". args are the words after "archive".
// It returns flag.ErrHelp after printing usage for -h.
func Run(args []string, db *store.Store, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return summary(db, stdout)
	}
	fs := flag.NewFlagSet("eartube archive export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "json or markdown")
	onlyGone := fs.Bool("unavailable", false, "only videos that are no longer on YouTube")
	output := fs.String("output", "", "file to write (default: standard output)")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	switch args[0] {
	case "export":
	case "-h", "-help", "--help", "help":
		fs.Usage()
		return flag.ErrHelp
	default:
		fs.Usage()
		return fmt.Errorf("unknown archive command %q", args[0])
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	vs, err := db.AllVideos()
	if err != nil {
		return err
	}
	if *output == "" {
		return Export(stdout, vs, *format, *onlyGone)
	}
	// Write beside the target and rename, so a failed export never leaves a
	// half-written file in place of a good one.
	tmp, err := os.CreateTemp(filepath.Dir(*output), ".eartube-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = Export(tmp, vs, *format, *onlyGone)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), *output)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s\n", *output)
	return nil
}

func summary(db *store.Store, w io.Writer) error {
	vs, err := db.AllVideos()
	if err != nil {
		return err
	}
	var full, gone, goneSaved int
	for _, v := range vs {
		if v.HasDetails() {
			full++
		}
		if !v.GoneAt.IsZero() {
			gone++
			if v.Title != "" {
				goneSaved++
			}
		}
	}
	if len(vs) == 0 {
		_, err := fmt.Fprintln(w, "No videos saved yet: open a playlist in eartube, or press I to read them all.")
		return err
	}
	_, err = fmt.Fprintf(w, "%d videos saved from your playlists, %d with full details.\n"+
		"%d are no longer on YouTube; eartube had saved %d of those before they went.\n\n"+
		"Export them with: eartube archive export [--format markdown] [--unavailable] [--output FILE]\n",
		len(vs), full, gone, goneSaved)
	return err
}
