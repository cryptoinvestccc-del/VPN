#!/usr/bin/env bash
# Installs the provisioning service next to Amnezia, behind HTTPS.
#
# Run this on the VPN server, as root:
#
#   ./install-provision.sh besy.example.com
#
# It puts besy-provision on loopback and Caddy in front of it. Caddy
# obtains and renews the certificate on its own, which is the reason to
# use it here: the alternative is a certificate that expires in ninety
# days and takes the app's ability to hand out configs with it.
set -euo pipefail

domain="${1:-}"
if [[ -z "$domain" ]]; then
	echo "usage: $0 <domain>" >&2
	echo >&2
	echo "The domain must already point at this server: Caddy proves it" >&2
	echo "owns the name by answering on it, and cannot get a certificate" >&2
	echo "until the DNS record resolves here." >&2
	exit 2
fi

[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }

binary=/usr/local/bin/besy-provision
[[ -x "$binary" ]] || { echo "$binary is missing; copy it over first" >&2; exit 1; }

echo "==> checking that Amnezia is reachable"
"$binary" -check -endpoint "${domain}:51820" || {
	echo >&2
	echo "The check failed. It prints the running containers when it" >&2
	echo "cannot find AmneziaWG, which is usually the answer." >&2
	exit 1
}

echo "==> service"
cat > /etc/systemd/system/besy-provision.service <<UNIT
[Unit]
Description=BESY provisioning (issues one AmneziaWG credential per install)
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=simple
SupplementaryGroups=docker
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ExecStart=$binary \\
    -listen 127.0.0.1:9181 \\
    -endpoint ${domain}:51820 \\
    -subnet 10.8.1.0/24 \\
    -trust-forwarded-for \\
    -persist
Restart=always
RestartSec=2s

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now besy-provision

echo "==> caddy"
if ! command -v caddy >/dev/null; then
	apt-get install -y -q debian-keyring debian-archive-keyring apt-transport-https curl gnupg
	curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key \
		| gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
	curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt \
		> /etc/apt/sources.list.d/caddy-stable.list
	apt-get update -qq
	apt-get install -y -q caddy
fi

# Only the issuing path is published. Nothing else on this host should be
# reachable through the name: the health endpoint and anything added
# later stay on loopback unless somebody decides otherwise.
cat > /etc/caddy/Caddyfile <<CADDY
${domain} {
	handle /v1/issue {
		reverse_proxy 127.0.0.1:9181
	}
	handle {
		respond "" 404
	}
}
CADDY
systemctl reload caddy || systemctl restart caddy

echo
echo "==> done"
echo
systemctl is-active --quiet besy-provision && echo "  besy-provision: running" || echo "  besy-provision: NOT running — journalctl -u besy-provision"
systemctl is-active --quiet caddy && echo "  caddy:          running" || echo "  caddy:          NOT running — journalctl -u caddy"
echo
echo "Put this in the app (app/res/values/strings.xml, provision_endpoint):"
echo
echo "    https://${domain}/v1/issue"
echo
echo "Check it answers, from anywhere:"
echo
echo "    curl -s -X POST https://${domain}/v1/issue \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"public_key\":\"'\"\$(wg genkey | wg pubkey)\"'\"}'"
