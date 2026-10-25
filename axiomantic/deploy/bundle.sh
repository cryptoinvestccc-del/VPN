#!/usr/bin/env bash
# Packs everything the server needs into one archive: the panel's program
# (linux/amd64, no Go needed on the server), the site's sources and the
# install script. Usage, from the repository root:
#   axiomantic/deploy/bundle.sh [OUT_DIR]   → OUT_DIR/axm-site-bundle.tar.gz
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$(mkdir -p "${1:-.}" && cd "${1:-.}" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
B="$WORK/axm-site-bundle"

mkdir -p "$B/bin" "$B/site-src" "$B/deploy"
(cd "$ROOT/leads" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$B/bin/axiomantic-leads" .)
cp -a "$ROOT/build.py" "$ROOT/content.py" "$ROOT/cms" "$ROOT/site" "$B/site-src/"
cp -a "$ROOT/deploy/axm-site.service" "$ROOT/deploy/snippet-axm-site.conf" \
	"$ROOT/deploy/axm-site.conf" "$ROOT/deploy/axm-site-test.conf" "$ROOT/deploy/INSTALL.md" "$B/deploy/"
install -m 755 "$ROOT/deploy/axm-site.sh" "$B/axm-site.sh"
find "$B" -name __pycache__ -prune -exec rm -rf {} +

# nothing secret travels in the archive
if grep -rEnI '\bsk-[A-Za-z0-9_-]{16,}|service_role|PRIVATE KEY|private_key_id|[0-9]{8,10}:AA[A-Za-z0-9_-]{30,}|AKIA[0-9A-Z]{16}' "$B"; then
	echo "secret-like string in the bundle" >&2
	exit 1
fi
if grep -aEq 'sk-[A-Za-z0-9_-]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY|[0-9]{8,10}:AA[A-Za-z0-9_-]{30,}|AKIA[0-9A-Z]{16}' "$B/bin/axiomantic-leads"; then
	echo "secret-like string in the program" >&2
	exit 1
fi

tar -C "$WORK" --owner=0 --group=0 -czf "$OUT/axm-site-bundle.tar.gz" axm-site-bundle
(cd "$OUT" && sha256sum axm-site-bundle.tar.gz)
