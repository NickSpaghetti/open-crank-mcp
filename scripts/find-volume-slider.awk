# Locates the Simulator's volume slider in a one-pixel-wide column of
# greyscale pixels taken down its device frame, one value per line.
#
# The column crosses the LOCK button, the MENU button, the volume trough, the
# trough's knob and the mute icon. Against the yellow frame every one of those
# reads as either dark or light, so the runs of non-yellow pixels are the
# widgets. The trough is the longest of them.
#
# Prints "top bottom mute" as row offsets, or nothing at all when the column
# doesn't look like a device frame. Printing nothing matters: callers treat it
# as "don't know" and leave the slider alone, which is better than aiming a
# click at a guess.
#
# Tunables, all overridable with -v:
#   skip      rows to ignore at the top, past the LOCK and MENU buttons
#   limit     last row to consider
#   dark      at or below this is dark
#   light     at or above this is light
#   merge     gap in rows small enough to treat two runs as one, which keeps
#             the knob attached to the trough it sits in
#   minimum   shortest run that can plausibly be a trough
#   gap       clear rows that must separate the trough from the mute icon

BEGIN {
  if (skip == 0) skip = 90
  if (limit == 0) limit = 400
  if (dark == 0) dark = 150
  if (light == 0) light = 230
  if (merge == 0) merge = 3
  if (minimum == 0) minimum = 20
  if (gap == 0) gap = 8
}

{ pixel[NR] = $1 }

END {
  runs = 0
  for (row = skip; row <= limit; row++) {
    if (row > NR) break
    if (pixel[row] >= dark && pixel[row] <= light) continue

    if (runs > 0 && row - previous <= merge) {
      last[runs] = row
    } else {
      runs++
      first[runs] = row
      last[runs] = row
    }
    previous = row
  }

  longest = 0
  for (run = 1; run <= runs; run++) {
    length_of_run = last[run] - first[run]
    if (length_of_run > longest) {
      longest = length_of_run
      trough = run
    }
  }

  if (longest < minimum) exit

  mute = 0
  for (run = trough + 1; run <= runs; run++) {
    if (first[run] > last[trough] + gap) {
      mute = int((first[run] + last[run]) / 2)
      break
    }
  }

  print first[trough], last[trough], mute
}
