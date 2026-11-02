package player

import "testing"

func TestNewAttemptStartsAtTheRightStage(t *testing.T) {
	if a := newAttempt("https://y/v", "", 0); a.stage != stagePlain {
		t.Errorf("no stream: stage = %v, want plain", a.stage)
	}
	if a := newAttempt("https://y/v", "https://s", 0); a.stage != stageDirect {
		t.Errorf("with stream: stage = %v, want direct", a.stage)
	}
}

func TestFallbackChain(t *testing.T) {
	cases := []struct {
		name    string
		a       attempt
		browser string
		want    []stage // stages tried after the first failure, in order
	}{
		{"resolved web video, browser set", newAttempt("https://y/v", "https://s", 0), "brave", []stage{stagePlain, stageCookies}},
		{"resolved web video, no browser", newAttempt("https://y/v", "https://s", 0), "", []stage{stagePlain}},
		{"plain web video, browser set", newAttempt("https://y/v", "", 0), "brave", []stage{stageCookies}},
		{"plain web video, no browser", newAttempt("https://y/v", "", 0), "", nil},
		{"local file never tries cookies", newAttempt("/music/a.mp3", "", 0), "brave", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []stage
			a := c.a
			for {
				n, ok := a.next(c.browser)
				if !ok {
					break
				}
				got = append(got, n.stage)
				a = n
			}
			if len(got) != len(c.want) {
				t.Fatalf("stages = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("stages = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestAttemptTargetAndOptions(t *testing.T) {
	const browser = "brave+gnomekeyring"
	cookieOpt := "cookies-from-browser=" + browser
	cases := []struct {
		name       string
		a          attempt
		wantTarget string
		wantOpts   string
	}{
		{"plain from the start", attempt{url: "U", stage: stagePlain}, "U", ""},
		{"plain with resume", attempt{url: "U", start: 12.34, stage: stagePlain}, "U", "start=12.3"},
		{"direct uses the stream and skips yt-dlp", attempt{url: "U", stream: "S", stage: stageDirect}, "S", "ytdl=no"},
		{"direct with resume", attempt{url: "U", stream: "S", start: 5, stage: stageDirect}, "S", "start=5.0,ytdl=no"},
		{"cookies pass the spec length-prefixed", attempt{url: "U", stream: "S", stage: stageCookies}, "U",
			"ytdl-raw-options=%" + itoa(len(cookieOpt)) + "%" + cookieOpt},
		{"cookies with resume", attempt{url: "U", start: 7, stage: stageCookies}, "U",
			"start=7.0,ytdl-raw-options=%" + itoa(len(cookieOpt)) + "%" + cookieOpt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.target(); got != c.wantTarget {
				t.Errorf("target = %q, want %q", got, c.wantTarget)
			}
			if got := c.a.options(browser); got != c.wantOpts {
				t.Errorf("options = %q, want %q", got, c.wantOpts)
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
