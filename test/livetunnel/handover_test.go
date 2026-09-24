package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestTheDescriptorCannotBeSentByNumber guards the mistake that cost a
// phone installation: the descriptor was passed as a number in the
// environment, and Java closes every descriptor above the standard three
// in a child process, so the number named nothing.
//
// The engine must refuse that arrangement outright rather than start and
// fail later on an unrelated-looking error about the TUN device.
func TestTheDescriptorCannotBeSentByNumber(t *testing.T) {
	qemu, engine := shippedEngine(t)

	t.Run("a number in the environment is not accepted", func(t *testing.T) {
		cmd := exec.Command(qemu, engine, "run")
		cmd.Env = append(os.Environ(), "WG_TUN_FD=3")
		cmd.Stdin = strings.NewReader("private_key=00\n\n")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatal("the engine started without being sent a descriptor")
		}
		if !strings.Contains(string(out), "WG_TUN_SOCKET") {
			t.Errorf("the engine did not say what it wanted:\n%s", out)
		}
	})

	t.Run("a configuration with no descriptor attached is refused", func(t *testing.T) {
		name := fmt.Sprintf("@besy-guard-%d-%d", os.Getpid(), time.Now().UnixNano())
		listener, err := net.Listen("unix", name)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()

		cmd := exec.Command(qemu, engine, "run")
		cmd.Env = append(os.Environ(), "WG_TUN_SOCKET="+name)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}()

		peer, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		// The configuration, and nothing attached to it.
		if _, err := peer.Write([]byte("private_key=00\n")); err != nil {
			t.Fatal(err)
		}
		_ = peer.(*net.UnixConn).CloseWrite()

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("the engine carried on without a descriptor")
			}
		case <-time.After(15 * time.Second):
			t.Fatal("the engine neither started nor refused")
		}
	})
}

// shippedEngine is the binary that travels inside the APK, built now.
//
// It is built rather than found, and built to the exact path the APK is
// packaged from, so that the binary under test and the binary that
// ships are one file. Reading a file somebody else built is how a test
// comes to pass against code that no longer exists: it happened twice
// on this project, once to the engine in the APK and once to this test.
func shippedEngine(t *testing.T) (qemu, engine string) {
	t.Helper()
	qemu, err := exec.LookPath("qemu-aarch64-static")
	if err != nil {
		t.Skip("no aarch64 emulation on this machine")
	}
	engine = abs(t, "../../android/app/jni/arm64-v8a/libawg.so")
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", engine, ".")
	cmd.Dir = abs(t, "../../android/awg")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the engine: %v\n%s", err, out)
	}
	return qemu, engine
}

// TestTheEngineRefusesAStranger checks that the engine will not take a
// tunnel from a socket owned by somebody else.
//
// The socket the app and the engine talk over lives in the abstract
// namespace, which on Android has no owner and no permissions: any app
// may listen on a name, and the name is not a secret. What travels on
// it is the tunnel descriptor and the private key. So the engine asks
// the kernel who the peer is, and this test makes the peer somebody
// else and watches it refuse.
func TestTheEngineRefusesAStranger(t *testing.T) {
	qemu, engine := shippedEngine(t)
	if os.Geteuid() != 0 {
		t.Skip("running a process as another user needs root")
	}
	const nobody = 65534

	// Somewhere the other user can actually reach: a test's own
	// directory is private to the user that made it.
	dir, err := os.MkdirTemp("", "besy-stranger")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "foreignlistener")
	build(t, helper, ".", "./foreignlistener")
	if err := os.Chmod(helper, 0o755); err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("@besy-stranger-%d-%d", os.Getpid(), time.Now().UnixNano())
	squatter := exec.Command(helper, name)
	squatter.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: nobody, Gid: nobody},
	}
	ready, err := squatter.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	squatter.Stderr = os.Stderr
	if err := squatter.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = squatter.Process.Kill()
		_, _ = squatter.Process.Wait()
	}()

	buf := make([]byte, 16)
	if _, err := ready.Read(buf); err != nil {
		t.Fatalf("the stranger never started listening: %v", err)
	}

	cmd := exec.Command(qemu, engine, "run")
	cmd.Env = append(os.Environ(), "WG_TUN_SOCKET="+name)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the engine took a tunnel from uid %d:\n%s", nobody, out)
	}
	if !strings.Contains(string(out), "refusing to hand over the tunnel") {
		t.Errorf("the engine failed, but not because of who it was talking to:\n%s", out)
	}
	t.Logf("   refused: %s", strings.TrimSpace(string(out)))
}
