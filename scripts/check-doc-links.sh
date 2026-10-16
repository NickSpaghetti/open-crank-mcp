#!/usr/bin/env bash
# Fails on a relative Markdown link whose file or heading anchor doesn't exist.
# External URLs are not fetched, so a dead third-party site never blocks a commit.
set -uo pipefail

# From git, so untracked guides are checked and ignored trees aren't. A read loop
# because macOS ships bash 3.2, which has no mapfile.
files=()
while IFS= read -r -d '' file; do
  files+=("$file")
done < <(git ls-files -z --cached --others --exclude-standard '*.md')

if [ "${#files[@]}" -eq 0 ]; then
  echo "check-doc-links: no Markdown files found; this should not happen" >&2
  exit 1
fi

# GitHub's heading-to-anchor rule: lowercase, drop all but alphanumerics, spaces and
# hyphens, then spaces to hyphens. No -1 dedup or Setext headings; either fails, not passes.
slugs_of() {
  awk '
    /^#+[ \t]/ {
      sub(/^#+[ \t]+/, "")
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

  while IFS= read -r target; do
    [ -n "$target" ] || continue

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
      # Absolute paths resolve to the filesystem root locally but the repo root on GitHub.
      report "$file: link target '$target' is absolute; use a path relative to $dir"
      continue
    else
      resolved="$dir/$path"
    fi

    if [ ! -e "$resolved" ]; then
      report "$file: link target '$target' does not exist (looked for $resolved)"
      continue
    fi

    if [ -d "$resolved" ]; then
      if [ -n "$anchor" ]; then
        report "$file: link target '$target' names an anchor on a directory"
      fi
      continue
    fi

    [ -n "$anchor" ] || continue

    # Not anchors: GitHub's per-theme image suffixes.
    case "$anchor" in
      gh-light-mode-only | gh-dark-mode-only) continue ;;
    esac

    case "$resolved" in
      *.md) ;;
      *) report "$file: link target '$target' names an anchor on a non-Markdown file"
         continue ;;
    esac

    if ! slugs_of "$resolved" | grep -qxF -- "$anchor"; then
      report "$file: link target '$target' names a heading that does not exist in $resolved"
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
