#!/usr/bin/env bash
# Rebuilds and reloads the running game whenever its source changes. Runs inside
# the shared container. The reload is the Simulator's Ctrl-R (Reset), which re-reads
# the .pdx in the same process, so the VNC view and get_status survive; game state doesn't.
set -uo pipefail

SOURCE_DIR="${SOURCE_DIR:-/your-game/Source}"
PDX_PATH="${PDX_PATH:-/your-game/your-game.pdx}"

if [ ! -d "$SOURCE_DIR" ]; then
  echo "shared-watch: no $SOURCE_DIR - is GAME_DIR pointing at a project with a Source directory?" >&2
  exit 1
fi

reload() {
  local build_output
  if ! build_output=$(pdc "$SOURCE_DIR" "$PDX_PATH" 2>&1); then
    echo "  build failed, keeping the running build:" >&2
    echo "$build_output" | sed 's/^/    /' >&2
    return 1
  fi
  [ -n "$build_output" ] && echo "$build_output" | sed 's/^/    /'

  local win
  win=$(xdotool search --name '^Playdate Simulator$' 2>/dev/null | tail -1)
  if [ -z "$win" ]; then
    echo "  built, but no Simulator window to reload - launch one first" >&2
    return 1
  fi

  # Ctrl-R is a menu accelerator, so it goes to the focused window.
  xdotool windowactivate "$win" 2>/dev/null
  sleep 0.3
  xdotool key --clearmodifiers ctrl+r
  echo "  reloaded"
}

echo "shared-watch: watching $SOURCE_DIR"
echo "  save a file to rebuild and reload. Ctrl-C to stop."
echo

while true; do
  # close_write rather than modify, which fires on every intermediate write.
  changed=$(inotifywait -q -r -e close_write -e moved_to -e delete \
    --format '%f' "$SOURCE_DIR" 2>/dev/null)
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "shared-watch: inotifywait exited ($status), stopping" >&2
    exit "$status"
  fi

  # Drain the burst of writes an editor makes, so one save is one rebuild.
  while inotifywait -q -r -t 1 -e close_write -e moved_to -e delete \
    "$SOURCE_DIR" >/dev/null 2>&1; do :; done

  echo "$(date +%H:%M:%S) $changed changed"
  reload || true
done
