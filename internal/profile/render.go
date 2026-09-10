package profile

import (
	"fmt"
	"strings"
)

// WireGuardConfig renders the tunnel half of a profile as a wg-quick
// config, so a device that imported a link can hand WireGuard the file it
// expects without anyone retyping keys.
//
// The Endpoint written here is the local obfuscator, never the server.
// That is the whole arrangement: WireGuard talks to something on
// loopback, and that something carries the traffic onward, obfuscated. A
// config pointing at the server would work and would defeat the purpose,
// which is why the parser refuses to read one.
func (p Profile) WireGuardConfig() string {
	var b strings.Builder

	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", p.WireGuard.PrivateKey)
	fmt.Fprintf(&b, "Address = %s\n", p.WireGuard.Address)
	if p.WireGuard.DNS != "" {
		fmt.Fprintf(&b, "DNS = %s\n", p.WireGuard.DNS)
	}
	b.WriteString("\n# Leaves room for the obfuscator's overhead. Raising it does not make\n")
	b.WriteString("# the tunnel faster; it makes large packets disappear.\n")
	fmt.Fprintf(&b, "MTU = %d\n", p.WireGuard.MTU)

	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", p.WireGuard.PeerPublicKey)
	if p.WireGuard.AllowedIPs != "" {
		fmt.Fprintf(&b, "AllowedIPs = %s\n", p.WireGuard.AllowedIPs)
	}
	b.WriteString("\n# Not the server. WireGuard talks to the local obfuscator, which is\n")
	b.WriteString("# what reaches the server.\n")
	fmt.Fprintf(&b, "Endpoint = %s\n", p.Transport.LocalAddr)
	if p.WireGuard.PersistentKeepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", p.WireGuard.PersistentKeepalive)
	}
	return b.String()
}
