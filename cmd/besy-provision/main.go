// Command besy-provision issues one AmneziaWG credential per install.
//
// It runs on the VPN server, next to Amnezia's container, and answers
// one question: a device has generated a key and wants a configuration.
// It generates no keys itself, receives no private keys, and keeps no
// record of who asked — a request carries a public key and nothing else.
//
// This is what lets an app connect on first launch with one button
// instead of asking somebody to paste a config. It is also what makes
// the alternative — one key shipped inside the app — unnecessary, which
// matters because a shared key does not merely leak: WireGuard keeps one
// endpoint per peer, so two devices holding the same key overwrite each
// other and neither works.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/provision"
)

func main() {
	var (
		listen    = flag.String("listen", "127.0.0.1:9181", "address to serve the issuing API on")
		container = flag.String("container", "", "Amnezia's AmneziaWG container; empty finds amnezia-awg2 or amnezia-awg")
		iface     = flag.String("interface", "", "AmneziaWG interface name; empty asks the server what it is running")
		confPath  = flag.String("config", "", "read the obfuscation parameters from this file instead of asking awg for them")
		endpoint  = flag.String("endpoint", "", "the server's public address clients connect to, host:port (required)")
		subnet    = flag.String("subnet", "10.8.1.0/24", "addresses to hand out; keep it clear of the range Amnezia's own clients use")
		serverIP  = flag.String("server-address", "", "the server's address inside the tunnel; empty uses the first address of -subnet")
		dns       = flag.String("dns", "1.1.1.1", "DNS servers handed to clients, comma separated; empty leaves it out")
		allowed   = flag.String("allowed-ips", "0.0.0.0/0, ::/0", "what clients route into the tunnel")
		mtu       = flag.Int("mtu", 1280, "client MTU")
		keepalive = flag.Int("keepalive", 25, "client PersistentKeepalive, seconds")
		maxPeers  = flag.Int("max-peers", provision.MaxPeers, "how many credentials this server will hold")
		ttl       = flag.Duration("ttl", provision.DefaultTTL, "withdraw a credential after this long without a handshake")
		grace     = flag.Duration("grace", provision.DefaultGrace, "withdraw a never-used credential after this long")
		sweep     = flag.Duration("sweep", provision.DefaultSweep, "how often to look for credentials to withdraw")
		rate      = flag.Float64("rate", provision.DefaultIssueRate, "sustained requests per second from one source address")
		burst     = flag.Float64("burst", provision.DefaultIssueBurst, "requests one source may make at once")
		forwarded = flag.Bool("trust-forwarded-for", false, "honour X-Forwarded-For; only with a proxy in front, never when exposed directly")
		persistTo = flag.String("persist-conf", "", "write the peer list to this path inside the container; empty lets awg-quick choose")
		persist   = flag.Bool("persist", false, "run 'awg-quick save' after each change so credentials survive a restart")
		certFile  = flag.String("cert", "", "TLS certificate; with -key, serves HTTPS and prints the pin clients must carry")
		keyFile   = flag.String("key", "", "TLS private key")
		check     = flag.Bool("check", false, "verify the server is reachable and the configuration is usable, then exit")
	)
	flag.Parse()

	if err := run(runOptions{
		listen: *listen, container: *container, iface: *iface, confPath: *confPath,
		endpoint: *endpoint, subnet: *subnet, serverIP: *serverIP, dns: *dns,
		allowed: *allowed, mtu: *mtu, keepalive: *keepalive, maxPeers: *maxPeers,
		ttl: *ttl, grace: *grace, sweep: *sweep, rate: *rate, burst: *burst,
		forwarded: *forwarded, persist: *persist, check: *check,
		persistTo: *persistTo,
		certFile:  *certFile, keyFile: *keyFile,
	}); err != nil {
		log.Fatalf("besy-provision: %v", err)
	}
}

type runOptions struct {
	listen, container, iface, confPath string
	endpoint, subnet, serverIP         string
	dns, allowed                       string
	mtu, keepalive, maxPeers           int
	ttl, grace, sweep                  time.Duration
	rate, burst                        float64
	forwarded, persist, check          bool
	certFile, keyFile                  string
	persistTo                          string
}

func run(o runOptions) error {
	if o.endpoint == "" {
		return errMissingEndpoint
	}
	if _, _, err := net.SplitHostPort(o.endpoint); err != nil {
		return errBadEndpoint(o.endpoint)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	container := o.container
	if container == "" {
		found, err := findContainer(ctx, runCommand)
		if err != nil {
			return err
		}
		container = found
		log.Printf("besy-provision: using container %s", container)
	}
	device := newAWGDevice(container, o.iface, 10*time.Second)
	device.saveConf = o.persistTo

	if o.iface == "" {
		name, err := device.InterfaceName(ctx)
		if err != nil {
			return err
		}
		device.iface = name
		log.Printf("besy-provision: using interface %s", name)
	}

	serverKey, err := device.ServerPublicKey(ctx)
	if err != nil {
		return err
	}
	endpoint, note, err := resolveEndpoint(ctx, device, o.endpoint)
	if err != nil {
		return err
	}
	if note != "" {
		log.Printf("besy-provision: %s", note)
	}

	conf, err := device.Config(ctx, o.confPath)
	if err != nil {
		return err
	}
	params, err := provision.ParseParams(conf)
	if err != nil {
		return err
	}

	prefix, err := netip.ParsePrefix(o.subnet)
	if err != nil {
		return err
	}
	serverAddr := prefix.Masked().Addr().Next()
	if o.serverIP != "" {
		if serverAddr, err = netip.ParseAddr(o.serverIP); err != nil {
			return err
		}
	}
	pool, err := provision.NewPool(prefix, serverAddr)
	if err != nil {
		return err
	}

	settings := provision.Settings{
		Endpoint:        endpoint,
		ServerPublicKey: serverKey,
		DNS:             parseDNS(o.dns),
		AllowedIPs:      o.allowed,
		MTU:             o.mtu,
		Keepalive:       o.keepalive,
		Params:          params,
	}
	svc, err := provision.NewService(persistAfterWrites(device, o.persist), pool, settings)
	if err != nil {
		return err
	}
	svc.SetMaxPeers(o.maxPeers)

	peers, err := device.Peers(ctx)
	if err != nil {
		return err
	}

	if o.check {
		log.Printf("besy-provision: configuration is usable (container=%s interface=%s peers=%d subnet=%s)",
			container, device.iface, len(peers), prefix)
		return nil
	}

	if !o.persist {
		// Loud, because the failure is total and arrives late: every
		// credential issued works perfectly until the server restarts,
		// and then stops working for everybody at once.
		log.Print("besy-provision: WARNING: -persist is off, so credentials are a runtime change only " +
			"and will be lost the next time the server restarts. Turn it on once you have checked that " +
			"'awg-quick save' is right for this install.")
	}
	log.Printf("besy-provision: serving on %s (container=%s interface=%s peers=%d/%d subnet=%s)",
		o.listen, container, device.iface, len(peers), o.maxPeers, prefix)

	limiter := provision.NewLimiter(o.rate, o.burst)
	limiter.TrustForwardedFor = o.forwarded
	if o.forwarded {
		log.Print("besy-provision: honouring X-Forwarded-For; make sure a proxy sets it, " +
			"or one caller can present a new address per request")
	}

	reaper := provision.NewReaper(device, o.ttl, o.grace)
	go reaper.Run(ctx, o.sweep)
	go sweepLimiter(ctx, limiter, o.sweep)

	server := &http.Server{
		Addr:              o.listen,
		Handler:           provision.Handler(svc, limiter),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	if o.certFile != "" || o.keyFile != "" {
		if o.certFile == "" || o.keyFile == "" {
			return errors.New("-cert and -key are given together or not at all")
		}
		pin, err := certificatePin(o.certFile)
		if err != nil {
			return err
		}
		// Printed on every start, because it is what a client checks the
		// server by, and an operator who cannot find it will be tempted
		// to turn the checking off.
		log.Printf("besy-provision: certificate pin (put this in the app): %s", pin)

		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if err := server.ListenAndServeTLS(o.certFile, o.keyFile); err != nil && err != http.ErrServerClosed {
			return err
		}
		log.Print("besy-provision: shut down")
		return nil
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	log.Print("besy-provision: shut down")
	return nil
}

// certificatePin is the SHA-256 of the certificate a client must see.
//
// This service is reached by an app over the open internet, and it hands
// out the address and key of the server that carries the traffic. A
// stranger able to answer in its place could point every new install at
// a machine of their own — the private key never leaves the phone, so
// nothing is decrypted, but the tunnel would be built to the wrong end.
//
// A certificate authority cannot help here: this server has an address
// and no name, and no authority issues for a bare IP. The client checks
// the certificate itself instead, which is what the rest of this project
// already does.
func certificatePin(certFile string) (string, error) {
	raw, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", fmt.Errorf("%s is not a PEM certificate", certFile)
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func sweepLimiter(ctx context.Context, limiter *provision.Limiter, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			limiter.Sweep()
		}
	}
}

func parseDNS(list string) []netip.Addr {
	var out []netip.Addr
	for _, entry := range strings.Split(list, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			out = append(out, addr)
		} else {
			log.Printf("besy-provision: ignoring DNS entry %q: not an IP address", entry)
		}
	}
	return out
}

// persistAfterWrites wraps the device so every change is written back to
// the interface's configuration, when the operator has asked for it.
func persistAfterWrites(device *awgDevice, on bool) provision.Device {
	if !on {
		return device
	}
	return saving{device}
}

type saving struct{ *awgDevice }

// AddPeer adds the peer, then tries to make it survive a restart.
//
// A failure to save is reported and not returned. The peer is already in
// the interface by then, so returning an error would tell the client it
// got nothing while the server keeps a credential nobody can use — an
// address spent, and a peer left behind on every attempt. The client's
// tunnel works either way; whether it survives a reboot is the
// operator's problem, and the log is where an operator looks.
func (s saving) AddPeer(ctx context.Context, key string, addr netip.Prefix) error {
	if err := s.awgDevice.AddPeer(ctx, key, addr); err != nil {
		return err
	}
	s.trySave(ctx, "after adding a peer")
	return nil
}

func (s saving) RemovePeer(ctx context.Context, key string) error {
	if err := s.awgDevice.RemovePeer(ctx, key); err != nil {
		return err
	}
	s.trySave(ctx, "after withdrawing a peer")
	return nil
}

// saveWarning fires once. A line per issued credential would bury the
// rest of the log, and the second occurrence says nothing the first did
// not.
var saveWarning sync.Once

func (s saving) trySave(ctx context.Context, when string) {
	if err := s.awgDevice.Save(ctx); err != nil {
		saveWarning.Do(func() {
			log.Printf("besy-provision: could not save the peer list %s: %v", when, err)
			log.Print("besy-provision: credentials work now but will be lost when the server restarts. " +
				"Point -persist-conf at the file this install actually uses, or run without -persist " +
				"and accept that clients re-provision after a reboot.")
		})
	}
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const errMissingEndpoint = simpleError("-endpoint is required: it is the address clients connect to, " +
	"and a configuration without one is unusable")

func errBadEndpoint(addr string) error {
	return simpleError("-endpoint " + addr + " is not host:port")
}

// resolveEndpoint decides the address clients are told to connect to:
// the host from -endpoint, and the port the server is actually reachable
// on.
//
// The port used to come from the flag alone, and the installer filled
// the flag with 51820 on the strength of that being WireGuard's usual
// port. Amnezia picks its own. Every credential then pointed at a port
// nothing listened on, and the symptom on a phone was a VPN that came up
// and carried no traffic at all — the least informative failure there
// is. So the server is asked, and the flag's port is kept only when the
// server cannot say.
func resolveEndpoint(ctx context.Context, d interface {
	ListenPort(context.Context) (int, error)
	PublishedPort(context.Context, int) (int, bool)
}, flagEndpoint string) (string, string, error) {
	host, flagPort, err := net.SplitHostPort(flagEndpoint)
	if err != nil {
		return "", "", errBadEndpoint(flagEndpoint)
	}

	listen, err := d.ListenPort(ctx)
	if err != nil {
		return flagEndpoint, "could not read the listen port (" + err.Error() +
			"); telling clients " + flagEndpoint + " as given", nil
	}
	actual := listen
	if published, ok := d.PublishedPort(ctx, listen); ok {
		actual = published
	}

	port := strconv.Itoa(actual)
	endpoint := net.JoinHostPort(host, port)
	if port == flagPort {
		return endpoint, "clients connect to " + endpoint, nil
	}
	return endpoint, "the server listens on port " + port + ", not " + flagPort +
		" as -endpoint says; telling clients " + endpoint, nil
}
