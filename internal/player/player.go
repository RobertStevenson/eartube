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

	// Ended receives a value when a track finishes on its own (EOF) or fails.
	// It does not fire for stop/replace, so loading a new track won't advance the queue.
	Ended chan struct{}

	mu sync.Mutex
	st Status
}

// Start launches mpv in idle, audio-only mode. browser is passed to yt-dlp for cookies.
func Start(browser string) (*Player, error) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("eartube-mpv-%d.sock", os.Getpid()))
	args := []string{
		"--idle=yes", "--no-video", "--no-terminal", "--force-window=no",
		"--input-ipc-server=" + sock,
	}
	if browser != "" {
		args = append(args, "--ytdl-raw-options=cookies-from-browser="+browser)
	}
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

	p := &Player{cmd: cmd, conn: conn, sock: sock, st: Status{Speed: 1, Idle: true}, Ended: make(chan struct{}, 1)}
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
		if ev.Event == "end-file" && (ev.Reason == "eof" || ev.Reason == "error") {
			select {
			case p.Ended <- struct{}{}:
			default:
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

func (p *Player) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}

func (p *Player) Load(url string)  { p.send("loadfile", url, "replace") }
func (p *Player) TogglePause()     { p.send("cycle", "pause") }
func (p *Player) Seek(sec float64) { p.send("seek", sec, "relative") }
func (p *Player) AddSpeed(d float64) {
	s := p.Status().Speed + d
	if s < 0.25 {
		s = 0.25
	}
	p.send("set_property", "speed", s)
}

func (p *Player) Close() {
	p.send("quit")
	p.conn.Close()
	p.cmd.Wait()
	os.Remove(p.sock)
}
