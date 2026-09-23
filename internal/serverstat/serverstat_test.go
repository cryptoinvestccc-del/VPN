package serverstat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Unix(1_800_000_000, 0)

// A dump shaped like AmneziaWG's: the interface line is wider than
// WireGuard's because the obfuscation parameters follow the port.
func awgDump(lines ...string) []byte {
	head := "awg0\tPRIVATEKEY=\tPUBLICKEY=\t51820\toff\t4\t40\t70\t0\t0\t1\t2\t3\t4"
	return []byte(strings.Join(append([]string{head}, lines...), "\n") + "\n")
}

func peer(handshake int64, rx, tx string) string {
	return strings.Join([]string{
		"awg0", "PEERKEY=", "(none)", "203.0.113.7:40000", "10.8.1.2/32",
		itoa(handshake), rx, tx, "off",
	}, "\t")
}

func itoa(v int64) string { b, _ := json.Marshal(v); return string(b) }

func TestParseDumpCountsOnlyRecentHandshakes(t *testing.T) {
	out := awgDump(
		peer(now.Add(-30*time.Second).Unix(), "100", "200"), // online
		peer(now.Add(-2*time.Minute).Unix(), "1", "1"),      // online, inside the window
		peer(now.Add(-10*time.Minute).Unix(), "5", "5"),     // gone
		peer(0, "0", "0"), // never connected
	)
	p, err := ParseDump(out, now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Online != 2 {
		t.Errorf("online = %d, want 2", p.Online)
	}
	if p.Bytes != 312 {
		t.Errorf("bytes = %d, want 312", p.Bytes)
	}
}

func TestParseDumpTreatsFirstLinePerInterfaceAsTheInterface(t *testing.T) {
	// A WireGuard-width interface line (5 fields) must not be read as a
	// short peer line, and a second interface starts its own header.
	out := []byte(strings.Join([]string{
		"wg0\tPRIV\tPUB\t51820\toff",
		strings.Replace(peer(now.Unix(), "10", "10"), "awg0", "wg0", 1),
		"awg0\tPRIV\tPUB\t51821\toff\t4\t40\t70\t0\t0\t1\t2\t3\t4",
		peer(now.Unix(), "1", "1"),
	}, "\n"))
	p, err := ParseDump(out, now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Online != 2 || p.Bytes != 22 {
		t.Errorf("got %+v, want 2 online and 22 bytes", p)
	}
}

func TestParseDumpRejectsEmptyAndMalformed(t *testing.T) {
	if _, err := ParseDump(nil, now); err == nil {
		t.Error("empty dump: want an error")
	}
	if _, err := ParseDump(awgDump("awg0\tonly\tthree"), now); err == nil {
		t.Error("short peer line: want an error")
	}
}

func TestProcParsers(t *testing.T) {
	a, err := ParseProcStat([]byte("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 2 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ParseProcStat([]byte("cpu  150 0 150 750 150 0 0 0 0 0\n"))
	// 100 busy ticks out of 200.
	if got := CPUPercent(a, b); got != 50 {
		t.Errorf("cpu = %v, want 50", got)
	}

	mem, err := ParseMeminfo([]byte("MemTotal:  1000 kB\nMemFree: 100 kB\nMemAvailable:  250 kB\n"))
	if err != nil || mem != 75 {
		t.Errorf("mem = %v, %v; want 75", mem, err)
	}

	up, err := ParseUptime([]byte("93784.51 180000.00\n"))
	if err != nil || up != 93784 {
		t.Errorf("uptime = %v, %v; want 93784", up, err)
	}
}

func TestMbpsIgnoresCounterReset(t *testing.T) {
	if got := Mbps(1_000_000, 2_250_000, time.Second); got != 10 {
		t.Errorf("rate = %v, want 10", got)
	}
	if got := Mbps(5_000_000, 100, time.Second); got != 0 {
		t.Errorf("after an interface restart the rate = %v, want 0", got)
	}
}

func TestCollectorNeedsTwoSamplesAndKeepsTwoMinutes(t *testing.T) {
	bytes := uint64(0)
	c := NewCollector(Readers{
		Dump: func(context.Context) ([]byte, error) {
			return awgDump(peer(now.Unix(), itoa(int64(bytes)), "0")), nil
		},
		ProcStat: func() ([]byte, error) { return []byte("cpu 1 0 1 8 0\n"), nil },
		Meminfo:  func() ([]byte, error) { return []byte("MemTotal: 100 kB\nMemAvailable: 60 kB\n"), nil },
		Uptime:   func() ([]byte, error) { return []byte("42.0 0\n"), nil },
	})

	c.Sample(context.Background(), now)
	if _, ok, _ := c.Latest(); ok {
		t.Fatal("one sample cannot give a rate; want not ready")
	}

	for i := 1; i <= HistoryLen+30; i++ {
		bytes += 1_250_000 // 10 Mbit/s
		c.Sample(context.Background(), now.Add(time.Duration(i)*time.Second))
	}
	s, ok, err := c.Latest()
	if !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if s.ThroughputMbps != 10 || s.ClientsOnline != 1 || s.MemPct != 40 || s.UptimeS != 42 {
		t.Errorf("snapshot = %+v", s)
	}
	if len(s.History) != HistoryLen {
		t.Errorf("history has %d points, want %d", len(s.History), HistoryLen)
	}
}

func TestCollectorReportsAFailedDump(t *testing.T) {
	c := NewCollector(Readers{
		Dump:     func(context.Context) ([]byte, error) { return nil, errors.New("container not running") },
		ProcStat: func() ([]byte, error) { return nil, errors.New("n/a") },
		Meminfo:  func() ([]byte, error) { return nil, errors.New("n/a") },
		Uptime:   func() ([]byte, error) { return nil, errors.New("n/a") },
	})
	c.Sample(context.Background(), now)
	if _, _, err := c.Latest(); err == nil {
		t.Error("want the dump error surfaced")
	}
}
