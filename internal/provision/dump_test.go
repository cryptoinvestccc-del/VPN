package provision

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// realDump is shaped like what `awg show all dump` prints: one interface
// line, then its peers. The interface line is deliberately given many
// trailing columns, because AmneziaWG's grew between versions and this
// parser must not care.
func realDump() string {
	iface := strings.Join([]string{
		"awg0", "SERVERPRIV=", "SERVERPUB=", "51820",
		"4", "40", "70", "86", "574", "0", "0",
		"1020325451", "1457919798", "1183463497", "1783538058",
		"0", "0", "0", "0", "0", "off",
	}, "\t")

	peer := func(key, allowed, handshake string) string {
		return strings.Join([]string{
			"awg0", key, "(none)", "198.51.100.9:51820", allowed, handshake, "1024", "2048", "off",
		}, "\t")
	}

	return iface + "\n" +
		peer("PEERA=", "10.8.0.2/32", "1758700000") + "\n" +
		peer("PEERB=", "10.8.0.3/32", "0") + "\n"
}

func TestParseDumpReadsPeers(t *testing.T) {
	peers, err := ParseDump([]byte(realDump()))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("parsed %d peers, expected 2", len(peers))
	}

	if peers[0].PublicKey != "PEERA=" {
		t.Errorf("public key: got %q", peers[0].PublicKey)
	}
	if got := peers[0].Addresses[0].String(); got != "10.8.0.2/32" {
		t.Errorf("address: got %q", got)
	}
	if !peers[0].LastHandshake.Equal(time.Unix(1758700000, 0)) {
		t.Errorf("handshake: got %v", peers[0].LastHandshake)
	}
	if !peers[0].Used() {
		t.Error("a peer with a handshake is reported unused")
	}
	if peers[1].Used() {
		t.Error("a peer with handshake 0 is reported used")
	}
}

// TestParseDumpIgnoresInterfaceWidth is the point of not reading the
// interface line. AmneziaWG has added parameters between versions, so
// its width moves; a parser that counted columns would work on the
// server it was written against and misread another.
func TestParseDumpIgnoresInterfaceWidth(t *testing.T) {
	for _, extra := range []int{0, 5, 40} {
		dump := realDump()
		lines := strings.Split(strings.TrimRight(dump, "\n"), "\n")
		lines[0] += strings.Repeat("\t0", extra)

		peers, err := ParseDump([]byte(strings.Join(lines, "\n")))
		if err != nil {
			t.Fatalf("%d extra interface columns: %v", extra, err)
		}
		if len(peers) != 2 {
			t.Errorf("%d extra interface columns: parsed %d peers, expected 2", extra, len(peers))
		}
	}
}

func TestParseDumpHandlesSeveralInterfaces(t *testing.T) {
	dump := realDump() + strings.Join([]string{"awg1", "P=", "K=", "51821", "0", "0", "0"}, "\t") + "\n" +
		strings.Join([]string{"awg1", "PEERC=", "(none)", "(none)", "10.9.0.2/32", "0", "0", "0", "off"}, "\t") + "\n"

	peers, err := ParseDump([]byte(dump))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 3 {
		t.Fatalf("parsed %d peers across two interfaces, expected 3", len(peers))
	}
}

func TestParseDumpHandlesAPeerWithNoAddresses(t *testing.T) {
	dump := strings.Join([]string{"awg0", "PRIV=", "PUB=", "51820", "0"}, "\t") + "\n" +
		strings.Join([]string{"awg0", "PEER=", "(none)", "(none)", "(none)", "0", "0", "0", "off"}, "\t") + "\n"

	peers, err := ParseDump([]byte(dump))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || len(peers[0].Addresses) != 0 {
		t.Errorf("a peer with no allowed IPs was not handled: %+v", peers)
	}
}

func TestParseDumpRejectsGarbage(t *testing.T) {
	cases := map[string]string{
		"not tab separated": "this is not a dump\n",
		"handshake not a number": strings.Join([]string{"awg0", "P=", "K=", "51820", "0"}, "\t") + "\n" +
			strings.Join([]string{"awg0", "PEER=", "(none)", "(none)", "10.8.0.2/32", "yesterday", "0", "0", "off"}, "\t") + "\n",
		"allowed IP not a prefix": strings.Join([]string{"awg0", "P=", "K=", "51820", "0"}, "\t") + "\n" +
			strings.Join([]string{"awg0", "PEER=", "(none)", "(none)", "10.8.0.2", "0", "0", "0", "off"}, "\t") + "\n",
		"truncated peer line": strings.Join([]string{"awg0", "P=", "K=", "51820", "0"}, "\t") + "\n" +
			strings.Join([]string{"awg0", "PEER=", "(none)"}, "\t") + "\n",
	}

	for name, dump := range cases {
		if _, err := ParseDump([]byte(dump)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestParseDumpEmptyServer(t *testing.T) {
	dump := strings.Join([]string{"awg0", "PRIV=", "PUB=", "51820", "0"}, "\t") + "\n"
	peers, err := ParseDump([]byte(dump))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Errorf("a server with no clients yielded %d peers", len(peers))
	}
}

func TestParseDumpScalesToManyPeers(t *testing.T) {
	var b strings.Builder
	b.WriteString(strings.Join([]string{"awg0", "PRIV=", "PUB=", "51820", "0"}, "\t") + "\n")
	const n = 5000
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "awg0\tPEER%d=\t(none)\t(none)\t10.%d.%d.%d/32\t0\t0\t0\toff\n",
			i, i/65536, (i/256)%256, i%256)
	}

	peers, err := ParseDump([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != n {
		t.Errorf("parsed %d of %d peers", len(peers), n)
	}
}

func TestServerPublicKey(t *testing.T) {
	key, err := ServerPublicKey([]byte(realDump()))
	if err != nil {
		t.Fatal(err)
	}
	if key != "SERVERPUB=" {
		t.Errorf("got %q, want SERVERPUB=", key)
	}
}

// TestServerPublicKeyNeverReturnsThePrivateOne: the private key sits one
// field earlier on the same line. Reading the wrong index would publish
// the server's identity to every client that asks for a config.
func TestServerPublicKeyNeverReturnsThePrivateOne(t *testing.T) {
	key, err := ServerPublicKey([]byte(realDump()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(key, "PRIV") {
		t.Fatalf("the server's private key was returned: %q", key)
	}
}

func TestServerPublicKeyRejectsNonsense(t *testing.T) {
	for name, dump := range map[string]string{
		"empty":     "",
		"too short": "awg0\tPRIV=\n",
	} {
		if _, err := ServerPublicKey([]byte(dump)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
