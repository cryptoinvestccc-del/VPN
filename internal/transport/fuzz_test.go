package transport

import (
	"bytes"
	"testing"
)

// FuzzReadFrame targets the stream parser. It runs on bytes from anyone
// who completed a TLS handshake — which, by design, includes censors
// probing the port — so it must reject anything malformed without
// panicking or reading past its buffer.
func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0x00, 0x01, 0xff})
	f.Add([]byte{0xff, 0xff})
	f.Add([]byte{0x00, 0x00})
	f.Add([]byte("GET / HTTP/1.1\r\n\r\n"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		buf := make([]byte, maxFrameSize)
		frame, err := readFrame(bytes.NewReader(data), buf)
		if err != nil {
			return
		}
		if len(frame) > len(buf) {
			t.Fatalf("readFrame returned %d bytes into a %d-byte buffer", len(frame), len(buf))
		}
	})
}

// FuzzCannedResponse checks the fallback path that answers unauthorized
// peers: it inspects attacker-controlled bytes to decide what an ordinary
// web server would reply, and must always produce a well-formed response.
func FuzzCannedResponse(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\n"))
	f.Add([]byte("GE"))
	f.Add([]byte{0x00})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		response := cannedResponse(data)
		if !bytes.HasPrefix(response, []byte("HTTP/1.1 ")) {
			t.Fatalf("fallback produced something that is not an HTTP response: %q", response)
		}
	})
}
