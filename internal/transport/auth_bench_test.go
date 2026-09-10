package transport

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

func benchAuthenticator(b *testing.B, clientCount int) (*authenticator, []byte, []byte) {
	b.Helper()

	credentials := make([]clients.Credential, 0, clientCount)
	var lastKey [32]byte
	for i := 0; i < clientCount; i++ {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			b.Fatal(err)
		}
		lastKey = key
		credentials = append(credentials, clients.Credential{
			ClientID: fmt.Sprintf("client-%d", i),
			Key:      key,
		})
	}

	set, err := clients.NewSetFromCredentials(credentials)
	if err != nil {
		b.Fatal(err)
	}
	auth, err := newAuthenticator(set, nil)
	if err != nil {
		b.Fatal(err)
	}

	// Worst case for a legitimate peer: the last credential tried.
	obf, err := obfuscator.New(lastKey)
	if err != nil {
		b.Fatal(err)
	}
	valid, err := obf.Wrap(make([]byte, 148))
	if err != nil {
		b.Fatal(err)
	}

	// What an attacker sends: bytes that will never authenticate, so
	// every credential is tried before the packet is dropped.
	junk := make([]byte, len(valid))
	if _, err := rand.Read(junk); err != nil {
		b.Fatal(err)
	}

	return auth, valid, junk
}

// BenchmarkAuthenticateWorstCase measures a legitimate peer whose
// credential is last in the list — the cost a real client pays when it
// first appears.
func BenchmarkAuthenticateWorstCase(b *testing.B) {
	for _, count := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("clients=%d", count), func(b *testing.B) {
			auth, valid, _ := benchAuthenticator(b, count)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, _, ok := auth.authenticate(valid); !ok {
					b.Fatal("a valid packet was rejected")
				}
			}
		})
	}
}

// BenchmarkAuthenticateGarbage measures what an attacker can make the
// server spend. Every credential is tried before an unauthenticated
// packet is dropped, so this is the cost per junk packet from an unknown
// source — the number that decides whether a flood is a real threat.
func BenchmarkAuthenticateGarbage(b *testing.B) {
	for _, count := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("clients=%d", count), func(b *testing.B) {
			auth, _, junk := benchAuthenticator(b, count)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, _, ok := auth.authenticate(junk); ok {
					b.Fatal("garbage authenticated")
				}
			}
		})
	}
}

// BenchmarkEstablishedSessionUnwrap is the comparison that matters: once
// a peer is known, its packets cost a single decryption no matter how
// many clients are configured. If this ever grew with the client count,
// adding clients would slow down every packet the server carries.
func BenchmarkEstablishedSessionUnwrap(b *testing.B) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		b.Fatal(err)
	}
	obf, err := obfuscator.New(key)
	if err != nil {
		b.Fatal(err)
	}
	packet, err := obf.Wrap(make([]byte, 1400))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := obf.Unwrap(packet); err != nil {
			b.Fatal(err)
		}
	}
}
