package serverstat

import (
	"context"
	"math"
	"sync"
	"time"
)

// Readers is where a Collector gets its raw input. The agent wires these
// to `docker exec` and /proc; tests wire them to fixed bytes.
type Readers struct {
	Dump     func(ctx context.Context) ([]byte, error)
	ProcStat func() ([]byte, error)
	Meminfo  func() ([]byte, error)
	Uptime   func() ([]byte, error)
}

// Collector samples the server on a fixed interval and keeps the latest
// Snapshot ready to serve. Serving never waits on a measurement: a page
// that polls every second cannot be held up by a slow `docker exec`.
type Collector struct {
	r Readers

	mu       sync.RWMutex
	snap     Snapshot
	err      error
	history  []Point
	prevCPU  CPUTimes
	prevByte uint64
	prevAt   time.Time
	primed   bool
}

// NewCollector returns a Collector that has not sampled yet.
func NewCollector(r Readers) *Collector {
	return &Collector{r: r}
}

// Run samples every interval until ctx ends.
func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	c.Sample(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.Sample(ctx, now)
		}
	}
}

// Sample takes one measurement. The first call only primes the counters:
// throughput and CPU are rates, and a rate needs two readings.
func (c *Collector) Sample(ctx context.Context, now time.Time) {
	dctx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()

	dump, err := c.r.Dump(dctx)
	if err != nil {
		c.fail(err)
		return
	}
	peers, err := ParseDump(dump, now)
	if err != nil {
		c.fail(err)
		return
	}

	// /proc readings are best effort: a missing figure shows as zero on
	// the card, which is better than taking the whole card down because
	// one file was unreadable.
	var cpu CPUTimes
	if b, err := c.r.ProcStat(); err == nil {
		cpu, _ = ParseProcStat(b)
	}
	var mem float64
	if b, err := c.r.Meminfo(); err == nil {
		mem, _ = ParseMeminfo(b)
	}
	var up int64
	if b, err := c.r.Uptime(); err == nil {
		up, _ = ParseUptime(b)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.primed {
		c.prevCPU, c.prevByte, c.prevAt, c.primed = cpu, peers.Bytes, now, true
		return
	}

	rate := round1(Mbps(c.prevByte, peers.Bytes, now.Sub(c.prevAt)))
	cpuPct := round1(CPUPercent(c.prevCPU, cpu))
	c.prevCPU, c.prevByte, c.prevAt = cpu, peers.Bytes, now

	stamp := now.UTC().Format(time.RFC3339)
	c.history = append(c.history, Point{T: stamp, V: rate})
	if len(c.history) > HistoryLen {
		c.history = c.history[len(c.history)-HistoryLen:]
	}

	c.snap = Snapshot{
		GeneratedAt:    stamp,
		ClientsOnline:  peers.Online,
		ThroughputMbps: rate,
		CPUPct:         cpuPct,
		MemPct:         round1(mem),
		UptimeS:        up,
		History:        append([]Point(nil), c.history...),
	}
	c.err = nil
}

func (c *Collector) fail(err error) {
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
}

// Latest returns the most recent Snapshot, and the error from the most
// recent attempt if it failed. ok is false until two samples have been
// taken.
func (c *Collector) Latest() (snap Snapshot, ok bool, err error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap, c.snap.GeneratedAt != "", c.err
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
