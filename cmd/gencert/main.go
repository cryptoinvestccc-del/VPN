// Command gencert generates a self-signed TLS certificate for obfsserver's
// TLS mode, along with a pre-shared key, and prints the config lines both
// ends need.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"

	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

func main() {
	commonName := flag.String("cn", "www.example.com", "common name / SNI cover identity for the certificate")
	certPath := flag.String("cert", "server.crt", "output certificate path")
	keyPath := flag.String("key", "server.key", "output private key path")
	flag.Parse()

	if err := tlscert.Generate(*commonName, *certPath, *keyPath); err != nil {
		log.Fatalf("gencert: %v", err)
	}

	pin, err := tlscert.PinFromCertFile(*certPath)
	if err != nil {
		log.Fatalf("gencert: %v", err)
	}

	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		log.Fatalf("gencert: %v", err)
	}
	pskB64 := base64.StdEncoding.EncodeToString(psk[:])

	fmt.Printf("Certificate: %s\nKey:         %s\n\n", *certPath, *keyPath)
	fmt.Println("Put this in BOTH configs (server and client):")
	fmt.Printf("psk: %q\n\n", pskB64)
	fmt.Println("Put this in the client config only:")
	fmt.Printf("pinned_cert_sha256: %s\n", pin)
	fmt.Println()
	fmt.Println("The pin authenticates the server to the client. The psk authorizes")
	fmt.Println("the client to the server — the certificate is shown to everyone who")
	fmt.Println("connects, so it cannot serve that purpose. Keep the psk secret; the")
	fmt.Println("pin is not secret.")
}
