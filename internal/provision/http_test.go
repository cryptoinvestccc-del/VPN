package provision

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, h http.Handler, body string, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/issue", strings.NewReader(body))
	if remote != "" {
		req.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIssueEndpoint(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))

	rec := post(t, h, `{"public_key":"`+testKey(1)+`"}`, "203.0.113.5:40000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var got issueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Address != "10.8.0.2/32" {
		t.Errorf("address: %q", got.Address)
	}
	if got.Endpoint != "198.51.100.9:51820" || got.ServerPublicKey == "" {
		t.Errorf("the client cannot connect with this: %+v", got)
	}
	for _, key := range []string{"jc", "jmin", "jmax", "s1", "s2", "h1", "h2", "h3", "h4"} {
		if got.Awg[key] == "" {
			t.Errorf("obfuscation parameter %s is missing; the handshake would never be answered", key)
		}
	}

	// A configuration is for one device. Anything caching it would be
	// handing somebody else's address to the next caller.
	if store := rec.Header().Get("Cache-Control"); store != "no-store" {
		t.Errorf("Cache-Control is %q", store)
	}
}

// TestResponseCarriesNoPrivateKey: the device made its own and this
// service never saw one. If a private key ever appeared in a reply it
// would mean the design had been abandoned somewhere.
func TestResponseCarriesNoPrivateKey(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))
	rec := post(t, h, `{"public_key":"`+testKey(1)+`"}`, "203.0.113.5:40000")

	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"private", "privatekey", "private_key"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the reply mentions %q: %s", forbidden, rec.Body)
		}
	}
}

func TestIssueEndpointRejectsBadRequests(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))

	cases := map[string]struct {
		body string
		want int
	}{
		"not json":       {`not json`, http.StatusBadRequest},
		"missing key":    {`{}`, http.StatusBadRequest},
		"bad key":        {`{"public_key":"short"}`, http.StatusBadRequest},
		"unknown field":  {`{"public_key":"` + testKey(1) + `","name":"vasya"}`, http.StatusBadRequest},
		"oversized body": {`{"public_key":"` + strings.Repeat("A", 8192) + `"}`, http.StatusBadRequest},
	}
	for name, tc := range cases {
		if rec := post(t, h, tc.body, "203.0.113.5:40000"); rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", name, rec.Code, tc.want, rec.Body)
		}
	}
}

// TestUnknownFieldsAreRejected spells out why the request shape is
// closed: there is no field for a name, a device id or an email, and
// silently ignoring one would let a later client start sending them
// without anybody deciding to collect them.
func TestUnknownFieldsAreRejected(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))
	rec := post(t, h, `{"public_key":"`+testKey(1)+`","device_id":"abc","email":"a@b.c"}`, "203.0.113.5:1")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a request carrying identifying fields was accepted: %d %s", rec.Code, rec.Body)
	}
}

func TestOnlyPostIsAllowed(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/v1/issue", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s returned %d", method, rec.Code)
		}
	}
}

// TestRateLimitIsPerSource: one caller must not be able to exhaust the
// address pool for everybody, and must not be able to lock everybody
// else out either.
func TestRateLimitIsPerSource(t *testing.T) {
	limiter := NewLimiter(0, 3)
	frozen := time.Now()
	limiter.nowFn = func() time.Time { return frozen }
	h := Handler(testService(t, &fakeDevice{}), limiter)

	var throttled bool
	for i := 0; i < 6; i++ {
		if post(t, h, `{"public_key":"`+testKey(i)+`"}`, "203.0.113.5:40000").Code == http.StatusTooManyRequests {
			throttled = true
		}
	}
	if !throttled {
		t.Fatal("a caller was never throttled")
	}

	// A different source is untouched by the first one's behaviour.
	if rec := post(t, h, `{"public_key":"`+testKey(99)+`"}`, "198.51.100.7:40000"); rec.Code != http.StatusOK {
		t.Errorf("a second source was blocked by the first one's limit: %d %s", rec.Code, rec.Body)
	}
}

// TestForwardedHeaderIsIgnoredByDefault: a header anybody can set is not
// an identity. Trusting it unasked would let one caller present a new
// address on every request and walk past the limit entirely.
func TestForwardedHeaderIsIgnoredByDefault(t *testing.T) {
	limiter := NewLimiter(0, 2)
	frozen := time.Now()
	limiter.nowFn = func() time.Time { return frozen }
	h := Handler(testService(t, &fakeDevice{}), limiter)

	throttled := false
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/issue",
			strings.NewReader(`{"public_key":"`+testKey(i)+`"}`))
		req.RemoteAddr = "203.0.113.5:40000"
		req.Header.Set("X-Forwarded-For", "10.0.0."+string(rune('1'+i)))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			throttled = true
		}
	}
	if !throttled {
		t.Fatal("a spoofed X-Forwarded-For let one caller past the limit")
	}
}

func TestFullServerSaysSoRatherThanFailing(t *testing.T) {
	svc := testService(t, &fakeDevice{})
	svc.SetMaxPeers(1)
	h := Handler(svc, NewLimiter(0, 0))

	post(t, h, `{"public_key":"`+testKey(1)+`"}`, "203.0.113.5:1")
	rec := post(t, h, `{"public_key":"`+testKey(2)+`"}`, "198.51.100.1:1")

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a full server returned %d, expected 503 so the app can say what happened", rec.Code)
	}
}

// TestInternalFailuresDoNotDescribeTheServer: an error naming the
// interface or the command that failed is a description of the server's
// insides, handed to anybody who asks.
func TestInternalFailuresDoNotDescribeTheServer(t *testing.T) {
	device := &fakeDevice{readErr: errFake("awg0: exec /usr/bin/docker: permission denied")}
	h := Handler(testService(t, device), NewLimiter(0, 0))

	rec := post(t, h, `{"public_key":"`+testKey(1)+`"}`, "203.0.113.5:1")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	for _, leak := range []string{"docker", "awg0", "permission denied"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("the reply leaks %q: %s", leak, rec.Body)
		}
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestHealthz(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz returned %d", rec.Code)
	}
}

func TestClientConfigRendersWhatAmneziaWGNeeds(t *testing.T) {
	svc := testService(t, &fakeDevice{})
	cfg, err := svc.Issue(t.Context(), testKey(1))
	if err != nil {
		t.Fatal(err)
	}

	conf := cfg.ClientConfig("DEVICEPRIVATEKEY=")
	for _, want := range []string{
		"[Interface]", "PrivateKey = DEVICEPRIVATEKEY=", "Address = 10.8.0.2/32",
		"Jc = 4", "S1 = 86", "H1 = 1", "H4 = 4",
		"[Peer]", "Endpoint = 198.51.100.9:51820", "AllowedIPs = 0.0.0.0/0, ::/0",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("the rendered config is missing %q:\n%s", want, conf)
		}
	}
}

func TestLimiterForgetsQuietSources(t *testing.T) {
	limiter := NewLimiter(0, 0)
	frozen := time.Now()
	limiter.nowFn = func() time.Time { return frozen }

	for i := 0; i < 100; i++ {
		limiter.Allow("10.0.0." + string(rune(i)))
	}
	if limiter.Tracked() != 100 {
		t.Fatalf("tracking %d sources", limiter.Tracked())
	}

	frozen = frozen.Add(limiterIdle + time.Minute)
	if dropped := limiter.Sweep(); dropped != 100 {
		t.Errorf("swept %d of 100 quiet sources", dropped)
	}
	if limiter.Tracked() != 0 {
		t.Errorf("%d sources are still remembered", limiter.Tracked())
	}
}

// TestReplyShapeIsPinned writes down the shape of the reply, because the
// client reading it lives in another language and cannot be checked from
// here. The two sides were written separately once and drifted: dns is a
// list, the app read it as a comma-separated string, and the first run
// on a phone put the literal ["1.1.1.1"] where an address belonged.
//
// A field changing type is the kind of break that compiles on both sides
// and fails on a device, so it is asserted rather than assumed.
func TestReplyShapeIsPinned(t *testing.T) {
	h := Handler(testService(t, &fakeDevice{}), NewLimiter(0, 0))
	rec := post(t, h, `{"public_key":"`+testKey(1)+`"}`, "203.0.113.5:1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}

	// What each field must be on the wire, named as the app reads it.
	expect := map[string]string{
		"address":           "string",
		"dns":               "list of strings",
		"mtu":               "number",
		"server_public_key": "string",
		"endpoint":          "string",
		"allowed_ips":       "string",
		"keepalive":         "number",
		"awg":               "object of strings",
	}

	for field, want := range expect {
		value, present := raw[field]
		if !present {
			t.Errorf("%s is missing from the reply", field)
			continue
		}
		if got := shapeOf(value); got != want {
			t.Errorf("%s is %s, the app expects %s", field, got, want)
		}
	}
}

func shapeOf(v any) string {
	switch t := v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		for _, e := range t {
			if _, ok := e.(string); !ok {
				return "list of something else"
			}
		}
		return "list of strings"
	case map[string]any:
		for _, e := range t {
			if _, ok := e.(string); !ok {
				return "object of something else"
			}
		}
		return "object of strings"
	}
	return "unknown"
}

// TestAForgedForwardedForDoesNotBuyMoreRequests is the production setup:
// nginx with proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for,
// which appends the real address to whatever the caller sent. Taking the
// first entry took the caller's own invention, so one machine could
// present a new address per request, walk past the limit, and fill every
// slot on the server with keys it generated for the purpose.
func TestAForgedForwardedForDoesNotBuyMoreRequests(t *testing.T) {
	const real = "198.51.100.7"
	var seen = map[string]bool{}
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodPost, "/v1/issue", nil)
		r.RemoteAddr = "127.0.0.1:40000" // nginx, on the same host
		forged := fmt.Sprintf("203.0.113.%d", i)
		// What nginx sends on: the caller's header, then the real peer.
		r.Header.Set("X-Forwarded-For", forged+", "+real)
		seen[clientIP(r, true)] = true
	}
	if len(seen) != 1 || !seen[real] {
		t.Errorf("five requests from one machine were counted as %v", seen)
	}
}

func TestForwardedForFromAnOverwritingProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/issue", nil)
	r.RemoteAddr = "127.0.0.1:40000"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := clientIP(r, true); got != "198.51.100.7" {
		t.Errorf("got %s", got)
	}
}
