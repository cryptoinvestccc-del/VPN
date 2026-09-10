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
