package tlscert

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func generateInto(t *testing.T, commonName string) (certPath, keyPath string) {
	t.Helper()
	dir := t.TempDir()
	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")
	if err := Generate(commonName, certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestGenerateProducesUsableKeyPair(t *testing.T) {
	certPath, keyPath := generateInto(t, "example.test")

	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		t.Fatalf("generated cert/key are not a usable TLS pair: %v", err)
	}
}

// TestPrivateKeyIsNotWorldReadable guards a deployment mistake that
// silently exposes the server's identity to every local user.
func TestPrivateKeyIsNotWorldReadable(t *testing.T) {
	_, keyPath := generateInto(t, "example.test")

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("private key mode is %o; it must not be readable by group or others", perm)
	}
}

// TestPinIsStableAndUnique is what the client's MITM protection rests on:
// the same certificate must always yield the same pin, and two different
// certificates must never collide.
func TestPinIsStableAndUnique(t *testing.T) {
	certA, _ := generateInto(t, "example.test")
	certB, _ := generateInto(t, "example.test")

	pinA1, err := PinFromCertFile(certA)
	if err != nil {
		t.Fatal(err)
	}
	pinA2, err := PinFromCertFile(certA)
	if err != nil {
		t.Fatal(err)
	}
	if pinA1 != pinA2 {
		t.Fatal("the same certificate produced two different pins")
	}
	if len(pinA1) != 64 {
		t.Fatalf("pin is %d hex chars, want 64 for SHA-256", len(pinA1))
	}

	pinB, err := PinFromCertFile(certB)
	if err != nil {
		t.Fatal(err)
	}
	if pinA1 == pinB {
		t.Fatal("two distinct certificates produced the same pin")
	}
}

// TestPinMatchesLiveConnection is the property that actually matters:
// the pin computed from the file on the server must equal the pin the
// client computes from the certificate presented during the handshake.
// If these ever diverge, pinning would reject every legitimate server.
func TestPinMatchesLiveConnection(t *testing.T) {
	certPath, keyPath := generateInto(t, "example.test")

	filePin, err := PinFromCertFile(certPath)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Hold the connection open long enough for the client's
		// handshake to complete.
		defer conn.Close()
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
	}()

	var connPin string
	client, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		ServerName:         "example.test",
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			pin, err := PinFromConnState(state)
			if err != nil {
				return err
			}
			connPin = pin
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if connPin != filePin {
		t.Fatalf("pin from live connection (%s) differs from pin from file (%s)", connPin, filePin)
	}
}

func TestPinFromCertFileRejectsNonPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.crt")
	if err := os.WriteFile(path, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PinFromCertFile(path); err == nil {
		t.Fatal("expected a non-PEM file to be rejected")
	}
}

func TestPinFromConnStateRequiresCertificate(t *testing.T) {
	if _, err := PinFromConnState(tls.ConnectionState{}); err == nil {
		t.Fatal("expected an error when no peer certificate was presented")
	}
}
