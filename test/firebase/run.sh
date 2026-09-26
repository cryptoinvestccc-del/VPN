#!/usr/bin/env bash
# Runs the APK on Firebase Test Lab devices and downloads what came back:
# screenshots of every screen the robo crawler reached, a video, logcat.
#
# Needs FIREBASE_SA_JSON in the environment: the whole JSON key of a
# service account in the Firebase project, set in the cloud environment's
# settings (never pasted into a chat). A new session picks it up.
#
#   ./test/firebase/run.sh [path/to.apk]
#
# Devices: TL_DEVICES, space-separated gcloud --device specs. The default
# is one Android 11 phone and one Android 14 phone, ARM, because the
# engine in the APK is ARM-only. `gcloud firebase test android models
# list` shows what the project can use.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$here/../.."
apk="${1:-}"
out="$here/results"
gdir="${GCLOUD_DIR:-$HOME/google-cloud-sdk}"

[[ -n "${FIREBASE_SA_JSON:-}" ]] || {
	echo "FIREBASE_SA_JSON is not set: add the service account key in the environment's settings and start a new session" >&2
	exit 1
}

if [[ -z "$apk" ]]; then
	echo "==> building the APK"
	"$root/android/toolchain.sh" >/dev/null
	apk="$root/android/besy-test.apk"
	"$root/android/build.sh" "$apk" | tail -1
fi

if [[ ! -x "$gdir/bin/gcloud" ]]; then
	echo "==> installing the gcloud CLI"
	curl -sSfL -o /tmp/gcloud.tgz \
		https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-linux-x86_64.tar.gz
	tar -xzf /tmp/gcloud.tgz -C "$(dirname "$gdir")"
fi
export PATH="$gdir/bin:$PATH"
export CLOUDSDK_CORE_DISABLE_PROMPTS=1

key="$(mktemp)"
trap 'rm -f "$key"' EXIT
printf '%s' "$FIREBASE_SA_JSON" > "$key"
project="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["project_id"])' "$key")"

echo "==> signing in as the service account for $project"
gcloud auth activate-service-account --key-file="$key" >/dev/null
gcloud config set project "$project" >/dev/null
gcloud services enable testing.googleapis.com toolresults.googleapis.com >/dev/null

devices="${TL_DEVICES:-model=MediumPhone.arm,version=30,locale=ru,orientation=portrait model=MediumPhone.arm,version=34,locale=ru,orientation=portrait}"
args=()
for d in $devices; do args+=(--device "$d"); done

stamp="besy-$(date -u +%Y%m%d-%H%M%S)"
echo "==> running on: $devices"
gcloud firebase test android run --type robo --app "$apk" \
	"${args[@]}" --timeout 300s --results-dir "$stamp" 2>&1 | tee "$here/last-run.log" || true

# gcloud prints the bucket either as gs://… or as a console link
bucket="$(grep -o 'gs://[^/ ]*' "$here/last-run.log" | head -1 || true)"
[[ -n "$bucket" ]] || bucket="$(grep -o 'storage/browser/[^/ ]*' "$here/last-run.log" | head -1 | sed 's|storage/browser/|gs://|' || true)"
if [[ -n "$bucket" ]]; then
	echo "==> downloading results into $out/$stamp"
	mkdir -p "$out"
	gsutil -m -q cp -r "$bucket/$stamp" "$out/" || true
	find "$out/$stamp" -name '*.png' | sort | head -40
fi
