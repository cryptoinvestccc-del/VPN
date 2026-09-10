package obfuscator

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// TestFullSizeWireGuardPacketSurvives covers the packet sizes a real
// WireGuard interface emits. A wrapper that rejects full-size packets
// looks fine on a ping and stalls every real transfer.
func TestFullSizeWireGuardPacketSurvives(t *testing.T) {
	o := mustObfuscator(t)

	// 1452 = WireGuard data packet at the default MTU of 1420.
	// 9000  = jumbo frame territory.
	for _, size := range []int{1, 32, 148, 1280, 1420, 1452, 1500, 9000} {
		plaintext := make([]byte, size)
		if _, err := rand.Read(plaintext); err != nil {
			t.Fatal(err)
		}

		wrapped, err := o.Wrap(plaintext)
		if err != nil {
			t.Fatalf("wrapping a %d-byte packet failed: %v", size, err)
		}
		got, err := o.Unwrap(wrapped)
		if err != nil {
			t.Fatalf("unwrapping a %d-byte packet failed: %v", size, err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("%d-byte packet did not survive the round trip", size)
		}
	}
}

// TestPaddingNeverForcesFragmentation checks that padding is trimmed as
// packets approach the path MTU. Padding a full-size packet past the MTU
// would fragment it — costing throughput and creating exactly the kind of
// distinctive traffic pattern padding exists to prevent.
func TestPaddingNeverForcesFragmentation(t *testing.T) {
	o := mustObfuscator(t)

	for size := 1; size <= SafeWireSize; size += 7 {
		plaintext := make([]byte, size)
		wrapped, err := o.Wrap(plaintext)
		if err != nil {
			t.Fatalf("wrapping %d bytes: %v", size, err)
		}

		// Either the wrapped packet fits within the safe size, or it
		// was already too big to fit before any padding was added.
		unpaddedSize := size + Overhead
		if len(wrapped) > SafeWireSize && unpaddedSize <= SafeWireSize {
			t.Fatalf("padding pushed a %d-byte packet to %d bytes, past the %d-byte safe size",
				size, len(wrapped), SafeWireSize)
		}
	}
}

// TestPaddingVariesAcrossPackets guards the whole point of the padding
// layer: identical inputs must not produce identical wire sizes, or the
// fixed lengths that identify WireGuard survive wrapping.
func TestPaddingVariesAcrossPackets(t *testing.T) {
	o := mustObfuscator(t)

	// A handshake-init-sized packet, the most recognizable WireGuard
	// signature and the one with the most padding headroom.
	plaintext := make([]byte, 148)

	sizes := map[int]bool{}
	for i := 0; i < 200; i++ {
		wrapped, err := o.Wrap(plaintext)
		if err != nil {
			t.Fatal(err)
		}
		sizes[len(wrapped)] = true
	}

	if len(sizes) < 50 {
		t.Fatalf("padding produced only %d distinct sizes across 200 packets; "+
			"WireGuard's fixed packet lengths would still be visible", len(sizes))
	}
}

// TestJunkPacketsLookLikeTraffic checks decoys stay in a plausible size
// range: a 64KB "decoy" would be a fingerprint of its own.
func TestJunkPacketsLookLikeTraffic(t *testing.T) {
	for i := 0; i < 100; i++ {
		junk, err := Junk()
		if err != nil {
			t.Fatal(err)
		}
		if len(junk) < minJunkSize || len(junk) > maxJunkSize {
			t.Fatalf("junk packet of %d bytes is outside the plausible range %d..%d",
				len(junk), minJunkSize, maxJunkSize)
		}
	}
}

// TestUnwrapRejectsTruncatedPacket covers corruption and deliberate
// truncation: neither may panic or return data.
func TestUnwrapRejectsTruncatedPacket(t *testing.T) {
	o := mustObfuscator(t)

	wrapped, err := o.Wrap([]byte("a packet that will be cut short"))
	if err != nil {
		t.Fatal(err)
	}

	for cut := 0; cut < len(wrapped); cut += 3 {
		if _, err := o.Unwrap(wrapped[:cut]); err == nil {
			t.Fatalf("truncating to %d bytes still authenticated", cut)
		}
	}
}

// TestUnwrapRejectsBitFlips confirms the AEAD tag is actually checked:
// every single-bit corruption must be rejected.
func TestUnwrapRejectsBitFlips(t *testing.T) {
	o := mustObfuscator(t)

	wrapped, err := o.Wrap([]byte("integrity matters"))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < len(wrapped); i += 5 {
		corrupted := make([]byte, len(wrapped))
		copy(corrupted, wrapped)
		corrupted[i] ^= 0x01

		if _, err := o.Unwrap(corrupted); err == nil {
			t.Fatalf("a flipped bit at offset %d was accepted", i)
		}
	}
}

// TestWrapProducesDistinctCiphertexts checks nonce freshness: wrapping
// the same plaintext twice must never produce the same bytes, since a
// repeat would mean a reused nonce.
func TestWrapProducesDistinctCiphertexts(t *testing.T) {
	o := mustObfuscator(t)
	plaintext := []byte("identical input")

	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		wrapped, err := o.Wrap(plaintext)
		if err != nil {
			t.Fatal(err)
		}
		key := string(wrapped)
		if seen[key] {
			t.Fatal("the same ciphertext was produced twice: nonce reuse")
		}
		seen[key] = true
	}
}

func TestWrapRejectsEmptyPacket(t *testing.T) {
	o := mustObfuscator(t)
	if _, err := o.Wrap(nil); err == nil {
		t.Fatal("expected an empty packet to be rejected")
	}
}

func BenchmarkWrap(b *testing.B) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		b.Fatal(err)
	}
	o, err := New(psk)
	if err != nil {
		b.Fatal(err)
	}

	plaintext := make([]byte, 1452)
	b.SetBytes(int64(len(plaintext)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := o.Wrap(plaintext); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnwrap(b *testing.B) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		b.Fatal(err)
	}
	o, err := New(psk)
	if err != nil {
		b.Fatal(err)
	}

	wrapped, err := o.Wrap(make([]byte, 1452))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(1452)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := o.Unwrap(wrapped); err != nil {
			b.Fatal(err)
		}
	}
}
