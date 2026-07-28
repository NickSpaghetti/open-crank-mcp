# Shared helpers for the byos/vnc profile, kept out of run-vnc.sh so they can
# be tested without booting a container. Source, don't execute.

BYOS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The Simulator's window size at a given zoom, measured at 1x and 2x with the
# accelerometer and crank panel shown. The 466 is fixed chrome that doesn't
# scale: menu bar, d-pad row, toolbar and that panel.
byos_window_size() {
  local zoom="$1"
  echo "$(( 400 * zoom + 82 ))x$(( 240 * zoom + 466 ))"
}

# Reads a greyscale pixel column (one value per line on stdin) and prints
# "top bottom mute" row offsets for the volume slider, or nothing when the
# column doesn't look like the device frame.
byos_find_slider() {
  awk -f "$BYOS_LIB_DIR/find-volume-slider.awk" "$@"
}

# Turns a raw greyscale file into the one-value-per-line form the parser wants.
byos_column_from_raw() {
  od -An -tu1 -v "$1" | tr -s ' ' '\n' | grep -v '^$'
}
