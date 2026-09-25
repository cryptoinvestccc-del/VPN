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
	keys map[string]bool
}

var _ provision.Registry = (*containerRegistry)(nil)

func loadRegistry(ctx context.Context, dev *awgDevice, path string) (*containerRegistry, error) {
	text, err := dev.readFile(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading the list of issued credentials at %s: %w", path, err)
	}
	r := &containerRegistry{dev: dev, path: path, keys: map[string]bool{}}
	for _, line := range strings.Split(text, "\n") {
		key := strings.TrimSpace(line)
		// Anything that is not a key is ignored rather than trusted:
		// this list decides what may be deleted.
		if provision.ValidatePublicKey(key) == nil {
			r.keys[key] = true
		}
	}
	return r, nil
}

func (r *containerRegistry) Owns(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.keys[key]
}

func (r *containerRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.keys)
}

func (r *containerRegistry) Add(ctx context.Context, key string) error {
	if err := provision.ValidatePublicKey(key); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys[key] {
		return nil
	}
	r.keys[key] = true
	if err := r.writeLocked(ctx); err != nil {
		delete(r.keys, key)
		return err
	}
	return nil
}

func (r *containerRegistry) Remove(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.keys[key] {
		return nil
	}
	delete(r.keys, key)
	if err := r.writeLocked(ctx); err != nil {
		r.keys[key] = true
		return err
	}
	return nil
}

func (r *containerRegistry) writeLocked(ctx context.Context) error {
	keys := make([]string, 0, len(r.keys))
	for k := range r.keys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	body := strings.Join(keys, "\n")
	if body != "" {
		body += "\n"
	}
	return r.dev.writeFile(ctx, r.path, []byte(body))
}
