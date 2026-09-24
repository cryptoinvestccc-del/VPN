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
//	awg run      run the tunnel on the descriptor in WG_TUN_FD
//
// The private key exists in this process and in the app's own storage,
// and nowhere else. It is generated here, on the phone, and the server
// is only ever told the public half.
package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"golang.org/x/crypto/curve25519"
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
  awg run      run the tunnel; the interface descriptor comes from
               WG_TUN_FD and the configuration from standard input

Neither command takes flags. The configuration format is WireGuard's own
UAPI: one key=value per line, ending with a blank line.
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
// The descriptor comes from VpnService.establish() on the Java side.
// Passing it through the environment rather than reopening anything is
// what makes this work at all: an app cannot create a tun device itself,
// only receive one from the system.
func run() error {
	fdStr := os.Getenv("WG_TUN_FD")
	if fdStr == "" {
		return errors.New("WG_TUN_FD is not set; this command expects the tunnel descriptor from VpnService.establish()")
	}
	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		return fmt.Errorf("WG_TUN_FD %q is not a descriptor number", fdStr)
	}

	mtu := 1280
	if v := os.Getenv("WG_TUN_MTU"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mtu = n
		}
	}

	config, err := readConfig(os.Stdin)
	if err != nil {
		return err
	}

	// The descriptor is owned by this process from here on; closing the
	// device closes it.
	file := os.NewFile(uintptr(fd), "tun")
	tunDevice, err := tun.CreateTUNFromFile(file, mtu)
	if err != nil {
		return fmt.Errorf("taking over the tunnel descriptor: %w", err)
	}

	logLevel := device.LogLevelError
	if os.Getenv("WG_VERBOSE") != "" {
		logLevel = device.LogLevelVerbose
	}
	logger := device.NewLogger(logLevel, "awg: ")

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

// readConfig reads UAPI lines until a blank one.
//
// Reading from standard input rather than a socket keeps the private key
// out of the filesystem entirely: it is written to a pipe by the parent
// and never lands anywhere it could be read later.
func readConfig(r io.Reader) (string, error) {
	var out []byte
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 8192), 1<<20)

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading the configuration: %w", err)
	}
	if len(out) == 0 {
		return "", errors.New("the configuration is empty")
	}
	return string(out), nil
}
