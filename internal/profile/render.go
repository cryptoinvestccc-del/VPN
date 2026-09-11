package profile

import (
	"fmt"
	"net/netip"
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
// Style selects which of the two incompatible dialects of wg-quick
// config to write.
//
// They differ over the route that keeps the obfuscator's own traffic out
// of the tunnel. wg-quick takes shell hooks and does it with a host
// route. The WireGuard mobile apps take no hooks at all — their parser
// rejects a key it does not know, so a config carrying PostUp fails to
// import — and solve the same problem by excluding an application from
// the tunnel instead.
//
// One config cannot serve both, and guessing wrong fails in a way that
// looks like a corrupt file.
type Style int

const (
	// StyleWgQuick is for wg-quick on Linux, macOS and BSD: hooks
	// allowed, and needed.
	StyleWgQuick Style = iota

	// StyleApp is for the WireGuard Android and iOS apps: no hooks, and
	// the route exclusion is the user's job through the app's excluded
	// applications setting.
	StyleApp
)

// WireGuardConfig renders in the wg-quick dialect, which is what a
// desktop expects.
func (p Profile) WireGuardConfig() string { return p.WireGuardConfigFor(StyleWgQuick) }

// WireGuardConfigFor renders in the dialect a given client understands.
func (p Profile) WireGuardConfigFor(style Style) string {
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
	if style == StyleWgQuick {
		b.WriteString(p.routeExclusion())
	} else {
		b.WriteString(appRouteNote)
	}

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

// routeExclusion writes the [Interface] hooks that keep the obfuscator's
// own connection to the server out of the tunnel.
//
// Without it nothing works at all on a device that routes everything
// through the tunnel. AllowedIPs 0.0.0.0/0 makes wg-quick install a rule
// sending every unmarked packet into the tunnel; WireGuard's own
// encrypted packets escape it because WireGuard marks its socket, but the
// obfuscator is a separate process with an unmarked socket, so its
// packets to the server are routed into the tunnel it is itself
// carrying. The tunnel then never comes up, and the symptom — a
// connection that simply does not establish — says nothing about the
// cause.
//
// A host route is the fix: the rule that consults the main table
// suppresses only default routes, so a /32 or /128 there is honoured and
// the packet leaves by the physical interface.
func (p Profile) routeExclusion() string {
	if p.Transport.EndpointIP == "" {
		return `
# WARNING: this profile carries no server IP, so the route that keeps the
# obfuscator's own traffic out of the tunnel could not be written.
#
# With AllowedIPs 0.0.0.0/0 below, the obfuscator's connection to the
# server will be routed into the tunnel it is carrying, and the tunnel
# will not come up. Add the route by hand, replacing SERVER_IP:
#
#   PostUp = ip route add SERVER_IP via $(ip route show default | awk '{print $3; exit}') || true
#   PostDown = ip route del SERVER_IP || true
`
	}

	ip, err := netip.ParseAddr(p.Transport.EndpointIP)
	if err != nil {
		return ""
	}
	cmd, prefix := "ip route", "/32"
	if ip.Is6() {
		cmd, prefix = "ip -6 route", "/128"
	}

	var b strings.Builder
	b.WriteString("\n# Keeps the obfuscator's own connection to the server outside the\n")
	b.WriteString("# tunnel. Without this the tunnel would carry the traffic that is\n")
	b.WriteString("# carrying it, and would never come up. Harmless to re-run.\n")
	fmt.Fprintf(&b, "PostUp = %s add %s%s via $(%s show default | awk '{print $3; exit}') || true\n",
		cmd, p.Transport.EndpointIP, prefix, cmd)
	fmt.Fprintf(&b, "PostDown = %s del %s%s || true\n", cmd, p.Transport.EndpointIP, prefix)
	return b.String()
}

// appRouteNote stands in for the hooks a mobile app will not accept. The
// problem the hooks solve does not go away on a phone: without excluding
// the obfuscator from the tunnel, its traffic is captured by the very
// tunnel it carries. The app cannot be told this in a config, so the
// person has to be.
const appRouteNote = `
# This config carries no PostUp hooks: the WireGuard apps reject keys they
# do not recognise, so a config with them fails to import.
#
# The problem they solve still applies. In the WireGuard app, open this
# tunnel's settings and EXCLUDE the application running the obfuscator
# from the tunnel. Without that its traffic is captured by the tunnel it
# is carrying, and nothing connects.
`
