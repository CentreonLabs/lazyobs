// Package session saves what the user was looking at (tabs, queries, time
// ranges...) so lazyobs reopens where it was left. State is kept per
// connection (context, organization or demo) in a JSON file separate from
// the user-edited configuration.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// version of the state file format.
const version = 1

// Session is the saved state of one connection.
type Session struct {
	Active int               `json:"active"`
	Tabs   []json.RawMessage `json:"tabs"` // one view state per tab, owned by the view
}

type file struct {
	Version  int                `json:"version"`
	Sessions map[string]Session `json:"sessions"`
}

// Store reads and writes the state file.
type Store struct{ Path string }

// DefaultPath returns $XDG_STATE_HOME/lazyobs/state.json, ~/.local/state/...
// on Unix-like systems, or %LocalAppData%\lazyobs\state.json on Windows.
func DefaultPath(getenv func(string) string) string {
	if dir := getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "lazyobs", "state.json")
	}
	if runtime.GOOS == "windows" {
		if dir, err := os.UserCacheDir(); err == nil { // %LocalAppData%
			return filepath.Join(dir, "lazyobs", "state.json")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".lazyobs-state.json"
	}
	return filepath.Join(home, ".local", "state", "lazyobs", "state.json")
}

// Load returns the session saved under key; ok is false when there is none.
func (s Store) Load(key string) (sess Session, ok bool, err error) {
	f, err := s.read()
	if err != nil {
		return Session{}, false, err
	}
	sess, ok = f.Sessions[key]
	return sess, ok && len(sess.Tabs) > 0, nil
}

// Save stores the session under key, keeping the other ones.
func (s Store) Save(key string, sess Session) error {
	f, err := s.read()
	if err != nil {
		f = &file{} // unreadable: start over rather than never saving again
	}
	if f.Sessions == nil {
		f.Sessions = map[string]Session{}
	}
	f.Version = version
	f.Sessions[key] = sess
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return writeAtomic(s.Path, b)
}

func (s Store) read() (*file, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return &file{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("session: %s is not valid: %w", s.Path, err)
	}
	if f.Version > version {
		return nil, fmt.Errorf("session: %s was written by a newer lazyobs", s.Path)
	}
	return &f, nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("session: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil { // queries may be sensitive
		return fmt.Errorf("session: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}
