package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Result describes a finished download.
type Result struct {
	Path  string
	Bytes int64
}

// Fetcher downloads audio with yt-dlp into Dir.
type Fetcher struct {
	Dir     string
	Browser string // yt-dlp cookie spec; used only if a cookie-free try fails
}

// validID keeps video IDs safe to use as file names.
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Fetch downloads the audio of url to Dir/<id>.<ext>, reporting progress as a
// percentage. Like playback, it tries without cookies first, since decrypting
// them is slow, and only retries with them if that fails.
func (f Fetcher) Fetch(ctx context.Context, id, url string, progress func(pct float64)) (Result, error) {
	if !validID.MatchString(id) {
		return Result{}, fmt.Errorf("unsafe video id %q", id)
	}
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return Result{}, err
	}
	res, err := f.run(ctx, id, url, "", progress)
	if err != nil && f.Browser != "" && ctx.Err() == nil {
		var err2 error
		if res, err2 = f.run(ctx, id, url, f.Browser, progress); err2 == nil {
			return res, nil
		}
		return Result{}, fmt.Errorf("%w (retried with cookies: %v)", err, err2)
	}
	return res, err
}

func (f Fetcher) args(id, url, browser string) []string {
	args := []string{
		"-f", "bestaudio/best", "--no-playlist", "--no-warnings",
		// --print makes yt-dlp quiet, so progress must be asked for explicitly.
		// The "download:" below only selects the template type; our own marker
		// keeps progress lines distinguishable from the printed file path.
		"--newline", "--progress", "--progress-template", "download:" + progressMarker + " %(progress._percent_str)s",
		"-o", filepath.Join(f.Dir, id+".%(ext)s"),
		"--no-simulate", "--print", "after_move:filepath",
	}
	if browser != "" {
		args = append(args, "--cookies-from-browser", browser)
	}
	return append(args, url)
}

func (f Fetcher) run(ctx context.Context, id, url, browser string, progress func(float64)) (Result, error) {
	cmd := exec.CommandContext(ctx, "yt-dlp", f.args(id, url, browser)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	var path string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if pct, ok := parseProgress(line); ok {
			if progress != nil {
				progress(pct)
			}
		} else if line != "" {
			path = line // the last non-progress line is the final file path
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("download failed: %s", lastLine(stderr.String(), err))
	}
	return finish(f.Dir, id, path)
}

// progressMarker prefixes the progress lines we ask yt-dlp to print.
const progressMarker = "eartube-progress"

// parseProgress reads "eartube-progress  42.3%" lines.
func parseProgress(line string) (float64, bool) {
	rest, ok := strings.CutPrefix(line, progressMarker)
	if !ok {
		return 0, false
	}
	pct, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(rest), "%"), 64)
	if err != nil {
		return 0, false
	}
	return pct, true
}

func lastLine(s string, fallback error) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return fallback.Error()
}

// finish locates and sizes the downloaded file. yt-dlp prints its final path;
// if that is missing or odd, fall back to looking for Dir/<id>.* directly.
func finish(dir, id, printed string) (Result, error) {
	for _, p := range []string{printed} {
		if p != "" && filepath.Dir(p) == filepath.Clean(dir) && !isTemp(p) {
			if st, err := os.Stat(p); err == nil && st.Size() > 0 {
				return Result{Path: p, Bytes: st.Size()}, nil
			}
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, id+".*"))
	for _, p := range matches {
		if isTemp(p) {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return Result{Path: p, Bytes: st.Size()}, nil
		}
	}
	return Result{}, errors.New("download finished but no audio file was found")
}

// isTemp reports yt-dlp's in-progress files, which must never be treated as done.
func isTemp(p string) bool {
	return strings.HasSuffix(p, ".part") || strings.HasSuffix(p, ".ytdl") || strings.Contains(filepath.Base(p), ".part-")
}
