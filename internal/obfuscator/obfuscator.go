// Package obfuscator wraps arbitrary UDP payloads (WireGuard packets) in an
// AEAD-encrypted envelope with random padding, so the wire format carries no
// recognizable WireGuard signature. It does not replace WireGuard's own
// encryption — it hides the shape of the traffic that carries it.
package obfuscator

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	nonceSize  = chacha20poly1305.NonceSize // 12
	tagSize    = chacha20poly1305.Overhead  // 16
	padLenSize = 1

	// MaxPadding bounds the random padding added to each packet.
	MaxPadding = 255

	// MinPacketSize/MaxPacketSize bound plausible UDP payload sizes we
	// operate on (WireGuard packets are always within this range).
	MinPacketSize = 32
	MaxPacketSize = 1400
)

var ErrInvalidPacket = errors.New("obfuscator: invalid or forged packet")

// Obfuscator wraps/unwraps packets using one or more pre-shared keys.
// Wrap always uses the first (current) key. Unwrap tries every key in
// order, which is what makes key rotation possible without downtime: the
// operator adds a new current key while keeping the old one as a
// fallback, redeploys both ends, and only drops the old key once every
// peer has picked up the new one.
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
		aead, err := chacha20poly1305.New(k[:])
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
	if len(plaintext) == 0 || len(plaintext) > MaxPacketSize {
		return nil, errors.New("obfuscator: plaintext size out of range")
	}

	padLen, err := randomPadLen()
	if err != nil {
		return nil, err
	}

	inner := make([]byte, padLenSize+len(plaintext)+int(padLen))
	inner[0] = padLen
	copy(inner[padLenSize:], plaintext)
	if _, err := io.ReadFull(rand.Reader, inner[padLenSize+len(plaintext):]); err != nil {
		return nil, err
	}

	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	out := make([]byte, 0, nonceSize+len(inner)+tagSize)
	out = append(out, nonce...)
	out = o.aeads[0].Seal(out, nonce, inner, nil)
	return out, nil
}

// Unwrap authenticates and decrypts a wire packet produced by Wrap, trying
// each configured key in order (current key first). Callers MUST treat
// any error as "drop the packet silently" — it may be a junk packet
// injected deliberately to defeat traffic analysis, not an attack.
func (o *Obfuscator) Unwrap(packet []byte) ([]byte, error) {
	if len(packet) < nonceSize+padLenSize+tagSize {
		return nil, ErrInvalidPacket
	}

	nonce := packet[:nonceSize]
	ciphertext := packet[nonceSize:]

	var inner []byte
	for _, aead := range o.aeads {
		var err error
		inner, err = aead.Open(nil, nonce, ciphertext, nil)
		if err == nil {
			break
		}
	}
	if inner == nil {
		return nil, ErrInvalidPacket
	}

	padLen := int(inner[0])
	plaintextEnd := len(inner) - padLen
	if plaintextEnd < padLenSize+1 {
		return nil, ErrInvalidPacket
	}

	return inner[padLenSize:plaintextEnd], nil
}

func randomPadLen() (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

// Junk generates a random-length, random-content packet that is
// indistinguishable on the wire from a real Wrap()-ped packet but will
// fail AEAD authentication on the receiving side and be dropped. Used to
// break behavioral fingerprints (e.g. "first packet always looks like a
// WireGuard handshake init").
func Junk() ([]byte, error) {
	sizeRange := MaxPacketSize - MinPacketSize
	sizeByte := make([]byte, 2)
	if _, err := io.ReadFull(rand.Reader, sizeByte); err != nil {
		return nil, err
	}
	size := MinPacketSize + int(uint16(sizeByte[0])<<8|uint16(sizeByte[1]))%sizeRange

	junk := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, junk); err != nil {
		return nil, err
	}
	return junk, nil
}
