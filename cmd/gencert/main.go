// Command gencert generates a self-signed TLS certificate for obfsserver's
// TLS mode and prints the SHA-256 pin to put in the client's config.
package main

import (
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

	fmt.Printf("Certificate: %s\nKey:         %s\n\n", *certPath, *keyPath)
	fmt.Printf("pinned_cert_sha256: %s\n", pin)
	fmt.Println("\nCopy this pin into the client's obfsclient.yaml (mode: tls).")
}
