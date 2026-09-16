---
name: setup-game
description: Wire the MCP harness into a Playdate game, build it, and launch it in the Simulator, stopping at the first step that did not actually work.
disable-model-invocation: true
---

# Set up a game

Take the game directory from `$ARGUMENTS`. If it is empty, ask for one rather
than guessing — `setup` writes into a source tree, so the wrong path is not a
harmless mistake.

Run these in order and stop at the first that fails:

1. `setup` with `source_dir` set to that directory. It detects Lua, C or hybrid,
   copies the harness in, and patches the entry point.

   **Read `manual_steps` in the response before continuing.** For C games some
   steps cannot be automated confidently — finding the right `PlaydateAPI`
   pointer in the update callback, most often. If `manual_steps` is non-empty,
   report exactly what is left and stop. Building on top of a half-wired harness
   produces a game that runs and ignores every input, which is a much more
   confusing thing to debug than the message you would have reported.

2. `build_game` with the same `source_dir`. It returns compile errors and
   warnings either way; report them rather than only the pass/fail.

3. `launch_simulator` with the `.pdx` path `build_game` returned. Use that path
   rather than constructing one.

4. `get_status`. Require **both** `running: true` and `harness_reachable: true`.

`harness_reachable: false` is the one worth pausing on: the game is running and
nothing is wired, so every later tool would time out five seconds at a time. It
usually means step 1 left `manual_steps` that were not done, or the game's update
loop never calls the harness. Do not proceed to playtesting until it is true.

If `get_status` reports a `harness_warning`, the game's vendored harness copy
does not match the server's. Say so and offer to re-run `setup`, which rewrites
it.

Report what happened at each step, including the compile warnings.
