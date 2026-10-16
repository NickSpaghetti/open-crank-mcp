# Helpers for the shared/vnc profile, testable without a container. Source, don't execute.

SHARED_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The Simulator's window size at a given zoom, with the crank panel shown. The
# 466 is fixed chrome that doesn't scale: menu bar, d-pad row, toolbar and panel.
shared_window_size() {
  local zoom="$1"
  echo "$(( 400 * zoom + 82 ))x$(( 240 * zoom + 466 ))"
}

# Reads a greyscale pixel column (one value per line on stdin) and prints
# "top bottom mute volume", or nothing when it doesn't look like the device frame.
shared_find_slider() {
  awk -f "$SHARED_LIB_DIR/find-volume-slider.awk" "$@"
}

shared_column_from_raw() {
  od -An -tu1 -v "$1" | tr -s ' ' '\n' | grep -v '^$'
}

# Prints "x y width height column top bottom mute volume" for the Simulator
# window's volume slider, or fails. Geometry is read fresh so a moved window or
# new zoom can't leave this reading the wrong pixels.
shared_read_slider() {
  local geometry display_size win
  win=$(xdotool search --name '^Playdate Simulator$' 2>/dev/null | tail -1)
  [ -n "$win" ] || return 1

  unset X Y WIDTH HEIGHT
  eval "$(xdotool getwindowgeometry --shell "$win" 2>/dev/null)"
  [ -n "${WIDTH:-}" ] && [ -n "${HEIGHT:-}" ] || return 1

  display_size=$(xdotool getdisplaygeometry | tr ' ' 'x')
  ffmpeg -loglevel error -f x11grab -video_size "$display_size" -i "$DISPLAY" \
    -frames:v 1 -y /tmp/pd-slider.png 2>/dev/null || return 1
  ffmpeg -loglevel error -i /tmp/pd-slider.png \
    -vf "crop=1:400:$(( X + WIDTH - 29 )):${Y},format=gray" \
    -f rawvideo -y /tmp/pd-slider.raw 2>/dev/null || return 1

  local parsed
  parsed=$(shared_column_from_raw /tmp/pd-slider.raw | shared_find_slider)
  [ -n "$parsed" ] || return 1
  echo "$X $Y $WIDTH $HEIGHT $(( X + WIDTH - 29 )) $parsed"
}
