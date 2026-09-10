package profile

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// ParseWireGuardConfig reads the WireGuard side of a profile out of an
// ordinary wg-quick config.
//
// The alternative was to ask an operator to retype four keys and an MTU
// into a second tool, which is a transcription step, and transcription
// steps are where wrong keys come from. The provisioning script already
// writes a correct config; this reads it back.
//
// Only what a profile needs is taken. Directives this project does not
// carry — PostUp, Table, and the rest — are ignored rather than rejected,
// because a config may legitimately contain them and refusing would help
// nobody. The one exception is a config with more than one peer: a
// profile describes a single tunnel, and quietly using the first peer of
// several would produce a connection to a server the operator did not
// pick.
//
// The Endpoint is returned separately: in this design it is not the
// server's address but the local address the obfuscator listens on, so
// it belongs to the transport rather than to the tunnel.
func ParseWireGuardConfig(text string) (WireGuard, string, error) {
	var (
		w        WireGuard
		section  string
		peers    int
		endpoint string
	)

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if i := strings.IndexAny(text, "#;"); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}

		if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			section = strings.ToLower(strings.Trim(text, "[]"))
			if section == "peer" {
				peers++
			}
			continue
		}

		k, v, ok := strings.Cut(text, "=")
		if !ok {
			return WireGuard{}, "", fmt.Errorf("line %d: %q is neither a section nor a key = value", line, text)
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)

		switch section {
		case "interface":
			switch k {
			case "privatekey":
				w.PrivateKey = v
			case "address":
				w.Address = normalizeList(v)
			case "dns":
				w.DNS = normalizeList(v)
			case "mtu":
				n, err := strconv.Atoi(v)
				if err != nil {
					return WireGuard{}, "", fmt.Errorf("line %d: MTU %q is not a number", line, v)
				}
				w.MTU = n
			}
		case "peer":
			switch k {
			case "publickey":
				w.PeerPublicKey = v
			case "allowedips":
				w.AllowedIPs = normalizeList(v)
			case "endpoint":
				endpoint = v
			case "persistentkeepalive":
				n, err := strconv.Atoi(v)
				if err != nil {
					return WireGuard{}, "", fmt.Errorf("line %d: PersistentKeepalive %q is not a number", line, v)
				}
				w.PersistentKeepalive = n
			}
		case "":
			return WireGuard{}, "", fmt.Errorf("line %d: %q appears before any [Interface] or [Peer] section", line, text)
		}
	}
	if err := scanner.Err(); err != nil {
		return WireGuard{}, "", err
	}

	switch peers {
	case 1:
	case 0:
		return WireGuard{}, "", fmt.Errorf("the config has no [Peer] section, so there is no server to connect to")
	default:
		return WireGuard{}, "", fmt.Errorf("the config has %d [Peer] sections; a profile describes one tunnel, "+
			"so pick which peer this profile is for rather than letting the tool guess", peers)
	}

	// A client config generated for this project points WireGuard at the
	// local obfuscator, not at the server. Seeing a public address here
	// means the config was written for a direct connection, and a profile
	// built from it would describe a tunnel that bypasses the obfuscator
	// entirely — working, unobfuscated, and not what anyone asked for.
	if endpoint != "" && !isLoopbackEndpoint(endpoint) {
		return WireGuard{}, "", fmt.Errorf("the config's Endpoint is %s, which is not the local obfuscator; "+
			"this looks like a plain WireGuard config, and a profile built from it would bypass the obfuscation", endpoint)
	}

	if endpoint == "" {
		return WireGuard{}, "", fmt.Errorf("the config's [Peer] has no Endpoint, so nothing says where the local obfuscator listens")
	}
	if w.MTU == 0 {
		// wg-quick's own default is derived from the route, which on this
		// path would be too large. Silence here would mean large packets
		// disappearing, so it has to be an error.
		return WireGuard{}, "", fmt.Errorf("the config sets no MTU; on this transport it must be at most %d", MaxTunnelMTU)
	}
	return w, endpoint, nil
}

func isLoopbackEndpoint(endpoint string) bool {
	host, _, err := splitHostPortLenient(endpoint)
	if err != nil {
		return false
	}
	return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
}

func splitHostPortLenient(addr string) (string, string, error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return "", "", fmt.Errorf("no port")
	}
	host := strings.Trim(addr[:i], "[]")
	return host, addr[i+1:], nil
}

func normalizeList(v string) string {
	return strings.Join(splitList(v), ", ")
}
