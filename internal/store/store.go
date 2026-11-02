// Package store persists listening progress and the play queue in SQLite.
package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Track struct {
	Pos    float64
	Played bool
}

type Store struct{ db *sql.DB }

// DefaultPath is $XDG_DATA_HOME/eartube/eartube.db (or ~/.local/share/...).
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "eartube", "eartube.db"), nil
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection: simple, and avoids SQLITE_BUSY between our own calls.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tracks (
			id     TEXT PRIMARY KEY,
			pos    REAL    NOT NULL DEFAULT 0,
			played INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS kv (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// All loads every known track's state.
func (s *Store) All() (map[string]Track, error) {
	rows, err := s.db.Query(`SELECT id, pos, played FROM tracks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Track{}
	for rows.Next() {
		var id string
		var t Track
		if err := rows.Scan(&id, &t.Pos, &t.Played); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

// SavePos records the resume position; the played flag is left as is.
func (s *Store) SavePos(id string, pos float64) error {
	_, err := s.db.Exec(`
		INSERT INTO tracks (id, pos) VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET pos = excluded.pos`, id, pos)
	return err
}

// SetPlayed sets the played flag and clears the resume position.
func (s *Store) SetPlayed(id string, played bool) error {
	_, err := s.db.Exec(`
		INSERT INTO tracks (id, pos, played) VALUES (?, 0, ?)
		ON CONFLICT(id) DO UPDATE SET pos = 0, played = excluded.played`, id, played)
	return err
}

// SaveJSON / LoadJSON keep small blobs (the queue) under a key.
func (s *Store) SaveJSON(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO kv (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(b))
	return err
}

// LoadJSON returns false (and no error) when the key is absent.
func (s *Store) LoadJSON(key string, v any) (bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}
