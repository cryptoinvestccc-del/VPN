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

# Google's aapt2 from toolchain.sh; Debian's cannot read android-36.jar.
AAPT2="$tools/aapt2"
[[ -x "$AAPT2" ]] || { echo "missing aapt2 — run ./android/toolchain.sh" >&2; exit 1; }
TARGET_SDK=36

rm -rf "$work"
mkdir -p "$work"/{classes,gen,res}

echo "==> resources"
"$AAPT2" compile --dir "$app/res" -o "$work/res.zip"
"$AAPT2" link \
	-o "$work/base.apk" \
	-I "$tools/android.jar" \
	--manifest "$app/AndroidManifest.xml" \
	--java "$work/gen" \
	--min-sdk-version 26 \
	--target-sdk-version $TARGET_SDK \
	"$work/res.zip"
# The same resources in the protobuf form an app bundle carries.
"$AAPT2" link --proto-format \
	-o "$work/proto.apk" \
	-I "$tools/android.jar" \
	--manifest "$app/AndroidManifest.xml" \
	--min-sdk-version 26 \
	--target-sdk-version $TARGET_SDK \
	"$work/res.zip"

echo "==> java"
# Release 8 is not a preference: the dexer refuses class files newer than
# version 52, and it is the only dexer reachable here.
javac -nowarn -Xlint:-options \
	--release 8 \
	-cp "$tools/android.jar" \
	-d "$work/classes" \
	$(find "$app/src" "$work/gen" -name '*.java')

# The app installs on Android 8 (API 26) and is compiled against 16.
# Compiling it again against 26 fails on any call Android 8 does not
# have — the crash that would otherwise be found on somebody's phone.
if [[ -f "$tools/android-26.jar" ]]; then
	echo "==> minimum-version check (API 26)"
	rm -rf "$work/api26" && mkdir -p "$work/api26"
	javac -nowarn -Xlint:-options --release 8 \
		-cp "$tools/android-26.jar" \
		-d "$work/api26" \
		$(find "$app/src" "$work/gen" -name '*.java') || {
		echo "the app calls something Android 8 does not have (above); guard it with Build.VERSION.SDK_INT" >&2
		exit 1
	}
else
	echo "==> minimum-version check skipped: run ./toolchain.sh to fetch android-26.jar" >&2
fi

echo "==> dex"
( cd "$work/classes" && java -cp "$tools/dalvik-dx.jar" com.android.dx.command.Main \
	--dex --output="$work/classes.dex" . )

echo "==> package"
cp "$work/base.apk" "$work/unsigned.apk"
( cd "$work" && zip -q unsigned.apk classes.dex )

# The engine is built here rather than kept around as a file, because a
# checked-in binary goes stale the moment its source changes and says
# nothing about it. That happened: a fix to the engine was written,
# committed, packaged and installed, and the APK still carried the
# build from before it.
#
# CGO off, so there is nothing to link against and no NDK to find.
echo "==> engine"
for pair in arm64-v8a:arm64: armeabi-v7a:arm:7; do
	abi="${pair%%:*}"; rest="${pair#*:}"
	goarch="${rest%%:*}"; goarm="${rest#*:}"
	mkdir -p "$app/jni/$abi"
	( cd "$here/awg" && CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
		go build -trimpath -ldflags='-s -w' -o "$app/jni/$abi/libawg.so" . )
	echo "    $abi $(stat -c%s "$app/jni/$abi/libawg.so") bytes"
done

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
# All three schemes. v1 is not needed above API 24 and costs a little
# size, but some vendor installers still look for it and refuse what
# they cannot find, which shows up as "not installed" and no reason.
apksigner sign \
	--ks "$here/.debug.keystore" --ks-pass pass:android --key-pass pass:android \
	--v1-signing-enabled true \
	--v2-signing-enabled true --v3-signing-enabled true \
	--out "$out" "$work/aligned.apk"

echo "==> verify"
apksigner verify --print-certs "$out" 2>/dev/null | head -2
echo
echo "$(basename "$out")  $(stat -c%s "$out") bytes"
"$AAPT2" dump badging "$out" 2>/dev/null | head -3

# ---- The app bundle Google Play takes ---------------------------------
#
# The base module is the proto resources, the dex and the engines laid
# out the way bundletool expects. Native libraries stay compressed in the
# APKs Play builds from it, so Android unpacks them at install time: the
# engine is started as a program from nativeLibraryDir, and a library
# left inside the APK (Play's default for bundles) would not be there.
#
# Signed with the upload key when BESY_UPLOAD_KEYSTORE and
# BESY_UPLOAD_PASSWORD are set. Without them the bundle is signed with the
# debug key and named so; never upload that one: the first bundle Play
# receives decides which key every later upload must be signed with.
if [[ -f "$tools/bundletool.jar" ]]; then
	echo "==> app bundle"
	rm -rf "$work/aab" && mkdir -p "$work/aab/base"
	( cd "$work/aab/base" && unzip -q "$work/proto.apk" \
		&& mkdir manifest dex && mv AndroidManifest.xml manifest/ \
		&& cp "$work/classes.dex" dex/ && { [[ -d "$work/lib" ]] && cp -r "$work/lib" . || true; } \
		&& zip -qr ../base.zip . )
	printf '%s' '{"optimizations":{"uncompressNativeLibraries":{"enabled":false}}}' > "$work/aab/config.json"
	if [[ -n "${BESY_UPLOAD_KEYSTORE:-}" && -n "${BESY_UPLOAD_PASSWORD:-}" ]]; then
		aab="${out%.apk}.aab"
		ks="$BESY_UPLOAD_KEYSTORE"; kp="$BESY_UPLOAD_PASSWORD"; alias="${BESY_UPLOAD_ALIAS:-besy-upload}"
	else
		aab="${out%.apk}-debug.aab"
		ks="$here/.debug.keystore"; kp="android"; alias="besy-debug"
	fi
	rm -f "$aab"
	java -jar "$tools/bundletool.jar" build-bundle --modules="$work/aab/base.zip" \
		--config="$work/aab/config.json" --output="$aab"
	jarsigner -sigalg SHA256withRSA -digestalg SHA-256 -keystore "$ks" \
		-storepass "$kp" -keypass "$kp" "$aab" "$alias" >/dev/null
	java -jar "$tools/bundletool.jar" validate --bundle="$aab" >/dev/null
	echo "$(basename "$aab")  $(stat -c%s "$aab") bytes, signed with $alias"
fi
