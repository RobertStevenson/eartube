// Package download fetches audio for offline playback and decides what to
// delete when the download cap is exceeded.
package download

import (
	"sort"
	"time"
)

// Entry is a downloaded file, for the purposes of eviction.
type Entry struct {
	ID       string
	Bytes    int64
	LastUsed time.Time
}

// Evict chooses which downloads to delete so the total stays within max bytes.
// It removes the least recently used files first and never removes a protected
// one (the playing track, queued tracks, and anything in the active batch).
// max <= 0 means unlimited. over reports that the total is still above max
// after everything allowed to go has gone.
func Evict(entries []Entry, max int64, protected map[string]bool) (ids []string, over bool) {
	if max <= 0 {
		return nil, false
	}
	var total int64
	for _, e := range entries {
		total += e.Bytes
	}
	if total <= max {
		return nil, false
	}
	candidates := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !protected[e.ID] {
			candidates = append(candidates, e)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].LastUsed.Equal(candidates[j].LastUsed) {
			return candidates[i].LastUsed.Before(candidates[j].LastUsed)
		}
		return candidates[i].ID < candidates[j].ID // stable for equal times
	})
	for _, e := range candidates {
		if total <= max {
			break
		}
		ids = append(ids, e.ID)
		total -= e.Bytes
	}
	return ids, total > max
}
