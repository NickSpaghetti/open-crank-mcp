#!/usr/bin/env bash
# Integration checks for the byos profile. Boots the container against the
# in-repo Lua fixture, launches the Simulator inside it, and asserts the
# invariants that have actually broken in practice.
#
# It launches the Simulator directly rather than through an MCP session. The
# MCP path is already covered by sdk-contract-check, and what's under test here
# is the VNC workspace: the window manager's configuration, the pages
# websockify serves, and the framebuffer-derived slider layout.
#
# Pass --keep to leave the container running afterwards, which is what the
# browser tests need.
set -uo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$REPO_DIR/docker-compose.yml")

# Only the WSL profile reads these, and compose warns about every unset
# variable it interpolates regardless of which profile is being used.
export PULSE_SERVER="${PULSE_SERVER:-}"
export WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-}"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp}"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

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

check_true() {
  local name="$1" condition="$2"
  checks=$(( checks + 1 ))
  if [ "$condition" = "1" ]; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n' "$name"
    failures=$(( failures + 1 ))
  fi
}

in_container() { "${COMPOSE[@]}" exec -T simulator-byos "$@"; }

cleanup() {
  if [ "$KEEP" = "1" ]; then
    echo
    echo "container left running, game dir $GAME_DIR"
    echo "  video + audio: http://localhost:6080/"
    return
  fi
  # pdc runs as root inside the container, so its output is root-owned on the
  # host. Delete it from in there, where the ownership matches, rather than
  # asking for sudo.
  in_container rm -rf /your-game/fixture.pdx >/dev/null 2>&1
  "${COMPOSE[@]}" --profile byos down >/dev/null 2>&1
  rm -rf "$GAME_DIR"
}

# The fixture plus the harness it imports. setup would normally copy the
# harness in; doing it here keeps this independent of the MCP tools.
GAME_DIR="$(mktemp -d)"
cp -r "$REPO_DIR/lua/test-fixture/Source" "$GAME_DIR/"
cp "$REPO_DIR/lua/mcp_harness.lua" "$GAME_DIR/Source/"
trap cleanup EXIT

echo "booting byos with the Lua fixture"
GAME_DIR="$GAME_DIR" PLAYDATE_SDK_VERSION="${PLAYDATE_SDK_VERSION:-3.1.1}" \
  make -C "$REPO_DIR" up-byos >/dev/null 2>&1 || {
    echo "FAIL container did not start"
    exit 1
  }
sleep 5

# --- the pages websockify serves -------------------------------------------
# All four have gone missing at least once, and a missing page is invisible
# until someone opens that exact URL.
for page in "" "vnc.html" "audio.html" "pd-audio.js" "pd-view.js"; do
  status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "http://localhost:6080/$page")
  check "GET /$page" "200" "$status"
done

# Browsers download rather than play a stream served as octet-stream.
content_type=$(curl -s -D - -o /dev/null --max-time 5 "http://localhost:8000/stream.mp3" 2>/dev/null \
  | tr -d '\r' | awk -F': ' 'tolower($1) == "content-type" { print $2; exit }')
check "audio stream content type" "audio/mpeg" "$content_type"

# --- window manager configuration ------------------------------------------
# The mouse bindings are compared against openbox's own shipped config rather
# than a hardcoded number. A partial rc.xml silently drops every binding it
# doesn't mention, which is how dragging, resizing and the window list were
# lost once already.
shipped=$(in_container grep -c '<mousebind' /etc/xdg/openbox/rc.xml | tr -d '\r')
patched=$(in_container grep -c '<mousebind' /root/.config/openbox/rc.xml | tr -d '\r')
check "openbox keeps every shipped mouse binding" "$shipped" "$patched"

check "titlebar has no buttons" "<titleLayout>L</titleLayout>" \
  "$(in_container grep -o '<titleLayout>.*</titleLayout>' /root/.config/openbox/rc.xml | tr -d '\r')"

# Each of these can lose the Simulator with no way back from inside the view.
for action in ToggleShowDesktop Close Iconify; do
  count=$(in_container bash -c "grep -A1 '<keybind' /root/.config/openbox/rc.xml | grep -c 'name=\"$action\"'" | tr -d '\r')
  check "no $action keybinding" "0" "$count"
done

# A stray scroll over the background switches desktops, and the Simulator is
# only ever on one of them.
check "single desktop" "1" "$(in_container xdotool get_num_desktops | tr -d '\r')"

# --- the Simulator window ---------------------------------------------------
echo "building and launching the fixture"
if ! build_output=$(in_container bash -c 'cd /your-game && pdc Source fixture.pdx' 2>&1); then
  echo "FAIL pdc could not build the fixture"
  echo "$build_output"
  exit 1
fi

# Detached exec, not 'nohup ... &' inside a normal exec. A backgrounded child of
# an exec session gets torn down with that session, which made the Simulator
# fail to be there about half the time.
"${COMPOSE[@]}" exec -d simulator-byos /opt/playdate-sdk/bin/PlaydateSimulator \
  /your-game/fixture.pdx /opt/playdate-sdk/Disk/Data/dev.open-crank-mcp.contractchecklua \
  >/dev/null 2>&1

# Waiting for the window and then for the layout to be published, rather than
# sleeping a guessed number of seconds. The watcher publishes a couple of
# seconds after the window appears.
for _ in $(seq 1 30); do
  if in_container xdotool search --name '^Playdate Simulator$' >/dev/null 2>&1; then break; fi
  sleep 1
done
for _ in $(seq 1 20); do
  if curl -s --max-time 5 "http://localhost:6080/pd-layout.json" | grep -q troughX; then break; fi
  sleep 1
done

geometry=$(in_container bash -c 'win=$(xdotool search --name "^Playdate Simulator$" | tail -1); xdotool getwindowgeometry --shell $win' | tr -d '\r')
win_x=$(echo "$geometry" | awk -F= '/^X=/ { print $2 }')
win_y=$(echo "$geometry" | awk -F= '/^Y=/ { print $2 }')
win_w=$(echo "$geometry" | awk -F= '/^WIDTH=/ { print $2 }')
win_h=$(echo "$geometry" | awk -F= '/^HEIGHT=/ { print $2 }')

# Pinned to the corner, allowing for the window manager's own decorations.
check_true "window pinned to the top-left corner" \
  "$([ "${win_x:-999}" -le 10 ] && [ "${win_y:-999}" -le 48 ] && echo 1 || echo 0)"

# The window's own size should match the measured formula for 1x.
check "window size matches the 1x formula" "$(bash -c "source '$REPO_DIR/scripts/byos-lib.sh'; byos_window_size 1")" \
  "${win_w}x${win_h}"

# --- the published slider layout -------------------------------------------
layout=$(curl -s --max-time 10 "http://localhost:6080/pd-layout.json")
trough_x=$(echo "$layout" | awk -F'[:,]' '/troughX/ { gsub(/ /, "", $2); print $2 }')
trough_top=$(echo "$layout" | awk -F'[:,]' '/troughTop/ { gsub(/ /, "", $2); print $2 }')
trough_bottom=$(echo "$layout" | awk -F'[:,]' '/troughBottom/ { gsub(/ /, "", $2); print $2 }')
mute_y=$(echo "$layout" | awk -F'[:,]' '/muteY/ { gsub(/ /, "", $2); print $2 }')

check_true "layout published a trough" \
  "$([ -n "${trough_top:-}" ] && [ -n "${trough_bottom:-}" ] && echo 1 || echo 0)"

# Asserted as relationships, not pixel values: the exact numbers move with the
# theme and the zoom level, but these have to hold for the click mapping to
# mean anything.
check_true "trough is inside the window horizontally" \
  "$([ "${trough_x:-0}" -gt "${win_x:-0}" ] && [ "${trough_x:-0}" -lt "$(( ${win_x:-0} + ${win_w:-0} ))" ] && echo 1 || echo 0)"
check_true "trough runs downwards a plausible distance" \
  "$([ "$(( ${trough_bottom:-0} - ${trough_top:-0} ))" -ge 40 ] \
    && [ "$(( ${trough_bottom:-0} - ${trough_top:-0} ))" -le 200 ] && echo 1 || echo 0)"
check_true "mute icon sits below the trough" \
  "$([ "${mute_y:-0}" -gt "${trough_bottom:-0}" ] && echo 1 || echo 0)"

# --- the input path to the slider ------------------------------------------
# Clicking near the top of the trough has to move the knob. This covers the
# whole chain the browser depends on: X input, GTK's click-to-warp, and the
# coordinates being right. Deliberately not asserting on audio levels: the
# fixture is silent, and a sound-producing fixture plus level thresholds is a
# flake factory. The pulse chain is checked separately below.
knob_before=$(in_container bash -c "
  source /workspace/scripts/byos-lib.sh
  ffmpeg -loglevel error -f x11grab -video_size \$(xdotool getdisplaygeometry | tr ' ' 'x') -i \$DISPLAY -frames:v 1 -y /tmp/check.png 2>/dev/null
  ffmpeg -loglevel error -i /tmp/check.png -vf crop=1:400:${trough_x}:${win_y},format=gray -f rawvideo -y /tmp/check.raw 2>/dev/null
  byos_column_from_raw /tmp/check.raw | byos_find_slider" | tr -d '\r')

in_container bash -c "xdotool mousemove ${trough_x} $(( trough_top + 4 )); sleep 0.4; xdotool click 1" >/dev/null 2>&1
sleep 1

volume_after=$(in_container bash -c '
  pactl list sink-inputs | awk -F"/" "/Volume: front-left/ { gsub(/ /, \"\", \$2); print \$2; exit }"' | tr -d '\r%')
check_true "clicking the trough raises the Simulator's volume above zero" \
  "$([ -n "${volume_after:-}" ] && echo 1 || echo 0)"

knob_after=$(in_container bash -c "
  source /workspace/scripts/byos-lib.sh
  ffmpeg -loglevel error -f x11grab -video_size \$(xdotool getdisplaygeometry | tr ' ' 'x') -i \$DISPLAY -frames:v 1 -y /tmp/check2.png 2>/dev/null
  ffmpeg -loglevel error -i /tmp/check2.png -vf crop=1:400:${trough_x}:${win_y},format=gray -f rawvideo -y /tmp/check2.raw 2>/dev/null
  byos_column_from_raw /tmp/check2.raw | byos_find_slider" | tr -d '\r')
check_true "the slider still parses after being clicked" \
  "$([ -n "${knob_after:-}" ] && echo 1 || echo 0)"
check_true "clicking near the top of the trough changed the frame" \
  "$([ "$knob_before" != "$knob_after" ] && echo 1 || echo 0)"

# --- the audio chain, without needing a noisy game -------------------------
# A synthetic tone into the sink proves sink, monitor, encoder and HTTP all
# work end to end, with no dependency on what the fixture plays.
tone_level=$(in_container bash -c '
  ffmpeg -loglevel error -f lavfi -i "sine=frequency=440:duration=4" -f pulse -device vnc_sink tone >/dev/null 2>&1 &
  sleep 1
  ffmpeg -loglevel info -f pulse -i vnc_sink.monitor -t 2 -af volumedetect -f null /dev/null 2>&1 \
    | awk -F": " "/mean_volume/ { print \$2 }"' | tr -d '\r')
tone_db=${tone_level%% dB}
check_true "a tone played into the sink is audible on its monitor (${tone_level:-none})" \
  "$(awk -v v="${tone_db:-0}" 'BEGIN { print (v > -60 && v < 0) ? 1 : 0 }')"

printf '\n%d checks, %d failed\n' "$checks" "$failures"
[ "$failures" -eq 0 ]
