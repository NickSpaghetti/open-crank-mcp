# Roadmap

## Vision

An MCP server that lets an AI agent play a Playdate game in the desktop
Simulator. See the screen, press buttons, turn the crank, read game state,
read logs. Playtest, debug, and help with level design instead of only
reading and writing source code blind.

## Design decisions

**Language: Go**, using the official `github.com/modelcontextprotocol/go-sdk`.
Cross-platform reach (Windows 11, macOS, Linux) through `GOOS`/`GOARCH`
cross-compilation to small, dependency-free static binaries. No runtime to
install on the end user's machine. The actual workload (subprocess
management, ~10ms file polling, JSON/PNG shuttling) is I/O-bound. Not chosen
for raw throughput over C#/.NET. Chosen for the simpler distribution story
across three OSes.

**Input and screenshots go through a Lua harness, not OS-level automation.**
No documented Playdate API injects button or crank input from outside the
Simulator process. `playdate.buttonIsPressed`, `getCrankPosition`, and
friends are read-only getters. The only way to fake input is to monkey-patch
those functions from inside the running Lua game. A small harness file
(`lua/mcp_harness.lua`) drops into the user's game: one `import`, one
per-frame call. It talks to the Go server over file-based command/response
JSON. Chosen over OS-level window screenshotting and synthetic keystrokes
because it uses only documented SDK APIs, needs no extra host tooling, and
gives real framebuffer screenshots (`playdate.graphics.getDisplayImage()`)
plus real input simulation.

**No "eval arbitrary Lua" tool, for now.** Considered and rejected. Real
code-execution risk inside the running game process. Revisit later behind
an explicit opt-in flag if a concrete need comes up.

**C games get their own harness, alongside the Lua one.** Playdate games can
be written in C, compiled for the Simulator target into a native shared
library (`pdex.so`/`.dylib`/`.dll`) through the SDK's CMake build support.
`pdc` packages it the same as a Lua project. A pure-C game has no Lua VM
running at all, so `mcp_harness.lua` has nothing to load. It needs a C
equivalent (`c-harness/mcp_harness.c` + `mcp_harness.h`) added to the game's
own build, exposing the same file-based command/response protocol through
`playdate->file->*`. The Go server's IPC client needs zero changes to
support C games. The protocol boundary is just JSON files on disk, and
neither harness's language matters on that side.

Input override does **not** work the same way as Lua's monkey-patching, and
that was a real, live bug, not a design choice made up front. `pd->system`
is typed as `const struct playdate_sys*`, and the original plan was to cast
that const away and overwrite `pd->system->getButtonState` etc. directly,
mirroring what Lua does. That compiles fine, but segfaults immediately in
the real Simulator — verified directly, not theorized: Panic's function
table isn't just nominally read-only, it lives in memory that's actually
write-protected there. Casting away const only avoids undefined behavior
when the underlying object wasn't truly declared const at its point of
definition; here it apparently is. So the C harness instead exposes
explicit query functions (`mcp_get_button_state(pd, ...)`,
`mcp_get_crank_angle(pd)`, `mcp_get_crank_change(pd)`,
`mcp_get_crank_docked(pd)`) that call the real, untouched
`pd->system->*` function and apply the override on top before returning.
A C game has to call *these* instead of `pd->system->getButtonState`
directly for overrides to take effect - a real, load-bearing difference
in integration effort between the two harnesses, not just an
implementation detail. Lua's `playdate` table is a genuinely mutable Lua
table (no read-only-memory equivalent), so its monkey-patch still works
transparently as originally designed.

Hybrid C+Lua games (C for hot loops, Lua for UI, an officially supported
pattern, see the SDK's "3D library", "Exposure", and "Particles" C examples)
still run a real Lua VM. The existing Lua harness works for those unchanged.
The C harness is only needed for pure-C, no-Lua projects.

Verified end to end: the SDK's bundled "Hello World" C example builds via
`cmake -S . -B build && cmake --build build` inside the container. No ARM
toolchain needed, only `cmake` and `build-essential`, since this project
only ever targets the Simulator, never real hardware. The resulting `.pdx`
runs cleanly under Xvfb.

**C harness data structures use "fat structs" (Casey Muratori's approach,
requested directly).** For a family of related things - here, commands and
responses - use one flat struct with every field any variant might need,
rather than a tagged union or a struct per variant. `McpCommand` and
`McpResponse` (`c-harness/mcp_harness.h`) carry every field any command or
response type needs; a `ping` leaves most of them at zero, and that's an
accepted trade. `McpOverrideState` gets the same flat treatment instead of
a per-button object. The payoff: every instance has an identical memory
layout, so dispatch code just reads fields directly instead of
tag-then-cast. Muratori's framing: splitting into tighter, variant-specific
types is "compression," and premature compression (guessing the right
split before real usage data exists) costs more in code complexity than it
saves in memory - compress later, if a real need shows up. The same
reasoning is why the JSON wire protocol itself (see Architecture) is one
flat shape across every command type too, rather than a schema that varies
per type - that's what lets the Lua table shape, the C struct shape, and
the future Go struct shape all agree on one schema without three different
ideas of "shape."

The Go leg of that sentence was not built until a performance and readability
review went looking for it, and the gap is worth recording rather than quietly
closing. The C harness had `McpCommand`/`McpResponse` from Checkpoint 2 and the
Lua harness had `emptyResponse()`, but `internal/tools` spoke `map[string]any`
in both directions with three unchecked type-assertion helpers
(`asFloat`/`asString`/`asBool`), so "all three agree on one schema" described
two implementations and an intention. Three defects were living in that gap,
all three confirmed against a real game (missile-command, both ports) rather
than argued from the code:

- `status` and `error` were produced carefully by both harnesses and read by
  nothing. Every tool reported success no matter what the harness said.
- The `id` was written on every command and never compared to the response's,
  so the stale-response detection this document already claimed - "a stale
  leftover response from a previous run is easy to detect and ignore" - did not
  exist. One round trip that outlived its five-second timeout left its answer
  on disk and the next call returned it as its own: `set_crank` timed out at
  5.005s, and the following `press_button` consumed its answer in 3ms. A
  planted response came back as `get_game_state`'s answer in 2ms, carrying the
  wrong payload, with its `status: "error"` dropped on the way through.
- `button` was validated at none of the three layers, so `press_button("A")`
  reported success and did nothing.

Fixed by giving Go the same flat shape: `internal/harness/protocol.go` holds
one `Command` and one `Response`, `SendCommand` clears any stale response
before writing, and `WaitForResponse` takes the id it is waiting for and
discards anything else. The field names in it were captured off the wire from
both harnesses rather than copied from this document.

One wrinkle the id check had to accommodate, and it is exactly the kind of
thing only running it finds: when the C harness fails to parse a command it
answers with an empty id, because `mcp_parse_command` bails before the id is
read. Rejecting non-matching ids naively would have turned every C-side parse
failure into a five-second timeout instead of the error message it was trying
to return, so an empty id is accepted as uncorrelatable rather than treated as
a mismatch.

The flat shape reaches the *wire*, not just the three structs: no field on a
command is `omitempty`, so a ping sends every field zeroed, exactly as this
decision describes. The first version of the Go struct did use `omitempty`, which
meant `set_crank(crank_angle: 0)` sent no `crank_angle` at all and "explicitly
zero" was indistinguishable from "unset". It behaved correctly only because both
harnesses independently default a missing field to zero - C memsets before
parsing, Lua reads `cmd.crank_angle or 0` - so a Go marshaling choice was resting
on an invariant implemented twice, in two other languages, and written down in
none of the three. `crank_docked` is the one that makes the risk concrete: a real
Playdate's crank is docked when idle, so a harness defaulting a missing
`crank_docked` to true would be a reasonable thing for someone to write, and
`set_crank(crank_docked: false)` would then mean its opposite. The harness-side
defaults stay as tolerance for anything writing `command.json` by hand; nothing
in Go relies on them.

The same tag on the *tool* input structs is left alone and is not the same
decision: there it only controls whether jsonschema-go marks a property
`required`, and optional is right, since asking to turn the crank to 90 degrees
should not oblige a caller to name a delta, a dock state and a duration.

**The crank's dock state was a live bug, and finding it needed a measurement.**
Removing `omitempty` would not have fixed it, because the problem was the type: a
bool has two states and the protocol needs three - dock it, undock it, leave it
alone. So every `set_crank` call sent a dock state whether the caller mentioned one
or not. That only matters if the Simulator's resting dock reading is *docked*, and
it is: `internal/contracttest` now asserts that at rest the crank reports docked,
which means the old shape was silently forcing every harnessed game to read the
crank as undocked on any call that meant to change only the angle. A game gating
behaviour on `playdate.isCrankDocked()` was being told something nobody asked for.
That assertion exists so a future SDK changing this fails loudly rather than
quietly making the regression test meaningless.

The wire now carries `crank_dock`, one of `unchanged`/`docked`/`undocked`, and the
override state in both harnesses grew a separate "was the dock asked about at all"
flag alongside the value. A string rather than the obvious value-plus-flag pair of
booleans: that pair has four states for three meanings, so one combination is
nonsense every reader must know to ignore, and the C harness's `strstr` key lookup
would have distinguished `crank_docked` from `crank_docked_set` only by the closing
quote in the pattern. One self-describing field avoids both, and it is the same
vocabulary the tool takes, so `command.json` reads the way the caller wrote it.

**The harness is a copy, and copies drift.** The review's own game-log format
change (see `docs/GOTCHAS.md`) broke every already-set-up game silently, because
`setup` writes the harness *into* a game's source tree and nothing compared the
two afterwards. The durable lesson is not about a filename: it is that this
project ships code by copying it, so any harness/server contract change has a
silent failure available to it, and only a version marker closes that class.

The marker is not a number anyone maintains. Each canonical harness source
carries a placeholder that `setup` substitutes for a truncated SHA-256 of the
canonical bytes as it writes the copy; the server hashes its own embedded copy
and compares, warning through `get_status` when they differ. Change any harness
source and every stamp changes automatically - nothing to bump, nothing to
forget, and a hand-edited copy is caught too. A hand-maintained integer was
considered and rejected for exactly the reason the bug happened: remembering to
bump it is the failure mode. `internal/harness/version.go`, and the guard that
keeps it honest is `setup` refusing to write an unstamped copy plus a test that
every embedded source still carries its placeholder.

Backward compatibility was explicitly declined - one developer, and the log is a
regenerated debug buffer - so the old format is not read. Being *silent* was the
defect, not being incompatible: `get_game_logs` now errors and names the remedy.
One direction stays uncovered and is recorded rather than papered over: a current
harness against an older server binary, which nothing here can reach.

**Two things the log investigation turned up, both fixed.** The Simulator is now
launched through `stdbuf -oL` where available, because a child's stdout on a pipe
is block-buffered and `Stop()` is a hard kill that never flushes - so the one
message explaining a Simulator that quit during startup used to sit in a buffer and
die there. Measured A/B: 223 captured bytes without it, 288 with, the difference
being the `Loading:` line. It does not make a Lua game's `print()` reliable on that
path, which is why `get_game_logs` stays; that channel writes each entry to disk
immediately.

And `wrapUpdate` protected the game's frame logic but called `mcp.update()` bare,
while `mcp.update()` invokes the game's own button callbacks. An error there escaped
the harness, was never recorded by the channel that advertises tracebacks, and
stopped the polling loop permanently. Now each callback is wrapped individually (so
one bad callback does not cost the rest of the frame's harness work) with
`mcp.update()` itself wrapped as a backstop. A contract test arms a throwing
callback and requires both the traceback and a subsequent answered ping; reverting
either layer makes it fail.

The fat-struct choice itself came out of that review looking better than it
went in, with one change. Its real cost is stack, not clarity: `McpResponse` is
~12.9KB and `mcp_harness_update` also had a 13KB response buffer, ~25.6KB of
automatic storage in a function every frame calls. Both are `static` now - the
harness is single-instance and single-threaded, so nothing is given up - which
keeps the identical-layout payoff and drops the cost. No measurable difference
in the Simulator, which is all this project targets; it matters for anyone who
builds a harnessed game for the device.

**Contract testing against the Playdate SDK itself uses direct
characterization tests, not Specmatic or Pact.** The actual goal: when
`PLAYDATE_SDK_VERSION` gets bumped, know exactly what broke, before it
shows up as a confusing runtime failure. Specmatic and Pact were both
considered and rejected - both model contracts between two systems
reachable over a network/queue transport (HTTP, gRPC, message queues,
and in Specmatic's case even MCP's own JSON-RPC transport). Panic's SDK
is neither: a vendored C header plus a Lua runtime, with no spec to
generate a mock from. Instead:
- Compile-time, C only, essentially free (`c-harness/test/test_sdk_contract.c`):
  `_Static_assert` on `LCD_COLUMNS`/`LCD_ROWS`/`LCD_ROWSIZE` and every
  `PDButtons` bit value, plus a function-pointer-typed local variable
  assigned from each real API field the harness depends on
  (`getButtonState`, `getCrankAngle`, `file->stat`/`open`/etc.,
  `getDisplayFrame`). If the SDK's declared signature changes, the
  assignment stops compiling - a hard failure (`-Werror`) at the exact
  function, immediately on the next SDK version bump, not a mystery crash
  later.
- Runtime/behavioral, both languages, needs the real Simulator
  (`internal/contracttest`, a `go test` skipped unless `PLAYDATE_SDK_PATH`
  is set, i.e. run inside the full simulator image, not on a plain
  runner): a constant matching doesn't mean
  `file->stat` still behaves the same, or that `simulator.writeToFile`
  still produces a valid PNG. Builds two minimal fixture games (one C, one
  Lua - `c-harness/test/fixture-game/`, `lua/test-fixture/`) with the
  harness wired in, runs each in the Simulator under Xvfb, drives a fixed
  command sequence (ping, press, release, crank, screenshot) with known
  expected outcomes, checked automatically. This is also what caught three
  real bugs during Checkpoint 2 that no amount of local unit testing could
  have (see Checkpoints below) - it's the load-bearing check, not a
  formality.
- Deliberately out of scope here: contract testing the Go MCP server's own
  tool-call interface (Checkpoint 3/4+, once it exposes real MCP tools over
  an actual JSON-RPC transport) is a separate question. Specmatic's
  documented MCP support is a plausible real fit *there*, since that's an
  actual network/stdio transport a tool can attach to, unlike this
  checkpoint's file-based SDK dependency. Revisit then.

**The C harness's tests build against a separate, much slimmer Docker
image stage, not the full Simulator image.** The full image (needed to
actually run the Simulator) carries the whole GUI/audio stack
(webkit/gtk, novnc, ffmpeg, pulseaudio), `build-essential`, `cmake`, and
the Go toolchain - none of it needed to compile and run a freestanding C
test suite, which only needs a C compiler, the ASan/UBSan runtime, and the
SDK's `C_API` headers (just the headers, fetched via a `tar` extraction
that pulls only that subdirectory out of the SDK tarball). Added as a
second stage in the same `Dockerfile` (multi-stage build, not a second
Dockerfile, to avoid duplicating the SDK-fetch-and-license logic), built
and run via `make test-c-harness` independently of `make build`. Roughly
6x smaller (~400MB vs ~2.4GB) and meaningfully faster to build/pull as a
result.

Considered [Fil-C](https://fil-c.org/) for memory safety in the C harness
and rejected it. Fil-C uses a fundamentally different ABI (fat/capability
pointers) than standard C, and requires the whole program plus all its
dependencies to be compiled with it. "Only limited FFI to unsafe code," per
its own docs. `PlaydateSimulator` is Panic's pre-built vendor binary, not
ours to recompile, and it `dlopen`s the game's `.so` and hands it a struct
of function pointers (`PlaydateAPI`) that every single harness call crosses
constantly. There's no isolable edge where a Fil-C/non-Fil-C boundary could
sit. It's also pre-1.0 and Linux-focused, which wouldn't cover the
Windows/macOS side anyway.

Instead: the harness is built and tested with `-fsanitize=address,undefined`
(ASan/UBSan) as its own separate test build. Real bug-catching (buffer
overreads, use-after-free, undefined behavior) with zero ABI risk, since
it's just a differently-instrumented build of the same standard C, never
shipped. This test build is part of Checkpoint 2's own deliverables, not an
afterthought.

**Screenshots are PNG from Lua, raw bytes from C - not the same format from
both, and not by choice.** The original plan was for both harnesses to dump
identical raw framebuffer bytes and let Go decode them uniformly. Checked
directly against `Inside Playdate.html`: **the Lua API has no raw
pixel/framebuffer accessor at all.** `playdate.graphics.getDisplayImage()`
returns an opaque `image` object; the only documented way to get bytes onto
disk from Lua is the simulator-only `playdate.simulator.writeToFile(image,
path)`, which writes a PNG, full stop - there's no Lua equivalent of the C
API's `getDisplayFrame()`. So the two can't actually share a format:
- Lua: `getDisplayImage()` + `simulator.writeToFile()` writes a PNG. Go
  just reads those bytes straight through, no decoding needed.
- C: `getDisplayFrame()` gives raw bytes (400x240, 1bpp, 52-byte row
  stride) directly, no PNG encoder needed since bundling one just for this
  wasn't worth it. Go decodes raw → PNG for this case only.

The response JSON's `format` field (`"png"` / `"raw"` / `"none"`) tells Go
which path to take, rather than having it guess from a file extension.

A second, separate wrinkle specific to Lua: `simulator.writeToFile()`
takes a path *on the dev machine*, not a path in the sandboxed Data
directory like the rest of the file API (it's meant for exporting
dev-time assets, e.g. a pre-rendered QR code, not for reading/writing
game data) - given a bare relative path, it resolves against the
Simulator process's own working directory, essentially never the Data
directory. Confirmed empirically (an actual screenshot landed in the
wrong place, not just reasoned about). Fixed by having whatever launches
the Simulator pass the Data directory's absolute path as an extra CLI
argument, which becomes `playdate.argv[2]` (`argv[1]` is always the pdx
path itself) - the Lua harness uses that as the base for an absolute
path when calling `writeToFile`. The Go server, once it exists, already
needs to know this same absolute path to read `command.json`/
`response.json` in the sandboxed directory, so passing it through as a
launch argument is free, not new plumbing.

**Everything builds and runs inside Docker, not on the bare host.** This
solves a real blocker: `PlaydateSimulator` needs
`libwebkit2gtk-4.1`/`libjavascriptcoregtk-4.1`, which aren't packaged on
Arch outside the AUR. The Debian/Ubuntu-based container sidesteps that
entirely. Xvfb makes the Simulator run fully headless by default.
Screenshots come from each harness's own SDK-provided capture (PNG for
Lua, raw framebuffer for C), not a window capture, so headless mode is
always enough. Audio uses SDL2's `dummy` driver, since nothing consumes
sound in automated use.

**Visual and audio spot checks are optional and platform-specific.** Docker
Desktop always runs a Linux VM regardless of host OS, but letting a human
see and hear it means reaching out of that VM differently per platform.

- Linux: bind-mount the host's X11 socket, forward audio to the host's
  PulseAudio/PipeWire server. Needs an Xauth cookie for the display.
  `scripts/ensure-xauth.sh` generates one automatically.
- Windows 11: WSL2's WSLg already exposes a display and a
  PulseAudio-compatible socket at `/mnt/wslg` for this. Same idea, different
  mount paths. Built from documented WSLg integration patterns. Untested
  against a real Windows machine.
- macOS has no equivalent built in. The native option, XQuartz plus
  PulseAudio-over-TCP, is real ongoing complexity and slow. Instead there's
  a universal VNC-based fallback that works the same on any OS through
  Docker Desktop's normal port publishing. The container runs its own
  PulseAudio daemon with a null sink, `x11vnc` bridges the Xvfb display,
  `ffmpeg` re-streams the null sink's monitor as an MP3 HTTP stream. Video
  through a browser (noVNC), audio through any player. See `README.md` for
  the three `make up-visual*` targets.

**The `shared` profile exists because `docker compose run` can't be watched.**
Never written down before, so here it is. Every visual profile above and every
MCP client config are two independent things. A client runs `docker compose run`,
which creates a brand new container per connection, so a separately started
`make up-vnc` was never the same container, display or filesystem as the one an
agent was driving. Two unrelated Simulators. The `shared` profile is one
long-lived container, started detached, that a human attaches to over noVNC and
an agent attaches to over `docker compose exec`. It is the only way to watch,
live, the exact Simulator process an agent is pressing buttons on.

That is a Docker artifact, not a feature. A native host mode gets the same
property for free: one host, one display, one process, nothing to attach to. The
profile exists to give container users what native users would have by default.

It was called `byos`, for "bring your own simulator", through Checkpoint 4. That
name described something it never did, since it runs its own Simulator, and it
misled its own author while reviewing this document. Renamed at Checkpoint 6.
`byos` was then retired outright rather than handed to the native mode that
genuinely fits it: recycling a name means both meanings coexist across old
branches, issues and client configs, and it would have cost the one property
that made the rename reviewable, that `grep -rni byos` is the whole diff. The
native mode is called `native` in every identifier. "Bring your own simulator"
survives as prose, where it is finally accurate.

**The SDK is fetched from Panic's own servers at image-build time, not
bundled in this repo.** The Playdate SDK License bans redistributing the
SDK. The Dockerfile here is just source that `curl`s `download.panic.com`
during each user's own local `docker build`. That user downloads and
accepts Panic's license, not this project. Holds only as long as a built
image is never published to any registry. That would flip it into
redistribution. See `README.md`.

**Project name and license.** Named `open-crank-mcp`, not `playdate-mcp`.
The Playdate SDK License bans using "Playdate" or "Panic" in the name of
anything built with the SDK. This project's own code is MIT, matching every
other original repo under `~/GitProjects/NickSpaghetti/`. Precedent for this
kind of community tooling around the SDK: unofficial third-party Rust
bindings (`crankstart`, `craydate`, `boozook/playdate`) generate FFI
bindings directly from Panic's C headers, a more aggressive reading of the
license's "don't build another SDK" clause than this project attempts, and
have run openly for years without apparent enforcement. Not a legal
guarantee. Just a documented risk assessment. Revisit if Panic ever raises
a concern.

## Architecture

Three pieces.

### 1. Lua harness (`lua/mcp_harness.lua`)

Single file, no dependencies beyond `CoreLibs/json` (bundled with the SDK).
Added to a game's `main.lua`:

```lua
import "mcp_harness"
-- inside playdate.update(), as the first line:
mcp.update()
```

Responsibilities:
- At load time, wraps `playdate.buttonIsPressed`, `buttonJustPressed`,
  `buttonJustReleased`, `getButtonState`, `getCrankPosition`,
  `getCrankChange`, `isCrankDocked`, and calls `playdate.file.mkdir("mcp")`
  once. Each wrapped getter checks an internal override table first, falls
  back to the real (closed-over) function otherwise - genuinely transparent,
  since `playdate` is an ordinary mutable Lua table.
- `mcp.update()`: once per frame, checks for a fixed `mcp/command.json` in
  the Simulator's data directory (via `playdate.file`, resolving to
  `Disk/Data/<bundleID>/mcp/`) - a single well-known filename rather than
  per-request filenames, since this is a synchronous one-request-at-a-time
  protocol; the `id` field *inside* the JSON is what correlates a response
  to its request. If a command is present, dispatches by `type`:
  - `screenshot`: `getDisplayImage()` + `simulator.writeToFile()` as a PNG.
    Needs an absolute path (see Design decisions) built from
    `playdate.argv[2]`, not the sandboxed relative-path convention the rest
    of the file API uses.
  - `press` / `release` / `crank`: updates the override table. Supports a
    duration in ms, so "hold A for 200ms" auto-releases, and (for `release`)
    actively forces not-pressed for the duration rather than just clearing
    the override - symmetric with `press`, since a passthrough-only release
    wouldn't force the button up if something else was also driving real
    input at the same time.
  - `state`: calls a user-registered inspector function, re-decodes its
    returned JSON string into a table so it embeds as a real nested object
    in the response, not a JSON-escaped string.
  - `ping`: liveness check.
  Writes a fixed `mcp/response.json`, deletes the command file.
- `mcp.registerState(fn)`: extensibility hook, so a specific game can expose
  its own debug state (player position, score, current level, entity list).
  What makes `get_game_state` useful beyond generic engine internals.

### 2. C harness (`c-harness/mcp_harness.h` + `mcp_harness.c`)

Same protocol as the Lua harness, for pure-C games with no Lua VM, but
**not** the same monkey-patching mechanism - see the const/read-only-memory
finding in Design decisions. Added to a game's own C sources and called
from its `eventHandler`:

```c
#include "mcp_harness.h"
// on kEventInit:
mcp_harness_init(pd);
// once per frame, in the update callback:
mcp_harness_update(pd);
```

A C game must also call `mcp_get_button_state(pd, &current, &pushed,
&released)`, `mcp_get_crank_angle(pd)`, `mcp_get_crank_change(pd)`, and
`mcp_get_crank_docked(pd)` instead of the raw `pd->system->*` equivalents
wherever it reads input, for overrides to actually take effect.

- `mcp_harness_init(pd)`: initializes the override state and calls
  `pd->file->mkdir("mcp")` once. Does *not* touch `pd->system` at all
  (see Design decisions for why).
- `mcp_get_button_state`/`mcp_get_crank_angle`/`mcp_get_crank_change`/
  `mcp_get_crank_docked`: each calls the real, untouched `pd->system->*`
  function and applies the override on top before returning.
- `mcp_harness_update(pd)`: same command/response file protocol via
  `playdate->file->*` (using `kFileReadData`, not `kFileRead` - the latter
  only searches the read-only pdx bundle, not the writable data directory
  our files actually live in, a distinction the C API draws that Lua's
  `kFileRead` doesn't), dispatching the same command types as the Lua
  harness. `screenshot` uses `getDisplayFrame()` for the raw bytes,
  written directly via `pd->file->open`/`write` - no path quirk here,
  unlike Lua's screenshot path.
- `mcp_harness_register_state(fn)`: same extensibility hook. The callback
  returns an already-formatted JSON string. No bundled JSON writer, keeps
  the harness dependency-free. The game author formats it themselves.

### 3. Go MCP server

```
open-crank-mcp/
  go.mod
  main.go
  internal/simulator/   # process management: launch/stop PlaydateSimulator, log capture
  internal/harness/      # file-based IPC client talking to either harness, protocol is language-agnostic
  internal/build/         # project-type detection (C vs Lua), cmake/pdc build step
  internal/screenshot/    # raw framebuffer dump to PNG decoder, for the C harness's screenshot format
  internal/tools/        # MCP tool registrations (github.com/modelcontextprotocol/go-sdk/mcp)
  internal/contracttest/ # internal/simulator + internal/harness + internal/build + internal/screenshot driven against a real PlaydateSimulator (go test, skipped without PLAYDATE_SDK_PATH)
  cmd/smoke-check/       # environment-health check: SDK shared libs resolve, pdc runs, Simulator launches cleanly under Xvfb
  lua/mcp_harness.lua
  lua/test-fixture/      # minimal fixture game for internal/contracttest
  c-harness/mcp_harness.h
  c-harness/mcp_harness.c
  c-harness/test/        # test_sdk_contract.c, test_pure_logic.c, test_fake_api.c, fixture-game/
  scripts/run-c-harness-tests.sh
  docs/ROADMAP.md
  Dockerfile
  docker-compose.yml
  Makefile
  LICENSE
  README.md
```

Tools exposed:
- `build_game(source_dir)`: detects project type. A `CMakeLists.txt`
  present means a C project. Runs the matching build: `cmake -S . -B build
  && cmake --build build` for C (which invokes `pdc` itself as a post-build
  step, per the SDK's own CMake support), plain `pdc` for Lua-only
  projects. Returns compile errors and warnings either way.
- `launch_simulator(pdx_path)` / `stop_simulator()` / `restart_simulator()`:
  launching also needs to pass the game's Data directory absolute path as
  an extra CLI argument (see the Lua screenshot path quirk above).
- `get_logs(tail_n)`: buffered stdout/stderr from the Simulator child
  process. Where `print()` output and Lua tracebacks land.
- `press_button(button, duration_ms)` / `set_crank(angle_or_delta, docked)`
- `get_screenshot()`: round-trips through the harness. For a C-sourced raw
  dump, reads the bytes off disk and decodes to PNG; for a Lua-sourced PNG,
  just reads the bytes straight through. Either way returns an MCP image
  content block.
- `get_game_state()`: round-trips through the harness's registered
  inspector, returns JSON.
- `read_save_data(filename?)` / `write_save_data(filename, json)`: direct
  file access to `Disk/Data/<bundleID>/`. No harness round-trip needed.
- `get_status()`: simulator running? bundleID? harness reachable?

IPC mechanism: the Go side writes a fixed `mcp/command.json`, then waits
for a fixed `mcp/response.json` to appear, reads and deletes it. This is a
synchronous, one-request-at-a-time protocol - fixed filenames rather than
per-request ones, with the `id` field *inside* the JSON correlating a
response to the request that produced it, so a stale leftover response from
a previous run is detected and ignored. That correlation is real as of the
performance review (see the fat-struct decision above); for Checkpoints 3-7
it was described here and not implemented, and a single slow round trip was
enough to make every later call return the previous one's answer. Simple, cross-platform,
and no longer just assumed fast enough relative to the Simulator's own
frame rate - actually stress-tested against three real games (see
`docs/GOTCHAS.md`), which found the Simulator's own ~30fps frame period,
not the wait mechanism, is what dominates round-trip latency. The wait
itself (`internal/harness/ipc.go`'s `WaitForFile`/`WaitForDir`/
`WaitForResponse`) is inotify-based (via `github.com/fsnotify/fsnotify`),
not a poll loop - it was a 100ms poll, tightened to 10ms, then to 1ms, then
replaced with blocking on a real filesystem notification, none of which
moved the median (still frame-bound, ~33ms) since none of them can cross
that floor, but each step removed wasted CPU wakeups/detection delay spent
asking "yet?" instead of being told.

## Checkpoints

- [x] **Checkpoint 1**: Repo and container scaffolding. LICENSE, README,
  `docs/ROADMAP.md`, `go.mod`, `Dockerfile` (Debian/Ubuntu, Go, Xvfb,
  GTK/WebKit libs, `cmake`/`build-essential` for C games, SDK fetched from
  Panic's URL at build time), `docker-compose.yml` (headless default plus
  Linux/Windows-WSLg/universal-VNC visual and audio profiles), `Makefile`
  wrapping every command. Verified a bundled C example (Hello World) builds
  via CMake and runs in the Simulator under Xvfb. Also added CI
  (`.github/workflows/ci.yml`): `docker-build`, `go-test`, and
  `mutation-test` (gremlins, Go-only, a no-op until Go code exists) run in
  parallel, all three required status checks on `main`. No server or
  harness logic yet.
- [x] **Checkpoint 2**: Harnesses. `lua/mcp_harness.lua` and
  `c-harness/mcp_harness.{h,c}`, plus their tests
  (`c-harness/test/{test_sdk_contract,test_pure_logic,test_fake_api}.c`,
  built against a new slim `c-harness-test` Docker stage) and the SDK
  contract check (originally `scripts/sdk-contract-check.sh`, later
  rewritten to `internal/contracttest` against real fixture games).
  `registerState` extensibility hook in both. Two design decisions
  from the original plan turned out wrong once actually run against the
  real Simulator, not just reasoned about, and are corrected above and in
  the code: C can't transparently monkey-patch `pd->system` (real
  segfault, not just nominal UB - explicit wrapper functions instead), and
  screenshots can't share one raw-bytes format (Lua has no raw
  framebuffer accessor at all - PNG from Lua, raw from C). Three further
  bugs surfaced only by the real-Simulator contract check, not by any
  local unit test: C's `kFileRead` silently only searches the read-only
  pdx bundle (needs `kFileReadData` for the data directory); the Lua
  harness never created its own `mcp/` directory; and
  `simulator.writeToFile()` resolves relative paths against the
  Simulator's cwd, not the data directory (fixed via `playdate.argv`).
- [x] **Scripts rewrite**: `scripts/smoke-check.sh` and
  `scripts/sdk-contract-check.sh` (bash) replaced with real Go packages:
  `internal/simulator` (child-process launch/stop/wait/log-capture) and
  `internal/harness` (file-based IPC client), plus `cmd/smoke-check` and
  `internal/contracttest` as their consumers, each with fast unit tests
  runnable via plain `go test ./...`. `scripts/run-c-harness-tests.sh`
  stays bash: converting it would mean adding the Go toolchain to the
  deliberately slim `c-harness-test` Docker stage for very little real
  logic gained. CI: `test-c-harness` and `smoke-check` required on every
  PR; `sdk-contract-check` required only on PRs touching harness-related
  paths (detected via `dorny/paths-filter`, not the workflow trigger's
  `paths:` key, so the required check always resolves instead of staying
  permanently pending on unrelated PRs) and unconditionally once a week
  (`.github/workflows/weekly.yml`, Sunday 9PM EST, matrix over the pinned
  SDK version and Panic's `latest` alias, catching upstream SDK drift even
  when nothing in this repo changed).
- [x] **Checkpoint 3**: Go server core. `internal/simulator` and
  `internal/harness` already existed (see Scripts rewrite above).
  `internal/build` adds project-type detection (`CMakeLists.txt` means C,
  `Source/main.lua` means Lua) and the `cmake`/`pdc` build step for each.
  `internal/screenshot` decodes the C harness's raw framebuffer dump
  (400x240, 1 bit per pixel, MSB-first, 52-byte row stride) into a PNG.
  The bit-to-color polarity isn't documented anywhere in the SDK, so it's
  pinned empirically: the C fixture now clears its display to
  `kColorBlack` at init, and `internal/contracttest` decodes its raw
  screenshot and asserts every pixel is black against the real Simulator.
  `internal/contracttest` also now builds both fixtures through
  `internal/build.Build` instead of its own bespoke cmake/pdc calls.
- [x] **Checkpoint 4**: MCP tool registrations (`internal/tools`) wiring
  everything together via `github.com/modelcontextprotocol/go-sdk/mcp` (the
  project's first external dependency, pinned to v1.6.1). All nine tools
  from the design above are live: `build_game`, `launch_simulator`,
  `stop_simulator`, `restart_simulator`, `get_status`, `get_logs`,
  `press_button`, `set_crank`, `get_screenshot`, `get_game_state`,
  `read_save_data`, `write_save_data`. Plus one more, requested alongside
  this checkpoint: `list_entities`, listing sprites currently in the
  display list. Lua's `playdate.graphics.sprite.getAllSprites()` gives a
  true, complete enumeration; the C API has no equivalent, so it falls
  back to `querySpritesInRect` over the full screen rect, which only
  matches sprites with a collision rect set. Verified against the SDK's
  own bundled `Sprite Game` example (harness wired in via a scratch copy,
  not committed): background/parallax/explosion sprites, none of which set
  a collide rect, were correctly absent; player and enemy planes, which
  do, correctly showed up. Every response carries a `complete` field so a
  caller can tell a full list from the approximation.

  Two fixes landed alongside the new tools: `Simulator.Output()` was only
  documented as safe to call after `Wait()` returns, which is fine for the
  existing smoke-check/contracttest callers (they always stop the process
  first) but not for `get_logs` reading a still-running simulator. It's
  now backed by a mutex-guarded buffer instead, proven race-free under
  `go test -race`. `internal/harness`'s poll interval was 100ms; tightened to
  10ms, matching what `docs/ROADMAP.md` always said the design should be.
- [x] **Shared session**: One persistent container an agent and a human both
  drive. Every profile above gives you one or the other, because an MCP client
  runs `docker compose run`, which creates a new container per connection: a
  separately started `make up-vnc` was never the same container, display or
  filesystem as the one an agent was driving. The `shared` profile is one
  long-lived detached container instead, watched over noVNC and attached to over
  `docker compose exec`. `scripts/run-vnc.sh` starts what that needs: Xvfb,
  openbox, x11vnc, websockify serving noVNC, a PulseAudio null sink, and ffmpeg
  restreaming that sink's monitor as MP3. openbox is patched from its shipped
  `rc.xml` down to one desktop with no keybindings, because four desktops and a
  stray toggle-show-desktop both make the Simulator appear to vanish, and there
  is no taskbar to recover it from - only an MCP client can start one.

  The volume slider was the interesting part. Nothing can read the Simulator's
  volume back (`getVolume()` has no setter and the INI has no key for it), and
  the browser only ever sees a framebuffer, so the slider is found in the pixels
  instead. A one-pixel-wide column 29px in from the window's right edge crosses
  the LOCK and MENU buttons, the volume trough, its knob and the mute icon, and
  the longest run of non-yellow below the buttons is the trough. Taking the
  longest run rather than fixed offsets is what makes it self-calibrating across
  zoom levels. Verified three ways: `scripts/run-shared-unit-tests.sh` against
  synthetic columns, `scripts/shared-check.sh` against a real Simulator, and
  Playwright tests for the page behaviour.

  Two follow-ups landed on top. `cmd/shared-load` builds and launches a game in
  the running container by driving the MCP server over stdio exactly as a client
  would, since `up-shared` only ever started a container and something still had
  to call `build_game`/`launch_simulator`. And `scripts/shared-watch.sh` rebuilds
  on save and reloads in place with the Simulator's own Ctrl-R, which re-reads
  the `.pdx` in the same process, so the display, the container and the browser
  tab all survive and the VNC view does not even reconnect. It is not
  state-preserving: Reset is the only reload the SDK has.

  Then a bug worth recording for how badly it presented. The Simulator treats
  SDL initialisation as fatal, so with `SDL_AUDIODRIVER=pulseaudio` set and no
  reachable PulseAudio socket it prints one line about SDL2 and exits. On a
  container that has just started, whatever launches a game can easily get there
  before `run-vnc.sh` has PulseAudio accepting connections, so the same command
  works or doesn't depending on timing, and the message explaining it goes to a
  buffer that is discarded when the session ends. What you observe is a game
  that launches, reports healthy, and is gone seconds later. Process groups and
  sessions, SIGPIPE from a closed stdout pipe, Docker killing exec descendants,
  CPU starvation, the framebuffer scanner and container age were all
  investigated first. The Simulator was never dying, it was never starting.
  `cmd/shared-load` now waits for `pactl info` to succeed before launching
  anything, and `launch_simulator` checks the process is still alive shortly
  after starting it and returns the captured output if it isn't. Written up in
  `docs/GOTCHAS.md`.
- [ ] **Checkpoint 5**: End-to-end verification, container. Build one of the
  SDK's Lua `Examples/` and one of its C `Examples/` (both with the matching
  harness wired in), run each through the containerized stack, confirm every
  tool against real gameplay for both. Independent of Checkpoints 6-11: those
  are the native-mode track and neither blocks this.
- [x] **Checkpoint 6**: Renamed the `byos` profile to `shared`. No behaviour
  change, and the point of doing it alone was that `grep -rni byos` is the whole
  review. `byos` stood for "bring your own simulator", which is not what that
  profile does - it runs its own Simulator and shares it. The name misled its
  own author during review, so it became `shared` and `byos` is now retired as a
  token repo-wide rather than reused, so there is no version of this repo where
  the word means two things. `virtual` was considered and rejected: one letter
  from the existing `simulator-visual` service and `make up-visual`.

  Six files and one Go package renamed, plus the service, the profile, the
  `.shared-data` mount, four shell functions, `SHARED_LIB_DIR`/`SHARED_URL` and
  seven `make` targets. `cmd/shared-load` had the service and profile names as
  bare literals in eight places across three functions, which is how a rename
  leaves a half-finished one behind, so they are two consts now. The
  `# shellcheck source=` directives moved with the file they point at, since
  those are load-bearing rather than prose. CI changed names only: broadening
  the paths-filter globs changes *when* a job fires, so it belongs to Checkpoint
  9 rather than here. Old target names became tombstones that fail with a
  pointer to the README's migration section, to be deleted at Checkpoint 11.

  `/.byos-data/` stays in `.gitignore` next to the new entry. The old directory
  is root-owned because the container runs as root, so clearing it needs `sudo`
  or a container, and nobody should have to do that just for a clean
  `git status`.

  Out of repo, and the order mattered: `byos` had to leave `main`'s required
  status checks *before* this merged, or every later PR would block forever on a
  check that will never report again. `shared` added after.
- [x] **Checkpoint 7**: Portability prep, plus one bug fix that shouldn't wait
  behind a feature. No behaviour change on any existing path. Everything here is
  a prerequisite for native mode that stands on its own.

  `internal/simulator` has three POSIX-only constructs, and they fail
  differently. `SysProcAttr{Setpgid: true}` and `syscall.Kill(-pid, SIGKILL)`
  are compile errors on Windows. `Exited()`'s `Process.Signal(syscall.Signal(0))`
  is worse: it compiles and returns the wrong answer, because Windows rejects
  every signal except Kill, so `Exited()` would always report true and
  `launch_simulator` would always claim the Simulator quit during startup. Split
  all three into `proc_unix.go`/`proc_windows.go` behind unexported helpers so no
  exported signature changes and no call site moves. A `make go-build-cross`
  target running `GOOS={linux,darwin,windows} go build ./...` plus `go vet` is
  what proves it, and it is the only cross-platform claim provable from a Linux
  box, so it lands first. `cmd/smoke-check` gets the same treatment for its Xvfb
  and `ldd` calls.

  The bug: `internal/setup/repo.go` finds the canonical harness sources by
  walking up from `os.Getwd()` looking for `go.mod`. That only works because the
  image sets `WORKDIR` to the repo root. For any binary invoked elsewhere it
  breaks two ways, and the second is worse than the first: with no `go.mod`
  above the cwd the `setup` tool dies outright, but with a cwd inside some
  *other* Go module it returns that module and fails with
  `reading /some/other/project/lua/mcp_harness.lua: no such file`, which names
  nothing useful. MCP servers routinely inherit an arbitrary cwd. Fix is
  `go:embed`, which forces a small structural change worth explaining once:
  `go:embed` cannot reach `../` and `internal/setup` cannot import `package
  main`, so the embedding package has to be a non-main package at the repo root,
  which is what moves `main.go` to `cmd/open-crank-mcp/`. On the read side use
  `path.Join`, never `filepath.Join` - `fs.FS` paths are always forward-slash.
  `Teardown` never called `repoRoot` and doesn't change.

  Also `make go-build` runs `go build ./...`, which writes nothing to disk.
  Native mode's client config points at a binary path, so it needs `-o`.

  Verified by running the same `setup` call against a binary built from `main`
  and one from this branch, both from a working directory inside an unrelated Go
  module. `main` failed with
  `reading /some/other/module/lua/mcp_harness.lua: no such file or directory`,
  naming a path that has nothing to do with anything. This branch succeeded and
  wrote the real embedded harness. That is the whole argument for the change,
  and it is worth re-running rather than trusting, because the broken version
  looks fine from inside the repo.

  Two things turned up that were not in the plan. `Exited()` needed the split as
  much as the two compile errors did, and needed it more quietly: signal 0 is
  rejected by Windows for every signal but Kill, so it compiles there and always
  answers "gone", which would have made `launch_simulator` always report that the
  Simulator quit during startup. A cross-compile gate cannot catch that class at
  all, which is worth remembering about what `go-build-cross` does and does not
  prove.

  And `.gremlins.yaml` had a latent trap. Its exclude list said `main.go`, which
  gremlins matches by basename, so it was also silently excluding
  `cmd/shared-load/main.go` and `cmd/smoke-check/main.go`. Moving `main.go` into
  `cmd/open-crank-mcp/` made the entry specific and dropped mutant coverage from
  95.9% to 72.9%, all of it `cmd/shared-load`'s 48 previously-hidden mutants. So
  the score the project had been reporting was better than its own config asked
  for. Every exclusion is now a full path with its own justification, which puts
  coverage back at 95.8% honestly. Splitting `cmd/smoke-check` also moved code
  out from under an existing exclusion, and those files are listed too.
- [ ] **Checkpoint 8**: Native mode. The same binary running against a Playdate
  SDK the developer installed themselves, no Docker. This is the mode "bring your own
  simulator" always described, though the phrase stays prose only: every
  identifier says `native`, so the retired name stays retired.

  A new `internal/sdk` owns SDK location and per-OS paths, resolving
  `PLAYDATE_SDK_PATH`, then `SDKRoot` in `~/.Playdate/config` (Panic's own
  mechanism, already read by the fixture's `CMakeLists.txt`), then a per-OS
  default. The env var winning first is what keeps container behaviour
  byte-identical. It takes its filesystem, environment and home directory as
  parameters rather than reaching for globals, which is what makes the whole
  package table-testable with `fstest.MapFS`: resolution order, a malformed
  config, the macOS `.app` layout, the Windows `.exe` layout, data-directory
  probing, and the error path listing every candidate tried. Those tests run on
  every OS on every PR without a Simulator anywhere, and they are what makes
  shipping unverified macOS paths honest rather than a shrug.

  Two real hazards. `internal/build/exec.go` re-reads `PLAYDATE_SDK_PATH` from
  the environment independently of the server, so any resolution logic would be
  honoured by `launch_simulator` and silently ignored by `build_game`; it takes
  the resolved paths as an argument instead, and sets them in the child
  environment so a game's own `CMakeLists.txt` can't fall through to the SDK
  template's `bash`/`egrep` fallback. And the sandboxed data directory is
  currently assumed to sit inside the SDK, which is the Linux layout only. Guess
  it wrong and the failure is silent: launch succeeds, `get_status` reports
  `harness_reachable: false`, and every other tool times out five seconds at a
  time. So don't guess. Screenshots don't need it at all, because
  `playdate.simulator.writeToFile` takes a path on the dev machine and a scratch
  directory serves better. The IPC directory is *observable* once a game runs,
  because the harness creates `mcp/` inside it, so probe for that after launch
  and make a failed probe a loud non-fatal warning listing every path tried.
  That turns the ugliest failure mode into a first-call diagnosis.

  Platform scope is linux and darwin. Windows compiles and its path logic is
  covered by the `fstest` suite, but the runtime is unsupported and says so:
  WSL2 already serves those users through the existing profile.

  That started as a scoping choice and has since become a structural one. The
  Windows SDK ships as an interactive installer `.exe`, with no archive
  equivalent to the Linux `.tar.gz` that CI fetches and extracts. So a Windows
  runner cannot provision itself, and Windows-native could never have the
  per-PR verification the other two platforms get. "Unsupported for now" is
  therefore "unsupported", not a queue position. Its layout values are still
  correct (see `docs/NATIVE-PROBE.md`) because keeping them right costs nothing
  and makes promoting Windows additive if that ever changes.

  Done when the `fstest` suite passes under `go-build-cross`, `make sdk-path`
  names both the resolved SDK and which source found it, `make
  smoke-check-native` and `make sdk-contract-check-native` pass on a host SDK,
  and a real MCP client pointed at the binary with no Docker runs `build_game`
  through `get_screenshot` and `press_button` to `setup`. Run `setup` from a cwd
  outside this repo and again from inside an unrelated Go module - that second
  one is the case Checkpoint 7 fixed.

  Where that stands, so the unticked box reads as a fact rather than an
  oversight. The code is complete and the per-OS logic is genuinely covered. The
  `fstest` suite passes under `go-build-cross`, and the layouts are named
  `linux_layout.go`/`macos_layout.go`/`windows_layout.go` rather than with GOOS
  suffixes, so all three compile and are exercised on every platform. That naming
  is load-bearing and was a real bug once: with `paths_darwin.go`-style names the
  package compiled on no platform at all and the whole test-macOS-from-Linux
  argument was void.

  The `setup`-from-a-foreign-cwd clause is verified. With no SDK on the machine,
  `setup` run with cwd `/tmp`, and again with cwd inside an unrelated Go module,
  both wrote a full 26KB stamped harness into the target game. That is the
  Checkpoint 7 bug and it stays fixed. It is checkable without an SDK because the
  server now serves even when resolution fails, which is deliberate.

  What is not verified, and what keeps this unticked: nothing has run against a
  real host SDK. There is no SDK on this machine, so `make smoke-check-native`,
  `make sdk-contract-check-native` and the end-to-end MCP client run have not
  happened. Every macOS and Windows path value comes from a probe on a real
  install (`docs/NATIVE-PROBE.md`), not from this project running there.
  Installing an SDK here and running those three is the whole remaining cost.
- [ ] **Checkpoint 9**: CI for native mode. One `native` job installing the
  Simulator's shared libraries and the SDK directly on the runner, then building
  and running the native targets. CI already fetches the SDK from Panic for
  `docker-build`, so this is not a new licence posture. The job doubles as the
  authoritative install line for the README's native requirements, so the two
  can't drift. A macOS leg is advisory (`continue-on-error`) because it is the
  platform with a real user story and the one worth pushing to green first;
  there is no Windows leg, since the cross-compile gate plus the `fstest` suite
  is the agreed coverage and a permanently red advisory leg is only noise. Don't
  matrix the existing jobs - `mode: [container, native]` would rename
  `smoke-check` to `smoke-check (container)` and break branch protection a
  second time for no signal.

  The paths-filter work belongs here rather than in Checkpoint 6, because it
  changes when jobs fire: broaden the enumerated `scripts/` globs so a future
  rename can't silently stop firing the browser leg, and add a step to both
  filtered jobs asserting every path they reference still exists. `weekly.yml`
  stays container-only on purpose, since its matrix knob is a Docker build arg
  and a native install is whatever the developer installed; say so in a comment
  or it reads as an oversight.

  Add `native` to `main`'s required checks once the ubuntu leg is green. Anything
  promoted later has to keep the always-run-the-job, conditionally-run-the-step
  shape, for the reason recorded under **Scripts rewrite** above.

  Where that stands. Everything in this checkpoint that lives in the repo is
  done and committed: the `native` job on ubuntu running both native targets,
  the advisory `native-macos` leg under `continue-on-error`, no Windows leg, no
  matrixing of the existing jobs, the `scripts/**` glob with a comment naming the
  rename that nearly broke it, the path-existence assertions on both filtered
  jobs, and the `weekly.yml` comment explaining why it stays container-only.

  The one thing left is not a code change: adding `native` to `main`'s required
  checks, which is a branch-protection setting and has to wait for the ubuntu leg
  to go green on a real run. That is why this stays unticked.
- [ ] **Checkpoint 10**: Documentation for two modes. The README currently
  interleaves *how the SDK runs* with *which client you configure*, which is why
  the connecting section repeats near-identical blocks. Separating those axes
  means adding a third mode reduces the block count rather than growing it.
  Specific things that become wrong rather than merely incomplete: Docker is
  listed as the only requirement, `### Linux (native X11/XWayland)` is not
  native and becomes a trap once a real native mode exists, one paragraph
  actively argues against a native path on macOS and needs narrowing to "for
  seeing a containerized Simulator", and `## Local development` claims the only
  host requirements are Docker and Go. The existing **Needs** and **Applies to**
  columns are already the right shape for marking mode, so the native rows drop
  straight in.

  ROADMAP supersessions, all in the "what we tried and what was actually true"
  voice rather than deletions: the Docker-only decision keeps the Arch WebKit
  blocker as the reason Docker exists and stays the default, and states what was
  overstated. The Go decision gets the two sentences that make its
  cross-compilation argument finally pay off. The visual-spot-check decision
  gets the note that native dissolves the problem class it describes. The SDK
  licence decision gets its native branch, where this repo never touches the SDK
  at all.

  `docs/GOTCHAS.md` needs a file-level note that everything in it was found on
  containerized Linux and is nonetheless SDK behaviour that applies natively,
  plus five inline exceptions where the platform genuinely matters. The
  PulseAudio entry is the interesting one: it is container-specific by
  construction, since it follows from the profiles forcing
  `SDL_AUDIODRIVER=pulseaudio`, but the mitigation it describes is general and
  right to keep.
- [ ] **Checkpoint 11**: End-to-end verification, native. The same two example
  games as Checkpoint 5, every tool confirmed against real gameplay, with no
  Docker in the loop. This is the checkpoint that turns assumptions into either
  confirmations or bugs, so it is where the unverified macOS paths get found.
  Linux first, since it is the only platform anything here has been verified on.
  Note that on Arch this needs the AUR WebKit packages, which is the same
  blocker that made Docker the default in the first place. Delete Checkpoint 6's
  tombstone targets here.
- [ ] **Docs drift pass**: Unrelated to the above and safe to do at any time.
  The IPC poll interval is recorded as three different numbers in three files:
  this document says 10ms, `docs/GOTCHAS.md` records 10ms then 1ms then the
  fsnotify wait that replaced polling, and `.gremlins.yaml` still says 100ms.
  `.gremlins.yaml` also justifies two exclusions by "the full simulator Docker
  environment", which becomes "a real SDK install, container or native".
