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

## The harness is a copy, so it drifts, and that went unnoticed once

`setup` writes `mcp_harness.lua` (or the C pair) *into a game's own source
tree*. From then on the game's copy and this repo's canonical version are two
different files, and nothing used to compare them.

That produced a real silent failure during the performance review. Renaming
`mcp/game_logs.json` to `mcp/game_logs.jsonl` (an unrelated, deliberate change
- the old format rewrote the whole file on every `print()`) meant every game
that had already been set up kept writing the old filename, while the new
reader only looked for the new one and treated "file missing" as "the game
hasn't logged anything". `get_game_logs` returned an empty list, no error. Both
vendored games at the time - missile-command and scuba-sally - were byte-for-byte
on the pre-change harness, so both would have gone quiet.

**Two independent detectors now, on purpose:**

- `get_game_logs` errors if it finds `game_logs.json` and no `.jsonl`, naming
  `setup` as the fix. It does not read the old format - there is no backward
  compatibility here by choice, only a refusal to go quiet.
- `get_status` reports a `harness_warning` when the harness answering it is not
  the one this binary ships. Any drift, not just this one file.

They use different evidence (a file on disk vs. a field in a response) so
neither depends on the other being right.

**How the version works, since it is not a number anyone maintains.** Each
canonical harness source carries a placeholder that `setup` substitutes for a
truncated SHA-256 of the canonical bytes as it writes the copy. The server
hashes its own embedded copy and compares. Change any harness source and every
stamp differs automatically; there is nothing to bump and nothing to forget.
It also catches a locally hand-edited copy, which is why the warning says
"differs from the harness this server ships" rather than claiming to know
whether it is old or modified. See `internal/harness/version.go`.

One guard worth knowing about: if a harness source ever loses its placeholder,
`setup` fails loudly rather than writing an unidentifiable copy, and a unit test
asserts every embedded source still has exactly one. Without that, the detector
could quietly stop detecting - the same failure mode it exists to prevent.

**Known gap, in one direction only.** A *current* harness with an *older*
`open-crank-mcp` binary still goes quiet: the old reader wants
`game_logs.json`, a current harness deletes it, and that binary has no version
check to fall back on. Nothing in this repo can reach that combination. If your
logs come back empty and `get_status` says nothing, check that the server binary
is as new as the harness.

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

## `get_logs` and Lua `print()`/error output - `get_game_logs` exists for this

> **Superseded twice, and the second correction matters more than the first.**
> `print()` and tracebacks both do reach real stdout - but stdout is
> block-buffered, so `get_logs` shows nothing from a quiet game and loses
> whatever is still buffered when the Simulator is killed. Read "Superseded
> twice" below before relying on anything in this section.

`get_logs`'s own description used to say it returns "buffered stdout/stderr
from the Simulator child process - where `print()` output and Lua
tracebacks land." That was believed to be only half true: that it only ever
returned the Simulator process's OS-level stdout/stderr (GTK warnings,
startup messages, that sort of thing), and that Lua `print()` calls and
unhandled Lua errors during gameplay never showed up in it at all.

**Consequence, before the fix below existed:** an unhandled Lua error
inside a game's `playdate.update()` froze that game's update loop
silently. Every tool needing a harness round-trip (`get_game_state`,
`get_screenshot`, `list_entities`) then timed out, since the harness's own
per-frame command polling stopped running along with the rest of the
update loop. `get_status`/`stop_simulator` still worked fine (no harness
round-trip needed), which was the tell that the Simulator process itself
was alive and the game's own frame loop was what's stuck.

**Fixed**: `lua/mcp_harness.lua` now captures both halves into a
file-based channel (`mcp/game_logs.jsonl`), read directly by the new
`get_game_logs` tool - see "A real fix" below for how. The
`playdate.file.write` + `read_save_data` workaround described in earlier
versions of this doc is no longer necessary for new code, though it's
still a generally useful pattern for surfacing custom debug state.

### Superseded twice. Read this whole section before trusting either half

The short version, measured 2026-07-30: **Lua `print()` and unhandled tracebacks
both reach the process's real stdout, but stdout is block-buffered, so low-volume
output is invisible and `Simulator.Stop()`'s SIGKILL discards whatever is still
buffered.** So `get_logs` is not a reliable channel for either, and
`get_game_logs` stays.

The buffering is the part nobody had found, and it explains why this section has
now been wrong in both directions. Two games differing only in how much they
print, same launch path, same container:

| game prints | lines `get_logs` returned |
|---|---|
| one short line | **3** - no `Loading:`, no print output at all |
| 300 lines (~15KB) | **299** - `Loading:` plus every print |

stdout connected to a pipe is block-buffered at ~4KB by libc. Under that, a
chatty game flushes constantly and looks fine; a quiet one looks silent. And
because `Simulator.Stop()` is a hard kill (PlaydateSimulator ignores SIGTERM),
the buffer is never flushed at exit, so the trailing <4KB is lost for good.

A traceback is exactly the low-volume, just-happened case. That is why
`get_game_logs` earns its keep: the harness does open/write/close per entry, so
its content is on disk immediately.

**`stdbuf` is now used, and it buys less than hoped.** `internal/simulator.Launch`
wraps the Simulator in `stdbuf -oL` when it is available, falling back to a direct
launch when it is not (a stock macOS has no `stdbuf`, and native mode targets
macOS). Measured A/B through that exact code path: captured output went from 223
bytes - the GTK warning alone - to 288 bytes including the Simulator's own
`Loading: <pdx>` line. That is worth having, because it is the same mechanism that
hid the one message explaining a Simulator which quits during startup.

It does **not** make a Lua game's `print()` appear on that path, which was checked
rather than assumed. So `get_logs` is still not a channel to rely on for a game's
own output, and `get_game_logs` is not going anywhere. Oddly, a *shell*-launched
Simulator under `stdbuf` does show Lua `print()`; the difference from the Go launch
is unexplained and is deliberately not claimed either way. `internal/contracttest`
asserts the `Loading:` line arrives, which is the part that was measured.

**Which unhandled errors go where**, all three measured under `stdbuf`:

| error raised in | traceback on stdout | captured in `game_logs` | Simulator |
|---|---|---|---|
| `playdate.update`, no harness | yes | n/a | `Update failed, simulator paused.` |
| a callback the harness invokes, harnessed | yes | **yes, since the fix below** | keeps running |
| import / top level, no harness | yes | n/a | paused |

That middle row was a real gap, and it is now closed. `wrapUpdate` called
`mcp.update()` *outside* its own `xpcall`, and `mcp.update()` invokes the game's
`AButtonDown`/`AButtonUp` callbacks - so an error there escaped the protection that
exists to stop exactly this, the traceback landed nowhere `get_game_logs` could see
it, and the polling loop stopped for good (a ping issued afterwards was never
answered). The measured stack said so directly:

```
Update error: main.lua:11: P2-CALLBACK-ERROR
stack traceback:
	main.lua:11: in local 'fn'
	mcp_harness.lua:245: in upvalue 'updateButtonEdges'
	mcp_harness.lua:486: in field 'update'
```

**Fixed in two layers.** `callGameCallback` wraps each button callback the harness
invokes, so one broken callback no longer stops the other five buttons' edges being
computed or the pending command being answered; and `mcp.update()` itself is now
called inside an `xpcall`, as a backstop for anything else in there that runs game
code (`list_entities` reads game-defined sprite fields, for instance). A contract
test arms a throwing `AButtonDown` in the Lua fixture, presses A, and requires both
that the traceback reaches `game_logs.jsonl` and that a ping afterwards is still
answered. Reverting either layer makes it fail, which was checked - a test that
passes without the fix guards nothing.

### Superseded, 2026-07-29: `print()` **does** reach stdout

The root cause below says Lua console output never reaches the process's
real stdout on Linux/SDK 3.1.1. Re-measured during a performance review, on
Linux and SDK 3.1.1, that is not what happens. A game printing once per
frame put its output on stdout both ways it was checked:

- Through this project's own capture: 153 of the 175 lines `get_logs`
  returned were the game's own `print()` text.
- Bypassing it entirely, the same way the original investigation did -
  `PlaydateSimulator game.pdx > /tmp/out.txt 2>&1`, plain shell
  redirection, no Go in the loop - 164 of 171 lines were the game's
  `print()` output.

Both runs were inside the `shared` profile container, SDK 3.1.1 (the
Simulator's own `Release: 3.1.1` startup line), which is the configuration
the section below describes.

What has **not** been re-measured either way is the other half: whether an
*unhandled* error's traceback reaches stdout. An attempt to check that
failed for an unrelated reason (the probe game never loaded - no `Loading:`
line), so there is no evidence for or against it here. That half matters
more than the `print()` half, because a traceback is what you need at the
exact moment the game can no longer answer anything else.

Why the original conclusion was drawn is not established. It records its own
empirical detail (100KB+ of output, `stdbuf -oL`, zero bytes observed), so
the honest reading is that something differs between the two setups rather
than that the earlier work was careless. Left standing below as the record
of what was seen then.

What did **not** change as a result: `get_game_logs` and `mcp.run()` both
stay. `mcp.run()`'s value never depended on this - it is what keeps the
harness polling after the game's own frame logic throws, and the contract
test asserts exactly that. And because `mcp.run()` catches errors with
`xpcall`, a traceback in a harnessed game is handled by the harness and
never reaches the SDK's own error path at all, so the file channel is where
it lives regardless of what the SDK would have printed.

Worth revisiting: if tracebacks do reach stdout, then `get_logs` alone may
cover both halves and this channel could be retired. That is a design
decision with a real simplification behind it, not a bug, and it needs the
traceback measurement first.

### Root cause (as investigated originally; see the note above)

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

### Confirmed on macOS too

Re-checked on a real macOS install (SDK 3.1.1, not a container): a game whose
Lua called `print()` was launched straight from a shell with stdout redirected to
a file, and the file was empty. Zero occurrences, no output at all.

So this is not a Linux quirk or a container artifact. The Simulator withholds Lua
console output from the process's real stdout on both platforms, and
`get_game_logs` is required on both rather than being a workaround for one. The
procedure and its raw output are in `docs/NATIVE-PROBE.md`.

Windows is still unchecked. Windows-native is unsupported (see
`docs/ROADMAP.md`), so nothing depends on the answer.

### The fix: `get_game_logs` + `mcp.run()`

Routes around the console entirely using the same kind of file-based
channel the earlier workaround used by hand (`playdate.file.write` +
direct file read), rather than stdout:

- `lua/mcp_harness.lua` monkey-patches `print` (same pattern as
  `buttonIsPressed`/`getCrankPosition`/etc.) to also append each call to
  `mcp/game_logs.jsonl` immediately - not batched into `mcp.update()` - so
  a log written the frame before a crash still lands on disk.

  One JSON object per line, appended. It was originally one JSON array held
  in a capped in-memory ring buffer and rewritten in full on every call,
  which measured at 0.855ms per `print()` at its 200-entry steady state
  (a ~15KB re-encode plus a whole-file write, per line logged) against
  0.0117ms to append a single line - 73x, or 2.6% of a 33ms frame per
  logged line versus 0.04%, measured inside the Simulator. The cost also
  scaled with how many entries were being retained, so a game logging
  while you hunt a bug paid the most.

  Unbounded growth is still prevented, in two generations: at 256KB the
  current file is *renamed* to `mcp/game_logs.1.jsonl` and a fresh one
  starts, and `get_game_logs` reads the rotated one first. One rename, no
  data copied, so it stays O(1) - trimming a prefix instead would mean
  reading the file back, which is the cost this change removed.

  The first version truncated at the cap, and the comment in the harness
  claimed it kept "the older half". It kept none. That is worst at exactly
  the moment the log gets read, because a traceback is always appended
  *after* whatever caused it, so a crash shortly after a rotation showed
  the traceback with none of its run-up. Not a rare corner either: at
  roughly 65 bytes an entry, 256KB is about 4,000 entries, so a game
  printing once per frame rotated every ~2 minutes and reset its history to
  zero each time. `internal/contracttest` now floods a fixture past the cap
  and requires a marker printed *before* the rotation to still be readable
  afterwards - the writer's half only exists in Lua inside a real
  Simulator, so that is the only place it can be checked.
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
- The new `get_game_logs` Go tool reads `mcp/game_logs.jsonl` directly
  (`internal/tools/gamelogs.go`), the same direct-file-access pattern
  `read_save_data` uses - deliberately, so it keeps working in exactly the
  scenario it exists to diagnose.

C games don't need any of this: a C game's `printf` already reaches the
Simulator process's real stdout (no Simulator-side interception layer for
C, unlike Lua), so `get_logs` already correctly captures C-side print
debugging. An unhandled C error is a process crash, not a silent freeze -
already observable via `get_status`/`stop_simulator` showing the process
gone.

## `setup`'s C teardown had a build-success-but-runtime-broken trap

The first version of `teardown` for C projects deleted `mcp_harness.h`/
`.c` and stripped `CMakeLists.txt`'s `src/mcp_harness.c` entry
unconditionally. `CMakeLists.txt` can't use marker comments the way Lua/C
source files do (a CMake `#` comment runs to end of line, so one placed
mid-argument-list would comment out the rest of that call), so there was
no way for `teardown` to tell whether that entry was something it added
or something a human wrote by hand.

Found by running `teardown` against a copy of missile-command's C port
(hand-wired before this tool existed, so its harness references predate
any marker) and then rebuilding: the build **succeeded** - GCC links a
`SHARED` library with undefined symbols by default (no
`-Wl,-z,defs`/`--no-undefined`), so a missing `mcp_harness_init`/`_update`
reference doesn't fail at compile time - but launching the resulting
`.pdx` and calling `get_status` showed `harness_reachable: false`, and
every harness-dependent tool (`get_game_state`, `list_entities`) timed
out. A silent, build-clean runtime break, only visible by actually
launching the build and making a real MCP tool call against it, not by
checking `build_game`'s exit code.

**Fixed**: `teardownC` now does a read-only scan first
(`cHasUnmarkedHarnessReference` in `internal/setup/c.go`) across every
`.c` file for any harness reference - `#include`, `mcp_harness_init(`,
`mcp_harness_update(`, or any `mcp_get_*` input call - sitting outside a
marker block. If it finds one anywhere, `teardown` becomes a complete
no-op: `CMakeLists.txt`, every marker block, and the harness files are
all left untouched, rather than attempting a partial removal that could
land in this same inconsistent state.

## `setup` wired the harness in but never touched a C game's own input calls

Even after `mcp_harness_init`/`mcp_harness_update` were correctly wired
into a game's `eventHandler`/update callback, a C game's actual gameplay
code still called `pd->system->getButtonState`/`getCrankAngle`/etc.
directly, unmodified. Since `pd->system` is write-protected in memory in
the real Simulator, an active `press_button`/`set_crank` override can
only take effect through the `mcp_get_*` wrapper functions in
`mcp_harness.h` - never by intercepting `pd->system` itself.

Found by running `setup` against a fresh, otherwise-untouched copy of
the SDK's own "Sprite Game" example and driving it with `press_button`:
`get_status` reported `harness_reachable: true` and background entities
(enemy planes) animated normally, but the player entity never moved at
all - proving the harness/game-loop wiring worked while input overrides
silently did nothing, because nothing had ever replaced the game's own
raw SDK calls.

**Fixed**: `patchInputCalls` in `internal/setup/c.go` rewrites
`pd->system->getButtonState(...)` / `getCrankAngle()` / `getCrankChange()`
/ `isCrankDocked()` to their `mcp_get_*` equivalents, project-wide (not
just in the eventHandler/update-callback files, since input-reading code
can live anywhere). This substitution can't be marker-wrapped (same
constraint as `CMakeLists.txt` - it happens mid-expression, not as a
whole inserted line), so it's never reversed by `teardown` - see above.
Confirmed after the fix by re-running the same Sprite Game test:
`press_button` now moves the player entity correctly.

## The 10ms IPC poll interval wasn't the real latency bottleneck - the Simulator's frame rate was

`docs/ROADMAP.md` always described the Go-side poll interval in
`internal/harness/ipc.go` (`WaitForFile`/`WaitForDir`/the empty-file retry
in `WaitForResponse`) as "fast enough relative to the Simulator's own
frame rate (~30-50fps)" - a stated assumption, never actually measured
against a real game.

Stress-tested by driving 300 back-to-back `press_button`/`get_game_state`
round trips (no artificial delay between calls) against three real,
otherwise-unrelated games: the SDK's own `Asheteroids` (Lua), and both
ports of the user's own `missile-command` (Lua and C). All three - despite
being different languages, different codebases, different gameplay -
converged on nearly identical numbers at the old 10ms interval: median
~32ms, p95 ~42ms, ~30.2 calls/sec. That consistency across three unrelated
games is itself the signal: it's not something about any one game's code,
it's the Simulator's own frame period. None of the three games (nor this
project's own harnesses or fixtures) ever calls
`playdate.display.setRefreshRate(...)`, so all three run at whatever the
Simulator's built-in default is - empirically ~30fps (~33ms/frame) based
on these numbers, confirming the low end of the ROADMAP's old "~30-50fps"
guess. `mcp.update()` (Lua) / `mcp_harness_update()` (C) only ever check
for a new command once per frame, so that frame period is a hard floor no
amount of Go-side poll tuning can cross.

**First change**: lowered the interval from 10ms to 1ms (a single
`pollInterval` constant in `internal/harness/ipc.go`, previously duplicated
as three separate magic-number `time.Sleep` calls) and re-ran the identical
stress test against all three games. The median didn't move (still
frame-bound, ~33ms) - expected, since the floor is the frame period, not
this interval - but the tail measurably tightened: p95 dropped from ~42ms
to ~34ms consistently across all three games, by removing polling-detection
delay that used to occasionally stack on top of the frame wait.

**Second change, going further**: even at 1ms, the Go side was still
waking up on a timer and calling `stat()` to ask "yet?" regardless of
whether anything had actually changed - correct, but wasteful compared to
just being woken up by the kernel the instant the file actually appears.
`internal/harness/ipc.go`'s `WaitForFile`/`WaitForDir`/`WaitForResponse`
now share one `awaitPath` helper built on inotify (via the
`github.com/fsnotify/fsnotify` dependency) instead of a poll loop: arm a
watch on the target's parent directory, re-check once to close the
watch-arming race, then block on `watcher.Events` until a matching event
fires or the deadline passes. `WaitForResponse`'s "empty-file mid-write"
tolerance still works the same way, just expressed as its `check` function
requiring non-empty content rather than mere existence - a `Create` event
firing before the write lands now correctly falls through to waiting for
the following `Write` event instead of busy-spinning on "the file exists
but is empty." Re-running the identical stress test showed latency
statistically unchanged (still frame-bound, ~33ms median) - expected, this
change targets wasted CPU wakeups while waiting, not the frame-period
floor, which no IPC-side change can cross. Zero malformed/dropped responses
across all three games at either the 10ms poll, the 1ms poll, or the
inotify-based wait (600 round trips per game, per approach).

One real behavior change worth noting: `WaitForDir`/`WaitForFile` now
require their target's parent directory to already exist to arm a watch on
it (inotify watches a directory, not a not-yet-existing path). The one
caller that could hit this before the parent exists - waiting for the
Simulator's data directory to first create `mcp/` - falls back to a short
bootstrap poll (`newWatcherForExistingDir`) purely for that one-time race;
the actual per-tool-call hot path (`WaitForResponse` waiting on
`mcp/response.json`) never needs it, since `SendCommand` already creates
`mcp/` itself before every wait. The stress driver itself isn't committed
(built around the same uncommitted SDK-example/user-project fixtures every
other piece of manual verification in this project uses), so these numbers
are the record of it.

## Concurrent tool calls could cross-talk on the harness's shared IPC files - fixed

While answering a question about whether the IPC wait (above) should be
blocking or async, tracing through the MCP go-sdk itself
(`go-sdk@v1.6.1/mcp/server.go:1445`) turned up something more important
than that question: every tool call except `initialize` is dispatched
**concurrently** by the SDK's own request handler (`jsonrpc2.Async(ctx)`,
confirmed against `internal/jsonrpc2/conn.go`'s `handleAsync`) - it does
not serialize tool calls for us. Nothing in JSON-RPC prevents a client
from sending a second tool call before the first one's response arrives.

But the harness protocol (`docs/ROADMAP.md`'s IPC section) is
fixed-filename, single-outstanding-request: one `mcp/command.json` /
`mcp/response.json` pair per simulator, and `get_screenshot` reads a
second fixed path afterward (`mcp/screenshot.png` in
`lua/mcp_harness.lua:397`, `mcp/screenshot.raw` in
`c-harness/mcp_harness.c:459/465`). `internal/tools/server.go`'s
`roundTrip` only held its mutex for the brief `dataDir`/`nextID` read, not
across the actual `SendCommand`/`WaitForResponse` exchange - so two
concurrent tool calls could genuinely race on those shared files.

Confirmed against real games, not just a synthetic test: firing 10
concurrent `get_screenshot` calls against a freshly-wired Asheteroids,
missile-command (Lua), and missile-command (C) all showed the exact same
symptom - all 10 requests came back with **identical** image bytes, when
the game state is visibly changing at ~30fps and 10 real, distinct frames
were expected. A concurrent `press_button`/`get_game_state` batch
separately produced outright timeouts (one call's response silently
consumed by another, leaving the first waiting until `responseTimeout`).
A Go-level regression test (`internal/tools/concurrency_test.go`,
`TestRoundTripConcurrentCallsDoNotCrossTalk`) reproduced the same
cross-talk and timeouts 10/10 runs against a looping fake harness that
echoes each command's own `id` back.

**Fixed**: a dedicated `harnessMu sync.Mutex` on `Server`
(`internal/tools/server.go`) now serializes every interaction with the
harness's shared channel, not just the struct-field read `s.mu` already
guarded. `roundTrip` locks `harnessMu` for its own duration; `getScreenshot`
(`internal/tools/screenshot.go`) locks it itself around its *entire* body
(round trip plus the fixed-path file read afterward) via an unexported
`roundTripLocked`, since the screenshot race needed the critical section
extended past the round trip itself, not just covering it. Confirmed fixed
both ways: the Go test now passes 20/20 runs under `-race`, and re-running
the same concurrent batch against all three real games produced 10/10
distinct screenshots and zero timeouts, where before it was 1/10 distinct
and multiple timeouts.

## The Simulator will not start without PulseAudio

`SDL_AUDIODRIVER=pulseaudio` is set for the VNC and shared profiles so a game's
audio reaches the stream. The consequence is easy to miss: the Simulator treats
SDL initialisation as fatal, so with no reachable PulseAudio socket it prints

```
SDL2 could not be initalized (-1 - Could not setup connection to PulseAudio).
SDL2 is required for the Playdate Simulator to run and it will now quit.
```

and exits. Not degraded audio - no Simulator at all.

This is worth knowing because of how it presents. On a container that has been
up for a while it never happens; on one that has just started, whatever launches
a game can easily get there before `run-vnc.sh` has PulseAudio accepting
connections. The same command works or doesn't depending on timing, and the
message explaining it goes to the Simulator's own stdout, which is captured into
the server's buffer and discarded when that session ends. What you observe is a
game that launches, reports healthy, and is gone seconds later.

Hours went into theories that were all wrong: process groups and sessions,
SIGPIPE from a closed stdout pipe, Docker killing exec descendants, CPU
starvation, the framebuffer scanner, container age. The Simulator was never
dying. It was never starting.

Two things now guard it. `cmd/shared-load` waits for `pactl info` to succeed
before launching anything, rather than waiting only for the container to accept
execs. And `launch_simulator` checks the process is still alive shortly after
starting it, returning the captured output if it isn't, so the message that
explains the failure reaches whoever asked.
