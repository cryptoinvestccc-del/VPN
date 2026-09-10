package wireguard_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

// testContext returns a context cancelled when the test finishes, so the
// proxies under test shut down rather than leaking into later tests.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

// testCert generates a throwaway server certificate and returns its pin.
func testCert(t *testing.T) (certPath, keyPath, pin string) {
	t.Helper()

	dir := t.TempDir()
	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")
	if err := tlscert.Generate("test.local", certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	pin, err := tlscert.PinFromCertFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, pin
}
