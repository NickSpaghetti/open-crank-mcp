# Wiring a game into the harness by hand

What the `setup` tool does under the hood.

You do not need this for most projects. `setup` automates all of it, and
[setting-up-a-game.md](setting-up-a-game.md) is the path to start from. Read
this to understand the wiring, to resolve a `manual_steps` entry, or to wire a
game up yourself.

wiring, resolving a `manual_steps` entry, or wiring a game up yourself.

There's no package manager for Playdate projects. `pdc` only ever
compiles what's physically under your own `Source/` directory, and
CMake's `add_library`/`add_executable` need locally-relative source
paths. So integrating either harness is a two-step copy-then-call, not
just a call:

**Lua games:**
1. Copy [`lua/mcp_harness.lua`](../lua/mcp_harness.lua) into your project's
   `Source/` directory.
2. In `main.lua`: `import "mcp_harness"`, write your per-frame logic as a
   plain function, and pass it to `mcp.run(yourUpdateFn)` once. Instead
   of assigning `playdate.update` yourself. This is what makes
   `get_game_logs` (see below) actually reliable: `mcp.run` wraps your
   function in `xpcall`, so the harness's own command polling keeps
   running every frame even if your code throws, instead of the whole
   update loop freezing silently. See
   [`lua/test-fixture/Source/main.lua`](../lua/test-fixture/Source/main.lua)
   for a minimal, complete example. (Manually assigning `playdate.update`
   and calling `mcp.update()` yourself each frame still works, for
   backward compatibility, but doesn't get this protection.)
3. `mcp.run` captures both `print()` output and unhandled-error tracebacks
   into `mcp/game_logs.jsonl` automatically; read them back with the
   `get_game_logs` tool. Tracebacks are the reason it matters. `mcp.run`
   catches them with `xpcall`, so they never reach the Simulator's console
   at all. `print()` output *does* also reach the Simulator's real stdout
   and so shows up in `get_logs` too, contrary to what this README and
   [`docs/GOTCHAS.md`](../docs/GOTCHAS.md) used to claim; `get_game_logs` is
   still the better read, since it gives one timestamped stream of just
   your game's output rather than your lines mixed into the Simulator's.
4. `press_button` synthesizes real button-down/up edges, so
   `buttonJustPressed`/`buttonJustReleased` and the SDK's
   `AButtonDown`/`leftButtonDown`/etc callbacks all fire correctly from
   an MCP-driven press, not just `buttonIsPressed`'s "currently held"
   bit. `AButtonHeld`/`BButtonHeld` (fired after a continuous 1-second
   hold) are a separate mechanism and aren't synthesized.
5. `set_crank` leaves the crank where you put it. Omit `duration_ms` and the
   override holds until another `set_crank` replaces it, which is what a real
   crank does. Pass one to have it lapse back to the game's own reading after
   that many milliseconds. This used to be the other way round: an omitted
   duration was read as a zero-length one, so the override was created and
   expired before any frame could read it, and `set_crank` reported success
   while the game saw nothing.
6. `set_crank` overrides the angle and delta, and only touches the docked
   state if you ask it to with `crank_dock` (`"docked"` or `"undocked"`).
   Omit it and `playdate.isCrankDocked()` keeps reading whatever the game
   would really see, which is *docked*, in the Simulator at rest. Before
   this was a three-valued field it was a bool, so every `set_crank` call
   silently forced the crank to read undocked.

**C games:**
1. Copy [`c-harness/mcp_harness.h`](../c-harness/mcp_harness.h) and
   [`c-harness/mcp_harness.c`](../c-harness/mcp_harness.c) into your
   project, and add `mcp_harness.c` to your CMakeLists.txt's source list
   (alongside your own `.c` files, in the same `add_library`/
   `add_executable` call). If the pair lands in `src/` and any of your own
   sources live outside it, add `include_directories(src)` too, which is
   what `setup` does. A bare `#include "mcp_harness.h"` only resolves
   against the including file's own directory before falling back to the
   `-I` search path, so a project keeping `main.c` at its root will not
   find a header that only exists in `src/`.
2. In your `eventHandler`: `#include "mcp_harness.h"`, call
   `mcp_harness_init(pd)` once on `kEventInit`, and call
   `mcp_harness_update(pd)` once per frame from your update callback. See
   [`c-harness/test/fixture-game/`](../c-harness/test/fixture-game/) for a
   minimal, complete example. Note its `src/mcp_harness.{h,c}` are copied
   in at test time by `internal/contracttest`, not committed there
   permanently. Copy from the canonical `c-harness/` location instead.
3. A C game must call `mcp_get_button_state`/`mcp_get_crank_angle`/
   `mcp_get_crank_change`/`mcp_get_crank_docked` instead of
   `pd->system->getButtonState`/etc. directly for input overrides to take
   effect. `pd->system` is genuinely write-protected in memory in the
   real Simulator, so unlike Lua's harness (which can transparently
   monkey-patch the mutable `playdate` table), C can't intercept those
   calls at the source. See the design note in
   [`c-harness/mcp_harness.h`](../c-harness/mcp_harness.h) for why.
4. `mcp_get_button_state`'s `pushed`/`released` outputs synthesize real
   edges from an active override, not just the `current` "currently
   held" bit. A game reading `pushed & kButtonA` for a one-shot
   action (fire, jump) works correctly from an MCP-driven press.

Either way, the game builds through the SDK's own CMake support
(`cmake -S . -B build && cmake --build build`, which invokes `pdc` itself
as a post-build step) or plain `pdc` for Lua-only projects. The container image
already has `cmake` and `build-essential`; natively they are yours to install,
and only a C game needs them. Either way, no ARM toolchain: this project only
targets the Simulator, never real hardware, so a C game builds as a shared
library with the host compiler.

**Hybrid C+Lua games** (C for hot loops, Lua for UI, an officially
supported pattern) can use the Lua harness alone, since a real Lua VM is
still running.


## Why teardown refuses to do a partial job

The paired `teardown` tool reverses this: strips exactly what `setup`
added and removes the copied harness files. It's deliberately
conservative. If it finds any harness reference (an `#include`, a
`mcp_harness_init`/`_update` call, or, for C, any `mcp_get_*` input call)
that it can't confidently attribute to its own insertion, it leaves the
whole project untouched rather than risk a partial, inconsistent
teardown. In practice this means:

- A project hand-wired before `setup` ever touched it (or edited by hand
  since) makes `teardown` a full no-op.
- For C, `setup` also rewrites your own `pd->system->getButtonState`/
  `getCrankAngle`/`getCrankChange`/`isCrankDocked` calls in place to
  their `mcp_get_*` equivalents (`pd->system` is write-protected in the
  real Simulator, so overrides can only take effect through those
  wrapper functions, see below). That rewrite can't be marked or
  reversed the way a whole-line insertion can, so once `setup` has
  touched a C project's input calls, `teardown` for it is permanent from
  then on. It becomes a no-op rather than leaving input calls pointing
  at wrapper functions that no longer exist.
- An input call `setup` finds but cannot rewrite safely comes back as a
  `manual_steps` entry naming the file, the line and the wrapper to use.
  A receiver it cannot reach (`api()->pd->system->getButtonState(...)`)
  and spacing it does not accept (`pd -> system -> getCrankAngle()`) both
  land there. That matters because the alternative is silence: an
  unrewritten call means `press_button` and `set_crank` do nothing to it
  while `setup` reports success.
- Only part of `CMakeLists.txt` can use marker comments. The
  `include_directories(src)` line `setup` adds is a statement of its own,
  so it is wrapped in `# BEGIN MCP HARNESS` markers and reversed by them.
  The `src/mcp_harness.c` source entry can't be, because a CMake `#`
  comment runs to end of line and one placed mid-argument-list would
  comment out the rest of that call. That entry is recognized as a whole
  argument instead, so a prefixed path like
  `${CMAKE_CURRENT_SOURCE_DIR}/src/mcp_harness.c` is removed intact
  rather than truncated down to its dangling prefix.
