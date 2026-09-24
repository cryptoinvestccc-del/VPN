package provision

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Params are the AmneziaWG obfuscation settings a client must match.
//
// They are not optional decoration: the junk counts and the header
// values change what goes on the wire, so a client that does not carry
// the server's exact numbers produces packets the server discards. The
// failure is silent — a handshake that is never answered — which is why
// these are read from the server rather than typed into a form.
//
// Only the parameters a client config carries are kept. The server has
// others (timeouts, rekey intervals) that belong to the server alone.
type Params struct {
	// Junk packets sent before a handshake, and their size range.
	Jc, Jmin, Jmax int

	// Junk prepended to the handshake init and response packets.
	S1, S2 int

	// Header values that replace WireGuard's four message types. These
	// are what stop a packet from being recognisable by its first byte.
	H1, H2, H3, H4 uint32

	// S3, S4 and I1..I5 exist in newer AmneziaWG versions. They are
	// carried through verbatim when the server config has them and left
	// out when it does not, because a client told about a parameter its
	// server does not use is as broken as one left uninformed.
	Extra map[string]string
}

// extraKeys are passed through untouched. Keeping them as text rather
// than parsing each one means a server running a newer AmneziaWG than
// this code knows about still hands its clients a working config.
var extraKeys = map[string]bool{
	"s3": true, "s4": true,
	"i1": true, "i2": true, "i3": true, "i4": true, "i5": true,
	"itime": true,
}

// ParseParams reads the [Interface] section of an AmneziaWG
// configuration.
//
// By name, not by position. The dump output would be the more obvious
// source, but AmneziaWG appends its parameters to the interface line and
// has added to them between versions, so every index there moves with
// the version. A key called Jc is called Jc in every version that has
// one.
func ParseParams(conf string) (Params, error) {
	var (
		p       = Params{Extra: map[string]string{}}
		section string
		found   int
	)

	sc := bufio.NewScanner(strings.NewReader(conf))
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if i := strings.IndexAny(text, "#;"); i >= 0 {
			text = strings.TrimSpace(text[:i])
		}
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			section = strings.ToLower(strings.Trim(text, "[]"))
			continue
		}
		if section != "interface" {
			continue
		}

		key, value, ok := strings.Cut(text, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "jc", "jmin", "jmax", "s1", "s2":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Params{}, fmt.Errorf("line %d: %s = %q is not a number", line, key, value)
			}
			switch key {
			case "jc":
				p.Jc = n
			case "jmin":
				p.Jmin = n
			case "jmax":
				p.Jmax = n
			case "s1":
				p.S1 = n
			case "s2":
				p.S2 = n
			}
			found++
		case "h1", "h2", "h3", "h4":
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return Params{}, fmt.Errorf("line %d: %s = %q is not a 32-bit number", line, key, value)
			}
			switch key {
			case "h1":
				p.H1 = uint32(n)
			case "h2":
				p.H2 = uint32(n)
			case "h3":
				p.H3 = uint32(n)
			case "h4":
				p.H4 = uint32(n)
			}
			found++
		default:
			if extraKeys[key] {
				p.Extra[key] = value
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Params{}, err
	}

	if found == 0 {
		// Plain WireGuard, or a config read from the wrong place. Either
		// way, handing clients a config with no obfuscation parameters
		// when the server expects them produces a tunnel that never
		// completes a handshake and says nothing about why.
		return Params{}, fmt.Errorf("the interface configuration carries no AmneziaWG parameters (Jc, S1, H1...); " +
			"this looks like a plain WireGuard config rather than an AmneziaWG one")
	}
	if err := p.validate(); err != nil {
		return Params{}, err
	}
	return p, nil
}

func (p Params) validate() error {
	if p.H1 == 0 || p.H2 == 0 || p.H3 == 0 || p.H4 == 0 {
		return fmt.Errorf("the interface configuration is missing one of H1..H4; all four are required")
	}
	// The four header values must differ: they are what tells the four
	// message types apart, and two the same makes a packet ambiguous.
	seen := map[uint32]bool{}
	for _, h := range []uint32{p.H1, p.H2, p.H3, p.H4} {
		if seen[h] {
			return fmt.Errorf("H1..H4 must all differ; %d appears twice", h)
		}
		seen[h] = true
	}
	if p.Jmin > p.Jmax {
		return fmt.Errorf("Jmin (%d) is above Jmax (%d)", p.Jmin, p.Jmax)
	}
	return nil
}
