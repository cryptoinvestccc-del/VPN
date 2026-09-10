package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
	"github.com/cryptoinvestccc-del/vpn/internal/profile"
)

// profileCommand builds the single artifact a device needs, out of two
// things that already exist: the credential this tool issued, and the
// WireGuard config the provisioning script wrote.
//
// Nothing here is retyped. Every value a person copies by hand is a
// value that can be copied wrong, and the faults that produces — a
// truncated key, a pin off by one character — do not announce
// themselves as mistakes. They look like a server that is down.
func profileCommand(file *clients.File, args []string) error {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `obfsctl profile builds a connection profile for one client.

Usage:
  obfsctl -file clients.yaml profile <client-id> -wg <client.conf> -endpoint <host:port> [flags]

The WireGuard config is the client-side file the provisioning script
printed for this client. The endpoint is the server's public address —
the one clients connect to, not the local address WireGuard uses.

Flags:
`)
		fs.PrintDefaults()
	}

	var (
		wgPath   = fs.String("wg", "", "path to this client's WireGuard config (the file with a [Peer] section)")
		endpoint = fs.String("endpoint", "", "the server's public address, host:port")
		mode     = fs.String("mode", "tls", `transport mode: "tls" or "udp"`)
		pin      = fs.String("pin", "", "the server certificate's SHA-256 fingerprint, from gencert (tls mode)")
		sni      = fs.String("sni", "", "the server name to present in the TLS handshake (tls mode)")
		junk     = fs.Int("junk", 0, "random packets to send when a udp session opens")
		name     = fs.String("name", "", "a label for this connection, shown to the person using it")
		out      = fs.String("out", "", "write the profile here instead of printing it")
	)

	// The client id may come before or after the flags. Go's flag package
	// stops at the first positional argument, so writing the id first —
	// which reads naturally, and matches every other subcommand here —
	// would otherwise leave the flags unparsed and produce a confusing
	// complaint about the id instead.
	clientID, rest := splitPositional(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if clientID == "" {
		// Written after the flags instead.
		if fs.NArg() == 1 {
			clientID = fs.Arg(0)
		}
	} else if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("profile takes one client id, got %q as well", fs.Arg(0))
	}
	if clientID == "" {
		fs.Usage()
		return fmt.Errorf("profile needs a client id")
	}

	client, err := findClient(file, clientID)
	if err != nil {
		return err
	}
	if client.Disabled {
		// Handing somebody a profile that cannot connect wastes their
		// time and looks like a fault in the server.
		return fmt.Errorf("client %q is revoked; restore it first if this profile is meant to work", clientID)
	}
	if *wgPath == "" {
		return fmt.Errorf("-wg is required: the profile carries the WireGuard settings, and they come from that file")
	}

	wgText, err := os.ReadFile(*wgPath)
	if err != nil {
		return fmt.Errorf("reading the WireGuard config: %w", err)
	}
	wg, localAddr, err := profile.ParseWireGuardConfig(string(wgText))
	if err != nil {
		return fmt.Errorf("%s: %w", *wgPath, err)
	}

	p := profile.Profile{
		Version:  profile.Version,
		Name:     *name,
		ClientID: clientID,
		Transport: profile.Transport{
			Mode:             strings.ToLower(*mode),
			Endpoint:         *endpoint,
			LocalAddr:        localAddr,
			PSKBase64:        client.PSKBase64,
			ServerName:       *sni,
			PinnedCertSHA256: *pin,
			JunkPackets:      *junk,
		},
		WireGuard: wg,
	}

	link, err := profile.Encode(p)
	if err != nil {
		return err
	}

	if *out == "" {
		fmt.Println(link)
		fmt.Fprintf(os.Stderr, "\n%s\n", p.String())
		fmt.Fprintf(os.Stderr, bearerWarning, clientID)
		return nil
	}

	// 0600: the profile holds this device's keys, so it is written the
	// way the credential file is.
	if err := os.WriteFile(*out, []byte(link+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n%s\n", *out, p.String())
	fmt.Printf(bearerWarning, clientID)
	return nil
}

const bearerWarning = `
Anyone who reads this profile has the access it grants: it carries the
pre-shared key and this device's WireGuard private key, and a copy is
indistinguishable from the original. Send it the way you would send a
password, and if it goes astray, revoke the client:

    obfsctl -file clients.yaml revoke %s
`

func findClient(file *clients.File, id string) (clients.Client, error) {
	for _, c := range file.Clients {
		if c.ID == id {
			return c, nil
		}
	}
	return clients.Client{}, fmt.Errorf("no client named %q; run 'obfsctl list' to see who has access", id)
}

// splitPositional pulls the client id out when it is written first, which
// is how every other subcommand here reads. Anything else is left to the
// flag package, which returns it as a positional after the flags.
//
// Deliberately not clever: an earlier version guessed which bare words
// were flag values, which would have quietly broken the first time a
// boolean flag was added.
func splitPositional(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}
