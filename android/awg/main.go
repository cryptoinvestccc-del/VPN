// Command awg is the tunnel engine that ships inside the APK.
//
// It is a separate process rather than a library called through JNI,
// because building a JNI library needs the Android NDK and the NDK is
// only distributed from a host this project cannot reach. Go compiles a
// static linux/arm64 binary without any of that, Android is Linux, and
// a binary placed in the APK's lib/ directory lands somewhere Android
// still allows execution — unlike the app's data directory, where it has
// been forbidden since API 29.
//
// Two jobs, chosen so the Java side never touches a private key or a
// cryptographic primitive:
//
//	awg genkey   print a fresh keypair, one line each
//	awg run      run the tunnel on the descriptor sent to it
//
// The private key exists in this process and in the app's own storage,
// and nowhere else. It is generated here, on the phone, and the server
// is only ever told the public half.
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "genkey":
		err = genkey(os.Stdout)
	case "run":
		err = run()
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "awg: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `awg is the tunnel engine for the BESY app.

  awg genkey   print a new private key and its public half
  awg run      run the tunnel; the interface descriptor and the
               configuration both arrive over the socket named in
               WG_TUN_SOCKET

Neither command takes flags. The configuration format is WireGuard's own
UAPI: one key=value per line.
`)
}

// genkey writes a fresh keypair.
//
// Two lines, private first, because whatever reads this should have to
// take the private key deliberately rather than by parsing loosely.
func genkey(w io.Writer) error {
	var private [32]byte
	if _, err := io.ReadFull(randSource(), private[:]); err != nil {
		return fmt.Errorf("reading randomness: %w", err)
	}

	// The clamping WireGuard expects of an X25519 private key.
	private[0] &= 248
	private[31] = (private[31] & 127) | 64

	public, err := curve25519.X25519(private[:], curve25519.Basepoint)
	if err != nil {
		return fmt.Errorf("deriving the public key: %w", err)
	}

	enc := base64.StdEncoding
	if _, err := fmt.Fprintf(w, "%s\n%s\n", enc.EncodeToString(private[:]), enc.EncodeToString(public)); err != nil {
		return err
	}
	return nil
}

// run brings the tunnel up on a descriptor the app already opened.
//
// The descriptor comes from VpnService.establish() on the Java side and
// has to cross a process boundary to get here. It cannot cross it by
// number: Java's ProcessBuilder closes every descriptor above the
// standard three in the child, so a number passed through the
// environment names nothing by the time this process reads it. That is
// measurable, and it was how this failed on a real phone — the engine
// asked the kernel about descriptor 30-something and was told, quite
// correctly, that there was no such thing.
//
// So the descriptor is sent rather than named: Android's LocalSocket
// attaches it to a message as SCM_RIGHTS, the kernel installs a copy in
// this process, and its number here is whatever the kernel chose. The
// configuration travels on the same socket, which also keeps the
// private key out of the filesystem and out of the process table.
func run() error {
	addr := os.Getenv("WG_TUN_SOCKET")
	if addr == "" {
		return errors.New("WG_TUN_SOCKET is not set; this command expects the tunnel descriptor to be sent to it")
	}

	fd, config, err := receive(addr)
	if err != nil {
		return err
	}
	if config == "" {
		return errors.New("the configuration is empty")
	}

	// Unmonitored on purpose. The other constructor opens a netlink
	// socket, looks the interface index up and sets the MTU, all of
	// which need privileges an app does not have. On Android the system
	// has already done that work in VpnService.Builder.
	tunDevice, name, err := tun.CreateUnmonitoredTUNFromFD(fd)
	if err != nil {
		unix.Close(fd)
		return fmt.Errorf("taking over the tunnel descriptor: %w", err)
	}

	logLevel := device.LogLevelError
	if os.Getenv("WG_VERBOSE") != "" {
		logLevel = device.LogLevelVerbose
	}
	logger := device.NewLogger(logLevel, "awg: ")
	logger.Verbosef("attached to %s", name)

	dev := device.NewDevice(tunDevice, newPhoneBind(), logger)
	defer dev.Close()

	if err := dev.IpcSet(config); err != nil {
		return fmt.Errorf("applying the configuration: %w", err)
	}
	if err := dev.Up(); err != nil {
		return fmt.Errorf("bringing the tunnel up: %w", err)
	}

	// Ready means the server answered, not that this process started.
	// It used to be printed straight after Up, and the app then said
	// "connected" over a tunnel nobody was on the other end of: a phone
	// showed the VPN icon and carried nothing. Now it waits for a
	// completed handshake, and says plainly when there is none.
	if err := awaitHandshake(dev, handshakeWait); err != nil {
		return err
	}
	fmt.Println("ready")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
	case <-dev.Wait():
	}
	return nil
}

// handshakeWait covers several of the handshake's own retries, which
// come every five seconds, so a slow first packet on a mobile network is
// not mistaken for a server that is not there.
const handshakeWait = 25 * time.Second

// awaitHandshake returns once the server has answered, or explains what
// was seen if it never did.
//
// The explanation is the point. "Sent 1480 bytes, received 0" means the
// packets left the phone and nothing came back — a wrong port, a server
// that does not know this key, or obfuscation that does not match —
// which is a different problem from not being able to send at all.
func awaitHandshake(dev *device.Device, within time.Duration) error {
	deadline := time.Now().Add(within)
	for {
		state, err := dev.IpcGet()
		if err != nil {
			return fmt.Errorf("reading the tunnel's state: %w", err)
		}
		st := parseState(state)
		if st.handshake > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the server at %s did not answer in %s: sent %d bytes, received %d. "+
				"Packets are leaving this phone and nothing is coming back — "+
				"check the server's port, that it knows this key, and that its obfuscation matches",
				st.endpoint, within, st.tx, st.rx)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

type tunnelState struct {
	endpoint  string
	handshake int64
	tx, rx    int64
}

// parseState reads the few fields of the UAPI dump this needs.
func parseState(dump string) tunnelState {
	var st tunnelState
	for _, line := range strings.Split(dump, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		switch k {
		case "endpoint":
			st.endpoint = v
		case "last_handshake_time_sec":
			st.handshake = n
		case "tx_bytes":
			st.tx += n
		case "rx_bytes":
			st.rx += n
		}
	}
	return st
}

// receive collects the tunnel descriptor and the configuration from the
// socket the app is listening on.
//
// The descriptor rides along with the first bytes of the configuration,
// because that is how SCM_RIGHTS works: it is attached to a message, not
// sent on its own. The rest of the configuration follows until the app
// closes its end.
func receive(addr string) (int, string, error) {
	c, err := net.Dial("unix", addr)
	if err != nil {
		return -1, "", fmt.Errorf("reaching the app on %s: %w", addr, err)
	}
	defer c.Close()
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return -1, "", fmt.Errorf("%s is not a unix socket", addr)
	}

	// Whoever is on the other end must be the app itself. A socket in
	// the abstract namespace has no owner and no permissions: anything
	// on the phone may listen on a name, and a tunnel descriptor handed
	// to the wrong listener is somebody else's tunnel. The kernel knows
	// who it is, so ask.
	if err := sameUser(uc); err != nil {
		return -1, "", err
	}

	buf := make([]byte, 4096)
	oob := make([]byte, syscall.CmsgSpace(4))
	n, oobn, _, _, err := uc.ReadMsgUnix(buf, oob)
	if err != nil {
		return -1, "", fmt.Errorf("reading the first message from the app: %w", err)
	}

	messages, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return -1, "", fmt.Errorf("reading what the app attached: %w", err)
	}
	var fd = -1
	for _, m := range messages {
		fds, err := syscall.ParseUnixRights(&m)
		if err != nil {
			continue
		}
		for i, got := range fds {
			if i == 0 && fd < 0 {
				fd = got
				continue
			}
			unix.Close(got) // more than one was sent; keep the first
		}
	}
	if fd < 0 {
		return -1, "", errors.New("the app sent a configuration but no tunnel descriptor")
	}

	rest, err := io.ReadAll(uc)
	if err != nil {
		unix.Close(fd)
		return -1, "", fmt.Errorf("reading the configuration: %w", err)
	}
	return fd, normalise(string(buf[:n]) + string(rest)), nil
}

// sameUser refuses a peer that is not the user this process runs as.
//
// SO_PEERCRED is filled in by the kernel when the connection is made and
// cannot be set by either side, so it says who the peer is rather than
// who it claims to be.
func sameUser(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return fmt.Errorf("inspecting the connection: %w", err)
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("inspecting the connection: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("asking who is on the other end: %w", credErr)
	}
	if mine := uint32(os.Getuid()); cred.Uid != mine {
		return fmt.Errorf("the socket belongs to user %d, not to this app (%d); refusing to hand over the tunnel", cred.Uid, mine)
	}
	return nil
}

// normalise drops anything after a blank line, so a configuration
// written with a trailing terminator reads the same as one without.
func normalise(config string) string {
	var out []byte
	for _, line := range strings.Split(config, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return string(out)
}
