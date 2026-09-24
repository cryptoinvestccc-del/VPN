// Package provision issues one AmneziaWG credential per install, so an
// app can connect on first launch without anybody pasting a key.
//
// The thing it exists to prevent is a shared key. A key baked into an
// APK is extractable in a minute, but that is the lesser problem: a
// WireGuard peer is identified by its public key and the server keeps
// one endpoint per peer, so two devices holding the same key overwrite
// each other's endpoint on every handshake and neither one works. A
// shared key does not degrade at scale — it fails at two users.
//
// So the device makes its own private key, sends only the public half,
// and the server adds a peer for it. The private key never leaves the
// phone, and access can be withdrawn from one device without touching
// the rest.
//
// # What this package will not do
//
// It never generates, receives, logs or stores a client private key. A
// request carries a public key and nothing else identifying; the reply
// carries what the device needs to connect. There is deliberately no
// record of who asked.
package provision

import (
	"bufio"
	"bytes"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Peer is one client as the running interface knows it.
type Peer struct {
	PublicKey string

	// Addresses is what the server routes to this peer. For a client
	// this is a single /32 or /128.
	Addresses []netip.Prefix

	// LastHandshake is zero when the peer has never completed one, which
	// is the normal state of a credential that was issued and never
	// used.
	LastHandshake time.Time
}

// Used reports whether the peer has ever completed a handshake.
func (p Peer) Used() bool { return !p.LastHandshake.IsZero() }

// ParseDump reads the peers out of `awg show all dump`.
//
// Only the peer lines are read, and only positionally, because the peer
// format is the one part of this output that has not moved: it is
// inherited unchanged from WireGuard, and is, after the interface name,
// public key, preshared key, endpoint, allowed IPs, latest handshake in
// unix seconds, bytes received, bytes sent, keepalive.
//
// The interface line is deliberately not parsed. AmneziaWG appends its
// obfuscation parameters there, and how many it appends has changed
// between versions — s3, s4 and i1..i5 arrived later, and fwmark sits at
// the end, so its index moves with the version. Reading those by
// position would work on the server it was written against and quietly
// return the wrong numbers on another. They are read from the interface
// configuration by name instead; see params.go.
//
// Interface and peer lines are told apart the way internal/serverstat
// does it on the BESY branch: the first line bearing an interface name
// is that interface, and every line after it is one of its peers. That
// holds however many columns a version adds.
func ParseDump(out []byte) ([]Peer, error) {
	var peers []Peer
	seenInterface := map[string]bool{}

	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)

	for line := 1; sc.Scan(); line++ {
		text := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(text) == "" {
			continue
		}

		fields := strings.Split(text, "\t")
		if len(fields) < 2 {
			return nil, fmt.Errorf("line %d: %q is not tab-separated dump output", line, text)
		}

		iface := fields[0]
		if !seenInterface[iface] {
			seenInterface[iface] = true
			continue // the interface's own line
		}

		peer, err := parsePeerLine(fields)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		peers = append(peers, peer)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return peers, nil
}

// peerFieldCount is the peer line's width in `show all dump`: the
// interface name plus the eight fields WireGuard has always printed.
const peerFieldCount = 9

func parsePeerLine(fields []string) (Peer, error) {
	if len(fields) < peerFieldCount {
		return Peer{}, fmt.Errorf("a peer line has %d fields, expected at least %d", len(fields), peerFieldCount)
	}

	peer := Peer{PublicKey: fields[1]}
	if peer.PublicKey == "" {
		return Peer{}, fmt.Errorf("a peer line carries no public key")
	}

	// "(none)" is what the tools print for an empty list.
	if allowed := fields[4]; allowed != "" && allowed != "(none)" {
		for _, entry := range strings.Split(allowed, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return Peer{}, fmt.Errorf("allowed IPs entry %q: %v", entry, err)
			}
			peer.Addresses = append(peer.Addresses, prefix)
		}
	}

	seconds, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil {
		return Peer{}, fmt.Errorf("latest handshake %q is not a number", fields[5])
	}
	if seconds > 0 {
		peer.LastHandshake = time.Unix(seconds, 0)
	}
	return peer, nil
}

// ServerPublicKey reads the interface's own public key out of the dump.
//
// Only the leading fields of an interface line are read — name, private
// key, public key, port — and those have not moved: AmneziaWG appends
// its parameters, so everything that shifted between versions is behind
// them. The private key is skipped over and never returned, logged, or
// kept.
//
// Reading it here rather than asking an operator to paste it removes a
// transcription step, and a mistyped server key produces a handshake
// that is never answered, with nothing to say why.
func ServerPublicKey(out []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			return "", fmt.Errorf("the first interface line has %d fields, expected at least 4", len(fields))
		}
		key := fields[2]
		if key == "" || key == "(none)" {
			return "", fmt.Errorf("the interface reports no public key")
		}
		return key, nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("the dump is empty; is the interface running?")
}
