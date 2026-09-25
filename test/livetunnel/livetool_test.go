package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheLiveToolWorks runs the live check itself — the one meant for the
// production server — against a local copy of that server: port 36635,
// the production obfuscation, and the server named rather than numbered.
//
// The tool is only useful if it can be trusted the first time it meets
// the real thing. It once handed the engine a name, which the engine
// refuses; this is where that would have been caught.
func TestTheLiveToolWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("brings up a tunnel; skipped in short mode")
	}
	const port = 36635
	dir := t.TempDir()

	serverPriv, serverPub := mustKeypair(t)
	peersFile := filepath.Join(dir, "peers")
	if err := os.WriteFile(peersFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	stub := filepath.Join(dir, "docker")
	build(t, stub, ".", "./stubdocker")
	provision := filepath.Join(dir, "besy-provision")
	build(t, provision, "../..", "./cmd/besy-provision")

	addr := "127.0.0.1:9191"
	svc := exec.Command(provision, "-listen", addr,
		// The installer's guess, and a name: both must be dealt with.
		"-endpoint", "localhost:51820",
		"-subnet", "10.8.1.0/24",
		"-issued", filepath.Join(dir, "issued.keys"))
	svc.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		fmt.Sprintf("STUB_LISTEN_PORT=%d", port),
		"STUB_CONTAINER=amnezia-awg2", "STUB_IFACE=awg0",
		"STUB_SERVER_PUBLIC="+serverPub, "STUB_PEERS="+peersFile,
		"STUB_SHOWCONF="+realShowconf)
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Process.Kill(); _, _ = svc.Process.Wait() }()
	waitListening(t, addr, 10*time.Second)

	serverNet, serverDev := startServerWith(t, serverPriv, peersFile, port, true)
	defer serverDev.Close()

	// The peer is added by the service during the run; hand it to the
	// server as soon as it appears, as awg set does on the real one.
	stopPeers := make(chan struct{})
	defer close(stopPeers)
	go func() {
		for {
			select {
			case <-stopPeers:
				return
			case <-time.After(100 * time.Millisecond):
			}
			data, _ := os.ReadFile(peersFile)
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Split(line, "\t")
				if len(f) < 5 {
					continue
				}
				if pub, err := hexKey(f[1]); err == nil {
					_ = serverDev.IpcSet(fmt.Sprintf("public_key=%s\nallowed_ip=%s\n", pub, f[4]))
				}
			}
		}
	}()

	// Something to reach through the tunnel that answers like a web
	// server, standing in for the internet beyond the real one.
	ln, err := serverNet.ListenTCP(&net.TCPAddr{Port: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
			c.Close()
		}
	}()

	if err := run("http://"+addr+"/v1/issue", "10.8.1.1:80"); err != nil {
		t.Fatalf("the live tool failed against a copy of the server: %v", err)
	}
}
