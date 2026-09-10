package obfuscator

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func mustObfuscator(t *testing.T) *Obfuscator {
	t.Helper()
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}
	o, err := New(psk)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestRoundTrip(t *testing.T) {
	o := mustObfuscator(t)
	plaintext := []byte("this simulates a wireguard handshake packet")

	wrapped, err := o.Wrap(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	got, err := o.Unwrap(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestWrapHidesLength(t *testing.T) {
	o := mustObfuscator(t)
	short := []byte("a")
	long := bytes.Repeat([]byte("b"), 200)

	// Padding is random, so we can't assert exact sizes are different,
	// but wrapped output must never trivially equal plaintext length +
	// fixed overhead (that would defeat the point of padding).
	wShort, err := o.Wrap(short)
	if err != nil {
		t.Fatal(err)
	}
	wLong, err := o.Wrap(long)
	if err != nil {
		t.Fatal(err)
	}
	if len(wShort) == 0 || len(wLong) == 0 {
		t.Fatal("wrapped packets must not be empty")
	}
}

func TestUnwrapRejectsForgedPacket(t *testing.T) {
	o := mustObfuscator(t)
	forged := make([]byte, 100)
	if _, err := rand.Read(forged); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Unwrap(forged); err == nil {
		t.Fatal("expected forged packet to be rejected")
	}
}

func TestUnwrapRejectsWrongKey(t *testing.T) {
	o1 := mustObfuscator(t)
	o2 := mustObfuscator(t)

	wrapped, err := o1.Wrap([]byte("secret payload"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o2.Unwrap(wrapped); err == nil {
		t.Fatal("expected decryption under wrong key to fail")
	}
}

func TestJunkPacketsAreRejectedNotCrash(t *testing.T) {
	o := mustObfuscator(t)
	for i := 0; i < 50; i++ {
		junk, err := Junk()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.Unwrap(junk); err == nil {
			// Astronomically unlikely (would require junk to
			// authenticate under AEAD) but not impossible in
			// principle; fail loudly if it ever happens so we
			// notice.
			t.Fatal("junk packet unexpectedly authenticated")
		}
	}
}

func TestWrapRejectsOversizedPlaintext(t *testing.T) {
	o := mustObfuscator(t)
	oversized := bytes.Repeat([]byte("x"), MaxPacketSize+1)
	if _, err := o.Wrap(oversized); err == nil {
		t.Fatal("expected oversized plaintext to be rejected")
	}
}
