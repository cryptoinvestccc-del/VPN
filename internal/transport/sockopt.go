package transport

import (
	"log"
	"net"
	"sync"
)

// socketBufferSize is what we ask the kernel for on the wire-facing
// socket, in both directions.
//
// The default receive buffer is small — a couple of hundred kilobytes on
// most Linux systems — and it is the kernel, not this program, that drops
// a packet when it fills. A forwarding server sees bursts: many clients
// waking at once, a reconnect storm after an outage, or simply somebody
// flooding the port. During a burst the legitimate packet that arrives
// while the buffer is full is discarded before any code here can look at
// it.
//
// Asking for more does not eliminate loss — nothing can, on UDP — but it
// absorbs the bursts that would otherwise cost a client a retransmit.
const socketBufferSize = 4 << 20 // 4 MiB

var bufferWarning sync.Once

// tuneSocketBuffers enlarges a UDP socket's kernel buffers where the
// system allows it.
//
// Linux silently caps the request at net.core.rmem_max, so the result is
// usually smaller than asked for. That is fine and needs no action from
// the operator; only an outright failure is worth a line in the log, and
// only once.
func tuneSocketBuffers(conn net.PacketConn) {
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		return
	}
	tuneUDPBuffers(udp)
}

func tuneUDPBuffers(conn *net.UDPConn) {
	if err := conn.SetReadBuffer(socketBufferSize); err != nil {
		bufferWarning.Do(func() {
			log.Printf("transport: could not enlarge the socket receive buffer (%v); "+
				"bursts of traffic may be dropped by the kernel before they reach us", err)
		})
	}
	// The send buffer matters less — writes are paced by what arrives —
	// but a burst of replies to many peers benefits from the same room.
	_ = conn.SetWriteBuffer(socketBufferSize)
}
