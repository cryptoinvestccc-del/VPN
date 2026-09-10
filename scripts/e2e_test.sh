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
echo "==> TLS mode (no PSK: key derived from the TLS session)"
##############################################################################
pin="$("$work_dir/gencert" -cn www.example.com \
	-cert "$work_dir/server.crt" -key "$work_dir/server.key" \
	| grep pinned_cert_sha256 | awk '{print $2}')"
[[ -n "$pin" ]] || fail "gencert did not print a certificate pin"
pass "gencert produced a certificate and pin"

cat > "$work_dir/obfsserver-tls.yaml" <<EOF
mode: "tls"
local_addr: "127.0.0.1:$wg_port"
listen_tls_addr: "127.0.0.1:$tls_port"
cert_file: "$work_dir/server.crt"
key_file: "$work_dir/server.key"
EOF

cat > "$work_dir/obfsclient-tls.yaml" <<EOF
mode: "tls"
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

echo
echo "All end-to-end checks passed."
