package player

import (
	"fmt"
	"strings"
)

// A play is tried in up to three stages, cheapest first. Each failure moves to
// the next stage; only when all fail does the queue see an error.
type stage int

const (
	// stageDirect plays a stream URL that was already resolved in the
	// background, skipping yt-dlp entirely (instant start).
	stageDirect stage = iota
	// stagePlain lets mpv's yt-dlp hook resolve the page URL without cookies
	// (~4s). Decrypting browser cookies adds ~6s per play, so avoid it by default.
	stagePlain
	// stageCookies resolves with the browser's cookies, for videos that need a
	// sign-in (private, age-restricted, members-only).
	stageCookies
)

type attempt struct {
	url    string  // the video's page URL
	stream string  // pre-resolved direct stream URL, or ""
	start  float64 // resume offset in seconds
	stage  stage
}

func newAttempt(url, stream string, start float64) attempt {
	a := attempt{url: url, stream: stream, start: start, stage: stagePlain}
	if stream != "" {
		a.stage = stageDirect
	}
	return a
}

func isRemote(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// next returns the attempt to try after this one failed. The cookie stage is
// only offered for web URLs and when a browser is configured.
func (a attempt) next(browser string) (attempt, bool) {
	switch a.stage {
	case stageDirect:
		a.stage = stagePlain
		return a, true
	case stagePlain:
		if browser != "" && isRemote(a.url) {
			a.stage = stageCookies
			return a, true
		}
	}
	return a, false
}

// target is what mpv is asked to open at this stage.
func (a attempt) target() string {
	if a.stage == stageDirect {
		return a.stream
	}
	return a.url
}

// options builds mpv's per-file option list for loadfile ("" if none).
func (a attempt) options(browser string) string {
	var opts []string
	if a.start > 0 {
		opts = append(opts, fmt.Sprintf("start=%.1f", a.start))
	}
	switch a.stage {
	case stageDirect:
		opts = append(opts, "ytdl=no")
	case stageCookies:
		// %N% length-prefixes the value so '=' and ',' inside it are literal.
		v := "cookies-from-browser=" + browser
		opts = append(opts, fmt.Sprintf("ytdl-raw-options=%%%d%%%s", len(v), v))
	}
	return strings.Join(opts, ",")
}
