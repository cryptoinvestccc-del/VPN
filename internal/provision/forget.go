package provision

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotYours is returned for any removal that cannot be proved to come
// from the device that holds the credential.
//
// One error for "no such key", "not issued here" and "wrong token"
// alike: telling them apart would let anyone ask the server which public
// keys it has handed out.
var ErrNotYours = errors.New("provision: this credential cannot be removed with that token")

// newForgetToken makes the secret a device keeps for removing its own
// credential, and the hash the server keeps in its place.
//
// A public key cannot serve: anyone who has seen one could then delete
// that person's VPN. The WireGuard private key cannot either, since it
// signs nothing. So the server hands the device a random secret at the
// one moment it knows it is talking to that device — when it creates
// the peer — and afterwards only compares hashes.
func newForgetToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("provision: no randomness for a token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Forget removes a credential at the request of the device that holds
// its token: the peer from the interface and the record from the
// registry.
func (s *Service) Forget(ctx context.Context, publicKey, token string) error {
	if err := ValidatePublicKey(publicKey); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.registry == nil || token == "" {
		return ErrNotYours
	}
	want, ok := s.registry.SecretHash(publicKey)
	if !ok || want == "" {
		return ErrNotYours
	}
	got := hashToken(token)
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return ErrNotYours
	}

	if err := s.device.RemovePeer(ctx, publicKey); err != nil {
		return fmt.Errorf("provision: removing the peer: %w", err)
	}
	if err := s.registry.Remove(ctx, publicKey); err != nil {
		// The peer is gone, which is what was asked for; the record
		// only means a key that no longer exists is remembered.
		return nil
	}
	return nil
}

// Status is what the app shows about the server before connecting.
//
// A count and nothing else. Which peers are active, and since when, is
// a log of who used the VPN and when, and does not leave the server.
type Status struct {
	Connected int `json:"connected"`
	Capacity  int `json:"capacity"`
}

// activeWithin is how recent a handshake must be for a peer to count as
// connected. WireGuard renews its session every two minutes while
// traffic flows, and a keepalive of 25 seconds keeps an idle phone
// inside the window.
const activeWithin = 3 * time.Minute

// Status counts the peers with a recent handshake.
func (s *Service) Status(ctx context.Context) (Status, error) {
	peers, err := s.device.Peers(ctx)
	if err != nil {
		return Status{}, err
	}
	now := time.Now()
	var st Status
	for _, p := range peers {
		if p.Used() && now.Sub(p.LastHandshake) < activeWithin {
			st.Connected++
		}
	}
	s.mu.Lock()
	st.Capacity = s.maxPeers
	s.mu.Unlock()
	return st, nil
}
