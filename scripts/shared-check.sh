#!/usr/bin/env bash
# Integration checks for the shared profile's VNC workspace, against the in-repo
# Lua fixture. Launches the Simulator directly; sdk-contract-check covers the MCP path.
# --keep leaves the container running for the browser tests.
set -uo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Its own compose project, ports and data directory, so it doesn't recreate a
# shared container someone is using with their game mounted.
export COMPOSE_PROJECT_NAME=open-crank-mcp-check
export VNC_PORT="${VNC_PORT:-6180}"
export AUDIO_PORT="${AUDIO_PORT:-8100}"

# Fixed paths, reused each run: they hold root-owned output that --keep leaves behind.
GAME_DIR="/tmp/open-crank-mcp-check-game"
export SHARED_DATA_DIR="/tmp/open-crank-mcp-check-data"

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

in_container() { "${COMPOSE[@]}" exec -T simulator-shared "$@"; }

cleanup() {
  if [ "$KEEP" = "1" ]; then
    echo
    echo "isolated container left running for the browser tests:"
    echo "  view:      $BASE_URL/"
    echo "  project:   $COMPOSE_PROJECT_NAME"
    echo "  game:      the Lua fixture in $GAME_DIR"
    echo "  stop with: COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME docker compose --profile shared down"
    echo
    echo "your own shared container, if you have one, is untouched on port 6080."
    return
  fi
  # Root-owned output, so deleted from inside a container rather than with sudo.
  in_container rm -rf /your-game/fixture.pdx >/dev/null 2>&1
  "${COMPOSE[@]}" --profile shared down >/dev/null 2>&1
  docker run --rm -v /tmp:/host-tmp alpine \
    rm -rf "/host-tmp/$(basename "$GAME_DIR")" "/host-tmp/$(basename "$SHARED_DATA_DIR")" \
    >/dev/null 2>&1
}

# The fixture plus its harness, copied here so this doesn't depend on the setup tool.
rm -rf "$GAME_DIR" 2>/dev/null || true
mkdir -p "$GAME_DIR" "$SHARED_DATA_DIR"
cp -r "$REPO_DIR/lua/test-fixture/Source" "$GAME_DIR/"
cp "$REPO_DIR/lua/mcp_harness.lua" "$GAME_DIR/Source/"
trap cleanup EXIT

echo "booting an isolated shared container on port $VNC_PORT with the Lua fixture"
PLAYDATE_SDK_VERSION="${PLAYDATE_SDK_VERSION:-3.1.2}" \
  "${COMPOSE[@]}" --profile shared build simulator-shared >/dev/null 2>&1
GAME_DIR="$GAME_DIR" "${COMPOSE[@]}" --profile shared up -d simulator-shared >/dev/null 2>&1 || {
    echo "FAIL container did not start"
    exit 1
  }
sleep 5

# --- the pages websockify serves -------------------------------------------
for page in "" "vnc.html" "audio.html" "pd-audio.js" "pd-view.js"; do
  status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$BASE_URL/$page")
  check "GET /$page" "200" "$status"
done

# Browsers download rather than play a stream served as octet-stream.
content_type=$(curl -s -D - -o /dev/null --max-time 8 "$AUDIO_URL/stream.mp3" 2>/dev/null \
  | tr -d '\r' | awk -F': ' 'tolower($1) == "content-type" { print $2; exit }')
check "audio stream content type" "audio/mpeg" "$content_type"

# An idle encoder drifts behind real time. Polled, because the listener above
# disconnected and its encoder only notices on its next write.
idle_encoders=missing
for _ in $(seq 1 10); do
  idle_encoders=$(in_container bash -c "ps -eo args | grep -c '[l]ibmp3lame'" | tr -d '\r')
  [ "${idle_encoders:-1}" = "0" ] && break
  sleep 1
done
check "no encoder runs while nobody is listening" "0" "${idle_encoders:-missing}"

# Two listeners at once.
in_container bash -c 'ffmpeg -loglevel error -i http://127.0.0.1:8000/stream.mp3 -t 6 -f null /dev/null >/dev/null 2>&1 &' >/dev/null 2>&1
in_container bash -c 'ffmpeg -loglevel error -i http://127.0.0.1:8000/stream.mp3 -t 6 -f null /dev/null >/dev/null 2>&1 &' >/dev/null 2>&1
sleep 2
concurrent=$(in_container bash -c "ps -eo args | grep -c '[l]ibmp3lame'" | tr -d '\r')
check_true "two listeners are served at once (saw ${concurrent:-0} encoders)" \
  "$([ "${concurrent:-0}" -ge 2 ] && echo 1 || echo 0)"
sleep 6

# --- window manager configuration ------------------------------------------
# Compared against openbox's shipped config: a partial rc.xml silently drops
# every mouse binding it doesn't mention.
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

# Detached exec: a backgrounded child of a normal exec session dies with it.
"${COMPOSE[@]}" exec -d simulator-shared /opt/playdate-sdk/bin/PlaydateSimulator \
  /your-game/fixture.pdx /opt/playdate-sdk/Disk/Data/dev.open-crank-mcp.contractchecklua \
  >/dev/null 2>&1

# The watcher publishes the layout a couple of seconds after the window appears.
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

check "window size matches the 1x formula" "$(bash -c "source '$REPO_DIR/scripts/shared-lib.sh'; shared_window_size 1")" \
  "${win_w}x${win_h}"

# --- nothing is covering the Simulator -------------------------------------
# The slider scan can't tell a covered widget (e.g. an SDK update modal) from a
# missing one. Exact match, so any unexpected window fails.
windows=$(in_container bash -c '
  for w in $(xdotool search --onlyvisible --name "." 2>/dev/null); do
    xdotool getwindowname "$w" 2>/dev/null
  done' | tr -d '\r' | sort | paste -sd', ' -)
check "only the Simulator window is open" "Playdate Simulator" "$windows"

# --- the published slider layout -------------------------------------------
layout=$(curl -s --max-time 10 "$BASE_URL/pd-layout.json")
trough_x=$(echo "$layout" | awk -F'[:,]' '/troughX/ { gsub(/ /, "", $2); print $2 }')
trough_top=$(echo "$layout" | awk -F'[:,]' '/troughTop/ { gsub(/ /, "", $2); print $2 }')
trough_bottom=$(echo "$layout" | awk -F'[:,]' '/troughBottom/ { gsub(/ /, "", $2); print $2 }')
mute_y=$(echo "$layout" | awk -F'[:,]' '/muteY/ { gsub(/ /, "", $2); print $2 }')

check_true "layout published a trough" \
  "$([ -n "${trough_top:-}" ] && [ -n "${trough_bottom:-}" ] && echo 1 || echo 0)"

# Relationships, not pixel values, which move with theme and zoom.
check_true "trough is inside the window horizontally" \
  "$([ "${trough_x:-0}" -gt "${win_x:-0}" ] && [ "${trough_x:-0}" -lt "$(( ${win_x:-0} + ${win_w:-0} ))" ] && echo 1 || echo 0)"
check_true "trough runs downwards a plausible distance" \
  "$([ "$(( ${trough_bottom:-0} - ${trough_top:-0} ))" -ge 40 ] \
    && [ "$(( ${trough_bottom:-0} - ${trough_top:-0} ))" -le 200 ] && echo 1 || echo 0)"
check_true "mute icon sits below the trough" \
  "$([ "${mute_y:-0}" -gt "${trough_bottom:-0}" ] && echo 1 || echo 0)"

# --- the slider reading -----------------------------------------------------
# A fresh Simulator starts at zero; clicking part way up the track has to move the
# published number. Covers X input, GTK's click-to-warp, the scan and the parser.
volume_before=$(curl -s --max-time 10 "$BASE_URL/pd-volume.json" \
  | awk -F'[:}]' '{ gsub(/ /, "", $2); print $2 }')
check "a fresh Simulator publishes zero volume" "0.000" "${volume_before:-missing}"

in_container bash -c "xdotool mousemove ${trough_x} $(( trough_top + 20 )); sleep 0.4; xdotool click 1" >/dev/null 2>&1
sleep 3

volume_after=$(curl -s --max-time 10 "$BASE_URL/pd-volume.json" \
  | awk -F'[:}]' '{ gsub(/ /, "", $2); print $2 }')
check_true "clicking the track raises the published volume (${volume_before:-?} -> ${volume_after:-?})" \
  "$(awk -v a="${volume_before:-0}" -v b="${volume_after:-0}" 'BEGIN { print (b > a && b <= 1) ? 1 : 0 }')"

# -1 means the scan failed.
check_true "the reading is a real number, not a failed scan" \
  "$(awk -v v="${volume_after:--1}" 'BEGIN { print (v >= 0) ? 1 : 0 }')"

# --- the audio chain, without needing a noisy game -------------------------
# A synthetic tone proves the sink and its monitor without depending on the fixture.
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
