package profile

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Validate rejects a profile that would produce a tunnel that does not
// work, or one that works but is not protected the way its owner
// believes.
//
// The checks are deliberately strict at the point of import. Every fault
// caught here has a symptom that appears somewhere else entirely: a wrong
// pin looks like an unreachable server, an oversized MTU looks like a
// site that will not load, a missing PSK looks like nothing at all until
// somebody notices the port is open to anyone.
func Validate(p Profile) error {
	if p.Version != Version {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, p.Version)
	}
	if err := validateTransport(p.Transport); err != nil {
		return err
	}
	return validateWireGuard(p.WireGuard)
}

func validateTransport(t Transport) error {
	switch t.Mode {
	case "udp", "tls":
	case "":
		return fmt.Errorf("profile: transport mode is missing; it must be \"udp\" or \"tls\"")
	default:
		return fmt.Errorf("profile: unknown transport mode %q; it must be \"udp\" or \"tls\"", t.Mode)
	}

	if err := validateHostPort(t.Endpoint, "endpoint"); err != nil {
		return err
	}
	if err := validateHostPort(t.LocalAddr, "local address"); err != nil {
		return err
	}
	if !isLoopbackEndpoint(t.LocalAddr) {
		// This listener takes plaintext WireGuard and forwards it into
		// the tunnel. There is nothing in front of it, so exposing it
		// beyond the device is an open door, not a looser setting.
		return fmt.Errorf("profile: local address %s is not on loopback; it accepts plaintext WireGuard "+
			"with nothing authenticating it, so it must not be reachable from the network", t.LocalAddr)
	}
	if _, err := decodeKey32(t.PSKBase64); err != nil {
		return fmt.Errorf("profile: pre-shared key: %w", err)
	}

	switch t.Mode {
	case "tls":
		if t.PinnedCertSHA256 == "" {
			// Not a lax setting but a missing one: this design pins
			// instead of trusting a certificate authority, so without a
			// pin there is nothing at all authenticating the server.
			return fmt.Errorf("profile: tls mode needs a certificate pin; without one the client would accept any server that answers")
		}
		pin := strings.ToLower(strings.ReplaceAll(t.PinnedCertSHA256, ":", ""))
		raw, err := hex.DecodeString(pin)
		if err != nil {
			return fmt.Errorf("profile: certificate pin is not hex: %v", err)
		}
		if len(raw) != 32 {
			return fmt.Errorf("profile: certificate pin is %d bytes, a SHA-256 fingerprint is 32", len(raw))
		}
	case "udp":
		if t.PinnedCertSHA256 != "" {
			// Carrying a pin in a mode that cannot check one leaves its
			// owner believing in a protection that is not running.
			return fmt.Errorf("profile: a certificate pin is set but the mode is udp, where nothing checks it; use tls mode or remove the pin")
		}
		if t.ServerName != "" {
			return fmt.Errorf("profile: a server name is set but the mode is udp, where no TLS handshake happens")
		}
	}

	if t.JunkPackets < 0 || t.JunkPackets > 32 {
		return fmt.Errorf("profile: junk packets is %d; it must be between 0 and 32", t.JunkPackets)
	}
	return nil
}

func validateWireGuard(w WireGuard) error {
	if _, err := decodeKey32(w.PrivateKey); err != nil {
		return fmt.Errorf("profile: wireguard private key: %w", err)
	}
	if _, err := decodeKey32(w.PeerPublicKey); err != nil {
		return fmt.Errorf("profile: wireguard peer public key: %w", err)
	}

	if w.Address == "" {
		return fmt.Errorf("profile: wireguard address is missing")
	}
	for _, a := range splitList(w.Address) {
		if _, err := netip.ParsePrefix(a); err != nil {
			return fmt.Errorf("profile: wireguard address %q is not an address with a prefix length, such as 10.8.0.2/32", a)
		}
	}

	for _, a := range splitList(w.AllowedIPs) {
		if _, err := netip.ParsePrefix(a); err != nil {
			return fmt.Errorf("profile: allowed IPs entry %q is not a prefix, such as 0.0.0.0/0", a)
		}
	}

	for _, d := range splitList(w.DNS) {
		if _, err := netip.ParseAddr(d); err != nil {
			return fmt.Errorf("profile: DNS entry %q is not an IP address", d)
		}
	}

	if w.MTU <= 0 {
		return fmt.Errorf("profile: MTU is missing; on this transport it must be at most %d", MaxTunnelMTU)
	}
	if w.MTU < 576 {
		return fmt.Errorf("profile: MTU %d is below 576, which is smaller than IPv4 requires any path to carry", w.MTU)
	}
	if w.MTU > MaxTunnelMTU {
		// The symptom of getting this wrong is not an error: small
		// packets go through and large ones disappear, so a browser
		// hangs on some sites and not others.
		return fmt.Errorf("profile: MTU %d leaves no room for the obfuscator; it must be at most %d, "+
			"or full-size packets will be fragmented or dropped on the way", w.MTU, MaxTunnelMTU)
	}

	if w.PersistentKeepalive < 0 || w.PersistentKeepalive > 3600 {
		return fmt.Errorf("profile: persistent keepalive is %d seconds; it must be between 0 and 3600", w.PersistentKeepalive)
	}
	return nil
}

func validateHostPort(addr, field string) error {
	if addr == "" {
		return fmt.Errorf("profile: %s is missing", field)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("profile: %s %q is not host:port", field, addr)
	}
	if host == "" {
		return fmt.Errorf("profile: %s %q has no host", field, addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("profile: %s %q does not have a valid port", field, addr)
	}
	return nil
}

func decodeKey32(s string) ([32]byte, error) {
	var key [32]byte
	if s == "" {
		return key, fmt.Errorf("missing")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return key, fmt.Errorf("not base64: %v", err)
	}
	if len(raw) != 32 {
		return key, fmt.Errorf("decodes to %d bytes, a key is 32", len(raw))
	}
	copy(key[:], raw)
	return key, nil
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
