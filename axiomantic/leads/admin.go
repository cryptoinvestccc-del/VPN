package main

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The panel's pages and scripts are built into the binary, so a client's
// deploy is this one file plus admin.json.
//
//go:embed admin
var adminFiles embed.FS

const (
	cookieName    = "__Host-axm_admin"
	sessionIdle   = 12 * time.Hour
	sessionMaxAge = 7 * 24 * time.Hour
	adminVersion  = "1.0.0"
	maxNote       = 2000
	waitingAfter  = time.Hour
	staleAfter    = 72 * time.Hour
)

// Admin is the team panel: sign-in, sessions and the /api/admin routes.
// Every handler works inside the signed-in user's workspace and nowhere
// else; ids from the browser are looked up in that workspace only.
type Admin struct {
	cfg       *ConfigSource
	sessions  *Sessions
	index     *Index
	store     *FileStore
	loginIP   *Limiter // every sign-in attempt, per address
	loginFail *Limiter // failed sign-ins, per login and address
	hashSlots chan struct{}
	clientIP  func(*http.Request) string
	hasNotify func(*Workspace) bool
	uiDir     string // serve the panel from this folder instead of the binary (development)
	now       func() time.Time
	log       *log.Logger

	writeMu sync.Mutex // one change at a time keeps "from" in history true

	seenMu sync.Mutex
	seen   map[string]time.Time
}

func NewAdmin(cfg *ConfigSource, idx *Index, store *FileStore, now func() time.Time, logger *log.Logger) *Admin {
	return &Admin{
		cfg:       cfg,
		sessions:  NewSessions(sessionIdle, sessionMaxAge),
		index:     idx,
		store:     store,
		loginIP:   NewLimiter(15*time.Minute, 10, 1<<30),
		loginFail: NewLimiter(15*time.Minute, 5, 1<<30),
		hashSlots: make(chan struct{}, 2),
		now:       now,
		log:       logger,
		seen:      map[string]time.Time{},
	}
}

func (a *Admin) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/login", a.handleLogin)
	mux.HandleFunc("POST /api/admin/logout", a.auth(roleViewer, a.handleLogout))
	mux.HandleFunc("GET /api/admin/session", a.auth(roleViewer, a.handleSession))
	mux.HandleFunc("GET /api/admin/leads", a.auth(roleViewer, a.handleLeads))
	mux.HandleFunc("GET /api/admin/leads/{id}", a.auth(roleViewer, a.handleLead))
	mux.HandleFunc("PATCH /api/admin/leads/{id}", a.auth(roleManager, a.handlePatch))
	mux.HandleFunc("DELETE /api/admin/leads/{id}", a.auth(roleOwner, a.handleErase))
	mux.HandleFunc("GET /api/admin/stats", a.auth(roleViewer, a.handleStats))
	mux.HandleFunc("GET /api/admin/activity", a.auth(roleViewer, a.handleActivity))
	mux.HandleFunc("GET /api/admin/team", a.auth(roleViewer, a.handleTeam))
	mux.HandleFunc("GET /api/admin/settings", a.auth(roleViewer, a.handleSettings))
	mux.HandleFunc("GET /api/admin/export.csv", a.auth(roleManager, a.handleExport))
	mux.HandleFunc("/api/admin/", func(w http.ResponseWriter, r *http.Request) {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
	})
	mux.Handle("GET /admin/", a.uiHandler())
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
}

type apiError struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

// adminJSON answers an API call. The extra headers keep a JSON answer
// from ever being rendered or framed as a page.
func adminJSON(w http.ResponseWriter, code int, v any) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	writeJSON(w, code, v)
}

// actor is who is calling, resolved from the session cookie on every request.
type actor struct {
	user  *User
	ws    *Workspace
	sess  *Session
	cfg   *AdminConfig
	token string
}

// sameSite rejects writes a browser makes on behalf of another site. An
// absent Origin is not a browser form; such a caller still needs the
// session and its CSRF token.
func sameSite(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

func clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}

// auth resolves the session, checks CSRF on writes and the role, and only
// then calls the handler. Nothing reaches a handler without all three.
func (a *Admin) auth(min string, next func(http.ResponseWriter, *http.Request, *actor)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := a.cfg.Get()
		token := ""
		if c, err := r.Cookie(cookieName); err == nil {
			token = c.Value
		}
		now := a.now()
		sess := a.sessions.Get(token, now)
		if sess == nil {
			if token != "" {
				clearCookie(w)
			}
			adminJSON(w, http.StatusUnauthorized, apiError{Error: "auth"})
			return
		}
		u := cfg.User(sess.UserID)
		ws := cfg.Workspace(sess.Workspace)
		if !sess.validFor(u) || ws == nil {
			a.sessions.Delete(token)
			clearCookie(w)
			adminJSON(w, http.StatusUnauthorized, apiError{Error: "auth"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameSite(r) || !sameToken(r.Header.Get("X-CSRF-Token"), sess.CSRF) {
				adminJSON(w, http.StatusForbidden, apiError{Error: "csrf"})
				return
			}
		}
		if roleRank[u.Role] < roleRank[min] {
			adminJSON(w, http.StatusForbidden, apiError{Error: "role"})
			return
		}
		a.seenMu.Lock()
		a.seen[u.ID] = now
		a.seenMu.Unlock()
		next(w, r, &actor{user: u, ws: ws, sess: sess, cfg: cfg, token: token})
	}
}

// decodeJSON reads one JSON object with no unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) (int, string) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return http.StatusUnsupportedMediaType, "content-type"
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return http.StatusRequestEntityTooLarge, "size"
		}
		return http.StatusBadRequest, "json"
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return http.StatusBadRequest, "json"
	}
	return 0, ""
}

func (a *Admin) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameSite(r) {
		adminJSON(w, http.StatusForbidden, apiError{Error: "csrf"})
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		adminJSON(w, http.StatusUnsupportedMediaType, apiError{Error: "content-type"})
		return
	}
	now := a.now()
	ip := a.clientIP(r)
	if ok, retry := a.loginIP.Allow(ip, now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		adminJSON(w, http.StatusTooManyRequests, apiError{Error: "rate"})
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if code, e := decodeJSON(w, r, 4<<10, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	login := normLogin(body.Login)
	switch {
	case login == "" || len(login) > 200:
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "login"})
		return
	case body.Password == "" || len(body.Password) > 256:
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "password"})
		return
	}
	// Failures count per login *and* address: guessing one person's
	// password from one place stops after five tries, while nobody can
	// lock that person out from somewhere else. There is no ceiling for
	// everyone together — that would let anyone lock the whole panel —
	// the two hashing slots below are what bounds the load.
	failKey := login + "\x00" + ip
	if over, retry := a.loginFail.Over(failKey, now); over {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		adminJSON(w, http.StatusTooManyRequests, apiError{Error: "rate"})
		return
	}
	// Key derivation is deliberately slow; two at a time keeps a burst of
	// sign-ins from eating every core, and a caller waits at most a few
	// seconds for a slot.
	select {
	case a.hashSlots <- struct{}{}:
	case <-time.After(5 * time.Second):
		w.Header().Set("Retry-After", "5")
		adminJSON(w, http.StatusServiceUnavailable, apiError{Error: "busy"})
		return
	case <-r.Context().Done():
		return
	}
	u := a.cfg.Get().UserByLogin(login)
	var good bool
	if u == nil || u.Disabled {
		CheckPassword(dummyHash(), body.Password)
	} else {
		good = CheckPassword(u.Hash, body.Password)
	}
	<-a.hashSlots
	if !good {
		a.loginFail.Hit(failKey, now)
		// No login in the log: a password typed into the login field
		// would otherwise end up there.
		a.log.Printf("admin: failed sign-in from %s", ip)
		adminJSON(w, http.StatusUnauthorized, apiError{Error: "credentials"})
		return
	}
	token, _ := a.sessions.New(u, now)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", MaxAge: int(sessionMaxAge.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	a.seenMu.Lock()
	a.seen[u.ID] = now
	a.seenMu.Unlock()
	a.log.Printf("admin: %s signed in to %s", u.ID, u.Workspace)
	adminJSON(w, http.StatusOK, reply{OK: true})
}

func (a *Admin) handleLogout(w http.ResponseWriter, r *http.Request, ac *actor) {
	a.sessions.Delete(ac.token)
	clearCookie(w)
	adminJSON(w, http.StatusOK, reply{OK: true})
}

// ---------------------------------------------------------------- views

type person struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Initials string `json:"initials"`
}

func initials(name string) string {
	var out []rune
	for _, f := range strings.Fields(name) {
		r, _ := utf8.DecodeRuneInString(f)
		out = append(out, []rune(strings.ToUpper(string(r)))...)
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

// personOf names a team member of this workspace. Someone removed from
// the config still shows up in old history, without their details.
func personOf(cfg *AdminConfig, ws, id string) *person {
	if id == "" {
		return nil
	}
	u := cfg.User(id)
	if u == nil || u.Workspace != ws {
		return &person{ID: id, Name: "Бывший участник", Initials: "—"}
	}
	return &person{ID: u.ID, Name: u.Name, Initials: initials(u.Name)}
}

type wsView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	TZ      string `json:"tz"`
	SiteURL string `json:"siteURL"`
	Brand   Brand  `json:"brand"`
}

func viewWS(w *Workspace) wsView {
	return wsView{ID: w.ID, Name: w.Name, TZ: w.TZ, SiteURL: w.SiteURL, Brand: w.Brand}
}

func (a *Admin) handleSession(w http.ResponseWriter, r *http.Request, ac *actor) {
	newCount := 0
	for _, v := range a.index.All(ac.ws.ID) {
		if v.Status == "new" {
			newCount++
		}
	}
	adminJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": ac.user.ID, "name": ac.user.Name, "login": ac.user.Login, "role": ac.user.Role,
			"initials": initials(ac.user.Name),
		},
		"workspace": viewWS(ac.ws),
		"csrf":      ac.sess.CSRF,
		"counts":    map[string]int{"new": newCount},
	})
}

type leadSummary struct {
	ID          string    `json:"id"`
	Time        time.Time `json:"time"`
	Updated     time.Time `json:"updated"`
	Status      string    `json:"status"`
	Name        string    `json:"name"`
	Mode        string    `json:"mode"`
	Source      string    `json:"source"`
	ContactKind string    `json:"contactKind"`
	Contact     string    `json:"contact"`
	Interest    string    `json:"interest"`
	Budget      string    `json:"budget"`
	Preview     string    `json:"preview"`
	Assignee    *person   `json:"assignee"`
	Notes       int       `json:"notes"`
	Erased      bool      `json:"erased"`
}

type leadDetail struct {
	leadSummary
	Phone   string `json:"phone"`
	When    string `json:"when"`
	Channel string `json:"channel"`
	Message string `json:"message"`
	Consent bool   `json:"consent"`
}

func contactOf(l Lead) (kind, value string) {
	switch l.Mode {
	case "call":
		return "phone", l.Phone
	case "write":
		if l.Channel == "email" {
			return "email", l.Contact
		}
		return l.Channel, l.Contact
	}
	if strings.Contains(l.Contact, "@") {
		return "email", l.Contact
	}
	return "phone", l.Contact
}

func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 140 {
		return strings.TrimSpace(string(r[:139])) + "…"
	}
	return s
}

func summarize(cfg *AdminConfig, ws string, v LeadView) leadSummary {
	l := v.Rec.Lead
	kind, contact := contactOf(l)
	return leadSummary{
		ID: v.Rec.ID, Time: v.Rec.Time, Updated: v.Updated, Status: v.Status, Name: l.Name,
		Mode: l.Mode, Source: l.Source, ContactKind: kind, Contact: contact, Interest: l.Interest,
		Budget: l.Budget, Preview: preview(l.Message), Assignee: personOf(cfg, ws, v.Assignee),
		Notes: v.Notes, Erased: v.Rec.Erased,
	}
}

type eventView struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
	By       *person   `json:"by"`
	From     string    `json:"from,omitempty"`
	To       string    `json:"to,omitempty"`
	Text     string    `json:"text,omitempty"`
	Assignee *person   `json:"assignee,omitempty"`
	Lead     *leadRef  `json:"lead,omitempty"`
}

type leadRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Mode   string `json:"mode"`
}

func viewEvent(cfg *AdminConfig, ws string, e *Event, v LeadView) eventView {
	if e == nil {
		return eventView{ID: "c_" + v.Rec.ID, Time: v.Rec.Time, Kind: "created"}
	}
	out := eventView{ID: e.ID, Time: e.Time, Kind: e.Ev, By: personOf(cfg, ws, e.By)}
	switch e.Ev {
	case evStatus:
		out.From, out.To = e.From, e.To
	case evNote:
		out.Text = e.Text
	case evAssign:
		out.Assignee = personOf(cfg, ws, e.To)
	}
	return out
}

func history(cfg *AdminConfig, ws string, v LeadView) []eventView {
	out := make([]eventView, 0, len(v.Events)+1)
	for i := len(v.Events) - 1; i >= 0; i-- {
		out = append(out, viewEvent(cfg, ws, &v.Events[i], v))
	}
	return append(out, viewEvent(cfg, ws, nil, v))
}

func detail(cfg *AdminConfig, ws string, v LeadView) map[string]any {
	l := v.Rec.Lead
	return map[string]any{
		"lead": leadDetail{
			leadSummary: summarize(cfg, ws, v), Phone: l.Phone, When: l.When, Channel: l.Channel,
			Message: l.Message, Consent: l.Consent,
		},
		"history": history(cfg, ws, v),
	}
}

// ---------------------------------------------------------------- list

type leadFilter struct {
	status, q, mode, interest, budget, assignee string
	waiting, stale                              bool
}

var reLeadID = regexp.MustCompile(`^[0-9]{8}-[0-9a-f]{10}$`)

func parseFilter(q url.Values) (leadFilter, string) {
	f := leadFilter{
		status: q.Get("status"), q: strings.ToLower(strings.TrimSpace(q.Get("q"))), mode: q.Get("mode"),
		interest: q.Get("interest"), budget: q.Get("budget"), assignee: q.Get("assignee"),
		waiting: q.Get("waiting") == "1", stale: q.Get("stale") == "1",
	}
	if f.status == "" {
		f.status = "all"
	}
	if f.status != "all" && f.status != "open" && f.status != "closed" && f.status != "progress" && statusLabels[f.status] == "" {
		return f, "status"
	}
	if f.mode != "" && f.mode != "call" && f.mode != "write" && f.mode != "form" {
		return f, "mode"
	}
	if f.interest != "" && !oneOf(f.interest, interestLabels) {
		return f, "interest"
	}
	if f.budget != "" && !oneOf(f.budget, budgetLabels) {
		return f, "budget"
	}
	if utf8.RuneCountInString(f.q) > 100 {
		return f, "q"
	}
	return f, ""
}

func statusMatches(want, got string) bool {
	switch want {
	case "all":
		return true
	case "open":
		return isOpen(got)
	case "closed":
		return !isOpen(got)
	case "progress": // being worked on: open, but past «Новая»
		return got == "contacted" || got == "proposal"
	}
	return want == got
}

// keep applies every filter except status, which the tab counts ignore.
func (f *leadFilter) keep(v *LeadView, now time.Time) bool {
	l := v.Rec.Lead
	switch {
	case f.mode != "" && l.Mode != f.mode,
		f.interest != "" && l.Interest != f.interest,
		f.budget != "" && l.Budget != f.budget,
		f.assignee == "none" && v.Assignee != "",
		f.assignee != "" && f.assignee != "none" && v.Assignee != f.assignee,
		f.waiting && !isWaiting(v, now),
		f.stale && !isStale(v, now):
		return false
	}
	return v.matches(f.q)
}

func isWaiting(v *LeadView, now time.Time) bool {
	return v.Status == "new" && v.Answered.IsZero() && now.Sub(v.Rec.Time) > waitingAfter
}

func isStale(v *LeadView, now time.Time) bool {
	return (v.Status == "contacted" || v.Status == "proposal") && now.Sub(v.Updated) > staleAfter
}

// filtered returns this workspace's leads that pass the filter, newest
// first, plus the per-status counts the tabs show.
func (a *Admin) filtered(ws string, f leadFilter, sortBy string) ([]LeadView, map[string]int) {
	now := a.now()
	counts := map[string]int{"all": 0, "open": 0, "closed": 0, "progress": 0}
	for _, s := range statusOrder {
		counts[s] = 0
	}
	var out []LeadView
	for _, v := range a.index.All(ws) {
		if !f.keep(&v, now) {
			continue
		}
		counts["all"]++
		counts[v.Status]++
		if isOpen(v.Status) {
			counts["open"]++
			if v.Status != "new" {
				counts["progress"]++
			}
		} else {
			counts["closed"]++
		}
		if statusMatches(f.status, v.Status) {
			out = append(out, v)
		}
	}
	switch sortBy {
	case "old":
		// All is oldest first already
	case "updated":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	default:
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, counts
}

func (a *Admin) handleLeads(w http.ResponseWriter, r *http.Request, ac *actor) {
	q := r.URL.Query()
	f, bad := parseFilter(q)
	if bad != "" {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: bad})
		return
	}
	sortBy := q.Get("sort")
	if sortBy != "" && sortBy != "new" && sortBy != "old" && sortBy != "updated" {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "sort"})
		return
	}
	limit, offset := 20, 0
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "limit"})
			return
		}
		limit = n
	}
	if s := q.Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "offset"})
			return
		}
		offset = n
	}
	list, counts := a.filtered(ac.ws.ID, f, sortBy)
	items := []leadSummary{}
	for i := offset; i < len(list) && i < offset+limit; i++ {
		items = append(items, summarize(ac.cfg, ac.ws.ID, list[i]))
	}
	adminJSON(w, http.StatusOK, map[string]any{
		"total": len(list), "limit": limit, "offset": offset, "counts": counts, "items": items,
	})
}

// lookup is the owner check: the id is searched for in the caller's
// workspace only, and a malformed id is as absent as a foreign one.
func (a *Admin) lookup(w http.ResponseWriter, r *http.Request, ac *actor) (LeadView, bool) {
	id := r.PathValue("id")
	if !reLeadID.MatchString(id) {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return LeadView{}, false
	}
	v, ok := a.index.Get(ac.ws.ID, id)
	if !ok {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return LeadView{}, false
	}
	return v, true
}

func (a *Admin) handleLead(w http.ResponseWriter, r *http.Request, ac *actor) {
	v, ok := a.lookup(w, r, ac)
	if !ok {
		return
	}
	adminJSON(w, http.StatusOK, detail(ac.cfg, ac.ws.ID, v))
}

func newEventID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "e_" + hex.EncodeToString(b[:])
}

func (a *Admin) handlePatch(w http.ResponseWriter, r *http.Request, ac *actor) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	v, ok := a.lookup(w, r, ac)
	if !ok {
		return
	}
	var body struct {
		Status   *string `json:"status"`
		Assignee *string `json:"assignee"`
		Note     *string `json:"note"`
	}
	if code, e := decodeJSON(w, r, 16<<10, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	if body.Status == nil && body.Assignee == nil && body.Note == nil {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "body"})
		return
	}
	// Check everything before writing anything.
	if body.Status != nil && statusLabels[*body.Status] == "" {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "status"})
		return
	}
	if body.Assignee != nil && *body.Assignee != "" {
		// The assignee id comes from the browser too: it must be an
		// active member of the caller's own workspace.
		u := ac.cfg.User(*body.Assignee)
		if u == nil || u.Workspace != ac.ws.ID || u.Disabled {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "assignee"})
			return
		}
	}
	var note string
	if body.Note != nil {
		note = strings.TrimSpace(*body.Note)
		if !between(note, 1, maxNote) || !plain(note, true) {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "note"})
			return
		}
	}
	now := a.now().UTC()
	var events []Event
	mk := func(kind string) Event {
		return Event{Ev: kind, ID: newEventID(), LeadID: v.Rec.ID, WS: ac.ws.ID, Time: now, By: ac.user.ID}
	}
	if body.Status != nil && *body.Status != v.Status {
		e := mk(evStatus)
		e.From, e.To = v.Status, *body.Status
		events = append(events, e)
	}
	if body.Assignee != nil && *body.Assignee != v.Assignee {
		e := mk(evAssign)
		e.From, e.To = v.Assignee, *body.Assignee
		events = append(events, e)
	}
	if body.Note != nil {
		e := mk(evNote)
		e.Text = note
		events = append(events, e)
	}
	for _, e := range events {
		if err := a.store.AppendEvent(e); err != nil {
			a.log.Printf("lead %s: store event: %v", v.Rec.ID, err)
			adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
			return
		}
		a.index.AddEvent(e)
	}
	v, _ = a.index.Get(ac.ws.ID, v.Rec.ID)
	adminJSON(w, http.StatusOK, detail(ac.cfg, ac.ws.ID, v))
}

// handleErase answers a visitor's request to delete their data (152-ФЗ,
// ст. 21): the store file itself is rewritten without it.
func (a *Admin) handleErase(w http.ResponseWriter, r *http.Request, ac *actor) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	v, ok := a.lookup(w, r, ac)
	if !ok {
		return
	}
	if v.Rec.Erased {
		adminJSON(w, http.StatusOK, reply{OK: true})
		return
	}
	e := Event{Ev: evErase, ID: newEventID(), LeadID: v.Rec.ID, WS: ac.ws.ID, Time: a.now().UTC(), By: ac.user.ID}
	if err := a.store.Erase(ac.ws.ID, a.index.defaultWS, v.Rec.ID, e); err != nil {
		a.log.Printf("lead %s: erase: %v", v.Rec.ID, err)
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.index.AddEvent(e)
	a.log.Printf("lead %s: personal data erased by %s", v.Rec.ID, ac.user.ID)
	adminJSON(w, http.StatusOK, reply{OK: true})
}

// ---------------------------------------------------------------- dashboard

var modeLabels = map[string]string{"call": "Звонок", "write": "Сообщение", "form": "Форма"}

type ranked struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// rank orders what leads asked for, most frequent first. Leads that did
// not say (the contacts form has no such question) are left out.
func rank(m map[string]int, labels map[string]string) []ranked {
	out := []ranked{}
	for k, n := range m {
		if k == "" {
			continue
		}
		label := labels[k]
		if r, size := utf8.DecodeRuneInString(label); size > 0 {
			label = strings.ToUpper(string(r)) + label[size:]
		}
		out = append(out, ranked{Key: k, Label: label, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func (a *Admin) handleStats(w http.ResponseWriter, r *http.Request, ac *actor) {
	days := 30
	if s := r.URL.Query().Get("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || (n != 7 && n != 30 && n != 90) {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "days"})
			return
		}
		days = n
	}
	loc := ac.ws.loc
	if loc == nil {
		loc = time.UTC
	}
	nowUTC := a.now()
	now := nowUTC.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from := today.AddDate(0, 0, -(days - 1))
	to := today.AddDate(0, 0, 1)
	prevFrom := from.AddDate(0, 0, -days)

	type day struct {
		Date  string `json:"date"`
		Leads int    `json:"leads"`
		Won   int    `json:"won"`
	}
	series := make([]day, days)
	for i := range series {
		series[i].Date = from.AddDate(0, 0, i).Format("2006-01-02")
	}
	byStatus := map[string]int{}
	for _, s := range statusOrder {
		byStatus[s] = 0
	}
	byInterest, byMode := map[string]int{}, map[string]int{}
	var leads, won, prevLeads, prevWon, open, waiting, stale, unassigned, responded int
	var mins []float64
	for _, v := range a.index.All(ac.ws.ID) {
		t := v.Rec.Time.In(loc)
		switch {
		case !t.Before(from) && t.Before(to):
			leads++
			byStatus[v.Status]++
			byInterest[v.Rec.Lead.Interest]++
			byMode[v.Rec.Lead.Mode]++
			// calendar days, so a DST change cannot shift a lead to the wrong day
			i := int(time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC).Sub(
				time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, time.UTC)).Hours() / 24)
			if i >= 0 && i < days {
				series[i].Leads++
				if v.Status == "won" {
					series[i].Won++
				}
			}
			if v.Status == "won" {
				won++
			}
			if !v.Answered.IsZero() {
				responded++
				mins = append(mins, v.Answered.Sub(v.Rec.Time).Minutes())
			}
		case !t.Before(prevFrom) && t.Before(from):
			prevLeads++
			if v.Status == "won" {
				prevWon++
			}
		}
		if isOpen(v.Status) {
			open++
			if v.Assignee == "" {
				unassigned++
			}
		}
		if isWaiting(&v, nowUTC) {
			waiting++
		}
		if isStale(&v, nowUTC) {
			stale++
		}
	}
	ratio := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return float64(a) / float64(b)
	}
	var median any
	if len(mins) > 0 {
		sort.Float64s(mins)
		m := mins[len(mins)/2]
		if len(mins)%2 == 0 {
			m = (mins[len(mins)/2-1] + mins[len(mins)/2]) / 2
		}
		median = int(m + 0.5)
	}
	attention := []map[string]any{
		{"kind": "waiting", "count": waiting, "title": "Без ответа больше часа",
			"hint": "Новые заявки, которых никто не коснулся", "query": "status=new&waiting=1"},
		{"kind": "stale", "count": stale, "title": "Зависли в работе",
			"hint": "Нет движения больше трёх дней", "query": "status=open&stale=1"},
		{"kind": "unassigned", "count": unassigned, "title": "Без ответственного",
			"hint": "Открытые заявки без менеджера", "query": "status=open&assignee=none"},
	}
	adminJSON(w, http.StatusOK, map[string]any{
		"days": days, "from": from.Format("2006-01-02"), "to": today.Format("2006-01-02"),
		"series": series,
		"totals": map[string]any{
			"leads": leads, "prevLeads": prevLeads, "open": open, "newWaiting": waiting,
			"won": won, "conversion": ratio(won, leads), "prevConversion": ratio(prevWon, prevLeads),
			"responseMedianMin": median, "responseTargetMin": 60, "respondedShare": ratio(responded, leads),
		},
		"byStatus":   byStatus,
		"byInterest": rank(byInterest, interestLabels),
		"byMode":     rank(byMode, modeLabels),
		"attention":  attention,
	})
}

func (a *Admin) handleActivity(w http.ResponseWriter, r *http.Request, ac *actor) {
	n := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > 100 {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "limit"})
			return
		}
		n = v
	}
	items := []eventView{}
	for _, f := range a.index.Feed(ac.ws.ID, n) {
		ev := viewEvent(ac.cfg, ac.ws.ID, f.Event, f.Lead)
		ev.Lead = &leadRef{ID: f.Lead.Rec.ID, Name: f.Lead.Rec.Lead.Name, Status: f.Lead.Status, Mode: f.Lead.Rec.Lead.Mode}
		items = append(items, ev)
	}
	adminJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *Admin) handleTeam(w http.ResponseWriter, r *http.Request, ac *actor) {
	type member struct {
		ID       string     `json:"id"`
		Name     string     `json:"name"`
		Login    string     `json:"login"`
		Role     string     `json:"role"`
		Initials string     `json:"initials"`
		LastSeen *time.Time `json:"lastSeen"`
		Active   bool       `json:"active"`
	}
	items := []member{}
	a.seenMu.Lock()
	for _, u := range ac.cfg.Members(ac.ws.ID) {
		m := member{ID: u.ID, Name: u.Name, Login: u.Login, Role: u.Role, Initials: initials(u.Name), Active: !u.Disabled}
		if t, ok := a.seen[u.ID]; ok {
			t := t.UTC()
			m.LastSeen = &t
		}
		items = append(items, m)
	}
	a.seenMu.Unlock()
	adminJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *Admin) handleSettings(w http.ResponseWriter, r *http.Request, ac *actor) {
	origins := append([]string{}, ac.ws.Origins...)
	adminJSON(w, http.StatusOK, map[string]any{
		"workspace":     viewWS(ac.ws),
		"origins":       origins,
		"notifications": map[string]bool{"telegram": a.hasNotify != nil && a.hasNotify(ac.ws)},
		"version":       adminVersion,
	})
}

// ---------------------------------------------------------------- pages

// The panel's own CSP: nothing but its files, never framed, no referrer.
const adminCSP = "default-src 'self'; img-src 'self' data:; style-src 'self'; font-src 'self'; script-src 'self'; " +
	"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

func setAdminPageHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", adminCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

var extTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".svg": "image/svg+xml", ".woff2": "font/woff2", ".txt": "text/plain; charset=utf-8", ".png": "image/png",
	".ico": "image/x-icon", ".json": "application/json",
}

type uiFile struct {
	data  []byte
	ctype string
	etag  string
}

// uiHandler serves the embedded panel. Files are revalidated by ETag on
// every load, so a new binary is picked up at once without stale scripts.
func (a *Admin) uiHandler() http.Handler {
	load := func(src fs.FS) map[string]uiFile {
		files := map[string]uiFile{}
		_ = fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ct := extTypes[path.Ext(p)]
			if ct == "" {
				return nil // only known kinds of file are published
			}
			data, err := fs.ReadFile(src, p)
			if err != nil {
				return nil
			}
			sum := sha256.Sum256(data)
			files[p] = uiFile{data: data, ctype: ct, etag: `"` + hex.EncodeToString(sum[:12]) + `"`}
			return nil
		})
		return files
	}
	var embedded map[string]uiFile
	if sub, err := fs.Sub(adminFiles, "admin"); err == nil {
		embedded = load(sub)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setAdminPageHeaders(w)
		p := strings.TrimPrefix(r.URL.Path, "/admin/")
		if p == "" {
			p = "index.html"
		}
		files := embedded
		if a.uiDir != "" {
			// Working on the panel: read the files as they are now.
			files = load(os.DirFS(a.uiDir))
		}
		f, ok := files[p]
		if !ok {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if p == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("ETag", f.etag)
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, f.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", f.ctype)
		w.Header().Set("Content-Length", strconv.Itoa(len(f.data)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(f.data)
		}
	})
}
