package transport

import (
	"crypto/x509"
	"log"
	"time"
)

const (
	// maxConcurrentSessions bounds how many TLS sessions the server holds
	// at once. Each costs a socket to the local WireGuard server, two
	// goroutines and their buffers, so an unbounded count is a way to
	// exhaust the host with connections that never authenticate.
	maxConcurrentSessions = 2048

	// maxConcurrentHandshakes bounds how many TLS handshakes run at the
	// same time. A handshake is the expensive part — a signature per
	// connection — so this is what keeps a flood of half-open connections
	// from consuming the CPU that serving real clients needs. Connections
	// beyond it wait rather than being dropped; a burst of genuine
	// reconnects (a server restart with many clients) is queued, not
	// refused.
	maxConcurrentHandshakes = 64

	// certExpiryWarning is how far ahead the server starts complaining
	// about a certificate that is running out.
	certExpiryWarning = 30 * 24 * time.Hour
)

// semaphore bounds concurrency. A buffered channel is the whole
// implementation; naming it makes the call sites read as intent.
type semaphore chan struct{}

func newSemaphore(n int) semaphore { return make(semaphore, n) }

// tryAcquire takes a slot if one is free, reporting whether it got one.
func (s semaphore) tryAcquire() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}

// acquire waits for a slot.
func (s semaphore) acquire() { s <- struct{}{} }

func (s semaphore) release() { <-s }

// warnOnCertificateExpiry tells the operator when the server's
// certificate is close to expiring, or already has.
//
// Expiry does not break the tunnel: clients pin the certificate by
// fingerprint and never check its validity dates, so an expired one keeps
// working. It breaks the disguise instead. Real sites renew, so a port
// serving a certificate that expired months ago is a detail an active
// prober can compare against a genuine HTTPS server — quietly undoing
// what TLS mode exists to provide.
func warnOnCertificateExpiry(der []byte) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		log.Printf("transport: could not read the certificate's expiry: %v", err)
		return
	}

	remaining := time.Until(cert.NotAfter)
	switch {
	case remaining <= 0:
		log.Printf("transport: the TLS certificate expired %s ago. Clients keep working "+
			"(they pin it by fingerprint), but a server presenting an expired certificate "+
			"stands out to anyone probing the port. Regenerate it with gencert and "+
			"distribute the new pin.", (-remaining).Round(24*time.Hour))
	case remaining < certExpiryWarning:
		log.Printf("transport: the TLS certificate expires in %s. Regenerate it with "+
			"gencert and distribute the new pin before then.", remaining.Round(24*time.Hour))
	}
}
