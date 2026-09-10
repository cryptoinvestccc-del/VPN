package transport

import (
	"github.com/cryptoinvestccc-del/vpn/internal/clients"
	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

// sharedClientID names the implicit client that a single shared psk
// stands for, so the per-client machinery has exactly one code path to
// serve and the shared-key setup is just its smallest case.
const sharedClientID = "shared"

// authEntry is one credential ready to try against an incoming packet.
type authEntry struct {
	clientID string
	obf      *obfuscator.Obfuscator
}

// authenticator decides which client, if any, a packet belongs to.
//
// Identifying the sender costs one AEAD attempt per candidate, so it is
// done only when a peer first appears: an established session remembers
// the client it authenticated and decrypts everything after that with a
// single key. This is the arrangement WireGuard itself uses — search the
// peer list on handshake, then index the session directly — and it is
// what keeps adding clients from slowing down traffic.
type authenticator struct {
	entries []authEntry
}

// newAuthenticator builds the candidate list from a credential set. deriveKey
// lets the TLS transport bind each client's key to the connection before
// use; passing nil uses the credential's key as it stands, which is what
// the UDP transport needs since it has no handshake to bind to.
func newAuthenticator(set *clients.Set, deriveKey func([32]byte) ([32]byte, error)) (*authenticator, error) {
	a := &authenticator{}
	for _, cred := range set.Credentials() {
		key := cred.Key
		if deriveKey != nil {
			derived, err := deriveKey(key)
			if err != nil {
				return nil, err
			}
			key = derived
		}
		obf, err := obfuscator.New(key)
		if err != nil {
			return nil, err
		}
		a.entries = append(a.entries, authEntry{clientID: cred.ClientID, obf: obf})
	}
	return a, nil
}

// authenticate finds the client whose key opens this packet. The returned
// obfuscator is the one to keep for the rest of that peer's session.
//
// A failure is indistinguishable from any other: callers drop the packet
// without responding, so an attacker learns nothing about which keys
// exist by watching how the server reacts.
func (a *authenticator) authenticate(packet []byte) (plaintext []byte, clientID string, obf *obfuscator.Obfuscator, ok bool) {
	for _, entry := range a.entries {
		if pt, err := entry.obf.Unwrap(packet); err == nil {
			return pt, entry.clientID, entry.obf, true
		}
	}
	return nil, "", nil, false
}

// clientCount reports how many distinct clients this authenticator can
// admit, for logging.
func (a *authenticator) clientCount() int {
	seen := make(map[string]bool, len(a.entries))
	for _, e := range a.entries {
		seen[e.clientID] = true
	}
	return len(seen)
}

// registryFor turns the two ways of configuring credentials into the one
// the transport uses. A shared psk becomes a single-client set, so there
// is no second code path to keep correct.
func registryFor(explicit *clients.Registry, psks [][32]byte) (*clients.Registry, error) {
	if explicit != nil {
		return explicit, nil
	}

	credentials := make([]clients.Credential, 0, len(psks))
	for _, psk := range psks {
		credentials = append(credentials, clients.Credential{ClientID: sharedClientID, Key: psk})
	}

	set, err := clients.NewSetFromCredentials(credentials)
	if err != nil {
		return nil, err
	}
	return clients.NewStaticRegistry(set), nil
}
