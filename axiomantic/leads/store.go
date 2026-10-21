package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
}

// OpenFileStore creates the file with mode 0600 (or tightens the mode of
// one that existed with looser permissions) and checks it can be written.
func OpenFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	f, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	// An erase cut off by a crash leaves its half-written copy next to the
	// store; it may hold the data that was being erased.
	stale, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".erase-*"))
	for _, p := range stale {
		_ = os.Remove(p)
	}
	return &FileStore{path: path}, nil
}

// openAppend opens the file for one append. The file is opened anew for
// every record rather than kept open: if someone replaces it (cleaning out
// old leads with sed -i, say), new leads go into the file that is there
// now, never into a deleted one. A last line cut short by a crash is
// finished first, so the fragment cannot swallow the next record.
func openAppend(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	if err := endLine(path, f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func endLine(path string, f *os.File) error {
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer r.Close()
	st, err := r.Stat()
	if err != nil || st.Size() == 0 {
		return err
	}
	last := make([]byte, 1)
	if _, err := r.ReadAt(last, st.Size()-1); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if last[0] != '\n' {
		if _, err := f.Write([]byte{'\n'}); err != nil {
			return fmt.Errorf("store: %w", err)
		}
	}
	return nil
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
	f, err := openAppend(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		// A short write (a full disk) must not leave half a line behind
		// for the next record to be glued to.
		_ = f.Truncate(st.Size())
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// Close is kept for callers; the store holds no open file between writes.
func (s *FileStore) Close() error { return nil }

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
	br := bufio.NewReaderSize(f, 64<<10)
	for {
		line, long, err := readLine(br)
		if err != nil {
			return skipped, fmt.Errorf("store: %w", err)
		}
		if line == nil && !long {
			break
		}
		raw := bytes.TrimSpace(line)
		if long {
			skipped++
			continue
		}
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
	return skipped, nil
}

// readLine returns the next line without its newline. A line longer than
// maxLine is never held in memory: it is read through and reported as
// long (no valid record comes close; only damage makes one). At the end
// of the file it returns nil, false, nil.
func readLine(br *bufio.Reader) (line []byte, long bool, err error) {
	for {
		frag, err := br.ReadSlice('\n')
		if !long {
			if len(line)+len(frag) > maxLine {
				long, line = true, nil
			} else {
				line = append(line, frag...)
			}
		}
		switch {
		case err == nil:
			if long {
				return nil, true, nil
			}
			return bytes.TrimSuffix(line, []byte{'\n'}), false, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if long {
				return nil, true, nil
			}
			if len(line) == 0 {
				return nil, false, nil
			}
			return line, false, nil
		default:
			return nil, false, err
		}
	}
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
	br := bufio.NewReaderSize(in, 64<<10)
	found := false
	inWS := func(lineWS string) bool {
		if lineWS == "" {
			lineWS = defaultWS
		}
		return lineWS == ws
	}
	for {
		line, long, err := readLine(br)
		if err != nil {
			tmp.Close()
			return fmt.Errorf("store: %w", err)
		}
		if long {
			continue // damage, not a record; it may hold personal data, so it goes
		}
		if line == nil {
			break
		}
		out := line
		var kind storeLine
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if json.Unmarshal(line, &kind) != nil {
			// A damaged line (a write cut short) is not a record Load
			// would use, but it may hold a copy of the data being erased:
			// it goes, like over-long damage.
			continue
		}
		{
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
	// IsLocal is true for "x" and for "..leads.jsonl" — a file in the
	// folder whose name merely starts with two dots — and false for
	// "../x"; a prefix test would let the second one through.
	if rel == "." || filepath.IsLocal(rel) {
		return errors.New("the lead store must be outside the published folder")
	}
	return nil
}
