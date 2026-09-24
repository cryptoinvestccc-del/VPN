package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestTheAppTalksToTheServer runs the app's own networking code — the
// class that builds the request, reads the reply and parses the name
// servers out of it — against a real provisioning service.
//
// It exists because of a specific failure: the server sent the name
// servers as a list and the app read the field as a string, so what
// reached Android was the text ["1.1.1.1"], brackets and quotes
// included, and Android refused it as not being an address. That was
// found on a phone. Everything it needed to be found here was already
// here.
func TestTheAppTalksToTheServer(t *testing.T) {
	jvm, ok := desktopClasses()
	if !ok {
		t.Skip("run android/desktop-check/build.sh first")
	}
	dir := t.TempDir()

	_, serverPub := mustKeypair(t)
	peersFile := filepath.Join(dir, "peers")
	if err := os.WriteFile(peersFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	stub := filepath.Join(dir, "docker")
	build(t, stub, ".", "./stubdocker")
	provision := filepath.Join(dir, "besy-provision")
	build(t, provision, "../..", "./cmd/besy-provision")

	addr := "127.0.0.1:9189"
	service := exec.Command(provision,
		"-listen", addr,
		"-endpoint", "127.0.0.1:51820",
		"-subnet", "10.8.1.0/24")
	service.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_CONTAINER=amnezia-awg2",
		"STUB_IFACE=awg0",
		"STUB_SERVER_PUBLIC="+serverPub,
		"STUB_PEERS="+peersFile,
		"STUB_SHOWCONF="+realShowconf,
	)
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = service.Process.Kill()
		_, _ = service.Process.Wait()
	}()
	waitListening(t, addr, 10*time.Second)

	_, clientPub := mustKeypair(t)
	endpoint := "http://" + addr + "/v1/issue"

	cmd := exec.Command("java", "-cp", jvm.cp(),
		"ReplyCheck", endpoint, clientPub)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the app could not fetch a configuration: %v\n%s", err, stderr.String())
	}

	var got struct {
		Address         string   `json:"address"`
		Endpoint        string   `json:"endpoint"`
		ServerPublicKey string   `json:"server_public_key"`
		DNS             []string `json:"dns"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the app produced %q, which is not readable: %v", out, err)
	}

	if got.ServerPublicKey != serverPub {
		t.Errorf("the app read the server key as %q, the server has %q", got.ServerPublicKey, serverPub)
	}
	if got.Endpoint != "127.0.0.1:51820" {
		t.Errorf("the app read the endpoint as %q", got.Endpoint)
	}
	if !strings.HasPrefix(got.Address, "10.8.1.") {
		t.Errorf("the app read the address as %q, which is not in the subnet", got.Address)
	}

	// The whole point. Each entry must be an address and nothing else:
	// no brackets, no quotes, no commas, nothing left over from the
	// format it travelled in.
	if len(got.DNS) == 0 {
		t.Fatal("the app found no name servers in the reply")
	}
	for _, server := range got.DNS {
		if strings.ContainsAny(server, "[]\", ") {
			t.Errorf("the app would hand Android %q, which is not an address", server)
		}
	}
	t.Logf("   the app read: address %s, dns %v", got.Address, got.DNS)
}

// TestTheAppSurvivesAnOddReply feeds the app's own parsing the shapes a
// server might plausibly send, including the one that broke it.
func TestTheAppSurvivesAnOddReply(t *testing.T) {
	jvm, ok := desktopClasses()
	if !ok {
		t.Skip("run android/desktop-check/build.sh first")
	}

	cases := []struct {
		name  string
		field string
		want  []string
	}{
		{"a list, as the server sends it", `["1.1.1.1","8.8.8.8"]`, []string{"1.1.1.1", "8.8.8.8"}},
		{"one address as a string", `"1.1.1.1"`, []string{"1.1.1.1"}},
		{"several in one string", `"1.1.1.1, 8.8.8.8"`, []string{"1.1.1.1", "8.8.8.8"}},
		{"a list that arrived as text", `"[\"1.1.1.1\"]"`, nil},
		{"nothing at all", `[]`, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := `{"address":"10.8.1.9/32","endpoint":"127.0.0.1:51820",` +
				`"server_public_key":"mKV46DYI4RCES+opiy6r6hyTJ6lnf0Q1k1Krtmp+vXU=",` +
				`"dns":` + c.field + `}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			cmd := exec.Command("java", "-cp", jvm.cp(), "ReplyCheck",
				server.URL, "mKV46DYI4RCES+opiy6r6hyTJ6lnf0Q1k1Krtmp+vXU=")
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("the app gave up on this reply: %v\n%s", err, stderr.String())
			}
			var got struct {
				DNS []string `json:"dns"`
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.DNS, c.want) && !(len(got.DNS) == 0 && len(c.want) == 0) {
				t.Errorf("the app would hand Android %q, expected %q", got.DNS, c.want)
			}
		})
	}
}
