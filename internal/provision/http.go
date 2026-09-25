package provision

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxRequestBody bounds what a client may send. A public key request is
// about a hundred bytes; anything approaching this is not one.
const maxRequestBody = 4 << 10

// issueRequest is everything a device sends.
//
// Deliberately one field. Nothing here identifies a person, and there is
// no field for a name, a device id or an email, because a free VPN that
// collects those has quietly become something else.
type issueRequest struct {
	PublicKey string `json:"public_key"`
}

type issueResponse struct {
	Address         string   `json:"address"`
	DNS             []string `json:"dns,omitempty"`
	MTU             int      `json:"mtu,omitempty"`
	ServerPublicKey string   `json:"server_public_key"`
	Endpoint        string   `json:"endpoint"`
	AllowedIPs      string   `json:"allowed_ips,omitempty"`
	Keepalive       int      `json:"keepalive,omitempty"`

	// Awg carries the obfuscation parameters as a flat map, so a client
	// built against a newer AmneziaWG than this server knows about still
	// receives everything the server actually uses.
	Awg map[string]string `json:"awg"`

	// ForgetToken is present only in the reply that created the peer.
	// The device keeps it to remove its own credential later.
	ForgetToken string `json:"forget_token,omitempty"`
}

// forgetRequest is what a device sends to remove its credential.
type forgetRequest struct {
	PublicKey   string `json:"public_key"`
	ForgetToken string `json:"forget_token"`
}

// Handler serves the issuing API.
//
// One route, one method. A provisioning endpoint is the most exposed
// thing this project runs — it is reachable by anyone who downloads the
// app, which is the point — so its surface is kept to what the app needs
// and nothing else.
func Handler(svc *Service, limiter *Limiter) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/issue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		if limiter != nil && !limiter.Allow(clientIP(r, limiter.TrustForwardedFor)) {
			// Deliberately bare: telling a caller how close it is to the
			// limit is telling whoever is abusing it how to pace.
			httpError(w, http.StatusTooManyRequests, "too many requests")
			return
		}

		var req issueRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "the request body must be {\"public_key\": \"...\"}")
			return
		}

		cfg, err := svc.Issue(r.Context(), req.PublicKey)
		switch {
		case errors.Is(err, ErrInvalidKey):
			httpError(w, http.StatusBadRequest, err.Error())
			return
		case errors.Is(err, ErrTooManyPeers), errors.Is(err, ErrPoolFull):
			// The server is full, not broken, and the app should say so
			// rather than look like it failed.
			httpError(w, http.StatusServiceUnavailable, "the server is full; try again later")
			return
		case err != nil:
			// The detail goes to the operator's log, not to the caller:
			// an error describing the interface is a description of the
			// server's insides.
			log.Printf("provision: issue failed: %v", err)
			httpError(w, http.StatusInternalServerError, "could not issue a configuration")
			return
		}

		writeJSON(w, http.StatusOK, responseFor(cfg))
	})

	// Under /v1/issue so that the proxy already in front — configured
	// with a location for /v1/issue, which nginx treats as a prefix —
	// passes them on without anybody editing its configuration.
	mux.HandleFunc("/v1/issue/forget", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			httpError(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		if limiter != nil && !limiter.Allow(clientIP(r, limiter.TrustForwardedFor)) {
			httpError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		var req forgetRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "the request body must be {\"public_key\": \"...\", \"forget_token\": \"...\"}")
			return
		}
		err := svc.Forget(r.Context(), req.PublicKey, req.ForgetToken)
		switch {
		case err == nil:
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrInvalidKey):
			httpError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotYours):
			httpError(w, http.StatusForbidden, "this credential cannot be removed with that token")
		default:
			log.Printf("provision: forget failed: %v", err)
			httpError(w, http.StatusInternalServerError, "could not remove the credential")
		}
	})

	status := &statusCache{svc: svc}
	mux.HandleFunc("/v1/issue/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			httpError(w, http.StatusMethodNotAllowed, "use GET")
			return
		}
		st, err := status.get(r.Context())
		if err != nil {
			log.Printf("provision: status failed: %v", err)
			httpError(w, http.StatusServiceUnavailable, "status unavailable")
			return
		}
		writeJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	return mux
}

func responseFor(cfg Config) issueResponse {
	s := cfg.Settings
	out := issueResponse{
		Address:         cfg.Address.String(),
		MTU:             s.MTU,
		ServerPublicKey: s.ServerPublicKey,
		Endpoint:        s.Endpoint,
		AllowedIPs:      s.AllowedIPs,
		Keepalive:       s.Keepalive,
		Awg:             awgMap(s.Params),
		ForgetToken:     cfg.ForgetToken,
	}
	for _, d := range s.DNS {
		out.DNS = append(out.DNS, d.String())
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// This answer is for one device and must not be held by anything in
	// between: a cached configuration is somebody else's address.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("provision: writing the response: %v", err)
	}
}

func httpError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// clientIP is the address the rate limiter counts against.
//
// X-Forwarded-For is honoured only when the operator has said the
// service sits behind a proxy. A header anybody can set is not an
// identity: trusting it unconditionally would let one caller present a
// different address on every request and walk straight past the limit.
func clientIP(r *http.Request, trustForwarded bool) string {
	if trustForwarded {
		// The last entry, not the first. The proxy in front appends the
		// address it actually saw to whatever the caller sent, so only
		// the last one was written by something trusted; the first is
		// whatever the caller chose. Taking the first let one machine
		// claim a fresh address per request and walk past the limit.
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
				return last
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func awgMap(p Params) map[string]string {
	m := map[string]string{
		"jc": strconv.Itoa(p.Jc), "jmin": strconv.Itoa(p.Jmin), "jmax": strconv.Itoa(p.Jmax),
		"s1": strconv.Itoa(p.S1), "s2": strconv.Itoa(p.S2),
		// Passed through as written: a header may be a single value or a
		// range, and rewriting it as a number would drop the range.
		"h1": p.H1, "h2": p.H2, "h3": p.H3, "h4": p.H4,
	}
	for k, v := range p.Extra {
		m[k] = v
	}
	return m
}

// statusCache answers the status route from a count at most ten seconds
// old. The route is public and each fresh count runs a command inside
// the container, so without this anyone could keep the server busy
// running it.
type statusCache struct {
	svc  *Service
	mu   sync.Mutex
	at   time.Time
	last Status
}

const statusTTL = 10 * time.Second

func (c *statusCache) get(ctx context.Context) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < statusTTL {
		return c.last, nil
	}
	st, err := c.svc.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	c.last, c.at = st, time.Now()
	return st, nil
}
