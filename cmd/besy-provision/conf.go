package main

import (
	"net/netip"
	"strings"
)

// quickOnly are the [Interface] keys that belong to awg-quick rather
// than to the interface. `awg showconf` cannot print them — the
// interface does not hold them — so a file written from showconf alone
// loses them.
//
// That was the first persistence step: it wrote showconf over Amnezia's
// awg0.conf after every credential. Address went with it, so the next
// restart of the container would have brought the interface up with no
// address, and every client on the server — Amnezia's as well as ours —
// with it. awg-quick's own save avoids this by rebuilding these lines;
// this does the same.
var quickOnly = map[string]bool{
	"address": true, "dns": true, "mtu": true, "table": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true,
}

// mergeConf builds the file to write: the interface's addresses as it
// is running now, the awg-quick lines the existing file already had, and
// everything showconf reports.
//
// Addresses come from the running interface in preference to the file,
// as awg-quick save takes them, because a file this service damaged
// before may not have them any more. The file's own Address lines are
// used only when the interface cannot be asked.
func mergeConf(existing, showconf string, live []string) string {
	var fileAddrs, kept []string
	section := "interface"
	for _, line := range strings.Split(existing, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "[") {
			section = strings.ToLower(strings.Trim(text, "[]"))
			continue
		}
		if section != "interface" || text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, _, ok := strings.Cut(text, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "address" {
			fileAddrs = append(fileAddrs, text)
			continue
		}
		if quickOnly[key] {
			kept = append(kept, text)
		}
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	if len(live) > 0 {
		for _, a := range live {
			b.WriteString("Address = " + a + "\n")
		}
	} else {
		for _, l := range fileAddrs {
			b.WriteString(l + "\n")
		}
	}
	for _, l := range kept {
		b.WriteString(l + "\n")
	}

	// showconf's own [Interface] header is dropped; the rest, peers
	// included, goes in as printed.
	body := strings.TrimLeft(showconf, "\n")
	if rest, ok := strings.CutPrefix(body, "[Interface]\n"); ok {
		body = rest
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// hasAddress reports whether a configuration file carries an Address
// line in its [Interface] section.
func hasAddress(conf string) bool {
	section := "interface"
	for _, line := range strings.Split(conf, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "[") {
			section = strings.ToLower(strings.Trim(text, "[]"))
			continue
		}
		if section != "interface" {
			continue
		}
		key, _, ok := strings.Cut(text, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "address") {
			return true
		}
	}
	return false
}

// parseAddrs reads `ip -o addr show dev X`: one line per address, the
// address following "inet" or "inet6".
//
// Link-local addresses are left out. The kernel gives a TUN interface a
// fe80:: address of its own when it comes up; nobody configured it, and
// written back as an Address line it would be added by hand at the next
// start as well — at best a duplicate, at worst the step that makes
// awg-quick stop halfway. The first run of the repair on the production
// server wrote 54 bytes where one IPv4 Address line is 22, which is what
// a second, link-local line looks like.
func parseAddrs(out string) []string {
	var addrs []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "inet" || f[i] == "inet6" {
				if !linkLocal(f[i+1]) {
					addrs = append(addrs, f[i+1])
				}
			}
		}
	}
	return addrs
}

// linkLocal reports an address the kernel assigns by itself.
func linkLocal(cidr string) bool {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	return p.Addr().IsLinkLocalUnicast()
}

// hasLinkLocalAddress reports an Address line carrying a link-local
// address, which only a previous version of this code would have put
// there.
func hasLinkLocalAddress(conf string) bool {
	section := "interface"
	for _, line := range strings.Split(conf, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "[") {
			section = strings.ToLower(strings.Trim(text, "[]"))
			continue
		}
		key, val, ok := strings.Cut(text, "=")
		if section != "interface" || !ok || !strings.EqualFold(strings.TrimSpace(key), "address") {
			continue
		}
		for _, a := range strings.Split(val, ",") {
			if linkLocal(strings.TrimSpace(a)) {
				return true
			}
		}
	}
	return false
}
