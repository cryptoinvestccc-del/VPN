package main

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/provision"
)

// awgDevice drives a running AmneziaWG interface inside Amnezia's
// container.
//
// This is the one part of provisioning that cannot be tested here: it
// needs a server with Amnezia on it. Everything worth getting right —
// allocation, idempotency, the peer ceiling, expiry, the rate limit —
// lives above it in internal/provision and is covered there. What is
// left in this file is one command per method, which is exactly the
// point of keeping it this thin.
type awgDevice struct {
	container string
	iface     string
	timeout   time.Duration

	// runner is swapped in tests. In production it is exec.
	runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func newAWGDevice(container, iface string, timeout time.Duration) *awgDevice {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &awgDevice{container: container, iface: iface, timeout: timeout, runner: runCommand}
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// The command line is included because this only ever reaches
		// the operator's log, never a client: see the handler.
		return nil, fmt.Errorf("%s %s: %w: %s",
			name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// exec runs a command inside the Amnezia container.
func (d *awgDevice) exec(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	full := append([]string{"exec", d.container}, args...)
	return d.runner(ctx, "docker", full...)
}

func (d *awgDevice) Peers(ctx context.Context) ([]provision.Peer, error) {
	out, err := d.exec(ctx, "awg", "show", "all", "dump")
	if err != nil {
		return nil, err
	}
	return provision.ParseDump(out)
}

func (d *awgDevice) AddPeer(ctx context.Context, publicKey string, address netip.Prefix) error {
	// The key is validated before it reaches here, and exec passes
	// arguments without a shell, so there is no quoting to get wrong.
	_, err := d.exec(ctx, "awg", "set", d.iface, "peer", publicKey, "allowed-ips", address.String())
	return err
}

func (d *awgDevice) RemovePeer(ctx context.Context, publicKey string) error {
	_, err := d.exec(ctx, "awg", "set", d.iface, "peer", publicKey, "remove")
	return err
}

// ServerPublicKey reads the interface's own public key.
func (d *awgDevice) ServerPublicKey(ctx context.Context) (string, error) {
	out, err := d.exec(ctx, "awg", "show", "all", "dump")
	if err != nil {
		return "", err
	}
	return provision.ServerPublicKey(out)
}

// Config reads the interface's settings, for the obfuscation parameters
// clients must match.
//
// It asks the tool rather than reading a file. Where a particular
// Amnezia install keeps its configuration is not something this code can
// know — the first version guessed a path and guessed wrong — but
// `awg showconf` prints the same settings by name, from whatever the
// interface is actually running. A path is used only when an operator
// names one.
func (d *awgDevice) Config(ctx context.Context, path string) (string, error) {
	if path != "" {
		out, err := d.exec(ctx, "cat", path)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}

	out, err := d.exec(ctx, "awg", "showconf", d.iface)
	if err != nil {
		return "", fmt.Errorf("%w\n\nThe interface is named by -interface; "+
			"`docker exec %s awg show interfaces` lists what is running", err, d.container)
	}
	return string(out), nil
}

// InterfaceName reports the interface the server is actually running.
//
// Guessing "wg0" is right often enough to be a trap: it works until it
// meets an install that named it something else, and then fails with a
// message about a file rather than about a name.
func (d *awgDevice) InterfaceName(ctx context.Context) (string, error) {
	out, err := d.exec(ctx, "awg", "show", "all", "dump")
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(out), "\n")
	name, _, ok := strings.Cut(line, "\t")
	if !ok || name == "" {
		return "", fmt.Errorf("no interface is running in %s", d.container)
	}
	return name, nil
}

// Save asks awg-quick to write the running peer list back to the
// interface's configuration.
//
// Without it `awg set` is a runtime change only, and every credential
// issued disappears the next time the server restarts — silently, and
// for everybody at once. It is a separate step because how a particular
// Amnezia install persists its configuration is not something this code
// can verify from here; see the warning in main.
func (d *awgDevice) Save(ctx context.Context) error {
	_, err := d.exec(ctx, "awg-quick", "save", d.iface)
	return err
}

// findContainer picks Amnezia's AmneziaWG container when the operator
// has not named one.
func findContainer(ctx context.Context, runner func(context.Context, string, ...string) ([]byte, error)) (string, error) {
	out, err := runner(ctx, "docker", "ps", "--format", "{{.Names}}")
	if err != nil {
		return "", err
	}
	names := strings.Fields(string(out))

	// Newer installs use amnezia-awg2; older ones amnezia-awg.
	for _, want := range []string{"amnezia-awg2", "amnezia-awg"} {
		for _, name := range names {
			if name == want {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("no AmneziaWG container is running (looked for amnezia-awg2 and amnezia-awg); "+
		"running containers: %s", strings.Join(names, ", "))
}
