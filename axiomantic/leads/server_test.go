package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Shaped unlike a real bot token, so secret scanners do not flag the repo.
const testToken = "0000000000:test-token-must-never-be-logged"

type harness struct {
	t      *testing.T
	srv    *Server
	h      http.Handler
	store  string
	logs   *bytes.Buffer
	mu     sync.Mutex
	tgHits []string // bodies the fake Telegram received
	tgPath []string
}

func newHarness(t *testing.T, tgStatus int) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{t: t, store: filepath.Join(dir, "data", "leads.jsonl"), logs: &bytes.Buffer{}}
	fs, err := OpenFileStore(h.store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.tgHits = append(h.tgHits, string(body))
		h.tgPath = append(h.tgPath, r.URL.Path)
		h.mu.Unlock()
		w.WriteHeader(tgStatus)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	t.Cleanup(tg.Close)
	notifier := NewTelegram(testToken, "-100500")
	notifier.base = tg.URL
	h.srv = &Server{
		store:   fs,
		notify:  notifier,
		limit:   NewLimiter(10*time.Minute, 5, 60),
		origins: map[string]bool{},
		log:     log.New(h.logs, "", 0),
		now:     time.Now,
	}
	h.h = h.srv.Handler()
	return h
}

func (h *harness) post(body any, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	var raw []byte
	switch b := body.(type) {
	case string:
		raw = []byte(b)
	default:
		raw, _ = json.Marshal(b)
	}
	req := httptest.NewRequest(http.MethodPost, "http://axiomantic.example/api/lead", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.7:51000"
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	h.srv.Wait()
	return rec
}

func (h *harness) stored() []Record {
	h.t.Helper()
	f, err := os.Open(h.store)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			h.t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		out = append(out, r)
	}
	return out
}

func callLead() map[string]any {
	return map[string]any{
		"source": "modal", "mode": "call", "name": "Марина", "phone": "+7 (900) 123-45-67",
		"when": "hour", "interest": "landing", "website": "", "consent": true,
	}
}

func writeLead() map[string]any {
	return map[string]any{
		"source": "modal", "mode": "write", "name": "Алексей", "channel": "telegram",
		"contact": "@alexey_d", "message": "Нужен сайт бюро", "interest": "multi-base", "website": "", "consent": true,
	}
}

func formLead() map[string]any {
	return map[string]any{
		"source": "contacts", "mode": "form", "name": "Ольга", "contact": "olga@example.ru",
		"budget": "100-300", "message": "Интернет-магазин керамики с оплатой", "website": "", "consent": true,
	}
}

func with(base map[string]any, k string, v any) map[string]any {
	out := map[string]any{}
	for kk, vv := range base {
		out[kk] = vv
	}
	if v == nil {
		delete(out, k)
	} else {
		out[k] = v
	}
	return out
}

func TestEachModeIsStoredAndForwarded(t *testing.T) {
	for name, lead := range map[string]map[string]any{"call": callLead(), "write": writeLead(), "form": formLead()} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, http.StatusOK)
			rec := h.post(lead)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			got := h.stored()
			if len(got) != 1 || got[0].Lead.Name != lead["name"] || got[0].IP != "203.0.113.7" {
				t.Fatalf("stored %+v", got)
			}
			if len(h.tgHits) != 1 || !strings.Contains(h.tgHits[0], lead["name"].(string)) {
				t.Fatalf("telegram got %q", h.tgHits)
			}
			if h.tgPath[0] != "/bot"+testToken+"/sendMessage" {
				t.Fatalf("telegram path %q", h.tgPath[0])
			}
			if strings.Contains(h.tgHits[0], "parse_mode") {
				t.Fatal("notifications must be plain text")
			}
		})
	}
}

func TestStoreFileIsPrivate(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	h.post(callLead())
	st, err := os.Stat(h.store)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("store mode %v, want 0600", st.Mode().Perm())
	}
	// A file left with loose permissions is tightened on open.
	loose := filepath.Join(t.TempDir(), "old.jsonl")
	if err := os.WriteFile(loose, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fs, err := OpenFileStore(loose)
	if err != nil {
		t.Fatal(err)
	}
	fs.Close()
	if st, _ := os.Stat(loose); st.Mode().Perm() != 0o600 {
		t.Fatalf("existing store left at %v", st.Mode().Perm())
	}
}

func TestInvalidLeadsAreRejected(t *testing.T) {
	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"no consent", with(callLead(), "consent", false), "consent"},
		{"one-letter name", with(callLead(), "name", "М"), "name"},
		{"name with control char", with(callLead(), "name", "Ma\x07rina"), "name"},
		{"short phone", with(callLead(), "phone", "12345"), "phone"},
		{"phone with letters", with(callLead(), "phone", "+7 900 CALL ME"), "phone"},
		{"unknown call time", with(callLead(), "when", "never"), "when"},
		{"unknown mode", with(callLead(), "mode", "fax"), "mode"},
		{"form mode from the dialog", with(formLead(), "source", "modal"), "mode"},
		{"call mode from contacts", with(callLead(), "source", "contacts"), "mode"},
		{"unknown channel", with(writeLead(), "channel", "pigeon"), "channel"},
		{"bad telegram nick", with(writeLead(), "contact", "@ab"), "contact"},
		{"email channel without email", with(with(writeLead(), "channel", "email"), "contact", "olga"), "contact"},
		{"whatsapp without number", with(with(writeLead(), "channel", "whatsapp"), "contact", "@olga_s"), "contact"},
		{"form without contact", with(formLead(), "contact", "olga"), "contact"},
		{"form message too short", with(formLead(), "message", "сайт"), "message"},
		{"message too long", with(writeLead(), "message", strings.Repeat("я", 3001)), "message"},
		{"unknown budget", with(formLead(), "budget", "million"), "budget"},
		{"unknown interest", with(callLead(), "interest", "rocket"), "interest"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, http.StatusOK)
			rec := h.post(c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			var r reply
			_ = json.Unmarshal(rec.Body.Bytes(), &r)
			if r.Field != c.field {
				t.Fatalf("field %q, want %q", r.Field, c.field)
			}
			if n := len(h.stored()); n != 0 || len(h.tgHits) != 0 {
				t.Fatalf("rejected lead reached the store (%d) or chat (%d)", n, len(h.tgHits))
			}
		})
	}
}

func TestMalformedRequests(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	cases := []struct {
		name   string
		body   any
		mutate func(*http.Request)
		code   int
	}{
		{"unknown field", with(callLead(), "is_admin", true), nil, http.StatusBadRequest},
		{"two objects", `{"source":"modal"}{"source":"modal"}`, nil, http.StatusBadRequest},
		{"not json", "name=Марина", nil, http.StatusBadRequest},
		{"form encoding", callLead(), func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, http.StatusUnsupportedMediaType},
		{"too large", `{"message":"` + strings.Repeat("a", maxBody) + `"}`, nil, http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m []func(*http.Request)
			if c.mutate != nil {
				m = append(m, c.mutate)
			}
			// a fresh address each time keeps the rate limit out of it
			m = append(m, func(r *http.Request) { r.RemoteAddr = "198.51.100." + c.name[:1] + ":1" })
			if rec := h.post(c.body, m...); rec.Code != c.code {
				t.Fatalf("status %d, want %d", rec.Code, c.code)
			}
		})
	}
	if n := len(h.stored()); n != 0 {
		t.Fatalf("%d malformed requests were stored", n)
	}
}

func TestHoneypotIsAnsweredButDropped(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	rec := h.post(with(callLead(), "website", "https://spam.example"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if len(h.stored()) != 0 || len(h.tgHits) != 0 {
		t.Fatal("a bot's lead was kept")
	}
}

func TestOnlyPostIsAccepted(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(m, "http://axiomantic.example/api/lead?id=20261008-0011223344", nil)
		rec := httptest.NewRecorder()
		h.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s answered %d", m, rec.Code)
		}
	}
}

// The service has no route that takes a lead id: nothing reads, changes
// or deletes a lead over HTTP, so another visitor's id leads nowhere.
func TestNoRouteByID(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	h.post(callLead())
	id := h.stored()[0].ID
	for _, p := range []string{"/api/lead/" + id, "/api/leads", "/api/leads/" + id, "/api/lead?id=" + id} {
		for _, m := range []string{http.MethodGet, http.MethodDelete} {
			req := httptest.NewRequest(m, "http://axiomantic.example"+p, nil)
			rec := httptest.NewRecorder()
			h.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s answered %d", m, p, rec.Code)
			}
			if strings.Contains(rec.Body.String(), "Марина") {
				t.Fatalf("%s %s returned a lead", m, p)
			}
		}
	}
	if n := len(h.stored()); n != 1 {
		t.Fatalf("store has %d leads after the probes, want 1", n)
	}
}

func TestOriginCheck(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	origin := func(o string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Origin", o) } }
	if rec := h.post(callLead(), origin("https://evil.example")); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign origin answered %d", rec.Code)
	}
	if rec := h.post(callLead(), origin("http://axiomantic.example")); rec.Code != http.StatusOK {
		t.Fatalf("same origin answered %d", rec.Code)
	}
	h.srv.origins = map[string]bool{"https://axiomantic.ru": true}
	if rec := h.post(callLead(), origin("http://axiomantic.example")); rec.Code != http.StatusForbidden {
		t.Fatalf("origin outside the list answered %d", rec.Code)
	}
	if rec := h.post(callLead(), origin("https://axiomantic.ru")); rec.Code != http.StatusOK {
		t.Fatalf("listed origin answered %d", rec.Code)
	}
}

func TestRateLimit(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	for i := 0; i < 5; i++ {
		if rec := h.post(callLead()); rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	rec := h.post(callLead())
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth attempt: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	other := func(r *http.Request) { r.RemoteAddr = "192.0.2.10:4000" }
	if rec := h.post(callLead(), other); rec.Code != http.StatusOK {
		t.Fatalf("another address was limited too: %d", rec.Code)
	}
}

func TestLimiterWindowAndCeiling(t *testing.T) {
	l := NewLimiter(time.Minute, 2, 3)
	t0 := time.Unix(1_700_000_000, 0)
	ok := func(ip string, at time.Time) bool { a, _ := l.Allow(ip, at); return a }
	if !ok("a", t0) || !ok("a", t0) || ok("a", t0) {
		t.Fatal("per-address limit")
	}
	if !ok("b", t0) || ok("c", t0) {
		t.Fatal("global ceiling")
	}
	if !ok("a", t0.Add(61*time.Second)) {
		t.Fatal("window did not slide")
	}
}

func TestProxyHeaderIsTrustedOnlyFromLoopback(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	real := func(peer string) func(*http.Request) {
		return func(r *http.Request) { r.RemoteAddr = peer; r.Header.Set("X-Real-IP", "192.0.2.55") }
	}
	h.post(callLead(), real("127.0.0.1:3000"))
	h.srv.trustProxy = true
	h.post(callLead(), real("203.0.113.9:3000"))
	h.post(callLead(), real("127.0.0.1:3000"))
	got := h.stored()
	want := []string{"127.0.0.1", "203.0.113.9", "192.0.2.55"}
	for i, r := range got {
		if r.IP != want[i] {
			t.Fatalf("lead %d stored IP %q, want %q", i, r.IP, want[i])
		}
	}
}

// A failing Telegram must neither lose the lead nor put the bot token in
// the log: Go's own HTTP errors quote the request URL, token included.
func TestTelegramFailureKeepsLeadAndToken(t *testing.T) {
	for _, mode := range []string{"refused", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t, http.StatusUnauthorized)
			if mode == "refused" {
				h.srv.notify.(*Telegram).base = "http://127.0.0.1:1"
			}
			rec := h.post(callLead())
			if rec.Code != http.StatusOK || len(h.stored()) != 1 {
				t.Fatalf("status %d, stored %d", rec.Code, len(h.stored()))
			}
			logs := h.logs.String()
			if !strings.Contains(logs, "notify") {
				t.Fatalf("failure not logged: %q", logs)
			}
			for _, leak := range []string{testToken, "test-token", "Марина", "123-45-67"} {
				if strings.Contains(logs, leak) || strings.Contains(rec.Body.String(), leak) {
					t.Fatalf("log or reply contains %q: %q", leak, logs)
				}
			}
		})
	}
}

func TestTelegramErrorsNeverQuoteTheToken(t *testing.T) {
	tg := NewTelegram(testToken, "1")
	tg.base = "http://[::1]:namedport" // makes request building fail
	if err := tg.Send(context.Background(), "x"); err == nil || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("bad URL error: %v", err)
	}
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// the token straddles the 200-character cut
		_, _ = w.Write([]byte(`{"description":"` + strings.Repeat("x", 180) + r.URL.Path + `"}`))
	}))
	defer echo.Close()
	tg.base = echo.URL
	err := tg.Send(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "0000000000:") || !strings.Contains(err.Error(), "<token>") {
		t.Fatalf("echoed error: %v", err)
	}
}

func TestStoreMustBeOutsideTheSite(t *testing.T) {
	site := t.TempDir()
	if err := os.MkdirAll(filepath.Join(site, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(site, "leads.jsonl"), filepath.Join(site, "assets", "x.jsonl")} {
		if err := checkOutside(p, site); err == nil {
			t.Fatalf("%s inside the site was accepted", p)
		}
	}
	if err := checkOutside(filepath.Join(t.TempDir(), "leads.jsonl"), site); err != nil {
		t.Fatalf("store outside the site refused: %v", err)
	}
}

func TestStaticServing(t *testing.T) {
	site := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(site, filepath.FromSlash(p))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "home")
	write("projects/index.html", "projects")
	write("404/index.html", "not here")
	write("assets/css/main.css", "body{}")
	h := newHarness(t, http.StatusOK)
	h.srv.static = site
	handler := h.srv.Handler()
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://axiomantic.example"+p, nil))
		return rec
	}
	if rec := get("/projects/"); rec.Code != http.StatusOK || rec.Body.String() != "projects" {
		t.Fatalf("/projects/: %d %q", rec.Code, rec.Body)
	}
	if rec := get("/assets/"); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "main.css") {
		t.Fatalf("folder listing: %d %q", rec.Code, rec.Body)
	}
	rec := get("/nope/")
	if rec.Code != http.StatusNotFound || rec.Body.String() != "not here" {
		t.Fatalf("missing page: %d %q", rec.Code, rec.Body)
	}
	if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
	// The mux redirects "/../" to its clean form; neither form may reach
	// a file outside the site folder.
	for _, p := range []string{"/../../etc/passwd", "/etc/passwd", "/..%2f..%2fetc/passwd"} {
		if rec := get(p); rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "root:") {
			t.Fatalf("%s answered %d", p, rec.Code)
		}
	}
}

func TestLeadTextIsPlainAndComplete(t *testing.T) {
	l := Lead{Mode: "write", Name: "Алексей", Channel: "email", Contact: "a@b.ru", Interest: "multi-custom", Message: "<b>hi</b> [x](http://evil)"}
	text := l.Text("20261008-abc")
	for _, want := range []string{"Написать", "Алексей", "Ответить в Email: a@b.ru", "Многостаночник · Под вас", "<b>hi</b>", "№ 20261008-abc"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text lacks %q:\n%s", want, text)
		}
	}
}
