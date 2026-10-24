// Command axiomantic-leads takes the order forms of the Axiomantic site
// and, with -admin, runs the team panel where those leads are worked on.
//
// It accepts POST /api/lead, checks the form the same way the page does,
// appends it to a local file readable only by the service user and, when
// a bot is configured, forwards a copy to a Telegram chat. The public
// endpoint never returns a lead; the panel at /admin/ does, only to a
// signed-in member of the workspace the lead belongs to.
//
//	axiomantic-leads -store /var/lib/axiomantic/leads.jsonl
//	axiomantic-leads -store /var/lib/axiomantic/leads.jsonl -admin /etc/axiomantic/admin.json
//	axiomantic-leads -store /tmp/leads.jsonl -static axiomantic/site   # local preview
//	axiomantic-leads admin …                                           # users and workspaces
//
// The bot token and chat come from the environment (AXM_TG_TOKEN,
// AXM_TG_CHAT), never from flags, which any user on the host can read in
// the process list. Passwords are never flags either.
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
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		os.Exit(adminCLI(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	addr := flag.String("addr", "127.0.0.1:8090", "listen address; keep it on loopback behind nginx")
	store := flag.String("store", "", "file the leads are appended to (required, outside the site folder)")
	static := flag.String("static", "", "serve this folder as the site too (for previews; nginx does it in production)")
	origins := flag.String("origins", "", "comma-separated site origins allowed to post, e.g. https://axiomantic.ru; empty means same host only")
	trustProxy := flag.Bool("trust-proxy", false, "take the client address from X-Real-IP set by a reverse proxy on loopback")
	adminPath := flag.String("admin", "", "admin.json with workspaces and users (mode 600); turns on the team panel at /admin/")
	adminUI := flag.String("admin-ui-dir", "", "serve the panel's files from this folder, re-read on every request (for working on the panel)")
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
		limit:      NewLimiter(10*time.Minute, 5, 1<<30),
		ceiling:    NewLimiter(10*time.Minute, 60, 1<<30),
		origins:    map[string]bool{},
		trustProxy: *trustProxy,
		static:     *static,
		log:        logger,
		now:        time.Now,
	}
	if *adminPath != "" {
		if err := checkOutside(*adminPath, *static); err != nil {
			logger.Fatal("-admin: the config must be outside the published folder")
		}
		cfg, err := NewConfigSource(*adminPath, time.Now, logger.Printf)
		if err != nil {
			logger.Fatal(err)
		}
		// Leads stored before workspaces existed belong to the default one.
		// It is fixed for the life of the process: a change needs a restart.
		srv.defaultWS = cfg.Get().DefaultWorkspace
		idx := NewIndex(srv.defaultWS)
		skipped, err := fs.Load(idx.AddRecord, idx.AddEvent)
		if err != nil {
			logger.Fatal(err)
		}
		if skipped > 0 {
			logger.Printf("store: %d damaged lines skipped", skipped)
		}
		srv.admin = NewAdmin(cfg, idx, fs, time.Now, logger)
		srv.admin.uiDir = *adminUI
		srv.admin.StartScheduler(context.Background())
		logger.Printf("team panel on /admin/ for %d workspaces", len(cfg.Get().Workspaces))
	}
	for _, o := range strings.Split(*origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			// the same form as workspace origins, so a case or a trailing
			// slash cannot quietly refuse the studio's own form
			n, err := normOrigin(o)
			if err != nil {
				logger.Fatalf("-origins: %v", err)
			}
			srv.origins[n] = true
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
	// ListenAndServe returns as soon as Shutdown starts; wait for it to
	// finish, so leads already being written and their Telegram copies
	// are not cut off.
	shutDone := make(chan struct{})
	go func() {
		defer close(shutDone)
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(shut)
	}()
	logger.Printf("listening on %s", *addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
	<-shutDone
	srv.Wait()
}
