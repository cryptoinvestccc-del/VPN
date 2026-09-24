// Command livetunnel checks the whole path against a running deployment:
// it asks the provisioning service for a credential, builds the tunnel
// configuration exactly as the app does, brings an AmneziaWG tunnel up
// against the real server, and carries real traffic through it.
//
// It exists because the only other way to find out whether any of this
// works was to hand somebody an APK and ask them to install it. Every
// step below was, at some point today, the one that was broken.
//
// What it does not cover is Android itself: VpnService, the descriptor
// handed across exec, and the engine running from the app's library
// directory. Everything before those is checked here.
//
//	go run . -endpoint https://besyvpn.online/v1/issue
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"

	"strconv"
	"strings"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun/netstack"
	"golang.org/x/crypto/curve25519"
)

type issued struct {
	Address         string            `json:"address"`
	DNS             []string          `json:"dns"`
	MTU             int               `json:"mtu"`
	ServerPublicKey string            `json:"server_public_key"`
	Endpoint        string            `json:"endpoint"`
	AllowedIPs      string            `json:"allowed_ips"`
	Keepalive       int               `json:"keepalive"`
	Awg             map[string]string `json:"awg"`
}

func main() {
	endpoint := flag.String("endpoint", "", "the provisioning URL, e.g. https://besyvpn.online/v1/issue")
	target := flag.String("target", "1.1.1.1:80", "what to reach through the tunnel, to prove it carries traffic")
	flag.Parse()

	if *endpoint == "" {
		fmt.Fprintln(os.Stderr, "-endpoint is required")
		os.Exit(2)
	}

	if err := run(*endpoint, *target); err != nil {
		fmt.Printf("\nFAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nAll steps passed.")
}

func step(n int, what string) { fmt.Printf("%d. %-42s", n, what) }
func ok(detail string)        { fmt.Printf("ok    %s\n", detail) }

func run(endpoint, target string) error {
	// 1. A key, made here the way the app makes one on the phone.
	step(1, "generate a keypair")
	private, public, err := keypair()
	if err != nil {
		return err
	}
	ok("public " + public[:16] + "…")

	// 2. Ask the real service.
	step(2, "ask the provisioning service")
	cfg, err := provision(endpoint, public)
	if err != nil {
		return err
	}
	ok(cfg.Address + " via " + cfg.Endpoint)

	// 3. The reply has to carry everything the tunnel needs.
	step(3, "check the reply is complete")
	if err := checkReply(cfg); err != nil {
		return err
	}
	ok(fmt.Sprintf("mtu %d, %d obfuscation parameters", cfg.MTU, len(cfg.Awg)))

	// 4. Build the configuration the app would build.
	step(4, "build the tunnel configuration")
	uapi, err := buildUAPI(private, cfg)
	if err != nil {
		return err
	}
	ok(fmt.Sprintf("%d lines", strings.Count(uapi, "\n")))

	// 5. Bring it up against the real server.
	step(5, "bring the tunnel up")
	tnet, dev, err := bringUp(cfg, uapi)
	if err != nil {
		return err
	}
	defer dev.Close()
	ok("interface created")

	// 6. A handshake is the first thing that can fail for a reason the
	//    configuration alone cannot show: wrong keys, wrong obfuscation,
	//    a server that never answers.
	step(6, "complete a handshake")
	if err := waitHandshake(dev, 25*time.Second); err != nil {
		return err
	}
	ok("server answered")

	// 7. And traffic, which is the only thing that proves the rest.
	step(7, "carry traffic through it")
	if err := reach(tnet, target); err != nil {
		return err
	}
	ok("reached " + target)

	return nil
}

func keypair() (private, public string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", err
	}
	priv[0] &= 248
	priv[31] = (priv[31] & 127) | 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	enc := base64.StdEncoding
	return enc.EncodeToString(priv[:]), enc.EncodeToString(pub), nil
}

func provision(endpoint, publicKey string) (*issued, error) {
	body, _ := json.Marshal(map[string]string{"public_key": publicKey})

	// No proxy: this has to reach the deployment the way a phone does.
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:           nil,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Post(endpoint, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("reaching the service: %w", err)
	}
	defer resp.Body.Close()

	var cfg issued
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("the reply is not a configuration: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the service refused the request (%d)", resp.StatusCode)
	}
	return &cfg, nil
}

func checkReply(c *issued) error {
	missing := []string{}
	if c.Address == "" {
		missing = append(missing, "address")
	}
	if c.ServerPublicKey == "" {
		missing = append(missing, "server_public_key")
	}
	if c.Endpoint == "" {
		missing = append(missing, "endpoint")
	}
	if c.MTU <= 0 {
		missing = append(missing, "mtu")
	}
	if len(missing) > 0 {
		return fmt.Errorf("the reply is missing %s", strings.Join(missing, ", "))
	}
	// Without these the handshake is never answered and nothing says why.
	for _, k := range []string{"h1", "h2", "h3", "h4"} {
		if c.Awg[k] == "" {
			return fmt.Errorf("the reply carries no %s; the handshake would never be answered", k)
		}
	}
	return nil
}

// buildUAPI mirrors Uapi.java line for line. The two are written in
// different languages against the same contract, which is exactly the
// kind of pair that drifts — it already did once, over dns.
func buildUAPI(privateKey string, c *issued) (string, error) {
	var b strings.Builder

	key, err := hexKey(privateKey)
	if err != nil {
		return "", fmt.Errorf("private key: %w", err)
	}
	fmt.Fprintf(&b, "private_key=%s\n", key)
	b.WriteString("listen_port=0\n")
	b.WriteString("replace_peers=true\n")

	// Positive-only, in the order the engine documents them.
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4"} {
		if v := c.Awg[k]; v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				fmt.Fprintf(&b, "%s=%s\n", k, v)
			}
		}
	}
	// Headers go through as written: they may be ranges. All four or
	// none — they replace WireGuard's four message types, so a partial
	// set makes a packet the server cannot classify.
	headers := []string{"h1", "h2", "h3", "h4"}
	complete := true
	for _, k := range headers {
		if c.Awg[k] == "" {
			complete = false
		}
	}
	if complete {
		for _, k := range headers {
			fmt.Fprintf(&b, "%s=%s\n", k, c.Awg[k])
		}
	}
	// The packet templates, in their own order rather than sorted, so
	// this matches the app line for line.
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if v := strings.TrimSpace(c.Awg[k]); v != "" {
			fmt.Fprintf(&b, "%s=%s\n", k, v)
		}
	}

	peer, err := hexKey(c.ServerPublicKey)
	if err != nil {
		return "", fmt.Errorf("server public key: %w", err)
	}
	fmt.Fprintf(&b, "public_key=%s\n", peer)
	fmt.Fprintf(&b, "endpoint=%s\n", c.Endpoint)

	allowed := c.AllowedIPs
	if allowed == "" {
		allowed = "0.0.0.0/0, ::/0"
	}
	for _, entry := range strings.Split(allowed, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			fmt.Fprintf(&b, "allowed_ip=%s\n", entry)
		}
	}
	if c.Keepalive > 0 {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", c.Keepalive)
	}
	return b.String(), nil
}

func hexKey(base64Key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return "", fmt.Errorf("not base64")
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("decodes to %d bytes, a key is 32", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func bringUp(c *issued, uapi string) (*netstack.Net, *device.Device, error) {
	addr, err := netip.ParsePrefix(c.Address)
	if err != nil {
		return nil, nil, fmt.Errorf("address %q: %w", c.Address, err)
	}

	dns := make([]netip.Addr, 0, len(c.DNS))
	for _, d := range c.DNS {
		if a, err := netip.ParseAddr(d); err == nil {
			dns = append(dns, a)
		}
	}
	if len(dns) == 0 {
		dns = append(dns, netip.MustParseAddr("1.1.1.1"))
	}

	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{addr.Addr()}, dns, c.MTU)
	if err != nil {
		return nil, nil, fmt.Errorf("creating the interface: %w", err)
	}

	level := device.LogLevelError
	if os.Getenv("VERBOSE") != "" {
		level = device.LogLevelVerbose
	}
	dev := device.NewDevice(tun, conn.NewStdNetBind(), device.NewLogger(level, "tunnel: "))

	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, nil, fmt.Errorf("the engine refused the configuration: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, nil, err
	}
	return tnet, dev, nil
}

func waitHandshake(dev *device.Device, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		state, err := dev.IpcGet()
		if err != nil {
			return err
		}
		for _, line := range strings.Split(state, "\n") {
			if strings.HasPrefix(line, "last_handshake_time_sec=") {
				if v := strings.TrimPrefix(line, "last_handshake_time_sec="); v != "0" && v != "" {
					return nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("the server never answered the handshake within %s — "+
		"the obfuscation parameters or the keys do not match", within)
}

func reach(tnet *netstack.Net, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := tnet.DialContext(ctx, "tcp", target)
	if err != nil {
		return fmt.Errorf("nothing reachable through the tunnel: %w", err)
	}
	defer c.Close()

	// A request and an answer, so this cannot pass on a connection that
	// merely opened.
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	host, _, _ := net.SplitHostPort(target)
	if _, err := fmt.Fprintf(c, "HEAD / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil || n == 0 {
		return fmt.Errorf("the tunnel opened a connection but carried nothing: %v", err)
	}
	if !strings.HasPrefix(string(buf[:n]), "HTTP/") {
		return fmt.Errorf("the answer through the tunnel is not HTTP: %q", string(buf[:n]))
	}
	return nil
}
