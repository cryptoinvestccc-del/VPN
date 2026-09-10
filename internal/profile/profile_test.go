package profile

import (
	"encoding/base64"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func key(b byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func pin() string {
	return strings.Repeat("ab", 32)
}

func validTLS() Profile {
	return Profile{
		Version:  Version,
		Name:     "Frankfurt",
		ClientID: "alice",
		Transport: Transport{
			Mode:             "tls",
			Endpoint:         "vpn.example.com:443",
			LocalAddr:        "127.0.0.1:51821",
			PSKBase64:        key(1),
			ServerName:       "vpn.example.com",
			PinnedCertSHA256: pin(),
		},
		WireGuard: WireGuard{
			PrivateKey:          key(2),
			Address:             "10.8.0.2/32",
			DNS:                 "1.1.1.1, 9.9.9.9",
			PeerPublicKey:       key(3),
			AllowedIPs:          "0.0.0.0/0, ::/0",
			MTU:                 MaxTunnelMTU,
			PersistentKeepalive: 25,
		},
	}
}

func validUDP() Profile {
	p := validTLS()
	p.Transport.Mode = "udp"
	p.Transport.ServerName = ""
	p.Transport.PinnedCertSHA256 = ""
	p.Transport.JunkPackets = 3
	return p
}

func TestRoundTrip(t *testing.T) {
	for _, p := range []Profile{validTLS(), validUDP()} {
		encoded, err := Encode(p)
		if err != nil {
			t.Fatalf("encode %s: %v", p.Transport.Mode, err)
		}
		back, err := Decode(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", p.Transport.Mode, err)
		}
		if back != p {
			t.Errorf("profile did not survive the round trip:\n got %+v\nwant %+v", back, p)
		}
	}
}

// TestEncodedFormSurvivesAChatWindow is the point of the single-line
// form. If it needed quoting, wrapped across lines, or grew past what a
// message box accepts, people would go back to sending three files.
func TestEncodedFormSurvivesAChatWindow(t *testing.T) {
	encoded, err := Encode(validTLS())
	if err != nil {
		t.Fatal(err)
	}

	if !regexp.MustCompile(`^obfsvpn://v1/[A-Za-z0-9_-]+$`).MatchString(encoded) {
		t.Errorf("the link contains characters that would need escaping: %q", encoded)
	}
	if len(encoded) > 800 {
		t.Errorf("the link is %d characters, long enough that clients will wrap or truncate it", len(encoded))
	}

	// Pasting picks up whitespace; that is not the user's mistake to pay
	// for.
	if _, err := Decode("  \n" + encoded + "\n "); err != nil {
		t.Errorf("a pasted link with surrounding whitespace was rejected: %v", err)
	}
}

func TestStringPrintsNoSecrets(t *testing.T) {
	p := validTLS()
	out := p.String()

	for name, secret := range map[string]string{
		"pre-shared key":        p.Transport.PSKBase64,
		"wireguard private key": p.WireGuard.PrivateKey,
	} {
		if strings.Contains(out, secret) {
			t.Errorf("String printed the %s: %q", name, out)
		}
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "vpn.example.com:443") {
		t.Errorf("String left out what makes a profile identifiable: %q", out)
	}
}

func TestDecodeRejectsDamagedInput(t *testing.T) {
	good, err := Encode(validTLS())
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"empty":                  "",
		"not a link":             "hello",
		"wrong scheme":           "https://example.com/",
		"future version":         "obfsvpn://v9/abcdef",
		"truncated payload":      good[:len(good)-20],
		"not base64":             uriPrefix + "!!!!",
		"base64 but not deflate": uriPrefix + base64.RawURLEncoding.EncodeToString([]byte("plain text")),
	}

	for name, input := range cases {
		if _, err := Decode(input); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestDecodeRefusesADecompressionBomb: the payload arrives from whoever
// sent the link, and a few hundred compressed bytes can expand to
// gigabytes. The limit has to apply while it expands, not after.
func TestDecodeRefusesADecompressionBomb(t *testing.T) {
	var zeros = make([]byte, 64<<20)
	bomb := deflateForTest(t, zeros)
	if len(bomb) > 100<<10 {
		t.Fatalf("test bomb is %d bytes compressed, too large to be a realistic paste", len(bomb))
	}

	if _, err := Decode(uriPrefix + base64.RawURLEncoding.EncodeToString(bomb)); err == nil {
		t.Fatal("a 64 MiB payload was accepted")
	}
}

func TestValidateCatchesConfigurationFaults(t *testing.T) {
	// Without this, a fault in the baseline would make every rejection
	// below pass for the wrong reason: the table would be testing that
	// an already-invalid profile stays invalid.
	if err := Validate(validTLS()); err != nil {
		t.Fatalf("the baseline profile is itself invalid, so the table below proves nothing: %v", err)
	}
	if err := Validate(validUDP()); err != nil {
		t.Fatalf("the udp baseline is itself invalid: %v", err)
	}

	cases := map[string]func(*Profile){
		"missing mode":             func(p *Profile) { p.Transport.Mode = "" },
		"unknown mode":             func(p *Profile) { p.Transport.Mode = "quic" },
		"endpoint without a port":  func(p *Profile) { p.Transport.Endpoint = "vpn.example.com" },
		"endpoint port zero":       func(p *Profile) { p.Transport.Endpoint = "vpn.example.com:0" },
		"missing psk":              func(p *Profile) { p.Transport.PSKBase64 = "" },
		"short psk":                func(p *Profile) { p.Transport.PSKBase64 = base64.StdEncoding.EncodeToString([]byte("short")) },
		"tls without a pin":        func(p *Profile) { p.Transport.PinnedCertSHA256 = "" },
		"pin of the wrong length":  func(p *Profile) { p.Transport.PinnedCertSHA256 = "abcd" },
		"pin that is not hex":      func(p *Profile) { p.Transport.PinnedCertSHA256 = strings.Repeat("zz", 32) },
		"missing private key":      func(p *Profile) { p.WireGuard.PrivateKey = "" },
		"missing peer key":         func(p *Profile) { p.WireGuard.PeerPublicKey = "" },
		"address without prefix":   func(p *Profile) { p.WireGuard.Address = "10.8.0.2" },
		"allowed IPs not a prefix": func(p *Profile) { p.WireGuard.AllowedIPs = "0.0.0.0" },
		"DNS that is a hostname":   func(p *Profile) { p.WireGuard.DNS = "dns.example.com" },
		"missing MTU":              func(p *Profile) { p.WireGuard.MTU = 0 },
		"MTU above the budget":     func(p *Profile) { p.WireGuard.MTU = 1500 },
		"absurdly small MTU":       func(p *Profile) { p.WireGuard.MTU = 200 },
		"negative keepalive":       func(p *Profile) { p.WireGuard.PersistentKeepalive = -1 },
		"too many junk packets":    func(p *Profile) { p.Transport.JunkPackets = 1000 },
	}

	for name, break_ := range cases {
		p := validTLS()
		break_(&p)
		if err := Validate(p); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if _, err := Encode(p); err == nil {
			t.Errorf("%s was encoded anyway", name)
		}
	}
}

// TestPinInUDPModeIsRejected guards against the worst kind of
// misconfiguration: one that leaves its owner believing in a protection
// that is not running. Nothing in udp mode checks a certificate pin, so
// carrying one is not a harmless leftover.
func TestPinInUDPModeIsRejected(t *testing.T) {
	p := validUDP()
	p.Transport.PinnedCertSHA256 = pin()

	err := Validate(p)
	if err == nil {
		t.Fatal("a udp profile carrying a certificate pin was accepted")
	}
	if !strings.Contains(err.Error(), "nothing checks it") {
		t.Errorf("the message does not explain why this matters: %v", err)
	}
}

// TestMaxTunnelMTUMatchesWhatWeProvision: the deployment script writes an
// MTU into every generated WireGuard config, and this package rejects
// profiles above its own derived limit. If the two ever disagree, the
// tooling would produce configs its own importer refuses — or worse,
// accept one that silently drops large packets.
func TestMaxTunnelMTUMatchesWhatWeProvision(t *testing.T) {
	script, err := os.ReadFile("../../deploy/provision-wireguard.sh")
	if err != nil {
		t.Skipf("deployment script not readable here: %v", err)
	}

	m := regexp.MustCompile(`(?m)^WG_MTU=(\d+)`).FindSubmatch(script)
	if m == nil {
		t.Fatal("could not find WG_MTU in the deployment script")
	}
	provisioned, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}

	if provisioned != MaxTunnelMTU {
		t.Fatalf("the deployment script provisions MTU %d but profiles are capped at %d; "+
			"one of the two is wrong and the symptom would be large packets disappearing",
			provisioned, MaxTunnelMTU)
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	// A field this build does not understand means the profile expects
	// behaviour that will not happen. Ignoring it silently is how a
	// client ends up connected on terms nobody chose.
	payload := `{"v":1,"transport":{"mode":"udp","endpoint":"a:1","local_addr":"127.0.0.1:9","psk":"` + key(1) +
		`"},"wireguard":{"private_key":"` + key(2) + `","address":"10.0.0.2/32","peer_public_key":"` +
		key(3) + `","mtu":1376},"require_dnssec":true}`

	if _, err := Decode(uriPrefix + base64.RawURLEncoding.EncodeToString(deflateForTest(t, []byte(payload)))); err == nil {
		t.Fatal("a profile with an unrecognised field was accepted")
	}
}
