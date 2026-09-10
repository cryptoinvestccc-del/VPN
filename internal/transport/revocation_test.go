package transport

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

// setRegistry builds a registry from a client list.
func setRegistry(t *testing.T, list []clients.Client) *clients.Registry {
	t.Helper()
	set, err := clients.NewSet(list)
	if err != nil {
		t.Fatal(err)
	}
	return clients.NewStaticRegistry(set)
}

// replaceClients swaps the credential set, standing in for an operator
// editing the credential file and reloading.
func replaceClients(t *testing.T, registry *clients.Registry, list []clients.Client) {
	t.Helper()
	set, err := clients.NewSet(list)
	if err != nil {
		t.Fatal(err)
	}
	registry.Replace(set)
}

// clientKey builds a client entry whose key is filled with one repeated
// byte, so a test can tell keys apart at a glance.
func clientKey(t *testing.T, b byte) (client clients.Client, psk [32]byte) {
	t.Helper()
	for i := range psk {
		psk[i] = b
	}
	return clients.Client{
		ID:        fmt.Sprintf("client-%d", b),
		PSKBase64: base64.StdEncoding.EncodeToString(psk[:]),
	}, psk
}

// mustObfuscatorFor builds an obfuscator for a key, failing the test if
// the key is unusable.
func mustObfuscatorFor(t *testing.T, key [32]byte) *obfuscator.Obfuscator {
	t.Helper()
	obf, err := obfuscator.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return obf
}

// TestRevokingOneClientLeavesOthersConnected is the property that makes
// per-client credentials worth having. With a single shared key, taking
// access away from one device means changing the key on every device —
// every revocation is an outage for everyone.
func TestRevokingOneClientLeavesOthersConnected(t *testing.T) {
	ctx := testContext(t)
	certPath, keyPath, pin := testCert(t)

	alice, alicePSK := clientKey(t, 1)
	bob, bobPSK := clientKey(t, 2)
	registry := setRegistry(t, []clients.Client{alice, bob})

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	go func() {
		_ = RunServerTLS(ctx, TLSConfig{
			Clients:       registry,
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	startClient := func(psk [32]byte) string {
		entrance := freeUDPAddr(t)
		go func() {
			_ = RunClientTLS(ctx, TLSConfig{
				PSKs:             [][32]byte{psk},
				LocalAddr:        entrance,
				RemoteTLSAddr:    serverTLSAddr,
				ServerName:       "test.local",
				PinnedCertSHA256: pin,
			})
		}()
		return entrance
	}

	aliceEntrance := startClient(alicePSK)
	bobEntrance := startClient(bobPSK)
	time.Sleep(500 * time.Millisecond)

	reaches := func(t *testing.T, entrance string, payload string) bool {
		t.Helper()
		conn, err := net.Dial("udp", entrance)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		if _, err := conn.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 65535)
		n, err := conn.Read(buf)
		return err == nil && bytes.Equal(buf[:n], []byte(payload))
	}

	if !reaches(t, aliceEntrance, "alice before") {
		t.Fatal("alice could not reach the tunnel before revocation")
	}
	if !reaches(t, bobEntrance, "bob before") {
		t.Fatal("bob could not reach the tunnel before revocation")
	}

	// The operator revokes alice.
	alice.Disabled = true
	replaceClients(t, registry, []clients.Client{alice, bob})

	// Bob is untouched, immediately.
	if !reaches(t, bobEntrance, "bob after") {
		t.Fatal("revoking one client disconnected another: the whole point of per-client credentials")
	}

	// Alice cannot establish a new session. Her existing one is closed on
	// the next idle check rather than instantly, so a fresh connection is
	// what this asserts on; the live-session case has its own test.
	aliceAfter := startClient(alicePSK)
	time.Sleep(500 * time.Millisecond)
	if reaches(t, aliceAfter, "alice after") {
		t.Fatal("a revoked client still established a tunnel")
	}
}

// TestRevokedClientIsDisconnectedFromLiveSession covers the half of
// revocation that is easy to get wrong: a credential taken away must end
// the tunnel it is currently holding open, not merely refuse the next
// connection. Otherwise a revoked device keeps its access for as long as
// it avoids reconnecting.
func TestRevokedClientIsDisconnectedFromLiveSession(t *testing.T) {
	psk := testPSK(t)
	obf := mustObfuscatorFor(t, psk)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()
	localAddr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		t.Fatal(err)
	}
	wireConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wireConn.Close()

	enabled, err := clients.NewSetFromCredentials([]clients.Credential{
		{ClientID: "doomed", Key: psk},
		{ClientID: "kept", Key: psk},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := clients.NewStaticRegistry(enabled)

	table := newSessionTable(wireConn, localAddr, registry)
	defer table.closeAll()

	for _, id := range []string{"doomed", "kept"} {
		packet, err := obf.Wrap([]byte(id))
		if err != nil {
			t.Fatal(err)
		}
		addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 6000 + len(id)}
		if _, err := table.open(addr, packet, id, obf); err != nil {
			t.Fatal(err)
		}
	}
	if got := table.count(); got != 2 {
		t.Fatalf("expected 2 sessions, got %d", got)
	}

	// Revoke one client by swapping in a set that no longer lists it.
	remaining, err := clients.NewSetFromCredentials([]clients.Credential{
		{ClientID: "kept", Key: psk},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Replace(remaining)

	if closed := table.disconnectRevoked(); closed != 1 {
		t.Fatalf("expected exactly the revoked client's session to close, closed %d", closed)
	}

	deadline := time.Now().Add(2 * time.Second)
	for table.count() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("revoked session was not removed: %d sessions remain", table.count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAuthenticatorIdentifiesTheRightClient checks the search itself: with
// several credentials configured, a packet must be attributed to the one
// that actually produced it.
func TestAuthenticatorIdentifiesTheRightClient(t *testing.T) {
	var credentials []clients.Credential
	keys := map[string][32]byte{}
	for i := byte(1); i <= 5; i++ {
		var key [32]byte
		for j := range key {
			key[j] = i
		}
		id := fmt.Sprintf("client-%d", i)
		keys[id] = key
		credentials = append(credentials, clients.Credential{ClientID: id, Key: key})
	}

	set, err := clients.NewSetFromCredentials(credentials)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := newAuthenticator(set, nil)
	if err != nil {
		t.Fatal(err)
	}

	for id, key := range keys {
		obf := mustObfuscatorFor(t, key)
		payload := []byte("packet from " + id)
		packet, err := obf.Wrap(payload)
		if err != nil {
			t.Fatal(err)
		}

		plaintext, gotID, _, ok := auth.authenticate(packet)
		if !ok {
			t.Fatalf("a packet from %s was not recognized at all", id)
		}
		if gotID != id {
			t.Fatalf("packet from %s was attributed to %s", id, gotID)
		}
		if !bytes.Equal(plaintext, payload) {
			t.Fatalf("payload for %s came back altered", id)
		}
	}

	// A key nobody holds must not be attributed to anyone.
	var stranger [32]byte
	stranger[0] = 0xff
	strangerPacket, err := mustObfuscatorFor(t, stranger).Wrap([]byte("outsider"))
	if err != nil {
		t.Fatal(err)
	}
	if _, id, _, ok := auth.authenticate(strangerPacket); ok {
		t.Fatalf("an unknown key was accepted as client %q", id)
	}
}
