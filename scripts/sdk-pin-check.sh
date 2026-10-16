#!/usr/bin/env bash
# Fails when PLAYDATE_SDK_VERSION is behind the SDK Panic currently ships. A stale
# pin makes the Simulator show an update modal over its volume slider.
# Weekly, not per-PR: Panic's release schedule shouldn't turn a PR red.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly MAKEFILE="$SCRIPT_DIR/../Makefile"

# Panic's `latest` alias 302s to the real versioned file.
readonly LATEST_URL=https://download.panic.com/playdate_sdk/Linux/PlaydateSDK-latest.tar.gz

pinned=$(awk '$1 == "PLAYDATE_SDK_VERSION" { print $3; n++ } END { exit n == 1 ? 0 : 1 }' \
	"$MAKEFILE") || {
	echo "sdk-pin-check: Makefile has no single PLAYDATE_SDK_VERSION line" >&2
	exit 1
}

resolved=$(curl -sSIL --max-time 30 -o /dev/null -w '%{url_effective}' "$LATEST_URL") || {
	echo "sdk-pin-check: could not reach $LATEST_URL" >&2
	exit 1
}

# .../PlaydateSDK-3.1.2.tar.gz -> 3.1.2
name=${resolved##*/}
latest=${name#PlaydateSDK-}
latest=${latest%.tar.gz}

if [ "$latest" = "latest" ] || [ -z "$latest" ]; then
	echo "sdk-pin-check: $LATEST_URL did not redirect to a versioned file (got $resolved)" >&2
	exit 1
fi

if [ "$pinned" = "$latest" ]; then
	echo "sdk-pin-check: pinned $pinned, and that is current"
	exit 0
fi

echo "sdk-pin-check: pinned $pinned, Panic now ships $latest" >&2
echo "  Bump PLAYDATE_SDK_VERSION and run \`bash scripts/shared-check.sh\` before" >&2
echo "  trusting it. A Simulator release can move the widgets the volume slider" >&2
echo "  scan reads, and sdk-contract-check does not cover that." >&2
exit 1
