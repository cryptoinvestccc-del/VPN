package provision

import (
	"errors"
	"fmt"
	"net/netip"
)

// ErrPoolFull is returned when every address in the subnet is taken.
var ErrPoolFull = errors.New("provision: no addresses left in the pool")

// Pool hands out one tunnel address per client.
//
// It keeps no records of its own. The set of addresses in use is read
// from the running interface every time, because the interface is the
// only thing that actually decides where packets go — a separate ledger
// can disagree with it, and the way it disagrees is by handing a second
// client an address the first one is already using. Two clients on one
// address is not a degraded tunnel: the server routes that address to
// one peer, so their traffic lands on a stranger's device.
//
// Reading the truth each time is slower. It is slower once per install,
// against a fault that is silent and unrecoverable, so the trade is not
// close.
type Pool struct {
	subnet netip.Prefix

	// reserved holds addresses that are never handed out: the network
	// address, the server's own, and the broadcast address on IPv4.
	reserved map[netip.Addr]bool
}

// NewPool prepares allocation from a subnet, reserving the server's
// address and the edges.
func NewPool(subnet netip.Prefix, serverAddr netip.Addr) (*Pool, error) {
	if !subnet.IsValid() {
		return nil, fmt.Errorf("provision: %q is not a valid subnet", subnet)
	}
	subnet = subnet.Masked()

	if subnet.Addr().Is4() && subnet.Bits() > 30 {
		return nil, fmt.Errorf("provision: subnet %s is too small to hold any client", subnet)
	}

	p := &Pool{subnet: subnet, reserved: map[netip.Addr]bool{}}
	p.reserved[subnet.Addr()] = true // the network address
	if serverAddr.IsValid() {
		p.reserved[serverAddr] = true
	}
	if last, ok := lastAddr(subnet); ok && subnet.Addr().Is4() {
		p.reserved[last] = true // broadcast
	}
	return p, nil
}

// Allocate returns the lowest free address, given the peers the
// interface currently has.
//
// Lowest-free rather than random: an address freed by a reaped peer is
// reused, which is what keeps a free service from exhausting a /24 after
// a few hundred installs and uninstalls. The cost is that addresses are
// predictable, which matters only if the tunnel's inside is treated as a
// secret — it is not, and never was.
func (p *Pool) Allocate(peers []Peer) (netip.Addr, error) {
	taken := make(map[netip.Addr]bool, len(peers)+len(p.reserved))
	for addr := range p.reserved {
		taken[addr] = true
	}
	for _, peer := range peers {
		for _, prefix := range peer.Addresses {
			// A peer may legitimately carry a wider route; only single
			// addresses take a slot out of the pool.
			taken[prefix.Addr()] = true
		}
	}

	for addr := p.subnet.Addr(); p.subnet.Contains(addr); addr = addr.Next() {
		if !taken[addr] {
			return addr, nil
		}
		if !addr.Next().IsValid() {
			break
		}
	}
	return netip.Addr{}, ErrPoolFull
}

// Subnet is the range this pool allocates from.
func (p *Pool) Subnet() netip.Prefix { return p.subnet }

// lastAddr returns the highest address in a prefix.
func lastAddr(prefix netip.Prefix) (netip.Addr, bool) {
	addr := prefix.Addr()
	if !addr.IsValid() {
		return netip.Addr{}, false
	}
	bytes := addr.AsSlice()
	bits := prefix.Bits()

	for i := range bytes {
		// Bits beyond the prefix length are set to one.
		for bit := 0; bit < 8; bit++ {
			if i*8+bit >= bits {
				bytes[i] |= 1 << (7 - bit)
			}
		}
	}
	last, ok := netip.AddrFromSlice(bytes)
	return last, ok
}
