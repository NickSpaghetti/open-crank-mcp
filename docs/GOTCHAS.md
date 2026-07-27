# Gotchas

Real, load-bearing behavior that isn't obvious from the tool descriptions
or the SDK docs, found by actually using this project to build a game
(missile-command), not by reading source ahead of time.

## `get_logs` doesn't capture Lua `print()`/error output

`get_logs`'s own description says it returns "buffered stdout/stderr from
the Simulator child process - where `print()` output and Lua tracebacks
land." In practice, it only returns the Simulator process's OS-level
stdout/stderr (GTK warnings, startup messages, that sort of thing). Lua
`print()` calls and unhandled Lua errors during gameplay do not show up
in it at all.

**Consequence:** an unhandled Lua error inside a game's `playdate.update()`
freezes that game's update loop silently. Once that happens, every tool
that needs a harness round-trip (`get_game_state`, `get_screenshot`,
`list_entities`) times out, since the harness's own per-frame command
polling stops running along with the rest of the update loop. `get_logs`
itself returns cleanly (it doesn't depend on the harness), and shows
nothing useful, since the error was never captured anywhere it can see.
`get_status`/`stop_simulator` also still work fine (no harness round-trip
needed), which is the tell that the Simulator process itself is alive and
the game's own frame loop is what's stuck.

**Workaround, found while building missile-command:** bypass the harness
entirely. Have the game write debug state directly to a file with
`playdate.file.write` each frame, and read it back with `read_save_data`
(direct file access, no harness round-trip). This works even when the
harness/update loop is the thing that's frozen, since it doesn't depend
on either.

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

So this is not a bug in `internal/simulator.go`'s `os/exec` capture
(`cmd.Stdout`/`cmd.Stderr` redirection is correct and standard). There is
simply nothing arriving on that pipe to capture, for Lua console content,
ever. `get_logs`'s description should be corrected to stop promising Lua
tracebacks, since that promise can't be kept on this platform.

### A real fix exists, just not through `get_logs`

Route around the console entirely using the same file-based channel that
already works (`playdate.file.write` + direct file read), rather than
stdout. Concretely: add a wrapper to `lua/mcp_harness.lua` that a game
calls around its own per-frame logic, using `xpcall`/`debug.traceback` to
catch an error and write it to a fixed file (e.g. `mcp/error.json`) in the
data directory. A new tool (or an extension to `get_status`) could then
read that file directly, the same way `read_save_data` already does direct
file access bypassing the harness/console. This would surface the same
traceback text found manually while building missile-command,
automatically, without depending on PlaydateSimulator's console behavior
at all. Not yet implemented. This is a design decision (it changes the
harness's calling convention) worth deciding on deliberately rather than
folding into an unrelated change.
