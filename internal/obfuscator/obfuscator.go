// Package obfuscator wraps arbitrary UDP payloads (WireGuard packets) in an
// AEAD-encrypted envelope with random padding, so the wire format carries no
// recognizable WireGuard signature. It does not replace WireGuard's own
// encryption — it hides the shape of the traffic that carries it.
package obfuscator

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	// XChaCha20-Poly1305 (24-byte nonce) rather than the 12-byte-nonce
	// variant: nonces here are drawn at random for every packet, and a
	// 192-bit nonce makes a repeat statistically unreachable no matter
	// how long a key stays in service. With a 96-bit nonce a busy server
	// on a static PSK would approach the birthday bound within days.
	nonceSize  = chacha20poly1305.NonceSizeX // 24
	tagSize    = chacha20poly1305.Overhead   // 16
	padLenSize = 2

	// Overhead is what wrapping adds to a packet before padding.
	Overhead = nonceSize + padLenSize + tagSize

	// SafeWireSize caps a wrapped packet so it still fits in a single
	// datagram on a path with the usual 1500-byte Ethernet MTU, minus
	// room for an IPv6 (40) + UDP (8) header. Padding is trimmed to
	// respect this: a padded packet that fragments would both cost
	// throughput and stand out to the very traffic analysis padding is
	// meant to defeat.
	SafeWireSize = 1452

	// MaxPadding bounds the random padding added to a packet, before the
	// SafeWireSize cap is applied.
	MaxPadding = 255

	// MaxPacketSize is the largest plaintext Wrap accepts. A WireGuard
	// data packet at the default MTU of 1420 is 1452 bytes on the wire,
	// and jumbo-frame setups go higher, so this is deliberately generous
	// rather than tuned to one MTU: undersizing it silently drops full
	// -size packets while small ones keep working, which looks like a
	// broken network rather than a broken tunnel.
	MaxPacketSize = 65535 - Overhead - MaxPadding

	// Junk packets imitate the size distribution of real traffic; sizing
	// them off MaxPacketSize would emit absurd 64KB decoys.
	minJunkSize = 64
	maxJunkSize = 1400
)

var ErrInvalidPacket = errors.New("obfuscator: invalid or forged packet")

// Obfuscator wraps/unwraps packets using one or more pre-shared keys.
// Wrap always uses the first (current) key. Unwrap tries every key in
// order, which is what makes key rotation possible without downtime: the
// operator adds a new current key while keeping the old one as a
// fallback, redeploys both ends, and only drops the old key once every
// peer has picked up the new one.
//
// An Obfuscator is safe for concurrent use by multiple goroutines.
type Obfuscator struct {
	aeads []cipher.AEAD
}

// New builds an Obfuscator from a single 32-byte pre-shared key.
func New(psk [32]byte) (*Obfuscator, error) {
	return NewMulti([][32]byte{psk})
}

// NewMulti builds an Obfuscator from one or more pre-shared keys, ordered
// current-first. At least one key is required.
func NewMulti(keys [][32]byte) (*Obfuscator, error) {
	if len(keys) == 0 {
		return nil, errors.New("obfuscator: at least one key is required")
	}
	aeads := make([]cipher.AEAD, 0, len(keys))
	for _, k := range keys {
		aead, err := chacha20poly1305.NewX(k[:])
		if err != nil {
			return nil, err
		}
		aeads = append(aeads, aead)
	}
	return &Obfuscator{aeads: aeads}, nil
}

// Wrap encrypts and pads plaintext into a wire-ready packet:
// nonce || AEAD(pad_len || plaintext || padding).
func (o *Obfuscator) Wrap(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("obfuscator: refusing to wrap an empty packet")
	}
	if len(plaintext) > MaxPacketSize {
		return nil, errors.New("obfuscator: plaintext exceeds maximum packet size")
	}

	// One read covers both random inputs: the nonce, and the entropy the
	// padding length is drawn from. Each call to the system CSPRNG costs
	// a syscall, and this is the hot path for every packet the tunnel
	// carries.
	var entropy [nonceSize + 4]byte
	if _, err := io.ReadFull(rand.Reader, entropy[:]); err != nil {
		return nil, err
	}

	padLen, err := paddingFor(len(plaintext), binary.BigEndian.Uint32(entropy[nonceSize:]))
	if err != nil {
		return nil, err
	}

	// Lay the packet out in a single buffer and encrypt in place:
	//
	//   [ nonce | pad_len | plaintext | padding ] -> [ nonce | ciphertext | tag ]
	//
	// Sealing with dst ending exactly where the plaintext begins lets the
	// AEAD write over its own input, so a packet costs one allocation
	// and one copy of the payload rather than two of each.
	//
	// The padding bytes are left as zeros deliberately. They sit inside
	// the AEAD-encrypted region, so on the wire they are ciphertext
	// indistinguishable from random either way — filling them with
	// entropy would buy nothing and cost a second CSPRNG read plus a
	// full write over the padding. TLS 1.3 pads its records with zeros
	// for the same reason (RFC 8446 §5.4).
	innerLen := padLenSize + len(plaintext) + padLen
	buf := make([]byte, nonceSize+innerLen+tagSize)

	nonce := buf[:nonceSize]
	copy(nonce, entropy[:nonceSize])

	inner := buf[nonceSize : nonceSize+innerLen]
	binary.BigEndian.PutUint16(inner[:padLenSize], uint16(padLen))
	copy(inner[padLenSize:], plaintext)

	return o.aeads[0].Seal(buf[:nonceSize], nonce, inner, nil), nil
}

// Unwrap authenticates and decrypts a wire packet produced by Wrap, trying
// each configured key in order (current key first). Callers MUST treat
// any error as "drop the packet silently" — it may be a junk packet
// injected deliberately to defeat traffic analysis, not an attack.
func (o *Obfuscator) Unwrap(packet []byte) ([]byte, error) {
	if len(packet) < Overhead+1 {
		return nil, ErrInvalidPacket
	}

	nonce := packet[:nonceSize]
	ciphertext := packet[nonceSize:]

	var inner []byte
	for _, aead := range o.aeads {
		opened, err := aead.Open(nil, nonce, ciphertext, nil)
		if err == nil {
			inner = opened
			break
		}
	}
	if inner == nil {
		return nil, ErrInvalidPacket
	}

	padLen := int(binary.BigEndian.Uint16(inner[:padLenSize]))
	plaintextEnd := len(inner) - padLen
	if plaintextEnd <= padLenSize {
		// Padding claims to cover the whole packet: forged or corrupt.
		return nil, ErrInvalidPacket
	}

	return inner[padLenSize:plaintextEnd], nil
}

// paddingFor picks a random padding length that keeps the wrapped packet
// within SafeWireSize where possible, so padding never turns a full-size
// WireGuard packet into a fragmented one. Packets already at or above the
// cap get no padding — their size is dictated by the tunnelled traffic,
// and fragmenting them would leak more than the padding hides.
//
// The caller supplies the random draw so the CSPRNG is read once per
// packet; unlike the padding bytes, the padding *length* is visible on
// the wire and must stay unpredictable.
func paddingFor(plaintextLen int, draw uint32) (int, error) {
	headroom := SafeWireSize - Overhead - plaintextLen
	if headroom <= 0 {
		return 0, nil
	}
	limit := MaxPadding
	if headroom < limit {
		limit = headroom
	}
	return boundedInt(draw, limit+1)
}

// boundedInt maps a uniform 32-bit draw onto [0, n) without the bias that
// plain modulo reduction introduces. A draw landing in the small biased
// tail is rejected and replaced by a fresh one, which happens for roughly
// one packet in 16 million at these bounds.
func boundedInt(draw uint32, n int) (int, error) {
	if n <= 1 {
		return 0, nil
	}
	limit := uint32(n)
	max := ^uint32(0) - (^uint32(0) % limit)

	for draw >= max {
		var buf [4]byte
		if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
			return 0, err
		}
		draw = binary.BigEndian.Uint32(buf[:])
	}
	return int(draw % limit), nil
}

// randomInt returns a uniform value in [0, n).
func randomInt(n int) (int, error) {
	if n <= 1 {
		return 0, nil
	}
	var buf [4]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return 0, err
	}
	return boundedInt(binary.BigEndian.Uint32(buf[:]), n)
}

// Junk generates a random-length, random-content packet that is
// indistinguishable on the wire from a real Wrap()-ped packet but will
// fail AEAD authentication on the receiving side and be dropped. Used to
// break behavioral fingerprints (e.g. "first packet always looks like a
// WireGuard handshake init").
func Junk() ([]byte, error) {
	extra, err := randomInt(maxJunkSize - minJunkSize + 1)
	if err != nil {
		return nil, err
	}

	junk := make([]byte, minJunkSize+extra)
	if _, err := io.ReadFull(rand.Reader, junk); err != nil {
		return nil, err
	}
	return junk, nil
}
