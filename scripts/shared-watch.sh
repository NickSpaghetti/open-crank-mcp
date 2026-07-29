#!/usr/bin/env bash
# Rebuilds and reloads the running game whenever its source changes. Runs
# inside the shared container; `make shared-watch` pipes it in.
#
# The reload is the Simulator's own Reset accelerator, Ctrl-R, which re-reads
# the .pdx from disk. Verified: swapping the compiled code under a running
# Simulator and pressing Ctrl-R picks up the new build, in the same process.
# That's what makes this a reload rather than a restart - the process, the
# display and the browser tab all survive, so the VNC view doesn't even
# reconnect.
#
# Two things it is not. It isn't state-preserving: Reset means the game starts
# over, because the SDK has no way to swap code under a running game. And it
# doesn't go through the MCP server, so an agent's view of the world is
# untouched: same process, so get_status stays true.
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
    # A failed build leaves the previous .pdx in place, so the running game
    # keeps working rather than being replaced by nothing.
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

  # Focus first: Ctrl-R is a menu accelerator, so it goes to the focused
  # window. This is also why the container runs a window manager at all -
  # without one there's no focus to give.
  xdotool windowactivate "$win" 2>/dev/null
  sleep 0.3
  xdotool key --clearmodifiers ctrl+r
  echo "  reloaded"
}

echo "shared-watch: watching $SOURCE_DIR"
echo "  save a file to rebuild and reload. Ctrl-C to stop."
echo

while true; do
  # -e close_write rather than modify: editors that write through a temp file
  # and rename would otherwise fire several times per save, and modify fires
  # on every intermediate write of a large file.
  changed=$(inotifywait -q -r -e close_write -e moved_to -e delete \
    --format '%f' "$SOURCE_DIR" 2>/dev/null)
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "shared-watch: inotifywait exited ($status), stopping" >&2
    exit "$status"
  fi

  # Editors often write a burst of files. Draining the burst before building
  # keeps this to one rebuild per save rather than one per file.
  while inotifywait -q -r -t 1 -e close_write -e moved_to -e delete \
    "$SOURCE_DIR" >/dev/null 2>&1; do :; done

  echo "$(date +%H:%M:%S) $changed changed"
  reload || true
done
