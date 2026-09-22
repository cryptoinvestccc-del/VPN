// Package webui serves the built landing page.
//
// The assets can come from either of two places. A release binary embeds
// them, so the site ships as one file with nothing to deploy beside it —
// the same reasoning that makes the server a static binary on a scratch
// image. A development build reads them off disk instead, so `npm run
// dev` and the Go server can be restarted independently.
//
// Embedding is behind the `webui` build tag rather than on by default,
// because `go build ./...` and CI must not require a Node toolchain to
// have run first.
package webui

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

// ErrNoBuild says no built assets were found in either place.
var ErrNoBuild = errors.New("webui: no built assets (run `npm run build` in web/, or build with -tags webui)")

// Assets picks the asset source: dir when it names a directory that
// exists, otherwise whatever the binary embedded.
func Assets(dir string) (fs.FS, error) {
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, errors.New("webui: " + dir + " is not a directory")
		}
		return os.DirFS(dir), nil
	}

	embedded, ok := Embedded()
	if !ok {
		return nil, ErrNoBuild
	}
	return embedded, nil
}

// Handler serves the site out of assets.
//
// Hashed files under /assets/ are immutable by construction — Vite puts
// the content hash in the name — so they get a year-long cache. Anything
// else, index.html included, is revalidated every time, which is what
// makes a deploy visible without asking anyone to hard-refresh.
func Handler(assets fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		setSecurityHeaders(w)

		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		name := strings.TrimPrefix(clean, "/")

		underAssets := strings.HasPrefix(clean, "/assets/")

		if name == "" || !fileExists(assets, name) {
			// A missing file under /assets/ is a broken build, and it has
			// to 404. Falling back to index.html there would answer a
			// request for a script with HTML and a 200, so the browser
			// would fail parsing it and the real cause — a stale or
			// half-deployed build — would never appear anywhere.
			if underAssets {
				http.NotFound(w, r)
				return
			}
			// A path that names a file is a request for that file, and
			// a missing one is a 404. Answering /robots.txt with the
			// landing page and a 200 told every crawler that a page is
			// a crawl policy; the same went for /favicon.ico and
			// /sitemap.xml. Routes have no extension, so this costs the
			// fallback nothing.
			if path.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
			// Everywhere else the fallback is right: the two pages this
			// site has, and any deep link added later, all land here.
			serveIndex(w, r, assets)
			return
		}

		if underAssets {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		// Vary goes on every response that could have been compressed,
		// answered that way or not: a cache that stored the plain body
		// under a key that ignores Accept-Encoding would hand it to a
		// client that asked for gzip, and vice versa.
		w.Header().Add("Vary", "Accept-Encoding")
		if servePrecompressed(w, r, assets, name) {
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

// encodings are the precompressed forms this server will serve, best
// first. scripts/build-web.sh writes the .gz files; .br is listed so a
// build that adds brotli is served without touching this code.
var encodings = []struct{ name, ext string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

// servePrecompressed answers with a compressed sibling of the asset when
// the client accepts one, and reports whether it did.
//
// Compressing here rather than on the fly is what suits these files:
// they are built once and never change, the hashed ones are immutable by
// name, and gzip -9 at build time costs nothing per request. Serving
// them raw cost every first-time visitor about 220 kB — three times the
// transfer — which on a phone is most of the wait before anything
// appears.
func servePrecompressed(w http.ResponseWriter, r *http.Request, assets fs.FS, name string) bool {
	if name == "" {
		return false
	}

	for _, enc := range encodings {
		if !acceptsEncoding(r, enc.name) {
			continue
		}

		f, err := assets.Open(name + enc.ext)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			_ = f.Close()
			continue
		}

		// The body is compressed, so the type has to come from the name
		// underneath it. Sniffing would read gzip's magic bytes and call
		// a stylesheet an octet-stream.
		ctype := mime.TypeByExtension(path.Ext(name))
		if ctype == "" {
			_ = f.Close()
			continue
		}

		h := w.Header()
		h.Set("Content-Type", ctype)
		h.Set("Content-Encoding", enc.name)
		h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		// Content-Length describes the compressed body, so a range over
		// it would be a range into a stream the client cannot seek.
		h.Del("Accept-Ranges")

		if r.Method == http.MethodHead {
			_ = f.Close()
			w.WriteHeader(http.StatusOK)
			return true
		}

		_, _ = io.Copy(w, f)
		_ = f.Close()
		return true
	}

	return false
}

// acceptsEncoding reports whether the request asks for this encoding.
// An explicit q=0 is a refusal, which is the one case a substring match
// gets backwards.
func acceptsEncoding(r *http.Request, enc string) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), enc) {
			continue
		}
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if after, ok := strings.CutPrefix(param, "q="); ok {
				q, err := strconv.ParseFloat(after, 64)
				if err == nil && q == 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	body, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "site not built", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(body)
}

func fileExists(assets fs.FS, name string) bool {
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

// setSecurityHeaders locks the page down to its own origin. The site
// loads no fonts, scripts, styles or images from anywhere else — that is
// a deliberate property of a VPN's landing page, and a policy that says
// so turns it into one a browser enforces rather than one a reviewer has
// to take on trust.
func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; base-uri 'none'; "+
			"object-src 'none'; frame-ancestors 'none'; form-action 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=(), interest-cohort=()")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
}
