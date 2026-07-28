#!/usr/bin/env bash
# Unit tests for the byos helpers. No container, no display, no network: these
# run anywhere awk and bash do.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/byos-lib.sh
source "$SCRIPT_DIR/byos-lib.sh"

failures=0
checks=0

check() {
  local name="$1" expected="$2" actual="$3"
  checks=$(( checks + 1 ))
  if [ "$expected" = "$actual" ]; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n       expected: %q\n       actual:   %q\n' "$name" "$expected" "$actual"
    failures=$(( failures + 1 ))
  fi
}

# Builds a synthetic pixel column. Yellow frame everywhere except the widgets
# named in the arguments, each given as start:end:value.
column() {
  local rows="$1"; shift
  local spec value
  local -a pixels
  local row
  for (( row = 1; row <= rows; row++ )); do
    pixels[row]=190
  done
  for spec in "$@"; do
    local from="${spec%%:*}"
    local rest="${spec#*:}"
    local to="${rest%%:*}"
    value="${rest#*:}"
    for (( row = from; row <= to; row++ )); do
      pixels[row]="$value"
    done
  done
  for (( row = 1; row <= rows; row++ )); do
    echo "${pixels[row]}"
  done
}

# --- byos_window_size -------------------------------------------------------
# Both were measured against the real Simulator, 1x with the crank panel shown
# and 2x on an oversized display so nothing was clipped.
check "window size at 1x" "482x706" "$(byos_window_size 1)"
check "window size at 2x" "882x946" "$(byos_window_size 2)"
check "window size at 3x" "1282x1186" "$(byos_window_size 3)"

# --- byos_find_slider -------------------------------------------------------
# The real 1x layout, as read off the framebuffer: LOCK 44-48, MENU 68-80,
# trough 124-193, knob 195-207, mute icon 219-229. The knob merges into the
# trough across the one-pixel gap at 194, so the trough reads as 124-207.
check "1x device frame" "124 207 224" \
  "$(column 400 44:48:120 68:80:255 124:193:130 195:207:255 219:229:120 | byos_find_slider)"

# The 2x layout sits about 19px lower. Same shape, so nothing should need
# retuning between zoom levels.
check "2x device frame" "143 226 243" \
  "$(column 400 63:67:120 87:99:255 143:212:130 214:226:255 238:248:120 | byos_find_slider)"

# Volume part-way up: the knob sits inside the trough and splits the dark run
# in two. Merging across the gaps has to keep it as one trough, or the longest
# run becomes half a trough and every click lands at the wrong level.
check "knob mid-trough stays one run" "124 207 224" \
  "$(column 400 44:48:120 68:80:255 124:155:130 157:169:255 171:207:130 219:229:120 | byos_find_slider)"

# Nothing that looks like a device frame. Printing nothing is the contract:
# the caller publishes no layout and the click mapping stays off.
check "plain yellow column yields nothing" "" \
  "$(column 400 | byos_find_slider)"
check "empty input yields nothing" "" "$(printf '' | byos_find_slider)"

# Buttons alone must not be mistaken for a trough. This is what the minimum
# run length is for.
check "buttons without a trough yield nothing" "" \
  "$(column 400 44:48:120 68:80:255 | byos_find_slider)"

# A trough with no mute icon below it still locates the trough, reporting the
# mute row as 0 rather than failing outright.
check "trough without mute icon" "124 193 0" \
  "$(column 400 124:193:130 | byos_find_slider)"

# Rows above the skip threshold are ignored even when they are long runs,
# which is what stops the screen bezel above the frame from winning.
check "long run above the skip window is ignored" "124 193 0" \
  "$(column 400 5:85:20 124:193:130 | byos_find_slider)"

printf '\n%d checks, %d failed\n' "$checks" "$failures"
[ "$failures" -eq 0 ]
