package main

// Blog comments and reactions. Visitors comment anonymously; a comment
// shows on the site only after a manager approves it in the panel. Likes
// and dislikes are counts per post. Nothing about a visitor is stored: no
// name, no address — rate limits and "who already reacted" live in memory.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type blogComment struct {
	ID     string    `json:"id"`
	Post   string    `json:"post"`
	Text   string    `json:"text"`
	Time   time.Time `json:"time"`
	Status string    `json:"status"` // pending | approved | hidden
	By     string    `json:"by,omitempty"`
}

type blogCounts struct {
	Likes    int `json:"likes"`
	Dislikes int `json:"dislikes"`
}

type blogData struct {
	Comments  []blogComment          `json:"comments"`
	Reactions map[string]*blogCounts `json:"reactions"`
}

const (
	maxComments    = 20000
	maxCommentText = 1000
)

var (
	rePostID  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	blogLocks sync.Map // site data folder → *sync.Mutex
)

func blogLock(sc *SiteConfig) *sync.Mutex {
	m, _ := blogLocks.LoadOrStore(sc.Data, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func blogPath(sc *SiteConfig) string { return filepath.Join(sc.Data, "blog.json") }

func readBlog(sc *SiteConfig) *blogData {
	d := &blogData{Reactions: map[string]*blogCounts{}}
	raw, err := os.ReadFile(blogPath(sc))
	if err == nil {
		_ = json.Unmarshal(raw, d)
	}
	if d.Reactions == nil {
		d.Reactions = map[string]*blogCounts{}
	}
	return d
}

func writeBlog(sc *SiteConfig, d *blogData) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sc.Data, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(blogPath(sc), raw)
}

// publishedPosts are the post ids of the site as it is published: a
// comment or a reaction for anything else is refused.
func publishedPosts(sc *SiteConfig) map[string]string {
	out := map[string]string{}
	id := currentRelease(sc)
	if id == "" {
		return out
	}
	m, err := readRelease(sc, id, true)
	if err != nil {
		return out
	}
	posts, _ := m.Content["POSTS"].([]any)
	for _, p := range posts {
		if o, ok := p.(map[string]any); ok {
			if pid, ok := o["id"].(string); ok && rePostID.MatchString(pid) {
				out[pid] = postTitle(o)
			}
		}
	}
	return out
}

func postTitle(o map[string]any) string {
	author, _ := o["author"].(string)
	body, _ := o["body"].([]any)
	first := ""
	if len(body) > 0 {
		if b, ok := body[0].([]any); ok && len(b) == 2 {
			first, _ = b[1].(string)
		}
	}
	first = strings.NewReplacer("**", "").Replace(first)
	if r := []rune(first); len(r) > 70 {
		first = string(r[:69]) + "…"
	}
	if author != "" {
		return author + ": " + first
	}
	return first
}

// ---------------------------------------------------------------- public

// reactions remembers which address reacted how to which post, so a
// second click takes a reaction back and a script cannot unlike others'
// likes. Kept in memory only, keyed by a hash; it forgets on restart.
type reactionMemory struct {
	mu   sync.Mutex
	seen map[string]string
}

func (m *reactionMemory) swap(key, kind string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen == nil || len(m.seen) > 200000 {
		m.seen = map[string]string{}
	}
	prev := m.seen[key]
	if kind == "" {
		delete(m.seen, key)
	} else {
		m.seen[key] = kind
	}
	return prev
}

// blogSite finds the site a blog request is about: by Origin for a post
// from a page, by the Host for a plain read.
func (s *Server) blogSite(r *http.Request) (*SiteConfig, bool) {
	if s.admin == nil {
		return nil, false
	}
	cfg := s.admin.cfg.Get()
	var ws *Workspace
	if o := r.Header.Get("Origin"); o != "" {
		ws = cfg.WorkspaceForOrigin(o)
		if ws == nil {
			if n, err := normOrigin(o); err == nil && s.origins[n] {
				ws = cfg.Workspace(s.workspaceID())
			}
		}
	} else if r.Method == http.MethodGet {
		ws = cfg.WorkspaceForOrigin("https://" + r.Host)
		if ws == nil {
			ws = cfg.Workspace(s.workspaceID())
		}
	}
	if ws == nil || ws.Site == nil {
		return nil, false
	}
	return ws.Site, true
}

func (s *Server) blogRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/blog/stats", s.handleBlogStats)
	mux.HandleFunc("GET /api/blog/comments", s.handleBlogComments)
	mux.HandleFunc("POST /api/blog/comments", s.handleBlogComment)
	mux.HandleFunc("POST /api/blog/react", s.handleBlogReact)
}

func (s *Server) handleBlogStats(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.blogSite(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, reply{Error: "not_found"})
		return
	}
	posts := publishedPosts(sc)
	mu := blogLock(sc)
	mu.Lock()
	d := readBlog(sc)
	mu.Unlock()
	out := map[string]map[string]int{}
	for pid := range posts {
		c := d.Reactions[pid]
		if c == nil {
			c = &blogCounts{}
		}
		out[pid] = map[string]int{"likes": c.Likes, "dislikes": c.Dislikes, "comments": 0}
	}
	for _, c := range d.Comments {
		if c.Status == "approved" && out[c.Post] != nil {
			out[c.Post]["comments"]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": out})
}

func (s *Server) handleBlogComments(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.blogSite(r)
	pid := r.URL.Query().Get("post")
	if !ok || !rePostID.MatchString(pid) {
		writeJSON(w, http.StatusNotFound, reply{Error: "not_found"})
		return
	}
	if _, ok := publishedPosts(sc)[pid]; !ok {
		writeJSON(w, http.StatusNotFound, reply{Error: "not_found"})
		return
	}
	mu := blogLock(sc)
	mu.Lock()
	d := readBlog(sc)
	mu.Unlock()
	items := []map[string]any{}
	for _, c := range d.Comments {
		if c.Post == pid && c.Status == "approved" {
			items = append(items, map[string]any{"text": c.Text, "time": c.Time})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// decodeBlog reads a small JSON body from a page of a known site.
func (s *Server) decodeBlog(w http.ResponseWriter, r *http.Request, v any) (*SiteConfig, bool) {
	if r.Header.Get("Origin") == "" {
		writeJSON(w, http.StatusForbidden, reply{Error: "origin"})
		return nil, false
	}
	sc, ok := s.blogSite(r)
	if !ok {
		writeJSON(w, http.StatusForbidden, reply{Error: "origin"})
		return nil, false
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, reply{Error: "content-type"})
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSON(w, http.StatusRequestEntityTooLarge, reply{Error: "size"})
		} else {
			writeJSON(w, http.StatusBadRequest, reply{Error: "json"})
		}
		return nil, false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, reply{Error: "json"})
		return nil, false
	}
	return sc, true
}

func (s *Server) handleBlogComment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Post    string `json:"post"`
		Text    string `json:"text"`
		Website string `json:"website"`
	}
	sc, ok := s.decodeBlog(w, r, &body)
	if !ok {
		return
	}
	ip := s.clientIP(r)
	if ok, retry := s.commentLimit.Allow(limitKey(ip), s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, reply{Error: "rate"})
		return
	}
	if body.Website != "" {
		writeJSON(w, http.StatusOK, reply{OK: true}) // a bot: answer as if it worked
		return
	}
	text := strings.TrimSpace(bidiControls.Replace(spaces.Replace(body.Text)))
	if !between(text, 2, maxCommentText) || !plain(text, false) || !noFormat(text) {
		writeJSON(w, http.StatusBadRequest, reply{Field: "text", Error: "invalid"})
		return
	}
	if _, ok := publishedPosts(sc)[body.Post]; !ok {
		writeJSON(w, http.StatusNotFound, reply{Error: "not_found"})
		return
	}
	mu := blogLock(sc)
	mu.Lock()
	defer mu.Unlock()
	d := readBlog(sc)
	pending := 0
	for _, c := range d.Comments {
		if c.Status == "pending" {
			pending++
		}
	}
	// a flood fills the moderation queue, not the disk
	if len(d.Comments) >= maxComments || pending >= 500 {
		writeJSON(w, http.StatusTooManyRequests, reply{Error: "full"})
		return
	}
	d.Comments = append(d.Comments, blogComment{ID: s.newID(), Post: body.Post, Text: text, Time: s.now().UTC(), Status: "pending"})
	if err := writeBlog(sc, d); err != nil {
		s.log.Printf("blog: store: %v", err)
		writeJSON(w, http.StatusInternalServerError, reply{Error: "store"})
		return
	}
	writeJSON(w, http.StatusOK, reply{OK: true})
}

func (s *Server) handleBlogReact(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Post string `json:"post"`
		Kind string `json:"kind"` // like | dislike | "" (take back)
		Prev string `json:"prev"` // what this browser says it had; the server checks its own memory
	}
	sc, ok := s.decodeBlog(w, r, &body)
	if !ok {
		return
	}
	if body.Kind != "" && body.Kind != "like" && body.Kind != "dislike" {
		writeJSON(w, http.StatusBadRequest, reply{Field: "kind", Error: "invalid"})
		return
	}
	ip := s.clientIP(r)
	if ok, retry := s.reactLimit.Allow(limitKey(ip), s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, reply{Error: "rate"})
		return
	}
	if _, ok := publishedPosts(sc)[body.Post]; !ok {
		writeJSON(w, http.StatusNotFound, reply{Error: "not_found"})
		return
	}
	sum := sha256.Sum256([]byte(sc.Data + "\x00" + limitKey(ip) + "\x00" + body.Post))
	prev := s.reacted.swap(hex.EncodeToString(sum[:16]), body.Kind)
	mu := blogLock(sc)
	mu.Lock()
	defer mu.Unlock()
	d := readBlog(sc)
	c := d.Reactions[body.Post]
	if c == nil {
		c = &blogCounts{}
		d.Reactions[body.Post] = c
	}
	// only a reaction this server saw from this address can be taken back
	switch prev {
	case "like":
		c.Likes = max(0, c.Likes-1)
	case "dislike":
		c.Dislikes = max(0, c.Dislikes-1)
	}
	switch body.Kind {
	case "like":
		c.Likes++
	case "dislike":
		c.Dislikes++
	}
	if err := writeBlog(sc, d); err != nil {
		s.log.Printf("blog: store: %v", err)
		writeJSON(w, http.StatusInternalServerError, reply{Error: "store"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"likes": c.Likes, "dislikes": c.Dislikes})
}

// ---------------------------------------------------------------- panel

// handleComments lists the site's comments for moderation, newest first.
func (a *Admin) handleComments(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	status := r.URL.Query().Get("status")
	titles := publishedPosts(sc)
	if d, err := a.loadDraft(r.Context(), sc); err == nil {
		posts, _ := d.Content["POSTS"].([]any)
		for _, p := range posts {
			if o, ok := p.(map[string]any); ok {
				if pid, ok := o["id"].(string); ok {
					titles[pid] = postTitle(o)
				}
			}
		}
	}
	mu := blogLock(sc)
	mu.Lock()
	d := readBlog(sc)
	mu.Unlock()
	counts := map[string]int{}
	items := []map[string]any{}
	for i := len(d.Comments) - 1; i >= 0; i-- {
		c := d.Comments[i]
		counts[c.Status]++
		if status != "" && c.Status != status {
			continue
		}
		if len(items) >= 500 {
			continue
		}
		items = append(items, map[string]any{"id": c.ID, "post": c.Post, "postTitle": titles[c.Post], "text": c.Text,
			"time": c.Time, "status": c.Status, "by": personOf(ac.cfg, ac.ws.ID, c.By)})
	}
	adminJSON(w, http.StatusOK, map[string]any{"items": items, "counts": counts})
}

// handleComment approves or hides one comment. Ids are looked up in the
// caller's own site only: another site's id is not found.
func (a *Admin) handleComment(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if r.Method == http.MethodPatch {
		if code, e := decodeJSON(w, r, 1<<10, &body); code != 0 {
			adminJSON(w, code, apiError{Error: e})
			return
		}
		if body.Status != "approved" && body.Status != "hidden" && body.Status != "pending" {
			adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "status"})
			return
		}
	}
	id := r.PathValue("id")
	mu := blogLock(sc)
	mu.Lock()
	defer mu.Unlock()
	d := readBlog(sc)
	at := -1
	for i, c := range d.Comments {
		if c.ID == id {
			at = i
			break
		}
	}
	if at < 0 {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	kind := "comment_delete"
	if r.Method == http.MethodDelete {
		d.Comments = append(d.Comments[:at], d.Comments[at+1:]...)
	} else {
		d.Comments[at].Status, d.Comments[at].By = body.Status, ac.user.ID
		kind = "comment_" + body.Status
	}
	if err := writeBlog(sc, d); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: kind})
	adminJSON(w, http.StatusOK, map[string]any{"ok": true})
}
