package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/serverstat"
)

// Server is the live status card on the landing page: one VPN server,
// measured once a second.
type Server struct {
	serverstat.Snapshot

	// Mock is true for the built-in sample, as everywhere in this API.
	Mock bool `json:"mock"`

	// Reachable is false when the site could not get a fresh reading
	// from the server. The page shows that as "нет связи", which is the
	// honest thing to show: from a visitor's side it is indistinguishable
	// from the server being down.
	Reachable bool `json:"reachable"`
}

// sampleServer is a plausible single server: a few dozen people online,
// traffic that breathes on a scale of seconds, CPU that follows traffic.
// It is a pure function of the second, so a page polling once a second
// sees a line that scrolls rather than one that is redrawn at random.
func sampleServer(now time.Time) Server {
	sec := now.Unix()
	rate := func(s int64) float64 {
		x := float64(s)
		v := 38 + 14*math.Sin(x/23) + 7*math.Sin(x/7.3+1) + 4*math.Sin(x/2.9+2)
		return math.Round(v*10) / 10
	}

	hist := make([]serverstat.Point, serverstat.HistoryLen)
	for i := range hist {
		s := sec - int64(serverstat.HistoryLen-1-i)
		hist[i] = serverstat.Point{T: time.Unix(s, 0).UTC().Format(time.RFC3339), V: rate(s)}
	}
	cur := rate(sec)

	return Server{
		Snapshot: serverstat.Snapshot{
			GeneratedAt:    now.UTC().Format(time.RFC3339),
			ClientsOnline:  int(math.Round(27 + 5*math.Sin(float64(sec)/97))),
			ThroughputMbps: cur,
			CPUPct:         math.Round((6+cur/5)*10) / 10,
			MemPct:         math.Round((34+2*math.Sin(float64(sec)/300))*10) / 10,
			// Counted from a fixed boot so it ticks up like the real one.
			UptimeS: sec - 1_790_000_000,
			History: hist,
		},
		Mock:      true,
		Reachable: true,
	}
}

// AgentSource serves everything from Inner except the server card, which
// it fetches from a besy-agent running on the VPN server.
type AgentSource struct {
	Source
	URL    string // e.g. http://10.0.0.5:9180/v1/snapshot
	Token  string
	Client *http.Client

	mu     sync.Mutex
	cached Server
	at     time.Time
}

// Server returns the agent's latest reading. Every visitor's page polls
// once a second; the agent is asked at most once a second in total,
// however many visitors there are.
func (a *AgentSource) Server(now time.Time) Server {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.at.IsZero() && now.Sub(a.at) < time.Second {
		return a.cached
	}
	a.at = now

	snap, err := a.fetch()
	if err != nil {
		a.cached = Server{
			// An empty list, not null: the page reads its length.
			Snapshot:  serverstat.Snapshot{GeneratedAt: now.UTC().Format(time.RFC3339), History: []serverstat.Point{}},
			Reachable: false,
		}
		return a.cached
	}
	if snap.History == nil {
		snap.History = []serverstat.Point{}
	}
	a.cached = Server{Snapshot: snap, Reachable: true}
	return a.cached
}

func (a *AgentSource) fetch() (serverstat.Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return serverstat.Snapshot{}, err
	}
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	}
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return serverstat.Snapshot{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return serverstat.Snapshot{}, fmt.Errorf("agent: %s", res.Status)
	}

	var snap serverstat.Snapshot
	dec := json.NewDecoder(http.MaxBytesReader(nil, res.Body, 64<<10))
	if err := dec.Decode(&snap); err != nil {
		return serverstat.Snapshot{}, fmt.Errorf("agent: %w", err)
	}
	return snap, nil
}
