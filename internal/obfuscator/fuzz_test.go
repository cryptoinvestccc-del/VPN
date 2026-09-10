package obfuscator

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// FuzzUnwrap throws malformed input at the packet parser. Unwrap runs on
// every datagram a public port receives, before anything is known about
// the sender, so it is the first thing an attacker can reach: a panic
// here is a remote crash, and an out-of-range read is worse.
func FuzzUnwrap(f *testing.F) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		f.Fatal(err)
	}
	o, err := New(psk)
	if err != nil {
		f.Fatal(err)
	}

	// Seed with shapes worth mutating: a genuine packet, a truncated
	// one, an empty one, and something the size of a WireGuard header.
	valid, err := o.Wrap([]byte("a genuine packet"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add([]byte{})
	f.Add(make([]byte, Overhead))
	f.Add(make([]byte, Overhead+1))

	f.Fuzz(func(t *testing.T, packet []byte) {
		// The contract: never panic, and never return a plaintext for
		// input that did not authenticate.
		plaintext, err := o.Unwrap(packet)
		if err != nil {
			if plaintext != nil {
				t.Fatal("Unwrap returned both an error and a plaintext")
			}
			return
		}
		if len(plaintext) == 0 {
			t.Fatal("Unwrap accepted a packet but produced no payload")
		}
	})
}

// FuzzWrapUnwrapRoundTrip checks the inverse property across arbitrary
// payloads: whatever Wrap accepts, Unwrap must return unchanged.
func FuzzWrapUnwrapRoundTrip(f *testing.F) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		f.Fatal(err)
	}
	o, err := New(psk)
	if err != nil {
		f.Fatal(err)
	}

	f.Add([]byte("x"))
	f.Add(bytes.Repeat([]byte("a"), 1452))
	f.Add(bytes.Repeat([]byte("b"), SafeWireSize))

	f.Fuzz(func(t *testing.T, payload []byte) {
		wrapped, err := o.Wrap(payload)
		if err != nil {
			return // rejected inputs are allowed, silent corruption is not
		}

		got, err := o.Unwrap(wrapped)
		if err != nil {
			t.Fatalf("a packet we produced did not survive Unwrap: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("round trip changed the payload: got %d bytes, sent %d", len(got), len(payload))
		}
	})
}
