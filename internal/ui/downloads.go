package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RobertStevenson/eartube/internal/download"
	"github.com/RobertStevenson/eartube/internal/store"
	"github.com/RobertStevenson/eartube/internal/ytdlp"
)

// Options configures the optional features: offline downloads, the settings
// view and the archive of video details.
type Options struct {
	Downloads    bool  // enable d / D / X
	DeletePlayed bool  // delete a download when its track is marked played
	MaxBytes     int64 // cap on total downloaded bytes; 0 = unlimited
	Downloader   Downloader
	Settings     Settings // for the settings view (c)
	// Archiver looks up a video's full details for the archive; nil turns
	// the lookups off (videos are still remembered from playlist listings).
	Archiver Archiver
	// ArchivePause is the wait before each lookup, to stay gentle on YouTube.
	ArchivePause time.Duration
	// RemovedDir is where a copy of saved details goes before they are
	// deleted (s on a saved playlist, then y). Empty refuses to delete.
	RemovedDir string
	// SubscriptionsCount is how many uploads the subscriptions view (f)
	// lists; 0 means the default.
	SubscriptionsCount int
}

type dlDoneMsg struct {
	item ytdlp.Item
	res  download.Result
	err  error
}

// dlProgress is shared between the download goroutine and the view.
type dlProgress struct {
	mu  sync.Mutex
	pct float64
}

func (p *dlProgress) set(v float64) { p.mu.Lock(); p.pct = v; p.mu.Unlock() }
func (p *dlProgress) get() float64  { p.mu.Lock(); defer p.mu.Unlock(); return p.pct }

// dlState is everything the model tracks about downloads.
type dlState struct {
	opts   Options
	files  map[string]store.Download // downloaded files by video ID
	queue  []ytdlp.Item              // waiting to download, one at a time
	active *ytdlp.Item
	batch  map[string]bool // queued or active IDs, protected from eviction
	done   int             // finished in the current batch, for status text
	total  int
	prog   *dlProgress
	ctx    context.Context
	cancel context.CancelFunc
}

func newDLState(opts Options, db *store.Store) dlState {
	ctx, cancel := context.WithCancel(context.Background())
	s := dlState{opts: opts, files: map[string]store.Download{}, batch: map[string]bool{},
		prog: &dlProgress{}, ctx: ctx, cancel: cancel}
	if all, err := db.Downloads(); err == nil {
		for id, d := range all {
			if _, err := os.Stat(d.Path); err != nil {
				db.RemoveDownload(id) // the file is gone; forget it
				continue
			}
			s.files[id] = d
		}
	}
	return s
}

func (m *Model) hasDownload(id string) bool { _, ok := m.dl.files[id]; return ok }

// localFile returns the downloaded file for id if it is still on disk. A row
// whose file has vanished is forgotten, so the track streams instead.
func (m *Model) localFile(id string) (string, bool) {
	d, ok := m.dl.files[id]
	if !ok {
		return "", false
	}
	if _, err := os.Stat(d.Path); err != nil {
		m.forgetDownload(id)
		return "", false
	}
	return d.Path, true
}

// touchDownload records that a download was just played, so eviction keeps it.
func (m *Model) touchDownload(id string) {
	if d, ok := m.dl.files[id]; ok {
		d.LastUsed = time.Now()
		m.dl.files[id] = d
		_ = m.db.TouchDownload(id, d.LastUsed)
	}
}

func (m *Model) forgetDownload(id string) {
	_ = m.db.RemoveDownload(id)
	delete(m.dl.files, id)
}

// removeDownload deletes the file and forgets it.
func (m *Model) removeDownload(id string) {
	if d, ok := m.dl.files[id]; ok {
		if err := os.Remove(d.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			m.status = "could not delete download: " + err.Error()
		}
	}
	m.forgetDownload(id)
}

// requestDownload queues videos for download (one at a time). Anything already
// downloaded, queued, or in progress is skipped.
func (m *Model) requestDownload(items []ytdlp.Item) tea.Cmd {
	if !m.dl.opts.Downloads || m.dl.opts.Downloader == nil {
		m.status = "downloads are off: set downloads = true in the config, or run with --downloads"
		return nil
	}
	added := 0
	for _, it := range items {
		if m.hasDownload(it.ID) || m.dl.batch[it.ID] {
			continue
		}
		m.dl.queue = append(m.dl.queue, it)
		m.dl.batch[it.ID] = true
		added++
	}
	if added == 0 {
		if len(items) == 1 {
			m.status = "already downloaded or queued: " + items[0].Title
		} else {
			m.status = "nothing new to download"
		}
		return nil
	}
	m.dl.total += added
	m.status = fmt.Sprintf("queued %d for download", added)
	return m.startNextDownload()
}

func (m *Model) startNextDownload() tea.Cmd {
	if m.dl.active != nil || len(m.dl.queue) == 0 {
		return nil
	}
	it := m.dl.queue[0]
	m.dl.queue = m.dl.queue[1:]
	m.dl.active = &it
	m.dl.prog.set(0)
	d, ctx, prog := m.dl.opts.Downloader, m.dl.ctx, m.dl.prog
	return func() tea.Msg {
		res, err := d.Fetch(ctx, it.ID, it.URL, prog.set)
		return dlDoneMsg{item: it, res: res, err: err}
	}
}

// onDownloadDone records a finished (or failed) download, enforces the size
// cap, and starts the next one.
func (m *Model) onDownloadDone(msg dlDoneMsg) tea.Cmd {
	m.dl.active = nil
	m.dl.done++
	switch {
	case errors.Is(msg.err, context.Canceled):
		m.finishBatch()
		return nil
	case msg.err != nil:
		delete(m.dl.batch, msg.item.ID)
		m.status = fmt.Sprintf("download failed: %s: %v", msg.item.Title, msg.err)
		return tea.Batch(m.refreshList(), m.startNextDownload())
	}

	rec := store.Download{ID: msg.item.ID, Path: msg.res.Path, Bytes: msg.res.Bytes,
		LastUsed: time.Now(), Title: msg.item.Title, Channel: msg.item.Channel, Duration: msg.item.Duration}
	if err := m.db.AddDownload(rec); err != nil {
		m.status = "could not record download: " + err.Error()
		return m.startNextDownload()
	}
	m.dl.files[msg.item.ID] = rec

	freed, over := m.enforceCap()
	switch {
	case over:
		skipped := len(m.dl.queue)
		m.dl.queue = nil
		m.finishBatch()
		m.status = fmt.Sprintf("download cap reached (%s): kept %q, skipped %d more", fmtBytes(m.dl.opts.MaxBytes), msg.item.Title, skipped)
	case len(m.dl.queue) == 0:
		m.status = m.summary(msg.item.Title, freed)
		m.finishBatch()
	default:
		m.status = fmt.Sprintf("downloaded %d of %d", m.dl.done, m.dl.total)
	}
	return tea.Batch(m.refreshList(), m.startNextDownload())
}

func (m *Model) summary(last string, freed int) string {
	s := "downloaded: " + last
	if m.dl.total > 1 {
		s = fmt.Sprintf("downloaded %d videos", m.dl.total)
	}
	if freed > 0 {
		s += fmt.Sprintf(" (deleted %d old download(s) to stay under the cap)", freed)
	}
	return s
}

func (m *Model) finishBatch() {
	m.dl.batch = map[string]bool{}
	m.dl.done, m.dl.total = 0, 0
}

// protectedIDs are downloads that must not be evicted: whatever is playing or
// queued, and the rest of the batch being downloaded.
func (m *Model) protectedIDs() map[string]bool {
	p := map[string]bool{}
	for _, it := range m.queue {
		p[it.ID] = true
	}
	for id := range m.dl.batch {
		p[id] = true
	}
	return p
}

// enforceCap deletes least-recently-played downloads until the total fits.
// over is true if it still does not fit because everything left is protected.
func (m *Model) enforceCap() (freed int, over bool) {
	entries := make([]download.Entry, 0, len(m.dl.files))
	for id, d := range m.dl.files {
		entries = append(entries, download.Entry{ID: id, Bytes: d.Bytes, LastUsed: d.LastUsed})
	}
	ids, over := download.Evict(entries, m.dl.opts.MaxBytes, m.protectedIDs())
	for _, id := range ids {
		m.removeDownload(id)
	}
	return len(ids), over
}

// cancelDownloads stops the active download and drops the queue (on quit).
func (m *Model) cancelDownloads() {
	m.dl.cancel()
	m.dl.queue = nil
}

// downloadStatus is the short "downloading…" text for the now-playing line.
func (m Model) downloadStatus() string {
	if m.dl.active == nil {
		return ""
	}
	s := fmt.Sprintf("⇩ %.0f%% %s", m.dl.prog.get(), m.dl.active.Title)
	if n := len(m.dl.queue); n > 0 {
		s += fmt.Sprintf(" (+%d)", n)
	}
	return s
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n > 0:
		return fmt.Sprintf("%d B", n)
	}
	return "unlimited"
}

// --- the Downloaded view --------------------------------------------------------

// downloadedItems lists every downloaded video, most recently played first. It
// is built from the local database alone, so it works with no network.
func (m *Model) downloadedItems() []list.Item {
	ds := make([]store.Download, 0, len(m.dl.files))
	for _, d := range m.dl.files {
		ds = append(ds, d)
	}
	sort.Slice(ds, func(i, j int) bool {
		if !ds[i].LastUsed.Equal(ds[j].LastUsed) {
			return ds[i].LastUsed.After(ds[j].LastUsed)
		}
		if ds[i].Title != ds[j].Title {
			return ds[i].Title < ds[j].Title
		}
		return ds[i].ID < ds[j].ID
	})
	items := make([]list.Item, len(ds))
	for i, d := range ds {
		title := d.Title
		if title == "" {
			title = d.ID // downloaded before titles were kept; learned when next seen
		}
		items[i] = entry{
			it: ytdlp.Item{ID: d.ID, Title: title, Channel: d.Channel, Duration: d.Duration,
				URL: "https://www.youtube.com/watch?v=" + d.ID},
			tr: m.state[d.ID],
			dl: true,
		}
	}
	return items
}

func (m *Model) downloadedTitle() string {
	var total int64
	for _, d := range m.dl.files {
		total += d.Bytes
	}
	if len(m.dl.files) == 0 {
		return "Downloaded · none"
	}
	noun := "videos"
	if len(m.dl.files) == 1 {
		noun = "video"
	}
	return fmt.Sprintf("Downloaded · %d %s · %s", len(m.dl.files), noun, fmtBytes(total))
}

// rebuildDownloads refreshes the Downloaded view, keeping the cursor nearby.
func (m *Model) rebuildDownloads(idx int) tea.Cmd {
	items := m.downloadedItems()
	m.list.Title = m.downloadedTitle()
	cmd := m.list.SetItems(items)
	if len(items) > 0 {
		m.list.Select(min(idx, len(items)-1))
	}
	return cmd
}

// openDownloads switches the list to the Downloaded view.
func (m *Model) openDownloads() tea.Cmd {
	if len(m.dl.files) == 0 {
		m.status = "no downloads yet: press d on a video"
		if !m.dl.opts.Downloads {
			m.status += " (after setting downloads = true)"
		}
		return nil
	}
	m.showQueue = false
	m.showDownloads = true
	m.subs = noSubs
	m.status = fmt.Sprintf("%d downloaded: enter plays, X deletes, o refreshes", len(m.dl.files))
	return m.rebuildDownloads(0)
}

// learnMetadata fills in titles for downloads recorded before titles were kept,
// as soon as the same videos show up in any list.
func (m *Model) learnMetadata(items []ytdlp.Item) {
	for _, it := range items {
		if d, ok := m.dl.files[it.ID]; ok && d.Title == "" && it.Title != "" && !it.Unavailable {
			d.Title, d.Channel, d.Duration = it.Title, it.Channel, it.Duration
			m.dl.files[it.ID] = d
			_ = m.db.SetDownloadMeta(it.ID, d.Title, d.Channel, d.Duration)
		}
	}
}
