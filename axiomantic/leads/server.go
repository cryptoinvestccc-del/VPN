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
	"sync"
	"time"
)

const maxBody = 16 << 10

// Sender delivers a notification about a stored lead.
type Sender interface {
	Send(ctx context.Context, text string) error
}

// Server takes leads at POST /api/lead and, optionally, serves the site.
//
// There is no route that reads, changes or deletes a lead: the endpoint is
// write-only, so no visitor can reach someone else's lead by guessing an
// id. Leads are read on the server or in the Telegram chat.
type Server struct {
	store      *FileStore
	notify     Sender
	limit      *Limiter
	origins    map[string]bool
	trustProxy bool
	static     string
	log        *log.Logger
	now        func() time.Time
	wg         sync.WaitGroup
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lead", s.handleLead)
	if s.static != "" {
		mux.Handle("/", s.staticHandler())
	}
	return mux
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
	if !s.originAllowed(r) {
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

	rec := Record{ID: s.newID(), Time: s.now().UTC(), IP: ip, Lead: l}
	if err := s.store.Append(rec); err != nil {
		// The error is about the file, never about the visitor's data.
		s.log.Printf("lead %s: store: %v", rec.ID, err)
		writeJSON(w, http.StatusInternalServerError, reply{Error: "store"})
		return
	}
	s.log.Printf("lead %s stored (%s/%s)", rec.ID, l.Source, l.Mode)

	if s.notify != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := s.notify.Send(ctx, l.Text(rec.ID)); err != nil {
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
