package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validKey(b byte) string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestLoadFullConfig(t *testing.T) {
	path := writeConfig(t, `
mode: tls
psk: "`+validKey(1)+`"
local_addr: "127.0.0.1:51821"
remote_tls_addr: "vpn.example.com:443"
server_name: "vpn.example.com"
pinned_cert_sha256: "abc123"
junk_packets: 5
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "tls" {
		t.Errorf("mode = %q, want tls", cfg.Mode)
	}
	if cfg.LocalAddr != "127.0.0.1:51821" {
		t.Errorf("local_addr = %q", cfg.LocalAddr)
	}
	if cfg.JunkPackets != 5 {
		t.Errorf("junk_packets = %d, want 5", cfg.JunkPackets)
	}
	if cfg.PinnedCertSHA256 != "abc123" {
		t.Errorf("pinned_cert_sha256 = %q", cfg.PinnedCertSHA256)
	}
}

// TestPSKsOmittedMeansAutoDerive covers the TLS-mode default: no psk in
// the file is a valid configuration, not an error, because the key comes
// from the TLS session instead.
func TestPSKsOmittedMeansAutoDerive(t *testing.T) {
	path := writeConfig(t, "mode: tls\nlocal_addr: \"127.0.0.1:51821\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cfg.PSKs()
	if err != nil {
		t.Fatalf("an omitted psk must not be an error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected no keys, got %d", len(keys))
	}
}

func TestPSKsCurrentAndPrevious(t *testing.T) {
	path := writeConfig(t, `
psk: "`+validKey(9)+`"
psk_previous: "`+validKey(7)+`"
local_addr: "127.0.0.1:51821"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cfg.PSKs()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected current + previous key, got %d", len(keys))
	}
	// Order matters: the current key must be first, since Wrap uses it.
	if keys[0][0] != 9 {
		t.Errorf("current key is not first: leading byte %d", keys[0][0])
	}
	if keys[1][0] != 7 {
		t.Errorf("previous key is not second: leading byte %d", keys[1][0])
	}
}

func TestPSKRejectsWrongLength(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	path := writeConfig(t, "psk: \""+short+"\"\nlocal_addr: \"127.0.0.1:1\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PSKs(); err == nil {
		t.Fatal("expected a key that isn't 32 bytes to be rejected")
	}
}

func TestPSKRejectsInvalidBase64(t *testing.T) {
	path := writeConfig(t, "psk: \"not base64 at all!!\"\nlocal_addr: \"127.0.0.1:1\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PSKs(); err == nil {
		t.Fatal("expected invalid base64 to be rejected")
	}
}

func TestPSKPreviousRejectsInvalidKey(t *testing.T) {
	path := writeConfig(t, `
psk: "`+validKey(1)+`"
psk_previous: "short"
local_addr: "127.0.0.1:1"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PSKs(); err == nil {
		t.Fatal("expected an invalid psk_previous to be rejected rather than silently ignored")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := writeConfig(t, "psk: [this is not a string\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected malformed YAML to be rejected")
	}
}
