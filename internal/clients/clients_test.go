package clients

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func key(b byte) string {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return base64.StdEncoding.EncodeToString(k[:])
}

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clients.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadEnabledAndRevoked(t *testing.T) {
	path := writeFile(t, `
clients:
  - id: laptop
    psk: "`+key(1)+`"
  - id: phone
    psk: "`+key(2)+`"
    disabled: true
  - id: tablet
    psk: "`+key(3)+`"
`)

	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if got := set.Count(); got != 2 {
		t.Fatalf("expected 2 enabled clients, got %d", got)
	}
	if !set.IsEnabled("laptop") || !set.IsEnabled("tablet") {
		t.Fatal("an enabled client was not reported as enabled")
	}
	if set.IsEnabled("phone") {
		t.Fatal("a revoked client is still enabled")
	}

	// A revoked client's key must not be among the candidates, or
	// revocation would be cosmetic.
	for _, c := range set.Credentials() {
		if c.ClientID == "phone" {
			t.Fatal("a revoked client's key is still accepted")
		}
	}
}

func TestPerClientKeyRotation(t *testing.T) {
	path := writeFile(t, `
clients:
  - id: laptop
    psk: "`+key(9)+`"
    psk_previous: "`+key(8)+`"
`)

	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	credentials := set.Credentials()
	if len(credentials) != 2 {
		t.Fatalf("expected the current and previous key, got %d", len(credentials))
	}
	// Order matters: the current key is tried first.
	if credentials[0].Key[0] != 9 || credentials[1].Key[0] != 8 {
		t.Fatal("keys are not ordered current-first")
	}
	for _, c := range credentials {
		if c.ClientID != "laptop" {
			t.Fatalf("a rotation key was attributed to %q", c.ClientID)
		}
	}
}

func TestDuplicateIDsRejected(t *testing.T) {
	path := writeFile(t, `
clients:
  - id: laptop
    psk: "`+key(1)+`"
  - id: laptop
    psk: "`+key(2)+`"
`)

	if _, err := Load(path); err == nil {
		t.Fatal("two clients sharing an id must be rejected: revocation could not tell them apart")
	}
}

func TestMalformedKeysRejected(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"short key", "clients:\n  - id: a\n    psk: \"" +
			base64.StdEncoding.EncodeToString([]byte("too short")) + "\"\n"},
		{"not base64", "clients:\n  - id: a\n    psk: \"not base64!!\"\n"},
		{"missing key", "clients:\n  - id: a\n"},
		{"missing id", "clients:\n  - psk: \"" + key(1) + "\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeFile(t, tc.body)); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

// TestRevokedClientKeyIsStillValidated covers a trap: a malformed key in a
// disabled entry must be reported now, not lie dormant until the operator
// restores that client and the server refuses to start.
func TestRevokedClientKeyIsStillValidated(t *testing.T) {
	path := writeFile(t, `
clients:
  - id: good
    psk: "`+key(1)+`"
  - id: broken
    psk: "not base64!!"
    disabled: true
`)

	if _, err := Load(path); err == nil {
		t.Fatal("a malformed key in a revoked entry was not reported")
	}
}

func TestEmptySetRejected(t *testing.T) {
	if _, err := Load(writeFile(t, "clients: []\n")); err == nil {
		t.Fatal("a set with no enabled clients would refuse every connection and must be rejected")
	}

	path := writeFile(t, "clients:\n  - id: only\n    psk: \""+key(1)+"\"\n    disabled: true\n")
	if _, err := Load(path); err == nil {
		t.Fatal("a set where every client is revoked must be rejected too")
	}
}

// TestReloadKeepsPreviousSetOnError is what stands between a typo and an
// outage for everyone: a bad credential file must leave the running
// configuration untouched.
func TestReloadKeepsPreviousSetOnError(t *testing.T) {
	path := writeFile(t, "clients:\n  - id: laptop\n    psk: \""+key(1)+"\"\n")

	registry, err := NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Current().IsEnabled("laptop") {
		t.Fatal("the initial set was not loaded")
	}

	if err := os.WriteFile(path, []byte("clients:\n  - id: laptop\n    psk: \"broken\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := registry.Reload(); err == nil {
		t.Fatal("a malformed file reloaded without error")
	}
	if !registry.Current().IsEnabled("laptop") {
		t.Fatal("a failed reload dropped the working credentials, locking every client out")
	}
}

func TestReloadPicksUpRevocation(t *testing.T) {
	path := writeFile(t, `
clients:
  - id: laptop
    psk: "`+key(1)+`"
  - id: phone
    psk: "`+key(2)+`"
`)

	registry, err := NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Current().IsEnabled("phone") {
		t.Fatal("phone should start enabled")
	}

	revoked := "clients:\n  - id: laptop\n    psk: \"" + key(1) + "\"\n" +
		"  - id: phone\n    psk: \"" + key(2) + "\"\n    disabled: true\n"
	if err := os.WriteFile(path, []byte(revoked), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := registry.Reload(); err != nil {
		t.Fatal(err)
	}

	if registry.Current().IsEnabled("phone") {
		t.Fatal("a revocation did not survive the reload")
	}
	if !registry.Current().IsEnabled("laptop") {
		t.Fatal("revoking one client disabled another")
	}
}

func TestStaticRegistryHasNoFileToReload(t *testing.T) {
	set, err := NewSetFromCredentials([]Credential{{ClientID: "a", Key: [32]byte{1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewStaticRegistry(set).Reload(); err == nil {
		t.Fatal("a registry with no file should report that it cannot reload")
	}
}
