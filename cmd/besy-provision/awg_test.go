package main

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// recordingRunner captures the command lines instead of running them, so
// the one thing this thin layer can get wrong — building the wrong
// command — is checked without a server.
type recordingRunner struct {
	calls [][]string
	out   map[string]string
	err   error

	// failOn makes only the commands containing this fragment fail, so a
	// test can break one step of a sequence rather than all of them.
	failOn string
}

func (r *recordingRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.err != nil {
		return nil, r.err
	}
	if r.failOn != "" && strings.Contains(strings.Join(args, " "), r.failOn) {
		return nil, errors.New("command failed: " + r.failOn)
	}
	if r.failOn != "" && strings.Contains(strings.Join(args, " "), r.failOn) {
		return nil, errors.New("command failed: " + r.failOn)
	}
	for fragment, out := range r.out {
		if strings.Contains(strings.Join(args, " "), fragment) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func (r *recordingRunner) last() string {
	if len(r.calls) == 0 {
		return ""
	}
	return strings.Join(r.calls[len(r.calls)-1], " ")
}

func testDevice(runner *recordingRunner) *awgDevice {
	d := newAWGDevice("amnezia-awg2", "wg0", time.Second)
	d.runner = runner.run
	return d
}

func TestAddPeerCommand(t *testing.T) {
	r := &recordingRunner{}
	err := testDevice(r).AddPeer(context.Background(), "CLIENTKEY=", netip.MustParsePrefix("10.8.1.5/32"))
	if err != nil {
		t.Fatal(err)
	}

	want := "docker exec amnezia-awg2 awg set wg0 peer CLIENTKEY= allowed-ips 10.8.1.5/32"
	if got := r.last(); got != want {
		t.Errorf("command:\n got %s\nwant %s", got, want)
	}
}

func TestRemovePeerCommand(t *testing.T) {
	r := &recordingRunner{}
	if err := testDevice(r).RemovePeer(context.Background(), "CLIENTKEY="); err != nil {
		t.Fatal(err)
	}

	want := "docker exec amnezia-awg2 awg set wg0 peer CLIENTKEY= remove"
	if got := r.last(); got != want {
		t.Errorf("command:\n got %s\nwant %s", got, want)
	}
}

// TestArgumentsAreNotShellQuoted: keys arrive from the internet. They are
// validated before reaching here, and exec passes arguments straight to
// the process with no shell in between, so there is nothing to escape —
// this test exists to keep it that way if anybody is ever tempted to
// build a command string.
func TestArgumentsAreNotShellQuoted(t *testing.T) {
	r := &recordingRunner{}
	hostile := "key; rm -rf /"
	_ = testDevice(r).AddPeer(context.Background(), hostile, netip.MustParsePrefix("10.8.1.5/32"))

	call := r.calls[len(r.calls)-1]
	for _, arg := range call {
		if arg == hostile {
			return // passed as exactly one argument, which is correct
		}
	}
	t.Errorf("the key was not passed as a single argument: %v", call)
}

func TestPeersParsesTheDump(t *testing.T) {
	dump := strings.Join([]string{"wg0", "PRIV=", "PUB=", "51820", "0"}, "\t") + "\n" +
		strings.Join([]string{"wg0", "PEER=", "(none)", "(none)", "10.8.1.2/32", "0", "0", "0", "off"}, "\t") + "\n"

	peers, err := testDevice(&recordingRunner{out: map[string]string{"show all dump": dump}}).
		Peers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || peers[0].PublicKey != "PEER=" {
		t.Errorf("parsed %+v", peers)
	}
}

func TestFindContainerPrefersTheNewerName(t *testing.T) {
	r := &recordingRunner{out: map[string]string{"ps": "nginx\namnezia-awg\namnezia-awg2\n"}}
	name, err := findContainer(context.Background(), r.run)
	if err != nil {
		t.Fatal(err)
	}
	if name != "amnezia-awg2" {
		t.Errorf("chose %q, expected amnezia-awg2", name)
	}
}

func TestFindContainerFallsBackToTheOlderName(t *testing.T) {
	r := &recordingRunner{out: map[string]string{"ps": "nginx\namnezia-awg\n"}}
	name, err := findContainer(context.Background(), r.run)
	if err != nil {
		t.Fatal(err)
	}
	if name != "amnezia-awg" {
		t.Errorf("chose %q", name)
	}
}

// TestFindContainerSaysWhatItSaw: "not found" on a server the operator
// believes is running Amnezia is a dead end. Listing what is actually
// running turns it into something they can act on.
func TestFindContainerSaysWhatItSaw(t *testing.T) {
	r := &recordingRunner{out: map[string]string{"ps": "nginx\npostgres\n"}}
	_, err := findContainer(context.Background(), r.run)
	if err == nil {
		t.Fatal("a server with no Amnezia container was accepted")
	}
	for _, want := range []string{"amnezia-awg2", "nginx", "postgres"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not mention %q: %v", want, err)
		}
	}
}

func TestDeviceReportsFailures(t *testing.T) {
	r := &recordingRunner{err: errors.New("permission denied")}
	if _, err := testDevice(r).Peers(context.Background()); err == nil {
		t.Error("a failing docker command was swallowed")
	}
}

func TestSaveCommand(t *testing.T) {
	r := &recordingRunner{}
	if err := testDevice(r).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "docker exec amnezia-awg2 awg-quick save wg0"
	if got := r.last(); got != want {
		t.Errorf("command:\n got %s\nwant %s", got, want)
	}
}

// TestPersistWrapperSavesAfterEveryChange: without it, `awg set` is a
// runtime change only and every credential issued disappears on the next
// restart — silently, and for everybody at once.
func TestPersistWrapperSavesAfterEveryChange(t *testing.T) {
	r := &recordingRunner{}
	device := persistAfterWrites(testDevice(r), true)

	if err := device.AddPeer(context.Background(), "K=", netip.MustParsePrefix("10.8.1.2/32")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.last(), "awg-quick save") {
		t.Errorf("adding a peer did not persist: %s", r.last())
	}

	if err := device.RemovePeer(context.Background(), "K="); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.last(), "awg-quick save") {
		t.Errorf("removing a peer did not persist: %s", r.last())
	}
}

func TestPersistOffDoesNotSave(t *testing.T) {
	r := &recordingRunner{}
	device := persistAfterWrites(testDevice(r), false)

	if err := device.AddPeer(context.Background(), "K=", netip.MustParsePrefix("10.8.1.2/32")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.last(), "save") {
		t.Errorf("saved although -persist is off: %s", r.last())
	}
}

func TestRunRequiresAnEndpoint(t *testing.T) {
	if err := run(runOptions{check: true}); err == nil {
		t.Fatal("a configuration with no endpoint was accepted")
	}
	if err := run(runOptions{endpoint: "no-port", check: true}); err == nil {
		t.Fatal("an endpoint without a port was accepted")
	}
}

// TestConfigAsksTheToolNotTheFilesystem: the first version guessed a
// path into Amnezia's container and guessed wrong. Where an install
// keeps its configuration is not knowable from here; what the interface
// is running is.
func TestConfigAsksTheToolNotTheFilesystem(t *testing.T) {
	r := &recordingRunner{out: map[string]string{"showconf": "Jc = 4\n"}}
	if _, err := testDevice(r).Config(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	want := "docker exec amnezia-awg2 awg showconf wg0"
	if got := r.last(); got != want {
		t.Errorf("command:\n got %s\nwant %s", got, want)
	}
}

func TestConfigUsesAPathWhenGivenOne(t *testing.T) {
	r := &recordingRunner{}
	if _, err := testDevice(r).Config(context.Background(), "/etc/awg/wg0.conf"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.last(), "cat /etc/awg/wg0.conf") {
		t.Errorf("an explicit path was ignored: %s", r.last())
	}
}

// TestInterfaceNameComesFromTheServer: "wg0" is right often enough to be
// a trap — it works until it meets an install that named it otherwise.
func TestInterfaceNameComesFromTheServer(t *testing.T) {
	dump := strings.Join([]string{"awg0", "PRIV=", "PUB=", "51820", "0"}, "\t") + "\n"
	name, err := testDevice(&recordingRunner{out: map[string]string{"show all dump": dump}}).
		InterfaceName(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if name != "awg0" {
		t.Errorf("got %q, want awg0", name)
	}
}

// TestSaveFailureKeepsThePeer is the fix for a fault the first real
// deployment produced: awg-quick save wanted a config file that install
// does not have, the error came back from AddPeer, and the client was
// told it got nothing — while the peer sat in the interface holding an
// address. Every attempt left another one behind.
func TestSaveFailureKeepsThePeer(t *testing.T) {
	r := &recordingRunner{failOn: "awg-quick"}
	device := persistAfterWrites(testDevice(r), true)

	if err := device.AddPeer(context.Background(), "K=", netip.MustParsePrefix("10.8.1.2/32")); err != nil {
		t.Fatalf("a failed save lost the peer: %v", err)
	}

	var added bool
	for _, call := range r.calls {
		if strings.Contains(strings.Join(call, " "), "awg set") {
			added = true
		}
	}
	if !added {
		t.Error("the peer was never added")
	}
}
