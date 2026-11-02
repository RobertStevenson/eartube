//go:build integration

package player

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
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
