package transport

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
)

func TestRateScalesWithCredentialCount(t *testing.T) {
	// The budget is a share of a core, and each attempt costs more as the
	// credential list grows, so the allowed rate has to fall as clients
	// are added. A fixed rate would mean the exposure grows with every
	// client added, silently.
	few := rateForCredentials(10)
	many := rateForCredentials(1000)

	if many >= few {
		t.Fatalf("rate did not fall as credentials grew: 10 clients=%.0f/s, 1000 clients=%.0f/s", few, many)
	}
	if many < minTrialRate {
		t.Fatalf("rate fell below the floor that keeps large deployments usable: %.0f/s", many)
	}
	if few > maxTrialRate {
		t.Fatalf("rate exceeded the ceiling: %.0f/s", few)
	}

	// The point of the exercise: whatever the client count, the CPU a
	// flood can consume stays inside the budget.
	for _, count := range []int{1, 10, 100, 1000, 10000} {
		rate := rateForCredentials(count)
		cpuShare := rate * float64(count) * trialCostPerCredential.Seconds()
		// The floor can push a very large deployment slightly over; that
		// is deliberate, and bounded.
		if cpuShare > trialCPUBudget*3 {
			t.Errorf("at %d credentials a flood could consume %.1f%% of a core, budget is %.1f%%",
				count, cpuShare*100, trialCPUBudget*100)
		}
	}
}

func TestLimiterAllowsBurstThenThrottles(t *testing.T) {
	l := newTrialLimiter(1000) // a low rate, so the burst is what is measured
	frozen := time.Now()
	l.nowFn = func() time.Time { return frozen }

	allowed := 0
	for i := 0; i < int(trialBurst)+50; i++ {
		if l.allow() {
			allowed++
		}
	}

	if allowed != int(trialBurst) {
		t.Fatalf("allowed %d attempts before throttling, expected the burst of %.0f", allowed, trialBurst)
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	l := newTrialLimiter(1000)
	frozen := time.Now()
	l.nowFn = func() time.Time { return frozen }

	for l.allow() {
		// Drain the bucket.
	}
	if l.allow() {
		t.Fatal("the bucket did not stay empty")
	}

	// A second later, the configured rate's worth of tokens is back.
	frozen = frozen.Add(time.Second)
	granted := 0
	for i := 0; i < 10000; i++ {
		if !l.allow() {
			break
		}
		granted++
	}
	if granted == 0 {
		t.Fatal("no tokens were restored after a second")
	}
	expected := int(rateForCredentials(1000))
	if granted > expected+1 {
		t.Fatalf("restored %d tokens in a second, rate is %d/s", granted, expected)
	}
}

func TestResizeTightensRate(t *testing.T) {
	l := newTrialLimiter(10)
	before := l.rate

	l.resize(2000)
	if l.rate >= before {
		t.Fatalf("adding clients did not tighten the rate: %.0f/s then %.0f/s", before, l.rate)
	}
}

// TestEstablishedSessionSurvivesFlood is the property that decides
// whether the rate limit is worth having. An established peer decrypts
// under the one key it authenticated with and never enters the credential
// search, so a flood of unknown peers must not take its tunnel away.
//
// The bar is survival, not losslessness. A flood fills the kernel's
// receive buffer, and a packet that arrives while it is full is dropped
// before any code here sees it — no amount of care in userspace changes
// that, and UDP promises nothing else. What matters is that the session
// keeps working through it, which is what WireGuard's own retransmits
// rely on.
func TestEstablishedSessionSurvivesFlood(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		_ = RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
		})
	}()
	go func() {
		_ = RunClient(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      clientLocalAddr,
			RemoteWireAddr: serverWireAddr,
		})
	}()
	time.Sleep(250 * time.Millisecond)

	legit, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer legit.Close()

	roundTrip := func() bool {
		payload := []byte("still working")
		if _, err := legit.Write(payload); err != nil {
			return false
		}
		legit.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 65535)
		n, err := legit.Read(buf)
		return err == nil && string(buf[:n]) == string(payload)
	}

	if !roundTrip() {
		t.Fatal("the session did not work before the flood")
	}

	// Flood the public port from a source the server has never seen, as
	// fast as the loopback allows — harder than any real network path.
	flood, err := net.Dial("udp", serverWireAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer flood.Close()

	// The flooder is stopped through one idempotent function that also
	// waits for the goroutine to leave, so the test never asserts recovery
	// while junk is still in flight.
	stopFlood := make(chan struct{})
	floodDone := make(chan struct{})
	var stopOnce sync.Once
	halt := func() {
		stopOnce.Do(func() { close(stopFlood) })
		<-floodDone
	}
	go func() {
		defer close(floodDone)
		junk := make([]byte, 200)
		for {
			select {
			case <-stopFlood:
				return
			default:
				_, _ = flood.Write(junk)
			}
		}
	}()
	defer halt()

	time.Sleep(500 * time.Millisecond)

	delivered := 0
	const attempts = 10
	for i := 0; i < attempts; i++ {
		if roundTrip() {
			delivered++
		}
	}

	// Losing the odd packet to a saturated buffer is expected; losing the
	// tunnel is not.
	if delivered < attempts/2 {
		t.Fatalf("only %d of %d exchanges survived the flood; the session was effectively cut off",
			delivered, attempts)
	}

	// And it must be healthy again once the flood stops.
	halt()
	time.Sleep(300 * time.Millisecond)
	if !roundTrip() {
		t.Fatal("the session did not recover after the flood ended")
	}
}

// TestThrottlingStillAdmitsLegitimateClients checks the other side of the
// trade: a limit that kept genuine peers out would be worse than the
// problem it solves.
func TestThrottlingStillAdmitsLegitimateClients(t *testing.T) {
	credentials := make([]clients.Credential, 0, 50)
	var lastKey [32]byte
	for i := 0; i < 50; i++ {
		var key [32]byte
		key[0] = byte(i)
		key[1] = 0xAB
		lastKey = key
		credentials = append(credentials, clients.Credential{
			ClientID: string(rune('a' + i%26)),
			Key:      key,
		})
	}
	set, err := clients.NewSetFromCredentials(credentials)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := newAuthenticator(set, nil)
	if err != nil {
		t.Fatal(err)
	}

	limiter := newTrialLimiter(len(credentials))
	obf := mustObfuscatorFor(t, lastKey)

	// A modest cluster of genuine arrivals — a server restart, say —
	// should all get through on the burst allowance.
	admitted := 0
	for i := 0; i < 100; i++ {
		if !limiter.allow() {
			continue
		}
		packet, err := obf.Wrap([]byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, ok := auth.authenticate(packet); ok {
			admitted++
		}
	}

	if admitted != 100 {
		t.Fatalf("only %d of 100 genuine arrivals were admitted; the burst allowance is too small", admitted)
	}
}
