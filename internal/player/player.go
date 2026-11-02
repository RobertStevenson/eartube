// Package player drives a headless mpv over its JSON IPC socket.
package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Status struct {
	Pos, Duration, Speed float64
	Paused, Idle         bool
}

type Player struct {
	cmd  *exec.Cmd
	conn net.Conn
	sock string

	// Ended receives when a track finishes on its own: true for EOF, false for a
	// playback error once every fallback stage has failed. It does not fire for
	// stop/replace, so loading a new track won't advance the queue.
	Ended chan bool

	browser string // yt-dlp cookie spec, used only by the last-resort stage

	mu  sync.Mutex
	st  Status
	cur *attempt // the play in progress, so a failure can try the next stage
}

// Start launches mpv in idle, audio-only mode. browser is a yt-dlp
// --cookies-from-browser spec (empty disables). Cookies are NOT used for every
// play, because decrypting them takes seconds; they are only tried after a
// cookie-less attempt fails. extraArgs are appended to mpv's command line,
// e.g. "--ao=null" to silence output in tests.
func Start(browser string, extraArgs ...string) (*Player, error) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("eartube-mpv-%d.sock", os.Getpid()))
	args := []string{
		"--idle=yes", "--no-video", "--no-terminal", "--force-window=no",
		"--input-ipc-server=" + sock,
		"--ytdl-format=bestaudio/best", // never show video, so don't open a video stream
	}
	args = append(args, extraArgs...)
	cmd := exec.Command("mpv", args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var conn net.Conn
	var err error
	for i := 0; i < 30; i++ {
		if conn, err = net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("connect to mpv: %w", err)
	}

	p := &Player{cmd: cmd, conn: conn, sock: sock, browser: browser, st: Status{Speed: 1, Idle: true}, Ended: make(chan bool, 1)}
	for _, prop := range []string{"time-pos", "duration", "pause", "speed", "idle-active"} {
		p.send("observe_property", 1, prop)
	}
	go p.readLoop()
	return p, nil
}

func (p *Player) send(cmd ...any) {
	b, _ := json.Marshal(map[string]any{"command": cmd})
	p.conn.Write(append(b, '\n'))
}

func (p *Player) readLoop() {
	sc := bufio.NewScanner(p.conn)
	for sc.Scan() {
		var ev struct {
			Event  string `json:"event"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
			Data   any    `json:"data"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Event == "end-file" {
			switch ev.Reason {
			case "eof":
				p.mu.Lock()
				p.cur = nil
				p.mu.Unlock()
				p.signal(true)
			case "error":
				p.failed()
			}
			continue
		}
		if ev.Event != "property-change" {
			continue
		}
		p.mu.Lock()
		switch ev.Name {
		case "time-pos":
			p.st.Pos, _ = ev.Data.(float64)
		case "duration":
			p.st.Duration, _ = ev.Data.(float64)
		case "speed":
			if v, ok := ev.Data.(float64); ok {
				p.st.Speed = v
			}
		case "pause":
			p.st.Paused, _ = ev.Data.(bool)
		case "idle-active":
			p.st.Idle, _ = ev.Data.(bool)
		}
		p.mu.Unlock()
	}
}

// Done returns the channel that signals a track finishing on its own.
func (p *Player) Done() <-chan bool { return p.Ended }

func (p *Player) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}

// Load replaces the current track, starting at start seconds (0 = beginning).
// It tries the page URL without cookies first, then with them (see attempt.go).
func (p *Player) Load(url string, start float64) { p.begin(newAttempt(url, "", start)) }

// LoadResolved is Load for a video whose direct stream URL is already known.
// It starts instantly; if the stream URL has gone stale it falls back to
// resolving url the normal way.
func (p *Player) LoadResolved(url, stream string, start float64) {
	p.begin(newAttempt(url, stream, start))
}

func (p *Player) begin(a attempt) {
	p.mu.Lock()
	p.st.Pos, p.st.Duration = 0, 0 // don't report the old track's position
	p.cur = &a
	p.mu.Unlock()
	p.run(a)
}

func (p *Player) run(a attempt) {
	if opts := a.options(p.browser); opts != "" {
		p.send("loadfile", a.target(), "replace", -1, opts)
		return
	}
	p.send("loadfile", a.target(), "replace")
}

// failed handles mpv reporting a load/playback error: move to the next stage
// if there is one, otherwise tell the queue. A late error from a track that was
// already replaced could advance the new track's stage by mistake; that only
// costs a slower start, never a wrong track.
func (p *Player) failed() {
	p.mu.Lock()
	var nxt attempt
	ok := false
	if p.cur != nil {
		if nxt, ok = p.cur.next(p.browser); ok {
			p.cur = &nxt
		} else {
			p.cur = nil
		}
	}
	p.mu.Unlock()
	if ok {
		p.run(nxt)
		return
	}
	p.signal(false)
}

func (p *Player) signal(eof bool) {
	select {
	case p.Ended <- eof:
	default:
	}
}

func (p *Player) TogglePause()     { p.send("cycle", "pause") }
func (p *Player) Seek(sec float64) { p.send("seek", sec, "relative") }
func (p *Player) AddSpeed(d float64) {
	s := p.Status().Speed + d
	if s < 0.25 {
		s = 0.25
	}
	p.send("set_property", "speed", s)
}

// Close stops mpv and removes its socket. mpv can ignore "quit" when it arrives
// right after startup (while property observers are still being registered),
// so after a grace period the process is killed rather than waited on forever.
func (p *Player) Close() {
	p.send("quit")
	p.conn.Close()
	done := make(chan struct{})
	go func() { p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeGrace):
		p.cmd.Process.Kill()
		<-done
	}
	os.Remove(p.sock)
}

// closeGrace is how long Close waits for mpv to exit on its own.
const closeGrace = 2 * time.Second
