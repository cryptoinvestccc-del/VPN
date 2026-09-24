package provision

import (
	"fmt"
	"sort"
	"strings"
)

// ClientConfig renders an AmneziaWG configuration for one device.
//
// The private key is the caller's to supply, because this service never
// had it: the device generated its own and sent only the public half.
// Passing an empty string leaves the field blank for an app that fills
// it in itself.
func (c Config) ClientConfig(privateKey string) string {
	s := c.Settings
	var b strings.Builder

	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", privateKey)
	fmt.Fprintf(&b, "Address = %s\n", c.Address)
	if len(s.DNS) > 0 {
		names := make([]string, 0, len(s.DNS))
		for _, d := range s.DNS {
			names = append(names, d.String())
		}
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(names, ", "))
	}
	if s.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", s.MTU)
	}

	// The obfuscation parameters have to match the server exactly. They
	// are written from what the server's own configuration says, never
	// from a copy kept here, so the two cannot drift.
	b.WriteString("\n")
	p := s.Params
	fmt.Fprintf(&b, "Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\n", p.Jc, p.Jmin, p.Jmax, p.S1, p.S2)
	fmt.Fprintf(&b, "H1 = %s\nH2 = %s\nH3 = %s\nH4 = %s\n", p.H1, p.H2, p.H3, p.H4)

	// Parameters newer than this code understands are passed through in
	// a stable order, so a server running a newer AmneziaWG still hands
	// out configs that work.
	if len(p.Extra) > 0 {
		keys := make([]string, 0, len(p.Extra))
		for k := range p.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s = %s\n", strings.ToUpper(k), p.Extra[k])
		}
	}

	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", s.ServerPublicKey)
	if s.AllowedIPs != "" {
		fmt.Fprintf(&b, "AllowedIPs = %s\n", s.AllowedIPs)
	}
	fmt.Fprintf(&b, "Endpoint = %s\n", s.Endpoint)
	if s.Keepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", s.Keepalive)
	}
	return b.String()
}
