package transport

import (
	"net"
	"sync"
	"testing"
)

var (
	issuedPortsMu sync.Mutex
	issuedPorts   = map[string]bool{}
)

// reserveAddr returns a loopback address the operating system is offering,
// and never returns the same one twice within this process.
//
// The obvious implementation — bind port 0, read the address back, close —
// can hand the same port to two consecutive callers, because the port is
// free again the instant it is read. A test that allocates several
// addresses then wires two of its components to the same one: the
// topology is silently wrong, and the failure surfaces much later as a
// timeout somewhere unrelated. Remembering what has already been issued
// turns that collision into a retry.
func reserveAddr(t *testing.T, network string) string {
	t.Helper()

	for attempt := 0; attempt < 100; attempt++ {
		var addr string
		switch network {
		case "udp":
			c, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr = c.LocalAddr().String()
			c.Close()
		case "tcp":
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr = ln.Addr().String()
			ln.Close()
		default:
			t.Fatalf("unsupported network %q", network)
		}

		issuedPortsMu.Lock()
		fresh := !issuedPorts[addr]
		issuedPorts[addr] = true
		issuedPortsMu.Unlock()

		if fresh {
			return addr
		}
	}

	t.Fatalf("could not find an unused %s port after 100 attempts", network)
	return ""
}

func freeUDPAddr(t *testing.T) string { return reserveAddr(t, "udp") }
func freeTCPAddr(t *testing.T) string { return reserveAddr(t, "tcp") }
