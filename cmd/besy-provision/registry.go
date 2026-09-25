package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cryptoinvestccc-del/vpn/internal/provision"
)

// containerRegistry is the list of public keys this service issued,
// kept in a file inside the Amnezia container.
//
// Inside the container, next to awg0.conf, rather than on the host,
// for two reasons. The peers it describes live there and go wherever
// that configuration goes. And the service's unit runs with
// ProtectSystem=strict, so it cannot write to the host's disk at all;
// it already writes inside the container, through docker.
//
// Peers issued before this list existed are not on it, and so are
// never withdrawn automatically. That is deliberate: the service cannot
// tell its own old peers from Amnezia's, and guessing wrong would delete
// somebody's working VPN.
type containerRegistry struct {
	mu   sync.Mutex
	dev  *awgDevice
	path string
	// keys maps each issued public key to the hash of its forget
	// token; empty for a key recorded without one.
	keys map[string]string
}

var _ provision.Registry = (*containerRegistry)(nil)

func loadRegistry(ctx context.Context, dev *awgDevice, path string) (*containerRegistry, error) {
	text, err := dev.readFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading the list of issued credentials at %s: %w", path, err)
	}
	r := &containerRegistry{dev: dev, path: path, keys: map[string]string{}}
	for _, line := range strings.Split(text, "\n") {
		// "key hash", or a bare key from before tokens existed.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		key, hash := fields[0], ""
		if len(fields) > 1 && isHexHash(fields[1]) {
			hash = fields[1]
		}
		// Anything that is not a key is ignored rather than trusted:
		// this list decides what may be deleted.
		if provision.ValidatePublicKey(key) == nil {
			r.keys[key] = hash
		}
	}
	return r, nil
}

func (r *containerRegistry) Owns(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.keys[key]
	return ok
}

func (r *containerRegistry) SecretHash(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.keys[key]
	return h, ok
}

func isHexHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (r *containerRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.keys)
}

func (r *containerRegistry) Add(ctx context.Context, key, hash string) error {
	if err := provision.ValidatePublicKey(key); err != nil {
		return err
	}
	if hash != "" && !isHexHash(hash) {
		return fmt.Errorf("not a token hash")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old, had := r.keys[key]
	if had && old == hash {
		return nil
	}
	r.keys[key] = hash
	if err := r.writeLocked(ctx); err != nil {
		if had {
			r.keys[key] = old
		} else {
			delete(r.keys, key)
		}
		return err
	}
	return nil
}

func (r *containerRegistry) Remove(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, had := r.keys[key]
	if !had {
		return nil
	}
	delete(r.keys, key)
	if err := r.writeLocked(ctx); err != nil {
		r.keys[key] = old
		return err
	}
	return nil
}

func (r *containerRegistry) writeLocked(ctx context.Context) error {
	lines := make([]string, 0, len(r.keys))
	for k, h := range r.keys {
		if h != "" {
			lines = append(lines, k+" "+h)
		} else {
			lines = append(lines, k)
		}
	}
	sort.Strings(lines)
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return r.dev.writeFile(ctx, r.path, []byte(body))
}
