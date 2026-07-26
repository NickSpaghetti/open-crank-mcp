#!/usr/bin/env bash
set -euo pipefail

display="${DISPLAY:-:0}"
number="${display#:}"

if ! xauth list 2>/dev/null | grep -q "unix:${number}\b"; then
  xauth generate "$display" . trusted
fi
