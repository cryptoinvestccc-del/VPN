package provision

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"
)

// Statistics for the operator, in totals only.
//
// Everything here is a count or a sum. Which key did what, and when, is
// a record of who used the VPN, and this package still keeps none: the
// numbers below are computed from the peer list as it stands and from
// counters that know that something happened but not to whom. The one
// per-key value held — the last byte totals, to turn running totals
// into a day's traffic — lives in memory, is overwritten every sample
// and is never written anywhere.

// Tally counts what the service did. It holds numbers, never keys.
type Tally struct {
	issued, forgotten, withdrawn atomic.Int64
}

func (t *Tally) addIssued() {
	if t != nil {
		t.issued.Add(1)
	}
}

func (t *Tally) addForgotten() {
	if t != nil {
		t.forgotten.Add(1)
	}
}

func (t *Tally) addWithdrawn() {
	if t != nil {
		t.withdrawn.Add(1)
	}
}

// Counts reports the totals since the process started: credentials
// issued, removed by their device, and withdrawn for disuse.
func (t *Tally) Counts() (issued, forgotten, withdrawn int64) {
	if t == nil {
		return 0, 0, 0
	}
	return t.issued.Load(), t.forgotten.Load(), t.withdrawn.Load()
}

// SetTally counts issues and removals from now on.
func (s *Service) SetTally(t *Tally) {
	s.mu.Lock()
	s.tally = t
	s.mu.Unlock()
}

// SetTally counts withdrawals from now on.
func (r *Reaper) SetTally(t *Tally) {
	r.mu.Lock()
	r.tally = t
	r.mu.Unlock()
}

// Snapshot is the state of the credentials this service issued, at one
// moment, as totals.
type Snapshot struct {
	Keys      int // credentials held
	Connected int // with a handshake in the last few minutes
	Active24h int // with a handshake in the last day
	Active7d  int // with a handshake in the last week
}

// Summarize counts the peers owns accepts. A nil owns counts them all.
func Summarize(peers []Peer, owns func(string) bool, now time.Time) Snapshot {
	var s Snapshot
	for _, p := range peers {
		if owns != nil && !owns(p.PublicKey) {
			continue
		}
		s.Keys++
		if !p.Used() {
			continue
		}
		age := now.Sub(p.LastHandshake)
		if age < activeWithin {
			s.Connected++
		}
		if age < 24*time.Hour {
			s.Active24h++
		}
		if age < 7*24*time.Hour {
			s.Active7d++
		}
	}
	return s
}

// TrafficMeter turns the interface's running byte totals into traffic
// since the previous reading.
type TrafficMeter struct {
	primed bool
	last   map[string][2]uint64
}

// Measure returns the bytes received and sent since the last call.
//
// The first call only takes a reading: after a restart every peer's
// totals are already large, and counting them would put days of
// traffic into one sample. After that, a peer not seen before is new
// and its totals count in full, and a total that went down means the
// interface restarted and counts from zero.
func (m *TrafficMeter) Measure(peers []Peer, owns func(string) bool) (rx, tx uint64) {
	next := make(map[string][2]uint64, len(peers))
	for _, p := range peers {
		if owns != nil && !owns(p.PublicKey) {
			continue
		}
		cur := [2]uint64{p.RxBytes, p.TxBytes}
		next[p.PublicKey] = cur
		if !m.primed {
			continue
		}
		prev, seen := m.last[p.PublicKey]
		rx += grown(prev[0], cur[0], seen)
		tx += grown(prev[1], cur[1], seen)
	}
	m.last, m.primed = next, true
	return rx, tx
}

func grown(prev, cur uint64, seen bool) uint64 {
	if !seen || cur < prev {
		return cur
	}
	return cur - prev
}

// Day is one calendar day's totals.
type Day struct {
	Date          string `json:"date"` // YYYY-MM-DD in the operator's zone
	Issued        int64  `json:"issued"`
	Forgotten     int64  `json:"forgotten"`
	Withdrawn     int64  `json:"withdrawn"`
	PeakConnected int    `json:"peak_connected"`
	Active24h     int    `json:"active_24h"` // at the day's last sample
	Keys          int    `json:"keys"`       // at the day's last sample
	RxBytes       uint64 `json:"rx_bytes"`
	TxBytes       uint64 `json:"tx_bytes"`
}

// keepDays bounds the book: two months answers any question the reports
// ask, and a bounded file cannot grow into a problem.
const keepDays = 62

// Book is the operator's day-by-day totals, kept in a small JSON file so
// a restart does not lose the day so far.
type Book struct {
	Days []Day `json:"days"`
}

// Add folds one sample into its day.
func (b *Book) Add(date string, snap Snapshot, issued, forgotten, withdrawn int64, rx, tx uint64) {
	d := b.day(date)
	d.Issued += issued
	d.Forgotten += forgotten
	d.Withdrawn += withdrawn
	d.RxBytes += rx
	d.TxBytes += tx
	if snap.Connected > d.PeakConnected {
		d.PeakConnected = snap.Connected
	}
	d.Active24h = snap.Active24h
	d.Keys = snap.Keys
}

func (b *Book) day(date string) *Day {
	for i := range b.Days {
		if b.Days[i].Date == date {
			return &b.Days[i]
		}
	}
	b.Days = append(b.Days, Day{Date: date})
	sort.Slice(b.Days, func(i, j int) bool { return b.Days[i].Date < b.Days[j].Date })
	if n := len(b.Days); n > keepDays {
		b.Days = append([]Day(nil), b.Days[n-keepDays:]...)
	}
	for i := range b.Days {
		if b.Days[i].Date == date {
			return &b.Days[i]
		}
	}
	// Older than everything kept: count it in a day that is not stored.
	return &Day{Date: date}
}

// Get returns a day's totals, zero if there are none.
func (b *Book) Get(date string) Day {
	for _, d := range b.Days {
		if d.Date == date {
			return d
		}
	}
	return Day{Date: date}
}

// LoadBook reads a book; a missing file is an empty book.
func LoadBook(path string) (*Book, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Book{}, nil
	}
	if err != nil {
		return nil, err
	}
	var b Book
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// Save writes the book through a temporary file, so a crash mid-write
// leaves the previous version rather than half of this one.
func (b *Book) Save(path string) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".stats-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
