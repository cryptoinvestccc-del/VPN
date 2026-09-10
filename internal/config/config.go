// Package config loads obfuscator settings from YAML and decodes the PSK.
package config

import (
	"encoding/base64"
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// File is the on-disk YAML config shape.
type File struct {
	// PSKBase64 is the current 32-byte pre-shared key, base64-encoded.
	// Generate one with: openssl rand -base64 32
	PSKBase64 string `yaml:"psk"`

	// PSKPreviousBase64 is an optional previous key, kept alongside psk
	// during a rotation window. Unwrap accepts packets under either key,
	// so client/server can be redeployed with a new psk one at a time
	// without a connectivity gap; drop this field once every peer has
	// picked up the new key.
	PSKPreviousBase64 string `yaml:"psk_previous,omitempty"`

	LocalAddr      string `yaml:"local_addr"`
	RemoteWireAddr string `yaml:"remote_wire_addr,omitempty"`
	ListenWireAddr string `yaml:"listen_wire_addr,omitempty"`
	JunkPackets    int    `yaml:"junk_packets,omitempty"`

	// Mode selects the wire transport: "udp" (default, plain obfuscated
	// UDP) or "tls" (obfuscated frames carried inside a real TLS
	// connection — stronger against active DPI probing).
	Mode string `yaml:"mode,omitempty"`

	// TLS mode fields.
	ListenTLSAddr    string `yaml:"listen_tls_addr,omitempty"`
	CertFile         string `yaml:"cert_file,omitempty"`
	KeyFile          string `yaml:"key_file,omitempty"`
	FallbackAddr     string `yaml:"fallback_addr,omitempty"`
	RemoteTLSAddr    string `yaml:"remote_tls_addr,omitempty"`
	ServerName       string `yaml:"server_name,omitempty"`
	PinnedCertSHA256 string `yaml:"pinned_cert_sha256,omitempty"`
}

// PSK decodes the base64 PSK into a fixed-size key.
func (f File) PSK() ([32]byte, error) {
	return decodeKey(f.PSKBase64, "psk")
}

// PSKs returns the configured keys, current key first, followed by
// psk_previous when set. Pass this to obfuscator.NewMulti so Unwrap
// accepts either key during a rotation window.
//
// An empty psk field returns (nil, nil) rather than an error, so callers
// can produce a message naming the mode they are in. Both modes require a
// key: it is what authorizes a peer, in TLS mode just as much as in UDP
// mode.
func (f File) PSKs() ([][32]byte, error) {
	if f.PSKBase64 == "" {
		return nil, nil
	}
	current, err := decodeKey(f.PSKBase64, "psk")
	if err != nil {
		return nil, err
	}
	keys := [][32]byte{current}
	if f.PSKPreviousBase64 != "" {
		previous, err := decodeKey(f.PSKPreviousBase64, "psk_previous")
		if err != nil {
			return nil, err
		}
		keys = append(keys, previous)
	}
	return keys, nil
}

func decodeKey(b64, fieldName string) ([32]byte, error) {
	var key [32]byte
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return key, err
	}
	if len(raw) != 32 {
		return key, errors.New("config: " + fieldName + " must decode to exactly 32 bytes")
	}
	copy(key[:], raw)
	return key, nil
}

// Load reads and parses a YAML config file.
func Load(path string) (File, error) {
	var f File
	data, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return f, err
	}
	return f, nil
}
