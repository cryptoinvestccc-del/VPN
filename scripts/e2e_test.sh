#!/usr/bin/env bash
# End-to-end test of the shipped product: real binaries, real config
# files, real processes, real sockets. The Go tests exercise the packages;
# this exercises what an operator actually deploys.
#
# Usage: ./scripts/e2e_test.sh
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$(mktemp -d)"
pids=()

cleanup() {
	for pid in "${pids[@]:-}"; do
		kill "$pid" 2>/dev/null || true
	done
	wait 2>/dev/null || true
	rm -rf "$work_dir"
}
trap cleanup EXIT

pass() { echo "  PASS: $1"; }
fail() { echo "  FAIL: $1" >&2; exit 1; }

echo "==> building binaries"
cd "$repo_dir"
go build -o "$work_dir/obfsserver" ./cmd/obfsserver
go build -o "$work_dir/obfsclient" ./cmd/obfsclient
go build -o "$work_dir/gencert" ./cmd/gencert
go build -o "$work_dir/obfsctl" ./cmd/obfsctl

# A stand-in for the WireGuard server: echoes datagrams back.
cat > "$work_dir/echo_peer.py" <<'PY'
import socket, sys
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("127.0.0.1", int(sys.argv[1])))
while True:
    data, addr = sock.recvfrom(65535)
    sock.sendto(data, addr)
PY

# Sends one datagram to the tunnel entrance and prints the reply.
cat > "$work_dir/probe.py" <<'PY'
import socket, sys
port, payload_size = int(sys.argv[1]), int(sys.argv[2])
payload = bytes(range(256)) * (payload_size // 256) + bytes(payload_size % 256)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(5)
sock.connect(("127.0.0.1", port))
sock.send(payload)
try:
    reply = sock.recv(65535)
except socket.timeout:
    print("TIMEOUT"); sys.exit(1)
print("OK" if reply == payload else f"MISMATCH len={len(reply)} want={len(payload)}")
PY

wg_port=51900
server_port=51901
client_port=51902
tls_port=51903
tls_client_port=51904

python3 "$work_dir/echo_peer.py" "$wg_port" &
pids+=($!)
sleep 0.5

psk="$(openssl rand -base64 32)"

##############################################################################
echo "==> UDP mode"
##############################################################################
cat > "$work_dir/obfsserver.yaml" <<EOF
mode: "udp"
psk: "$psk"
local_addr: "127.0.0.1:$wg_port"
listen_wire_addr: "127.0.0.1:$server_port"
EOF

cat > "$work_dir/obfsclient.yaml" <<EOF
mode: "udp"
psk: "$psk"
local_addr: "127.0.0.1:$client_port"
remote_wire_addr: "127.0.0.1:$server_port"
junk_packets: 3
EOF

"$work_dir/obfsserver" -config "$work_dir/obfsserver.yaml" >"$work_dir/server.log" 2>&1 &
server_pid=$!
pids+=($server_pid)
"$work_dir/obfsclient" -config "$work_dir/obfsclient.yaml" >"$work_dir/client.log" 2>&1 &
client_pid=$!
pids+=($client_pid)
sleep 1

for size in 64 1452 8000; do
	result="$(python3 "$work_dir/probe.py" "$client_port" "$size")"
	[[ "$result" == "OK" ]] || fail "UDP mode, ${size}-byte packet: $result"
	pass "UDP mode carries a ${size}-byte packet"
done

# The public port must ignore garbage without answering: a port that
# replies to random data identifies itself to an active DPI probe.
probe_reply="$(python3 - "$server_port" <<'PY'
import socket, sys, os
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(2)
sock.connect(("127.0.0.1", int(sys.argv[1])))
sock.send(os.urandom(200))
try:
    sock.recv(65535); print("RESPONDED")
except socket.timeout:
    print("SILENT")
PY
)"
[[ "$probe_reply" == "SILENT" ]] || fail "server answered an unauthenticated probe: $probe_reply"
pass "server stays silent when probed with random data"

# Graceful shutdown: SIGTERM must exit 0, so systemd doesn't log a crash.
kill -TERM $client_pid
if wait $client_pid 2>/dev/null; then
	pass "client exits cleanly on SIGTERM"
else
	fail "client exited with a non-zero status on SIGTERM"
fi

kill -TERM $server_pid
if wait $server_pid 2>/dev/null; then
	pass "server exits cleanly on SIGTERM"
else
	fail "server exited with a non-zero status on SIGTERM"
fi

##############################################################################
echo "==> TLS mode"
##############################################################################
gencert_out="$("$work_dir/gencert" -cn www.example.com \
	-cert "$work_dir/server.crt" -key "$work_dir/server.key")"
pin="$(echo "$gencert_out" | grep pinned_cert_sha256 | awk '{print $2}')"
tls_psk="$(echo "$gencert_out" | grep '^psk:' | sed 's/^psk: "//; s/"$//')"
[[ -n "$pin" ]] || fail "gencert did not print a certificate pin"
[[ -n "$tls_psk" ]] || fail "gencert did not print a pre-shared key"
pass "gencert produced a certificate, pin and pre-shared key"

cat > "$work_dir/obfsserver-tls.yaml" <<EOF
mode: "tls"
psk: "$tls_psk"
local_addr: "127.0.0.1:$wg_port"
listen_tls_addr: "127.0.0.1:$tls_port"
cert_file: "$work_dir/server.crt"
key_file: "$work_dir/server.key"
EOF

cat > "$work_dir/obfsclient-tls.yaml" <<EOF
mode: "tls"
psk: "$tls_psk"
local_addr: "127.0.0.1:$tls_client_port"
remote_tls_addr: "127.0.0.1:$tls_port"
server_name: "www.example.com"
pinned_cert_sha256: "$pin"
EOF

"$work_dir/obfsserver" -config "$work_dir/obfsserver-tls.yaml" >"$work_dir/server-tls.log" 2>&1 &
tls_server_pid=$!
pids+=($tls_server_pid)
"$work_dir/obfsclient" -config "$work_dir/obfsclient-tls.yaml" >"$work_dir/client-tls.log" 2>&1 &
tls_client_pid=$!
pids+=($tls_client_pid)
sleep 1.5

for size in 64 1452 8000; do
	result="$(python3 "$work_dir/probe.py" "$tls_client_port" "$size")"
	[[ "$result" == "OK" ]] || fail "TLS mode, ${size}-byte packet: $result"
	pass "TLS mode carries a ${size}-byte packet"
done

# The whole point of TLS mode: an active prober speaking TLS must get a
# real handshake, so the port looks like an ordinary HTTPS server.
if echo | openssl s_client -connect "127.0.0.1:$tls_port" \
	-servername www.example.com </dev/null 2>/dev/null | grep -q "BEGIN CERTIFICATE"; then
	pass "port completes a real TLS handshake with an active prober"
else
	fail "port did not present a certificate to a TLS prober"
fi

# Probe resistance: an unauthorized peer must be answered the way a real
# web server answers, not with silence. A port that accepts bytes, replies
# to nothing and never hangs up identifies itself by how it fails.
cat > "$work_dir/http_probe.py" <<'PROBE'
import socket, ssl, sys
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
sock = ctx.wrap_socket(
    socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=10),
    server_hostname="www.example.com")
sock.settimeout(15)
sock.send(b"GET / HTTP/1.1\r\nHost: www.example.com\r\n\r\n")
try:
    print(sock.recv(256).decode("latin-1").split("\r\n")[0])
except Exception as exc:
    print("NO_RESPONSE %s" % exc)
PROBE

probe_out="$(python3 "$work_dir/http_probe.py" "$tls_port")"
case "$probe_out" in
	HTTP/1.1*) pass "unauthorized probe gets a web-server response ($probe_out)" ;;
	*) fail "probe got no plausible web response: $probe_out" ;;
esac

# An unauthorized peer must not be able to push traffic to WireGuard.
cat > "$work_dir/obfsclient-wrongpsk.yaml" <<EOF
mode: "tls"
psk: "$(openssl rand -base64 32)"
local_addr: "127.0.0.1:51907"
remote_tls_addr: "127.0.0.1:$tls_port"
server_name: "www.example.com"
pinned_cert_sha256: "$pin"
EOF
"$work_dir/obfsclient" -config "$work_dir/obfsclient-wrongpsk.yaml" >"$work_dir/client-wrongpsk.log" 2>&1 &
wrongpsk_pid=$!
pids+=($wrongpsk_pid)
sleep 1
if [[ "$(python3 "$work_dir/probe.py" 51907 128 2>/dev/null)" == "OK" ]]; then
	fail "a client with the wrong PSK tunnelled traffic through"
fi
pass "a client with the wrong PSK cannot tunnel"
kill -TERM $wrongpsk_pid 2>/dev/null || true

# Server restart: the client must recover on its own.
kill -TERM $tls_server_pid
wait $tls_server_pid 2>/dev/null || true
sleep 0.5
"$work_dir/obfsserver" -config "$work_dir/obfsserver-tls.yaml" >>"$work_dir/server-tls.log" 2>&1 &
tls_server_pid=$!
pids+=($tls_server_pid)

recovered=""
for _ in $(seq 1 20); do
	sleep 1
	if [[ "$(python3 "$work_dir/probe.py" "$tls_client_port" 512 2>/dev/null)" == "OK" ]]; then
		recovered=yes
		break
	fi
done
[[ -n "$recovered" ]] || fail "client never recovered after the server restarted"
pass "client reconnects by itself after a server restart"

# A client pinned to the wrong certificate must refuse to run, not retry
# forever against a possible interceptor.
cat > "$work_dir/obfsclient-badpin.yaml" <<EOF
mode: "tls"
psk: "$tls_psk"
local_addr: "127.0.0.1:51905"
remote_tls_addr: "127.0.0.1:$tls_port"
server_name: "www.example.com"
pinned_cert_sha256: "0000000000000000000000000000000000000000000000000000000000000000"
EOF

if timeout 20 "$work_dir/obfsclient" -config "$work_dir/obfsclient-badpin.yaml" \
	>"$work_dir/client-badpin.log" 2>&1; then
	fail "client accepted a server whose certificate did not match the pin"
fi
grep -q "pin mismatch" "$work_dir/client-badpin.log" \
	|| fail "client failed for the wrong reason: $(cat "$work_dir/client-badpin.log")"
pass "client refuses a certificate that does not match its pin"

##############################################################################
echo "==> configuration validation"
##############################################################################
cat > "$work_dir/bad.yaml" <<EOF
mode: "udp"
local_addr: "127.0.0.1:51906"
remote_wire_addr: "127.0.0.1:$server_port"
EOF
if "$work_dir/obfsclient" -config "$work_dir/bad.yaml" >"$work_dir/bad.log" 2>&1; then
	fail "client started in UDP mode without a PSK"
fi
grep -q "psk is required" "$work_dir/bad.log" || fail "missing PSK was not reported clearly"
pass "UDP mode without a PSK is rejected with a clear message"

cat > "$work_dir/bad-tls.yaml" <<EOF
mode: "tls"
local_addr: "127.0.0.1:51908"
remote_tls_addr: "127.0.0.1:$tls_port"
server_name: "www.example.com"
pinned_cert_sha256: "$pin"
EOF
if "$work_dir/obfsclient" -config "$work_dir/bad-tls.yaml" >"$work_dir/bad-tls.log" 2>&1; then
	fail "client started in TLS mode without a PSK"
fi
grep -q "psk is required" "$work_dir/bad-tls.log" || fail "missing PSK in TLS mode was not reported clearly"
pass "TLS mode without a PSK is rejected with a clear message"


##############################################################################
echo "==> per-client credentials and revocation"
##############################################################################
# What a shared key cannot do: take access away from one device without
# rekeying every device. Exercised through the shipped tools and a real
# SIGHUP, the way an operator would do it.
clients_file="$work_dir/clients.yaml"
alice_psk="$("$work_dir/obfsctl" -file "$clients_file" add alice | grep '^psk:' | sed 's/^psk: "//; s/"$//')"
bob_psk="$("$work_dir/obfsctl" -file "$clients_file" add bob | grep '^psk:' | sed 's/^psk: "//; s/"$//')"
[[ -n "$alice_psk" && -n "$bob_psk" ]] || fail "obfsctl did not generate client keys"
[[ "$alice_psk" != "$bob_psk" ]] || fail "two clients were given the same key"
pass "obfsctl issued distinct credentials per client"

[[ "$(stat -c '%a' "$clients_file")" == "600" ]] || fail "the credential file is readable by others"
pass "the credential file is not world-readable"

multi_port=51910
alice_port=51911
bob_port=51912

cat > "$work_dir/obfsserver-multi.yaml" <<EOF
mode: "tls"
clients_file: "$clients_file"
local_addr: "127.0.0.1:$wg_port"
listen_tls_addr: "127.0.0.1:$multi_port"
cert_file: "$work_dir/server.crt"
key_file: "$work_dir/server.key"
EOF

for name in alice bob; do
	port_var="${name}_port"; psk_var="${name}_psk"
	cat > "$work_dir/obfsclient-$name.yaml" <<EOF
mode: "tls"
psk: "${!psk_var}"
local_addr: "127.0.0.1:${!port_var}"
remote_tls_addr: "127.0.0.1:$multi_port"
server_name: "www.example.com"
pinned_cert_sha256: "$pin"
EOF
done

"$work_dir/obfsserver" -config "$work_dir/obfsserver-multi.yaml" >"$work_dir/server-multi.log" 2>&1 &
multi_server_pid=$!
pids+=($multi_server_pid)
sleep 1

for name in alice bob; do
	"$work_dir/obfsclient" -config "$work_dir/obfsclient-$name.yaml" >"$work_dir/client-$name.log" 2>&1 &
	pids+=($!)
done
sleep 1.5

for name in alice bob; do
	port_var="${name}_port"
	[[ "$(python3 "$work_dir/probe.py" "${!port_var}" 256)" == "OK" ]] \
		|| fail "$name could not reach the tunnel with their own credential"
	pass "$name reaches the tunnel with their own credential"
done

# Revoke one client and reload the running server, as an operator would.
"$work_dir/obfsctl" -file "$clients_file" revoke alice >/dev/null
kill -HUP $multi_server_pid
sleep 1
grep -q "credentials reloaded" "$work_dir/server-multi.log" \
	|| fail "the server did not report reloading its credentials"
pass "server reloaded credentials on SIGHUP without restarting"

# Bob must be undisturbed: this is the entire reason per-client
# credentials exist.
[[ "$(python3 "$work_dir/probe.py" "$bob_port" 256)" == "OK" ]] \
	|| fail "revoking alice cut off bob"
pass "revoking one client left the other connected"

# Alice must not be able to come back.
cat > "$work_dir/obfsclient-alice2.yaml" <<EOF
mode: "tls"
psk: "$alice_psk"
local_addr: "127.0.0.1:51913"
remote_tls_addr: "127.0.0.1:$multi_port"
server_name: "www.example.com"
pinned_cert_sha256: "$pin"
EOF
"$work_dir/obfsclient" -config "$work_dir/obfsclient-alice2.yaml" >"$work_dir/client-alice2.log" 2>&1 &
pids+=($!)
sleep 1.5
if [[ "$(python3 "$work_dir/probe.py" 51913 256 2>/dev/null)" == "OK" ]]; then
	fail "a revoked client reconnected successfully"
fi
pass "a revoked client cannot reconnect"

"$work_dir/obfsctl" -file "$clients_file" list | grep -q "alice.*revoked" \
	|| fail "obfsctl list does not show the revocation"
pass "obfsctl list reports who has access"

# Alice's tunnel was still open and carrying traffic when she was
# revoked. The check used to sit in a read-timeout branch, so a busy
# session was never re-checked and ran on indefinitely — which is the
# case revocation exists for. Bob stays busy here too, to be sure the
# cut is aimed at one client and not at whoever happens to be talking.
alice_still_up=""
for _ in $(seq 1 20); do
	if [[ "$(python3 "$work_dir/probe.py" "$alice_port" 256 2>/dev/null)" != "OK" ]]; then
		alice_still_up="no"
		break
	fi
	alice_still_up="yes"
	sleep 0.5
done
[[ "$alice_still_up" == "no" ]] || fail "a revoked client's open session kept carrying traffic"
pass "revoking a client cuts the session it already had open"

[[ "$(python3 "$work_dir/probe.py" "$bob_port" 256)" == "OK" ]] \
	|| fail "cutting alice's session also cut bob's"
pass "the cut was aimed at the revoked client only"

# Revoking the last client used to be refused as an invalid credential
# file, so the server kept the previous list and the device being cut
# off went on working while the log said only that a reload had failed.
"$work_dir/obfsctl" -file "$clients_file" revoke bob >/dev/null
kill -HUP $multi_server_pid
sleep 1
grep -q "NO CLIENTS ARE ENABLED" "$work_dir/server-multi.log" \
	|| fail "revoking the last client did not take effect"
pass "revoking the last client is honoured, not refused"

if [[ "$(python3 "$work_dir/probe.py" "$bob_port" 256 2>/dev/null)" == "OK" ]]; then
	sleep 6
	if [[ "$(python3 "$work_dir/probe.py" "$bob_port" 256 2>/dev/null)" == "OK" ]]; then
		fail "the last client kept tunnelling after being revoked"
	fi
fi
pass "the last client stops tunnelling once revoked"

##############################################################################
echo "==> connection profiles"
##############################################################################
# One artifact instead of three files and a fingerprint to retype. Every
# value copied by hand is a value that can be copied wrong, and a wrong
# pin or MTU does not look like a mistake — it looks like a server that
# is down.
profile_clients="$work_dir/profile-clients.yaml"
profile_wire_port=51930
profile_tls_port=51931
profile_local_port=51932

"$work_dir/obfsctl" -file "$profile_clients" add carol >/dev/null

python3 "$work_dir/echo_peer.py" "$profile_wire_port" &
pids+=($!)

cat > "$work_dir/carol-wg.conf" <<EOF
[Interface]
PrivateKey = $(openssl rand -base64 32)
Address = 10.9.0.2/32
DNS = 1.1.1.1
MTU = 1376

[Peer]
PublicKey = $(openssl rand -base64 32)
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 127.0.0.1:$profile_local_port
PersistentKeepalive = 25
EOF

"$work_dir/obfsctl" -file "$profile_clients" profile carol \
	-wg "$work_dir/carol-wg.conf" \
	-endpoint "127.0.0.1:$profile_tls_port" \
	-mode tls -pin "$pin" -sni "www.example.com" \
	-name "E2E" -out "$work_dir/carol.profile" >/dev/null 2>&1 \
	|| fail "obfsctl could not build a profile"

grep -q '^obfsvpn://v1/' "$work_dir/carol.profile" || fail "the profile is not a link"
[[ "$(stat -c '%a' "$work_dir/carol.profile")" == "600" ]] \
	|| fail "the profile holds this device's keys and must not be world-readable"
pass "obfsctl built a profile the client can import"

cat > "$work_dir/obfsserver-profile.yaml" <<EOF
mode: "tls"
clients_file: "$profile_clients"
local_addr: "127.0.0.1:$profile_wire_port"
listen_tls_addr: "127.0.0.1:$profile_tls_port"
cert_file: "$work_dir/server.crt"
key_file: "$work_dir/server.key"
EOF
"$work_dir/obfsserver" -config "$work_dir/obfsserver-profile.yaml" >"$work_dir/server-profile.log" 2>&1 &
pids+=($!)
sleep 1

"$work_dir/obfsclient" -profile "$work_dir/carol.profile" \
	-write-wireguard "$work_dir/carol-out.conf" >"$work_dir/client-profile.log" 2>&1 &
pids+=($!)
sleep 1.5

[[ "$(python3 "$work_dir/probe.py" "$profile_local_port" 1200)" == "OK" ]] \
	|| fail "a client configured only from a profile could not tunnel"
pass "a client configured only from a profile carries traffic"

grep -q "MTU = 1376" "$work_dir/carol-out.conf" \
	|| fail "the written WireGuard config lost its MTU"
grep -q "Endpoint = 127.0.0.1:$profile_local_port" "$work_dir/carol-out.conf" \
	|| fail "the written WireGuard config does not point at the local obfuscator"
pass "the client wrote a WireGuard config pointing at the obfuscator, not the server"

# A damaged link must be refused rather than half-applied.
python3 - "$work_dir/carol.profile" "$work_dir/tampered.profile" <<'PY2'
import sys
text = open(sys.argv[1]).read().strip()
body = list(text[len("obfsvpn://v1/"):])
body[30] = "A" if body[30] != "A" else "B"
open(sys.argv[2], "w").write("obfsvpn://v1/" + "".join(body))
PY2
if "$work_dir/obfsclient" -profile "$work_dir/tampered.profile" >"$work_dir/tampered.log" 2>&1; then
	fail "a damaged profile was accepted"
fi
grep -qi "not a profile" "$work_dir/tampered.log" \
	|| fail "a damaged profile was refused without saying why"
pass "a damaged profile is refused with a clear message"

# A pin in udp mode is a protection nobody is running.
if "$work_dir/obfsctl" -file "$profile_clients" profile carol \
	-wg "$work_dir/carol-wg.conf" -endpoint "127.0.0.1:$profile_tls_port" \
	-mode udp -pin "$pin" >/dev/null 2>&1; then
	fail "a udp profile carrying a certificate pin was built"
fi
pass "a profile that would mislead its owner about pinning is refused"

##############################################################################
echo "==> metrics"
##############################################################################
metrics_port=51920
metrics_wire_port=51921
metrics_client_port=51922
metrics_psk="$(openssl rand -base64 32)"

cat > "$work_dir/obfsserver-metrics.yaml" <<EOF
mode: "udp"
psk: "$metrics_psk"
local_addr: "127.0.0.1:$wg_port"
listen_wire_addr: "127.0.0.1:$metrics_wire_port"
metrics_addr: "127.0.0.1:$metrics_port"
EOF

cat > "$work_dir/obfsclient-metrics.yaml" <<EOF
mode: "udp"
psk: "$metrics_psk"
local_addr: "127.0.0.1:$metrics_client_port"
remote_wire_addr: "127.0.0.1:$metrics_wire_port"
EOF

"$work_dir/obfsserver" -config "$work_dir/obfsserver-metrics.yaml" >"$work_dir/server-metrics.log" 2>&1 &
metrics_server_pid=$!
pids+=($metrics_server_pid)
"$work_dir/obfsclient" -config "$work_dir/obfsclient-metrics.yaml" >"$work_dir/client-metrics.log" 2>&1 &
pids+=($!)
sleep 1

[[ "$(python3 "$work_dir/probe.py" "$metrics_client_port" 512)" == "OK" ]] \
	|| fail "the tunnel did not carry traffic in the metrics run"

scrape="$(curl -fsS "http://127.0.0.1:$metrics_port/metrics")" \
	|| fail "the metrics endpoint did not respond"
grep -q "^obfsvpn_sessions_active 1$" <<<"$scrape" || fail "sessions_active is not 1: $(grep obfsvpn_sessions_active <<<"$scrape")"
grep -q "^obfsvpn_bytes_received_total 512$" <<<"$scrape" || fail "byte counter is wrong: $(grep bytes_received <<<"$scrape")"
pass "metrics endpoint reports live traffic accurately"

curl -fsS "http://127.0.0.1:$metrics_port/healthz" >/dev/null || fail "healthz did not respond"
pass "health endpoint responds"

# Privacy default: without per_client_metrics the server must not publish
# per-device usage, which would be a usage log in all but name.
if grep -q "obfsvpn_client_" <<<"$scrape"; then
	fail "per-client usage counters were published without being enabled"
fi
pass "no per-client usage is published unless enabled"

kill -TERM $metrics_server_pid 2>/dev/null || true

##############################################################################
echo "==> WireGuard provisioning"
##############################################################################
# The generated server config is checked for the two properties that are
# easy to get wrong by hand and fatal when wrong: WireGuard reachable only
# over loopback, and an MTU that leaves room for the wrapper. Both fail
# silently in production — the tunnel keeps working while the protection
# is gone — so they are asserted here rather than left to review.
provision="$repo_dir/deploy/provision-wireguard.sh"
server_cfg="$("$provision" init --dry-run 2>/dev/null)"

grep -q "MTU = 1376" <<<"$server_cfg" \
	|| fail "the generated WireGuard config does not set the MTU the obfuscator needs"
pass "generated server config sets MTU 1376"

grep -q -- "--dport 51821 ! -s 127.0.0.1 -j DROP" <<<"$server_cfg" \
	|| fail "the generated config does not block outside traffic to the WireGuard port"
pass "generated server config blocks direct access to WireGuard"

grep -q "MASQUERADE" <<<"$server_cfg" \
	|| fail "the generated config gives tunnelled clients no route out"
pass "generated server config routes client traffic out"

client_cfg="$("$provision" add-client testclient vpn.example.com --dry-run 2>/dev/null)"
grep -q "MTU = 1376" <<<"$client_cfg" \
	|| fail "the generated client config does not match the server MTU"
grep -q "Endpoint = 127.0.0.1:51821" <<<"$client_cfg" \
	|| fail "the client config points WireGuard somewhere other than the local obfuscator"
pass "generated client config points WireGuard at the local obfuscator"

# A client config that named the server directly would bypass the tunnel
# entirely while looking like it worked.
if grep -qE "^Endpoint = vpn\.example\.com" <<<"$client_cfg"; then
	fail "the client config sends WireGuard straight to the server, bypassing the obfuscator"
fi
pass "generated client config does not bypass the obfuscator"

##############################################################################
echo "==> packaging and config validation"
##############################################################################
# -check does the same loading a real start does, then exits. It is what
# an operator runs before restarting a server that is carrying traffic.
if ! "$work_dir/obfsserver" -config "$work_dir/obfsserver.yaml" -check >"$work_dir/check-ok.log" 2>&1; then
	fail "-check rejected a configuration that works: $(cat "$work_dir/check-ok.log")"
fi
grep -q "configuration is valid" "$work_dir/check-ok.log" || fail "-check gave no verdict"
pass "-check accepts a working configuration"

cat > "$work_dir/broken-clients.yaml" <<'BROKEN'
clients:
  - id: a
    psk: "not base64 at all!!"
BROKEN
cat > "$work_dir/obfsserver-broken.yaml" <<EOF
mode: "udp"
clients_file: "$work_dir/broken-clients.yaml"
local_addr: "127.0.0.1:$wg_port"
listen_wire_addr: "127.0.0.1:51930"
EOF
if "$work_dir/obfsserver" -config "$work_dir/obfsserver-broken.yaml" -check >"$work_dir/check-bad.log" 2>&1; then
	fail "-check accepted a credential file with an unusable key"
fi
pass "-check catches a broken credential file before a restart would"

# The container image builds from a static binary on a base with no shell
# and no libc; if CGO ever crept back in, the image would not run at all.
for binary in obfsserver obfsclient gencert obfsctl; do
	CGO_ENABLED=0 go build -trimpath -o "$work_dir/static-$binary" "./cmd/$binary" \
		|| fail "$binary does not build without cgo, so the scratch image would not run"
done
pass "all binaries build statically for the container image"

if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
	if docker build -q -t obfsvpn:e2e "$repo_dir" >"$work_dir/docker-build.log" 2>&1; then
		pass "container image builds"
	else
		tail -20 "$work_dir/docker-build.log" >&2
		fail "container image failed to build"
	fi
else
	echo "  SKIP: no Docker daemon here, so the image build is unverified"
fi

##############################################################################
echo "==> real WireGuard integration"
##############################################################################
# Separate module: it runs a genuine WireGuard implementation in userspace
# (wireguard-go + gVisor netstack), which the production module must not
# depend on. Skipped when its dependencies are unavailable offline.
if (cd "$repo_dir/test/wireguard" && go test -count=1 -short ./... >"$work_dir/wg-integration.log" 2>&1); then
	pass "real WireGuard carries traffic through both transports"
else
	if grep -qiE "cannot find module|no required module|dial tcp|proxyconnect" "$work_dir/wg-integration.log"; then
		echo "  SKIP: real-WireGuard tests need network access to fetch wireguard-go"
	else
		echo "--- test/wireguard output ---" >&2
		tail -30 "$work_dir/wg-integration.log" >&2
		fail "real WireGuard could not use the tunnel"
	fi
fi

echo
echo "All end-to-end checks passed."
