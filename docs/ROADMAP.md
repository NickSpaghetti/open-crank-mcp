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

Input override works the same way as Lua's monkey-patching. At init, the C
harness overwrites specific function pointers inside the `PlaydateAPI`
struct it's given (`pd->system->getButtonState`, for example) with its own
wrapper, keeping the original for fallback. The game's existing calls to
those functions keep working unmodified.

Hybrid C+Lua games (C for hot loops, Lua for UI, an officially supported
pattern, see the SDK's "3D library", "Exposure", and "Particles" C examples)
still run a real Lua VM. The existing Lua harness works for those unchanged.
The C harness is only needed for pure-C, no-Lua projects.

Verified end to end: the SDK's bundled "Hello World" C example builds via
`cmake -S . -B build && cmake --build build` inside the container. No ARM
toolchain needed, only `cmake` and `build-essential`, since this project
only ever targets the Simulator, never real hardware. The resulting `.pdx`
runs cleanly under Xvfb.

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

**Screenshots are raw framebuffer dumps, decoded in Go, not PNGs written by
the harness.** The Lua harness was going to call
`playdate.simulator.writeToFile()`, a Lua-only convenience that writes a PNG
directly. C has no equivalent without bundling a PNG encoder. Instead both
harnesses dump the same raw, fixed-format framebuffer bytes (400x240, 1bpp,
52-byte row stride, from `getDisplayImage()`/`getDisplayFrame()`) to a file.
The Go server already needs `image/png` from its standard library for
nothing else, and decodes that into a PNG once, uniformly, regardless of
which language wrote the game. One code path to get right and test, instead
of two.

**Everything builds and runs inside Docker, not on the bare host.** This
solves a real blocker: `PlaydateSimulator` needs
`libwebkit2gtk-4.1`/`libjavascriptcoregtk-4.1`, which aren't packaged on
Arch outside the AUR. The Debian/Ubuntu-based container sidesteps that
entirely. Xvfb makes the Simulator run fully headless by default.
Screenshots come from the Lua harness's framebuffer API, not a window
capture, so headless mode is always enough. Audio uses SDL2's `dummy`
driver, since nothing consumes sound in automated use.

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
  `getCrankChange`, `isCrankDocked`. Each checks an internal override table
  first, falls back to the real (closed-over) function otherwise.
- `mcp.update()`: once per frame, checks for `mcp/command.json` under the
  Simulator's data directory (via `playdate.file`, resolving to
  `Disk/Data/<bundleID>/mcp/`). If present, dispatches by `type`:
  - `screenshot`: `getDisplayImage()`, dumps the raw framebuffer bytes to a
    file. Go decodes to PNG, see Design decisions.
  - `press` / `release` / `crank`: updates the override table. Supports a
    duration in frames/ms, so "hold A for 200ms" auto-releases.
  - `state`: calls a user-registered inspector function, JSON-encodes the
    result.
  - `ping`: liveness check.
  Writes `mcp/response_<id>.json`, then deletes the command file.
- `mcp.registerState(fn)`: extensibility hook, so a specific game can expose
  its own debug state (player position, score, current level, entity list).
  What makes `get_game_state` useful beyond generic engine internals.

### 2. C harness (`c-harness/mcp_harness.h` + `mcp_harness.c`)

Same protocol and responsibilities as the Lua harness, for pure-C games with
no Lua VM. Added to a game's own C sources and called from its
`eventHandler`:

```c
#include "mcp_harness.h"
// on kEventInit:
mcp_harness_init(pd);
// once per frame, in the update callback:
mcp_harness_update(pd);
```

- `mcp_harness_init(pd)`: overwrites specific function pointers inside the
  `PlaydateAPI` struct (`pd->system->getButtonState`, for example) with
  wrappers that check an override state first, fall back to the original
  (retained) function otherwise. The C-side equivalent of Lua's
  monkey-patching.
- `mcp_harness_update(pd)`: same command/response file protocol via
  `playdate->file->*`, dispatching the same command types as the Lua
  harness. `screenshot` uses `getDisplayFrame()`/`getFrame()` for the raw
  bytes.
- `mcp_harness_register_state(fn)`: same extensibility hook. The callback
  returns an already-formatted JSON string. No bundled JSON writer, keeps
  the harness dependency-free. The game author formats it themselves.

### 3. Go MCP server

```
open-crank-mcp/
  go.mod
  main.go
  internal/simulator/   # process management: pdc/cmake build, launch/stop PlaydateSimulator, log capture
  internal/harness/      # file-based IPC client talking to either harness, protocol is language-agnostic
  internal/tools/        # MCP tool registrations (github.com/modelcontextprotocol/go-sdk/mcp)
  lua/mcp_harness.lua
  c-harness/mcp_harness.h
  c-harness/mcp_harness.c
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
- `launch_simulator(pdx_path)` / `stop_simulator()` / `restart_simulator()`
- `get_logs(tail_n)`: buffered stdout/stderr from the Simulator child
  process. Where `print()` output and Lua tracebacks land.
- `press_button(button, duration_ms)` / `set_crank(angle_or_delta, docked)`
- `get_screenshot()`: round-trips through the harness, reads the raw
  framebuffer dump off disk, decodes it to PNG, returns it as an MCP image
  content block.
- `get_game_state()`: round-trips through the harness's registered
  inspector, returns JSON.
- `read_save_data(filename?)` / `write_save_data(filename, json)`: direct
  file access to `Disk/Data/<bundleID>/`. No harness round-trip needed.
- `get_status()`: simulator running? bundleID? harness reachable?

IPC mechanism: the Go side writes `mcp/command_<id>.json`, then polls (short
ticker, every 10ms) for `mcp/response_<id>.json` to appear, reads and
deletes it. Correlated by request ID, so concurrent tool calls don't cross
wires. Simple, cross-platform, fast enough relative to the Simulator's own
frame rate (~30-50fps).

## Checkpoints

- [x] **Checkpoint 1**: Repo and container scaffolding. LICENSE, README,
  `docs/ROADMAP.md`, `go.mod`, `Dockerfile` (Debian/Ubuntu, Go, Xvfb,
  GTK/WebKit libs, `cmake`/`build-essential` for C games, SDK fetched from
  Panic's URL at build time), `docker-compose.yml` (headless default plus
  Linux/Windows-WSLg/universal-VNC visual and audio profiles), `Makefile`
  wrapping every command. Verified a bundled C example (Hello World) builds
  via CMake and runs in the Simulator under Xvfb. No server or harness
  logic yet.
- [ ] **Checkpoint 2**: Harnesses. `lua/mcp_harness.lua` and
  `c-harness/mcp_harness.{h,c}`. Same command/response file protocol and
  input-override approach in both. `registerState` extensibility hook in
  both. The C harness's tests build and run with
  `-fsanitize=address,undefined` (a separate build from the one shipped in
  a game, see Design decisions) as part of this checkpoint's verification,
  not left for later.
- [ ] **Checkpoint 3**: Go server core. `internal/simulator` (project-type
  detection, `pdc`/`cmake` build, launch/stop, log capture),
  `internal/harness` (file-based IPC client, works with either harness
  since the protocol doesn't care which language wrote the files), the
  raw-framebuffer-to-PNG decoder.
- [ ] **Checkpoint 4**: MCP tool registrations (`internal/tools`) wiring
  everything together via `github.com/modelcontextprotocol/go-sdk/mcp`.
- [ ] **Checkpoint 5**: End-to-end verification. Build one of the SDK's Lua
  `Examples/` and one of its C `Examples/` (both with the matching harness
  wired in), run each through the full containerized stack, confirm every
  tool against real gameplay for both.
