package transport

import (
	"log"
	"sync"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

// RecommendedWireGuardMTU is the largest WireGuard interface MTU that
// still lets a full-size packet fit inside a single datagram once this
// tunnel wraps it.
//
// The arithmetic, working outwards from a 1500-byte path MTU:
//
//	1500 − 40 (IPv6 header) − 8 (UDP header)     = 1452  wrapped packet budget
//	1452 − 42 (nonce + length + AEAD tag)        = 1410  WireGuard packet
//	1410 − 32 (WireGuard header + its own tag)   = 1378  encrypted payload
//	1378 rounded down to WireGuard's 16-byte pad = 1376  interface MTU
//
// WireGuard's default MTU of 1420 is chosen for carrying WireGuard
// directly over IP. Running it through a second layer of encapsulation —
// which is what this tunnel is — leaves it too large, and the result is
// silent IP fragmentation: throughput drops, and fragmented flows are a
// traffic pattern of their own, which is the opposite of what an
// obfuscator is for.
const RecommendedWireGuardMTU = 1376

var oversizedWarning sync.Once

// warnIfOversized reports, once per process, that wrapped packets no
// longer fit the path MTU. Logging per packet would flood the journal at
// line rate; logging never would leave an operator with an unexplained
// throughput problem and no way to connect it to the MTU.
func warnIfOversized(wrappedSize int) {
	if wrappedSize <= obfuscator.SafeWireSize {
		return
	}
	oversizedWarning.Do(func() {
		log.Printf("transport: wrapped packets reach %d bytes, above the %d-byte "+
			"budget for a 1500-byte path — they will fragment. Lower the WireGuard "+
			"interface MTU to %d (MTU = %d in the [Interface] section of wg0.conf).",
			wrappedSize, obfuscator.SafeWireSize, RecommendedWireGuardMTU, RecommendedWireGuardMTU)
	})
}
