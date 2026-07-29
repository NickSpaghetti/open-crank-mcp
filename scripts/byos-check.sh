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

# Its own compose project, its own ports, its own data directory. Without this
# the suite recreates the container that a person may be using right now: it
# needs a known game mounted, so it would unmount theirs, and it would fight
# them for the single-listener audio stream. Isolated, it can run while someone
# plays.
export COMPOSE_PROJECT_NAME=open-crank-mcp-check
export VNC_PORT="${VNC_PORT:-6180}"
export AUDIO_PORT="${AUDIO_PORT:-8100}"

# A fixed path rather than mktemp, reused each run. mktemp accumulated a
# directory per run, each holding root-owned build output that needs sudo to
# remove, because --keep skips cleanup by design.
GAME_DIR="/tmp/open-crank-mcp-check-game"
export BYOS_DATA_DIR="/tmp/open-crank-mcp-check-data"

COMPOSE=(docker compose -f "$REPO_DIR/docker-compose.yml")

# Only the WSL profile reads these, and compose warns about every unset
# variable it interpolates regardless of which profile is being used.
export PULSE_SERVER="${PULSE_SERVER:-}"
export WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-}"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp}"
KEEP=0
[ "${1:-}" = "--keep" ] && KEEP=1

BASE_URL="http://localhost:${VNC_PORT}"
AUDIO_URL="http://localhost:${AUDIO_PORT}"

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
    echo "isolated container left running for the browser tests:"
    echo "  view:      $BASE_URL/"
    echo "  project:   $COMPOSE_PROJECT_NAME"
    echo "  game:      the Lua fixture in $GAME_DIR"
    echo "  stop with: COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME docker compose --profile byos down"
    echo
    echo "your own byos container, if you have one, is untouched on port 6080."
    return
  fi
  # pdc runs as root inside the container, so its output is root-owned on the
  # host. Delete it from in there, where the ownership matches, rather than
  # asking for sudo.
  in_container rm -rf /your-game/fixture.pdx >/dev/null 2>&1
  "${COMPOSE[@]}" --profile byos down >/dev/null 2>&1
  # Root-owned, because the container runs as root, so removed from in there.
  docker run --rm -v /tmp:/host-tmp alpine \
    rm -rf "/host-tmp/$(basename "$GAME_DIR")" "/host-tmp/$(basename "$BYOS_DATA_DIR")" \
    >/dev/null 2>&1
}

# The fixture plus the harness it imports. setup would normally copy the
# harness in; doing it here keeps this independent of the MCP tools.
rm -rf "$GAME_DIR" 2>/dev/null || true
mkdir -p "$GAME_DIR" "$BYOS_DATA_DIR"
cp -r "$REPO_DIR/lua/test-fixture/Source" "$GAME_DIR/"
cp "$REPO_DIR/lua/mcp_harness.lua" "$GAME_DIR/Source/"
trap cleanup EXIT

echo "booting an isolated byos container on port $VNC_PORT with the Lua fixture"
PLAYDATE_SDK_VERSION="${PLAYDATE_SDK_VERSION:-3.1.1}" \
  "${COMPOSE[@]}" --profile byos build simulator-byos >/dev/null 2>&1
GAME_DIR="$GAME_DIR" "${COMPOSE[@]}" --profile byos up -d simulator-byos >/dev/null 2>&1 || {
    echo "FAIL container did not start"
    exit 1
  }
sleep 5

# --- the pages websockify serves -------------------------------------------
# All four have gone missing at least once, and a missing page is invisible
# until someone opens that exact URL.
for page in "" "vnc.html" "audio.html" "pd-audio.js" "pd-view.js"; do
  status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$BASE_URL/$page")
  check "GET /$page" "200" "$status"
done

# Browsers download rather than play a stream served as octet-stream. Checked
# over HTTP directly, which is safe now that each listener gets its own encoder -
# it used to be that connecting could fail simply because someone was listening.
content_type=$(curl -s -D - -o /dev/null --max-time 8 "$AUDIO_URL/stream.mp3" 2>/dev/null \
  | tr -d '\r' | awk -F': ' 'tolower($1) == "content-type" { print $2; exit }')
check "audio stream content type" "audio/mpeg" "$content_type"

# Nothing should be capturing while nobody is listening. An encoder left running
# on an idle stream drifts behind real time - measured at ~22s after a few
# minutes - and hands that backlog to whoever connects next.
# Polled, because the check just above disconnected a listener and its encoder
# notices on its next write rather than instantly. Asserting immediately measures
# this script's own last connection.
idle_encoders=missing
for _ in $(seq 1 10); do
  idle_encoders=$(in_container bash -c "ps -eo args | grep -c '[l]ibmp3lame'" | tr -d '\r')
  [ "${idle_encoders:-1}" = "0" ] && break
  sleep 1
done
check "no encoder runs while nobody is listening" "0" "${idle_encoders:-missing}"

# Two listeners at once, which the previous single-listener server could not do.
in_container bash -c 'ffmpeg -loglevel error -i http://127.0.0.1:8000/stream.mp3 -t 6 -f null /dev/null >/dev/null 2>&1 &' >/dev/null 2>&1
in_container bash -c 'ffmpeg -loglevel error -i http://127.0.0.1:8000/stream.mp3 -t 6 -f null /dev/null >/dev/null 2>&1 &' >/dev/null 2>&1
sleep 2
concurrent=$(in_container bash -c "ps -eo args | grep -c '[l]ibmp3lame'" | tr -d '\r')
check_true "two listeners are served at once (saw ${concurrent:-0} encoders)" \
  "$([ "${concurrent:-0}" -ge 2 ] && echo 1 || echo 0)"
sleep 6

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
  if curl -s --max-time 5 "$BASE_URL/pd-layout.json" | grep -q troughX; then break; fi
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
layout=$(curl -s --max-time 10 "$BASE_URL/pd-layout.json")
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

# --- the slider reading -----------------------------------------------------
# The published volume is what the browser follows, so what matters is that it
# tracks the real slider. A fresh Simulator starts at zero; clicking part way up
# the track has to move the number. This covers the whole chain the audio depends
# on: X input, GTK's click-to-warp, the framebuffer scan, and the parser.
volume_before=$(curl -s --max-time 10 "$BASE_URL/pd-volume.json" \
  | awk -F'[:}]' '{ gsub(/ /, "", $2); print $2 }')
check "a fresh Simulator publishes zero volume" "0.000" "${volume_before:-missing}"

in_container bash -c "xdotool mousemove ${trough_x} $(( trough_top + 20 )); sleep 0.4; xdotool click 1" >/dev/null 2>&1
sleep 3

volume_after=$(curl -s --max-time 10 "$BASE_URL/pd-volume.json" \
  | awk -F'[:}]' '{ gsub(/ /, "", $2); print $2 }')
check_true "clicking the track raises the published volume (${volume_before:-?} -> ${volume_after:-?})" \
  "$(awk -v a="${volume_before:-0}" -v b="${volume_after:-0}" 'BEGIN { print (b > a && b <= 1) ? 1 : 0 }')"

# A reading of -1 means the scan failed, and the page deliberately ignores it.
# Publishing 0 on a failed scan would silence working audio.
check_true "the reading is a real number, not a failed scan" \
  "$(awk -v v="${volume_after:--1}" 'BEGIN { print (v >= 0) ? 1 : 0 }')"

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
