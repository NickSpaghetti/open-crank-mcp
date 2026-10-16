# Locates the Simulator's volume slider in a one-pixel greyscale column down its
# device frame. Against the yellow frame every widget reads dark or light; the
# longest such run is the trough.
#
# Prints "top bottom mute volume", or nothing when the column doesn't look like a
# device frame, so callers leave the slider alone rather than act on a guess.
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

  # The knob's position along the track is the Simulator's volume; the SDK
  # exposes no other way to read it.
  knob_first = 0
  knob_last = 0
  for (row = first[trough]; row <= last[trough]; row++) {
    if (pixel[row] < light) continue
    if (knob_first == 0) knob_first = row
    knob_last = row
  }

  # -1 for "no knob found": 0 is a real volume, and acting on it would silence
  # the audio on a bad read.
  volume = -1
  if (knob_first > 0) {
    knob_height = knob_last - knob_first
    # The knob's centre only travels to within half its height of each end, so
    # those are the ends of the scale, not the track's edges.
    lowest = last[trough] - knob_height / 2
    highest = first[trough] + knob_height / 2
    centre = (knob_first + knob_last) / 2
    if (lowest > highest) {
      volume = (lowest - centre) / (lowest - highest)
      if (volume < 0) volume = 0
      if (volume > 1) volume = 1
    }
  }

  printf "%d %d %d %.3f\n", first[trough], last[trough], mute, volume
}
