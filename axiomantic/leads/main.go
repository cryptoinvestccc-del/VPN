// Command axiomantic-leads takes the order forms of the Axiomantic site.
//
// It accepts POST /api/lead, checks the form the same way the page does,
// appends it to a local file readable only by the service user and, when
// a bot is configured, forwards a copy to a Telegram chat. It never
// returns a lead over HTTP.
//
//	axiomantic-leads -store /var/lib/axiomantic/leads.jsonl
//	axiomantic-leads -store /tmp/leads.jsonl -static axiomantic/site   # local preview
//
// The bot token and chat come from the environment (AXM_TG_TOKEN,
// AXM_TG_CHAT), never from flags, which any user on the host can read in
// the process list.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "listen address; keep it on loopback behind nginx")
	store := flag.String("store", "", "file the leads are appended to (required, outside the site folder)")
	static := flag.String("static", "", "serve this folder as the site too (for previews; nginx does it in production)")
	origins := flag.String("origins", "", "comma-separated site origins allowed to post, e.g. https://axiomantic.ru; empty means same host only")
	trustProxy := flag.Bool("trust-proxy", false, "take the client address from X-Real-IP set by a reverse proxy on loopback")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	if *store == "" {
		logger.Fatal("-store is required: every lead is written there first")
	}
	if err := checkOutside(*store, *static); err != nil {
		logger.Fatal(err)
	}
	fs, err := OpenFileStore(*store)
	if err != nil {
		logger.Fatal(err)
	}
	defer fs.Close()

	srv := &Server{
		store:      fs,
		limit:      NewLimiter(10*time.Minute, 5, 60),
		origins:    map[string]bool{},
		trustProxy: *trustProxy,
		static:     *static,
		log:        logger,
		now:        time.Now,
	}
	for _, o := range strings.Split(*origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			srv.origins[strings.TrimRight(o, "/")] = true
		}
	}
	token, chat := os.Getenv("AXM_TG_TOKEN"), os.Getenv("AXM_TG_CHAT")
	switch {
	case token != "" && chat != "":
		srv.notify = NewTelegram(token, chat)
		logger.Print("leads are also forwarded to Telegram")
	case token != "" || chat != "":
		logger.Fatal("set both AXM_TG_TOKEN and AXM_TG_CHAT, or neither")
	}

	hs := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(shut)
	}()
	logger.Printf("listening on %s", *addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
	srv.Wait()
}
