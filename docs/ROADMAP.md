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
management, file-notification waits, JSON/PNG shuttling) is I/O-bound. Not chosen
for raw throughput over C#/.NET. Chosen for the simpler distribution story
across three OSes.

Native mode is what makes this argument pay. Until it existed the cross-compilation
and "no runtime to install" reasoning applied to a binary that ran inside a Linux
container regardless of the host OS, which is a distribution story the container was
already telling on its own - `GOOS`/`GOARCH` reach bought nothing. A host binary on
Linux and macOS is the thing this language choice was made for. The workload description above has been corrected in place: the IPC wait blocks
on a filesystem notification, and polls only as a bounded backstop. See
`docs/GOTCHAS.md`.

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
  is set. The full simulator image is one way to satisfy that; a host SDK is
  another, which is what makes `make sdk-contract-check-native` possible on a
  developer's machine with no Docker at all. Not, as this used to say, only
  inside the image and never on a plain
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
`libwebkit2gtk-4.1`/`libjavascriptcoregtk-4.1`, and the Debian/Ubuntu-based
container has them.

Superseded in part, and worth correcting rather than leaving to mislead. This
originally said those libraries "aren't packaged on Arch outside the AUR". They
are: `webkit2gtk-4.1` is in Arch's `extra`, and it provides both sonames. With it
installed, every native target passes on the Arch machine this was written on -
see Checkpoint 8. The dependency itself is real and unavoidable, since both
libraries are in the binary's `DT_NEEDED` and the loader refuses to start the
process without them (exit 127, before any of Panic's code runs), but the
packaging obstacle was overstated.

Docker still stays the default, on the reasons that actually hold: it is the only
path where the SDK version is pinned and reproducible (`PLAYDATE_SDK_VERSION` is a
build arg, whereas a native install is whatever the developer installed), it is
the only path CI exercises end to end on every PR, and it needs nothing installed
on the host at all. Xvfb makes the Simulator run fully headless by default.
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

The same goes for the whole per-platform spot-check apparatus above. Native mode
dissolves that problem class rather than solving it: the Simulator is an ordinary
application on your desktop, with your window manager and your audio device.
Nothing is bind-mounted, bridged or re-streamed, and the openbox configuration,
the VNC workspace geometry and the framebuffer slider scan are all container
concerns that simply do not exist there. That includes the thing the macOS bullet
says is missing - no XQuartz, no PulseAudio-over-TCP, no browser tab.

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
accepts Panic's license, not this project.

In native mode the question is structurally absent rather than mitigated: this
repo never fetches, extracts or ships the SDK at all. The developer installed it
themselves under their own acceptance of Panic's licence, and the server only
reads a path. That makes native the licence-cleanest of the two paths, though it
changes nothing about the container path below.

Holds only as long as a built
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
  `go test -race`. `internal/harness`'s poll interval was 100ms, tightened to
  10ms here. It went to 1ms afterwards and was then replaced outright by a
  filesystem notification, so the number in this entry is history rather than
  current behaviour - see `docs/GOTCHAS.md` for the sequence.
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
- [x] **Checkpoint 5**: End-to-end verification, container. Build one of the
  SDK's Lua `Examples/` and one of its C `Examples/` (both with the matching
  harness wired in), run each through the containerized stack, confirm every
  tool against real gameplay for both. Independent of Checkpoints 6-11: those
  are the native-mode track and neither blocks this.

  Done with `Asheteroids` (Lua) and `Sprite Game` (C), the same two as Checkpoint
  11, so the two modes are compared on identical inputs rather than on whatever
  each happened to reach for. Every tool passed on both, with
  `harness_reachable: true` and `data_dir_source: observed`.

  No bind mount and no `GAME_DIR`. The image already carries the SDK, so it
  already carries its `Examples/`, and the container copies one into `/tmp` and
  works on that. That is a better test than mounting a game from the host as well
  as a simpler one: nothing on the host is touched, and the copy is discarded with
  the container.

  The two modes agreed on everything that matters and differed only where they
  should. `data_dir` resolved to `/opt/playdate-sdk/Disk/Data/<bundle>` here
  against `~/PlaydateSDK/Disk/Data/<bundle>` natively, which is the per-machine
  resolution working rather than a hardcoded path. The C example built under GCC
  13.3.0 in the container and 16.1.1 natively, so `setup`'s patches to Panic's own
  code compile across two compiler generations. Same bundle IDs, same tool
  results, same observed data directories.

  Worth recording that the missing-`bundleID` problem from Checkpoint 11 is not
  mode-specific: `Asheteroids` needs the same one-line `pdxinfo` addition here.
  That confirms it is a property of the SDK's example, not of either stack.
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
- [x] **Checkpoint 8**: Native mode. The same binary running against a Playdate
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

  That is a scoping choice, and the reasoning behind it needs a correction.

  It was written up here as structural: the Windows SDK ships as an interactive
  installer `.exe` with no archive equivalent, so a runner could not provision
  itself, so Windows-native could never have per-PR verification. The same
  argument was made about macOS and turned out to be a category error. The macOS
  download is a `.zip` containing a `.pkg`, and a `.pkg` installs without
  interaction via `installer -pkg` - "installer, not archive" simply does not
  imply "cannot be automated". Windows installers routinely take a silent flag
  too, and nobody has checked whether this one does.

  So the honest position is narrower than what was here. Windows-native is
  unsupported because nobody here can run or debug it and WSL2 already serves
  those users through container mode - a scoping decision, and a revisitable one.
  It is not unsupported because provisioning is impossible; that was asserted
  without being tested. Its layout values are still
  correct (see `docs/NATIVE-PROBE.md`) because keeping them right costs nothing
  and makes promoting Windows additive if that ever changes.

  Done when the `fstest` suite passes under `go-build-cross`, `make sdk-path`
  names both the resolved SDK and which source found it, `make
  smoke-check-native` and `make sdk-contract-check-native` pass on a host SDK,
  and a real MCP client pointed at the binary with no Docker runs `build_game`
  through `get_screenshot` and `press_button` to `setup`. Run `setup` from a cwd
  outside this repo and again from inside an unrelated Go module - that second
  one is the case Checkpoint 7 fixed.

  The layouts are named `linux_layout.go`/`macos_layout.go`/`windows_layout.go`
  rather than with GOOS suffixes, and that naming is load-bearing rather than a
  style choice. Go applies an implicit build constraint to any file whose name
  ends in `_linux.go`, `_darwin.go` or `_windows.go`, with or without a
  `//go:build` line, so the first attempt at this compiled on no platform at all
  and the whole argument for testing the macOS layout from Linux was void.
  Renaming them back would silently stop the cross-platform tests compiling the
  code they claim to test.

  Verified against a real SDK on Linux, which is what this box could not do until
  `webkit2gtk-4.1` went in. Every step below ran with no container anywhere:

  - `make sdk-path` resolved `~/PlaydateSDK` via *default install location*, with
    no `PLAYDATE_SDK_PATH` set. That exercises the fallback chain rather than the
    env var the container already proves.
  - `make smoke-check-native` passed: libraries resolve, `pdc` reports 3.1.1, and
    the Simulator launched under Xvfb and stayed up without logging an error.
  - `make sdk-contract-check-native` passed `TestSDKContract` and
    `TestSetupContract`, C and Lua both, driving real games through a real
    Simulator.
  - A real MCP client over stdio ran `setup`, `build_game`, `launch_simulator`,
    `get_status`, `get_screenshot`, `press_button`, `set_crank`, `get_game_state`,
    `get_game_logs` and `stop_simulator` against a real game.

  Three things only a real SDK could establish. `launch_simulator` reported
  `data_dir_source: observed`, so the post-launch probe found the sandboxed
  directory rather than falling back to the assumption - the mechanism that
  replaced the hardcoded guess works against a real install. `setup` run with cwd
  `/tmp`, outside any Go module, wrote the harness correctly, which is the
  Checkpoint 7 `go:embed` fix confirmed where it used to fail. And the input
  overrides genuinely reach the game rather than merely returning success:
  `a_down_count` went to 1 after `press_button`, and `crank_angle` read back 123
  with `crank_docked` false after `set_crank`.

  One usability edge found by getting it wrong. `set_crank` with no `duration_ms`
  returns success and has no observable effect: the override expires before the
  next frame, so a following `get_game_state` still reads the real crank. Fine for
  `press_button`, which reads as a tap, but surprising for the crank, which reads
  as a position. Worth either a default or a word in the tool description.

  Still unverified, and deliberately: no macOS or Windows *run*. Every path value
  for those platforms comes from a probe on a real install
  (`docs/NATIVE-PROBE.md`) plus `fstest` coverage of the logic, not from this
  project executing there. Checkpoint 11 is where that changes for macOS. Windows
  never will - see the note under this checkpoint's platform scope.
- [x] **Checkpoint 9**: CI for native mode. One `native` job installing the
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

  The ubuntu `native` job was validated by running its steps by hand on this
  machine rather than by pushing and watching. Every library in its apt line
  resolves for the real Simulator binary (`ldd` clean, 21 of 21), the SDK fetch
  and extract to `~/PlaydateSDK` is byte-for-byte what the job does, and
  `make sdk-path`, `make go-build`, `make smoke-check-native` and
  `make sdk-contract-check-native` all pass in that state. See Checkpoint 8.

  Doing it that way caught two bugs that would otherwise have shipped as red
  builds. `cmd/smoke-check` still read `PLAYDATE_SDK_PATH` from the environment
  and built its paths by string concatenation - converted everywhere else and
  missed here - so `make smoke-check-native` could not work at all, which is the
  one thing it exists for. And the job originally wrapped both native targets in
  `xvfb-run`, but each already starts its own Xvfb, so that would have nested a
  second server and could have collided on a display number. Neither was findable
  by reading.

  The path-existence guard was checked both ways: it passes against the real tree
  and fails against a deliberately bogus path. Worth doing, because its whole
  purpose is catching the case where a stale glob matches nothing and the job goes
  green having tested nothing.

  The `native-macos` leg has since run, repeatedly, and is no longer advisory.
  What it established, none of which was verifiable from here: the macOS SDK
  provisions non-interactively after all (the `.zip` holds a `.pkg`, and
  `installer -pkg` handles it - the earlier "cannot provision" claim was a
  category error), detection finds the SDK through the `SDKRoot` key the
  installer writes rather than through an environment variable CI set for itself,
  the `.app` bundle path in `internal/sdk` is correct, and `smoke-check-native`
  passes.

  It stops short of `sdk-contract-check-native`, deliberately and with the reason
  recorded in `docs/GOTCHAS.md`: a fresh macOS install shows a first-run modal
  that blocks game execution, and the setting that suppresses it in the container
  does not work on macOS. Eight runs went into establishing that, most of them
  wasted on hypotheses that a screenshot disproved in one - which is its own
  lesson about diagnosing GUI applications remotely.

  Adding `native` to `main`'s required checks is still a branch-protection
  setting, not a code change, so it waits for a push and a green run.
- [x] **Checkpoint 10**: Documentation for two modes. The README currently
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
  plus inline exceptions where the platform genuinely matters. The PulseAudio
  entry is the interesting one: it is container-specific by construction, since
  it follows from the profiles forcing `SDL_AUDIODRIVER=pulseaudio`, but the
  mitigation it describes is general and right to keep.

  Done. The README now splits by mode wherever a claim differs and leaves it
  alone wherever it does not, which mattered more than the restructure: several
  statements had become wrong rather than merely incomplete. Docker was listed as
  the only requirement. `### Linux (native X11/XWayland)` titled a section about a
  container reaching a host X11 socket, which is the one heading a native user
  would go to first. One paragraph argued against a native path on macOS on
  grounds that only apply to reaching into a container. `## Local development`
  claimed the only host requirements were Docker and Go.

  The connecting section was the structural fix. It had a config block per client
  with the container command inlined into each, so adding a mode would have meant
  a block per mode per client. Splitting it into "the command" (three, by mode)
  and "the config shape" (three, by client, with placeholders) means the third
  mode *reduced* the block count.

  One correction found while re-scoping `docs/GOTCHAS.md`, and it was mine.
  A "Confirmed on macOS too" section asserted the Simulator withholds Lua console
  output from stdout on macOS as well - directly contradicting the same file's
  later, better measurement that `print()` does reach stdout and is merely
  block-buffered. The macOS probe returned a completely empty capture, missing even
  the Simulator's own native startup lines, which is a stdout that was never
  flushed rather than evidence about one channel on it. Rewritten to say what the
  measurement supports: consistent with buffering, no independent evidence either
  way, and the practical conclusion unchanged. Two documents agreeing on a wrong
  reading is worse than one being silent, so it is worth stating that the file now
  contradicted itself for several days.
- [x] **Checkpoint 11**: End-to-end verification, native. The same two example
  games as Checkpoint 5, every tool confirmed against real gameplay, with no
  Docker in the loop. This is the checkpoint that turns assumptions into either
  confirmations or bugs, so it is where the unverified macOS paths get found.
  Linux first, since it is the only platform anything here has been verified on.
  Note that on Arch this needs `webkit2gtk-4.1` from `extra` - one pacman
  install, not the AUR, contrary to what the Docker decision above used to say.
  Delete Checkpoint 6's tombstone targets here.

  Done on Linux, against two of the SDK's own examples rather than this repo's
  fixtures. That distinction was the point: the fixtures were written for the
  harness, and these were not. `Asheteroids` is a five-file Lua game;
  `Sprite Game` is C that reads input in code Panic wrote years before this
  project existed.

  Both went through every tool - `setup`, `build_game`, `launch_simulator`,
  `get_status`, `get_screenshot`, `press_button`, `set_crank`, `get_game_state`,
  `get_game_logs`, `stop_simulator` - with `harness_reachable: true` and
  `data_dir_source: observed` on each.

  The C result is the one worth reading, because `setup` had to edit a stranger's
  code and got it right in a way that is easy to get wrong. It rewrote both
  `pd->system->getButtonState` calls in `game.c` to `mcp_get_button_state`, added
  the include and `mcp_harness_init` to `main.c`, added `src/mcp_harness.c` to
  *both* the `add_executable` and `add_library` target lists in `CMakeLists.txt`,
  and added `include_directories(src)` because this project keeps its sources at
  the root rather than under `src/`. Then the part that could most plausibly have
  failed: `setUpdateCallback(update, NULL)` is in `main.c` while `update()` itself
  is defined in `game.c`, and the per-frame `mcp_harness_update` call landed
  correctly as that function's first statement, in the other file.

  One real bug found, which is what this checkpoint is for. `Asheteroids` ships
  with **no `pdxinfo` at all**, and `pdc` builds it anyway, synthesising one that
  carries `pdxversion` and `buildtime` and no `bundleID`. So `setup` and
  `build_game` both succeeded and `launch_simulator` then failed two calls later
  with `no bundleID found in .../pdxinfo` - accurate, and useless. Everything the
  harness does is keyed on the bundle ID, so there is no proceeding without one;
  the error now says that, says `pdc` does not require one which is why it
  surfaces so late, and gives the line to add. Covered by a test built from
  exactly what `pdc` emits. Worth knowing that the SDK's own examples are not all
  launchable as shipped.
- [x] **Docs drift pass**: The IPC poll interval was recorded as three different
  numbers in three files: this document said 10ms in two places, `.gremlins.yaml`
  said 100ms, and `docs/GOTCHAS.md` traced 10ms then 1ms then the fsnotify wait
  that replaced polling. None of them described the code, which has two constants
  and no polling loop: a 1ms bootstrap used once per launch while the data
  directory does not exist yet, and a 10ms backstop re-check running alongside the
  notification.

  Fixed by saying that in each place rather than by picking one number. The
  Checkpoint 4 entry keeps its 100ms-to-10ms history, since that is what happened,
  but now says so instead of reading as current behaviour.