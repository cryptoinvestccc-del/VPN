package main

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
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

	// saveConf is where the peer list is written so it survives a
	// restart. Empty lets awg-quick choose, which works only when the
	// install keeps its configuration where awg-quick expects it — the
	// first one this met did not.
	saveConf string

	// runner is swapped in tests. In production it is exec.
	runner func(ctx context.Context, name string, args ...string) ([]byte, error)

	// feeder runs a command with data on its standard input, for writing
	// files inside the container. Swapped in tests like runner.
	feeder func(ctx context.Context, stdin []byte, name string, args ...string) error
}

func newAWGDevice(container, iface string, timeout time.Duration) *awgDevice {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &awgDevice{container: container, iface: iface, timeout: timeout,
		runner: runCommand, feeder: feedCommand}
}

func feedCommand(ctx context.Context, stdin []byte, name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s",
			name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// readFile returns a file inside the container, or "" if it does not
// exist.
func (d *awgDevice) readFile(ctx context.Context, path string) (string, error) {
	out, err := d.exec(ctx, "sh", "-c",
		"if [ -e "+shellQuote(path)+" ]; then cat "+shellQuote(path)+"; fi")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// writeFile replaces a file inside the container in one step: written
// beside it, then renamed over it, so a failure halfway leaves the old
// file whole rather than a truncated one that the next restart reads.
func (d *awgDevice) writeFile(ctx context.Context, path string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	tmp := path + ".besy-tmp"
	return d.feeder(ctx, data, "docker", "exec", "-i", d.container, "sh", "-c",
		"cat > "+shellQuote(tmp)+" && mv "+shellQuote(tmp)+" "+shellQuote(path))
}

// liveAddresses are the addresses the interface is running with, as
// `ip` reports them — what awg-quick save writes back as Address.
func (d *awgDevice) liveAddresses(ctx context.Context) ([]string, error) {
	out, err := d.exec(ctx, "ip", "-o", "addr", "show", "dev", d.iface)
	if err != nil {
		return nil, err
	}
	return parseAddrs(string(out)), nil
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

// ListenPort reads the UDP port the running interface listens on.
//
// It comes from the interface's own line in the dump — name, private
// key, public key, port — rather than from anything an operator typed.
// The first installer wrote 51820 into every credential because that is
// WireGuard's customary port; Amnezia chooses its own, and a phone told
// the wrong one connects, shows a VPN icon, and carries nothing.
func (d *awgDevice) ListenPort(ctx context.Context) (int, error) {
	out, err := d.exec(ctx, "awg", "show", "all", "dump")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) < 4 || (d.iface != "" && f[0] != d.iface) {
			continue
		}
		// The interface's own line comes first for each interface.
		port, err := strconv.Atoi(f[3])
		if err != nil || port <= 0 || port > 65535 {
			return 0, fmt.Errorf("the interface reports no usable listen port (%q)", f[3])
		}
		return port, nil
	}
	return 0, fmt.Errorf("no interface %q is running in %s", d.iface, d.container)
}

// PublishedPort asks docker which host port carries the container's UDP
// port. A container on the host's network has no mapping, and then the
// port the interface listens on is the one the world sees; that is the
// false case.
func (d *awgDevice) PublishedPort(ctx context.Context, port int) (int, bool) {
	out, err := d.runner(ctx, "docker", "port", d.container, fmt.Sprintf("%d/udp", port))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		i := strings.LastIndex(line, ":")
		if i < 0 {
			continue
		}
		if p, err := strconv.Atoi(line[i+1:]); err == nil && p > 0 && p <= 65535 {
			return p, true
		}
	}
	return 0, false
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
	if d.saveConf == "" {
		_, err := d.exec(ctx, "awg-quick", "save", d.iface)
		return err
	}
	_, err := d.saveMerged(ctx)
	return err
}

// saveMerged writes the interface's configuration the way awg-quick save
// would, and reports the addresses it wrote.
//
// showconf alone is not the configuration: it has no Address, DNS, MTU
// or Post* lines, because the interface does not hold them. The first
// version wrote showconf by itself and so stripped Address from
// Amnezia's file on every credential issued. Now the addresses come from
// the running interface and the other awg-quick lines from the file
// already there. Before the first write a copy of the original is kept
// beside it, once.
func (d *awgDevice) saveMerged(ctx context.Context) ([]string, error) {
	conf, err := d.exec(ctx, "awg", "showconf", d.iface)
	if err != nil {
		return nil, err
	}
	if len(conf) == 0 {
		return nil, fmt.Errorf("showconf returned nothing for %s", d.iface)
	}
	existing, err := d.readFile(ctx, d.saveConf)
	if err != nil {
		return nil, err
	}
	live, err := d.liveAddresses(ctx)
	if err != nil {
		// Not fatal: the file's own Address lines are used instead.
		live = nil
	}

	merged := mergeConf(existing, string(conf), live)
	if !hasAddress(merged) {
		// Writing this would be what the old code did. Refuse, and keep
		// the peers in memory until someone looks.
		return nil, fmt.Errorf("refusing to write %s: neither the running interface nor the file gives an Address", d.saveConf)
	}

	backup := d.saveConf + ".before-besy"
	if _, err := d.exec(ctx, "sh", "-c",
		"[ -e "+shellQuote(backup)+" ] || [ ! -e "+shellQuote(d.saveConf)+" ] || cp -p "+
			shellQuote(d.saveConf)+" "+shellQuote(backup)); err != nil {
		return nil, fmt.Errorf("keeping a copy of %s: %w", d.saveConf, err)
	}
	if err := d.writeFile(ctx, d.saveConf, []byte(merged)); err != nil {
		return nil, err
	}
	return live, nil
}

// shellQuote wraps a path for the one place a shell is unavoidable:
// redirecting output inside the container.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
