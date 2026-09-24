package provision

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestManyDevicesProvisionOverHTTP drives the whole path the app will
// take — real HTTP, real JSON, many devices at once — rather than
// calling Issue directly.
//
// The fake device refuses a duplicate address, so if serialisation were
// ever lost the failure surfaces here as an error rather than as two
// people sharing a tunnel address on a server nobody is watching.
func TestManyDevicesProvisionOverHTTP(t *testing.T) {
	device := &fakeDevice{}
	svc := testService(t, device)
	server := httptest.NewServer(Handler(svc, NewLimiter(1e6, 1e6)))
	defer server.Close()

	const devices = 100
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[string]string{}
		fail []string
	)

	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := testKey(i)

			body, _ := json.Marshal(issueRequest{PublicKey: key})
			resp, err := http.Post(server.URL+"/v1/issue", "application/json", bytes.NewReader(body))
			if err != nil {
				mu.Lock()
				fail = append(fail, err.Error())
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			var got issueResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != http.StatusOK {
				mu.Lock()
				fail = append(fail, resp.Status)
				mu.Unlock()
				return
			}

			mu.Lock()
			if other, clash := seen[got.Address]; clash {
				fail = append(fail, "address "+got.Address+" went to both "+other+" and "+key)
			}
			seen[got.Address] = key
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if len(fail) > 0 {
		t.Fatalf("%d failures, first: %s", len(fail), fail[0])
	}
	if len(seen) != devices {
		t.Fatalf("%d devices received %d distinct addresses", devices, len(seen))
	}
	if device.count() != devices {
		t.Errorf("the interface has %d peers for %d devices", device.count(), devices)
	}
}

// TestReinstallDoesNotConsumeASecondAddress: reinstalling the app makes
// a new key, and that legitimately takes another address. Retrying with
// the same key must not.
func TestReinstallDoesNotConsumeASecondAddress(t *testing.T) {
	device := &fakeDevice{}
	server := httptest.NewServer(Handler(testService(t, device), NewLimiter(1e6, 1e6)))
	defer server.Close()

	issue := func(key string) string {
		body, _ := json.Marshal(issueRequest{PublicKey: key})
		resp, err := http.Post(server.URL+"/v1/issue", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got issueResponse
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got.Address
	}

	first := issue(testKey(1))
	for i := 0; i < 5; i++ {
		if again := issue(testKey(1)); again != first {
			t.Fatalf("a retry moved the device from %s to %s", first, again)
		}
	}
	if device.count() != 1 {
		t.Fatalf("retries left %d peers", device.count())
	}

	if issue(testKey(2)) == first {
		t.Error("a genuinely new key was given the same address")
	}
	if device.count() != 2 {
		t.Errorf("after a reinstall the interface has %d peers, expected 2", device.count())
	}
}

// TestIssuedConfigIsUsable checks that what crosses the wire is enough
// to build a working AmneziaWG config — the obfuscation parameters
// included, since without them the handshake is never answered and
// nothing says why.
func TestIssuedConfigIsUsable(t *testing.T) {
	server := httptest.NewServer(Handler(testService(t, &fakeDevice{}), NewLimiter(1e6, 1e6)))
	defer server.Close()

	body, _ := json.Marshal(issueRequest{PublicKey: testKey(7)})
	resp, err := http.Post(server.URL+"/v1/issue", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got issueResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	conf := strings.Join([]string{
		"[Interface]",
		"PrivateKey = " + "DEVICEKEY=",
		"Address = " + got.Address,
		"MTU = " + strconv.Itoa(got.MTU),
		"Jc = " + got.Awg["jc"],
		"H1 = " + got.Awg["h1"],
		"[Peer]",
		"PublicKey = " + got.ServerPublicKey,
		"AllowedIPs = " + got.AllowedIPs,
		"Endpoint = " + got.Endpoint,
	}, "\n")

	for _, must := range []string{"Address = 10.8.0.", "Jc = 4", "H1 = 1", "Endpoint = 198.51.100.9:51820"} {
		if !strings.Contains(conf, must) {
			t.Errorf("the config an app would build is missing %q:\n%s", must, conf)
		}
	}
}
