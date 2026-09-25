package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheAppDeletesItsOwnKey runs the app's own code for "Delete key and
// data" against the real provisioning service: a key is issued with a
// token, a wrong token is refused, the right one removes the peer from
// the interface, and the removal cannot be repeated.
func TestTheAppDeletesItsOwnKey(t *testing.T) {
	jvm, ok := desktopClasses()
	if !ok {
		t.Skip("run android/desktop-check/build.sh first")
	}
	dir := t.TempDir()
	_, serverPub := mustKeypair(t)
	peersFile := filepath.Join(dir, "peers")
	if err := os.WriteFile(peersFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "docker")
	build(t, stub, ".", "./stubdocker")
	provision := filepath.Join(dir, "besy-provision")
	build(t, provision, "../..", "./cmd/besy-provision")

	addr := "127.0.0.1:9193"
	svc := exec.Command(provision, "-listen", addr,
		"-endpoint", "127.0.0.1:51820", "-subnet", "10.8.1.0/24",
		"-issued", filepath.Join(dir, "issued.keys"))
	svc.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_CONTAINER=amnezia-awg2", "STUB_IFACE=awg0",
		"STUB_SERVER_PUBLIC="+serverPub, "STUB_PEERS="+peersFile,
		"STUB_SHOWCONF="+realShowconf)
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Process.Kill(); _, _ = svc.Process.Wait() }()
	waitListening(t, addr, 10*time.Second)

	_, clientPub := mustKeypair(t)
	cmd := exec.Command("java", "-cp", jvm.cp(), "AccountCheck", "http://"+addr+"/v1/issue", clientPub)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the app's account code failed: %v\n%s", err, stderr.String())
	}
	got := strings.TrimSpace(string(out))
	t.Logf("\n%s", got)

	want := []string{
		"token present",
		"token-on-repeat absent",
		"connected 0",
		"forget-wrong false",
		"forget-right true",
		"forget-again false",
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			t.Errorf("expected %q", line)
		}
	}
	peers, _ := os.ReadFile(peersFile)
	if strings.Contains(string(peers), clientPub) {
		t.Errorf("the peer is still on the interface after deletion:\n%s", peers)
	}
	issued, _ := os.ReadFile(filepath.Join(dir, "issued.keys"))
	if strings.Contains(string(issued), clientPub) {
		t.Errorf("the key is still on record after deletion")
	}
}
