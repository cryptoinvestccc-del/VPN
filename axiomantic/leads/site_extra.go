package main

// The site editor's tools around editing: the media library, the activity
// log, what changed since the site was published, publishing at a set
// time and telling search engines about new pages (IndexNow).

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------- activity

type activityEntry struct {
	Time    time.Time `json:"time"`
	By      string    `json:"by"`
	Kind    string    `json:"kind"` // save | publish | restore | upload | delete | schedule | unschedule | indexnow
	Section string    `json:"section,omitempty"`
	Release string    `json:"release,omitempty"`
	Media   string    `json:"media,omitempty"`
	Note    string    `json:"note,omitempty"`
}

const maxActivity = 2000

func activityPath(sc *SiteConfig) string { return filepath.Join(sc.Data, "activity.jsonl") }

// activity appends to the site's log. The log keeps the newest entries
// only; it is a record for people, not an audit trail.
func (a *Admin) activity(sc *SiteConfig, e activityEntry) {
	if e.Time.IsZero() {
		e.Time = a.now().UTC()
	}
	raw, _ := json.Marshal(e)
	if err := os.MkdirAll(sc.Data, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(activityPath(sc), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		a.log.Printf("activity: %v", err)
		return
	}
	_, _ = f.Write(append(raw, '\n'))
	st, _ := f.Stat()
	f.Close()
	if st != nil && st.Size() > 2<<20 {
		entries := readActivity(sc, maxActivity)
		var buf bytes.Buffer
		for i := len(entries) - 1; i >= 0; i-- {
			line, _ := json.Marshal(entries[i])
			buf.Write(append(line, '\n'))
		}
		_ = writeFileAtomic(activityPath(sc), buf.Bytes())
	}
}

// readActivity returns up to n entries, newest first.
func readActivity(sc *SiteConfig, n int) []activityEntry {
	f, err := os.Open(activityPath(sc))
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []activityEntry
	sc2 := bufio.NewScanner(f)
	sc2.Buffer(make([]byte, 64<<10), 64<<10)
	for sc2.Scan() {
		var e activityEntry
		if json.Unmarshal(sc2.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	out := []activityEntry{}
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out
}

func (a *Admin) handleSiteActivity(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 || n > 500 {
		n = 200
	}
	titles := map[string]string{}
	if schema, err := a.schemaOf(sc); err == nil {
		for _, s := range schema.Sections {
			titles[s.ID] = s.Title
		}
	}
	items := []map[string]any{}
	for _, e := range readActivity(sc, n) {
		items = append(items, map[string]any{
			"time": e.Time, "by": personOf(ac.cfg, ac.ws.ID, e.By), "kind": e.Kind, "section": e.Section,
			"sectionTitle": titles[e.Section], "release": e.Release, "media": e.Media, "note": e.Note,
		})
	}
	adminJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---------------------------------------------------------------- media library

var reUploadName = regexp.MustCompile(`[^\p{L}\p{N} ._()-]+`)

// uploadName keeps a readable file name for the library: letters, digits
// and a few marks, at most 80 characters.
func uploadName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(reUploadName.ReplaceAllString(name, ""))
	if !utf8.ValidString(name) || name == "." {
		return ""
	}
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	return name
}

// mediaUses lists where a picture is used: sections of the draft and kept
// releases.
func (a *Admin) mediaUses(ctx context.Context, sc *SiteConfig) (map[string][]string, map[string]bool) {
	draft := map[string][]string{}
	schema, err := a.schemaOf(sc)
	d, derr := a.loadDraft(ctx, sc)
	if err == nil && derr == nil {
		for _, s := range schema.Sections {
			seen := map[string]bool{}
			for _, f := range s.Fields {
				walkStrings(getPath(d.Content, f.Key), func(v string) {
					if reMediaID.MatchString(v) && !seen[v] {
						seen[v] = true
						draft[v] = append(draft[v], s.Title)
					}
				})
			}
		}
	}
	released := map[string]bool{}
	for _, m := range a.listReleases(sc) {
		if full, err := readRelease(sc, m.ID, true); err == nil {
			walkStrings(any(full.Content), func(v string) {
				if reMediaID.MatchString(v) {
					released[v] = true
				}
			})
		}
	}
	return draft, released
}

func walkStrings(v any, fn func(string)) {
	switch x := v.(type) {
	case string:
		fn(x)
	case []any:
		for _, y := range x {
			walkStrings(y, fn)
		}
	case map[string]any:
		for _, y := range x {
			walkStrings(y, fn)
		}
	}
}

func (a *Admin) handleMediaList(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	draft, released := a.mediaUses(r.Context(), sc)
	items := []map[string]any{}
	for id, m := range readMediaManifest(sc) {
		thumb := ""
		if len(m.JPG) > 0 {
			thumb = fmt.Sprintf("/admin/media/%s", m.JPG[0][1])
		}
		uses := draft[id]
		if uses == nil {
			uses = []string{}
		}
		var when any // pictures uploaded before the library have no date
		if !m.Time.IsZero() {
			when = m.Time
		}
		items = append(items, map[string]any{
			"id": id, "w": m.W, "h": m.H, "thumb": thumb, "icon": len(m.PNG) > 0, "name": m.Name,
			"time": when, "by": personOf(ac.cfg, ac.ws.ID, m.By), "uses": uses, "inRelease": released[id],
		})
	}
	sort.Slice(items, func(i, j int) bool {
		ti, _ := items[i]["time"].(time.Time)
		tj, _ := items[j]["time"].(time.Time)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return items[i]["id"].(string) < items[j]["id"].(string)
	})
	adminJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleMediaDelete removes a picture nobody uses: not the draft and not a
// kept release, which could be restored. Ids are looked up in the caller's
// own site only.
func (a *Admin) handleMediaDelete(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	id := r.PathValue("id")
	if !reMediaID.MatchString(id) {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	m := readMediaManifest(sc)
	entry, ok := m[id]
	if !ok {
		adminJSON(w, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	draft, released := a.mediaUses(r.Context(), sc)
	if len(draft[id]) > 0 || released[id] {
		where := draft[id]
		if where == nil {
			where = []string{}
		}
		adminJSON(w, http.StatusConflict, map[string]any{"error": "in_use", "where": where, "inRelease": released[id]})
		return
	}
	delete(m, id)
	raw, _ := json.Marshal(m)
	if err := writeFileAtomic(filepath.Join(sc.Data, "media", "manifest.json"), raw); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	for _, f := range append(append([][2]any{}, entry.JPG...), entry.PNG...) {
		if name, ok := f[1].(string); ok && reMediaFile.MatchString(name) {
			_ = os.Remove(filepath.Join(sc.Data, "media", name))
		}
	}
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: "delete", Media: id, Note: entry.Name})
	adminJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- diff

type change struct {
	Label  string `json:"label"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// leaves flattens a value into readable labels and short texts, following
// the schema so the labels are the editor's own.
func leaves(f *Field, label string, v any, out map[string]string, order *[]string) {
	put := func(l, s string) {
		if _, ok := out[l]; !ok {
			*order = append(*order, l)
		}
		out[l] = s
	}
	join := func(a, b string) string {
		switch {
		case a == "":
			return b
		case b == "":
			return a
		}
		return a + " · " + b
	}
	switch f.Type {
	case "object":
		m, _ := v.(map[string]any)
		for _, k := range f.Fields {
			if k.Hidden {
				continue
			}
			leaves(k, join(label, k.Label), m[k.Key], out, order)
		}
	case "tuple":
		arr, _ := v.([]any)
		for i, it := range f.Items {
			if it.Hidden {
				continue
			}
			var x any
			if i < len(arr) {
				x = arr[i]
			}
			leaves(it, join(label, it.Label), x, out, order)
		}
	case "list", "blocks":
		arr, _ := v.([]any)
		put(join(label, "количество"), strconv.Itoa(len(arr)))
		for i, x := range arr {
			name := "№" + strconv.Itoa(i+1)
			if t := itemTitle(f, x); t != "" {
				name += " «" + t + "»"
			}
			if f.Type == "blocks" {
				blk, _ := x.([]any)
				s := ""
				if len(blk) == 2 {
					s = short(blk[1])
				}
				put(join(label, "блок "+strconv.Itoa(i+1)), s)
				continue
			}
			if f.Item != nil {
				leaves(f.Item, join(label, name), x, out, order)
			}
		}
	case "map":
		m, _ := v.(map[string]any)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			leaves(f.Item, join(label, k), m[k], out, order)
		}
	case "image":
		s, _ := v.(string)
		if s == "" {
			put(label, "")
		} else {
			put(label, "картинка "+s[:6])
		}
	default:
		put(label, short(v))
	}
}

func itemTitle(f *Field, x any) string {
	switch t := f.ItemTitle.(type) {
	case float64:
		if arr, ok := x.([]any); ok && int(t) < len(arr) {
			return short(arr[int(t)])
		}
	case string:
		if m, ok := x.(map[string]any); ok {
			return short(m[t])
		}
	}
	return ""
}

func short(v any) string {
	var s string
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s = x
	case bool:
		if x {
			return "да"
		}
		return "нет"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := []string{}
		for _, y := range x {
			parts = append(parts, short(y))
		}
		s = strings.Join(parts, "; ")
	default:
		raw, _ := json.Marshal(x)
		s = string(raw)
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 140 {
		s = string(r[:139]) + "…"
	}
	return s
}

// sectionChanges compares one section of two contents.
func sectionChanges(sec *Section, before, after map[string]any) []change {
	b, a := map[string]string{}, map[string]string{}
	var order []string
	for _, f := range sec.Fields {
		var bv any
		if before != nil {
			bv = getPath(before, f.Key)
		}
		leaves(f, f.Label, bv, b, &order)
		leaves(f, f.Label, getPath(after, f.Key), a, &order)
	}
	out := []change{}
	seen := map[string]bool{}
	for _, l := range order {
		if seen[l] {
			continue
		}
		seen[l] = true
		if b[l] != a[l] {
			label := l
			if label == "" {
				label = sec.Title
			}
			out = append(out, change{Label: label, Before: b[l], After: a[l]})
		}
	}
	return out
}

func (a *Admin) handleDiff(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
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
	var pub map[string]any
	if id := currentRelease(sc); id != "" {
		if m, err := readRelease(sc, id, true); err == nil {
			pub = m.Content
		}
	}
	sections := []map[string]any{}
	for i := range schema.Sections {
		sec := &schema.Sections[i]
		ch := sectionChanges(sec, pub, d.Content)
		if len(ch) == 0 {
			continue
		}
		more := 0
		if len(ch) > 60 {
			more, ch = len(ch)-60, ch[:60]
		}
		sections = append(sections, map[string]any{"id": sec.ID, "title": sec.Title, "changes": ch, "more": more})
	}
	adminJSON(w, http.StatusOK, map[string]any{"published": pub != nil, "sections": sections})
}

// ---------------------------------------------------------------- schedule

type schedule struct {
	At   time.Time `json:"at"`
	By   string    `json:"by"`
	Note string    `json:"note"`
	Last string    `json:"last,omitempty"` // why the last scheduled run did not publish
}

func schedulePath(sc *SiteConfig) string { return filepath.Join(sc.Data, "schedule.json") }

func readSchedule(sc *SiteConfig) *schedule {
	raw, err := os.ReadFile(schedulePath(sc))
	if err != nil {
		return nil
	}
	var s schedule
	if json.Unmarshal(raw, &s) != nil || s.At.IsZero() {
		return nil
	}
	return &s
}

func scheduleView(ac *actor, s *schedule) map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{"at": s.At, "note": s.Note, "by": personOf(ac.cfg, ac.ws.ID, s.By)}
}

func (a *Admin) handleSchedule(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	var body struct {
		At   time.Time `json:"at"`
		Note string    `json:"note"`
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
	now := a.now()
	if body.At.Before(now.Add(time.Minute)) || body.At.After(now.Add(90*24*time.Hour)) {
		adminJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Field: "at"})
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	s := schedule{At: body.At.UTC().Truncate(time.Minute), By: ac.user.ID, Note: note}
	raw, _ := json.Marshal(s)
	if err := os.MkdirAll(sc.Data, 0o700); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	if err := writeFileAtomic(schedulePath(sc), raw); err != nil {
		adminJSON(w, http.StatusInternalServerError, apiError{Error: "store"})
		return
	}
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: "schedule", Note: s.At.Format(time.RFC3339)})
	adminJSON(w, http.StatusOK, map[string]any{"scheduled": scheduleView(ac, &s)})
}

func (a *Admin) handleUnschedule(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	siteLock.Lock()
	defer siteLock.Unlock()
	if readSchedule(sc) != nil {
		_ = os.Remove(schedulePath(sc))
		a.activity(sc, activityEntry{By: ac.user.ID, Kind: "unschedule"})
	}
	adminJSON(w, http.StatusOK, map[string]any{"scheduled": nil})
}

// RunSchedules publishes every site whose time has come. The person who
// set the time must still be an active manager or owner of that site;
// otherwise the plan is dropped and the log says why.
func (a *Admin) RunSchedules(ctx context.Context) {
	cfg := a.cfg.Get()
	for _, ws := range cfg.Workspaces {
		sc := ws.Site
		if sc == nil {
			continue
		}
		siteLock.Lock()
		s := readSchedule(sc)
		if s == nil || a.now().Before(s.At) {
			siteLock.Unlock()
			continue
		}
		_ = os.Remove(schedulePath(sc))
		u := cfg.User(s.By)
		if u == nil || u.Disabled || u.Workspace != ws.ID || roleRank[u.Role] < roleRank[roleManager] {
			a.activity(sc, activityEntry{By: s.By, Kind: "publish_failed", Note: "автор плана больше не может публиковать"})
			siteLock.Unlock()
			continue
		}
		if _, _, err := a.publishNow(ctx, ws, sc, s.By, s.Note); err != nil {
			a.log.Printf("site %s: scheduled publish: %v", ws.ID, err)
			a.activity(sc, activityEntry{By: s.By, Kind: "publish_failed", Note: "сборка не удалась"})
		}
		siteLock.Unlock()
	}
}

// StartScheduler checks the plans every half minute until ctx ends.
func (a *Admin) StartScheduler(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.RunSchedules(ctx)
			}
		}
	}()
}

// ---------------------------------------------------------------- IndexNow

// IndexNow tells Yandex (and through it Bing and others) which pages to
// crawl again. The site proves it owns the report with a key file at its
// root, written into every release.

type indexState struct {
	Time  time.Time `json:"time"`
	OK    bool      `json:"ok"`
	Count int       `json:"count"`
	Code  int       `json:"code,omitempty"`
	Error string    `json:"error,omitempty"`
}

var indexNowEndpoint = "https://yandex.com/indexnow"

// indexNowSend posts the report; tests replace it.
var indexNowSend = func(ctx context.Context, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, indexNowEndpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

var reIndexKey = regexp.MustCompile(`^[0-9a-f]{32}$`)

// indexNowKey is the site's key, made on first use; "" when the site asks
// not to be indexed or has no real address yet.
func (a *Admin) indexNowKey(sc *SiteConfig) string {
	d, err := a.loadDraft(context.Background(), sc)
	if err != nil || !indexable(d.Content) {
		return ""
	}
	p := filepath.Join(sc.Data, "indexnow.key")
	if raw, err := os.ReadFile(p); err == nil && reIndexKey.Match(bytes.TrimSpace(raw)) {
		return string(bytes.TrimSpace(raw))
	}
	key := hex.EncodeToString(randBytes(16))
	if err := writeFileAtomic(p, []byte(key)); err != nil {
		return ""
	}
	return key
}

// indexable: the site is open to search engines, IndexNow is on, and its
// address is a real public one.
func indexable(c map[string]any) bool {
	if v, ok := getPath(c, "SEO.index").(bool); ok && !v {
		return false
	}
	if v, ok := getPath(c, "SEO.indexnow").(bool); ok && !v {
		return false
	}
	host := siteHost(c)
	return host != "" && strings.Contains(host, ".") && !strings.HasSuffix(host, ".example") &&
		!strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".test") && host != "localhost"
}

func siteHost(c map[string]any) string {
	raw, _ := getPath(c, "SITE.url").(string)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	return u.Hostname()
}

var reLoc = regexp.MustCompile(`<loc>([^<]+)</loc>`)

func (a *Admin) submitIndexNow(sc *SiteConfig, content map[string]any, release, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := indexState{Time: a.now().UTC()}
	raw, err := os.ReadFile(filepath.Join(release, "sitemap.xml"))
	host := siteHost(content)
	var urls []string
	if err == nil {
		for _, m := range reLoc.FindAllSubmatch(raw, 10000) {
			u := strings.ReplaceAll(string(m[1]), "&amp;", "&")
			if pu, err := url.Parse(u); err == nil && pu.Hostname() == host {
				urls = append(urls, u)
			}
		}
	}
	if len(urls) == 0 {
		st.Error = "в карте сайта нет страниц этого адреса"
	} else {
		body, _ := json.Marshal(map[string]any{"host": host, "key": key, "keyLocation": "https://" + host + "/" + key + ".txt", "urlList": urls})
		code, err := indexNowSend(ctx, body)
		st.Code, st.Count = code, len(urls)
		switch {
		case err != nil:
			st.Error = "поисковик недоступен"
		case code == http.StatusOK || code == http.StatusAccepted:
			st.OK = true
		default:
			st.Error = "поисковик ответил " + strconv.Itoa(code)
		}
	}
	out, _ := json.Marshal(st)
	_ = writeFileAtomic(filepath.Join(sc.Data, "indexnow.json"), out)
}

func readIndexState(sc *SiteConfig) *indexState {
	raw, err := os.ReadFile(filepath.Join(sc.Data, "indexnow.json"))
	if err != nil {
		return nil
	}
	var st indexState
	if json.Unmarshal(raw, &st) != nil {
		return nil
	}
	return &st
}

func (a *Admin) handleIndexing(w http.ResponseWriter, r *http.Request, ac *actor) {
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
	open, _ := getPath(d.Content, "SEO.index").(bool)
	if getPath(d.Content, "SEO.index") == nil {
		open = true
	}
	adminJSON(w, http.StatusOK, map[string]any{
		"host": siteHost(d.Content), "open": open, "ready": indexable(d.Content),
		"published": currentRelease(sc) != "", "last": readIndexState(sc),
	})
}

// handleIndexNow sends the published site's pages again, on request.
func (a *Admin) handleIndexNow(w http.ResponseWriter, r *http.Request, ac *actor) {
	sc := siteOf(w, ac)
	if sc == nil {
		return
	}
	siteLock.Lock()
	id := currentRelease(sc)
	var content map[string]any
	if id != "" {
		if m, err := readRelease(sc, id, true); err == nil {
			content = m.Content
		}
	}
	key := ""
	if content != nil && indexable(content) {
		key = a.indexNowKey(sc)
	}
	siteLock.Unlock()
	if key == "" {
		adminJSON(w, http.StatusConflict, apiError{Error: "not_ready"})
		return
	}
	rel := filepath.Join(sc.Root, "releases", id)
	if _, err := os.Stat(filepath.Join(rel, key+".txt")); err != nil {
		// the release was published before the key: publish again first
		adminJSON(w, http.StatusConflict, apiError{Error: "republish"})
		return
	}
	a.submitIndexNow(sc, content, rel, key)
	a.activity(sc, activityEntry{By: ac.user.ID, Kind: "indexnow"})
	adminJSON(w, http.StatusOK, map[string]any{"last": readIndexState(sc)})
}
