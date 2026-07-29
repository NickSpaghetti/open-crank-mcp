# Shared helpers for the shared/vnc profile, kept out of run-vnc.sh so they can
# be tested without booting a container. Source, don't execute.

SHARED_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The Simulator's window size at a given zoom, measured at 1x and 2x with the
# accelerometer and crank panel shown. The 466 is fixed chrome that doesn't
# scale: menu bar, d-pad row, toolbar and that panel.
shared_window_size() {
  local zoom="$1"
  echo "$(( 400 * zoom + 82 ))x$(( 240 * zoom + 466 ))"
}

# Reads a greyscale pixel column (one value per line on stdin) and prints
# "top bottom mute" row offsets for the volume slider, or nothing when the
# column doesn't look like the device frame.
shared_find_slider() {
  awk -f "$SHARED_LIB_DIR/find-volume-slider.awk" "$@"
}

# Turns a raw greyscale file into the one-value-per-line form the parser wants.
shared_column_from_raw() {
  od -An -tu1 -v "$1" | tr -s ' ' '\n' | grep -v '^$'
}

# Grabs the one-pixel column down the device frame that crosses the volume
# slider, and prints what the parser makes of it: "top bottom mute volume".
# Prints nothing when the Simulator isn't there or the column doesn't look like
# a device frame.
#
# Geometry is read fresh every time rather than cached, so dragging the window
# or changing zoom can't leave this reading the wrong pixels.
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
