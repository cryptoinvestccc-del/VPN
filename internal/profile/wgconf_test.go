package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sampleClientConfig = `[Interface]
PrivateKey = ` + `PRIVATE
Address = 10.8.0.2/32
DNS = 1.1.1.1

# Must match the server: the obfuscator adds 42 bytes to every packet.
MTU = 1376

[Peer]
PublicKey = ` + `PUBLIC
AllowedIPs = 0.0.0.0/0, ::/0

# Not the server's address.
Endpoint = 127.0.0.1:51821
PersistentKeepalive = 25
`

func sampleConfig() string {
	return strings.NewReplacer("PRIVATE", key(2), "PUBLIC", key(3)).Replace(sampleClientConfig)
}

func TestParseWireGuardConfig(t *testing.T) {
	w, local, err := ParseWireGuardConfig(sampleConfig())
	if err != nil {
		t.Fatal(err)
	}
	if local != "127.0.0.1:51821" {
		t.Errorf("local endpoint: got %q, want 127.0.0.1:51821", local)
	}

	want := WireGuard{
		PrivateKey:          key(2),
		Address:             "10.8.0.2/32",
		DNS:                 "1.1.1.1",
		PeerPublicKey:       key(3),
		AllowedIPs:          "0.0.0.0/0, ::/0",
		MTU:                 1376,
		PersistentKeepalive: 25,
	}
	if w != want {
		t.Errorf("parsed:\n got %+v\nwant %+v", w, want)
	}
	if err := validateWireGuard(w); err != nil {
		t.Errorf("a config this project generates did not pass its own validation: %v", err)
	}
}

// TestParsesWhatTheProvisioningScriptActuallyWrites closes the loop that
// matters. The sample above is a copy, and a copy drifts; this runs the
// real script and parses its real output, so a change to either side has
// to keep them agreeing.
func TestParsesWhatTheProvisioningScriptActuallyWrites(t *testing.T) {
	script, err := filepath.Abs("../../deploy/provision-wireguard.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skipf("deployment script not available: %v", err)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}

	// The script's dry run prints the configs it would write without
	// touching the system, which is exactly what a test may use.
	out, err := exec.Command("bash", script, "add-client", "profile-test", "vpn.example.com", "--dry-run").CombinedOutput()
	if err != nil {
		t.Fatalf("the provisioning script failed (%v):\n%s", err, out)
	}

	config := extractClientConfig(string(out))
	if config == "" {
		t.Fatalf("could not find a client config in the script's output:\n%s", out)
	}

	w, _, err := ParseWireGuardConfig(config)
	if err != nil {
		t.Fatalf("the script writes a client config this parser rejects: %v\n\n%s", err, config)
	}
	if w.MTU != MaxTunnelMTU {
		t.Errorf("the script provisions MTU %d, profiles are capped at %d", w.MTU, MaxTunnelMTU)
	}
	if w.PeerPublicKey == "" || w.PrivateKey == "" {
		t.Errorf("the parsed config carries no keys: %+v", w)
	}
}

// extractClientConfig pulls the [Interface]...[Peer] block out of the
// script's human-readable output. It takes the block that has a [Peer]
// section, which is what distinguishes a client config from the server's.
func extractClientConfig(out string) string {
	var (
		block   []string
		current []string
	)
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "[Interface]":
			current = []string{trimmed}
		case strings.HasPrefix(trimmed, "===") || strings.HasPrefix(trimmed, "---"):
			if containsPeer(current) {
				block = current
			}
			current = nil
		case current != nil:
			current = append(current, trimmed)
		}
	}
	if containsPeer(current) {
		block = current
	}
	return strings.Join(block, "\n")
}

func containsPeer(lines []string) bool {
	for _, l := range lines {
		if l == "[Peer]" {
			return true
		}
	}
	return false
}

func TestParseWireGuardConfigRejectsFaults(t *testing.T) {
	cases := map[string]struct {
		config string
		expect string
	}{
		"no endpoint": {
			config: "[Interface]\nMTU = 1376\n[Peer]\nPublicKey = x\n",
			expect: "no Endpoint",
		},
		"no peer": {
			config: "[Interface]\nPrivateKey = k\nMTU = 1376\n",
			expect: "no [Peer]",
		},
		"two peers": {
			config: sampleConfig() + "\n[Peer]\nPublicKey = " + key(4) + "\nAllowedIPs = 10.0.0.0/8\n",
			expect: "one tunnel",
		},
		"no MTU": {
			config: strings.ReplaceAll(sampleConfig(), "MTU = 1376", ""),
			expect: "sets no MTU",
		},
		"MTU not a number": {
			config: strings.ReplaceAll(sampleConfig(), "MTU = 1376", "MTU = large"),
			expect: "not a number",
		},
		"key before any section": {
			config: "PrivateKey = k\n[Interface]\n",
			expect: "before any",
		},
		"garbage line": {
			config: "[Interface]\nthis is not a setting\n",
			expect: "neither a section",
		},
	}

	for name, tc := range cases {
		_, _, err := ParseWireGuardConfig(tc.config)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.expect) {
			t.Errorf("%s: message does not explain the fault: %v", name, err)
		}
	}
}

// TestRejectsAPlainWireGuardConfig is the fault worth catching most. A
// config whose Endpoint is the real server describes a tunnel that works
// — and carries traffic in the clear, past everything this project
// exists to do. It has to be refused rather than imported.
func TestRejectsAPlainWireGuardConfig(t *testing.T) {
	plain := strings.ReplaceAll(sampleConfig(), "Endpoint = 127.0.0.1:51821", "Endpoint = vpn.example.com:51820")

	_, _, err := ParseWireGuardConfig(plain)
	if err == nil {
		t.Fatal("a config pointing straight at the server was accepted")
	}
	if !strings.Contains(err.Error(), "bypass the obfuscation") {
		t.Errorf("the message does not say what is at stake: %v", err)
	}
}

func TestCommentsAndCaseAreTolerated(t *testing.T) {
	odd := `[interface]
	privatekey=` + key(2) + `   # trailing comment
	ADDRESS = 10.8.0.2/32
	mtu   =   1376
[PEER]
	PUBLICKEY = ` + key(3) + `
	allowedips = 0.0.0.0/0,::/0
	endpoint = localhost:51821
`
	w, _, err := ParseWireGuardConfig(odd)
	if err != nil {
		t.Fatal(err)
	}
	if w.PrivateKey != key(2) || w.MTU != 1376 || w.AllowedIPs != "0.0.0.0/0, ::/0" {
		t.Errorf("case and spacing changed the result: %+v", w)
	}
}

// TestRenderAndParseAgree is the property that keeps the two halves
// honest. A profile is written out as a WireGuard config on one device
// and read back on another; if rendering and parsing ever disagreed, a
// value would change in transit without anybody being told.
func TestRenderAndParseAgree(t *testing.T) {
	p := validTLS()
	p.Transport.LocalAddr = "127.0.0.1:51821"

	back, local, err := ParseWireGuardConfig(p.WireGuardConfig())
	if err != nil {
		t.Fatalf("a config we rendered ourselves does not parse: %v\n\n%s", err, p.WireGuardConfig())
	}
	if back != p.WireGuard {
		t.Errorf("the tunnel changed on the way through:\n got %+v\nwant %+v", back, p.WireGuard)
	}
	if local != p.Transport.LocalAddr {
		t.Errorf("local address: got %q, want %q", local, p.Transport.LocalAddr)
	}
}

// TestFullTunnelConfigExcludesTheServerRoute is the invariant behind the
// whole arrangement. The obfuscator is a separate process, so its socket
// carries none of the firewall mark WireGuard uses to keep its own
// packets out of the tunnel. With AllowedIPs 0.0.0.0/0 and no host route,
// its connection to the server is routed into the tunnel it is carrying,
// and the tunnel never comes up.
//
// Nothing catches this in a loopback test, which is why it survived every
// check until somebody asked how to actually use the thing.
func TestFullTunnelConfigExcludesTheServerRoute(t *testing.T) {
	p := validTLS()
	p.Transport.EndpointIP = "198.51.100.7"

	config := p.WireGuardConfig()
	if !strings.Contains(config, "AllowedIPs = 0.0.0.0/0") {
		t.Fatal("this test is about full-tunnel configs and the baseline is not one")
	}
	if !strings.Contains(config, "PostUp = ip route add 198.51.100.7/32") {
		t.Errorf("a full-tunnel config carries no host route to the server:\n%s", config)
	}
	if !strings.Contains(config, "PostDown = ip route del 198.51.100.7/32") {
		t.Errorf("the route is added but never removed:\n%s", config)
	}

	// Still has to parse: the hooks must not break import on the other side.
	if _, _, err := ParseWireGuardConfig(config); err != nil {
		t.Errorf("the config with route hooks no longer parses: %v", err)
	}
}

func TestIPv6ServerGetsAnIPv6Route(t *testing.T) {
	p := validTLS()
	p.Transport.Endpoint = "[2001:db8::1]:443"
	p.Transport.EndpointIP = "2001:db8::1"

	config := p.WireGuardConfig()
	if !strings.Contains(config, "ip -6 route add 2001:db8::1/128") {
		t.Errorf("an IPv6 server did not get an IPv6 host route:\n%s", config)
	}
}

// TestMissingServerIPIsLoudInTheConfig: if the address could not be
// resolved the route cannot be written, and a config that silently omits
// it produces a tunnel that never comes up for no visible reason.
func TestMissingServerIPIsLoudInTheConfig(t *testing.T) {
	p := validTLS()
	p.Transport.EndpointIP = ""

	config := p.WireGuardConfig()
	if !strings.Contains(config, "WARNING") || !strings.Contains(config, "will not come up") {
		t.Errorf("a config without the route says nothing about it:\n%s", config)
	}
}

// TestScriptWritesTheRouteExclusion holds the deployment script to the
// same rule, since it generates client configs without going through the
// profile package at all.
func TestScriptWritesTheRouteExclusion(t *testing.T) {
	script, err := filepath.Abs("../../deploy/provision-wireguard.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skipf("deployment script not available: %v", err)
	}

	out, err := exec.Command("bash", script, "add-client", "route-test", "198.51.100.7", "--dry-run").CombinedOutput()
	if err != nil {
		t.Fatalf("the provisioning script failed (%v):\n%s", err, out)
	}

	config := extractClientConfig(string(out))
	if config == "" {
		t.Fatalf("no client config in the script's output:\n%s", out)
	}
	if !strings.Contains(config, "ip route add 198.51.100.7/32") {
		t.Errorf("the script writes a full-tunnel config with no route to the server:\n%s", config)
	}
}
