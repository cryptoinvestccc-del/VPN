package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// siteHarness is the admin harness with a real site behind alpha (and a
// separate one behind beta): the repository's build.py and schema.
func newSiteHarness(t *testing.T) *adminHarness {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed to build the site")
	}
	repo, _ := filepath.Abs("..")
	h := newAdminHarness(t)
	cfg, _ := LoadAdminConfig(h.cfgPath)
	for _, w := range cfg.Workspaces {
		dir := t.TempDir()
		w.Site = &SiteConfig{Builder: []string{py, filepath.Join(repo, "build.py")}, Template: filepath.Join(repo, "site"),
			Root: filepath.Join(dir, "www"), Data: filepath.Join(dir, "data"), Schema: filepath.Join(repo, "cms", "schema.json"), Keep: 3}
	}
	if err := SaveAdminConfig(h.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	h.clock.Add(2 * time.Second)
	h.start()
	return h
}

func (h *adminHarness) site(ws string) *SiteConfig {
	return h.srv.admin.cfg.Get().Workspace(ws).Site
}

type siteResp struct {
	Enabled bool           `json:"enabled"`
	Content map[string]any `json:"content"`
	Version int            `json:"version"`
	Changed []string       `json:"changed"`
	Dirty   bool           `json:"dirty"`
	Current map[string]any `json:"current"`
}

func (c *client) save(section string, version int, values map[string]any) *httptest.ResponseRecorder {
	return c.do("PUT", "/api/admin/site/sections/"+section, map[string]any{"version": version, "values": values})
}

func TestSiteEditPublishRestore(t *testing.T) {
	h := newSiteHarness(t)
	owner := h.login("owner@alpha.example")
	st := decode[siteResp](t, owner.do("GET", "/api/admin/site", nil))
	if !st.Enabled || st.Version != 1 || len(st.Changed) == 0 || st.Content["HERO"] == nil {
		t.Fatalf("first state %+v", st.Changed)
	}
	hero := st.Content["HERO"].(map[string]any)
	hero["lines"] = []any{"Новая строка", "второй строкой."}
	rec := owner.save("hero", st.Version, map[string]any{"HERO": hero})
	if rec.Code != http.StatusOK {
		t.Fatalf("save hero: %d %s", rec.Code, rec.Body)
	}
	if rec := owner.save("hero", st.Version, map[string]any{"HERO": hero}); rec.Code != http.StatusConflict {
		t.Fatalf("stale version: %d", rec.Code)
	}
	st = decode[siteResp](t, rec)
	// preview: built, forms inert, behind the session
	pv := owner.do("POST", "/api/admin/site/preview", map[string]any{})
	if pv.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", pv.Code, pv.Body)
	}
	page := owner.do("GET", "/admin/preview/", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Новая строка") || !strings.Contains(page.Body.String(), `data-endpoint=""`) {
		t.Fatalf("preview page: %d", page.Code)
	}
	anon := &client{h: h, ip: "198.51.100.9"}
	if rec := anon.do("GET", "/admin/preview/", nil); rec.Code != http.StatusSeeOther || strings.Contains(rec.Body.String(), "Новая строка") {
		t.Fatalf("preview without a session: %d", rec.Code)
	}
	for _, p := range []string{"/admin/preview/../draft.json", "/admin/preview/%2e%2e/data/draft.json"} {
		if rec := owner.do("GET", p, nil); rec.Code == http.StatusOK {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
	// publish
	pub := owner.do("POST", "/api/admin/site/publish", map[string]any{"note": "новый заголовок"})
	if pub.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", pub.Code, pub.Body)
	}
	sc := h.site("alpha")
	first := currentRelease(sc)
	idx, err := os.ReadFile(filepath.Join(sc.Root, "current", "index.html"))
	if err != nil || !strings.Contains(string(idx), "Новая строка") || strings.Contains(string(idx), `data-endpoint=""`) {
		t.Fatalf("published page: %v", err)
	}
	if st, _ := os.Stat(filepath.Join(sc.Root, "current", "index.html")); st.Mode().Perm() != 0o644 {
		t.Fatalf("published file mode %v: the web server could not read it", st.Mode().Perm())
	}
	st = decode[siteResp](t, owner.do("GET", "/api/admin/site", nil))
	if st.Dirty || len(st.Changed) != 0 {
		t.Fatalf("after publish still dirty: %v", st.Changed)
	}
	// second release, then roll back to the first
	h.clock.Add(2 * time.Second)
	hero["lines"] = []any{"Третья версия", "заголовка."}
	owner.save("hero", st.Version, map[string]any{"HERO": hero})
	owner.do("POST", "/api/admin/site/publish", map[string]any{"note": ""})
	if currentRelease(sc) == first {
		t.Fatal("second publish did not switch")
	}
	rec = owner.do("POST", "/api/admin/site/releases/"+first+"/restore", map[string]any{})
	if rec.Code != http.StatusOK || currentRelease(sc) != first {
		t.Fatalf("restore: %d", rec.Code)
	}
	st = decode[siteResp](t, rec)
	if st.Content["HERO"].(map[string]any)["lines"].([]any)[0] != "Новая строка" {
		t.Fatal("restore did not bring the draft back")
	}
	// old releases are pruned to Keep
	for i := 0; i < 4; i++ {
		h.clock.Add(2 * time.Second)
		owner.do("POST", "/api/admin/site/publish", map[string]any{"note": ""})
	}
	if n := len(h.srv.admin.listReleases(sc)); n > sc.Keep {
		t.Fatalf("%d releases kept, limit %d", n, sc.Keep)
	}
}

func TestSiteValidation(t *testing.T) {
	h := newSiteHarness(t)
	owner := h.login("owner@alpha.example")
	st := decode[siteResp](t, owner.do("GET", "/api/admin/site", nil))
	c := st.Content
	cp := func(v any) any { return deepCopy(v) }
	services := cp(c["SERVICES"]).([]any)
	services[0].([]any)[0] = "hacked"
	cases := cp(c["CASES"]).([]any)
	badSlug := cp(cases).([]any)
	badSlug[0].(map[string]any)["slug"] = "../../etc"
	badColor := cp(cases).([]any)
	badColor[0].(map[string]any)["colors"] = []any{"red;}body{display:none", "#FFFFFF", "#FFFFFF"}
	dup := cp(cases).([]any)
	dup[1].(map[string]any)["slug"] = dup[0].(map[string]any)["slug"]
	reviews := cp(c["REVIEWS"]).([]any)
	reviews[0].([]any)[2] = "no-such-case"
	withoutReferenced := []any{}
	for _, x := range cp(cases).([]any) {
		if x.(map[string]any)["slug"] != reviews[1].([]any)[2] {
			withoutReferenced = append(withoutReferenced, x)
		}
	}
	extra := cp(c["HERO"]).(map[string]any)
	extra["script"] = "<script>"
	bidi := cp(c["HERO"]).(map[string]any)
	bidi["lead"] = "текст‮перевёрнут"
	site := cp(c["SITE"]).(map[string]any)
	site["map_ll"] = "37.6&z=1&pt=evil"
	tariffs := cp(c["TARIFFS"]).([]any)
	tariffs = tariffs[:2]
	cases2 := []struct {
		name, section string
		values        map[string]any
	}{
		{"read-only key", "services", map[string]any{"SERVICES": services}},
		{"slug outside the pattern", "cases", map[string]any{"CASES": badSlug}},
		{"colour that is CSS", "cases", map[string]any{"CASES": badColor}},
		{"repeated slug", "cases", map[string]any{"CASES": dup}},
		{"review of a missing case", "reviews", map[string]any{"REVIEWS": reviews}},
		{"case a review points at removed", "cases", map[string]any{"CASES": withoutReferenced}},
		{"unknown field", "hero", map[string]any{"HERO": extra}},
		{"bidi override", "hero", map[string]any{"HERO": bidi}},
		{"map parameters smuggled", "site", map[string]any{"SITE": site}},
		{"fixed list shortened", "tariffs", map[string]any{"TARIFFS": tariffs}},
		{"field of another section", "hero", map[string]any{"FAQ": c["FAQ"]}},
		{"no such section", "nope", map[string]any{}},
	}
	for _, tc := range cases2 {
		rec := owner.save(tc.section, st.Version, tc.values)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}
	after := decode[siteResp](t, owner.do("GET", "/api/admin/site", nil))
	if after.Version != st.Version {
		t.Fatal("a refused save changed the draft")
	}
	// operator lives on SITE but is edited in «legal»: saving «site» keeps it
	rec := owner.save("legal", after.Version, map[string]any{"SITE.operator": "ИП Тестов Т. Т."})
	if rec.Code != http.StatusOK {
		t.Fatalf("legal: %d %s", rec.Code, rec.Body)
	}
	v := decode[siteResp](t, rec)
	if rec := owner.save("site", v.Version, map[string]any{"SITE": c["SITE"]}); rec.Code != http.StatusOK {
		t.Fatalf("site: %d %s", rec.Code, rec.Body)
	}
	got := decode[siteResp](t, owner.do("GET", "/api/admin/site", nil))
	if got.Content["SITE"].(map[string]any)["operator"] != "ИП Тестов Т. Т." {
		t.Fatal("saving one section dropped another section's field")
	}
}

func TestSiteRolesAndWorkspaces(t *testing.T) {
	h := newSiteHarness(t)
	viewer := h.login("viewer@alpha.example")
	manager := h.login("manager@alpha.example")
	owner := h.login("owner@alpha.example")
	beta := h.login("owner@beta.example")
	st := decode[siteResp](t, viewer.do("GET", "/api/admin/site", nil))
	if rec := viewer.save("faq", st.Version, map[string]any{"FAQ": st.Content["FAQ"]}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer save: %d", rec.Code)
	}
	if rec := viewer.do("POST", "/api/admin/site/publish", map[string]any{}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer publish: %d", rec.Code)
	}
	if rec := manager.save("faq", st.Version, map[string]any{"FAQ": st.Content["FAQ"]}); rec.Code != http.StatusOK {
		t.Fatalf("manager save: %d %s", rec.Code, rec.Body)
	}
	owner.do("POST", "/api/admin/site/publish", map[string]any{})
	id := currentRelease(h.site("alpha"))
	if rec := manager.do("POST", "/api/admin/site/releases/"+id+"/restore", map[string]any{}); rec.Code != http.StatusForbidden {
		t.Fatalf("manager restore: %d", rec.Code)
	}
	// beta's owner cannot reach alpha's release, draft or preview
	if rec := beta.do("POST", "/api/admin/site/releases/"+id+"/restore", map[string]any{}); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign release: %d", rec.Code)
	}
	bst := decode[siteResp](t, beta.do("GET", "/api/admin/site", nil))
	if bst.Version != 1 || bst.Current != nil {
		t.Fatalf("beta sees alpha's site state: %+v", bst.Current)
	}
	if strings.Contains(beta.do("GET", "/api/admin/site/releases", nil).Body.String(), id) {
		t.Fatal("beta lists alpha's release")
	}
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (c *client) upload(name string, data []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req := httptest.NewRequest("POST", "https://admin.example/api/admin/site/media", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	req.Header.Set("Origin", "https://admin.example")
	req.Header.Set("X-CSRF-Token", c.csrf)
	req.RemoteAddr = c.ip + ":4000"
	return c.h.serve(req)
}

func TestSiteMediaCover(t *testing.T) {
	h := newSiteHarness(t)
	owner := h.login("owner@alpha.example")
	rec := owner.upload("cover.png", pngBytes(1200, 800))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var up struct {
		ID    string `json:"id"`
		W, H  int
		Thumb string `json:"thumb"`
	}
	json.Unmarshal(rec.Body.Bytes(), &up)
	if up.W != 1200 || up.H != 800 || owner.do("GET", up.Thumb, nil).Code != http.StatusOK {
		t.Fatalf("upload result %+v", up)
	}
	for name, data := range map[string][]byte{
		"not an image":   []byte("<?php system($_GET[1]);"),
		"svg":            []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
		"tiny":           pngBytes(4, 4),
		"huge in header": hugePNGHeader(),
	} {
		if rec := owner.upload("x.png", data); rec.Code == http.StatusOK {
			t.Fatalf("%s accepted", name)
		}
	}
	st := decode[siteResp](t, owner.do("GET", "/api/admin/site", nil))
	cases := deepCopy(st.Content["CASES"]).([]any)
	cases[0].(map[string]any)["cover"] = up.ID
	if rec := owner.save("cases", st.Version, map[string]any{"CASES": cases}); rec.Code != http.StatusOK {
		t.Fatalf("save cover: %d %s", rec.Code, rec.Body)
	}
	cases[0].(map[string]any)["cover"] = "0123456789abcdef"
	if rec := owner.save("cases", st.Version+1, map[string]any{"CASES": cases}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown picture accepted: %d", rec.Code)
	}
	if rec := owner.do("POST", "/api/admin/site/publish", map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body)
	}
	slug := cases[0].(map[string]any)["slug"].(string)
	page, _ := os.ReadFile(filepath.Join(h.site("alpha").Root, "current", "projects", slug, "index.html"))
	if !strings.Contains(string(page), "assets/media/"+up.ID+"-") {
		t.Fatal("case page does not use the uploaded cover")
	}
	if _, err := os.Stat(filepath.Join(h.site("alpha").Root, "current", "assets", "media", up.ID+"-480.jpg")); err != nil {
		t.Fatal("cover file not published")
	}
	// another workspace cannot read alpha's uploads
	beta := h.login("owner@beta.example")
	if rec := beta.do("GET", up.Thumb, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("beta reads alpha's upload: %d", rec.Code)
	}
}

// a PNG header that claims 60000×60000 pixels
func hugePNGHeader() []byte {
	b := pngBytes(16, 16)
	b[16], b[17], b[18], b[19] = 0, 0, 0xEA, 0x60
	b[20], b[21], b[22], b[23] = 0, 0, 0xEA, 0x60
	return b
}

func TestTeamManagement(t *testing.T) {
	h := newAdminHarness(t)
	owner := h.login("owner@alpha.example")
	manager := h.login("manager@alpha.example")
	rec := owner.do("POST", "/api/admin/users", map[string]string{"login": "new@alpha.example", "name": "Новый Сотрудник", "role": "manager"})
	if rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	var added struct {
		User     map[string]any `json:"user"`
		Password string         `json:"password"`
	}
	json.Unmarshal(rec.Body.Bytes(), &added)
	h.clock.Add(time.Second)
	if r := h.loginReq("new@alpha.example", added.Password, "198.51.100.80"); r.Code != http.StatusOK {
		t.Fatalf("new user cannot sign in: %d", r.Code)
	}
	if r := owner.do("POST", "/api/admin/users", map[string]string{"login": "OWNER@beta.example", "name": "Дубль", "role": "viewer"}); r.Code != http.StatusConflict {
		t.Fatalf("login taken in another workspace: %d", r.Code)
	}
	if r := manager.do("POST", "/api/admin/users", map[string]string{"login": "x@alpha.example", "name": "X", "role": "owner"}); r.Code != http.StatusForbidden {
		t.Fatalf("manager adds users: %d", r.Code)
	}
	betaOwner := h.ids["owner@beta.example"]
	for _, r := range []*httptest.ResponseRecorder{
		owner.do("PATCH", "/api/admin/users/"+betaOwner, map[string]any{"disabled": true}),
		owner.do("PATCH", "/api/admin/users/"+betaOwner, map[string]any{"resetPassword": true}),
		owner.do("DELETE", "/api/admin/users/"+betaOwner, nil),
	} {
		if r.Code != http.StatusNotFound || strings.Contains(r.Body.String(), "password") {
			t.Fatalf("foreign user: %d %s", r.Code, r.Body)
		}
	}
	cfg, _ := LoadAdminConfig(h.cfgPath)
	if u := cfg.User(betaOwner); u == nil || u.Disabled {
		t.Fatal("beta's owner was changed from alpha")
	}
	self := h.ids["owner@alpha.example"]
	if r := owner.do("PATCH", "/api/admin/users/"+self, map[string]any{"disabled": true}); r.Code != http.StatusBadRequest {
		t.Fatalf("owner disabled themself: %d", r.Code)
	}
	if r := owner.do("PATCH", "/api/admin/users/"+self, map[string]any{"role": "viewer"}); r.Code != http.StatusBadRequest {
		t.Fatalf("owner demoted themself: %d", r.Code)
	}
	// a reset password ends the person's sessions at once
	mid := h.ids["manager@alpha.example"]
	r := owner.do("PATCH", "/api/admin/users/"+mid, map[string]any{"resetPassword": true})
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"password"`) {
		t.Fatalf("reset: %d %s", r.Code, r.Body)
	}
	if manager.do("GET", "/api/admin/session", nil).Code != http.StatusUnauthorized {
		t.Fatal("session survived a password reset")
	}
	if r := owner.do("DELETE", "/api/admin/users/"+mid, nil); r.Code != http.StatusOK {
		t.Fatalf("remove: %d", r.Code)
	}
}

func TestOwnPasswordChange(t *testing.T) {
	h := newAdminHarness(t)
	c := h.login("viewer@alpha.example")
	other := h.login("viewer@alpha.example")
	if r := c.do("PUT", "/api/admin/me/password", map[string]string{"current": "wrong-password", "new": "a-new-password-1"}); r.Code != http.StatusBadRequest {
		t.Fatalf("wrong current: %d", r.Code)
	}
	if r := c.do("PUT", "/api/admin/me/password", map[string]string{"current": "alpha-viewer-pass", "new": "short"}); r.Code != http.StatusBadRequest {
		t.Fatalf("short new: %d", r.Code)
	}
	r := c.do("PUT", "/api/admin/me/password", map[string]string{"current": "alpha-viewer-pass", "new": "a-new-password-1"})
	if r.Code != http.StatusOK {
		t.Fatalf("change: %d %s", r.Code, r.Body)
	}
	for _, ck := range r.Result().Cookies() {
		if ck.Name == cookieName {
			c.cookie = ck.Value
		}
	}
	if c.do("GET", "/api/admin/session", nil).Code != http.StatusOK {
		t.Fatal("this browser lost its session")
	}
	if other.do("GET", "/api/admin/session", nil).Code != http.StatusUnauthorized {
		t.Fatal("another session survived the change")
	}
	if h.loginReq("viewer@alpha.example", "alpha-viewer-pass", "198.51.100.81").Code == http.StatusOK {
		t.Fatal("old password still works")
	}
	_ = fmt.Sprint
}
