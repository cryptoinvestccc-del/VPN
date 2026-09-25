#!/usr/bin/env bash
# Fetches the pieces of an Android toolchain that are not on dl.google.com,
# which this environment cannot reach. See README.md for what replaces what.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tools="${ANDROID_TOOLCHAIN_DIR:-$here/.toolchain}"
mkdir -p "$tools"

# Pinned, because a build that silently changes its compiler is not a
# reproducible build.
ANDROID_JAR_URL="https://raw.githubusercontent.com/Sable/android-platforms/master/android-34/android.jar"
# The oldest Android the app installs on. Compiling against it as well is
# the check that nothing newer is called: javac against API 34 accepts a
# call that does not exist on Android 8 and the phone finds out instead.
ANDROID_MIN_JAR_URL="https://raw.githubusercontent.com/Sable/android-platforms/master/android-26/android.jar"
DX_VERSION="16.0.1"
DX_URL="https://repo1.maven.org/maven2/com/jakewharton/android/repackaged/dalvik-dx/${DX_VERSION}/dalvik-dx-${DX_VERSION}.jar"

fetch() { # url dest minimum-bytes
	local url="$1" dest="$2" min="$3"
	if [[ -f "$dest" && $(stat -c%s "$dest") -ge $min ]]; then
		echo "  have $(basename "$dest")"
		return
	fi
	# Maven Central rate-limits shared addresses with 429, which is not a
	# failure worth aborting a build over; back off and try again.
	local delay=4
	for attempt in 1 2 3 4 5; do
		if curl -sSL --fail -o "$dest" "$url" && [[ $(stat -c%s "$dest") -ge $min ]]; then
			echo "  fetched $(basename "$dest") ($(stat -c%s "$dest") bytes)"
			return
		fi
		echo "  attempt $attempt failed, waiting ${delay}s" >&2
		sleep $delay
		delay=$((delay * 2))
	done
	echo "could not fetch $url" >&2
	exit 1
}

echo "==> system build tools"
missing=()
for t in aapt2 apksigner zipalign javac; do
	command -v "$t" >/dev/null || missing+=("$t")
done
if ((${#missing[@]})); then
	echo "missing: ${missing[*]}" >&2
	echo "install them with: apt-get install -y android-sdk-build-tools default-jdk" >&2
	exit 1
fi
echo "  aapt2, apksigner, zipalign, javac present"

echo "==> android.jar (API 34)"
fetch "$ANDROID_JAR_URL" "$tools/android.jar" 20000000

echo "==> android.jar (API 26, for the minimum-version check)"
fetch "$ANDROID_MIN_JAR_URL" "$tools/android-26.jar" 20000000

echo "==> dexer"
fetch "$DX_URL" "$tools/dalvik-dx.jar" 500000

echo "==> checking the dexer runs"
printf 'public class Probe { public static void main(String[] a) {} }\n' > "$tools/Probe.java"
( cd "$tools" && javac -nowarn --release 8 Probe.java \
	&& java -cp dalvik-dx.jar com.android.dx.command.Main --dex --output=probe.dex Probe.class ) >/dev/null 2>&1
[[ -s "$tools/probe.dex" ]] || { echo "the dexer did not produce a dex file" >&2; exit 1; }
rm -f "$tools/Probe.java" "$tools/Probe.class" "$tools/probe.dex"
echo "  dexer works"

echo
echo "Toolchain ready in $tools"
