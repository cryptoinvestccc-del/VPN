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

// shippedEngine is the binary that travels inside the APK, or a skip.
func shippedEngine(t *testing.T) (qemu, engine string) {
	t.Helper()
	qemu, err := exec.LookPath("qemu-aarch64-static")
	if err != nil {
		t.Skip("no aarch64 emulation on this machine")
	}
	engine = abs(t, "../../android/app/jni/arm64-v8a/libawg.so")
	if _, err := os.Stat(engine); err != nil {
		t.Skip("run android/build.sh first")
	}
	return qemu, engine
}
