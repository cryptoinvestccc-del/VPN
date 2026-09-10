// Package transport implements the two ends of the obfuscated UDP tunnel:
// a local-facing proxy that talks plaintext WireGuard to a local peer, and
// a wire-facing proxy that exchanges obfuscated packets with the other side.
package transport

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
	"github.com/cryptoinvestccc-del/vpn/internal/metrics"
	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

var (
	errReplayedPacket = errors.New("transport: packet already opened a session")

	// ErrInvalidPacket reports a packet too short to be one of ours.
	ErrInvalidPacket = errors.New("transport: malformed packet")
)

// Config configures one end of the obfuscated tunnel.
type Config struct {
	// PSKs is the single-key configuration: one secret shared by every
	// client, current key first. Convenient for a few personal devices,
	// but revoking one of them means rekeying all of them.
	//
	// Ignored when Clients is set.
	PSKs [][32]byte

	// Clients is the per-client configuration, where each device has its
	// own credential and can be revoked on its own. Internally this is
	// the only mechanism: a shared PSK is served as a set containing one
	// client.
	Clients *clients.Registry

	// LocalAddr is where we listen for/send plaintext WireGuard packets
	// (typically 127.0.0.1:<wg-port> on the client, or forwards to the
	// real WireGuard server on the server side).
	LocalAddr string

	// RemoteWireAddr is the other obfuscator endpoint (across the
	// network, subject to DPI).
	RemoteWireAddr string

	// ListenWireAddr is where we listen for obfuscated wire traffic.
	// On the client this is typically not bound (we dial out instead);
	// on the server this is the public-facing listener.
	ListenWireAddr string

	// JunkPackets is how many decoy packets to send to RemoteWireAddr
	// before the first real packet, to break "first packet looks like a
	// WG handshake" fingerprinting. 0 disables it.
	JunkPackets int

	// Metrics records what an operator needs to see. Nil is fine: every
	// recording method is safe on a nil registry, so there is nothing to
	// guard at the call sites.
	Metrics *metrics.Registry
}

const (
	maxUDPPacket = 65535

	// sessionIdleTimeout is how long a client session survives without
	// traffic. WireGuard re-handshakes every 2 minutes and its optional
	// keepalive runs every 25 seconds, so a few minutes of silence means
	// the peer is really gone rather than merely quiet.
	sessionIdleTimeout = 5 * time.Minute

	// sessionSweepInterval is how often expired sessions are reaped.
	sessionSweepInterval = 30 * time.Second

	// maxSessions bounds the session table so a bug or a key leak can't
	// exhaust file descriptors on the host. Reaching it evicts the
	// quietest peer rather than refusing the newest one.
	maxSessions = 4096

	// replayCacheSize and replayWindow bound the memory the replay guard
	// uses. Only session-opening packets are recorded, so this covers
	// far more peers than maxSessions allows to exist at once.
	replayCacheSize = 16384
	replayWindow    = 10 * time.Minute
)

// RunClient proxies plaintext WireGuard packets from a local WireGuard
// client (connected to LocalAddr) out to RemoteWireAddr in obfuscated form,
// and delivers obfuscated responses back as plaintext.
func RunClient(ctx context.Context, cfg Config) error {
	// Derived so every helper goroutine below is torn down when this
	// function returns, not just when the caller's context ends.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	obf, err := obfuscator.NewMulti(cfg.PSKs)
	if err != nil {
		return err
	}

	localConn, err := net.ListenPacket("udp", cfg.LocalAddr)
	if err != nil {
		return err
	}
	defer localConn.Close()
	tuneSocketBuffers(localConn)

	remoteAddr, err := net.ResolveUDPAddr("udp", cfg.RemoteWireAddr)
	if err != nil {
		return err
	}
	wireConn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		return err
	}
	defer wireConn.Close()
	tuneUDPBuffers(wireConn)

	if cfg.JunkPackets > 0 {
		if err := sendJunk(wireConn, cfg.JunkPackets); err != nil {
			log.Printf("transport: junk send failed (continuing): %v", err)
		}
	}

	// The local WireGuard endpoint's address, learned from its first
	// packet. Held atomically because the two pump goroutines below run
	// concurrently: one writes it, the other reads it for every reply.
	var localPeer atomic.Pointer[net.Addr]

	go closeOnDone(ctx, localConn, wireConn)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer wireConn.Close()

		buf := make([]byte, maxUDPPacket)
		for {
			n, addr, err := localConn.ReadFrom(buf)
			if err != nil {
				return
			}
			localPeer.Store(&addr)

			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			warnIfOversized(len(wrapped))
			if _, err := wireConn.Write(wrapped); err != nil {
				log.Printf("transport: write to wire failed: %v", err)
			}
		}
	}()

	buf := make([]byte, maxUDPPacket)
	for {
		n, err := wireConn.Read(buf)
		if err != nil {
			localConn.Close()
			wg.Wait()
			return ctxErrOr(ctx, err)
		}
		plaintext, err := obf.Unwrap(buf[:n])
		if err != nil {
			// Could be a junk packet reflected back, or noise
			// injected by an on-path observer. Drop silently.
			continue
		}
		peer := localPeer.Load()
		if peer == nil {
			continue
		}
		if _, err := localConn.WriteTo(plaintext, *peer); err != nil {
			log.Printf("transport: write to local failed: %v", err)
		}
	}
}

// RunServer listens for obfuscated wire traffic and forwards decoded
// WireGuard packets to a local WireGuard server, relaying responses back
// in obfuscated form.
//
// Each remote peer gets its own socket to the local WireGuard server, so
// replies are routed back to the peer they belong to. A single shared
// socket would deliver every reply to whichever peer transmitted last —
// which looks like it works with one client and silently cross-wires
// traffic as soon as there are two.
func RunServer(ctx context.Context, cfg Config) error {
	// Derived so the session reaper and the shutdown watcher stop when
	// this function returns, however it returns.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	registry, err := registryFor(cfg.Clients, cfg.PSKs)
	if err != nil {
		return err
	}

	localAddr, err := net.ResolveUDPAddr("udp", cfg.LocalAddr)
	if err != nil {
		return err
	}

	wireConn, err := net.ListenPacket("udp", cfg.ListenWireAddr)
	if err != nil {
		return err
	}
	defer wireConn.Close()
	tuneSocketBuffers(wireConn)

	sessions := newSessionTable(wireConn, localAddr, registry)
	sessions.metrics = cfg.Metrics
	defer sessions.closeAll()

	go sessions.reapLoop(ctx)
	go closeOnDone(ctx, wireConn)

	buf := make([]byte, maxUDPPacket)
	for {
		n, addr, err := wireConn.ReadFrom(buf)
		if err != nil {
			return ctxErrOr(ctx, err)
		}
		wirePacket := buf[:n]

		// A peer we already know decrypts under the key it authenticated
		// with — one AEAD attempt, however many clients are configured.
		if session, ok := sessions.lookup(addr); ok {
			plaintext, err := session.obf.Unwrap(wirePacket)
			if err != nil {
				continue
			}
			session.touch()
			cfg.Metrics.PacketIn(session.clientID, len(plaintext))
			if _, err := session.localConn.Write(plaintext); err != nil {
				log.Printf("transport: write to local failed: %v", err)
			}
			continue
		}

		// An unknown peer costs one attempt per credential. This is the
		// only place that search happens, and it runs before any state
		// is allocated: a spoofed source address that fails to
		// authenticate leaves nothing behind, and gets no reply that
		// would tell a prober it found the right port.
		//
		// It is also the one path an attacker can make expensive, so it
		// is rate limited. Dropping here costs nothing and stays silent,
		// exactly like any other unauthenticated packet.
		auth := sessions.authenticator()
		if !sessions.trials.allow() {
			cfg.Metrics.TrialThrottled()
			continue
		}

		plaintext, clientID, obf, ok := auth.authenticate(wirePacket)
		if !ok {
			cfg.Metrics.AuthFailure()
			continue
		}

		session, err := sessions.open(addr, wirePacket, clientID, obf)
		if err != nil {
			if errors.Is(err, errReplayedPacket) {
				cfg.Metrics.ReplayRejected()
			} else {
				log.Printf("transport: cannot serve peer %s: %v", addr, err)
			}
			continue
		}
		cfg.Metrics.PacketIn(clientID, len(plaintext))
		if _, err := session.localConn.Write(plaintext); err != nil {
			log.Printf("transport: write to local failed: %v", err)
		}
	}
}

// session is one remote peer's private path to the local WireGuard
// server, together with the credential that peer authenticated under.
type session struct {
	localConn  *net.UDPConn
	peerAddr   net.Addr
	clientID   string
	obf        *obfuscator.Obfuscator
	lastActive atomic.Int64 // unix nanoseconds
	closeOnce  sync.Once
}

func (s *session) touch() {
	s.lastActive.Store(time.Now().UnixNano())
}

func (s *session) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, s.lastActive.Load()))
}

func (s *session) close() {
	s.closeOnce.Do(func() { s.localConn.Close() })
}

type sessionTable struct {
	mu          sync.Mutex
	sessions    map[string]*session
	wireConn    net.PacketConn
	localAddr   *net.UDPAddr
	registry    *clients.Registry
	maxSessions int
	replay      *replayGuard
	metrics     *metrics.Registry
	trials      *trialLimiter

	// auth is rebuilt whenever the credential set changes, so a reload
	// costs one rebuild rather than a comparison on every packet.
	authMu    sync.Mutex
	authSet   *clients.Set
	authCache *authenticator
}

func newSessionTable(wireConn net.PacketConn, localAddr *net.UDPAddr, registry *clients.Registry) *sessionTable {
	return &sessionTable{
		sessions:    make(map[string]*session),
		wireConn:    wireConn,
		localAddr:   localAddr,
		registry:    registry,
		maxSessions: maxSessions,
		replay:      newReplayGuard(replayCacheSize, replayWindow),
		trials:      newTrialLimiter(len(registry.Current().Credentials())),
	}
}

// authenticator returns the current candidate keys, rebuilding them only
// when the credential set has actually been swapped.
func (t *sessionTable) authenticator() *authenticator {
	set := t.registry.Current()

	t.authMu.Lock()
	defer t.authMu.Unlock()

	if t.authCache != nil && t.authSet == set {
		return t.authCache
	}
	auth, err := newAuthenticator(set, nil)
	if err != nil {
		// Only possible from a malformed key, which the credential
		// loader already rejects. Keep serving the previous set rather
		// than locking every client out.
		log.Printf("transport: could not rebuild credentials: %v", err)
		if t.authCache != nil {
			return t.authCache
		}
		return &authenticator{}
	}
	t.authSet, t.authCache = set, auth
	t.trials.resize(len(set.Credentials()))
	return auth
}

// lookup returns an established session for a peer, without creating one.
func (t *sessionTable) lookup(peerAddr net.Addr) (*session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[peerAddr.String()]
	return s, ok
}

// open creates a session for a peer that has just authenticated, keeping
// the credential it used so every later packet costs a single decryption.
//
// The replay check happens here rather than at authentication: a packet
// that has already opened a session must not open another from a
// different source address, which is how one captured packet would
// otherwise be turned into unlimited server state.
func (t *sessionTable) open(peerAddr net.Addr, wirePacket []byte, clientID string, obf *obfuscator.Obfuscator) (*session, error) {
	if len(wirePacket) < obfuscator.NonceSize {
		return nil, ErrInvalidPacket
	}
	if !t.replay.admit(wirePacket[:obfuscator.NonceSize]) {
		return nil, errReplayedPacket
	}

	key := peerAddr.String()

	t.mu.Lock()
	if existing, ok := t.sessions[key]; ok {
		t.mu.Unlock()
		existing.touch()
		return existing, nil
	}
	if len(t.sessions) >= t.maxSessions {
		// Evict the least recently active peer rather than turning the
		// newcomer away: refusing would let whoever filled the table
		// lock out every client that arrives afterwards.
		if victim := t.leastRecentlyActiveLocked(); victim != nil {
			delete(t.sessions, victim.peerAddr.String())
			defer victim.close()
		}
	}
	t.mu.Unlock()

	localConn, err := net.DialUDP("udp", nil, t.localAddr)
	if err != nil {
		return nil, err
	}
	tuneUDPBuffers(localConn)

	s := &session{
		localConn: localConn,
		peerAddr:  peerAddr,
		clientID:  clientID,
		obf:       obf,
	}
	s.touch()

	t.mu.Lock()
	// Another packet from the same peer may have raced us here; keep
	// whichever session landed first so both packets share one socket.
	if existing, ok := t.sessions[key]; ok {
		t.mu.Unlock()
		localConn.Close()
		existing.touch()
		return existing, nil
	}
	t.sessions[key] = s
	t.mu.Unlock()

	t.metrics.SessionOpened(clientID)
	go t.pumpReplies(s, key)
	return s, nil
}

// pumpReplies forwards everything the local WireGuard server sends back to
// the peer that owns this session, wrapped under that peer's own key.
func (t *sessionTable) pumpReplies(s *session, key string) {
	defer func() {
		s.close()
		t.mu.Lock()
		if t.sessions[key] == s {
			delete(t.sessions, key)
		}
		t.mu.Unlock()
		t.metrics.SessionClosed()
	}()

	buf := make([]byte, maxUDPPacket)
	for {
		n, err := s.localConn.Read(buf)
		if err != nil {
			return
		}
		s.touch()

		wrapped, err := s.obf.Wrap(buf[:n])
		if err != nil {
			log.Printf("transport: wrap failed: %v", err)
			continue
		}
		warnIfOversized(len(wrapped))
		t.metrics.PacketOut(s.clientID, n)
		if _, err := t.wireConn.WriteTo(wrapped, s.peerAddr); err != nil {
			log.Printf("transport: write to wire failed: %v", err)
			return
		}
	}
}

// disconnectRevoked closes sessions whose client no longer has access, so
// revoking a credential ends the tunnel that credential is holding open
// rather than only refusing the next one. Reports how many were closed.
func (t *sessionTable) disconnectRevoked() int {
	set := t.registry.Current()

	var revoked []*session
	t.mu.Lock()
	for _, s := range t.sessions {
		if !set.IsEnabled(s.clientID) {
			revoked = append(revoked, s)
		}
	}
	t.mu.Unlock()

	for _, s := range revoked {
		log.Printf("transport: disconnecting %s: client %q was revoked", s.peerAddr, s.clientID)
		s.close()
	}
	t.metrics.RevocationEnforced(len(revoked))
	return len(revoked)
}

// reapLoop closes sessions that have gone quiet, so a server that has
// served many short-lived peers doesn't hold their sockets forever.
func (t *sessionTable) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(sessionSweepInterval)
	defer ticker.Stop()

	// Revocation runs on its own, faster clock. Reaping idle sessions can
	// afford to be lazy — a socket held a little longer costs a socket.
	// A credential that keeps carrying traffic after being withdrawn
	// costs whatever it was withdrawn to prevent, so the two are not the
	// same deadline and should not share one.
	revocations := time.NewTicker(revocationCheckInterval)
	defer revocations.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-revocations.C:
			t.disconnectRevoked()
		case now := <-ticker.C:
			t.disconnectRevoked()

			var expired []*session
			t.mu.Lock()
			for _, s := range t.sessions {
				if s.idleFor(now) > sessionIdleTimeout {
					expired = append(expired, s)
				}
			}
			t.mu.Unlock()

			// Closing the socket ends pumpReplies, which removes
			// the session from the table.
			for _, s := range expired {
				s.close()
			}
		}
	}
}

func (t *sessionTable) closeAll() {
	t.mu.Lock()
	all := make([]*session, 0, len(t.sessions))
	for _, s := range t.sessions {
		all = append(all, s)
	}
	t.mu.Unlock()

	for _, s := range all {
		s.close()
	}
}

// leastRecentlyActiveLocked returns the session that has been quiet
// longest. The caller must hold t.mu.
func (t *sessionTable) leastRecentlyActiveLocked() *session {
	var oldest *session
	for _, s := range t.sessions {
		if oldest == nil || s.lastActive.Load() < oldest.lastActive.Load() {
			oldest = s
		}
	}
	return oldest
}

// count reports how many sessions are currently live (used by tests).
func (t *sessionTable) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

func sendJunk(conn *net.UDPConn, count int) error {
	for i := 0; i < count; i++ {
		junk, err := obfuscator.Junk()
		if err != nil {
			return err
		}
		if _, err := conn.Write(junk); err != nil {
			return err
		}
	}
	return nil
}

// closeOnDone closes the given connections when ctx is cancelled, which
// unblocks the read loops so shutdown is immediate rather than waiting on
// a packet that may never arrive.
func closeOnDone(ctx context.Context, conns ...interface{ Close() error }) {
	<-ctx.Done()
	for _, c := range conns {
		c.Close()
	}
}

// ctxErrOr reports a cancelled context rather than the "use of closed
// network connection" error that cancellation causes, so a clean shutdown
// isn't logged as a failure.
func ctxErrOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
