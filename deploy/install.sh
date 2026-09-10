#!/usr/bin/env bash
# Installs obfsserver or obfsclient as a systemd service.
#
# Usage:
#   sudo ./deploy/install.sh server   # on the VPS, next to the real WG server
#   sudo ./deploy/install.sh client   # on the client machine
#
# Builds the binary from this repo, installs it to /usr/local/bin, creates
# an unprivileged "obfsvpn" system user, and lays down /etc/obfsvpn with a
# config template (root:obfsvpn, 0640 — it holds the PSK) if one isn't
# already there. Does NOT overwrite an existing config.
set -euo pipefail

role="${1:-}"
if [[ "$role" != "server" && "$role" != "client" ]]; then
	echo "usage: $0 <server|client>" >&2
	exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
	echo "run as root (sudo $0 $role)" >&2
	exit 1
fi

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin_name="obfs${role}"
config_dir="/etc/obfsvpn"
config_path="${config_dir}/${bin_name}.yaml"

echo "==> building ${bin_name}"
( cd "$repo_dir" && go build -o "/usr/local/bin/${bin_name}" "./cmd/${bin_name}" )

if ! id -u obfsvpn >/dev/null 2>&1; then
	echo "==> creating system user obfsvpn"
	useradd --system --no-create-home --shell /usr/sbin/nologin obfsvpn
fi

mkdir -p "$config_dir"
chown root:obfsvpn "$config_dir"
chmod 750 "$config_dir"

if [[ ! -f "$config_path" ]]; then
	echo "==> installing config template (EDIT THIS before starting the service)"
	cp "${repo_dir}/examples/${bin_name}.yaml" "$config_path"
	chown root:obfsvpn "$config_path"
	chmod 640 "$config_path"
else
	echo "==> config already exists at ${config_path}, leaving it alone"
fi

echo "==> installing systemd unit"
cp "${repo_dir}/deploy/systemd/${bin_name}.service" "/etc/systemd/system/${bin_name}.service"
systemctl daemon-reload

cat <<EOF

Done. Before starting:
  1. Edit ${config_path} — set a real PSK (openssl rand -base64 32) and
     the correct addresses. For TLS mode, see examples/${bin_name}-tls.yaml
     and run: go run ./cmd/gencert (server side only).
  2. Enable + start: systemctl enable --now ${bin_name}

Logs: journalctl -u ${bin_name} -f
EOF
