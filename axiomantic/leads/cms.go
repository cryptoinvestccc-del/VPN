package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The site editor: a draft of the content, checked against the site's
// schema on every save, built into a preview on request and published as
// a new release. nginx serves root/current, a link to the release in use,
// so publishing and rolling back are one rename each and a visitor never
// sees a half-built site.

const (
	buildTimeout  = 3 * time.Minute
	maxBuildLog   = 64 << 10
	maxDraftBytes = 4 << 20
)

var reReleaseID = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

type draftFile struct {
	Version int            `json:"version"`
	Updated time.Time      `json:"updated"`
	By      string         `json:"by"`
	Content map[string]any `json:"content"`
}

type releaseMeta struct {
	ID      string         `json:"id"`
	Time    time.Time      `json:"time"`
	By      string         `json:"by"`
	Note    string         `json:"note"`
	Content map[string]any `json:"content,omitempty"`
}

type buildResult struct {
	OK      bool      `json:"ok"`
	Kind    string    `json:"kind"` // preview | publish
	Time    time.Time `json:"time"`
	Log     string    `json:"log"`
	Missing []string  `json:"missing"`
}

// siteLock serialises everything that writes a site: saves, builds,
// releases. Builds are rare and short; one at a time keeps it simple.
var siteLock sync.Mutex

func (a *Admin) siteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/site", a.auth(roleViewer, a.handleSite))
	mux.HandleFunc("PUT /api/admin/site/sections/{id}", a.auth(roleManager, a.handleSaveSection))
	mux.HandleFunc("POST /api/admin/site/preview", a.auth(roleManager, a.handlePreview))
	mux.HandleFunc("POST /api/admin/site/publish", a.auth(roleManager, a.handlePublish))
	mux.HandleFunc("GET /api/admin/site/releases", a.auth(roleViewer, a.handleReleases))
	mux.HandleFunc("POST /api/admin/site/releases/{id}/restore", a.auth(roleOwner, a.handleRestore))
	mux.HandleFunc("POST /api/admin/site/media", a.auth(roleManager, a.handleUpload))
	mux.HandleFunc("GET /api/admin/site/health", a.auth(roleViewer, a.handleHealth))
	mux.HandleFunc("GET /api/admin/site/media", a.auth(roleViewer, a.handleMediaList))
	mux.HandleFunc("DELETE /api/admin/site/media/{id}", a.auth(roleManager, a.handleMediaDelete))
	mux.HandleFunc("GET /api/admin/site/activity", a.auth(roleViewer, a.handleSiteActivity))
	mux.HandleFunc("GET /api/admin/site/diff", a.auth(roleViewer, a.handleDiff))
	mux.HandleFunc("PUT /api/admin/site/schedule", a.auth(roleManager, a.handleSchedule))
	mux.HandleFunc("DELETE /api/admin/site/schedule", a.auth(roleManager, a.handleUnschedule))
	mux.HandleFunc("GET /api/admin/site/indexing", a.auth(roleViewer, a.handleIndexing))
	mux.HandleFunc("POST /api/admin/site/indexing", a.auth(roleManager, a.handleIndexNow))
	mux.HandleFunc("GET /admin/preview/", a.authPage(a.servePreview))
	mux.HandleFunc("GET /admin/media/{name}", a.authPage(a.serveMedia))
}

// authPage is auth for pages opened in a browser tab: without a session
// it sends the person to the sign-in page instead of answering JSON.
func (a *Admin) authPage(next func(http.ResponseWriter, *http.Request, *actor)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &statusCatcher{ResponseWriter: w}
		a.auth(roleViewer, next)(rec, r)
	}
}

// statusCatcher turns the JSON 401 of auth into a redirect to sign-in.
type statusCatcher struct {
	http.ResponseWriter
	redirected bool
}

func (s *statusCatcher) WriteHeader(code int) {
	if code == http.StatusUnauthorized {
		s.ResponseWriter.Header().Set("Location", "/admin/")
		s.ResponseWriter.Header().Del("Content-Type")
		s.ResponseWriter.WriteHeader(http.StatusSeeOther)
		s.redirected = true
		return
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusCatcher) Write(b []byte) (int, error) {
	if s.redirected {
		return len(b), nil
	}
	return s.ResponseWriter.Write(b)
}

func siteOf(w http.ResponseWriter, ac *actor) *SiteConfig {
	if ac.ws.Site == nil {
		adminJSON(w, http.StatusNotFound, apiError{Error: "no_site"})
		return nil
	}
	return ac.ws.Site
}

func (a *Admin) schemaOf(sc *SiteConfig) (*Schema, error) { return LoadSchema(sc.Schema) }

func draftPath(sc *SiteConfig) string { return filepath.Join(sc.Data, "draft.json") }

// loadDraft reads the draft, making it on first use from what the
// generator holds now (its --export-content).
func (a *Admin) loadDraft(ctx context.Context, sc *SiteConfig) (*draftFile, error) {
	raw, err := os.ReadFile(draftPath(sc))
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(sc.Data, 0o700); err != nil {
			return nil, err
		}
		content, err := exportContent(ctx, sc)
		if err != nil {
			return nil, err
		}
		d := &draftFile{Version: 1, Updated: a.now().UTC(), Content: content}
		return d, a.saveDraft(sc, d)
	}
	if err != nil {
		return nil, err
	}
	var d draftFile
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("draft: %v", err)
	}
	if err := a.fillNewKeys(ctx, sc, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// exportContent asks the generator for everything its sources hold now.
func exportContent(ctx context.Context, sc *SiteConfig) (map[string]any, error) {
	tmp := filepath.Join(sc.Data, ".export.json")
	defer os.Remove(tmp)
	args := append(append([]string{}, sc.Builder[1:]...), "--export-content", tmp)
	cmd := exec.CommandContext(ctx, sc.Builder[0], args...)
	cmd.Env = buildEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("export content: %v: %s", err, trimLog(out))
	}
	raw, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	var content map[string]any
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, err
	}
	return content, nil
}

// fillNewKeys brings a draft up to a newer schema: a block or a field the
// site gained after the draft was made gets the generator's value, so the
// editor shows it filled in. What the draft has is never touched.
func (a *Admin) fillNewKeys(ctx context.Context, sc *SiteConfig, d *draftFile) error {
	schema, err := a.schemaOf(sc)
	if err != nil {
		return nil // the editor reports a broken schema itself
	}
	missing := func(src map[string]any, f *Field) bool {
		cur := getPath(src, f.Key)
		if cur == nil {
			return true
		}
		obj, ok := cur.(map[string]any)
		if !ok || f.Type != "object" {
			return false
		}
		for _, k := range f.Fields {
			if _, has := obj[k.Key]; !has {
				return true
			}
		}
		return false
	}
	need := false
	for _, sec := range schema.Sections {
		for _, f := range sec.Fields {
			need = need || missing(d.Content, f)
		}
	}
	if !need {
		return nil
	}
	exp, err := exportContent(ctx, sc)
	if err != nil {
		return err
	}
	changed := false
	for _, sec := range schema.Sections {
		for _, f := range sec.Fields {
			cur, def := getPath(d.Content, f.Key), getPath(exp, f.Key)
			if def == nil {
				continue
			}
			if cur == nil {
				setPath(d.Content, f.Key, def)
				changed = true
				continue
			}
			obj, ok1 := cur.(map[string]any)
			defObj, ok2 := def.(map[string]any)
			if !ok1 || !ok2 || f.Type != "object" {
				continue
			}
			for _, k := range f.Fields {
				if _, has := obj[k.Key]; !has {
					if v, ok := defObj[k.Key]; ok {
						obj[k.Key] = v
						changed = true
					}
				}
			}
		}
	}
	if !changed {
		return nil
	}
	return a.saveDraft(sc, d)
}

func (a *Admin) saveDraft(sc *SiteConfig, d *draftFile) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return writeFileAtomic(draftPath(sc), raw)
}

// currentRelease is the release root/current points at, or "".
func currentRelease(sc *SiteConfig) string {
	target, err := os.Readlink(filepath.Join(sc.Root, "current"))
	if err != nil {
		return ""
	}
	id := filepath.Base(target)
	if !reReleaseID.MatchString(id) {
		return ""
	}
	return id
}

func readRelease(sc *SiteConfig, id string, withContent bool) (*releaseMeta, error) {
	raw, err := os.ReadFile(filepath.Join(sc.Data, "releases", id+".json"))
	if err != nil {
		return nil, err
	}
	var m releaseMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if !withContent {
		m.Content = nil
	}
	return &m, nil
}

// changedSections lists the sections whose values differ from what is
// published.
func changedSections(s *Schema, draft, pub map[string]any) []string {
	out := []string{}
	for _, sec := range s.Sections {
		for _, f := range sec.Fields {
			if pub == nil || !jsonEqual(getPath(draft, f.Key), getPath(pub, f.Key)) {
				out = append(out, sec.ID)
				break
			}
		}
	}
	return out
}

func (a *Admin) siteState(ctx context.Context, ac *actor, sc *SiteConfig, withSchema bool) (map[string]any, error) {
	schema, err := a.schemaOf(sc)
	if err != nil {
		return nil, err
	}
	d, err := a.loadDraft(ctx, sc)
	if err != nil {
		return nil, err
	}
	var pub map[string]any
	var cur *releaseMeta
	if id := currentRelease(sc); id != "" {
		if m, err := readRelease(sc, id, true); err == nil {
			pub = m.Content
			m.Content = nil
			cur = m
		}
	}
	changed := changedSections(schema, d.Content, pub)
	state := map[string]any{
		"enabled": true, "content": d.Content, "version": d.Version, "updated": d.Updated,
		"by": personOf(ac.cfg, ac.ws.ID, d.By), "current": releaseView(ac, cur), "changed": changed,
		"dirty": len(changed) > 0, "lastBuild": a.lastBuild(sc), "scheduled": scheduleView(ac, readSchedule(sc)),
	}
	if withSchema {
		state["schema"] = schema
	}
	return state, nil
}

func releaseView(ac *actor, m *releaseMeta) map[string]any {
	if m == nil {
		return nil
	}
	return map[string]any{"id": m.ID, "time": m.Time, "note": m.Note, "by": personOf(ac.cfg, ac.ws.ID, m.By)}
}

func (a *Admin) handleSite(w http.ResponseWriter, r *http.Request, ac *actor) {
	if ac.ws.Site == nil {
		adminJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	st, err := a.siteState(r.Context(), ac, ac.ws.Site, true)
	if err != nil {
		a.log.Printf("site %s: %v", ac.ws.ID, err)
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	adminJSON(w, http.StatusOK, st)
}

func (a *Admin) mediaExists(sc *SiteConfig) func(string) bool {
	m := readMediaManifest(sc)
	return func(id string) bool { _, ok := m[id]; return ok }
}

func (a *Admin) handleSaveSection(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	var body struct {
		Version int            `json:"version"`
		Values  map[string]any `json:"values"`
	}
	if code, e := decodeJSON(w, r, maxDraftBytes, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	schema, err := a.schemaOf(sc)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "schema"})
		return
	}
	d, err := a.loadDraft(r.Context(), sc)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	if body.Version != d.Version {
		// someone else saved in between: the editor reloads and merges
		adminJSON(w, http.StatusConflict, map[string]any{"error": "conflict", "version": d.Version})
		return
	}
	next, err := schema.ApplySection(d.Content, r.PathValue("id"), body.Values, a.mediaExists(sc))
	if err != nil {
		var fe *FieldError
		if errors.As(err, &fe) {
			adminJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid", "field": fe.Path, "message": fe.Message})
			return
		}
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid"})
		return
	}
	d.Content, d.Version, d.Updated, d.By = next, d.Version+1, a.now().UTC(), ac.user.ID
	if err := a.saveDraft(sc, d); err != nil {
		a.log.Printf("site %s: save draft: %v", ac.ws.ID, err)
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: "save", Section: r.PathValue("id")})
	st, err := a.siteState(r.Context(), ac, sc, false)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	adminJSON(w, http.StatusOK, st)
}

// ---------------------------------------------------------------- builds

// buildEnv passes the generator only what it needs; nothing of the
// service's own environment (the bot token lives there) reaches it.
func buildEnv(extra map[string]string) []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "PYTHONDONTWRITEBYTECODE=1", "LANG=C.UTF-8"}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func trimLog(b []byte) string {
	if len(b) > maxBuildLog {
		b = b[len(b)-maxBuildLog:]
	}
	return strings.TrimSpace(string(b))
}

// copyTree copies a built site to start a build from. Files are made
// readable by the web server whatever the service's umask.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			return os.Chmod(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil // links and devices are not part of a site
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		if err := out.Chmod(0o644); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func publicModes(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o755)
		}
		if d.Type().IsRegular() {
			return os.Chmod(p, 0o644)
		}
		return nil
	})
}

var reMissing = regexp.MustCompile(`pictures not rendered yet: (.*)`)

// build turns content into a site in out (which must not exist yet).
func (a *Admin) build(ctx context.Context, sc *SiteConfig, content map[string]any, out, kind string) (*buildResult, error) {
	res := &buildResult{Kind: kind, Time: a.now().UTC(), Missing: []string{}}
	snap := filepath.Join(sc.Data, ".build-content.json")
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(snap, raw); err != nil {
		return nil, err
	}
	defer os.Remove(snap)
	if err := copyTree(sc.Template, out); err != nil {
		return nil, fmt.Errorf("copy template: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sc.Builder[0], sc.Builder[1:]...)
	cmd.Env = buildEnv(map[string]string{
		"AXM_CONTENT": snap, "AXM_OUT": out, "AXM_MEDIA": filepath.Join(sc.Data, "media"), "AXM_ALLOW_MISSING": "1",
	})
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	runErr := cmd.Run()
	res.Log = trimLog(buf.Bytes())
	if m := reMissing.FindStringSubmatch(res.Log); m != nil {
		for _, x := range strings.Split(m[1], ",") {
			if x = strings.TrimSpace(x); x != "" {
				res.Missing = append(res.Missing, x)
			}
		}
	}
	if runErr == nil {
		runErr = publicModes(out)
	}
	res.OK = runErr == nil
	a.saveLastBuild(sc, res)
	if runErr != nil {
		return res, fmt.Errorf("build: %v", runErr)
	}
	return res, nil
}

func (a *Admin) saveLastBuild(sc *SiteConfig, res *buildResult) {
	raw, _ := json.Marshal(res)
	_ = writeFileAtomic(filepath.Join(sc.Data, "last-build.json"), raw)
}

func (a *Admin) lastBuild(sc *SiteConfig) *buildResult {
	raw, err := os.ReadFile(filepath.Join(sc.Data, "last-build.json"))
	if err != nil {
		return nil
	}
	var res buildResult
	if json.Unmarshal(raw, &res) != nil {
		return nil
	}
	return &res
}

func newReleaseID(now time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// swapDir puts a freshly built folder in place of dst in one rename,
// then removes what was there.
func swapDir(fresh, dst string) error {
	old := dst + ".old-" + hex.EncodeToString(randBytes(4))
	if err := os.Rename(dst, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(fresh, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	return os.RemoveAll(old)
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func (a *Admin) handlePreview(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	d, err := a.loadDraft(r.Context(), sc)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	if err := os.MkdirAll(sc.Root, 0o755); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	tmp := filepath.Join(sc.Root, ".preview-"+hex.EncodeToString(randBytes(4)))
	defer os.RemoveAll(tmp)
	// Forms in a preview must not send real leads: an empty endpoint makes
	// them only pretend to.
	content := deepCopy(d.Content).(map[string]any)
	setPath(content, "SITE.endpoint", "")
	res, err := a.build(r.Context(), sc, content, tmp, "preview")
	if err != nil {
		a.log.Printf("site %s: preview: %v", ac.ws.ID, err)
		adminJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "build", "build": res})
		return
	}
	if err := swapDir(tmp, filepath.Join(sc.Root, "preview")); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	adminJSON(w, http.StatusOK, map[string]any{"ok": true, "url": "/admin/preview/", "build": res})
}

func (a *Admin) handlePublish(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if code, e := decodeJSON(w, r, 4<<10, &body); code != 0 {
		adminJSON(w, code, apiError{Error: e})
		return
	}
	note := strings.TrimSpace(body.Note)
	if !between(note, 0, 200) || !textOK(note, false) {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "note"})
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	meta, res, err := a.publishNow(r.Context(), ac.ws, sc, ac.user.ID, note)
	if err != nil {
		a.log.Printf("site %s: publish: %v", ac.ws.ID, err)
		if res != nil {
			adminJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "build", "build": res})
			return
		}
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	adminJSON(w, http.StatusOK, map[string]any{"ok": true, "release": releaseView(ac, meta), "build": res})
}

// publishNow builds the draft into a new release and puts it on the site.
// The caller holds siteLock. A failed build returns its result with the
// error; anything else returns a nil result.
func (a *Admin) publishNow(ctx context.Context, ws *Workspace, sc *SiteConfig, by, note string) (*releaseMeta, *buildResult, error) {
	d, err := a.loadDraft(ctx, sc)
	if err != nil {
		return nil, nil, err
	}
	id := newReleaseID(a.now())
	rels := filepath.Join(sc.Root, "releases")
	if err := os.MkdirAll(rels, 0o755); err != nil {
		return nil, nil, err
	}
	tmp := filepath.Join(rels, ".building-"+id)
	defer os.RemoveAll(tmp)
	res, err := a.build(ctx, sc, d.Content, tmp, "publish")
	if err != nil {
		if res == nil {
			res = &buildResult{Kind: "publish", Time: a.now().UTC()}
		}
		return nil, res, err
	}
	key := a.indexNowKey(sc)
	if key != "" {
		// search engines check that the site holds the key it reports with
		if err := os.WriteFile(filepath.Join(tmp, key+".txt"), []byte(key), 0o644); err != nil {
			return nil, nil, err
		}
	}
	if err := os.Rename(tmp, filepath.Join(rels, id)); err != nil {
		return nil, nil, err
	}
	meta := releaseMeta{ID: id, Time: a.now().UTC(), By: by, Note: note, Content: d.Content}
	raw, _ := json.Marshal(meta)
	if err := os.MkdirAll(filepath.Join(sc.Data, "releases"), 0o700); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(filepath.Join(sc.Data, "releases", id+".json"), raw); err != nil {
		return nil, nil, err
	}
	if err := switchCurrent(sc, id); err != nil {
		return nil, nil, err
	}
	a.pruneReleases(sc)
	a.log.Printf("site %s: release %s published by %s", ws.ID, id, by)
	a.activity(sc, activityEntry{By: by, Kind: "publish", Note: note, Release: id})
	if key != "" {
		a.bg.Add(1)
		go func() {
			defer a.bg.Done()
			a.submitIndexNow(sc, d.Content, filepath.Join(rels, id), key)
		}()
	}
	meta.Content = nil
	return &meta, res, nil
}

// switchCurrent points root/current at a release with one rename, so the
// web server sees either the old site or the new one.
func switchCurrent(sc *SiteConfig, id string) error {
	cur := filepath.Join(sc.Root, "current")
	if st, err := os.Lstat(cur); err == nil && st.Mode()&fs.ModeSymlink == 0 {
		return fmt.Errorf("%s is a folder, not a link: move it away once", cur)
	}
	tmp := filepath.Join(sc.Root, ".current-"+hex.EncodeToString(randBytes(4)))
	if err := os.Symlink(path.Join("releases", id), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, cur); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (a *Admin) listReleases(sc *SiteConfig) []*releaseMeta {
	entries, _ := os.ReadDir(filepath.Join(sc.Data, "releases"))
	var out []*releaseMeta
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if !reReleaseID.MatchString(id) {
			continue
		}
		if _, err := os.Stat(filepath.Join(sc.Root, "releases", id)); err != nil {
			continue // its files are gone
		}
		if m, err := readRelease(sc, id, false); err == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// pruneReleases keeps the newest releases and always the current one.
func (a *Admin) pruneReleases(sc *SiteConfig) {
	cur := currentRelease(sc)
	for i, m := range a.listReleases(sc) {
		if i < sc.Keep || m.ID == cur {
			continue
		}
		_ = os.RemoveAll(filepath.Join(sc.Root, "releases", m.ID))
		_ = os.Remove(filepath.Join(sc.Data, "releases", m.ID+".json"))
	}
}

func (a *Admin) handleReleases(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	cur := currentRelease(sc)
	items := []map[string]any{}
	for _, m := range a.listReleases(sc) {
		v := releaseView(ac, m)
		v["current"] = m.ID == cur
		items = append(items, v)
	}
	adminJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleRestore puts an earlier release back on the site and makes its
// content the draft again. Release ids are looked up in the caller's own
// workspace only.
func (a *Admin) handleRestore(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	id := r.PathValue("id")
	if !reReleaseID.MatchString(id) {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	m, err := readRelease(sc, id, true)
	if err != nil {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	if _, err := os.Stat(filepath.Join(sc.Root, "releases", id)); err != nil {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	if err := switchCurrent(sc, id); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "switch"})
		return
	}
	d, err := a.loadDraft(r.Context(), sc)
	if err == nil {
		d.Content, d.Version, d.Updated, d.By = m.Content, d.Version+1, a.now().UTC(), ac.user.ID
		err = a.saveDraft(sc, d)
	}
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.log.Printf("site %s: release %s restored by %s", ac.ws.ID, id, ac.user.ID)
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: "restore", Release: id})
	st, err := a.siteState(r.Context(), ac, sc, false)
	if err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "site"})
		return
	}
	adminJSON(w, http.StatusOK, st)
}

// ---------------------------------------------------------------- pages

var siteTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".svg": "image/svg+xml", ".woff2": "font/woff2", ".avif": "image/avif", ".webp": "image/webp", ".jpg": "image/jpeg",
	".png": "image/png", ".mp4": "video/mp4", ".webm": "video/webm", ".xml": "application/xml", ".txt": "text/plain; charset=utf-8",
	".json": "application/json", ".ico": "image/x-icon",
}

// servePreview shows the last preview build to signed-in members only. The
// pages get the site's own CSP and are never indexed or framed elsewhere.
func (a *Admin) servePreview(w http.ResponseWriter, r *http.Request, ac *actor) {
	if ac.ws.Site == nil {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/admin/preview/")
	if p == "" || strings.HasSuffix(p, "/") {
		p += "index.html"
	}
	if !fs.ValidPath(p) {
		http.NotFound(w, r)
		return
	}
	root := os.DirFS(filepath.Join(ac.ws.Site.Root, "preview"))
	if st, err := fs.Stat(root, p); err == nil && st.IsDir() {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
		return
	}
	data, err := fs.ReadFile(root, p)
	ct := siteTypes[path.Ext(p)]
	if err != nil || ct == "" {
		http.NotFound(w, r)
		return
	}
	setPageHeaders(w)
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(data)
}

var reMediaFile = regexp.MustCompile(`^[0-9a-f]{16}-[0-9]{2,4}\.(jpg|png)$`)

func (a *Admin) serveMedia(w http.ResponseWriter, r *http.Request, ac *actor) {
	name := r.PathValue("name")
	if ac.ws.Site == nil || !reMediaFile.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(ac.ws.Site.Data, "media", name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	setAdminPageHeaders(w)
	if strings.HasSuffix(name, ".png") {
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}
