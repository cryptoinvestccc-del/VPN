package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAppAndHarnessAgree checks that the configuration the Android app
// builds is the same text as the one this harness builds — the one every
// other test here proves carries traffic.
//
// Without this the harness only proves that *a* correct configuration
// works, which says nothing about the app. The app's own class is
// compiled and run on the desktop JVM, against a stand-in for
// android.util.Base64, because android.jar's copy only throws.
func TestAppAndHarnessAgree(t *testing.T) {
	jvm, ok := desktopClasses()
	if !ok {
		t.Skip("run android/desktop-check/build.sh first")
	}

	const privateKey = "QJ1cq3GTPCyTUkFjLRiLDcsLzqZ0bLKNrqQK7yzM01A="

	cases := map[string]*issued{
		"the server as it runs today": {
			Address:         "10.8.1.77/32",
			DNS:             []string{"1.1.1.1"},
			MTU:             1280,
			ServerPublicKey: "mKV46DYI4RCES+opiy6r6hyTJ6lnf0Q1k1Krtmp+vXU=",
			Endpoint:        "besyvpn.online:51820",
			AllowedIPs:      "0.0.0.0/0, ::/0",
			Keepalive:       25,
			Awg:             awgFromShowconf(),
		},
		"no obfuscation at all": {
			Address:         "10.8.1.78/32",
			ServerPublicKey: "mKV46DYI4RCES+opiy6r6hyTJ6lnf0Q1k1Krtmp+vXU=",
			Endpoint:        "besyvpn.online:51820",
			AllowedIPs:      "0.0.0.0/0",
			Awg:             map[string]string{},
		},
		"packet templates, which are hex and not numbers": {
			Address:         "10.8.1.79/32",
			ServerPublicKey: "mKV46DYI4RCES+opiy6r6hyTJ6lnf0Q1k1Krtmp+vXU=",
			Endpoint:        "besyvpn.online:51820",
			AllowedIPs:      "0.0.0.0/0",
			Keepalive:       25,
			Awg:             withTemplates(),
		},
	}

	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(reply)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "reply.json")
			if err := os.WriteFile(file, body, 0o600); err != nil {
				t.Fatal(err)
			}

			mine, err := buildUAPI(privateKey, reply)
			if err != nil {
				t.Fatalf("the harness could not build a configuration: %v", err)
			}

			cmd := exec.Command("java", "-cp", jvm.classes+":"+jvm.json, "CrossCheck", privateKey, file)
			// Stdout only: the JVM prints a line about its own
			// options to stderr, which is not part of the answer.
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("the app could not build a configuration: %v\n%s", err, stderr.String())
			}
			theirs := string(out)

			if theirs != mine {
				t.Errorf("the app and the harness disagree\n--- the app ---\n%s\n--- the harness ---\n%s\n--- difference ---\n%s",
					theirs, mine, diff(theirs, mine))
			}
		})
	}
}

func withTemplates() map[string]string {
	m := awgFromShowconf()
	// What AmneziaWG calls a signature: a packet written out byte by
	// byte, carried as hex. Nothing about it is a number.
	m["i1"] = "<b 0xf1a2b3c4>"
	m["i2"] = "<b 0x0102030405>"
	return m
}

func diff(a, b string) string {
	left, right := strings.Split(a, "\n"), strings.Split(b, "\n")
	var out []string
	for i := 0; i < len(left) || i < len(right); i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			out = append(out, "app: "+l+" | harness: "+r)
		}
	}
	return strings.Join(out, "\n")
}

// appUAPI runs the app's own class, when it has been compiled for the
// desktop JVM, and reports whether it could.
func appUAPI(t *testing.T, privateKey string, reply *issued) (string, bool) {
	t.Helper()
	jvm, ok := desktopClasses()
	if !ok {
		return "", false
	}
	body, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "reply.json")
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("java", "-cp", jvm.classes+":"+jvm.json, "CrossCheck", privateKey, file)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the app could not build a configuration: %v\n%s", err, stderr.String())
	}
	return string(out), true
}

// desktopClasses locates what android/desktop-check/build.sh produced.
type desktop struct{ classes, json string }

func desktopClasses() (desktop, bool) {
	d := desktop{
		classes: "../../android/.build-desktop",
		json:    "../../android/.toolchain/json.jar",
	}
	if _, err := os.Stat(filepath.Join(d.classes, "vpn", "besy", "Uapi.class")); err != nil {
		return desktop{}, false
	}
	if _, err := os.Stat(d.json); err != nil {
		return desktop{}, false
	}
	return d, true
}
