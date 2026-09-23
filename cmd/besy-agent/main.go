// Command besy-agent runs on the Amnezia VPN server and reports its load
// to the BESY website once a second.
//
// It reads two things: `awg show all dump` from inside Amnezia's
// AmneziaWG container, for how many clients are connected and how much
// traffic they move, and the host's /proc, for CPU, memory and uptime.
// It serves the result as one small JSON document for cmd/obfsweb to
// fetch. It never serves anything per client: see internal/serverstat.
//
// It runs as root because `docker exec` needs to. It changes nothing on
// the server and writes nothing to disk.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/serverstat"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9180", "address to serve the snapshot on")
	token := flag.String("token", os.Getenv("BESY_AGENT_TOKEN"), "shared secret the site must send; required unless listening on loopback (env BESY_AGENT_TOKEN)")
	container := flag.String("container", "", "AmneziaWG container name; empty finds amnezia-awg2 or amnezia-awg")
	interval := flag.Duration("interval", time.Second, "how often to sample")
	flag.Parse()

	if err := run(*listen, *token, *container, *interval); err != nil {
		log.Fatalf("besy-agent: %v", err)
	}
}

func run(listen, token, container string, interval time.Duration) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("-listen: %w", err)
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && token == "" {
		// Reachable from outside and unauthenticated is how a status
		// endpoint becomes someone else's scraping target.
		return errors.New("listening beyond loopback needs -token")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dump := &dumper{container: container}
	c := serverstat.NewCollector(serverstat.Readers{
		Dump:     dump.read,
		ProcStat: func() ([]byte, error) { return os.ReadFile("/proc/stat") },
		Meminfo:  func() ([]byte, error) { return os.ReadFile("/proc/meminfo") },
		Uptime:   func() ([]byte, error) { return os.ReadFile("/proc/uptime") },
	})
	go c.Run(ctx, interval)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		snap, ok, err := c.Latest()
		w.Header().Set("Cache-Control", "no-store")
		if err != nil || !ok {
			// The reason stays in the agent's log. The site only needs
			// to know that there is no fresh reading.
			if err != nil {
				log.Printf("sample failed: %v", err)
			}
			http.Error(w, "no fresh sample", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(snap)
	})

	srv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("serving on %s, sampling every %s", listen, interval)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func authorized(r *http.Request, token string) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// dumper runs `awg show all dump` in the AmneziaWG container. Amnezia has
// shipped the container under two names and the tool under two names
// (amnezia-awg with wg, amnezia-awg2 with awg), so both are found once and
// remembered rather than guessed on every call.
type dumper struct {
	container string

	mu   sync.Mutex
	tool string
}

func (d *dumper) read(ctx context.Context) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.container == "" {
		name, err := findContainer(ctx)
		if err != nil {
			return nil, err
		}
		d.container = name
		log.Printf("using container %s", name)
	}

	tools := []string{"awg", "wg"}
	if d.tool != "" {
		tools = []string{d.tool}
	}
	var lastErr error
	for _, tool := range tools {
		out, err := exec.CommandContext(ctx, "docker", "exec", d.container, tool, "show", "all", "dump").Output()
		if err == nil {
			d.tool = tool
			return out, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("docker exec %s: %w", d.container, lastErr)
}

func findContainer(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return "", fmt.Errorf("docker ps: %w", err)
	}
	names := strings.Fields(string(out))
	for _, want := range []string{"amnezia-awg2", "amnezia-awg"} {
		for _, n := range names {
			if n == want {
				return n, nil
			}
		}
	}
	return "", fmt.Errorf("no amnezia-awg2 or amnezia-awg container running (found: %s); pass -container", strings.Join(names, ", "))
}
