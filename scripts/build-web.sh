#!/usr/bin/env bash
# Builds the landing page and the binary that serves it.
#
# The two steps are separate on purpose. `npm run build` produces the
# assets; copying them under internal/webui/ and building with -tags webui
# compiles them into the binary, so the site deploys as one file with
# nothing beside it — the same property the rest of the project's binaries
# have.
#
# Without the tag, `go build ./...` needs no Node toolchain at all, which
# is what keeps CI and a plain source checkout working.
#
# Usage: ./scripts/build-web.sh [output-binary]
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${1:-$repo_dir/dist/obfsweb}"

cd "$repo_dir/web"
if [[ ! -d node_modules ]]; then
	npm ci
fi
npm run build

# Precompress what compresses. These files are built once and never
# change, so paying gzip -9 here costs nothing per request and saves
# every first-time visitor about two thirds of the transfer. The server
# picks the .gz up on its own when the client accepts it; when it is
# missing it just serves the original.
#
# -n keeps the source name and timestamp out of the archive, so two
# builds of the same assets produce byte-identical files.
find "$repo_dir/web/dist" -type f \
	\( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.svg' \
	   -o -name '*.json' -o -name '*.map' -o -name '*.txt' \) \
	-size +1k -print0 |
	while IFS= read -r -d '' file; do
		gzip -9 -n -k -f "$file"
	done

# A stale asset from an earlier build would be embedded and served
# forever, so the target is replaced rather than merged into.
rm -rf "$repo_dir/internal/webui/dist"
cp -r "$repo_dir/web/dist" "$repo_dir/internal/webui/dist"

cd "$repo_dir"
version="$(git describe --tags --always --dirty 2>/dev/null || echo unknown)"
mkdir -p "$(dirname "$out")"
CGO_ENABLED=0 go build -tags webui -trimpath \
	-ldflags "-s -w -X main.version=$version" \
	-o "$out" ./cmd/obfsweb

echo
echo "Built $out (assets embedded, version $version)"
echo "Run it with: $out -addr 127.0.0.1:8080"
