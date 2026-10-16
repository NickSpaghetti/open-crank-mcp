# Wiring a game into the harness by hand

What the `setup` tool does under the hood.

Most projects do not need this. `setup` automates all of it, and
[setting-up-a-game.md](setting-up-a-game.md) is the place to start. Read this to
understand the wiring, to resolve a `manual_steps` entry, or to wire a game up
yourself.

There is no package manager for Playdate projects. `pdc` only compiles what is
under your own `Source/` directory, and CMake's `add_library`/`add_executable`
need local source paths. So either harness is copied in, then called.

**Lua games**

1. Copy [`lua/mcp_harness.lua`](../lua/mcp_harness.lua) into your project's
   `Source/` directory.
2. Add `import "mcp_harness"` to `main.lua`. Placement does not matter. The
   harness wraps `playdate.update` whether you assign it before or after the
   import, and runs your update and its own command handling each under
   `xpcall`, so an error in your code cannot stop it answering. `mcp.run(fn)`
   does the same thing explicitly, if you prefer to pass your update function
   in. See [`lua/test-fixture/Source/main.lua`](../lua/test-fixture/Source/main.lua)
   for a complete example.
3. The harness captures `print()` output and uncaught-error tracebacks into
   `mcp/game_logs.jsonl`. Read them with `get_game_logs`. See
   [reading-the-logs.md](reading-the-logs.md).
4. `press_button` synthesizes real button-down and button-up edges, so
   `buttonJustPressed`, `buttonJustReleased` and the `AButtonDown`/`AButtonUp`
   style callbacks all fire. `AButtonHeld`/`BButtonHeld` are not synthesized.
5. `hold_button` holds with no expiry and `release_button` ends it. A held button
   fires `AButtonDown` once going down and `AButtonUp` once on release, not once
   per frame. `reset_input` drops every override and produces the same release
   edges.
6. `set_crank` with no `duration_ms` holds until another `set_crank` or
   `reset_input`. Pass a duration to have it lapse back to the game's own
   reading.
7. `set_crank` only changes the docked state if you pass `crank_dock`
   (`docked` or `undocked`). Omit it and `playdate.isCrankDocked()` reads what
   the game would really see, which in the Simulator at rest is docked.

**C games**

1. Copy [`c-harness/mcp_harness.h`](../c-harness/mcp_harness.h) and
   [`c-harness/mcp_harness.c`](../c-harness/mcp_harness.c) into your project,
   and add `mcp_harness.c` to the same `add_library`/`add_executable` call as
   your own sources. If the pair lands in `src/` and any of your sources live
   outside it, add `include_directories(src)` too, as `setup` does. A bare
   `#include "mcp_harness.h"` only looks next to the including file before the
   `-I` path, so a `main.c` at the project root will not find a header in
   `src/`.
2. In your `eventHandler`, `#include "mcp_harness.h"`, call
   `mcp_harness_init(pd)` once on `kEventInit`, and call
   `mcp_harness_update(pd)` once per frame from your update callback. See
   [`c-harness/test/fixture-game/src/main.c`](../c-harness/test/fixture-game/src/main.c)
   for a complete example. Copy the harness from `c-harness/`, not from the
   fixture, whose copy is placed there at test time.
3. Read input through `mcp_get_button_state`, `mcp_get_crank_angle`,
   `mcp_get_crank_change` and `mcp_get_crank_docked` instead of
   `pd->system->getButtonState` and friends. `pd->system` is write-protected in
   the Simulator, so unlike Lua, C cannot intercept those calls at the source.
   The design note in [`c-harness/mcp_harness.h`](../c-harness/mcp_harness.h)
   has the detail.
4. `mcp_get_button_state` synthesizes `pushed` and `released` edges from an
   override, so a game reading `pushed & kButtonA` for a one-shot action works
   from an MCP-driven press.

Either way the game builds through the SDK's CMake support
(`cmake -S . -B build && cmake --build build`, which runs `pdc` as a post-build
step) or plain `pdc` for Lua-only projects. The container image has `cmake` and
`build-essential`. Natively they are yours to install, and only a C game needs
them. No ARM toolchain is involved: this project targets the Simulator only, so
a C game builds as a shared library with the host compiler.

**Hybrid C and Lua games** use the Lua harness alone, since a Lua VM is still
running.

## Why teardown refuses to do a partial job

`teardown` strips what `setup` added and removes the copied harness files. If it
finds any harness reference (an `#include`, a `mcp_harness_init`/`_update` call,
or for C any `mcp_get_*` input call) that it cannot attribute to its own
insertion, it leaves the whole project untouched. In practice:

- A project wired by hand, or edited by hand after `setup`, makes `teardown` a
  no-op.
- For C, `setup` rewrites your `pd->system->getButtonState`, `getCrankAngle`,
  `getCrankChange` and `isCrankDocked` calls in place to their `mcp_get_*`
  equivalents (see point 3 under **C games**). That rewrite cannot be marked or
  reversed the way an inserted line can, so once `setup` has touched a C
  project's input calls, `teardown` is a no-op for it. The alternative is input
  calls pointing at functions that no longer exist.
- An input call `setup` finds but cannot rewrite safely comes back as a
  `manual_steps` entry naming the file, the line and the wrapper to use. A
  receiver it cannot reach (`api()->pd->system->getButtonState(...)`) and
  spacing it does not accept (`pd -> system -> getCrankAngle()`) both land
  there. Left alone, that call ignores `press_button` and `set_crank` while
  `setup` reports success.
- Only part of `CMakeLists.txt` can carry marker comments. The
  `include_directories(src)` line is a statement of its own, so it is wrapped in
  `# BEGIN MCP HARNESS` markers. The `src/mcp_harness.c` source entry cannot be,
  because a CMake `#` comment runs to end of line and would comment out the rest
  of the argument list. That entry is matched as a whole argument instead, so a
  prefixed path like `${CMAKE_CURRENT_SOURCE_DIR}/src/mcp_harness.c` is removed
  intact.
