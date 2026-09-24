package main

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"syscall"

	"github.com/amnezia-vpn/amneziawg-go/conn"
)

// phoneBind is the engine's UDP layer, written for what an Android app
// is allowed to do.
//
// The library's own layer, built for linux as this engine is, turns on
// "sticky sockets": it records which local address each packet arrived
// on, and to keep that record current when the phone's routes change it
// opens a netlink socket and binds it to the kernel's route updates.
// Android 11 stopped letting ordinary apps bind NETLINK_ROUTE. The bind
// is refused with EACCES, bringing the device up fails, and a phone
// reported exactly that: "Unable to update bind: permission denied".
//
// The official Android client never hits this because it is built with
// GOOS=android, which switches sticky sockets off. That build needs cgo
// for 32-bit ARM, and cgo needs the NDK, which cannot be fetched here.
// So instead of a different build, this is a different layer: plain UDP
// sockets, no source-address tracking, nothing that needs netlink.
//
// The device only looks for route updates when its layer is the
// library's own type, so using this one switches that off as well.
//
// What is given up is the sticky source address, which matters on
// multi-homed servers and not on a phone, and the kernel's UDP batching,
// which matters at speeds a phone does not reach.
type phoneBind struct {
	mu sync.Mutex
	v4 *net.UDPConn
	v6 *net.UDPConn
}

var _ conn.Bind = (*phoneBind)(nil)

var errNotOurEndpoint = errors.New("endpoint was not made by this layer")

func newPhoneBind() *phoneBind { return &phoneBind{} }

// Open listens for IPv4 and, where the phone has it, IPv6, on one port.
func (b *phoneBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.v4 != nil || b.v6 != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}

	var fns []conn.ReceiveFunc
	actual := port

	v4, err := net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
	switch {
	case err == nil:
		b.v4 = v4
		actual = uint16(v4.LocalAddr().(*net.UDPAddr).Port)
		fns = append(fns, receiveOn(v4))
	case !errors.Is(err, syscall.EAFNOSUPPORT):
		return nil, 0, err
	}

	// IPv6 on the same port if it can be had. A phone on a network
	// without IPv6, or a port already taken on the IPv6 side, still has
	// a working IPv4 path, and the server is reached over IPv4 anyway
	// whenever its name has an A record.
	v6, err := net.ListenUDP("udp6", &net.UDPAddr{Port: int(actual)})
	if err == nil {
		b.v6 = v6
		if actual == 0 {
			actual = uint16(v6.LocalAddr().(*net.UDPAddr).Port)
		}
		fns = append(fns, receiveOn(v6))
	}

	if len(fns) == 0 {
		if err != nil {
			return nil, 0, err
		}
		return nil, 0, syscall.EAFNOSUPPORT
	}
	return fns, actual, nil
}

// receiveOn reads one packet at a time; BatchSize says so.
func receiveOn(c *net.UDPConn) conn.ReceiveFunc {
	return func(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		n, from, err := c.ReadFromUDPAddrPort(packets[0])
		if err != nil {
			return 0, err
		}
		sizes[0] = n
		eps[0] = &conn.StdNetEndpoint{AddrPort: unmapped(from)}
		return 1, nil
	}
}

func (b *phoneBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var first error
	for _, c := range []*net.UDPConn{b.v4, b.v6} {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	b.v4, b.v6 = nil, nil
	return first
}

// SetMark does nothing. A mark keeps the engine's own packets out of
// the tunnel on a desktop; on Android the app excludes itself from its
// own VPN, which covers this process too, and setting SO_MARK would need
// a privilege an app does not have.
func (b *phoneBind) SetMark(uint32) error { return nil }

func (b *phoneBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	e, ok := ep.(*conn.StdNetEndpoint)
	if !ok {
		return errNotOurEndpoint
	}
	dst := unmapped(e.AddrPort)

	b.mu.Lock()
	c := b.v4
	if dst.Addr().Is6() {
		c = b.v6
	}
	b.mu.Unlock()
	if c == nil {
		return syscall.EAFNOSUPPORT
	}

	for _, buf := range bufs {
		if _, err := c.WriteToUDPAddrPort(buf, dst); err != nil {
			return err
		}
	}
	return nil
}

func (b *phoneBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &conn.StdNetEndpoint{AddrPort: unmapped(ap)}, nil
}

func (b *phoneBind) BatchSize() int { return 1 }

// unmapped turns ::ffff:1.2.3.4 into 1.2.3.4, so an IPv4 peer is always
// sent to over the IPv4 socket whichever way its address was written.
func unmapped(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
