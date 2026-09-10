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
	// PSKBase64 is a 32-byte pre-shared key, base64-encoded. Generate one
	// with: openssl rand -base64 32
	PSKBase64 string `yaml:"psk"`

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
	RemoteTLSAddr    string `yaml:"remote_tls_addr,omitempty"`
	ServerName       string `yaml:"server_name,omitempty"`
	PinnedCertSHA256 string `yaml:"pinned_cert_sha256,omitempty"`
}

// PSK decodes the base64 PSK into a fixed-size key.
func (f File) PSK() ([32]byte, error) {
	var key [32]byte
	raw, err := base64.StdEncoding.DecodeString(f.PSKBase64)
	if err != nil {
		return key, err
	}
	if len(raw) != 32 {
		return key, errors.New("config: psk must decode to exactly 32 bytes")
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
