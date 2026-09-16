#!/usr/bin/env bash
# Fetches the live Agent Plugins 1.0.0 schemas and diffs them against the copies
# vendored at plugin/schemas/1.0.0/.
#
# Weekly rather than per-PR, and that placement is the point. A revised upstream
# spec is news about somebody else's release schedule, not about the pull request
# in front of you - failing a PR for it trains people to ignore the check, and
# failing it because agent-plugins.org is briefly down trains them faster. The
# same reasoning scripts/check-doc-links.sh already applies to external URLs.
#
# When this fails, take the new copy and fix whatever it now rejects. Do not edit
# the vendored files to make the diff go away.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly VENDORED="$SCRIPT_DIR/../plugin/schemas/1.0.0"
readonly BASE=https://agent-plugins.org/schemas/1.0.0

tmp=$(mktemp -d) || exit 1
trap 'rm -rf "$tmp"' EXIT

status=0
for schema in plugin mcp; do
  if ! curl -fsSL --max-time 30 "$BASE/$schema.schema.json" -o "$tmp/$schema.schema.json"; then
    echo "plugin-schema-check: could not fetch $BASE/$schema.schema.json" >&2
    status=1
    continue
  fi
  if diff -u "$VENDORED/$schema.schema.json" "$tmp/$schema.schema.json"; then
    echo "plugin-schema-check: $schema.schema.json unchanged"
  else
    echo "plugin-schema-check: $schema.schema.json has changed upstream" >&2
    status=1
  fi
done

exit $status
