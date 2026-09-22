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
	"io/fs"
	"net/http"
	"os"
	"path"
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
			// Everywhere else the fallback is right: one page and no
			// client-side router today, but deep links keep working the
			// day one is added, and it costs nothing now.
			serveIndex(w, r, assets)
			return
		}

		if underAssets {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
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
