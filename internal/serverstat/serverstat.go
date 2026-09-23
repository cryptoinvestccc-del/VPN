// Package serverstat measures one AmneziaWG server for the public status
// card on the landing page.
//
// It is shared by two programs. cmd/besy-agent runs on the VPN server,
// next to Amnezia's container, and produces a Snapshot once a second.
// cmd/obfsweb, which serves the site, fetches that Snapshot and hands it
// to the page. Keeping the type here means the two cannot drift apart.
//
// What a Snapshot may carry is decided by the fact that the page is
// public. Everything in it is an aggregate over the whole server: how
// many people are connected, not who; how fast the server is moving
// traffic, not whose. The raw `awg show all dump` this is computed from
// contains the interface's private key and every client's public key and
// address; none of that leaves this package.
package serverstat

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OnlineWindow is how recent a handshake must be for a client to count
// as connected. WireGuard re-handshakes every two minutes while traffic
// flows, so three minutes covers one missed rekey without counting
// someone who closed the app an hour ago.
const OnlineWindow = 3 * time.Minute

// HistoryLen is how many one-second throughput samples a Snapshot keeps
// for the chart: two minutes.
const HistoryLen = 120

// Point is one throughput sample, in Mbit/s.
type Point struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

// Snapshot is the server's state at one instant.
type Snapshot struct {
	GeneratedAt string `json:"generated_at"`

	// ClientsOnline counts clients with a handshake inside OnlineWindow.
	ClientsOnline int `json:"clients_online"`

	// ThroughputMbps is both directions summed, over the last interval.
	ThroughputMbps float64 `json:"throughput_mbps"`

	CPUPct  float64 `json:"cpu_pct"`
	MemPct  float64 `json:"mem_pct"`
	UptimeS int64   `json:"uptime_s"`

	// History is ThroughputMbps over the last HistoryLen samples, oldest
	// first, so the page can draw a line without keeping state.
	History []Point `json:"history"`
}

// Peers is what the dump says about clients, reduced to aggregates.
type Peers struct {
	Online int
	// Bytes is received plus sent, summed over every peer. It only ever
	// grows until the interface restarts, which is why throughput is
	// computed from its difference and guarded against going negative.
	Bytes uint64
}

// ParseDump reads the output of `awg show all dump` (or `wg show all
// dump`; the format is the same).
//
// Each interface contributes one line describing itself and then one
// line per peer. The interface line's width differs between WireGuard
// and the AmneziaWG versions (AmneziaWG appends its obfuscation
// parameters), so lines are not told apart by field count: the first
// line seen for an interface name is that interface, and the rest are
// its peers. A peer line is, after the interface name: public key,
// preshared key, endpoint, allowed IPs, latest handshake (unix seconds,
// 0 for never), bytes received, bytes sent, keepalive.
func ParseDump(out []byte, now time.Time) (Peers, error) {
	var p Peers
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		iface := f[0]
		if !seen[iface] {
			seen[iface] = true
			continue
		}
		if len(f) < 9 {
			return Peers{}, fmt.Errorf("peer line has %d fields, want at least 9", len(f))
		}

		handshake, err := strconv.ParseInt(f[5], 10, 64)
		if err != nil {
			return Peers{}, fmt.Errorf("latest handshake: %w", err)
		}
		rx, err := strconv.ParseUint(f[6], 10, 64)
		if err != nil {
			return Peers{}, fmt.Errorf("bytes received: %w", err)
		}
		tx, err := strconv.ParseUint(f[7], 10, 64)
		if err != nil {
			return Peers{}, fmt.Errorf("bytes sent: %w", err)
		}

		if handshake > 0 && now.Sub(time.Unix(handshake, 0)) < OnlineWindow {
			p.Online++
		}
		p.Bytes += rx + tx
	}
	if err := sc.Err(); err != nil {
		return Peers{}, err
	}
	if len(seen) == 0 {
		return Peers{}, errors.New("dump is empty: no interfaces")
	}
	return p, nil
}

// CPUTimes is the first line of /proc/stat, reduced to what a busy
// percentage needs.
type CPUTimes struct {
	Busy, Total uint64
}

// ParseProcStat reads the aggregate "cpu" line of /proc/stat.
func ParseProcStat(b []byte) (CPUTimes, error) {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	f := strings.Fields(string(line))
	if len(f) < 5 || f[0] != "cpu" {
		return CPUTimes{}, errors.New("/proc/stat: no aggregate cpu line")
	}
	var t CPUTimes
	for i, s := range f[1:] {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return CPUTimes{}, fmt.Errorf("/proc/stat: %w", err)
		}
		t.Total += v
		// Fields 4 and 5 are idle and iowait; everything else is work.
		if i != 3 && i != 4 {
			t.Busy += v
		}
	}
	return t, nil
}

// CPUPercent is the busy share between two readings.
func CPUPercent(prev, cur CPUTimes) float64 {
	if cur.Total <= prev.Total || cur.Busy < prev.Busy {
		return 0
	}
	return 100 * float64(cur.Busy-prev.Busy) / float64(cur.Total-prev.Total)
}

// ParseMeminfo returns the share of memory in use, counting reclaimable
// cache as free — which is what MemAvailable already does.
func ParseMeminfo(b []byte) (float64, error) {
	var total, avail uint64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total == 0 || avail > total {
		return 0, errors.New("/proc/meminfo: no MemTotal or MemAvailable")
	}
	return 100 * float64(total-avail) / float64(total), nil
}

// ParseUptime reads /proc/uptime.
func ParseUptime(b []byte) (int64, error) {
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, errors.New("/proc/uptime: empty")
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/uptime: %w", err)
	}
	return int64(v), nil
}

// Mbps turns a byte delta over an interval into megabits per second. A
// negative delta means the interface restarted and its counters went
// back to zero; that interval has no meaningful rate, so it reads as 0
// rather than as a huge negative spike.
func Mbps(prevBytes, curBytes uint64, dt time.Duration) float64 {
	if curBytes < prevBytes || dt <= 0 {
		return 0
	}
	return float64(curBytes-prevBytes) * 8 / dt.Seconds() / 1e6
}
