package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Record is one stored lead. WS is the workspace whose site it came from;
// lines written before workspaces existed have none and belong to the
// first one.
type Record struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	IP     string    `json:"ip,omitempty"`
	WS     string    `json:"ws,omitempty"`
	Lead   Lead      `json:"lead"`
	Erased bool      `json:"erased,omitempty"`
}

// Event is something a team member did to a lead: changed its status,
// assigned it, wrote a note, or erased the visitor's personal data.
// Events are appended after the lead's line and never edited, except that
// erasing a lead also wipes the texts of its notes.
type Event struct {
	Ev     string    `json:"ev"`
	ID     string    `json:"id"`
	LeadID string    `json:"lead"`
	WS     string    `json:"ws"`
	Time   time.Time `json:"time"`
	By     string    `json:"by"`
	From   string    `json:"from,omitempty"`
	To     string    `json:"to,omitempty"`
	Text   string    `json:"text,omitempty"`
}

const (
	evStatus = "status"
	evAssign = "assign"
	evNote   = "note"
	evErase  = "erase"
)

// FileStore appends records to a JSON-lines file that only the service
// user can read. It is the primary copy of every lead: a Telegram message
// is a convenience on top of it, and a failed one loses nothing.
type FileStore struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

// OpenFileStore opens (or creates) the file with mode 0600 and tightens
// the mode of a file that already existed with looser permissions.
func OpenFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	f, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	return &FileStore{path: path, f: f}, nil
}

func openAppend(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	return f, nil
}

func (s *FileStore) Append(r Record) error { return s.appendLine(r) }

func (s *FileStore) AppendEvent(e Event) error { return s.appendLine(e) }

func (s *FileStore) appendLine(v any) error {
	line, err := json.Marshal(v)
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

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}

// storeLine tells a lead from an event: only events have "ev".
type storeLine struct {
	Ev string `json:"ev"`
}

const maxLine = 1 << 20

// Load replays the file from the start. A line that does not parse (a
// write cut short by a crash) is reported and skipped; it never stops
// the server from starting.
func (s *FileStore) Load(onRecord func(Record), onEvent func(Event)) (skipped int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		return 0, fmt.Errorf("store: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		var kind storeLine
		if json.Unmarshal(raw, &kind) != nil {
			skipped++
			continue
		}
		if kind.Ev == "" {
			var r Record
			if json.Unmarshal(raw, &r) != nil || r.ID == "" {
				skipped++
				continue
			}
			onRecord(r)
		} else {
			var e Event
			if json.Unmarshal(raw, &e) != nil || e.LeadID == "" {
				skipped++
				continue
			}
			onEvent(e)
		}
	}
	if err := sc.Err(); err != nil {
		return skipped, fmt.Errorf("store: %w", err)
	}
	return skipped, nil
}

// Erase wipes one lead's personal data from the file itself — the name,
// phone, contact and message, and the texts of its notes — then appends
// the erase event. The file is rewritten next to itself and swapped in
// with a rename, so a crash leaves either the old file or the new one.
// Lines it cannot parse are kept as they are.
func (s *FileStore) Erase(ws, defaultWS, leadID string, ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, err := os.Open(s.path)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer in.Close()
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.path)+".erase-*")
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("store: %w", err)
	}
	w := bufio.NewWriter(tmp)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	found := false
	inWS := func(lineWS string) bool {
		if lineWS == "" {
			lineWS = defaultWS
		}
		return lineWS == ws
	}
	for sc.Scan() {
		line := sc.Bytes()
		out := line
		var kind storeLine
		if json.Unmarshal(line, &kind) == nil {
			if kind.Ev == "" {
				var r Record
				if json.Unmarshal(line, &r) == nil && r.ID == leadID && inWS(r.WS) {
					r.Lead = erasedLead(r.Lead)
					r.Erased = true
					r.IP = ""
					found = true
					if out, err = json.Marshal(r); err != nil {
						tmp.Close()
						return err
					}
				}
			} else {
				var e Event
				if json.Unmarshal(line, &e) == nil && e.LeadID == leadID && inWS(e.WS) && e.Text != "" {
					e.Text = ""
					if out, err = json.Marshal(e); err != nil {
						tmp.Close()
						return err
					}
				}
			}
		}
		w.Write(out)
		w.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: %w", err)
	}
	if !found {
		tmp.Close()
		return errors.New("store: lead to erase not found")
	}
	line, err := json.Marshal(ev)
	if err != nil {
		tmp.Close()
		return err
	}
	w.Write(line)
	w.WriteByte('\n')
	if err := w.Flush(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	// The old handle points at the replaced file; appends must go to the new one.
	nf, err := openAppend(s.path)
	if err != nil {
		return err
	}
	s.f.Close()
	s.f = nf
	return nil
}

// erasedLead keeps what says nothing about the person: how they reached
// the studio and what they were interested in.
func erasedLead(l Lead) Lead {
	return Lead{Source: l.Source, Mode: l.Mode, Channel: l.Channel, When: l.When, Interest: l.Interest, Budget: l.Budget, Consent: l.Consent}
}

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
