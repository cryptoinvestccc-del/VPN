package provision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serviceWithRegistry(t *testing.T) (*Service, *fakeDevice, memRegistry) {
	t.Helper()
	dev := &fakeDevice{}
	svc := testService(t, dev)
	reg := memRegistry{}
	svc.SetRegistry(reg)
	return svc, dev, reg
}

func hasPeer(dev *fakeDevice, key string) bool {
	peers, _ := dev.Peers(context.Background())
	for _, p := range peers {
		if p.PublicKey == key {
			return true
		}
	}
	return false
}

func TestATokenComesOnceAndOnlyTheHashIsKept(t *testing.T) {
	svc, _, reg := serviceWithRegistry(t)
	key := testKey(1)

	first, err := svc.Issue(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if first.ForgetToken == "" {
		t.Fatal("a new credential came without a forget token")
	}
	if stored := reg[key]; stored == "" || strings.Contains(stored, first.ForgetToken) {
		t.Errorf("the registry holds %q; it must hold a hash, never the token", stored)
	}

	// Asking again hands back the same peer — and no token, because the
	// server no longer knows it is talking to the device that got it.
	again, err := svc.Issue(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if again.ForgetToken != "" {
		t.Error("a repeated request received a token; anyone presenting the public key would")
	}
}

func TestOnlyTheTokenRemovesTheCredential(t *testing.T) {
	svc, dev, reg := serviceWithRegistry(t)
	key := testKey(2)
	cfg, err := svc.Issue(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}

	for _, wrong := range []string{"", "guess", cfg.ForgetToken + "x"} {
		if err := svc.Forget(context.Background(), key, wrong); !errors.Is(err, ErrNotYours) {
			t.Errorf("token %q: got %v, want refusal", wrong, err)
		}
	}
	if !hasPeer(dev, key) {
		t.Fatal("a wrong token removed the peer")
	}

	if err := svc.Forget(context.Background(), key, cfg.ForgetToken); err != nil {
		t.Fatalf("the right token was refused: %v", err)
	}
	if hasPeer(dev, key) {
		t.Error("the peer is still on the interface")
	}
	if reg.Owns(key) {
		t.Error("the credential is still on record")
	}
	if err := svc.Forget(context.Background(), key, cfg.ForgetToken); !errors.Is(err, ErrNotYours) {
		t.Errorf("a second removal: got %v", err)
	}
}

// TestAmneziasPeersCannotBeForgotten: a peer this service did not create
// has no token, and no request removes it.
func TestAmneziasPeersCannotBeForgotten(t *testing.T) {
	svc, dev, _ := serviceWithRegistry(t)
	amnezia := testKey(7)
	dev.peers = append(dev.peers, Peer{PublicKey: amnezia})
	if err := svc.Forget(context.Background(), amnezia, "anything"); !errors.Is(err, ErrNotYours) {
		t.Fatalf("got %v", err)
	}
	if !hasPeer(dev, amnezia) {
		t.Error("Amnezia's peer was removed")
	}
}

func TestForgetOverHTTP(t *testing.T) {
	svc, dev, _ := serviceWithRegistry(t)
	h := Handler(svc, NewLimiter(0, 0))
	key := testKey(3)

	rec := post(t, h, `{"public_key":"`+key+`"}`, "203.0.113.5:1")
	var reply struct {
		ForgetToken string `json:"forget_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil || reply.ForgetToken == "" {
		t.Fatalf("no token in the reply: %s", rec.Body)
	}

	send := func(body string) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/issue/forget", strings.NewReader(body))
		r.RemoteAddr = "203.0.113.5:1"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := send(`{"public_key":"` + key + `","forget_token":"wrong"}`); code != http.StatusForbidden {
		t.Errorf("wrong token: %d", code)
	}
	if code := send(`{"public_key":"` + key + `","forget_token":"` + reply.ForgetToken + `","extra":1}`); code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", code)
	}
	if code := send(`{"public_key":"` + key + `","forget_token":"` + reply.ForgetToken + `"}`); code != http.StatusNoContent {
		t.Errorf("right token: %d", code)
	}
	if hasPeer(dev, key) {
		t.Error("the peer survived")
	}
}

func TestStatusCountsRecentHandshakesOnly(t *testing.T) {
	dev := &fakeDevice{peers: []Peer{
		{PublicKey: "a", LastHandshake: time.Now().Add(-30 * time.Second)},
		{PublicKey: "b", LastHandshake: time.Now().Add(-10 * time.Minute)},
		{PublicKey: "c"},
	}}
	svc := testService(t, dev)
	svc.SetMaxPeers(4000)
	h := Handler(svc, nil)

	r := httptest.NewRequest(http.MethodGet, "/v1/issue/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var st map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st["connected"] != float64(1) || st["capacity"] != float64(4000) {
		t.Errorf("got %v", st)
	}
	if len(st) != 2 {
		t.Errorf("the status says more than a count: %v", st)
	}
}
