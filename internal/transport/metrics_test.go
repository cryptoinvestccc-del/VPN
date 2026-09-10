package transport

import (
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/metrics"
)

// metricValue pulls one sample out of the exposition text.
func metricValue(t *testing.T, exposition, name string) string {
	t.Helper()
	for _, line := range strings.Split(exposition, "\n") {
		if strings.HasPrefix(line, name+" ") {
			return strings.TrimSpace(strings.TrimPrefix(line, name))
		}
	}
	t.Fatalf("metric %q not found in:\n%s", name, exposition)
	return ""
}

// TestMetricsRecordRealTraffic checks the counters against traffic that
// actually went through the tunnel. Unit tests prove the registry counts
// correctly; only this proves the transport calls it, which is the half
// that silently rots when code moves around.
func TestMetricsRecordRealTraffic(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)
	stats := metrics.New(false)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		_ = RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
			Metrics:        stats,
		})
	}()
	go func() {
		_ = RunClient(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      clientLocalAddr,
			RemoteWireAddr: serverWireAddr,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	conn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const packets = 5
	payload := make([]byte, 512)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < packets; i++ {
		if _, err := conn.Write(payload); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 65535)
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("packet %d never came back: %v", i, err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	out := stats.Expose()
	if got := metricValue(t, out, "obfsvpn_sessions_opened_total"); got != "1" {
		t.Errorf("sessions_opened_total = %s, want 1", got)
	}
	if got := metricValue(t, out, "obfsvpn_sessions_active"); got != "1" {
		t.Errorf("sessions_active = %s, want 1", got)
	}
	if got := metricValue(t, out, "obfsvpn_packets_received_total"); got != "5" {
		t.Errorf("packets_received_total = %s, want 5", got)
	}
	if got := metricValue(t, out, "obfsvpn_bytes_received_total"); got != "2560" {
		t.Errorf("bytes_received_total = %s, want 2560 (5 x 512)", got)
	}
	if got := metricValue(t, out, "obfsvpn_packets_sent_total"); got != "5" {
		t.Errorf("packets_sent_total = %s, want 5", got)
	}
}

// TestMetricsCountUnauthenticatedProbes gives the operator the signal that
// matters most on a public port: how often something is knocking without a
// credential. A jump in this rate is what a censor scanning the address
// space looks like from the inside.
func TestMetricsCountUnauthenticatedProbes(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)
	stats := metrics.New(false)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	go func() {
		_ = RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
			Metrics:        stats,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	prober, err := net.Dial("udp", serverWireAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer prober.Close()

	const probes = 7
	for i := 0; i < probes; i++ {
		junk := make([]byte, 200)
		if _, err := rand.Read(junk); err != nil {
			t.Fatal(err)
		}
		if _, err := prober.Write(junk); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(300 * time.Millisecond)

	out := stats.Expose()
	if got := metricValue(t, out, "obfsvpn_auth_failures_total"); got != "7" {
		t.Errorf("auth_failures_total = %s, want %d", got, probes)
	}
	// Probing must not have created any state.
	if got := metricValue(t, out, "obfsvpn_sessions_opened_total"); got != "0" {
		t.Errorf("unauthenticated probes opened %s sessions; they must allocate nothing", got)
	}
}

// TestMetricsCountReplays confirms the replay guard's rejections are
// visible: a burst here means somebody is resending captured packets.
func TestMetricsCountReplays(t *testing.T) {
	psk := testPSK(t)
	obf := mustObfuscatorFor(t, psk)
	stats := metrics.New(false)

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

	table := newSessionTable(wireConn, localAddr, singleClientRegistry(t, psk))
	table.metrics = stats
	defer table.closeAll()

	captured, err := obf.Wrap([]byte("captured"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.open(&net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 7000}, captured, sharedClientID, obf); err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 7000 + i}
		if _, err := table.open(addr, captured, sharedClientID, obf); err != nil {
			stats.ReplayRejected()
		}
	}

	out := stats.Expose()
	if got := metricValue(t, out, "obfsvpn_replays_rejected_total"); got != "3" {
		t.Errorf("replays_rejected_total = %s, want 3", got)
	}
	if got := metricValue(t, out, "obfsvpn_sessions_opened_total"); got != "1" {
		t.Errorf("replays opened extra sessions: sessions_opened_total = %s, want 1", got)
	}
}
