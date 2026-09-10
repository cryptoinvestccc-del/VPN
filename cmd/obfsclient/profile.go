package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cryptoinvestccc-del/vpn/internal/config"
	"github.com/cryptoinvestccc-del/vpn/internal/profile"
)

// loadProfile accepts either the link itself or the path to a file
// holding one.
//
// People paste these out of chat messages and also save them as files,
// and asking which of the two they have is a question with no useful
// answer: the two are trivially distinguishable, so the tool
// distinguishes them.
func loadProfile(arg string) (profile.Profile, error) {
	text := strings.TrimSpace(arg)

	if !strings.HasPrefix(text, profile.Scheme+"://") {
		raw, err := os.ReadFile(text)
		if err != nil {
			return profile.Profile{}, fmt.Errorf("reading the profile: %w", err)
		}
		text = strings.TrimSpace(string(raw))
	}
	return profile.Decode(text)
}

// configFromProfile maps a profile onto the settings the transport
// already takes, so a profile is a way of writing a config rather than a
// second code path through the client.
func configFromProfile(p profile.Profile) config.File {
	return config.File{
		Mode:             p.Transport.Mode,
		PSKBase64:        p.Transport.PSKBase64,
		LocalAddr:        p.Transport.LocalAddr,
		RemoteWireAddr:   remoteFor(p, "udp"),
		RemoteTLSAddr:    remoteFor(p, "tls"),
		ServerName:       p.Transport.ServerName,
		PinnedCertSHA256: p.Transport.PinnedCertSHA256,
		JunkPackets:      p.Transport.JunkPackets,
	}
}

func remoteFor(p profile.Profile, mode string) string {
	if p.Transport.Mode == mode {
		return p.Transport.Endpoint
	}
	return ""
}

// writeWireGuardConfig saves the tunnel half of the profile where
// wg-quick can find it. 0600 because it contains this device's private
// key, and wg-quick refuses a world-readable config anyway.
func writeWireGuardConfig(p profile.Profile, path string) error {
	if err := os.WriteFile(path, []byte(p.WireGuardConfig()), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "obfsclient: wrote %s\n", path)
	fmt.Fprintf(os.Stderr, "obfsclient: bring the tunnel up with: wg-quick up %s\n", path)
	return nil
}
