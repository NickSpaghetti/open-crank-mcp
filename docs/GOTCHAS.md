# Gotchas

Real, load-bearing behavior that isn't obvious from the tool descriptions
or the SDK docs, found by actually using this project to build a game
(missile-command), not by reading source ahead of time.

## `press_button` only faked "currently held" state - fixed, real edges now synthesized

`buttonJustPressed`/`buttonJustReleased` (Lua) and the `pushed`/`released`
bitmasks from `mcp_get_button_state` (C) used to pass through unmodified
from real hardware, regardless of an active override - only
`buttonIsPressed`/the `current` bitmask were fakeable. Discovered while
trying to wire the harness into the Playdate SDK's own `Asheteroids`
example: its turn/thrust/shoot controls are implemented entirely through
the SDK's button-down/up event callbacks (`playdate.leftButtonDown()`,
`upButtonDown()`, `BButtonDown()`, etc - "the following functions in your
script when input events occur" per the SDK docs), which the old override
couldn't trigger at all, since those reflect a real hardware edge the
Simulator's own runtime decides to dispatch - a harder blocker than the
`buttonJustPressed` gap alone, not just missing convenience.

**Fixed**: both harnesses now track each button's previous
override-effective state and synthesize a real pushed/released edge
whenever it's caused by an active (or just-expired) override -
`mcp_override_update_edges` in `c-harness/mcp_harness.c`,
`updateButtonEdges` in `lua/mcp_harness.lua`. The Lua harness additionally
calls the matching `*ButtonDown`/`*ButtonUp` callback directly, since
those aren't reachable through `buttonJustPressed` alone. One frame of
latency between a press/release command and its edge becoming visible,
by design - the edge always reflects the *previous* frame's override
state, so it doesn't matter whether a game reads input before or after
calling `mcp_harness_update`/`mcp.update` that same frame.
`AButtonHeld`/`BButtonHeld` (fired after a continuous 1-second hold) are
a separate mechanism, not synthesized - out of scope, no example needed
it.

## `read_save_data`'s schema broke every tool call from Claude Code

`ReadSaveDataOutput.Data` is `any` (a save file's shape isn't known ahead
of time), which produces the JSON Schema value `"data": true` for that
property - spec-legal ("any value is valid"), but Claude Code's
client-side tool-schema validator rejects a bare boolean there, and
failing to validate *one* tool's schema aborts the *entire* `tools/list`
fetch. Every tool was unusable from Claude Code, not just
`read_save_data` - found by actually registering this server with
`claude mcp add` and checking `claude mcp list`, not by reading the go-sdk
docs (which say a bare `true` is spec-compliant, and it is).

**Fixed**: `read_save_data`'s tool registration in
`internal/tools/server.go` now supplies an explicit `OutputSchema`,
built from `jsonschema.For` with a `TypeSchemas` override for the `any`
field. An empty override schema doesn't work by itself -
`jsonschema-go`'s `Schema.MarshalJSON` deliberately collapses any
all-empty schema to `true` as a spec-legal shorthand, so the override
needs real content (a `Description`) to survive as an actual schema
object instead of collapsing right back to the same `true`.

## `get_logs` doesn't capture Lua `print()`/error output - fixed via `get_game_logs`

`get_logs`'s own description used to say it returns "buffered stdout/stderr
from the Simulator child process - where `print()` output and Lua
tracebacks land." That was only half true: it only ever returned the
Simulator process's OS-level stdout/stderr (GTK warnings, startup
messages, that sort of thing). Lua `print()` calls and unhandled Lua
errors during gameplay never showed up in it at all. `get_logs`'s
description has been corrected to say so plainly.

**Consequence, before the fix below existed:** an unhandled Lua error
inside a game's `playdate.update()` froze that game's update loop
silently. Every tool needing a harness round-trip (`get_game_state`,
`get_screenshot`, `list_entities`) then timed out, since the harness's own
per-frame command polling stopped running along with the rest of the
update loop. `get_status`/`stop_simulator` still worked fine (no harness
round-trip needed), which was the tell that the Simulator process itself
was alive and the game's own frame loop was what's stuck.

**Fixed**: `lua/mcp_harness.lua` now captures both halves into a
file-based channel (`mcp/game_logs.json`), read directly by the new
`get_game_logs` tool - see "A real fix" below for how. The
`playdate.file.write` + `read_save_data` workaround described in earlier
versions of this doc is no longer necessary for new code, though it's
still a generally useful pattern for surfacing custom debug state.

### Root cause (confirmed)

PlaydateSimulator's own Lua console output does not go through the
process's real stdout/stderr file descriptors at all, on Linux, in this
SDK version (3.1.1). This contradicts the SDK's own claim.
`Inside Playdate.html` states: "Printed text is also copied to stdout,
which is helpful if you run the simulator from the command line." That's
empirically false for this build. Lua `print()`/error text is rendered
only into the Simulator's internal GUI console widget, through a code
path that never touches fd 1/2, headless or not. `PlaydateSimulator -h`
exposes no relevant flag (its only flags are `-h`/`--help`); no
environment variable changes this; no on-disk log file contains Lua
console text either (checked `~/.config/Playdate Simulator/`,
`~/.local/share/recently-used.xbel`, and the Sentry/crashpad crash
reporter's own files, none of which carry Lua output). Confirmed via
direct empirical testing (not just reading code):

- A deliberate Lua error, and separately 100KB+ of plain `print()` output
  over 15 seconds, both produced zero bytes on the raw process stdout,
  checked with plain shell redirection (bypassing this project's own Go
  capture code entirely, ruling out a bug in `internal/simulator`).
- Forcing line-buffering with `stdbuf -oL -eL` (which overrides whatever
  buffering mode the app itself requests) still produced zero Lua output,
  while it *did* immediately surface the Simulator's own native
  `"Loading: ..."` startup diagnostic. This proves the native/C output
  path and the Lua console path are genuinely separate channels, not the
  same stream just buffered differently.
- This isn't specific to crash conditions: an error-free fixture with
  plain `print()` calls showed the identical symptom.

So this was never a bug in `internal/simulator.go`'s `os/exec` capture
(`cmd.Stdout`/`cmd.Stderr` redirection is correct and standard). There is
simply nothing arriving on that pipe to capture, for Lua console content,
ever - a real, permanent platform limitation to route around, not a
transient bug to wait out.

### The fix: `get_game_logs` + `mcp.run()`

Routes around the console entirely using the same kind of file-based
channel the earlier workaround used by hand (`playdate.file.write` +
direct file read), rather than stdout:

- `lua/mcp_harness.lua` monkey-patches `print` (same pattern as
  `buttonIsPressed`/`getCrankPosition`/etc.) to also append each call into
  a capped in-memory ring buffer, flushed to `mcp/game_logs.json`
  immediately on every call - not batched into `mcp.update()` - so a log
  written the frame before a crash still lands on disk.
- A new `mcp.run(gameUpdateFn)` replaces the old pattern of assigning
  `playdate.update` directly and calling `mcp.update()` manually at the
  end. It wraps the game's frame logic in `xpcall`/`debug.traceback`,
  appends any caught error's traceback to the same ring buffer, and -
  critically - always calls `mcp.update()` afterward regardless of
  whether the game's own logic threw. This is what actually fixes the
  freeze: the harness's own polling loop (and every tool depending on it)
  keeps working even when the game's own code has a bug. Calling
  `mcp.update()` manually still works for backward compatibility, it just
  doesn't get this protection.
- The new `get_game_logs` Go tool reads `mcp/game_logs.json` directly
  (`internal/tools/gamelogs.go`), the same direct-file-access pattern
  `read_save_data` uses - deliberately, so it keeps working in exactly the
  scenario it exists to diagnose.

C games don't need any of this: a C game's `printf` already reaches the
Simulator process's real stdout (no Simulator-side interception layer for
C, unlike Lua), so `get_logs` already correctly captures C-side print
debugging. An unhandled C error is a process crash, not a silent freeze -
already observable via `get_status`/`stop_simulator` showing the process
gone.
