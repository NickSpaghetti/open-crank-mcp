#!/usr/bin/env bash
# Checks the two ways Agent Plugins can move out from under the pin: the schemas
# being edited where they stand, and a newer spec version being published beside
# them. Both are the same news - upstream moved, go look - so they are one script
# and one job, reporting every finding rather than stopping at the first.
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
readonly MANIFEST="$SCRIPT_DIR/../plugin/plugin.json"
readonly SPEC_API=https://api.github.com/repos/agentplugins/agent-plugins-spec/contents/spec
readonly SPEC_RAW=https://raw.githubusercontent.com/agentplugins/agent-plugins-spec/main/spec

# What a published spec document says about itself. Upstream cuts no tags and no
# releases, so this line is the only thing separating a published version from a
# working draft sitting in the same directory.
readonly PUBLISHED='**Status: Published**'

# The pinned version is read out of the $schema URL rather than written down here,
# so there is one copy of it and bumping the manifest moves every check with it.
schema=$(jq -er '."$schema"' "$MANIFEST") || {
  echo "plugin-upstream-check: no \$schema in plugin/plugin.json" >&2
  exit 1
}
pinned=$(echo "$schema" | cut -d/ -f5)
case $pinned in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "plugin-upstream-check: cannot read a version out of $schema" >&2; exit 1 ;;
esac

vendored="$SCRIPT_DIR/../plugin/schemas/$pinned"
base="https://agent-plugins.org/schemas/$pinned"

tmp=$(mktemp -d) || exit 1
trap 'rm -rf "$tmp"' EXIT

status=0

# 1. The vendored schemas against the live ones.
for schema_name in plugin mcp; do
  if ! curl -fsSL --max-time 30 "$base/$schema_name.schema.json" -o "$tmp/$schema_name.schema.json"; then
    echo "plugin-upstream-check: could not fetch $base/$schema_name.schema.json" >&2
    status=1
    continue
  fi
  if diff -u "$vendored/$schema_name.schema.json" "$tmp/$schema_name.schema.json"; then
    echo "plugin-upstream-check: $schema_name.schema.json unchanged"
  else
    echo "plugin-upstream-check: $schema_name.schema.json has changed upstream" >&2
    status=1
  fi
done

# 2. The pinned document still says what this script looks for. Without this the
# version scan below fails open: reword the status line upstream and every
# release reads as an unpublished draft forever, silently.
if ! pinned_doc=$(curl -fsSL --max-time 30 "$SPEC_RAW/$pinned.md"); then
  echo "plugin-upstream-check: could not fetch $SPEC_RAW/$pinned.md" >&2
  status=1
elif ! echo "$pinned_doc" | grep -qF "$PUBLISHED"; then
  echo "plugin-upstream-check: $pinned.md no longer contains '$PUBLISHED'" >&2
  echo "  the status convention moved, so the version check below is now blind." >&2
  echo "  read $SPEC_RAW/$pinned.md and update PUBLISHED in this script" >&2
  status=1
fi

# 3. Any published version above the pin.
if ! listing=$(curl -fsSL --max-time 30 "$SPEC_API"); then
  echo "plugin-upstream-check: could not list $SPEC_API" >&2
  exit 1
fi
versions=$(echo "$listing" | jq -r '.[] | select(.type == "file") | .name | sub("\\.md$"; "")')
if [ -z "$versions" ]; then
  echo "plugin-upstream-check: $SPEC_API listed no spec documents" >&2
  exit 1
fi

for version in $versions; do
  [ "$version" = "$pinned" ] && continue
  [ "$(printf '%s\n%s\n' "$pinned" "$version" | sort -V | tail -1)" = "$pinned" ] && continue

  if ! doc=$(curl -fsSL --max-time 30 "$SPEC_RAW/$version.md"); then
    echo "plugin-upstream-check: could not fetch $SPEC_RAW/$version.md" >&2
    status=1
    continue
  fi
  if echo "$doc" | grep -qF "$PUBLISHED"; then
    echo "plugin-upstream-check: $version is published and this plugin pins $pinned" >&2
    echo "  read $SPEC_RAW/$version.md, then move plugin/schemas/ and both \$schema values" >&2
    status=1
  else
    echo "plugin-upstream-check: $version exists but is not published yet"
  fi
done

[ $status -eq 0 ] && echo "plugin-upstream-check: $pinned is current and its schemas are unchanged"
exit $status
