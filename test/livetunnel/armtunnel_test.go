package main

import (
	"bufio"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun/netstack"
	"golang.org/x/sys/unix"
)

// TestShippedEngineCarriesTraffic runs the exact ARM binary that travels
// inside the APK — not a rebuild for this machine — against a real
// AmneziaWG server, over a real kernel tun device, with the descriptor
// handed across exec the way the app hands it across.
//
// It is the closest this machine gets to the phone. What is still left
// over is Android's own half: VpnService.establish() producing the
// descriptor, and Android permitting execution from the library
// directory. The engine itself, the configuration the app builds, the
// obfuscation, the handshake and the traffic are all covered here.
func TestShippedEngineCarriesTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("brings up a tunnel; skipped in short mode")
	}
	qemu, err := exec.LookPath("qemu-aarch64-static")
	if err != nil {
		t.Skip("no aarch64 emulation on this machine")
	}
	engine := abs(t, "../../android/app/jni/arm64-v8a/libawg.so")
	if _, err := os.Stat(engine); err != nil {
		t.Skipf("the engine has not been built: %v", err)
	}
	if os.Geteuid() != 0 {
		t.Skip("creating a tun device needs root")
	}

	const (
		iface      = "besyarm"
		clientCIDR = "10.8.1.77/32"
		serverIP   = "10.8.1.1"
	)

	t.Log("1. the shipped binary generates its own keypair")
	clientPriv, clientPub := armKeypair(t, qemu, engine)
	serverPriv, serverPub := armKeypair(t, qemu, engine)

	t.Log("2. standing up the server with the real obfuscation")
	serverNet, serverDev := serverWithPeer(t, serverPriv, clientPub, clientCIDR)
	defer serverDev.Close()

	t.Log("3. opening a kernel tun device, as VpnService would")
	tunFile := openTun(t, iface)
	defer tunFile.Close()
	ipCmd(t, "addr", "add", clientCIDR, "dev", iface)
	ipCmd(t, "link", "set", iface, "up")
	ipCmd(t, "route", "add", serverIP+"/32", "dev", iface)

	t.Log("4. building the configuration, as the app builds it")
	uapi, err := buildUAPI(clientPriv, &issued{
		Address:         clientCIDR,
		DNS:             []string{"1.1.1.1"},
		MTU:             1280,
		ServerPublicKey: serverPub,
		Endpoint:        "127.0.0.1:51820",
		AllowedIPs:      "0.0.0.0/0",
		Keepalive:       25,
		Awg:             awgFromShowconf(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// When the app's own class can be run here, use what it produces
	// rather than this harness's copy of the same logic, so the text
	// going into the engine is the app's text and not a stand-in.
	if fromApp, ok := appUAPI(t, clientPriv, &issued{
		Address:         clientCIDR,
		DNS:             []string{"1.1.1.1"},
		MTU:             1280,
		ServerPublicKey: serverPub,
		Endpoint:        "127.0.0.1:51820",
		AllowedIPs:      "0.0.0.0/0",
		Keepalive:       25,
		Awg:             awgFromShowconf(),
	}); ok {
		t.Log("   using the configuration the app itself built")
		uapi = fromApp
	}

	t.Log("5. sending the descriptor to the ARM engine, as the app sends it")
	// Not as a number in the environment. That is what the app tried
	// first, and on a phone it fails: ProcessBuilder closes every
	// descriptor above the standard three in the child, so the number
	// names nothing by the time the engine reads it. This harness used
	// to pass it as a number too, which is precisely why it did not
	// catch that — so it now does what the app does.
	name := fmt.Sprintf("@besy-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	listener, err := net.Listen("unix", name)
	if err != nil {
		t.Fatalf("listening on %s: %v", name, err)
	}
	defer listener.Close()

	cmd := exec.Command(qemu, engine, "run")
	cmd.Env = append(os.Environ(), "WG_TUN_SOCKET="+name)
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

	accepted, err := listener.Accept()
	if err != nil {
		t.Fatalf("the engine never called back: %v", err)
	}
	defer accepted.Close()
	peer, ok := accepted.(*net.UnixConn)
	if !ok {
		t.Fatal("not a unix connection")
	}
	// The descriptor is attached to the first message, which is also
	// the configuration — the same single write the app performs.
	rights := syscall.UnixRights(int(tunFile.Fd()))
	if _, _, err := peer.WriteMsgUnix([]byte(uapi), rights, nil); err != nil {
		t.Fatalf("sending the descriptor: %v", err)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			t.Logf("   engine: %s", sc.Text())
			select {
			case lines <- sc.Text():
			default:
			}
		}
		close(lines)
	}()

	t.Log("6. waiting for the engine to say it is carrying traffic")
	deadline := time.After(30 * time.Second)
	ready := false
	for !ready {
		select {
		case line, open := <-lines:
			if !open {
				t.Fatal("the engine stopped before it was ready")
			}
			if strings.TrimSpace(line) == "ready" {
				ready = true
			}
		case <-deadline:
			t.Fatal("the engine never reported ready")
		}
	}

	t.Log("7. carrying traffic through the kernel, into the ARM engine, out the other side")
	ln, err := serverNet.ListenTCP(&net.TCPAddr{Port: 8777})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		_, _ = c.Write(buf[:n])
	}()

	const message = "через APK"
	var last error
	for attempt := 0; attempt < 20; attempt++ {
		conn, err := net.DialTimeout("tcp", serverIP+":8777", 3*time.Second)
		if err != nil {
			last = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte(message)); err != nil {
			conn.Close()
			last = err
			continue
		}
		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		conn.Close()
		if err != nil {
			last = err
			continue
		}
		if got := string(buf[:n]); got != message {
			t.Fatalf("what came back is not what went in: %q", got)
		}
		t.Log("   the binary from the APK carried real traffic")
		return
	}
	t.Fatalf("nothing went through the tunnel: %v", last)
}

// armKeypair asks the shipped binary itself for a keypair, so the keys
// the test uses are made by the code the phone would run.
func armKeypair(t *testing.T, qemu, engine string) (private, public string) {
	t.Helper()
	out, err := exec.Command(qemu, engine, "genkey").Output()
	if err != nil {
		t.Fatalf("the engine could not generate a key: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("genkey printed %q, expected two lines", out)
	}
	return fields[0], fields[1]
}

// openTun creates the kind of descriptor VpnService.establish() returns.
func openTun(t *testing.T, name string) *os.File {
	t.Helper()
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		t.Skipf("no tun device available here: %v", err)
	}
	var req struct {
		Name  [16]byte
		Flags uint16
		_     [22]byte
	}
	copy(req.Name[:], name)
	req.Flags = unix.IFF_TUN | unix.IFF_NO_PI
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd),
		uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&req))); errno != 0 {
		unix.Close(fd)
		t.Skipf("could not create %s: %v", name, errno)
	}
	return os.NewFile(uintptr(fd), "/dev/net/tun")
}

func ipCmd(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		t.Fatalf("ip %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func abs(t *testing.T, p string) string {
	t.Helper()
	a, err := exec.Command("readlink", "-f", p).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(a))
}

// awgFromShowconf turns the server's own report into the map the
// provisioning service puts in its reply.
func awgFromShowconf() map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(realShowconf), "\n") {
		k, v, ok := strings.Cut(line, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		if !ok || k == "listenport" {
			continue
		}
		m[k] = strings.TrimSpace(v)
	}
	return m
}

// serverWithPeer is startServer without the file the stub would have
// written: this test is about the engine, not about provisioning.
func serverWithPeer(t *testing.T, privateKey, peerPublic, allowed string) (*netstack.Net, *device.Device) {
	t.Helper()
	tunDev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{netip.MustParseAddr("10.8.1.1")},
		[]netip.Addr{netip.MustParseAddr("1.1.1.1")}, 1280)
	if err != nil {
		t.Fatal(err)
	}
	dev := device.NewDevice(tunDev, conn.NewStdNetBind(), device.NewLogger(device.LogLevelError, "server: "))

	var b strings.Builder
	key, err := hexKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "private_key=%s\nlisten_port=51820\n", key)
	for k, v := range awgFromShowconf() {
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	pub, err := hexKey(peerPublic)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "public_key=%s\nallowed_ip=%s\n", pub, allowed)

	if err := dev.IpcSet(b.String()); err != nil {
		t.Fatalf("configuring the server: %v", err)
	}
	if err := dev.Up(); err != nil {
		t.Fatalf("bringing the server up: %v", err)
	}
	return tnet, dev
}
