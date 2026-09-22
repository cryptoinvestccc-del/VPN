package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                {Data: []byte("<!doctype html><title>Besy</title>")},
		"assets/index-abc123.js":    {Data: []byte("console.log(1)")},
		"assets/index-abc123.js.gz": {Data: gzipped("console.log(1)")},
		"assets/index-abc123.css":   {Data: []byte("body{}")},
		"robots.txt":                {Data: []byte("User-agent: *\nAllow: /\n")},
	}
}

func gzipped(body string) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return buf.Bytes()
}

func serveWith(t *testing.T, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Handler(testAssets()).ServeHTTP(rec, req)
	return rec
}

// The built assets are gzipped once at build time. Serving them raw cost
// every first-time visitor about three times the transfer, which on a
// phone is most of the wait before anything renders.
func TestCompressedAssetIsServedWhenAccepted(t *testing.T) {
	rec := serveWith(t, "/assets/index-abc123.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", enc)
	}
	// The body is compressed, so the type has to come from the name
	// underneath it rather than from sniffing gzip's magic bytes.
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("no Vary: Accept-Encoding; a shared cache would hand the gzip body to a client that cannot read it")
	}

	r, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "console.log(1)" {
		t.Errorf("decompressed body = %q", body)
	}
}

func TestPlainAssetIsServedWhenCompressionIsNotAccepted(t *testing.T) {
	for _, accept := range []string{"", "identity", "gzip;q=0"} {
		rec := serveWith(t, "/assets/index-abc123.js", map[string]string{"Accept-Encoding": accept})

		if enc := rec.Header().Get("Content-Encoding"); enc != "" {
			t.Errorf("Accept-Encoding %q got Content-Encoding %q, want none", accept, enc)
		}
		if body := rec.Body.String(); body != "console.log(1)" {
			t.Errorf("Accept-Encoding %q got body %q", accept, body)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Errorf("Accept-Encoding %q: response is not marked Vary", accept)
		}
	}
}

// An asset with no .gz beside it still has to be served. The compressed
// copy is an optimisation, not a requirement of the deploy.
func TestAssetWithoutACompressedCopyIsStillServed(t *testing.T) {
	rec := serveWith(t, "/assets/index-abc123.css", map[string]string{"Accept-Encoding": "gzip"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q, want none", enc)
	}
	if rec.Body.String() != "body{}" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// A path that names a file is a request for that file. Answering
// /robots.txt with the landing page and a 200 told every crawler that a
// page was a crawl policy.
func TestMissingFilePathsAreNotAnsweredWithThePage(t *testing.T) {
	for _, path := range []string{"/sitemap.xml", "/favicon.ico", "/apple-touch-icon.png"} {
		rec := serve(t, http.MethodGet, path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}

	// A real file at such a path is served as itself.
	rec := serve(t, http.MethodGet, "/robots.txt")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "User-agent") {
		t.Errorf("/robots.txt = %d %q, want the file", rec.Code, rec.Body.String())
	}

	// Routes have no extension and still reach the page.
	for _, path := range []string{"/dashboard", "/anything/deep"} {
		rec := serve(t, http.MethodGet, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<!doctype html>") {
			t.Errorf("%s = %d, want the page: a deep link must survive a reload", path, rec.Code)
		}
	}
}

func serve(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler(testAssets()).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestRootServesThePage(t *testing.T) {
	rec := serve(t, http.MethodGet, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Errorf("body = %q, want index.html", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache so a deploy is visible without a hard refresh", cc)
	}
}

// Vite puts the content hash in the filename, so these files can never
// change under a given name — a year-long cache is safe and every other
// path's is not.
func TestHashedAssetsAreCachedImmutably(t *testing.T) {
	rec := serve(t, http.MethodGet, "/assets/index-abc123.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want an immutable cache for a content-hashed file", cc)
	}
}

func TestUnknownPathFallsBackToThePage(t *testing.T) {
	rec := serve(t, http.MethodGet, "/tariffs")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Error("unknown path did not fall back to index.html, so a deep link would 404")
	}
}

func TestMissingAssetDoesNotServeThePageAsJavaScript(t *testing.T) {
	// A missing hashed asset must not come back as index.html with a 200:
	// the browser would try to execute HTML as a script and the real
	// failure would be invisible.
	rec := serve(t, http.MethodGet, "/assets/gone-000000.js")

	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/html") && rec.Code == http.StatusOK {
		t.Errorf("missing asset served as HTML with 200; Content-Type = %q", ct)
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	rec := serve(t, http.MethodGet, "/")

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	// The page deliberately loads nothing from anywhere else; the policy
	// is what makes that a property the browser enforces.
	for _, directive := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP %q is missing %q", csp, directive)
		}
	}
}

func TestWriteMethodsAreRejected(t *testing.T) {
	rec := serve(t, http.MethodPost, "/")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST / = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", allow, "GET, HEAD")
	}
}

func TestPathTraversalCannotEscapeTheAssets(t *testing.T) {
	for _, path := range []string{"/../secret", "/assets/../../secret", "//etc/passwd"} {
		rec := serve(t, http.MethodGet, path)
		if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "root:") {
			t.Errorf("%s escaped the asset root", path)
		}
	}
}

func TestAssetsPrefersTheNamedDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("from disk"), 0o600); err != nil {
		t.Fatal(err)
	}

	assets, err := Assets(dir)
	if err != nil {
		t.Fatalf("Assets(%q): %v", dir, err)
	}

	rec := httptest.NewRecorder()
	Handler(assets).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "from disk" {
		t.Errorf("body = %q, want the file from %s", rec.Body.String(), dir)
	}
}

func TestAssetsReportsAMissingDirectory(t *testing.T) {
	if _, err := Assets(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("Assets returned no error for a directory that does not exist")
	}

	// Without the build tag there is nothing embedded, and the caller
	// needs to be told which command was not run rather than getting a
	// server that answers every request with a 404.
	if _, err := Assets(""); err == nil && !embeddedAvailable() {
		t.Error("Assets(\"\") returned no error with no embedded build")
	}
}

func embeddedAvailable() bool {
	_, ok := Embedded()
	return ok
}
