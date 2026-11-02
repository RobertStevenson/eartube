package store

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePos("a", 42.5); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlayed("b", true); err != nil {
		t.Fatal(err)
	}
	// Played clears the resume position; SavePos leaves the flag alone.
	s.SavePos("b", 10)
	s.SetPlayed("a", true)
	s.SavePos("a", 7)
	type q struct{ N int }
	if err := s.SaveJSON("queue", q{3}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path) // reopen: data must persist
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	all, _ := s.All()
	if a := all["a"]; !a.Played || a.Pos != 7 {
		t.Errorf("a = %+v", a)
	}
	if b := all["b"]; !b.Played || b.Pos != 10 {
		t.Errorf("b = %+v", b)
	}
	var got q
	if ok, err := s.LoadJSON("queue", &got); !ok || err != nil || got.N != 3 {
		t.Errorf("queue: ok=%v err=%v got=%+v", ok, err, got)
	}
	if ok, _ := s.LoadJSON("missing", &got); ok {
		t.Error("missing key reported present")
	}
}
