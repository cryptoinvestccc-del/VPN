package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestTheEngineWillNotTakeAName is why the app has to resolve at all.
//
// The engine parses the endpoint with netip.ParseAddrPort, which refuses
// anything that is not already numeric, and it cannot look a name up
// itself: it is a static Go binary, so its resolver reads
// /etc/resolv.conf, and Android has no such file. A phone found this
// the hard way.
func TestTheEngineWillNotTakeAName(t *testing.T) {
	qemu, engine := shippedEngine(t)

	name := fmt.Sprintf("@besy-name-%d-%d", os.Getpid(), time.Now().UnixNano())
	listener, err := net.Listen("unix", name)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	tunFile := openTun(t, "besyname")
	defer tunFile.Close()

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

	sendTunnel(t, listener, tunFile,
		"private_key=0000000000000000000000000000000000000000000000000000000000000000\n"+
			"public_key=1111111111111111111111111111111111111111111111111111111111111111\n"+
			"endpoint=localhost:51820\n")

	said, _ := readAll(out, 15*time.Second)
	if !strings.Contains(said, "ParseAddr") {
		t.Errorf("the engine did not refuse the name as expected:\n%s", said)
	}
	t.Logf("   refused, as it must: %s", firstLine(said))
}

// TestTheAppResolvesTheEndpoint runs the app's own class on the shapes
// it will meet.
func TestTheAppResolvesTheEndpoint(t *testing.T) {
	jvm, ok := desktopClasses()
	if !ok {
		t.Skip("run android/desktop-check/build.sh first")
	}

	cases := []struct{ given, want string }{
		{"127.0.0.1:51820", "127.0.0.1:51820"},
		{"1.1.1.1:51820", "1.1.1.1:51820"},
		{"[2001:db8::1]:51820", "[2001:db8::1]:51820"},
		{"localhost:51820", "127.0.0.1:51820"},
		{"  127.0.0.1:51820  ", "127.0.0.1:51820"},
		{"besyvpn.online", "REFUSED: the server address has no port: besyvpn.online"},
		{"", "REFUSED: the server did not say where to connect"},
	}
	for _, c := range cases {
		t.Run(strings.TrimSpace(c.given), func(t *testing.T) {
			got := appResolve(t, jvm, c.given)
			if got != c.want {
				t.Errorf("the app would tell the engine %q, expected %q", got, c.want)
			}
		})
	}
}

func appResolve(t *testing.T, jvm desktop, endpoint string) string {
	t.Helper()
	cmd := exec.Command("java", "-cp", jvm.cp(), "EndpointCheck", endpoint)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the app could not read %q: %v\n%s", endpoint, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}
