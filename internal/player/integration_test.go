//go:build integration

package player

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests drive a real, silent mpv (--ao=null):
//
//	go test -tags integration ./internal/player
//
// The local-file tests need only mpv. The YouTube test needs network and
// yt-dlp, and is skipped by -short.

func startPlayer(t *testing.T) *Player {
	t.Helper()
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	p, err := Start("", "--ao=null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// silentWAV writes a mono 8kHz 16-bit silent WAV of the given length. A real
// file is seekable with a known duration, unlike mpv's synthetic lavfi sources.
func silentWAV(t *testing.T, seconds int) string {
	t.Helper()
	const rate = 8000
	data := make([]byte, seconds*rate*2)
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(data)))
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], 1) // mono
	binary.LittleEndian.PutUint32(hdr[24:], rate)
	binary.LittleEndian.PutUint32(hdr[28:], rate*2)
	binary.LittleEndian.PutUint16(hdr[32:], 2)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(data)))
	path := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(path, append(hdr, data...), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitStatus(t *testing.T, p *Player, desc string, cond func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := p.Status(); cond(s) {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; status = %+v", desc, p.Status())
	return Status{}
}

func TestLoadReportsStatus(t *testing.T) {
	p := startPlayer(t)
	if s := p.Status(); !s.Idle {
		t.Fatalf("fresh player should be idle: %+v", s)
	}
	p.Load(silentWAV(t, 30), 0)
	s := waitStatus(t, p, "playing", func(s Status) bool { return !s.Idle && s.Duration > 0 && s.Pos > 0 })
	if s.Duration < 29.5 || s.Duration > 30.5 {
		t.Errorf("duration = %v, want ~30", s.Duration)
	}
}

func TestLoadWithStartOffset(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 30), 12)
	s := waitStatus(t, p, "position near 12", func(s Status) bool { return s.Pos >= 12 })
	if s.Pos > 14 {
		t.Errorf("pos = %v, want to start near 12", s.Pos)
	}
}

func TestSeek(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 60), 0)
	waitStatus(t, p, "playing", func(s Status) bool { return s.Pos > 0 })
	p.Seek(20)
	waitStatus(t, p, "forward seek", func(s Status) bool { return s.Pos >= 20 })
	p.Seek(-10)
	waitStatus(t, p, "backward seek", func(s Status) bool { return s.Pos >= 9 && s.Pos < 20 })
}

func TestPauseAndSpeed(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 30), 0)
	waitStatus(t, p, "playing", func(s Status) bool { return s.Pos > 0 })

	p.TogglePause()
	waitStatus(t, p, "paused", func(s Status) bool { return s.Paused })
	p.TogglePause()
	waitStatus(t, p, "resumed", func(s Status) bool { return !s.Paused })

	p.AddSpeed(0.5)
	waitStatus(t, p, "1.5x", func(s Status) bool { return s.Speed == 1.5 })
	p.AddSpeed(-5) // clamps instead of going to zero or negative
	waitStatus(t, p, "speed floor", func(s Status) bool { return s.Speed == 0.25 })
}

func TestLoadResetsStalePosition(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 30), 25)
	waitStatus(t, p, "late position", func(s Status) bool { return s.Pos >= 25 })
	p.Load(silentWAV(t, 30), 0)
	// Right after Load the old track's position must not leak into the new one.
	if s := p.Status(); s.Pos != 0 || s.Duration != 0 {
		t.Errorf("status after Load = %+v, want zeroed position/duration", s)
	}
}

func TestEndedFiresOnEOF(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 1), 0)
	select {
	case eof := <-p.Done():
		if !eof {
			t.Fatal("Done reported an error for a clean end")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no Done signal; status = %+v", p.Status())
	}
}

func TestEndedFiresWithErrorForBadFile(t *testing.T) {
	p := startPlayer(t)
	p.Load(filepath.Join(t.TempDir(), "missing.mp3"), 0)
	select {
	case eof := <-p.Done():
		if eof {
			t.Fatal("a missing file must not look like a clean end")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no Done signal for a missing file")
	}
}

func TestReplacingATrackDoesNotSignalEnded(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 30), 0)
	waitStatus(t, p, "playing", func(s Status) bool { return s.Pos > 0 })
	p.Load(silentWAV(t, 30), 0)
	waitStatus(t, p, "second track", func(s Status) bool { return s.Duration > 0 && s.Pos > 0 })
	select {
	case eof := <-p.Done():
		t.Fatalf("replace signalled Done(%v); the queue would skip a track", eof)
	case <-time.After(1500 * time.Millisecond):
	}
}

func TestCloseStopsMpvAndRemovesSocket(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	p, err := Start("", "--ao=null")
	if err != nil {
		t.Fatal(err)
	}
	sock := p.sock
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket should exist while running: %v", err)
	}
	p.Close()
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket still present after Close: %v", err)
	}
	if p.cmd.ProcessState == nil {
		t.Error("mpv process was not reaped by Close")
	}
}

// Closing immediately after Start used to hang forever: mpv ignores "quit"
// that arrives while it is still registering property observers.
func TestCloseRightAfterStartDoesNotHang(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	p, err := Start("", "--ao=null")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeGrace + 3*time.Second):
		t.Fatal("Close hung")
	}
}

func TestCloseAfterSettlingIsPrompt(t *testing.T) {
	p := startPlayer(t)
	p.Load(silentWAV(t, 30), 0)
	waitStatus(t, p, "playing", func(s Status) bool { return s.Pos > 0 })
	start := time.Now()
	p.Close() // a second Close from Cleanup must not panic or hang either
	if d := time.Since(start); d > time.Second {
		t.Errorf("Close took %v, want a prompt clean exit", d)
	}
}

func TestResolvesYouTubeURL(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
	p := startPlayer(t)
	p.Load("https://www.youtube.com/watch?v=jNQXAC9IVRw", 0) // "Me at the zoo", 19s
	s := waitStatus(t, p, "youtube playback", func(s Status) bool { return s.Duration > 0 && s.Pos > 0 })
	if s.Duration < 15 || s.Duration > 25 {
		t.Errorf("duration = %v, want ~19", s.Duration)
	}
}

// --- fallback stages -------------------------------------------------------

func expectPlaying(t *testing.T, p *Player, desc string) Status {
	t.Helper()
	return waitStatus(t, p, desc, func(s Status) bool { return !s.Idle && s.Duration > 0 && s.Pos > 0 })
}

func expectNoEnded(t *testing.T, p *Player, d time.Duration) {
	t.Helper()
	select {
	case eof := <-p.Done():
		t.Fatalf("unexpected Done(%v)", eof)
	case <-time.After(d):
	}
}

func TestLoadResolvedPlaysTheStream(t *testing.T) {
	p := startPlayer(t)
	wav := silentWAV(t, 30)
	// The page URL is bogus; only the stream can play, and ytdl=no must be accepted.
	p.LoadResolved(filepath.Join(t.TempDir(), "page-does-not-exist"), wav, 0)
	s := expectPlaying(t, p, "stream playing")
	if s.Duration < 29.5 || s.Duration > 30.5 {
		t.Errorf("duration = %v, want ~30", s.Duration)
	}
}

func TestLoadResolvedHonoursStartOffset(t *testing.T) {
	p := startPlayer(t)
	p.LoadResolved("unused", silentWAV(t, 30), 12)
	s := waitStatus(t, p, "position near 12", func(s Status) bool { return s.Pos >= 12 })
	if s.Pos > 14 {
		t.Errorf("pos = %v, want to start near 12", s.Pos)
	}
}

func TestStaleStreamFallsBackToThePageURL(t *testing.T) {
	p := startPlayer(t)
	wav := silentWAV(t, 30)
	// A dead stream URL (expired in real life); the page URL is playable.
	p.LoadResolved(wav, filepath.Join(t.TempDir(), "stale-stream"), 0)
	expectPlaying(t, p, "fallback playback")
	expectNoEnded(t, p, 500*time.Millisecond) // the queue must not see the failed first try
}

func TestOnlyReportsErrorAfterEveryStageFails(t *testing.T) {
	p, err := Start("some-browser", "--ao=null") // a browser is set, but local files never use it
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(p.Close)
	missing := filepath.Join(t.TempDir(), "missing.mp3")
	p.LoadResolved(missing, missing+".stream", 0)
	select {
	case eof := <-p.Done():
		if eof {
			t.Fatal("reported a clean end for a file that does not exist")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("never reported the failure")
	}
	expectNoEnded(t, p, 300*time.Millisecond) // and only once
}

// mpv must accept the length-prefixed ytdl-raw-options value used by the
// cookie stage. A local file ignores yt-dlp, so this checks only the syntax.
func TestCookieStageOptionSyntaxIsAccepted(t *testing.T) {
	p := startPlayer(t)
	p.browser = "brave+gnomekeyring"
	a := attempt{url: silentWAV(t, 30), stage: stageCookies, start: 3}
	p.mu.Lock()
	p.cur = &a
	p.mu.Unlock()
	p.run(a)
	s := expectPlaying(t, p, "playback with cookie-stage options")
	if s.Pos < 3 {
		t.Errorf("pos = %v, want the start option applied", s.Pos)
	}
}

func TestLoadFilePlaysTheLocalFile(t *testing.T) {
	p := startPlayer(t)
	// The page URL is bogus; only the downloaded file can play.
	p.LoadFile(filepath.Join(t.TempDir(), "page-does-not-exist"), silentWAV(t, 30), 0)
	s := expectPlaying(t, p, "local file playing")
	if s.Duration < 29.5 || s.Duration > 30.5 {
		t.Errorf("duration = %v, want ~30", s.Duration)
	}
}

func TestLoadFileHonoursStartOffset(t *testing.T) {
	p := startPlayer(t)
	p.LoadFile("unused", silentWAV(t, 30), 20)
	s := waitStatus(t, p, "position near 20", func(s Status) bool { return s.Pos >= 20 })
	if s.Pos > 22 {
		t.Errorf("pos = %v, want to start near 20", s.Pos)
	}
}

func TestMissingDownloadFallsBackToThePageURL(t *testing.T) {
	p := startPlayer(t)
	// The downloaded file was deleted behind our back; the page URL still plays.
	p.LoadFile(silentWAV(t, 30), filepath.Join(t.TempDir(), "deleted.m4a"), 0)
	expectPlaying(t, p, "fallback playback")
	expectNoEnded(t, p, 500*time.Millisecond)
}

func TestCorruptDownloadFallsBackToThePageURL(t *testing.T) {
	p := startPlayer(t)
	bad := filepath.Join(t.TempDir(), "corrupt.m4a")
	if err := os.WriteFile(bad, []byte("this is not audio at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.LoadFile(silentWAV(t, 30), bad, 0)
	expectPlaying(t, p, "fallback playback")
	expectNoEnded(t, p, 500*time.Millisecond)
}

// --- real YouTube (network) --------------------------------------------------

const zooVideo = "https://www.youtube.com/watch?v=jNQXAC9IVRw" // "Me at the zoo", 19s

func needNetwork(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}
}

// With a browser configured, a normal play must NOT pay the cookie-decryption
// cost (~6s on a typical setup). It would take 10s+ if cookies were used.
func TestPlainLoadDoesNotUseCookies(t *testing.T) {
	needNetwork(t)
	browser := os.Getenv("EARTUBE_BROWSER")
	if browser == "" {
		t.Skip("set EARTUBE_BROWSER (e.g. brave+gnomekeyring) to test the cookie-free fast path")
	}
	p, err := Start(browser, "--ao=null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	began := time.Now()
	p.Load(zooVideo, 0)
	expectPlaying(t, p, "youtube playback")
	took := time.Since(began)
	t.Logf("time to first audio: %v", took.Round(100*time.Millisecond))
	if took > 8*time.Second {
		t.Errorf("took %v: cookies are probably being decrypted on every play", took)
	}
}

// The last-resort stage must really work against YouTube, not just parse.
func TestCookieStageWorksAgainstYouTube(t *testing.T) {
	needNetwork(t)
	browser := os.Getenv("EARTUBE_BROWSER")
	if browser == "" {
		t.Skip("set EARTUBE_BROWSER (e.g. brave+gnomekeyring) to test the cookie stage")
	}
	p, err := Start(browser, "--ao=null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	a := attempt{url: zooVideo, stage: stageCookies}
	p.mu.Lock()
	p.cur = &a
	p.mu.Unlock()
	p.run(a)
	waitStatus(t, p, "playback via the cookie stage", func(s Status) bool { return !s.Idle && s.Duration > 0 && s.Pos > 0 })
}

// A stream URL found ahead of time should start much faster than resolving.
func TestPreResolvedStreamStartsFast(t *testing.T) {
	needNetwork(t)
	out, err := exec.Command("yt-dlp", "-g", "-f", "bestaudio/best", "--no-playlist", "--no-warnings", zooVideo).Output()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	stream := strings.TrimSpace(string(out))
	p := startPlayer(t)
	began := time.Now()
	p.LoadResolved(zooVideo, stream, 0)
	expectPlaying(t, p, "pre-resolved playback")
	took := time.Since(began)
	t.Logf("time to first audio from a pre-resolved stream: %v", took.Round(100*time.Millisecond))
	if took > 5*time.Second {
		t.Errorf("took %v; a pre-resolved stream should skip yt-dlp entirely", took)
	}
}
