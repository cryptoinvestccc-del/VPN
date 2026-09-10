package transport

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
)

func TestSemaphoreBoundsConcurrency(t *testing.T) {
	s := newSemaphore(2)

	if !s.tryAcquire() || !s.tryAcquire() {
		t.Fatal("a fresh semaphore refused its own capacity")
	}
	if s.tryAcquire() {
		t.Fatal("semaphore handed out more slots than its capacity")
	}

	s.release()
	if !s.tryAcquire() {
		t.Fatal("a released slot was not reusable")
	}
}

// TestSemaphoreAcquireWaits covers the handshake path, which queues rather
// than refusing: a burst of genuine reconnects after a server restart
// should be served late, not dropped.
func TestSemaphoreAcquireWaits(t *testing.T) {
	s := newSemaphore(1)
	s.acquire()

	acquired := make(chan struct{})
	go func() {
		s.acquire()
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("acquire returned while the only slot was held")
	case <-time.After(50 * time.Millisecond):
	}

	s.release()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("acquire never returned after a slot was freed")
	}
}

func TestSemaphoreIsSafeUnderConcurrentUse(t *testing.T) {
	const capacity = 8
	s := newSemaphore(capacity)

	var mu sync.Mutex
	held, peak := 0, 0

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.acquire()

			mu.Lock()
			held++
			if held > peak {
				peak = held
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			held--
			mu.Unlock()
			s.release()
		}()
	}
	wg.Wait()

	if peak > capacity {
		t.Fatalf("%d holders at once, above the capacity of %d", peak, capacity)
	}
}

// selfSignedDER builds a certificate expiring at the given time, so the
// expiry warning can be checked without waiting for one to age.
func selfSignedDER(t *testing.T, notAfter time.Time) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "expiry.test"},
		NotBefore:    time.Now().Add(-2 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func captureLog(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(previous)

	fn()
	return buf.String()
}

// TestCertificateExpiryWarning covers an operational trap. Expiry does not
// break the tunnel — clients pin the certificate and ignore its dates — so
// nothing would otherwise tell the operator. What it breaks is the
// disguise: real sites renew, and a port serving a long-expired
// certificate is something an active prober can notice.
func TestCertificateExpiryWarning(t *testing.T) {
	for _, tc := range []struct {
		name     string
		notAfter time.Time
		want     string
	}{
		{"already expired", time.Now().Add(-48 * time.Hour), "expired"},
		{"expiring soon", time.Now().Add(5 * 24 * time.Hour), "expires in"},
		{"healthy", time.Now().Add(365 * 24 * time.Hour), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			der := selfSignedDER(t, tc.notAfter)
			output := captureLog(t, func() { warnOnCertificateExpiry(der) })

			if tc.want == "" {
				if output != "" {
					t.Fatalf("a healthy certificate produced a warning: %s", output)
				}
				return
			}
			if !strings.Contains(output, tc.want) {
				t.Fatalf("expected a warning containing %q, got %q", tc.want, output)
			}
			if !strings.Contains(output, "gencert") {
				t.Fatalf("the warning does not tell the operator what to do: %q", output)
			}
		})
	}
}

func TestCertificateExpiryWarningIgnoresGarbage(t *testing.T) {
	output := captureLog(t, func() { warnOnCertificateExpiry([]byte("not a certificate")) })
	if !strings.Contains(output, "could not read") {
		t.Fatalf("unparseable certificate should be reported, got %q", output)
	}
}
