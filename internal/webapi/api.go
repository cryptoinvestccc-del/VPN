package webapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// Handler returns the JSON routes, rooted at /api/v1/.
//
// Everything here is a GET. There is no write path, no session, and no
// CORS header: the page that reads this API is served from the same
// origin by cmd/obfsweb, so cross-origin access would only widen who can
// scrape it without helping anyone who is supposed to use it.
func Handler(src Source, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}

	mux := http.NewServeMux()

	// Go's ServeMux matches the method as part of the pattern, so
	// anything but GET or HEAD gets a 405 with the right Allow header
	// without a check in each handler.
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		// The status is the one response that must not be cached: a
		// stale "11 of 12 nodes online" is worse than a slow one.
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, r, http.StatusOK, src.Status(now()))
	})

	mux.HandleFunc("GET /api/v1/dashboard", func(w http.ResponseWriter, r *http.Request) {
		// Same reasoning as the status route: a dashboard that shows a
		// cached window is showing the wrong window.
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, r, http.StatusOK, src.Dashboard(r.URL.Query().Get("range"), now()))
	})

	mux.HandleFunc("GET /api/v1/locations", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		writeJSON(w, r, http.StatusOK, src.Locations())
	})

	mux.HandleFunc("GET /api/v1/plans", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		writeJSON(w, r, http.StatusOK, src.Plans())
	})

	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusNotFound, map[string]string{
			"error": "unknown endpoint",
		})
	})

	return mux
}

// writeJSON marshals first and writes second, so a marshalling failure
// becomes a 500 rather than a 200 with a truncated body — the response
// header cannot be taken back once the encoder has started writing.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":"encoding failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	// A HEAD request gets the headers and no body; net/http discards a
	// body written here anyway, but not writing it keeps the intent plain.
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}
