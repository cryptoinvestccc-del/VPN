package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is one stored lead.
type Record struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	IP   string    `json:"ip"`
	Lead Lead      `json:"lead"`
}

// FileStore appends records to a JSON-lines file that only the service
// user can read. It is the primary copy of every lead: a Telegram message
// is a convenience on top of it, and a failed one loses nothing.
type FileStore struct {
	mu sync.Mutex
	f  *os.File
}

// OpenFileStore opens (or creates) the file with mode 0600 and tightens
// the mode of a file that already existed with looser permissions.
func OpenFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	return &FileStore{f: f}, nil
}

func (s *FileStore) Append(r Record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.f.Write(line); err != nil {
		return err
	}
	return s.f.Sync()
}

func (s *FileStore) Close() error { return s.f.Close() }

// checkOutside refuses a store path inside the folder the server publishes:
// leads put there would be downloadable by anyone who guesses the name.
func checkOutside(store, static string) error {
	if static == "" {
		return nil
	}
	resolve := func(p string) (string, error) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		// The store file may not exist yet; its folder must.
		dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, filepath.Base(abs)), nil
	}
	st, err := resolve(store)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(static)
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	rel, err := filepath.Rel(root, st)
	if err != nil {
		return err
	}
	if rel == "." || !strings.HasPrefix(rel, "..") {
		return errors.New("the lead store must be outside the published folder")
	}
	return nil
}
