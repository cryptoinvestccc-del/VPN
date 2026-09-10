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

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

var (
	errReplayedPacket = errors.New("transport: packet already opened a session")

	// ErrInvalidPacket reports a packet too short to be one of ours.
	ErrInvalidPacket = errors.New("transport: malformed packet")
)

// Config configures one end of the obfuscated tunnel.
type Config struct {
	// PSKs are the shared secrets used to wrap/unwrap packets, current
	// key first. Wrap uses PSKs[0]; Unwrap accepts any of them, which is
	// what allows rotating to a new key without downtime (see
	// config.File.PSKs).
	PSKs [][32]byte

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

	remoteAddr, err := net.ResolveUDPAddr("udp", cfg.RemoteWireAddr)
	if err != nil {
		return err
	}
	wireConn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		return err
	}
	defer wireConn.Close()

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

	obf, err := obfuscator.NewMulti(cfg.PSKs)
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

	sessions := newSessionTable(wireConn, localAddr, obf)
	defer sessions.closeAll()

	go sessions.reapLoop(ctx)
	go closeOnDone(ctx, wireConn)

	buf := make([]byte, maxUDPPacket)
	for {
		n, addr, err := wireConn.ReadFrom(buf)
		if err != nil {
			return ctxErrOr(ctx, err)
		}

		// Authenticate before touching the session table: an
		// unauthenticated packet must never cause us to allocate a
		// socket, so spoofed source addresses can't exhaust
		// resources. Junk packets and DPI probes land here too and
		// are dropped without any response, which is what keeps the
		// port from behaving like an oracle.
		wirePacket := buf[:n]
		plaintext, err := obf.Unwrap(wirePacket)
		if err != nil {
			continue
		}

		session, err := sessions.getForPacket(addr, wirePacket)
		if err != nil {
			if !errors.Is(err, errReplayedPacket) {
				log.Printf("transport: cannot serve peer %s: %v", addr, err)
			}
			continue
		}
		if _, err := session.localConn.Write(plaintext); err != nil {
			log.Printf("transport: write to local failed: %v", err)
		}
	}
}

// session is one remote peer's private path to the local WireGuard
// server.
type session struct {
	localConn  *net.UDPConn
	peerAddr   net.Addr
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
	obf         *obfuscator.Obfuscator
	maxSessions int
	replay      *replayGuard
}

func newSessionTable(wireConn net.PacketConn, localAddr *net.UDPAddr, obf *obfuscator.Obfuscator) *sessionTable {
	return &sessionTable{
		sessions:    make(map[string]*session),
		wireConn:    wireConn,
		localAddr:   localAddr,
		obf:         obf,
		maxSessions: maxSessions,
		replay:      newReplayGuard(replayCacheSize, replayWindow),
	}
}

// getForPacket resolves the session for a peer, given the wire packet that
// arrived from it. Known peers are served directly; an unknown peer opens
// a session only if its packet is not one we have already seen open a
// session, which is what stops a captured packet from being replayed into
// unlimited server state.
func (t *sessionTable) getForPacket(peerAddr net.Addr, wirePacket []byte) (*session, error) {
	key := peerAddr.String()

	t.mu.Lock()
	if s, ok := t.sessions[key]; ok {
		t.mu.Unlock()
		s.touch()
		return s, nil
	}
	t.mu.Unlock()

	if len(wirePacket) < obfuscator.NonceSize {
		return nil, ErrInvalidPacket
	}
	if !t.replay.admit(wirePacket[:obfuscator.NonceSize]) {
		return nil, errReplayedPacket
	}
	return t.get(peerAddr)
}

// get returns the session for peerAddr, creating one (with its own socket
// to the local WireGuard server and a goroutine pumping replies back) if
// this peer hasn't been seen before.
func (t *sessionTable) get(peerAddr net.Addr) (*session, error) {
	key := peerAddr.String()

	t.mu.Lock()
	if s, ok := t.sessions[key]; ok {
		t.mu.Unlock()
		s.touch()
		return s, nil
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

	s := &session{localConn: localConn, peerAddr: peerAddr}
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

	go t.pumpReplies(s, key)
	return s, nil
}

// pumpReplies forwards everything the local WireGuard server sends back to
// the peer that owns this session, wrapped for the wire.
func (t *sessionTable) pumpReplies(s *session, key string) {
	defer func() {
		s.close()
		t.mu.Lock()
		if t.sessions[key] == s {
			delete(t.sessions, key)
		}
		t.mu.Unlock()
	}()

	buf := make([]byte, maxUDPPacket)
	for {
		n, err := s.localConn.Read(buf)
		if err != nil {
			return
		}
		s.touch()

		wrapped, err := t.obf.Wrap(buf[:n])
		if err != nil {
			log.Printf("transport: wrap failed: %v", err)
			continue
		}
		warnIfOversized(len(wrapped))
		if _, err := t.wireConn.WriteTo(wrapped, s.peerAddr); err != nil {
			log.Printf("transport: write to wire failed: %v", err)
			return
		}
	}
}

// reapLoop closes sessions that have gone quiet, so a server that has
// served many short-lived peers doesn't hold their sockets forever.
func (t *sessionTable) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(sessionSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
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
