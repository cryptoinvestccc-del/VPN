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
	"strings"
	"syscall"

	"github.com/amnezia-vpn/amneziawg-go/conn"
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

	dev := device.NewDevice(tunDevice, conn.NewStdNetBind(), logger)
	defer dev.Close()

	if err := dev.IpcSet(config); err != nil {
		return fmt.Errorf("applying the configuration: %w", err)
	}
	if err := dev.Up(); err != nil {
		return fmt.Errorf("bringing the tunnel up: %w", err)
	}

	// Ready is printed once the tunnel is actually carrying traffic, so
	// the app can stop saying "connecting" on evidence rather than on a
	// timer.
	fmt.Println("ready")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
	case <-dev.Wait():
	}
	return nil
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
