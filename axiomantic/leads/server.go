package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBody = 16 << 10

// Sender delivers a notification about a stored lead to a chat; an empty
// chat means the default one.
type Sender interface {
	Send(ctx context.Context, chat, text string) error
}

// Server takes leads at POST /api/lead and, optionally, serves the site.
//
// The public endpoint is write-only: no visitor can read anything back
// from it. Leads are read in the team panel (/admin/, only with a session
// and only within the user's own workspace), on the server or in the
// Telegram chat.
type Server struct {
	store      *FileStore
	notify     Sender
	limit      *Limiter // attempts per address
	ceiling    *Limiter // stored leads for everyone together (key "all"); nil means none
	origins    map[string]bool
	trustProxy bool
	static     string
	log        *log.Logger
	now        func() time.Time
	wg         sync.WaitGroup

	// admin is nil unless the server was started with -admin.
	admin     *Admin
	defaultWS string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lead", s.handleLead)
	if s.admin != nil {
		s.admin.clientIP = s.clientIP
		s.admin.hasNotify = func(w *Workspace) bool { return s.notify != nil && s.chatFor(w) != "-" }
		s.admin.routes(mux)
	}
	if s.static != "" {
		mux.Handle("/", s.staticHandler())
	}
	return mux
}

// workspaceID is the default workspace: leads stored before workspaces
// existed and leads posted without an Origin belong to it.
func (s *Server) workspaceID() string { return s.defaultWS }

// chatFor picks the Telegram chat for a workspace's leads: its own chat,
// or the default chat for the default workspace only — a client's leads
// never land in the studio's chat by accident. "-" means none.
func (s *Server) chatFor(w *Workspace) string {
	if w == nil {
		if s.admin == nil {
			return ""
		}
		return "-"
	}
	if w.TelegramChat != "" {
		return w.TelegramChat
	}
	if w.ID == s.workspaceID() {
		return ""
	}
	return "-"
}

// leadWorkspace decides whose site a lead came from. With the panel on,
// a browser's Origin must be listed by a workspace (or in -origins, which
// means the default one) — there is no same-host guess, so a client's
// site reached under an unlisted name is refused rather than filed under
// the studio. A request without Origin is not a browser form and goes to
// the default workspace. Without the panel the old rules stand and leads
// carry no workspace at all.
func (s *Server) leadWorkspace(r *http.Request) (*Workspace, string, bool) {
	if s.admin == nil {
		return nil, "", s.originAllowed(r)
	}
	cfg := s.admin.cfg.Get()
	o := r.Header.Get("Origin")
	if o == "" {
		return cfg.Workspace(s.workspaceID()), s.workspaceID(), true
	}
	if w := cfg.WorkspaceForOrigin(o); w != nil {
		return w, w.ID, true
	}
	if s.origins[o] {
		return cfg.Workspace(s.workspaceID()), s.workspaceID(), true
	}
	return nil, "", false
}

// Wait blocks until notifications already in flight have finished.
func (s *Server) Wait() { s.wg.Wait() }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type reply struct {
	OK    bool   `json:"ok"`
	Field string `json:"field,omitempty"`
	Error string `json:"error,omitempty"`
}

func (s *Server) handleLead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, reply{Error: "method"})
		return
	}
	ws, wsID, ok := s.leadWorkspace(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, reply{Error: "origin"})
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, reply{Error: "content-type"})
		return
	}
	ip := s.clientIP(r)
	if ok, retry := s.limit.Allow(ip, s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, reply{Error: "rate"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var l Lead
	if err := dec.Decode(&l); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, reply{Error: "size"})
			return
		}
		writeJSON(w, http.StatusBadRequest, reply{Error: "json"})
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, reply{Error: "json"})
		return
	}

	l.Normalize()
	if l.Website != "" {
		// A bot filled the hidden field. Answer as if it worked, so that
		// it has no reason to try again differently.
		writeJSON(w, http.StatusOK, reply{OK: true})
		return
	}
	if field := l.Validate(); field != "" {
		writeJSON(w, http.StatusBadRequest, reply{Field: field, Error: "invalid"})
		return
	}
	// The ceiling for everyone together counts only leads about to be
	// stored: junk from many addresses cannot use it up and shut the form
	// for real visitors, while a flood of valid-looking leads still stops
	// before it fills the chat.
	if s.ceiling != nil {
		if over, retry := s.ceiling.Over("all", s.now()); over {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			writeJSON(w, http.StatusTooManyRequests, reply{Error: "rate"})
			return
		}
		s.ceiling.Hit("all", s.now())
	}

	rec := Record{ID: s.newID(), Time: s.now().UTC(), IP: ip, WS: wsID, Lead: l}
	if err := s.store.Append(rec); err != nil {
		// The error is about the file, never about the visitor's data.
		s.log.Printf("lead %s: store: %v", rec.ID, err)
		writeJSON(w, http.StatusInternalServerError, reply{Error: "store"})
		return
	}
	if s.admin != nil {
		s.admin.index.AddRecord(rec)
		s.log.Printf("lead %s stored (%s/%s/%s)", rec.ID, wsID, l.Source, l.Mode)
	} else {
		s.log.Printf("lead %s stored (%s/%s)", rec.ID, l.Source, l.Mode)
	}

	chat := s.chatFor(ws)
	if s.notify != nil && chat != "-" {
		text := l.Text(rec.ID)
		if s.admin != nil && ws != nil && ws.SiteURL != "" {
			text += "\nОткрыть: " + strings.TrimRight(ws.SiteURL, "/") + "/admin/#/leads/" + rec.ID
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := s.notify.Send(ctx, chat, text); err != nil {
				s.log.Printf("lead %s: notify: %v", rec.ID, err)
			}
		}()
	}
	writeJSON(w, http.StatusOK, reply{OK: true})
}

// originAllowed turns away form posts that a browser makes from another
// site. Without a configured list the page must come from the same host
// the request was sent to. Requests without Origin are not from a
// browser form and are left to the rate limit.
func (s *Server) originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	if len(s.origins) > 0 {
		return s.origins[o]
	}
	u, err := url.Parse(o)
	return err == nil && u.Host != "" && u.Host == r.Host
}

// clientIP is the peer address, or the X-Real-IP header when the peer is
// the local reverse proxy and the server was told to trust it.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.trustProxy {
		if peer := net.ParseIP(host); peer != nil && peer.IsLoopback() {
			if real := net.ParseIP(r.Header.Get("X-Real-IP")); real != nil {
				return real.String()
			}
		}
	}
	return host
}

func (s *Server) newID() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return s.now().UTC().Format("20060102") + "-" + hex.EncodeToString(b[:])
}

// staticHandler serves the built site the way nginx is configured to:
// folders answer with their index.html, nothing is listed, and a missing
// page gets the site's own 404.
func (s *Server) staticHandler() http.Handler {
	files := http.FileServer(http.Dir(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setPageHeaders(w)
		p := path.Clean("/" + r.URL.Path)
		full := filepath.Join(s.static, filepath.FromSlash(p))
		st, err := os.Stat(full)
		if err == nil && st.IsDir() {
			_, err = os.Stat(filepath.Join(full, "index.html"))
		}
		if err != nil {
			notFound, rerr := os.ReadFile(filepath.Join(s.static, "404", "index.html"))
			if rerr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(notFound)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// CSP matches what the pages need: their own files, the Yandex map frame
// on the contacts page, and inline JSON-LD (which is never executed).
const csp = "default-src 'self'; img-src 'self' data:; style-src 'self'; font-src 'self'; script-src 'self'; " +
	"connect-src 'self'; frame-src https://yandex.ru; form-action 'self'; base-uri 'self'; frame-ancestors 'none'"

func setPageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}
