#!/usr/bin/env bash
# Cross-compiles the client for the platforms people actually carry.
#
# Static, CGO-free binaries: there is no libc to match on an Android
# device, and a VPN client that cannot start because of a glibc version
# is a support burden nobody needs.
#
# Usage: ./scripts/build-release.sh [output-dir]
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${1:-$repo_dir/dist}"
mkdir -p "$out_dir"

# Android is Linux, and a static arm64 binary runs there unmodified —
# which is what makes the Termux route work without an APK.
targets=(
	"linux/amd64"
	"linux/arm64"
	"linux/arm"
	"darwin/amd64"
	"darwin/arm64"
	"windows/amd64"
)

cd "$repo_dir"
version="$(git describe --tags --always --dirty 2>/dev/null || echo unknown)"

for target in "${targets[@]}"; do
	os="${target%%/*}"
	arch="${target##*/}"
	suffix=""
	[[ "$os" == "windows" ]] && suffix=".exe"

	for cmd in obfsclient obfsctl obfsserver gencert; do
		# The server and the credential tool are not shipped for
		# Windows: nothing about the deployment expects it there.
		if [[ "$os" == "windows" && "$cmd" != "obfsclient" ]]; then
			continue
		fi
		name="${cmd}-${os}-${arch}${suffix}"
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
			go build -trimpath -ldflags "-s -w -X main.version=$version" \
			-o "$out_dir/$name" "./cmd/$cmd"
	done
	echo "  built $target"
done

cd "$out_dir"
sha256sum ./* > SHA256SUMS
echo
echo "Binaries and SHA256SUMS are in $out_dir"
echo "Version: $version"
