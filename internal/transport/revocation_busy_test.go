package transport

import (
	"net"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
)

// The revocation checks used to sit inside a read-timeout branch, so they
// were reached only while a tunnel was quiet. A session carrying traffic
// never timed out, never re-checked its credential, and kept running
// indefinitely after being revoked.
//
// That is the case revocation exists for. A device is cut off precisely
// when somebody is using it; a revocation that works only on idle
// sessions is closest to useless exactly when it is needed. The tests
// below therefore keep the tunnel busy throughout, which is what the
// earlier revocation tests did not do.

// revocationDeadline is how long a test waits for a withdrawn credential
// to stop working: the check interval plus room for scheduling.
const revocationDeadline = revocationCheckInterval + 4*time.Second

func TestRevocationCutsABusyUDPSession(t *testing.T) {
	ctx := testContext(t)
	alice, aliceKey := clientKey(t, 1)
	bob, _ := clientKey(t, 2)
	registry := setRegistry(t, []clients.Client{alice, bob})

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	go func() {
		_ = RunServer(ctx, Config{
			Clients:        registry,
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	conn, err := net.Dial("udp", serverWireAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	obf := mustObfuscatorFor(t, aliceKey)

	exchange := func() bool {
		packet, err := obf.Wrap([]byte("traffic"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(packet); err != nil {
			return false
		}
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 65535)
		_, err = conn.Read(buf)
		return err == nil
	}

	if !exchange() {
		t.Fatal("the session did not work before revocation")
	}

	replaceClients(t, registry, []clients.Client{
		{ID: alice.ID, PSKBase64: alice.PSKBase64, Disabled: true},
		bob,
	})

	if !waitUntilCutOff(t, exchange, revocationDeadline) {
		t.Fatalf("a busy session kept carrying traffic for %s after its credential was revoked", revocationDeadline)
	}
}

func TestRevocationCutsABusyTLSSession(t *testing.T) {
	ctx := testContext(t)
	alice, aliceKey := clientKey(t, 3)
	bob, _ := clientKey(t, 4)
	registry := setRegistry(t, []clients.Client{alice, bob})
	certPath, keyPath, pin := testCert(t)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

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

	go func() {
		_ = RunClientTLS(ctx, TLSConfig{
			PSKs:             [][32]byte{aliceKey},
			LocalAddr:        clientLocalAddr,
			RemoteTLSAddr:    serverTLSAddr,
			ServerName:       "test.local",
			PinnedCertSHA256: pin,
		})
	}()
	time.Sleep(500 * time.Millisecond)

	conn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	exchange := func() bool {
		if _, err := conn.Write([]byte("traffic")); err != nil {
			return false
		}
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 65535)
		_, err := conn.Read(buf)
		return err == nil
	}

	if !exchange() {
		t.Fatal("the tunnel did not work before revocation")
	}

	replaceClients(t, registry, []clients.Client{
		{ID: alice.ID, PSKBase64: alice.PSKBase64, Disabled: true},
		bob,
	})

	if !waitUntilCutOff(t, exchange, revocationDeadline) {
		t.Fatalf("a busy TLS session kept carrying traffic for %s after its credential was revoked", revocationDeadline)
	}
}

// waitUntilCutOff keeps the tunnel busy and reports whether it stopped
// working within the deadline.
//
// Busy is the point. An exchange that pauses to wait would let the read
// loop time out, which is the one condition under which the old code
// happened to notice a revocation.
func waitUntilCutOff(t *testing.T, exchange func() bool, within time.Duration) bool {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !exchange() {
			// One failure could be a dropped packet. Require the tunnel
			// to stay down, so a lost datagram is not read as a
			// revocation that did not happen.
			stayed := true
			for i := 0; i < 3; i++ {
				if exchange() {
					stayed = false
					break
				}
			}
			if stayed {
				return true
			}
		}
	}
	return false
}
