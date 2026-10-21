package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func init() {
	// Real hashes take a good part of a second each; tests make dozens.
	hashIterations = 1000
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type sentMsg struct{ chat, text string }

type fakeSender struct {
	mu   sync.Mutex
	sent []sentMsg
}

func (f *fakeSender) Send(_ context.Context, chat, text string) error {
	f.mu.Lock()
	f.sent = append(f.sent, sentMsg{chat, text})
	f.mu.Unlock()
	return nil
}

type adminHarness struct {
	t       *testing.T
	srv     *Server
	h       http.Handler
	store   string
	cfgPath string
	clock   *fakeClock
	logs    *bytes.Buffer
	tg      *fakeSender
	ids     map[string]string // login → user id
}

var testUsers = []struct{ login, name, role, ws, pw string }{
	{"owner@alpha.example", "Анна Смирнова", roleOwner, "alpha", "alpha-owner-pass"},
	{"manager@alpha.example", "Олег Ким", roleManager, "alpha", "alpha-manager-pass"},
	{"viewer@alpha.example", "Илья Орлов", roleViewer, "alpha", "alpha-viewer-pass"},
	{"owner@beta.example", "Борис Бета", roleOwner, "beta", "beta-owner-pass"},
}

func testConfig(t *testing.T) (*AdminConfig, map[string]string) {
	t.Helper()
	c := &AdminConfig{DefaultWorkspace: "alpha", Workspaces: []*Workspace{
		{ID: "alpha", Name: "Альфа", Origins: []string{"https://alpha.example"}, SiteURL: "https://alpha.example"},
		{ID: "beta", Name: "Бета", Origins: []string{"https://beta.example"}},
	}}
	ids := map[string]string{}
	for _, u := range testUsers {
		hash, err := HashPassword(u.pw)
		if err != nil {
			t.Fatal(err)
		}
		id := newUserID()
		ids[u.login] = id
		c.Users = append(c.Users, &User{ID: id, Login: u.login, Name: u.name, Role: u.role, Workspace: u.ws, Hash: hash})
	}
	return c, ids
}

func newAdminHarness(t *testing.T) *adminHarness {
	t.Helper()
	dir := t.TempDir()
	h := &adminHarness{t: t, store: filepath.Join(dir, "leads.jsonl"), cfgPath: filepath.Join(dir, "admin.json"),
		clock: &fakeClock{t: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}, logs: &bytes.Buffer{}, tg: &fakeSender{}}
	cfg, ids := testConfig(t)
	h.ids = ids
	if err := SaveAdminConfig(h.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	h.start()
	return h
}

// start builds the server from the files on disk, as a restart would.
func (h *adminHarness) start() {
	h.t.Helper()
	logger := log.New(h.logs, "", 0)
	fs, err := OpenFileStore(h.store)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { fs.Close() })
	src, err := NewConfigSource(h.cfgPath, h.clock.Now, logger.Printf)
	if err != nil {
		h.t.Fatal(err)
	}
	idx := NewIndex("alpha")
	if _, err := fs.Load(idx.AddRecord, idx.AddEvent); err != nil {
		h.t.Fatal(err)
	}
	h.srv = &Server{
		store: fs, notify: h.tg, limit: NewLimiter(10*time.Minute, 1000, 1000), origins: map[string]bool{},
		log: logger, now: h.clock.Now, defaultWS: "alpha",
		admin: NewAdmin(src, idx, fs, h.clock.Now, logger),
	}
	h.h = h.srv.Handler()
}

func (h *adminHarness) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	h.srv.Wait()
	return rec
}

// postLead sends a site form from origin and returns the new lead's id.
func (h *adminHarness) postLead(origin string, lead map[string]any) string {
	h.t.Helper()
	raw, _ := json.Marshal(lead)
	req := httptest.NewRequest(http.MethodPost, "https://api.example/api/lead", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.RemoteAddr = "203.0.113.7:5000"
	rec := h.serve(req)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("lead from %s: %d %s", origin, rec.Code, rec.Body)
	}
	recs := h.records()
	return recs[len(recs)-1].ID
}

func (h *adminHarness) records() []Record {
	var out []Record
	_, err := h.srv.store.Load(func(r Record) { out = append(out, r) }, func(Event) {})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *adminHarness) storeBytes() string {
	b, err := os.ReadFile(h.store)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

type client struct {
	h      *adminHarness
	cookie string
	csrf   string
	ip     string
}

func (h *adminHarness) loginReq(login, pw, ip string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"login": login, "password": pw})
	req := httptest.NewRequest(http.MethodPost, "https://admin.example/api/admin/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://admin.example")
	req.RemoteAddr = ip + ":4000"
	for _, m := range mutate {
		m(req)
	}
	return h.serve(req)
}

func (h *adminHarness) login(login string) *client {
	h.t.Helper()
	pw := ""
	for _, u := range testUsers {
		if u.login == login {
			pw = u.pw
		}
	}
	rec := h.loginReq(login, pw, "198.51.100.1")
	if rec.Code != http.StatusOK {
		h.t.Fatalf("login %s: %d %s", login, rec.Code, rec.Body)
	}
	c := &client{h: h, ip: "198.51.100.1"}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == cookieName {
			c.cookie = ck.Value
		}
	}
	var s struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(c.do(http.MethodGet, "/api/admin/session", nil).Body.Bytes(), &s); err != nil || s.CSRF == "" {
		h.t.Fatalf("session after login: %v", err)
	}
	c.csrf = s.CSRF
	return c
}

func (c *client) do(method, path string, body any, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "https://admin.example"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", "https://admin.example")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	req.RemoteAddr = c.ip + ":4000"
	for _, m := range mutate {
		m(req)
	}
	return c.h.serve(req)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body, err)
	}
	return v
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[apiError](t, rec).Error
}

type listResp struct {
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
	Items  []leadSummary  `json:"items"`
}

type detailResp struct {
	Lead    leadDetail  `json:"lead"`
	History []eventView `json:"history"`
}

// ------------------------------------------------------------- sign-in

func TestAdminLoginSetsHardenedCookie(t *testing.T) {
	h := newAdminHarness(t)
	rec := h.loginReq("owner@alpha.example", "alpha-owner-pass", "198.51.100.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var ck *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			ck = c
		}
	}
	if ck == nil || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/" || ck.Domain != "" {
		t.Fatalf("cookie %+v", ck)
	}
	if len(ck.Value) < 40 {
		t.Fatalf("session token too short: %d", len(ck.Value))
	}
	// Logins are case-insensitive.
	if rec := h.loginReq("  OWNER@Alpha.example ", "alpha-owner-pass", "198.51.100.2"); rec.Code != http.StatusOK {
		t.Fatalf("login in other case: %d", rec.Code)
	}
}

func TestAdminLoginRejectsBadCredentialsUniformly(t *testing.T) {
	h := newAdminHarness(t)
	cfg, _ := LoadAdminConfig(h.cfgPath)
	cfg.UserByLogin("viewer@alpha.example").Disabled = true
	if err := SaveAdminConfig(h.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	h.clock.Add(2 * time.Second)
	bodies := map[string]bool{}
	for i, c := range []struct{ login, pw string }{
		{"owner@alpha.example", "wrong-password"},
		{"nobody@alpha.example", "alpha-owner-pass"},
		{"viewer@alpha.example", "alpha-viewer-pass"}, // disabled
	} {
		rec := h.loginReq(c.login, c.pw, fmt.Sprintf("198.51.100.%d", 10+i))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", c.login, rec.Code)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Fatalf("%s: a cookie was set", c.login)
		}
		bodies[rec.Body.String()] = true
	}
	if len(bodies) != 1 {
		t.Fatalf("answers differ, which tells logins apart: %v", bodies)
	}
	logs := h.logs.String()
	for _, leak := range []string{"wrong-password", "alpha-owner-pass", "nobody@"} {
		if strings.Contains(logs, leak) {
			t.Fatalf("log contains %q: %s", leak, logs)
		}
	}
}

func TestAdminLoginRateLimits(t *testing.T) {
	h := newAdminHarness(t)
	// Five failures for one login from one address stop that pair…
	for i := 0; i < 5; i++ {
		h.loginReq("owner@alpha.example", "nope-nope-nope", "198.51.100.20")
	}
	rec := h.loginReq("owner@alpha.example", "alpha-owner-pass", "198.51.100.20")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("6th try for one login from one address: %d", rec.Code)
	}
	// …but the person can still sign in from their own address,
	if rec := h.loginReq("owner@alpha.example", "alpha-owner-pass", "198.51.100.21"); rec.Code != http.StatusOK {
		t.Fatalf("same login from another address: %d", rec.Code)
	}
	// and the address can still be used for other logins until it has
	// made ten attempts in all (six so far, the blocked one included).
	for i := 0; i < 4; i++ {
		h.loginReq("nobody@alpha.example", "x-x-x-x-x-x", "198.51.100.20")
	}
	if rec := h.loginReq("manager@alpha.example", "alpha-manager-pass", "198.51.100.20"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th attempt from one address: %d", rec.Code)
	}
	h.clock.Add(16 * time.Minute)
	if rec := h.loginReq("owner@alpha.example", "alpha-owner-pass", "198.51.100.20"); rec.Code != http.StatusOK {
		t.Fatalf("after the window: %d", rec.Code)
	}
}

// Nobody can lock the panel by flooding it: junk sign-ins from many
// addresses, or failures aimed at one person's login, leave a correct
// sign-in from elsewhere working.
func TestAdminLoginFloodCannotLockOthersOut(t *testing.T) {
	h := newAdminHarness(t)
	for ip := 0; ip < 20; ip++ {
		for i := 0; i < 10; i++ {
			h.loginReq(fmt.Sprintf("junk%d@x.example", i), "junk-junk-junk", fmt.Sprintf("192.0.2.%d", ip))
		}
		// a determined guesser spreads failures for the owner's login too
		h.loginReq("owner@alpha.example", "guess-guess", fmt.Sprintf("203.0.113.%d", ip))
	}
	if rec := h.loginReq("owner@alpha.example", "alpha-owner-pass", "198.51.100.99"); rec.Code != http.StatusOK {
		t.Fatalf("owner locked out by a flood: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminLoginNeedsSameOriginJSON(t *testing.T) {
	h := newAdminHarness(t)
	cases := map[string]func(*http.Request){
		"foreign origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"cross-site":     func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"form encoding":  func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") },
	}
	for name, m := range cases {
		rec := h.loginReq("owner@alpha.example", "alpha-owner-pass", "198.51.100.30", m)
		if rec.Code == http.StatusOK || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("%s: signed in (%d)", name, rec.Code)
		}
	}
}

// ------------------------------------------------------------- sessions

func TestAdminRoutesNeedASession(t *testing.T) {
	h := newAdminHarness(t)
	id := h.postLead("https://alpha.example", callLead())
	anon := &client{h: h, ip: "198.51.100.40"}
	bogus := &client{h: h, ip: "198.51.100.40", cookie: "forged-token-value", csrf: "x"}
	for _, c := range []*client{anon, bogus} {
		for _, r := range []struct{ m, p string }{
			{"GET", "/api/admin/session"}, {"GET", "/api/admin/leads"}, {"GET", "/api/admin/leads/" + id},
			{"PATCH", "/api/admin/leads/" + id}, {"DELETE", "/api/admin/leads/" + id}, {"GET", "/api/admin/stats"},
			{"GET", "/api/admin/activity"}, {"GET", "/api/admin/team"}, {"GET", "/api/admin/settings"},
			{"GET", "/api/admin/export.csv"}, {"POST", "/api/admin/logout"},
		} {
			var body any
			if r.m == "PATCH" {
				body = map[string]string{"status": "spam"}
			}
			rec := c.do(r.m, r.p, body)
			if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "Марина") {
				t.Fatalf("%s %s with cookie %q: %d %s", r.m, r.p, c.cookie, rec.Code, rec.Body)
			}
		}
	}
	if v, _ := h.srv.admin.index.Get("alpha", id); v.Status != "new" || v.Rec.Erased {
		t.Fatal("an anonymous request changed the lead")
	}
}

func TestAdminSessionPayloadHoldsNoSecrets(t *testing.T) {
	h := newAdminHarness(t)
	h.postLead("https://alpha.example", callLead())
	c := h.login("owner@alpha.example")
	rec := c.do("GET", "/api/admin/session", nil)
	body := rec.Body.String()
	for _, leak := range []string{"pbkdf2", "hash", "203.0.113.7", "beta"} {
		if strings.Contains(body, leak) {
			t.Fatalf("session payload contains %q: %s", leak, body)
		}
	}
	s := decode[struct {
		User      map[string]any `json:"user"`
		Workspace wsView         `json:"workspace"`
		Counts    map[string]int `json:"counts"`
	}](t, rec)
	if s.Workspace.ID != "alpha" || s.User["role"] != roleOwner || s.User["initials"] != "АС" || s.Counts["new"] != 1 {
		t.Fatalf("session %+v", s)
	}
	for _, p := range []string{"/api/admin/leads", "/api/admin/activity"} {
		if b := c.do("GET", p, nil).Body.String(); strings.Contains(b, "203.0.113.7") || strings.Contains(b, `"ip"`) {
			t.Fatalf("%s exposes the visitor's address: %s", p, b)
		}
	}
}

func TestAdminSessionsEnd(t *testing.T) {
	h := newAdminHarness(t)
	alive := func(c *client) bool { return c.do("GET", "/api/admin/session", nil).Code == http.StatusOK }

	c := h.login("owner@alpha.example")
	if rec := c.do("POST", "/api/admin/logout", map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("logout: %d", rec.Code)
	}
	if alive(c) {
		t.Fatal("cookie still works after logout")
	}

	// a new password ends sessions made with the old one
	c = h.login("owner@alpha.example")
	other := h.login("manager@alpha.example")
	var out bytes.Buffer
	if code := adminCLI([]string{"user", "passwd", "-config", h.cfgPath, "-login", "owner@alpha.example", "-password-stdin"},
		strings.NewReader("a-brand-new-password\n"), &out, &out); code != 0 {
		t.Fatalf("passwd: %s", out.String())
	}
	h.clock.Add(2 * time.Second)
	if alive(c) {
		t.Fatal("session survived a password change")
	}
	if !alive(other) {
		t.Fatal("another user's session ended too")
	}

	// disabling and removing
	for _, cmd := range []string{"disable", "remove"} {
		login := "manager@alpha.example"
		if cmd == "remove" {
			login = "viewer@alpha.example"
		}
		var c *client
		if cmd == "disable" {
			c = other
		} else {
			c = h.login(login)
		}
		if code := adminCLI([]string{"user", cmd, "-config", h.cfgPath, "-login", login}, nil, &out, &out); code != 0 {
			t.Fatalf("%s: %s", cmd, out.String())
		}
		h.clock.Add(2 * time.Second)
		if alive(c) {
			t.Fatalf("session survived «user %s»", cmd)
		}
	}

	// idle and absolute limits
	c = h.login("owner@beta.example")
	h.clock.Add(sessionIdle + time.Minute)
	if alive(c) {
		t.Fatal("session survived the idle timeout")
	}
	c = h.login("owner@beta.example")
	for i := 0; i < 16; i++ { // used every 11 hours, so never idle
		h.clock.Add(11 * time.Hour)
		alive(c)
	}
	if alive(c) {
		t.Fatal("session outlived its absolute limit")
	}
}

func TestAdminSessionsAreKeptHashed(t *testing.T) {
	h := newAdminHarness(t)
	c := h.login("owner@alpha.example")
	s := h.srv.admin.sessions
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if bytes.Contains(k[:], []byte(c.cookie)) || v.CSRF == c.cookie {
			t.Fatal("the cookie value is stored as is")
		}
	}
}

// ------------------------------------------------------------- owner checks

func TestAdminWorkspacesSeeOnlyTheirLeads(t *testing.T) {
	h := newAdminHarness(t)
	h.postLead("https://alpha.example", callLead())                           // Марина
	h.postLead("https://beta.example", with(writeLead(), "name", "Бетослав")) // beta's
	alpha := h.login("owner@alpha.example")
	beta := h.login("owner@beta.example")

	la := decode[listResp](t, alpha.do("GET", "/api/admin/leads", nil))
	if la.Total != 1 || la.Items[0].Name != "Марина" || la.Counts["all"] != 1 {
		t.Fatalf("alpha list %+v", la)
	}
	lb := decode[listResp](t, beta.do("GET", "/api/admin/leads", nil))
	if lb.Total != 1 || lb.Items[0].Name != "Бетослав" {
		t.Fatalf("beta list %+v", lb)
	}
	if l := decode[listResp](t, alpha.do("GET", "/api/admin/leads?q=%D0%B1%D0%B5%D1%82%D0%BE", nil)); l.Total != 0 {
		t.Fatal("search reached another workspace")
	}
	for _, p := range []string{"/api/admin/activity", "/api/admin/stats?days=7", "/api/admin/export.csv", "/api/admin/team", "/api/admin/settings"} {
		body := alpha.do("GET", p, nil).Body.String()
		for _, leak := range []string{"Бетослав", "Борис", "beta.example"} {
			if strings.Contains(body, leak) {
				t.Fatalf("%s shows %q to alpha: %s", p, leak, body)
			}
		}
	}
}

// A lead id from another workspace must behave exactly like an id that
// does not exist, for reading, changing and erasing.
func TestAdminForeignIDIsNotFound(t *testing.T) {
	h := newAdminHarness(t)
	betaID := h.postLead("https://beta.example", with(writeLead(), "name", "Бетослав"))
	alphaID := h.postLead("https://alpha.example", callLead())
	alpha := h.login("owner@alpha.example")
	missing := "20261017-00000000ff"

	for _, id := range []string{betaID, missing} {
		for _, r := range []struct {
			m    string
			body any
		}{
			{"GET", nil}, {"PATCH", map[string]string{"status": "spam"}}, {"PATCH", map[string]string{"note": "чужая"}},
			{"PATCH", map[string]string{"assignee": h.ids["owner@alpha.example"]}}, {"DELETE", nil},
		} {
			rec := alpha.do(r.m, "/api/admin/leads/"+id, r.body)
			if rec.Code != http.StatusNotFound || errCode(t, rec) != "not_found" || strings.Contains(rec.Body.String(), "Бетослав") {
				t.Fatalf("%s %s: %d %s", r.m, id, rec.Code, rec.Body)
			}
		}
	}
	v, ok := h.srv.admin.index.Get("beta", betaID)
	if !ok || v.Status != "new" || v.Assignee != "" || v.Notes != 0 || v.Rec.Erased || v.Rec.Lead.Name != "Бетослав" {
		t.Fatalf("beta's lead changed: %+v", v)
	}
	if !strings.Contains(h.storeBytes(), "Бетослав") {
		t.Fatal("beta's lead was erased from the store")
	}
	// Ids that are not ids, or try to climb out of the route.
	for _, p := range []string{"..%2F..%2Fetc%2Fpasswd", "20261017-ZZ", alphaID + "x", "%00"} {
		if rec := alpha.do("GET", "/api/admin/leads/"+p, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: %d", p, rec.Code)
		}
	}
	// The beta owner still has it.
	if rec := h.login("owner@beta.example").do("GET", "/api/admin/leads/"+betaID, nil); rec.Code != http.StatusOK {
		t.Fatalf("beta owner: %d", rec.Code)
	}
}

func TestAdminAssigneeMustBeOwnActiveMember(t *testing.T) {
	h := newAdminHarness(t)
	id := h.postLead("https://alpha.example", callLead())
	c := h.login("owner@alpha.example")
	cfg, _ := LoadAdminConfig(h.cfgPath)
	cfg.UserByLogin("viewer@alpha.example").Disabled = true
	_ = SaveAdminConfig(h.cfgPath, cfg)
	h.clock.Add(2 * time.Second)
	for _, who := range []string{h.ids["owner@beta.example"], h.ids["viewer@alpha.example"], "u_000000000000", "nobody"} {
		rec := c.do("PATCH", "/api/admin/leads/"+id, map[string]string{"assignee": who})
		if rec.Code != http.StatusBadRequest || decode[apiError](t, rec).Field != "assignee" {
			t.Fatalf("assignee %s: %d %s", who, rec.Code, rec.Body)
		}
	}
	if v, _ := h.srv.admin.index.Get("alpha", id); v.Assignee != "" || len(v.Events) != 0 {
		t.Fatalf("lead changed: %+v", v)
	}
	ok := c.do("PATCH", "/api/admin/leads/"+id, map[string]string{"assignee": h.ids["manager@alpha.example"]})
	if ok.Code != http.StatusOK || decode[detailResp](t, ok).Lead.Assignee.Name != "Олег Ким" {
		t.Fatalf("own manager: %d %s", ok.Code, ok.Body)
	}
}

func TestAdminWritesNeedCSRF(t *testing.T) {
	h := newAdminHarness(t)
	id := h.postLead("https://alpha.example", callLead())
	c := h.login("owner@alpha.example")
	other := h.login("manager@alpha.example")
	cases := map[string]func(*http.Request){
		"no token":        func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"wrong token":     func(r *http.Request) { r.Header.Set("X-CSRF-Token", "guess") },
		"another's token": func(r *http.Request) { r.Header.Set("X-CSRF-Token", other.csrf) },
		"foreign origin":  func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"cross-site":      func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
	}
	for name, m := range cases {
		for _, r := range []struct {
			method string
			body   any
		}{{"PATCH", map[string]string{"status": "spam"}}, {"DELETE", nil}} {
			rec := c.do(r.method, "/api/admin/leads/"+id, r.body, m)
			if rec.Code != http.StatusForbidden || errCode(t, rec) != "csrf" {
				t.Fatalf("%s %s: %d %s", name, r.method, rec.Code, rec.Body)
			}
		}
	}
	if v, _ := h.srv.admin.index.Get("alpha", id); v.Status != "new" || v.Rec.Erased {
		t.Fatal("a forged request changed the lead")
	}
}

func TestAdminRoles(t *testing.T) {
	h := newAdminHarness(t)
	id := h.postLead("https://alpha.example", callLead())
	viewer := h.login("viewer@alpha.example")
	manager := h.login("manager@alpha.example")
	owner := h.login("owner@alpha.example")
	expect := func(c *client, m, p string, body any, code int) {
		t.Helper()
		if rec := c.do(m, p, body); rec.Code != code {
			t.Fatalf("%s %s: %d, want %d (%s)", m, p, rec.Code, code, rec.Body)
		}
	}
	expect(viewer, "GET", "/api/admin/leads/"+id, nil, http.StatusOK)
	expect(viewer, "PATCH", "/api/admin/leads/"+id, map[string]string{"status": "spam"}, http.StatusForbidden)
	expect(viewer, "GET", "/api/admin/export.csv", nil, http.StatusForbidden)
	expect(viewer, "DELETE", "/api/admin/leads/"+id, nil, http.StatusForbidden)
	expect(manager, "DELETE", "/api/admin/leads/"+id, nil, http.StatusForbidden)
	expect(manager, "PATCH", "/api/admin/leads/"+id, map[string]string{"status": "contacted"}, http.StatusOK)
	expect(manager, "GET", "/api/admin/export.csv", nil, http.StatusOK)
	expect(owner, "DELETE", "/api/admin/leads/"+id, nil, http.StatusOK)
}

// ------------------------------------------------------------- changes

func TestAdminPatchRecordsHistory(t *testing.T) {
	h := newAdminHarness(t)
	id := h.postLead("https://alpha.example", callLead())
	c := h.login("manager@alpha.example")
	h.clock.Add(10 * time.Minute)
	rec := c.do("PATCH", "/api/admin/leads/"+id, map[string]any{
		"status": "contacted", "assignee": h.ids["manager@alpha.example"], "note": "  Перезвонить в пятницу  ",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	d := decode[detailResp](t, rec)
	if d.Lead.Status != "contacted" || d.Lead.Notes != 1 || d.Lead.Assignee == nil {
		t.Fatalf("lead %+v", d.Lead)
	}
	kinds := []string{}
	for _, e := range d.History {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "note,assign,status,created" {
		t.Fatalf("history order %v", kinds)
	}
	if d.History[0].Text != "Перезвонить в пятницу" || d.History[2].From != "new" || d.History[2].To != "contacted" ||
		d.History[2].By.Name != "Олег Ким" || d.History[3].By != nil {
		t.Fatalf("history %+v", d.History)
	}
	// the same status again changes nothing
	again := decode[detailResp](t, c.do("PATCH", "/api/admin/leads/"+id, map[string]string{"status": "contacted"}))
	if len(again.History) != 4 {
		t.Fatalf("repeated status added an event: %d", len(again.History))
	}
	for name, body := range map[string]string{
		"empty":         `{}`,
		"bad status":    `{"status":"done"}`,
		"long note":     `{"note":"` + strings.Repeat("я", maxNote+1) + `"}`,
		"blank note":    `{"note":"   "}`,
		"control char":  `{"note":"a\u0007b"}`,
		"unknown field": `{"status":"won","is_admin":true}`,
		"two objects":   `{"status":"won"}{"status":"won"}`,
	} {
		if rec := c.do("PATCH", "/api/admin/leads/"+id, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if v, _ := h.srv.admin.index.Get("alpha", id); v.Status != "contacted" {
		t.Fatalf("a rejected patch changed the status to %s", v.Status)
	}
	// It all survives a restart.
	h.start()
	v, ok := h.srv.admin.index.Get("alpha", id)
	if !ok || v.Status != "contacted" || v.Assignee != h.ids["manager@alpha.example"] || v.Notes != 1 {
		t.Fatalf("after restart %+v", v)
	}
}

func TestAdminEraseWipesTheStoreFile(t *testing.T) {
	h := newAdminHarness(t)
	keep := h.postLead("https://alpha.example", with(callLead(), "name", "Сохранённая"))
	id := h.postLead("https://alpha.example", map[string]any{
		"source": "modal", "mode": "write", "name": "Стираемая Персона", "channel": "email",
		"contact": "erase.me@example.ru", "message": "Мой адрес: Тайная улица, 7", "interest": "landing", "consent": true,
	})
	owner := h.login("owner@alpha.example")
	owner.do("PATCH", "/api/admin/leads/"+id, map[string]string{"status": "contacted"})
	owner.do("PATCH", "/api/admin/leads/"+id, map[string]string{"note": "Телефон жены 8-900-555-12-12"})
	if rec := owner.do("DELETE", "/api/admin/leads/"+id, nil); rec.Code != http.StatusOK {
		t.Fatalf("erase: %d %s", rec.Code, rec.Body)
	}
	raw := h.storeBytes()
	for _, gone := range []string{"Стираемая", "erase.me", "Тайная", "555-12-12"} {
		if strings.Contains(raw, gone) {
			t.Fatalf("store still holds %q", gone)
		}
	}
	if !strings.Contains(raw, "Сохранённая") {
		t.Fatal("another lead was lost")
	}
	if st, _ := os.Stat(h.store); st.Mode().Perm() != 0o600 {
		t.Fatalf("store mode after rewrite: %v", st.Mode().Perm())
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(h.store), ".*erase*"))
	if len(matches) != 0 {
		t.Fatalf("temporary files left: %v", matches)
	}
	d := decode[detailResp](t, owner.do("GET", "/api/admin/leads/"+id, nil))
	if !d.Lead.Erased || d.Lead.Name != "" || d.Lead.Contact != "" || d.Lead.Message != "" || d.Lead.Status != "contacted" {
		t.Fatalf("erased lead %+v", d.Lead)
	}
	if d.History[0].Kind != "erase" || d.History[1].Text != "" {
		t.Fatalf("history %+v", d.History)
	}
	if rec := owner.do("DELETE", "/api/admin/leads/"+id, nil); rec.Code != http.StatusOK {
		t.Fatalf("second erase: %d", rec.Code)
	}
	// Writing goes on into the new file, and a restart sees the erasure.
	later := h.postLead("https://alpha.example", with(callLead(), "name", "Поздняя"))
	h.start()
	if v, ok := h.srv.admin.index.Get("alpha", id); !ok || !v.Rec.Erased || v.Rec.Lead.Name != "" {
		t.Fatalf("after restart %+v", v)
	}
	for _, x := range []string{keep, later} {
		if _, ok := h.srv.admin.index.Get("alpha", x); !ok {
			t.Fatalf("lead %s lost", x)
		}
	}
}

func TestAdminStats(t *testing.T) {
	h := newAdminHarness(t)
	a := h.postLead("https://alpha.example", callLead())
	h.clock.Add(-40 * 24 * time.Hour) // one in the previous 30-day period
	h.postLead("https://alpha.example", callLead())
	h.clock.Add(40 * 24 * time.Hour)
	b := h.postLead("https://alpha.example", formLead())
	h.postLead("https://beta.example", callLead())
	c := h.login("owner@alpha.example")
	h.clock.Add(30 * time.Minute)
	c.do("PATCH", "/api/admin/leads/"+a, map[string]string{"status": "won"})
	h.clock.Add(2 * time.Hour)

	type stats struct {
		Series []struct{ Leads, Won int } `json:"series"`
		Totals map[string]any             `json:"totals"`
		Attn   []struct {
			Kind  string `json:"kind"`
			Count int    `json:"count"`
		} `json:"attention"`
	}
	s := decode[stats](t, c.do("GET", "/api/admin/stats?days=30", nil))
	if len(s.Series) != 30 || s.Series[29].Leads != 2 || s.Series[29].Won != 1 {
		t.Fatalf("series end %+v", s.Series[29])
	}
	if s.Totals["leads"] != 2.0 || s.Totals["prevLeads"] != 1.0 || s.Totals["won"] != 1.0 || s.Totals["conversion"] != 0.5 ||
		s.Totals["responseMedianMin"] != 30.0 || s.Totals["newWaiting"] != 2.0 {
		t.Fatalf("totals %+v", s.Totals)
	}
	if s.Attn[0].Kind != "waiting" || s.Attn[0].Count != 2 {
		t.Fatalf("attention %+v", s.Attn)
	}
	l := decode[listResp](t, c.do("GET", "/api/admin/leads?status=new&waiting=1", nil))
	if l.Total != 2 || (l.Items[0].ID != b && l.Items[1].ID != b) {
		t.Fatalf("waiting list %+v", l)
	}
	if rec := c.do("GET", "/api/admin/stats?days=365", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("days=365: %d", rec.Code)
	}
}

func TestAdminListFiltersAndPaging(t *testing.T) {
	h := newAdminHarness(t)
	for i := 0; i < 25; i++ {
		h.clock.Add(time.Minute)
		h.postLead("https://alpha.example", with(callLead(), "phone", fmt.Sprintf("+7 (900) 000-00-%02d", i)))
	}
	h.postLead("https://alpha.example", formLead())
	c := h.login("owner@alpha.example")
	l := decode[listResp](t, c.do("GET", "/api/admin/leads?limit=10&offset=20", nil))
	if l.Total != 26 || len(l.Items) != 6 {
		t.Fatalf("paging: total %d items %d", l.Total, len(l.Items))
	}
	if l := decode[listResp](t, c.do("GET", "/api/admin/leads?q=90000000%2007", nil)); l.Total != 1 {
		t.Fatalf("search by digits: %d", l.Total)
	}
	if l := decode[listResp](t, c.do("GET", "/api/admin/leads?mode=form", nil)); l.Total != 1 || l.Items[0].ContactKind != "email" {
		t.Fatalf("mode filter %+v", l)
	}
	first := decode[listResp](t, c.do("GET", "/api/admin/leads?limit=1", nil)).Items[0]
	oldest := decode[listResp](t, c.do("GET", "/api/admin/leads?limit=1&sort=old", nil)).Items[0]
	if !first.Time.After(oldest.Time) {
		t.Fatal("sort order")
	}
	// «В работе» is contacted + proposal and never includes «Новая»
	c.do("PATCH", "/api/admin/leads/"+first.ID, map[string]string{"status": "contacted"})
	if l := decode[listResp](t, c.do("GET", "/api/admin/leads?status=progress", nil)); l.Total != 1 || l.Counts["progress"] != 1 ||
		l.Counts["new"] != 25 || l.Items[0].ID != first.ID {
		t.Fatalf("progress tab %+v", l.Counts)
	}
	for _, q := range []string{"status=done", "limit=0", "limit=101", "offset=-1", "sort=name", "mode=fax", "budget=lots"} {
		if rec := c.do("GET", "/api/admin/leads?"+q, nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", q, rec.Code)
		}
	}
}

func TestAdminExportCSV(t *testing.T) {
	h := newAdminHarness(t)
	h.postLead("https://alpha.example", with(callLead(), "name", "=HYPERLINK(\"http://evil\")"))
	h.postLead("https://alpha.example", with(callLead(), "name", "x;=1+1, y"))
	h.postLead("https://beta.example", with(callLead(), "name", "Бетослав"))
	rec := h.login("owner@alpha.example").do("GET", "/api/admin/export.csv", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(body, "\xEF\xBB\xBF") ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("export: %d %q", rec.Code, rec.Header())
	}
	if !strings.Contains(body, `"'=HYPERLINK(""http://evil"")"`) {
		t.Fatalf("formula not neutralised: %s", body)
	}
	if !strings.Contains(body, `"x;=1+1, y"`) {
		t.Fatalf("a field with a separator inside is not one quoted cell: %s", body)
	}
	for _, line := range strings.Split(strings.TrimSpace(body), "\r\n") {
		if !strings.HasPrefix(strings.TrimPrefix(line, "\xEF\xBB\xBF"), `"`) || !strings.HasSuffix(line, `"`) {
			t.Fatalf("unquoted field in %q", line)
		}
	}
	if strings.Contains(body, "Бетослав") {
		t.Fatal("export includes another workspace")
	}
}

// ------------------------------------------------------------- intake

func TestLeadGoesToTheWorkspaceOfItsSite(t *testing.T) {
	h := newAdminHarness(t)
	a := h.postLead("https://alpha.example", callLead())
	b := h.postLead("https://beta.example", callLead())
	n := h.postLead("", callLead()) // not a browser: the default workspace
	if _, ok := h.srv.admin.index.Get("alpha", a); !ok {
		t.Fatal("alpha's lead not in alpha")
	}
	if _, ok := h.srv.admin.index.Get("beta", b); !ok {
		t.Fatal("beta's lead not in beta")
	}
	if _, ok := h.srv.admin.index.Get("alpha", n); !ok {
		t.Fatal("origin-less lead not in the default workspace")
	}
	post := func(origin, host string) int {
		raw, _ := json.Marshal(callLead())
		req := httptest.NewRequest(http.MethodPost, "https://"+host+"/api/lead", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		return h.serve(req).Code
	}
	// Unlisted origins are refused — including a variant of a client's
	// own site, which must not fall back to the studio's workspace.
	for _, o := range []string{"https://evil.example", "https://www.beta.example", "http://beta.example", "https://beta.example:8443"} {
		host := strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
		if code := post(o, host); code != http.StatusForbidden {
			t.Fatalf("unlisted origin %s (same host): %d", o, code)
		}
	}
	// An origin given with -origins means the default workspace.
	h.srv.origins = map[string]bool{"https://old-studio.example": true}
	before := len(h.srv.admin.index.All("alpha"))
	if code := post("https://old-studio.example", "old-studio.example"); code != http.StatusOK || len(h.srv.admin.index.All("alpha")) != before+1 {
		t.Fatalf("-origins entry: %d", code)
	}
	// Notifications: alpha uses the default chat with a panel link; beta
	// has no chat of its own, so its visitors' data stays out of the
	// studio's chat.
	h.tg.mu.Lock()
	sent := append([]sentMsg(nil), h.tg.sent...)
	h.tg.mu.Unlock()
	// a, n and the -origins lead went to alpha; beta's lead to nobody
	if len(sent) != 3 || sent[0].chat != "" || sent[1].chat != "" || !strings.Contains(sent[1].text, n) {
		t.Fatalf("sent %+v", sent)
	}
	if !strings.Contains(sent[0].text, "https://alpha.example/admin/#/leads/"+a) {
		t.Fatalf("no panel link: %s", sent[0].text)
	}
	cfg, _ := LoadAdminConfig(h.cfgPath)
	cfg.Workspace("beta").TelegramChat = "-100777"
	_ = SaveAdminConfig(h.cfgPath, cfg)
	h.clock.Add(2 * time.Second)
	h.postLead("https://beta.example", callLead())
	h.tg.mu.Lock()
	last := h.tg.sent[len(h.tg.sent)-1]
	h.tg.mu.Unlock()
	if last.chat != "-100777" {
		t.Fatalf("beta's own chat not used: %+v", last)
	}
}

func TestAdminIsOffWithoutConfig(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	for _, p := range []string{"/api/admin/session", "/admin/", "/api/admin/login"} {
		rec := httptest.NewRecorder()
		h.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://axiomantic.example"+p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d", p, rec.Code)
		}
	}
}

// ------------------------------------------------------------- pages

func TestAdminPagesHeaders(t *testing.T) {
	h := newAdminHarness(t)
	get := func(p string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "https://admin.example"+p, nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		return h.serve(req)
	}
	rec := get("/admin/")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("/admin/: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	hd := rec.Header()
	if hd.Get("Content-Security-Policy") != adminCSP || hd.Get("X-Frame-Options") != "DENY" ||
		!strings.Contains(hd.Get("X-Robots-Tag"), "noindex") || hd.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers %v", hd)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "style=") || strings.Contains(body, "<style") {
		t.Fatal("inline script or style in the panel page")
	}
	if rec := get("/admin/", "If-None-Match", hd.Get("ETag")); rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", rec.Code)
	}
	for _, p := range []string{"/admin/../admin.go", "/admin/admin.go", "/admin/assets/", "/admin/nope.html", "/admin/%2e%2e/main.go"} {
		if rec := get(p); rec.Code == http.StatusOK {
			t.Fatalf("%s answered 200", p)
		}
	}
	if rec := get("/admin"); rec.Code != http.StatusMovedPermanently {
		t.Fatalf("/admin: %d", rec.Code)
	}
}

// ------------------------------------------------------------- config and CLI

func TestAdminConfigRefusesLooseModeAndBadValues(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := testConfig(t)
	p := filepath.Join(dir, "admin.json")
	if err := SaveAdminConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("saved with mode %v", st.Mode().Perm())
	}
	_ = os.Chmod(p, 0o644)
	if _, err := LoadAdminConfig(p); err == nil {
		t.Fatal("a world-readable config was accepted")
	}
	bad := []func(c *AdminConfig){
		func(c *AdminConfig) { c.DefaultWorkspace = "" },
		func(c *AdminConfig) { c.DefaultWorkspace = "gamma" },
		func(c *AdminConfig) { c.Workspaces[1].Origins = []string{"https://клиент.рф"} },
		func(c *AdminConfig) { c.Workspaces[0].Name = "Альфа\u202eатеБ" },
		func(c *AdminConfig) { c.Users[0].Name = "Анна\u200f" },
		func(c *AdminConfig) { c.Workspaces[0].Brand.Logo = "<b" },
		func(c *AdminConfig) { c.Workspaces[1].Origins = []string{"https://alpha.example"} },
		func(c *AdminConfig) { c.Workspaces[0].Brand.Accent = "red; background:url(x)" },
		func(c *AdminConfig) { c.Users[0].Role = "root" },
		func(c *AdminConfig) { c.Users[0].Workspace = "gamma" },
		func(c *AdminConfig) { c.Users[1].Login = strings.ToUpper(c.Users[0].Login) },
		func(c *AdminConfig) { c.Users[0].Hash = "plaintext" },
		func(c *AdminConfig) { c.Workspaces[0].ID = "Alpha/../x" },
		func(c *AdminConfig) { c.Workspaces[0].Origins = []string{"https://alpha.example/path"} },
	}
	for i, mut := range bad {
		c, _ := testConfig(t)
		mut(c)
		if err := c.Validate(); err == nil {
			t.Fatalf("bad config %d accepted", i)
		}
	}
}

func TestAdminCLIUserLifecycle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "etc", "admin.json")
	run := func(stdin string, args ...string) (int, string) {
		var out bytes.Buffer
		code := adminCLI(args, strings.NewReader(stdin), &out, &out)
		return code, out.String()
	}
	if code, out := run("", "workspace", "set", "-config", p, "-id", "studio", "-name", "Аксиомантик",
		"-origins", "https://axiomantic.example", "-site", "https://axiomantic.example"); code != 0 {
		t.Fatal(out)
	}
	if code, out := run("short\n", "user", "add", "-config", p, "-workspace", "studio", "-login", "a@x.ru", "-name", "Анна",
		"-role", "owner", "-password-stdin"); code == 0 || !strings.Contains(out, "at least") {
		t.Fatalf("short password accepted: %s", out)
	}
	code, out := run("", "user", "add", "-config", p, "-workspace", "studio", "-login", "a@x.ru", "-name", "Анна", "-role", "owner")
	if code != 0 || !strings.Contains(out, "password (shown once") {
		t.Fatalf("add: %s", out)
	}
	pw := strings.TrimSpace(out[strings.LastIndex(out, ":")+1:])
	c, err := LoadAdminConfig(p)
	if err != nil || !CheckPassword(c.UserByLogin("a@x.ru").Hash, pw) {
		t.Fatalf("generated password does not work: %v", err)
	}
	if code, _ := run("", "user", "add", "-config", p, "-workspace", "nope", "-login", "b@x.ru", "-name", "Б", "-role", "owner"); code == 0 {
		t.Fatal("user added to a missing workspace")
	}
	if code, _ := run("", "user", "add", "-config", p, "-workspace", "studio", "-login", "A@X.RU", "-name", "Дубль", "-role", "viewer"); code == 0 {
		t.Fatal("duplicate login accepted")
	}
	_, list := run("", "user", "list", "-config", p)
	if !strings.Contains(list, "a@x.ru") || strings.Contains(list, "pbkdf2") {
		t.Fatalf("list: %s", list)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v", st.Mode().Perm())
	}
	if code, out := run("", "demo", "-config", p, "-store", filepath.Join(filepath.Dir(p), "leads.jsonl"), "-workspace", "studio", "-n", "30"); code != 0 {
		t.Fatalf("demo: %s", out)
	}
	if code, _ := run("", "demo", "-config", p, "-store", filepath.Join(filepath.Dir(p), "leads.jsonl"), "-workspace", "studio"); code == 0 {
		t.Fatal("demo wrote into a store that already has leads")
	}
}

func TestCheckPasswordRejectsDamagedHashes(t *testing.T) {
	good, _ := HashPassword("correct horse battery")
	if !CheckPassword(good, "correct horse battery") || CheckPassword(good, "correct horse batterY") {
		t.Fatal("basic check")
	}
	for _, h := range []string{"", "plain", "pbkdf2-sha256$1$AAAA$AAAA", good + "x", strings.Replace(good, "pbkdf2", "md5", 1)} {
		if CheckPassword(h, "correct horse battery") {
			t.Fatalf("damaged hash %q accepted", h)
		}
	}
}

// ------------------------------------------------------------- store robustness

// A write cut short by a crash leaves half a line. The next lead must not
// be glued onto it (and so lost from the panel and out of Erase's reach).
func TestStoreSurvivesACutWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "leads.jsonl")
	good, _ := json.Marshal(Record{ID: "20261017-aaaaaaaaaa", Time: time.Now(), WS: "alpha", Lead: Lead{Name: "Первая"}})
	if err := os.WriteFile(p, append(append(good, '\n'), []byte(`{"id":"20261017-bbbbbbbbbb","lead":{"name":"Обры`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, err := OpenFileStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if err := fs.Append(Record{ID: "20261017-cccccccccc", Time: time.Now(), WS: "alpha", Lead: Lead{Name: "Третья"}}); err != nil {
		t.Fatal(err)
	}
	var names []string
	skipped, err := fs.Load(func(r Record) { names = append(names, r.Lead.Name) }, func(Event) {})
	if err != nil || skipped != 1 || strings.Join(names, ",") != "Первая,Третья" {
		t.Fatalf("loaded %v, skipped %d, err %v", names, skipped, err)
	}
}

// A damaged line longer than any record neither stops the start nor
// breaks erasing; it is not kept by an erase, since it may hold
// personal data nobody can address.
func TestStoreSkipsOverlongDamage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "leads.jsonl")
	rec := Record{ID: "20261017-aaaaaaaaaa", Time: time.Now(), WS: "alpha", Lead: Lead{Name: "Стереть"}}
	good, _ := json.Marshal(rec)
	junk := bytes.Repeat([]byte("x"), maxLine+10)
	data := append(append(append(append(good, '\n'), junk...), '\n'), []byte("{\"broken\n")...)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fs, err := OpenFileStore(p)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	n := 0
	skipped, err := fs.Load(func(Record) { n++ }, func(Event) {})
	if err != nil || n != 1 || skipped != 2 {
		t.Fatalf("records %d skipped %d err %v", n, skipped, err)
	}
	ev := Event{Ev: evErase, ID: newEventID(), LeadID: rec.ID, WS: "alpha", Time: time.Now(), By: "u_000000000001"}
	if err := fs.Erase("alpha", "alpha", rec.ID, ev); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if bytes.Contains(after, []byte("Стереть")) || bytes.Contains(after, junk[:1000]) || len(after) > 4096 {
		t.Fatalf("after erase: %d bytes", len(after))
	}
	if !bytes.Contains(after, []byte("{\"broken")) {
		t.Fatal("a short damaged line was dropped")
	}
}

// Without the panel, leads carry no workspace, so turning the panel on
// later files them under the default workspace instead of orphaning them.
func TestLeadsWithoutPanelCarryNoWorkspace(t *testing.T) {
	h := newHarness(t, http.StatusOK)
	h.post(callLead())
	if got := h.stored(); len(got) != 1 || got[0].WS != "" {
		t.Fatalf("stored %+v", got)
	}
}

func TestSavingConfigKeepsItsOwner(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("needs root to hand the file to another user")
	}
	p := filepath.Join(t.TempDir(), "admin.json")
	cfg, _ := testConfig(t)
	if err := SaveAdminConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(p, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	cfg.Users[0].Name = "Новое Имя"
	if err := SaveAdminConfig(p, cfg); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if sys := st.Sys().(*syscall.Stat_t); sys.Uid != 65534 || sys.Gid != 65534 || st.Mode().Perm() != 0o600 {
		t.Fatalf("owner %d:%d mode %v", sys.Uid, sys.Gid, st.Mode().Perm())
	}
}
