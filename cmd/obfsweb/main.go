// Command obfsweb serves the public landing page and its read-only JSON
// API.
//
// This is the one component of the project that is meant to be reachable
// from the internet by anyone, and it should not share a host with a
// node. The whole point of the obfuscator is that a passive observer
// cannot tell a VPN runs on an address; a page that says "this is a VPN"
// served from that same address gives it away in the clearest possible
// terms. Run it somewhere else.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"io/fs"

	"github.com/cryptoinvestccc-del/vpn/internal/webapi"
	"github.com/cryptoinvestccc-del/vpn/internal/webui"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	static := flag.String("static", "", "directory of built assets; empty uses the embedded build")
	flag.Parse()

	if err := run(*addr, *static); err != nil {
		log.Fatalf("obfsweb: %v", err)
	}
}

func run(addr, static string) error {
	assets, err := resolveAssets(static)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", webapi.Handler(webapi.SampleSource{}, time.Now))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/", webui.Handler(assets))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("obfsweb: serving on %s (figures are the built-in sample set)", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("obfsweb: stopped")
	return nil
}

// resolveAssets prefers what the caller named, then the embedded build,
// then the conventional development path — and, failing all three, says
// which of the two commands the operator forgot to run rather than
// starting up and serving 404s.
func resolveAssets(static string) (fs.FS, error) {
	if static != "" {
		return webui.Assets(static)
	}

	if assets, err := webui.Assets(""); err == nil {
		return assets, nil
	}

	const devPath = "web/dist"
	assets, err := webui.Assets(devPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"no built assets: run `npm install && npm run build` in web/, "+
					"or pass -static with the directory they are in (looked in %q)", devPath)
		}
		return nil, err
	}
	log.Printf("obfsweb: serving assets from %s", devPath)
	return assets, nil
}
