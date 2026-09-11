// Package profile carries everything one device needs to connect, in a
// single artifact.
//
// Before this existed, giving somebody access meant handing them three
// things — a WireGuard config, an obfuscator config, and a certificate
// pin — and trusting them to put the pieces in the right places. Each
// hand-off is a chance to paste the wrong value, and the failure mode of
// a wrong pin or a wrong MTU is not an error message but a tunnel that
// almost works.
//
// A profile is therefore one file, or one line of text that survives
// being pasted into a chat window.
//
// # This is a bearer credential
//
// A profile contains the pre-shared key and the device's WireGuard
// private key. Whoever reads it has the access it grants — there is no
// second factor and no way to tell a copy from the original. Treat it
// like a password: hand it over on a channel you would trust with one,
// and revoke the client (obfsctl revoke) if it goes astray. Everything
// here is written on that assumption: files are created 0600, and String
// on a Profile does not print secrets.
package profile

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

// Scheme prefixes the single-line form. The version sits in the URI as
// well as inside the payload so a reader can reject a future format
// without having to decompress it first.
const (
	Scheme  = "obfsvpn"
	Version = 1

	uriPrefix = Scheme + "://v1/"

	// maxEncodedSize bounds what decoding will accept. A real profile is
	// a few hundred bytes; anything far larger is either a mistake or an
	// attempt to make the decompressor allocate.
	maxEncodedSize = 64 << 10

	// maxDecodedSize bounds the decompressed payload, which is the size
	// that actually matters: base64 of a small deflate stream can expand
	// enormously.
	maxDecodedSize = 256 << 10
)

// Profile is what a device needs to connect.
type Profile struct {
	Version int `json:"v"`

	// Name is what a person sees in a list of connections. It has no
	// effect on anything.
	Name string `json:"name,omitempty"`

	// ClientID matches the credential on the server, so a person can tell
	// which entry to revoke without opening the profile.
	ClientID string `json:"client_id,omitempty"`

	Transport Transport `json:"transport"`
	WireGuard WireGuard `json:"wireguard"`
}

// Transport describes how to reach the obfuscator.
type Transport struct {
	// Mode is "udp" or "tls".
	Mode string `json:"mode"`

	// Endpoint is the server's public address, host:port.
	Endpoint string `json:"endpoint"`

	// EndpointIP is the server's address as a literal IP.
	//
	// It exists for one purpose: to keep the device's route to the server
	// outside the tunnel. With AllowedIPs 0.0.0.0/0 the obfuscator's own
	// connection to the server would otherwise be routed into the tunnel
	// it is carrying, which is a loop — the tunnel would never come up at
	// all. WireGuard avoids this for its own socket with a firewall mark;
	// a separate process needs a host route instead, and a host route
	// needs an address rather than a name.
	//
	// Resolved when the profile is built, because at the moment the route
	// is added the tunnel is already up and a DNS query would be sent
	// into it.
	EndpointIP string `json:"endpoint_ip,omitempty"`

	// LocalAddr is where the obfuscator listens for the WireGuard
	// interface on this device. It must be a loopback address: it accepts
	// plaintext WireGuard with nothing in front of it, so binding it to a
	// reachable interface would hand anyone on the same network an
	// unauthenticated way into the tunnel.
	LocalAddr string `json:"local_addr"`

	// PSKBase64 authorizes this device. In both modes: it is what
	// separates a client from a stranger.
	PSKBase64 string `json:"psk"`

	// ServerName is the SNI to present in TLS mode. Empty means the
	// endpoint's host is used.
	ServerName string `json:"sni,omitempty"`

	// PinnedCertSHA256 is the certificate fingerprint the client must
	// see, hex-encoded. Required in TLS mode: this design pins rather
	// than trusting a CA, so an omitted pin is not a lax setting but a
	// missing one.
	PinnedCertSHA256 string `json:"pin,omitempty"`

	// JunkPackets sends a few random-length packets when a UDP session
	// opens, so the first thing on the wire is not always the same size.
	JunkPackets int `json:"junk,omitempty"`
}

// WireGuard is the tunnel that runs inside the obfuscated transport.
type WireGuard struct {
	PrivateKey string `json:"private_key"`
	Address    string `json:"address"`
	DNS        string `json:"dns,omitempty"`

	PeerPublicKey string `json:"peer_public_key"`
	AllowedIPs    string `json:"allowed_ips,omitempty"`

	// MTU must leave room for the obfuscator's overhead. Validate
	// enforces this: a too-large MTU produces a tunnel where small
	// packets work and large ones vanish, which is among the harder
	// faults to diagnose from the inside.
	MTU int `json:"mtu"`

	// PersistentKeepalive keeps a NAT mapping open, in seconds.
	PersistentKeepalive int `json:"keepalive,omitempty"`
}

// MaxTunnelMTU is the largest interface MTU that still fits a full-size
// obfuscated packet into one datagram on a normal path.
//
// It is derived rather than written down, because every term in it can
// change: SafeWireSize is the datagram budget after IPv6 and UDP headers,
// Overhead is what wrapping adds, and wireGuardDataOverhead is
// WireGuard's own framing. A constant would silently stop being true the
// first time any of those moved.
const wireGuardDataOverhead = 32

var MaxTunnelMTU = (obfuscator.SafeWireSize - obfuscator.Overhead - wireGuardDataOverhead) / 16 * 16

var (
	ErrNotAProfile        = errors.New("not a profile")
	ErrUnsupportedVersion = errors.New("unsupported profile version")
)

// Encode renders a profile as a single line that survives a chat window.
func Encode(p Profile) (string, error) {
	p.Version = Version
	if err := Validate(p); err != nil {
		return "", err
	}

	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	var compressed bytes.Buffer
	w, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(raw); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return uriPrefix + base64.RawURLEncoding.EncodeToString(compressed.Bytes()), nil
}

// Decode parses the single-line form. Surrounding whitespace is tolerated
// because these arrive pasted, and a stray newline is not the user's
// mistake to pay for.
func Decode(s string) (Profile, error) {
	s = strings.TrimSpace(s)

	if !strings.HasPrefix(s, uriPrefix) {
		if i := strings.Index(s, "://"); i > 0 && s[:i] == Scheme {
			return Profile{}, fmt.Errorf("%w: this profile was made by a newer version", ErrUnsupportedVersion)
		}
		return Profile{}, fmt.Errorf("%w: expected a link starting with %s", ErrNotAProfile, uriPrefix)
	}
	payload := s[len(uriPrefix):]

	if len(payload) > maxEncodedSize {
		return Profile{}, fmt.Errorf("%w: %d bytes is far larger than any real profile", ErrNotAProfile, len(payload))
	}

	compressed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: the link is damaged (%v)", ErrNotAProfile, err)
	}

	// LimitReader, not a size check afterwards: a decompression bomb has
	// to be refused while it expands, not once it has.
	r := flate.NewReader(bytes.NewReader(compressed))
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, maxDecodedSize+1))
	if err != nil {
		return Profile{}, fmt.Errorf("%w: the link is damaged (%v)", ErrNotAProfile, err)
	}
	if len(raw) > maxDecodedSize {
		return Profile{}, fmt.Errorf("%w: the payload expands to more than %d bytes", ErrNotAProfile, maxDecodedSize)
	}

	var p Profile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("%w: %v", ErrNotAProfile, err)
	}
	if p.Version != Version {
		return Profile{}, fmt.Errorf("%w: this profile is version %d, this build understands %d",
			ErrUnsupportedVersion, p.Version, Version)
	}
	if err := Validate(p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// String describes a profile without printing anything secret, so it is
// safe in a log or on a terminal somebody is sharing.
func (p Profile) String() string {
	name := p.Name
	if name == "" {
		name = "(unnamed)"
	}
	return fmt.Sprintf("%s [%s] %s via %s, address %s, MTU %d",
		name, p.ClientID, p.Transport.Endpoint, p.Transport.Mode, p.WireGuard.Address, p.WireGuard.MTU)
}
