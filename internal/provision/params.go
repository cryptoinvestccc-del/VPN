package provision

import (
	"bufio"
	"errors"
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
	// Kept as text because the value is a specification, not a number:
	// AmneziaWG accepts a single value or a range, "N" or "N-M", and the
	// server chooses. Parsing to an integer here would have to pick one
	// and would silently refuse the other, which is what the first
	// version did.
	H1, H2, H3, H4 string

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

	// Before any [section] header the keys belong to the interface. That
	// is how `awg showconf` prints, and treating its first lines as
	// nothing would silently yield a profile with no obfuscation.
	section = "interface"

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
			if _, _, err := parseHeader(value); err != nil {
				return Params{}, fmt.Errorf("line %d: %s = %q: %w", line, key, value, err)
			}
			switch key {
			case "h1":
				p.H1 = value
			case "h2":
				p.H2 = value
			case "h3":
				p.H3 = value
			case "h4":
				p.H4 = value
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

// parseHeader reads one of H1..H4 and returns the range it covers.
//
// The value is a specification rather than a number: AmneziaWG takes
// either a single value or a range, "N" or "N-M", and a server that uses
// ranges is not unusual — the first one this met did. A single value is
// the range containing only itself.
func parseHeader(spec string) (start, end uint64, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, errors.New("empty")
	}

	lo, hi, isRange := strings.Cut(spec, "-")
	start, err = strconv.ParseUint(strings.TrimSpace(lo), 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("%q is not a 32-bit number", lo)
	}

	end = start
	if isRange {
		end, err = strconv.ParseUint(strings.TrimSpace(hi), 10, 32)
		if err != nil {
			return 0, 0, fmt.Errorf("%q is not a 32-bit number", hi)
		}
	}
	if end < start {
		return 0, 0, fmt.Errorf("the range ends before it begins (%d-%d)", start, end)
	}
	return start, end, nil
}

func (p Params) validate() error {
	headers := []struct {
		name string
		spec string
	}{{"H1", p.H1}, {"H2", p.H2}, {"H3", p.H3}, {"H4", p.H4}}

	type span struct{ lo, hi uint64 }
	spans := make([]span, 0, 4)

	for _, h := range headers {
		if h.spec == "" {
			return fmt.Errorf("the interface configuration is missing %s; all four headers are required", h.name)
		}
		lo, hi, err := parseHeader(h.spec)
		if err != nil {
			return fmt.Errorf("%s: %w", h.name, err)
		}
		spans = append(spans, span{lo, hi})
	}

	// The four headers are what tell the message types apart, so their
	// ranges must not meet. A packet whose header falls in an overlap
	// belongs to two types at once, and the receiver has no way to know
	// which — a fault that shows up as some handshakes working.
	for i := 0; i < len(spans); i++ {
		for j := i + 1; j < len(spans); j++ {
			if spans[i].lo <= spans[j].hi && spans[j].lo <= spans[i].hi {
				return fmt.Errorf("%s and %s overlap (%s and %s); each header must cover its own values",
					headers[i].name, headers[j].name, headers[i].spec, headers[j].spec)
			}
		}
	}
	if p.Jmin > p.Jmax {
		return fmt.Errorf("Jmin (%d) is above Jmax (%d)", p.Jmin, p.Jmax)
	}
	return nil
}
