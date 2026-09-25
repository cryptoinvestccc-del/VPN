package main

import (
	"context"
	"strings"
	"testing"
)

// What an Amnezia awg0.conf looks like before this service ever wrote
// it: awg-quick's Address, then what the interface itself holds.
const amneziaConf = `[Interface]
PrivateKey = SERVERPRIVATE=
Address = 10.8.1.0/24
ListenPort = 36635
Jc = 5
H1 = 654395697-1564484786
PostUp = iptables -A FORWARD -i awg0 -j ACCEPT

[Peer]
PublicKey = AMNEZIACLIENT=
AllowedIPs = 10.8.1.1/32
`

// What `awg showconf` prints: no Address, no PostUp — the interface
// does not hold them.
const showconfOut = `[Interface]
ListenPort = 36635
Jc = 5
H1 = 654395697-1564484786
PrivateKey = SERVERPRIVATE=

[Peer]
PublicKey = AMNEZIACLIENT=
AllowedIPs = 10.8.1.1/32

[Peer]
PublicKey = BESYCLIENT=
AllowedIPs = 10.8.1.10/32
`

func TestShowconfAloneLosesTheAddress(t *testing.T) {
	// The fact the old persistence step ignored, pinned so nobody
	// "simplifies" back to writing showconf.
	if hasAddress(showconfOut) {
		t.Fatal("showconf output carries an Address; the premise of mergeConf is wrong")
	}
}

func TestMergeKeepsWhatAwgQuickNeeds(t *testing.T) {
	got := mergeConf(amneziaConf, showconfOut, []string{"10.8.1.0/24"})

	for _, want := range []string{
		"Address = 10.8.1.0/24",
		"PostUp = iptables -A FORWARD -i awg0 -j ACCEPT",
		"ListenPort = 36635",
		"PrivateKey = SERVERPRIVATE=",
		"PublicKey = AMNEZIACLIENT=",
		"PublicKey = BESYCLIENT=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the written file lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "[Interface]"); n != 1 {
		t.Errorf("%d [Interface] sections:\n%s", n, got)
	}
	if n := strings.Count(got, "Address ="); n != 1 {
		t.Errorf("%d Address lines:\n%s", n, got)
	}
	if !strings.HasPrefix(got, "[Interface]\nAddress = ") {
		t.Errorf("the file does not open with its interface and address:\n%s", got)
	}
}

// TestADamagedFileIsRepaired is the production server's likely state:
// a file this service already wrote from showconf, Address gone.
func TestADamagedFileIsRepaired(t *testing.T) {
	damaged := showconfOut
	got := mergeConf(damaged, showconfOut, []string{"10.8.1.0/24"})
	if !strings.Contains(got, "Address = 10.8.1.0/24") {
		t.Errorf("the address was not restored from the running interface:\n%s", got)
	}
}

func TestWithoutTheLiveInterfaceTheFilesAddressIsKept(t *testing.T) {
	got := mergeConf(amneziaConf, showconfOut, nil)
	if !strings.Contains(got, "Address = 10.8.1.0/24") {
		t.Errorf("the file's own Address was dropped:\n%s", got)
	}
}

func TestParseAddrsReadsIP(t *testing.T) {
	out := "5: awg0    inet 10.8.1.0/24 scope global awg0\\       valid_lft forever preferred_lft forever\n" +
		"5: awg0    inet6 fd00::1/64 scope global \\       valid_lft forever preferred_lft forever\n"
	got := parseAddrs(out)
	if len(got) != 2 || got[0] != "10.8.1.0/24" || got[1] != "fd00::1/64" {
		t.Errorf("read %v", got)
	}
}

// TestLinkLocalAddressesAreNotWrittenBack: the kernel's own fe80::
// address on a TUN interface is not configuration.
func TestLinkLocalAddressesAreNotWrittenBack(t *testing.T) {
	out := "5: awg0    inet 10.8.1.0/24 scope global awg0\\       valid_lft forever preferred_lft forever\n" +
		"5: awg0    inet6 fe80::6d3e:1c2b:a71f:9c04/64 scope link stable-privacy \\       valid_lft forever preferred_lft forever\n"
	got := parseAddrs(out)
	if len(got) != 1 || got[0] != "10.8.1.0/24" {
		t.Errorf("read %v; the link-local address is the kernel's, not configuration", got)
	}
	written := mergeConf(showconfOut, showconfOut, got)
	if hasLinkLocalAddress(written) {
		t.Errorf("wrote a link-local Address:\n%s", written)
	}
	if !hasLinkLocalAddress("[Interface]\nAddress = 10.8.1.0/24\nAddress = fe80::1/64\n") {
		t.Error("did not recognise a file an earlier version damaged this way")
	}
}

func TestSaveRefusesToWriteAFileWithNoAddress(t *testing.T) {
	r := &recordingRunner{out: map[string]string{"awg showconf": showconfOut}}
	d := testDevice(r)
	d.saveConf = "/opt/amnezia/awg/awg0.conf"
	// No existing file and no answer from ip: nothing to take an
	// Address from.
	if err := d.Save(context.Background()); err == nil {
		t.Fatal("wrote a configuration with no Address")
	}
	if len(r.fed) != 0 {
		t.Errorf("something was written anyway: %q", r.fed)
	}
}

func TestSaveWritesTheMergedFileAndKeepsACopy(t *testing.T) {
	r := &recordingRunner{out: map[string]string{
		"awg showconf":                     showconfOut,
		"cat '/opt/amnezia/awg/awg0.conf'": amneziaConf,
		"ip -o addr show dev wg0":          "5: wg0    inet 10.8.1.0/24 scope global wg0\n",
	}}
	d := testDevice(r)
	d.saveConf = "/opt/amnezia/awg/awg0.conf"
	if err := d.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	var backedUp bool
	for _, c := range r.calls {
		if strings.Contains(strings.Join(c, " "), "awg0.conf.before-besy") {
			backedUp = true
		}
	}
	if !backedUp {
		t.Error("no copy of the original was kept before writing")
	}
	if len(r.fed) != 1 || !strings.Contains(r.fed[0], "Address = 10.8.1.0/24") ||
		!strings.Contains(r.fed[0], "PostUp") {
		t.Errorf("wrote %q", r.fed)
	}
}

func TestRegistryKeepsOnlyKeys(t *testing.T) {
	good := "kDhsuFh/nOvx3v/XAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	r := &recordingRunner{out: map[string]string{
		"besy-issued.keys": good + "\nnot-a-key\n\n",
	}}
	d := testDevice(r)
	reg, err := loadRegistry(context.Background(), d, "/opt/amnezia/awg/besy-issued.keys")
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Owns(good) || reg.Owns("not-a-key") || reg.Len() != 1 {
		t.Fatalf("loaded %v", reg.keys)
	}

	other := "8RVUdLEokDorhmcrAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	hash := strings.Repeat("ab", 32)
	if err := reg.Add(context.Background(), other, hash); err != nil {
		t.Fatal(err)
	}
	if last := r.fed[len(r.fed)-1]; !strings.Contains(last, good) || !strings.Contains(last, other+" "+hash) {
		t.Errorf("wrote %q", last)
	}
	// Read back, the hash is what was recorded.
	r.out["besy-issued.keys"] = r.fed[len(r.fed)-1]
	again, err := loadRegistry(context.Background(), d, "/opt/amnezia/awg/besy-issued.keys")
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := again.SecretHash(other); !ok || h != hash {
		t.Errorf("read back %q, %v", h, ok)
	}
	if err := reg.Remove(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	if reg.Owns(good) || strings.Contains(r.fed[len(r.fed)-1], good) {
		t.Error("a removed key is still on record")
	}
}
