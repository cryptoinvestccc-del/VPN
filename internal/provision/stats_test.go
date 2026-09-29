package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDumpReadsByteTotals(t *testing.T) {
	peers, err := ParseDump([]byte(realDump()))
	if err != nil {
		t.Fatal(err)
	}
	if peers[0].RxBytes != 1024 || peers[0].TxBytes != 2048 {
		t.Errorf("got rx=%d tx=%d, want 1024 and 2048", peers[0].RxBytes, peers[0].TxBytes)
	}
}

func TestSummarizeCountsOnlyOwnedPeersByAge(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	peers := []Peer{
		usedPeer("NOW", time.Minute, now),
		usedPeer("HOURS", 5*time.Hour, now),
		usedPeer("DAYS", 3*24*time.Hour, now),
		usedPeer("MONTH", 20*24*time.Hour, now),
		unusedPeer("NEVER"),
		usedPeer("AMNEZIA", time.Minute, now), // not issued here
	}
	owns := func(k string) bool { return k != "AMNEZIA" }

	got := Summarize(peers, owns, now)
	want := Snapshot{Keys: 5, Connected: 1, Active24h: 2, Active7d: 3}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTrafficMeterCountsGrowthNotTotals(t *testing.T) {
	var m TrafficMeter
	peer := func(k string, rx, tx uint64) Peer { return Peer{PublicKey: k, RxBytes: rx, TxBytes: tx} }

	// The first reading after a start takes stock and counts nothing:
	// those totals are days old.
	if rx, tx := m.Measure([]Peer{peer("A", 5000, 9000)}, nil); rx != 0 || tx != 0 {
		t.Fatalf("first reading counted rx=%d tx=%d", rx, tx)
	}
	// A grows; B is new and counts in full.
	rx, tx := m.Measure([]Peer{peer("A", 5100, 9300), peer("B", 40, 60)}, nil)
	if rx != 140 || tx != 360 {
		t.Errorf("got rx=%d tx=%d, want 140 and 360", rx, tx)
	}
	// The interface restarted: A's totals fell, so they count from zero.
	rx, tx = m.Measure([]Peer{peer("A", 10, 20), peer("B", 40, 60)}, nil)
	if rx != 10 || tx != 20 {
		t.Errorf("after a restart got rx=%d tx=%d, want 10 and 20", rx, tx)
	}
}

func TestTrafficMeterSkipsPeersNotOwned(t *testing.T) {
	var m TrafficMeter
	owns := func(k string) bool { return k == "OURS" }
	m.Measure(nil, owns)
	rx, _ := m.Measure([]Peer{{PublicKey: "OURS", RxBytes: 7}, {PublicKey: "THEIRS", RxBytes: 900}}, owns)
	if rx != 7 {
		t.Errorf("got rx=%d, want only our peer's 7", rx)
	}
}

func TestBookFoldsSamplesIntoDays(t *testing.T) {
	var b Book
	b.Add("2026-09-29", Snapshot{Keys: 10, Connected: 3, Active24h: 5}, 2, 0, 1, 100, 10)
	b.Add("2026-09-29", Snapshot{Keys: 11, Connected: 7, Active24h: 6}, 1, 1, 0, 50, 5)
	b.Add("2026-09-29", Snapshot{Keys: 11, Connected: 2, Active24h: 6}, 0, 0, 0, 0, 0)

	d := b.Get("2026-09-29")
	want := Day{Date: "2026-09-29", Issued: 3, Forgotten: 1, Withdrawn: 1,
		PeakConnected: 7, Active24h: 6, Keys: 11, RxBytes: 150, TxBytes: 15}
	if d != want {
		t.Errorf("got %+v, want %+v", d, want)
	}
}

func TestBookKeepsABoundedHistory(t *testing.T) {
	var b Book
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < keepDays+30; i++ {
		b.Add(start.AddDate(0, 0, i).Format("2006-01-02"), Snapshot{}, 1, 0, 0, 0, 0)
	}
	if len(b.Days) != keepDays {
		t.Fatalf("kept %d days, want %d", len(b.Days), keepDays)
	}
	if last := b.Days[len(b.Days)-1].Date; last != start.AddDate(0, 0, keepDays+29).Format("2006-01-02") {
		t.Errorf("the newest day is %s", last)
	}
}

func TestBookSurvivesARestartAndHoldsNoKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	if b, err := LoadBook(path); err != nil || len(b.Days) != 0 {
		t.Fatalf("a missing file should be an empty book, got %v, %v", b, err)
	}

	var b Book
	b.Add("2026-09-29", Snapshot{Keys: 1}, 1, 0, 0, 1, 1)
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadBook(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Get("2026-09-29") != b.Get("2026-09-29") {
		t.Errorf("read back %+v", back.Get("2026-09-29"))
	}

	// The file is totals: nothing in it looks like a key or an address.
	data, _ := os.ReadFile(path)
	var any map[string]any
	if err := json.Unmarshal(data, &any); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "=") || strings.Contains(string(data), "10.8.") {
		t.Errorf("the book holds something that is not a total:\n%s", data)
	}
}

func TestTallyCountsIssuesForgetsAndWithdrawals(t *testing.T) {
	svc, dev, _ := serviceWithRegistry(t)
	var tally Tally
	svc.SetTally(&tally)

	first, err := svc.Issue(context.Background(), testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	// Asking again for the same key hands back the same credential and
	// is not a new one.
	if _, err := svc.Issue(context.Background(), testKey(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(context.Background(), testKey(2)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Forget(context.Background(), testKey(1), first.ForgetToken); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_800_000_000, 0)
	dev.mu.Lock()
	dev.peers = append(dev.peers, usedPeer(testKey(3), 60*24*time.Hour, now))
	dev.mu.Unlock()
	reaper := NewReaper(dev, &ownsAll{}, time.Hour, time.Hour)
	reaper.nowFn = func() time.Time { return now }
	reaper.SetTally(&tally)
	if _, err := reaper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	issued, forgotten, withdrawn := tally.Counts()
	if got := fmt.Sprint(issued, forgotten, withdrawn); got != "2 1 1" {
		t.Errorf("issued, forgotten, withdrawn = %s, want 2 1 1", got)
	}
}

func TestANilTallyIsHarmless(t *testing.T) {
	var tally *Tally
	tally.addIssued()
	if a, b, c := tally.Counts(); a+b+c != 0 {
		t.Error("a nil tally counted something")
	}
}
