#!/usr/bin/env bash
# Builds and signs the APK without any part of Google's SDK, which this
# environment cannot reach. Run ./toolchain.sh first.
#
# Usage: ./android/build.sh [output.apk]
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tools="${ANDROID_TOOLCHAIN_DIR:-$here/.toolchain}"
app="$here/app"
work="$here/.build"
out="${1:-$here/besy.apk}"

# JAVA_TOOL_OPTIONS is set in some environments and prints a banner on
# stderr that hides the first line of any real error.
unset JAVA_TOOL_OPTIONS

for f in "$tools/android.jar" "$tools/dalvik-dx.jar"; do
	[[ -f "$f" ]] || { echo "missing $(basename "$f") — run ./android/toolchain.sh" >&2; exit 1; }
done

rm -rf "$work"
mkdir -p "$work"/{classes,gen,res}

echo "==> resources"
aapt2 compile --dir "$app/res" -o "$work/res.zip"
aapt2 link \
	-o "$work/base.apk" \
	-I "$tools/android.jar" \
	--manifest "$app/AndroidManifest.xml" \
	--java "$work/gen" \
	--min-sdk-version 31 \
	--target-sdk-version 34 \
	"$work/res.zip"

echo "==> java"
# Release 8 is not a preference: the dexer refuses class files newer than
# version 52, and it is the only dexer reachable here.
javac -nowarn -Xlint:-options \
	--release 8 \
	-cp "$tools/android.jar" \
	-d "$work/classes" \
	$(find "$app/src" "$work/gen" -name '*.java')

echo "==> dex"
( cd "$work/classes" && java -cp "$tools/dalvik-dx.jar" com.android.dx.command.Main \
	--dex --output="$work/classes.dex" . )

echo "==> package"
cp "$work/base.apk" "$work/unsigned.apk"
( cd "$work" && zip -q unsigned.apk classes.dex )

# The tunnel engine travels as lib/<abi>/libawg.so. Android unpacks that
# directory to somewhere an app may still execute from; the data
# directory, where an asset would land, has been refused since API 29.
if [[ -d "$app/jni" ]]; then
	mkdir -p "$work/lib"
	cp -r "$app/jni/." "$work/lib/"
	( cd "$work" && zip -qr unsigned.apk lib )
	echo "    engines: $(cd "$work/lib" && ls | tr '\n' ' ')"
fi

# A debug key, generated once and kept out of git. A release build signs
# with a key the operator holds; see README.
if [[ ! -f "$here/.debug.keystore" ]]; then
	echo "==> debug key"
	keytool -genkeypair -keystore "$here/.debug.keystore" \
		-storepass android -keypass android -alias besy-debug \
		-dname "CN=BESY Debug" -keyalg RSA -keysize 2048 -validity 10000 >/dev/null 2>&1
fi

echo "==> align and sign"
zipalign -f 4 "$work/unsigned.apk" "$work/aligned.apk"
apksigner sign \
	--ks "$here/.debug.keystore" --ks-pass pass:android --key-pass pass:android \
	--v2-signing-enabled true --v3-signing-enabled true \
	--out "$out" "$work/aligned.apk"

echo "==> verify"
apksigner verify --print-certs "$out" 2>/dev/null | head -2
echo
echo "$(basename "$out")  $(stat -c%s "$out") bytes"
aapt dump badging "$out" 2>/dev/null | head -3
