package provision

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"sync"
)

// Device is the running AmneziaWG interface, as much of it as issuing a
// credential needs.
//
// It is an interface because the real implementation shells out to `awg`
// on a server this code cannot reach from a test. Everything worth
// getting right — allocation, idempotency, the peer cap, reaping — sits
// above this line and is tested against a fake; below it is one exec
// call per method.
type Device interface {
	// Peers reports every peer the interface currently has.
	Peers(ctx context.Context) ([]Peer, error)

	// AddPeer adds one peer routed to a single address.
	AddPeer(ctx context.Context, publicKey string, address netip.Prefix) error

	// RemovePeer withdraws a peer.
	RemovePeer(ctx context.Context, publicKey string) error
}

// Settings are what every issued client shares: where to connect, what
// to route, and the obfuscation parameters the server expects.
// Registry remembers which peers this service created.
//
// The interface is shared with Amnezia's own clients, in the same
// subnet, so a peer's presence says nothing about whose it is. Anything
// that removes peers asks this first.
type Registry interface {
	Owns(publicKey string) bool
	Add(ctx context.Context, publicKey string) error
	Remove(ctx context.Context, publicKey string) error
}

type Settings struct {
	// Endpoint is the server's public address, host:port.
	Endpoint string

	// ServerPublicKey is the interface's own public key.
	ServerPublicKey string

	// DNS is handed to clients; empty leaves the field out.
	DNS []netip.Addr

	// AllowedIPs decides what the client routes into the tunnel.
	// "0.0.0.0/0, ::/0" is a full tunnel.
	AllowedIPs string

	MTU       int
	Keepalive int

	Params Params
}

// MaxPeers bounds how many credentials one server will hand out.
//
// The bound is not about disk. A handshake is matched against the peer
// list, so the work an unauthenticated packet costs the server grows
// with the number of peers — the same shape of fault as finding 22 in
// docs/AUDIT.md, arriving by a different road. A free app that issues a
// credential per install and never takes one back walks into it on its
// own, without anybody attacking anything.
const MaxPeers = 4000

var (
	ErrTooManyPeers = errors.New("provision: the server is at its peer limit")
	ErrInvalidKey   = errors.New("provision: not a WireGuard public key")
)

// Service issues one credential per device.
type Service struct {
	registry Registry
	device   Device
	pool     *Pool
	settings Settings
	maxPeers int

	// Issuing is serialised. Reading the peer list, picking a free
	// address and adding the peer has to be one indivisible step: two
	// requests that read the same list before either writes would both
	// pick the same address, and the server would then route one
	// address to whichever peer was added last. Once per install is not
	// a throughput problem.
	mu sync.Mutex
}

// NewService prepares issuing against a device.
func NewService(device Device, pool *Pool, settings Settings) (*Service, error) {
	if device == nil || pool == nil {
		return nil, errors.New("provision: a device and a pool are required")
	}
	if settings.Endpoint == "" {
		return nil, errors.New("provision: an endpoint is required; clients have nowhere to connect without one")
	}
	if settings.ServerPublicKey == "" {
		return nil, errors.New("provision: the server's public key is required")
	}
	return &Service{device: device, pool: pool, settings: settings, maxPeers: MaxPeers}, nil
}

// SetMaxPeers overrides the peer ceiling, for a server sized differently
// from the default assumption.
// SetRegistry records every peer this service creates from now on.
func (s *Service) SetRegistry(r Registry) {
	s.mu.Lock()
	s.registry = r
	s.mu.Unlock()
}

func (s *Service) SetMaxPeers(n int) {
	if n > 0 {
		s.mu.Lock()
		s.maxPeers = n
		s.mu.Unlock()
	}
}

// Issue returns the configuration for a device that has just generated
// itself a key.
//
// Asking twice with the same public key returns the same address rather
// than allocating a second one. That is not a nicety: a reply lost on
// the way back to a phone is an ordinary event, the app retries, and
// without this every retry would burn another address and leave a peer
// nobody uses.
func (s *Service) Issue(ctx context.Context, publicKey string) (Config, error) {
	if err := ValidatePublicKey(publicKey); err != nil {
		return Config{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	peers, err := s.device.Peers(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("provision: reading the interface: %w", err)
	}

	for _, peer := range peers {
		if peer.PublicKey == publicKey {
			if len(peer.Addresses) == 0 {
				// A peer with no route is useless to its owner and
				// cannot be repaired by handing back an empty address.
				return Config{}, fmt.Errorf("provision: this key is already a peer but has no address; remove it and try again")
			}
			return s.configFor(peer.Addresses[0].Addr()), nil
		}
	}

	if len(peers) >= s.maxPeers {
		return Config{}, ErrTooManyPeers
	}

	addr, err := s.pool.Allocate(peers)
	if err != nil {
		return Config{}, err
	}

	prefix := netip.PrefixFrom(addr, addr.BitLen())
	if err := s.device.AddPeer(ctx, publicKey, prefix); err != nil {
		return Config{}, fmt.Errorf("provision: adding the peer: %w", err)
	}
	// Recorded only here, where this service created the peer — not on
	// the path above that hands back an existing one. Anyone can present
	// a public key; knowing one must not make its peer ours to remove.
	if s.registry != nil {
		if err := s.registry.Add(ctx, publicKey); err != nil {
			// The client is served either way. Unrecorded, the peer is
			// never withdrawn automatically, which is the safe side.
			log.Printf("provision: could not record an issued credential: %v", err)
		}
	}
	return s.configFor(addr), nil
}

func (s *Service) configFor(addr netip.Addr) Config {
	return Config{
		Address:  netip.PrefixFrom(addr, addr.BitLen()),
		Settings: s.settings,
	}
}

// Config is what one device needs to connect.
//
// It carries no private key, by construction: the device made its own
// and this service never saw it.
type Config struct {
	Address  netip.Prefix
	Settings Settings
}

// ValidatePublicKey checks that a submitted key is what a WireGuard
// public key looks like.
//
// Anything else would be written into the interface's configuration, so
// this is the boundary where text from the internet stops being text.
func ValidatePublicKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: it is empty", ErrInvalidKey)
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("%w: it is not base64", ErrInvalidKey)
	}
	if len(raw) != 32 {
		return fmt.Errorf("%w: it decodes to %d bytes, a key is 32", ErrInvalidKey, len(raw))
	}

	// An all-zero key is not a key any device generated; it is what an
	// empty buffer encodes to.
	var zero [32]byte
	if string(raw) == string(zero[:]) {
		return fmt.Errorf("%w: it is all zeroes", ErrInvalidKey)
	}
	return nil
}
