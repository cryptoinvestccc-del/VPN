#!/usr/bin/env bash
# Compiles the app's configuration builder for a desktop JVM, so a test
# can run the app's own code rather than a second copy of its logic.
#
# Two substitutions make that possible. android.util.Base64 in
# android.jar is a stub that only throws, so a real one stands in for
# it; org.json is part of Android but not of the JDK, so it comes from
# Maven Central. Nothing else about the class changes.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="$here/../.build-desktop"
jar="$here/../.toolchain/json.jar"

if [[ ! -f "$jar" ]]; then
	echo "==> org.json"
	curl -fsSL -o "$jar" \
		https://repo1.maven.org/maven2/org/json/json/20240303/json-20240303.jar
fi

rm -rf "$out"
mkdir -p "$out"
javac -nowarn -d "$out" -cp "$jar:$here/../.toolchain/android.jar" \
	"$here/android/util/Base64.java" \
	"$here/stub/vpn/besy/R.java" \
	"$here/../app/src/vpn/besy/Uapi.java" \
	"$here/../app/src/vpn/besy/Reply.java" \
	"$here/../app/src/vpn/besy/Provisioning.java" \
	"$here/CrossCheck.java" \
	"$here/ReplyCheck.java"
echo "    classes in ${out#"$here/../"}"
