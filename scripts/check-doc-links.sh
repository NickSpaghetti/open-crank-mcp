#!/usr/bin/env bash
# Resolves every relative Markdown link in the docs and fails on one that points at a
# file or heading that isn't there.
#
# Why it exists. The README was split into guides/, and a link that survives a move
# looks identical to one that doesn't - both are blue, and nothing reads them. The
# extraction left three phrases pointing at nothing ("as below", "the profiles above")
# plus a `See "Volume" above` in the README aimed at a section that had moved into a
# guide. Those were prose rather than links, so this would not have caught them; what it
# does catch is the next step of the same mistake, which is writing the link and getting
# the target or the anchor wrong. Anchors especially: GitHub's slug rules are not
# guessable, and `#any-os-universal-fallback-vnc--audio-stream` has a double hyphen
# because a `+` vanished between two spaces. That was written by hand and only this
# check knows whether it is right.
#
# Deliberately not a link *reachability* check. External URLs are not fetched: a network
# call would make this fail on a plane, and a dead third-party URL is not something a
# commit should be blocked on.
#
# grep and awk rather than a Go program, on the same reasoning as the no-regex rule in
# the Makefile: command-line grep is fine, and this is text processing over a handful of
# files rather than logic that wants a test suite of its own.
set -uo pipefail

# Files come from git rather than a bare find, so an uncommitted new guide is checked
# while an ignored working copy (a worktree, a vendored tree) is not. Same reasoning as
# the no-regex target.
mapfile -t files < <(git ls-files --cached --others --exclude-standard '*.md')

if [ "${#files[@]}" -eq 0 ]; then
  echo "check-doc-links: no Markdown files found; this should not happen" >&2
  exit 1
fi

# GitHub's heading-to-anchor rule, as far as it matters here: lowercase, drop anything
# that is not alphanumeric, space or hyphen, then spaces to hyphens. Backticks and
# punctuation vanish, which is why `CMakeCache.txt` becomes `cmakecachetxt`.
#
# Not exhaustive - it does not deduplicate repeated headings the way GitHub appends -1,
# and it does not handle Setext underlined headings, neither of which this repo uses. It
# would report a false failure rather than a false pass if either turned up, which is
# the right direction.
slugs_of() {
  awk '
    /^#+[ \t]/ {
      sub(/^#+[ \t]+/, "")
      # Trailing hashes, for anyone writing `## Heading ##`.
      sub(/[ \t]+#+[ \t]*$/, "")
      line = tolower($0)
      out = ""
      n = length(line)
      for (i = 1; i <= n; i++) {
        c = substr(line, i, 1)
        if (c ~ /[a-z0-9-]/) {
          out = out c
        } else if (c == " ") {
          out = out "-"
        }
      }
      print out
    }
  ' "$1"
}

failures=0

report() {
  printf '%s\n' "$1" >&2
  failures=$((failures + 1))
}

for file in "${files[@]}"; do
  dir=$(dirname "$file")

  # Every ](target) in the file. -o so one line with several links yields several
  # matches; the target is everything up to the closing paren.
  while IFS= read -r target; do
    [ -n "$target" ] || continue

    # Leave external and mail links alone: not fetched, on purpose.
    case "$target" in
      http://*|https://*|mailto:*|\#*) ;;
      *) ;;
    esac
    case "$target" in
      http://*|https://*|mailto:*) continue ;;
    esac

    path="${target%%#*}"
    anchor=""
    case "$target" in
      *#*) anchor="${target#*#}" ;;
    esac

    # A bare #anchor points inside the same file.
    if [ -z "$path" ]; then
      resolved="$file"
    elif [ "${path#/}" != "$path" ]; then
      # Absolute paths would resolve against the filesystem root when clicked
      # locally and against the repo root on GitHub, which is two different targets
      # for one link. Nothing here uses one; say so rather than guess.
      report "$file: link target '$target' is absolute; use a path relative to $dir"
      continue
    else
      resolved="$dir/$path"
    fi

    if [ ! -e "$resolved" ]; then
      report "$file: link target '$target' does not exist (looked for $resolved)"
      continue
    fi

    # A link to a directory is fine and has no anchors to check.
    if [ -d "$resolved" ]; then
      if [ -n "$anchor" ]; then
        report "$file: link target '$target' names an anchor on a directory"
      fi
      continue
    fi

    [ -n "$anchor" ] || continue

    case "$resolved" in
      *.md) ;;
      *) report "$file: link target '$target' names an anchor on a non-Markdown file"
         continue ;;
    esac

    if ! slugs_of "$resolved" | grep -qxF -- "$anchor"; then
      report "$file: link target '$target' names a heading that does not exist in $resolved"
      # Naming the near misses, because an anchor is almost always a slug typo rather
      # than a missing section, and the correct one is tedious to derive by hand.
      slugs_of "$resolved" | grep -F -- "${anchor%%-*}" | head -3 |
        while IFS= read -r near; do
          printf '    did you mean #%s ?\n' "$near" >&2
        done
    fi
  done < <(grep -oh '](\([^)]*\))' "$file" 2>/dev/null | sed 's/^](//; s/)$//')
done

if [ "$failures" -ne 0 ]; then
  echo >&2
  echo "check-doc-links: $failures broken link(s)" >&2
  exit 1
fi

echo "check-doc-links: ok (${#files[@]} files)"
