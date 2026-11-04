package download

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Unix(1_700_000_000, 0)

func entry(id string, mb int64, ageHours int) Entry {
	return Entry{ID: id, Bytes: mb << 20, LastUsed: t0.Add(-time.Duration(ageHours) * time.Hour)}
}

func TestEvict(t *testing.T) {
	const cap = 100 << 20
	cases := []struct {
		name      string
		entries   []Entry
		max       int64
		protected map[string]bool
		want      []string
		wantOver  bool
	}{
		{"under the cap: nothing goes", []Entry{entry("a", 40, 1), entry("b", 50, 2)}, cap, nil, nil, false},
		{"exactly at the cap: nothing goes", []Entry{entry("a", 50, 1), entry("b", 50, 2)}, cap, nil, nil, false},
		{"oldest first, and only as many as needed",
			[]Entry{entry("new", 60, 1), entry("mid", 30, 5), entry("old", 30, 9)}, cap, nil, []string{"old"}, false},
		{"takes several when one is not enough",
			[]Entry{entry("big", 90, 1), entry("x", 30, 5), entry("y", 30, 9)}, cap, nil, []string{"y", "x"}, false},
		{"protected files are skipped even when oldest",
			[]Entry{entry("playing", 60, 99), entry("a", 30, 5), entry("b", 30, 1)}, cap,
			map[string]bool{"playing": true}, []string{"a"}, false},
		{"everything protected: nothing can go, and we are over",
			[]Entry{entry("a", 80, 5), entry("b", 80, 1)}, cap, map[string]bool{"a": true, "b": true}, nil, true},
		{"evicts what it can but still over",
			[]Entry{entry("keep", 90, 5), entry("small", 20, 9)}, cap, map[string]bool{"keep": true}, []string{"small"}, false},
		{"one protected file bigger than the cap",
			[]Entry{entry("huge", 150, 1), entry("tiny", 5, 9)}, cap, map[string]bool{"huge": true}, []string{"tiny"}, true},
		{"zero means unlimited", []Entry{entry("a", 5000, 1)}, 0, nil, nil, false},
		{"negative also means unlimited", []Entry{entry("a", 5000, 1)}, -1, nil, nil, false},
		{"ties on last-used break by ID, so the result is stable",
			[]Entry{{ID: "b", Bytes: 60 << 20, LastUsed: t0}, {ID: "a", Bytes: 60 << 20, LastUsed: t0}}, cap, nil, []string{"a"}, false},
		{"empty", nil, cap, nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, over := Evict(c.entries, c.max, c.protected)
			if !reflect.DeepEqual(got, c.want) || over != c.wantOver {
				t.Errorf("Evict = %v, over=%v; want %v, over=%v", got, over, c.want, c.wantOver)
			}
		})
	}
}

func TestEvictDoesNotModifyItsInput(t *testing.T) {
	in := []Entry{entry("new", 60, 1), entry("old", 60, 9)}
	snapshot := append([]Entry(nil), in...)
	Evict(in, 100<<20, nil)
	if !reflect.DeepEqual(in, snapshot) {
		t.Fatal("Evict reordered or changed the caller's slice")
	}
}
