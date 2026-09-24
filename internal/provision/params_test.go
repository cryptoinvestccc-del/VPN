package provision

import (
	"strings"
	"testing"
)

const amneziaConf = `[Interface]
PrivateKey = SERVERPRIV=
Address = 10.8.0.1/24
ListenPort = 51820

Jc = 4
Jmin = 40
Jmax = 70
S1 = 86
S2 = 574
H1 = 1020325451
H2 = 1457919798
H3 = 1183463497
H4 = 1783538058

[Peer]
PublicKey = CLIENT=
AllowedIPs = 10.8.0.2/32
`

func TestParseParams(t *testing.T) {
	p, err := ParseParams(amneziaConf)
	if err != nil {
		t.Fatal(err)
	}

	if p.Jc != 4 || p.Jmin != 40 || p.Jmax != 70 || p.S1 != 86 || p.S2 != 574 {
		t.Errorf("junk parameters: %+v", p)
	}
	if p.H1 != "1020325451" || p.H4 != "1783538058" {
		t.Errorf("header parameters: %+v", p)
	}
}

// TestParseParamsIgnoresThePeerSection: a server config lists every
// client after the interface, and nothing there belongs in the numbers
// handed to a new one.
func TestParseParamsIgnoresThePeerSection(t *testing.T) {
	conf := amneziaConf + "\nJc = 99\n" // after [Peer], so not the interface's
	p, err := ParseParams(conf)
	if err != nil {
		t.Fatal(err)
	}
	if p.Jc != 4 {
		t.Errorf("Jc = %d; a value from the [Peer] section was used", p.Jc)
	}
}

// TestParseParamsCarriesUnknownParametersThrough: AmneziaWG has added
// parameters between versions. A server running a newer one than this
// code knows about must still be able to hand out a working config, so
// recognised-but-unparsed keys pass through verbatim.
func TestParseParamsCarriesUnknownParametersThrough(t *testing.T) {
	conf := strings.Replace(amneziaConf, "H4 = 1783538058",
		"H4 = 1783538058\nS3 = 12\nS4 = 34\nI1 = <b 0xf1>", 1)

	p, err := ParseParams(conf)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"s3": "12", "s4": "34", "i1": "<b 0xf1>"} {
		if p.Extra[key] != want {
			t.Errorf("%s: got %q, want %q", key, p.Extra[key], want)
		}
	}
}

// TestPlainWireGuardConfigIsRejected: handing an AmneziaWG client a
// config with no obfuscation parameters produces a handshake the server
// never answers, and nothing says why. Refusing at the source is the
// only place this is visible.
func TestPlainWireGuardConfigIsRejected(t *testing.T) {
	plain := `[Interface]
PrivateKey = k
Address = 10.8.0.1/24
ListenPort = 51820
`
	_, err := ParseParams(plain)
	if err == nil {
		t.Fatal("a plain WireGuard config was accepted as AmneziaWG")
	}
	if !strings.Contains(err.Error(), "plain WireGuard") {
		t.Errorf("the message does not name the problem: %v", err)
	}
}

func TestParseParamsRejectsFaults(t *testing.T) {
	cases := map[string]string{
		"missing H3": strings.Replace(amneziaConf, "H3 = 1183463497", "", 1),
		"identical headers": strings.Replace(amneziaConf,
			"H2 = 1457919798", "H2 = 1020325451", 1),
		"Jmin above Jmax":     strings.Replace(amneziaConf, "Jmin = 40", "Jmin = 90", 1),
		"Jc not a number":     strings.Replace(amneziaConf, "Jc = 4", "Jc = four", 1),
		"header out of range": strings.Replace(amneziaConf, "H1 = 1020325451", "H1 = 99999999999999", 1),
	}

	for name, conf := range cases {
		if _, err := ParseParams(conf); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestParseParamsAcceptsShowconfOutput: `awg showconf` prints the
// interface's settings with no section header, and the first version of
// this parser skipped every line before one — producing a profile with
// no obfuscation parameters at all, which is a tunnel whose handshake is
// never answered.
func TestParseParamsAcceptsShowconfOutput(t *testing.T) {
	showconf := `ListenPort = 51820
FwMark = off
PrivateKey = SERVERPRIV=
Jc = 4
Jmin = 40
Jmax = 70
S1 = 86
S2 = 574
H1 = 1020325451
H2 = 1457919798
H3 = 1183463497
H4 = 1783538058

[Peer]
PublicKey = CLIENT=
AllowedIPs = 10.8.1.2/32
`
	p, err := ParseParams(showconf)
	if err != nil {
		t.Fatalf("output with no [Interface] header was refused: %v", err)
	}
	if p.Jc != 4 || p.S1 != 86 || p.H1 != "1020325451" {
		t.Errorf("parameters were not read: %+v", p)
	}
}

// TestShowconfPeerSectionIsStillIgnored: the header-less start must not
// turn into reading everything.
func TestShowconfPeerSectionIsStillIgnored(t *testing.T) {
	conf := "Jc = 4\nJmin = 40\nJmax = 70\nS1 = 86\nS2 = 574\n" +
		"H1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nJc = 99\n"

	p, err := ParseParams(conf)
	if err != nil {
		t.Fatal(err)
	}
	if p.Jc != 4 {
		t.Errorf("Jc = %d; a value from the [Peer] section was used", p.Jc)
	}
}

// TestHeaderMayBeARange is what the first real server turned out to
// use. AmneziaWG takes a header as "N" or as "N-M", and the value is a
// specification rather than a number: parsing it to an integer accepts
// one form and silently refuses the other, which is how this was first
// written and why it failed on a live configuration.
func TestHeaderMayBeARange(t *testing.T) {
	conf := strings.Replace(amneziaConf, "H1 = 1020325451", "H1 = 654395697-999999999", 1)

	p, err := ParseParams(conf)
	if err != nil {
		t.Fatalf("a header range was refused: %v", err)
	}
	if p.H1 != "654395697-999999999" {
		t.Errorf("the range was not carried through verbatim: %q", p.H1)
	}
}

// TestOverlappingHeadersAreRefused: the four headers are what tell the
// message types apart. A packet whose header falls where two ranges meet
// belongs to both at once, and the receiver cannot know which — a fault
// that shows up as some handshakes working and others not.
func TestOverlappingHeadersAreRefused(t *testing.T) {
	conf := strings.Replace(amneziaConf, "H1 = 1020325451", "H1 = 1000000000-1500000000", 1)
	conf = strings.Replace(conf, "H2 = 1457919798", "H2 = 1400000000-1600000000", 1)

	_, err := ParseParams(conf)
	if err == nil {
		t.Fatal("two overlapping header ranges were accepted")
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Errorf("the message does not name the problem: %v", err)
	}
}

func TestHeaderRangeFaults(t *testing.T) {
	cases := map[string]string{
		"ends before it begins": "H1 = 900-100",
		"three parts":           "H1 = 1-2-3",
		"not a number":          "H1 = abc-200",
		"above 32 bits":         "H1 = 1-99999999999999",
		"negative":              "H1 = -1564484786",
	}
	for name, line := range cases {
		conf := strings.Replace(amneziaConf, "H1 = 1020325451", line, 1)
		if _, err := ParseParams(conf); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
