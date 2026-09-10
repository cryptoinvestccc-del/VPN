package metrics

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// Serve exposes the registry over HTTP until ctx is cancelled.
//
// The listener belongs on loopback. A public metrics endpoint gives away
// both that this host runs a VPN — which is the one thing the rest of the
// project works to hide — and, if per-client counters are on, who uses it
// and how much. Prometheus should reach it over an SSH tunnel or a
// private interface, not the internet.
func Serve(ctx context.Context, addr string, registry *Registry) error {
	if addr == "" {
		return nil
	}
	warnIfPubliclyReachable(addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(registry.Expose()))
	})
	// A liveness probe that says nothing about what the service is.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("metrics: serving on %s/metrics", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// warnIfPubliclyReachable says something when the metrics listener is
// bound anywhere but loopback. It is a warning rather than a refusal:
// a private interface or a container network is a legitimate place for
// it, and only the operator knows which of those an address is.
func warnIfPubliclyReachable(addr string) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		log.Printf("metrics: %s listens on every interface. If that reaches the internet, "+
			"it announces that this host runs a VPN and exposes its usage counters. "+
			"Bind it to 127.0.0.1 and reach it over SSH or a private network.", addr)
		return
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip != nil && !ip.IsLoopback() && !ip.IsPrivate() {
		log.Printf("metrics: %s is a public address. It announces that this host runs a VPN "+
			"and exposes its usage counters; bind it to 127.0.0.1 instead.", addr)
	}
}
