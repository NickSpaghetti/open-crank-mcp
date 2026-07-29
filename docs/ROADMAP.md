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
response to the request that produced it (so a stale leftover response
from a previous run is easy to detect and ignore). Simple, cross-platform,
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
  filesystem as the one an agent was driving. The `byos` profile is one
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
  zoom levels. Verified three ways: `scripts/run-byos-unit-tests.sh` against
  synthetic columns, `scripts/byos-check.sh` against a real Simulator, and
  Playwright tests for the page behaviour.

  Two follow-ups landed on top. `cmd/byos-load` builds and launches a game in
  the running container by driving the MCP server over stdio exactly as a client
  would, since `up-byos` only ever started a container and something still had
  to call `build_game`/`launch_simulator`. And `scripts/byos-watch.sh` rebuilds
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
  `cmd/byos-load` now waits for `pactl info` to succeed before launching
  anything, and `launch_simulator` checks the process is still alive shortly
  after starting it and returns the captured output if it isn't. Written up in
  `docs/GOTCHAS.md`.
- [ ] **Checkpoint 5**: End-to-end verification, container. Build one of the
  SDK's Lua `Examples/` and one of its C `Examples/` (both with the matching
  harness wired in), run each through the containerized stack, confirm every
  tool against real gameplay for both. Independent of Checkpoints 6-11: those
  are the native-mode track and neither blocks this.
- [ ] **Checkpoint 6**: Rename the `byos` profile to `shared`. No behaviour
  change, and the point of doing it alone is that `grep -rni byos` is the whole
  review. `byos` stood for "bring your own simulator", which is not what that
  profile does - it runs its own Simulator and shares it. The name misled its
  own author during review, so it goes to `shared` and is then retired as a
  token repo-wide, leaving it free for Checkpoint 8's native mode. `virtual` was
  considered and rejected: one letter from the existing `simulator-visual`
  service and `make up-visual`.

  Touches `docker-compose.yml` (service, profile, the `./.byos-data` mount, and
  the concept comment above the service, which needs rewriting rather than
  renaming), seven `make` targets, four `scripts/*.sh` files including their
  `# shellcheck source=` directives, `tests/browser/`, `.github/workflows/ci.yml`
  (names only - anything that changes *when* a job fires belongs in Checkpoint
  9), and the Go package `cmd/byos-load`, which is why this checkpoint has to
  compile. That package hardcodes the service and profile names as bare literals
  in several places; pull them into consts so the next rename can't leave a
  half-finished one. Keep `/.byos-data/` in `.gitignore` alongside the new
  entry: the old directory is root-owned and needs `sudo` to remove, so leaving
  it ignored keeps it out of everyone's `git status`. Old target names get
  tombstones that fail with a pointer to the README's migration table, to be
  deleted at Checkpoint 11.

  Out of repo, and the order matters: drop `byos` from `main`'s required status
  checks *before* this merges, or every later PR blocks forever on a check that
  will never report again. Add `shared` after.

  Done when `grep -rni byos` returns only the `.gitignore` legacy entry and this
  checkpoint's own history, `go build ./...` and `go vet ./...` pass, and
  `make shared-load`, `make shared-watch`, `make shared-check`,
  `make test-shared-unit`, `make test-shared-types` and
  `make test-shared-browser` all still work against a real container.
- [ ] **Checkpoint 7**: Portability prep, plus one bug fix that shouldn't wait
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

  Done when `make go-build-cross` passes for all three platforms, `make go-build`
  produces a binary where the README says it will, a new test fails if the embed
  pattern breaks, and the container path is untouched: `make build`,
  `make smoke-check`, `make sdk-contract-check`.
- [ ] **Checkpoint 8**: Native mode. The same binary running against a Playdate
  SDK the developer installed themselves, no Docker. This is where `byos`
  finally means what it says, though only in prose - every identifier says
  `native`, so the retired token stays retired.

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
  WSL2 already serves those users through the existing profile, and claiming
  Windows would roughly double the untested surface for a platform nobody here
  can drive to green.

  Done when the `fstest` suite passes under `go-build-cross`, `make sdk-path`
  names both the resolved SDK and which source found it, `make
  smoke-check-native` and `make sdk-contract-check-native` pass on a host SDK,
  and a real MCP client pointed at the binary with no Docker runs `build_game`
  through `get_screenshot` and `press_button` to `setup`. Run `setup` from a cwd
  outside this repo and again from inside an unrelated Go module - that second
  one is the case Checkpoint 7 fixed.
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
