package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun/netstack"
	"golang.org/x/crypto/curve25519"
)

// The obfuscation parameters a real deployment reported, ranges and all.
//
// Using a live server's actual numbers rather than tidy invented ones is
// the point of this file: the tidy ones passed every test while the real
// ones broke three separate things — a guessed config path, a header
// read as an integer, and a DNS list read as a string.
const realShowconf = `ListenPort = 51820
Jc = 5
Jmin = 10
Jmax = 50
S1 = 141
S2 = 77
S3 = 3
S4 = 19
H1 = 654395697-1564484786
H2 = 1815940314-1819067479
H3 = 2074747675-2076920678
H4 = 2132725960-2145482467
`

// TestWholePathWithoutAPhone runs everything the app does, in order,
// against a real AmneziaWG server — the provisioning binary, the reply
// it produces, the configuration built from it, the handshake, and
// traffic through the tunnel.
//
// What it cannot cover is Android: VpnService, the descriptor handed
// across exec, and the engine running from the app's library directory.
// Everything before those is here, so a broken step is found without
// anybody installing anything.
func TestWholePathWithoutAPhone(t *testing.T) {
	wholePath(t, 51820, "127.0.0.1:51820", "127.0.0.1:51820")
}

// TestTheCredentialPointsAtTheRealPort is the failure a phone met after
// everything else worked: the VPN came up and carried nothing. The
// installer wrote 51820 into -endpoint because that is WireGuard's usual
// port; Amnezia listens wherever it chose. Here the server listens on
// 34567 while -endpoint still says 51820, and the tunnel must work
// anyway, because the service asks the server which port it uses.
func TestTheCredentialPointsAtTheRealPort(t *testing.T) {
	wholePath(t, 34567, "127.0.0.1:51820", "127.0.0.1:34567")
}

// wholePath runs everything the app does, in order: the server listens
// on serverPort, the service is started with -endpoint flagEndpoint, and
// the credential handed out must say wantEndpoint.
func wholePath(t *testing.T, serverPort int, flagEndpoint, wantEndpoint string) {
	t.Helper()
	if testing.Short() {
		t.Skip("brings up a tunnel; skipped in short mode")
	}
	dir := t.TempDir()

	// The server's own key. The stub reports its public half, so what
	// the provisioning service hands out points at the device this test
	// runs.
	serverPriv, serverPub := mustKeypair(t)

	peersFile := filepath.Join(dir, "peers")
	if err := os.WriteFile(peersFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Log("1. building the pieces")
	stub := filepath.Join(dir, "docker")
	build(t, stub, ".", "./stubdocker")
	provision := filepath.Join(dir, "besy-provision")
	build(t, provision, "../..", "./cmd/besy-provision")

	t.Log("2. starting the provisioning service")
	addr := "127.0.0.1:9187"
	cmd := exec.Command(provision,
		"-listen", addr,
		"-endpoint", flagEndpoint,
		"-subnet", "10.8.1.0/24")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("STUB_LISTEN_PORT=%d", serverPort),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_CONTAINER=amnezia-awg2",
		"STUB_IFACE=awg0",
		"STUB_SERVER_PUBLIC="+serverPub,
		"STUB_PEERS="+peersFile,
		"STUB_SHOWCONF="+realShowconf,
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	_ = out
	waitListening(t, addr, 10*time.Second)

	t.Log("3. asking for a credential, as the app does")
	clientPriv, clientPub := mustKeypair(t)
	issuedConfig := ask(t, "http://"+addr+"/v1/issue", clientPub)

	t.Log("4. checking the reply carries what the tunnel needs")
	if err := checkReply(issuedConfig); err != nil {
		t.Fatalf("the reply is unusable: %v", err)
	}
	for key, want := range map[string]string{
		"h1": "654395697-1564484786",
		"jc": "5",
		"s4": "19",
	} {
		if got := issuedConfig.Awg[key]; got != want {
			t.Errorf("%s reached the app as %q, the server has %q", key, got, want)
		}
	}

	if issuedConfig.Endpoint != wantEndpoint {
		t.Fatalf("the credential says %s; the server is on %s", issuedConfig.Endpoint, wantEndpoint)
	}

	t.Log("5. standing up the server the credential points at")
	serverNet, serverDev := startServerOn(t, serverPriv, peersFile, serverPort)
	defer serverDev.Close()
	_ = serverNet

	t.Log("6. building the client configuration, as the app does")
	uapi, err := buildUAPI(clientPriv, issuedConfig)
	if err != nil {
		t.Fatalf("the app could not build a configuration: %v", err)
	}

	t.Log("7. bringing the tunnel up")
	clientNet, clientDev, err := bringUp(issuedConfig, uapi)
	if err != nil {
		t.Fatalf("the tunnel did not come up: %v", err)
	}
	defer clientDev.Close()

	t.Log("8. completing a handshake through the obfuscation")
	if err := waitHandshake(clientDev, 20*time.Second); err != nil {
		t.Fatalf("no handshake: %v", err)
	}

	t.Log("9. carrying traffic")
	if err := talkThrough(t, clientNet, serverNet); err != nil {
		t.Fatalf("the tunnel came up but carried nothing: %v", err)
	}
	t.Log("   the whole path works")
}

// startServer runs an AmneziaWG device holding the private key the
// provisioning service advertised, with the peers it handed out.
func startServerOn(t *testing.T, privateKey, peersFile string, port int) (*netstack.Net, *device.Device) {
	t.Helper()

	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{netip.MustParseAddr("10.8.1.1")},
		[]netip.Addr{netip.MustParseAddr("1.1.1.1")}, 1280)
	if err != nil {
		t.Fatal(err)
	}

	dev := device.NewDevice(tun, conn.NewStdNetBind(), device.NewLogger(device.LogLevelError, "server: "))

	var b strings.Builder
	key, err := hexKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "private_key=%s\nlisten_port=%d\n", key, port)
	// The same obfuscation the stub reports, so the client's copy of it
	// is what is actually being checked.
	for _, line := range strings.Split(strings.TrimSpace(realShowconf), "\n") {
		k, v, ok := strings.Cut(line, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		if !ok || k == "listenport" {
			continue
		}
		fmt.Fprintf(&b, "%s=%s\n", k, strings.TrimSpace(v))
	}

	peers, err := os.ReadFile(peersFile)
	if err != nil {
		t.Fatal(err)
	}
	var added int
	for _, line := range strings.Split(string(peers), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue
		}
		pub, err := hexKey(fields[1])
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "public_key=%s\nallowed_ip=%s\n", pub, fields[4])
		added++
	}
	if added == 0 {
		t.Fatal("the provisioning service added no peer to the interface")
	}

	if err := dev.IpcSet(b.String()); err != nil {
		t.Fatalf("the server refused its own configuration: %v", err)
	}
	if err := dev.Up(); err != nil {
		t.Fatal(err)
	}
	return tnet, dev
}

// talkThrough opens a listener on the server's side of the tunnel and
// reaches it from the client's, which is the only thing that shows the
// tunnel carries data rather than merely completing a handshake.
func talkThrough(t *testing.T, client, server *netstack.Net) error {
	t.Helper()

	ln, err := server.ListenTCP(&net.TCPAddr{Port: 8777})
	if err != nil {
		return fmt.Errorf("listening inside the tunnel: %w", err)
	}
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		buf := make([]byte, 32)
		n, err := c.Read(buf)
		if err != nil {
			done <- err
			return
		}
		_, err = c.Write(buf[:n])
		done <- err
	}()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.DialContext(ctx, "tcp", "10.8.1.1:"+port)
	if err != nil {
		return fmt.Errorf("connecting through the tunnel: %w", err)
	}
	defer c.Close()

	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	const message = "через туннель"
	if _, err := c.Write([]byte(message)); err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		return err
	}
	if string(buf[:n]) != message {
		return fmt.Errorf("what came back is not what went in: %q", string(buf[:n]))
	}
	return <-done
}

func mustKeypair(t *testing.T) (private, public string) {
	t.Helper()
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		t.Fatal(err)
	}
	priv[0] &= 248
	priv[31] = (priv[31] & 127) | 64
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub)
}

// build compiles a package, running from its own module's root: the
// provisioning service lives in the repository's main module and this
// test in its own, so one go build cannot see both.
func build(t *testing.T, out, dir, pkg string) {
	t.Helper()
	abs, err := filepath.Abs(out)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", abs, pkg)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s in %s: %v\n%s", pkg, dir, err, output)
	}
}

func waitListening(t *testing.T, addr string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the provisioning service never listened on %s", addr)
}

func ask(t *testing.T, url, publicKey string) *issued {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"public_key": publicKey})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var cfg issued
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("the reply is not a configuration: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("the service refused the request (%d)", resp.StatusCode)
	}
	return &cfg
}
