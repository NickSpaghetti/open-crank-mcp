# Roadmap

## Vision

An MCP server that lets an AI agent actually *play* a Playdate game running
in the desktop Simulator — see the screen, press buttons, turn the crank,
read game state, and read logs — so the agent can playtest, debug, and help
with level design instead of only reading/writing source code blind.

## Design decisions

- **Language: Go**, using the official `github.com/modelcontextprotocol/go-sdk`.
  Chosen for cross-platform reach (Windows 11 / macOS / Linux) via trivial
  `GOOS`/`GOARCH` cross-compilation to small, dependency-free static
  binaries — no runtime needs to be installed on the end user's machine. The
  actual workload (subprocess management, ~10ms file polling, JSON/PNG
  shuttling) is I/O-bound, so this isn't chosen for raw throughput over
  C#/.NET — it's the smaller/simpler distribution story across three OSes.

- **Input/screenshots via a Lua harness, not OS-level automation.** There is
  no documented Playdate API to inject button/crank input from outside the
  Simulator process — `playdate.buttonIsPressed`, `getCrankPosition`, etc.
  are read-only getters. The only way to fake input is to monkey-patch those
  functions from inside the running Lua game itself. A small harness file
  (`lua/mcp_harness.lua`) gets dropped into the user's game (one `import` +
  one per-frame call) and talks to the Go server over file-based
  command/response JSON. This was chosen over OS-level window screenshotting
  + synthetic keystrokes because it uses only documented SDK APIs, needs no
  extra host tooling, and gives real framebuffer screenshots
  (`playdate.graphics.getDisplayImage()`) plus real input simulation.

- **No "eval arbitrary Lua" tool**, for now. Considered and rejected: real
  code-execution risk inside the running game process. Revisit later behind
  an explicit opt-in flag if a concrete need comes up.

- **Everything builds/runs inside Docker**, not on the bare host. This
  directly solves a real blocker: `PlaydateSimulator` needs
  `libwebkit2gtk-4.1`/`libjavascriptcoregtk-4.1`, which aren't packaged on
  Arch outside the AUR; the Debian/Ubuntu-based container sidesteps that
  entirely. Xvfb makes the Simulator run fully headless by default (our
  screenshots come from the Lua harness's framebuffer API, not a window
  capture, so headless is always sufficient, with audio on SDL2's `dummy`
  driver since nothing consumes sound in automated use).

- **Visual/audio spot-checks are optional and platform-specific**, since
  Docker Desktop always runs a Linux VM regardless of host OS, but "let a
  human see and hear it" means reaching out of that VM differently per
  platform:
  - Linux: bind-mount the host's X11 socket and forward audio to the host's
    PulseAudio/PipeWire server. Needs an Xauth cookie for the display, which
    `scripts/ensure-xauth.sh` generates automatically.
  - Windows 11: WSL2's WSLg already exposes a display and a
    PulseAudio-compatible socket at `/mnt/wslg` for exactly this purpose, so
    the same idea applies with different mount paths. Built from documented
    WSLg integration patterns but **not verified against a real Windows
    machine**.
  - macOS has no equivalent built in — the native option (XQuartz +
    PulseAudio-over-TCP) is real ongoing complexity and notably slow. Instead
    there's a **universal VNC-based fallback** that works identically on any
    OS via Docker Desktop's normal port publishing: the container runs its
    own PulseAudio daemon with a null sink, `x11vnc` bridges the Xvfb
    display, and `ffmpeg` re-streams the null sink's monitor as an MP3 HTTP
    stream — video via a browser (noVNC), audio via any player. See
    `README.md` for the three `make up-visual*` targets.

- **The SDK is fetched from Panic's own servers at image-build time**, not
  bundled in this repo. The Playdate SDK License bans redistributing the
  SDK. The `Dockerfile` here is just source that `curl`s
  `download.panic.com` during each user's own local `docker build` — that
  user is the one downloading/accepting Panic's license, not this project.
  This only holds as long as a *built image* is never published to any
  registry (that would flip it into redistribution). See `README.md`.

- **Project name and license.** Named `open-crank-mcp`, not `playdate-mcp`
  — the Playdate SDK License bans using "Playdate" or "Panic" in the name of
  anything built with the SDK. This project's own code is MIT-licensed,
  matching every other original repo under `~/GitProjects/NickSpaghetti/`.
  Precedent for this kind of community tooling around the SDK: unofficial
  third-party Rust bindings (`crankstart`, `craydate`, `boozook/playdate`)
  generate FFI bindings directly from Panic's C headers — a more aggressive
  reading of the license's "don't build another SDK" clause than this
  project attempts — and have operated openly for years without apparent
  enforcement action. Not a legal guarantee, just a documented risk
  assessment; revisit if Panic ever raises a concern.

## Architecture

Two pieces:

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
  `getCrankChange`, `isCrankDocked` — each checks an internal override table
  first, falling back to the real (closed-over) function otherwise.
- `mcp.update()`: once per frame, checks for `mcp/command.json` under the
  Simulator's data directory (via `playdate.file`, resolving to
  `Disk/Data/<bundleID>/mcp/`). If present, dispatches by `type`:
  - `screenshot` → `getDisplayImage()` + `simulator.writeToFile(...)`
  - `press` / `release` / `crank` → updates the override table (supports a
    duration in frames/ms so "hold A for 200ms" auto-releases)
  - `state` → calls a user-registered inspector function, JSON-encodes result
  - `ping` → liveness check
  Writes `mcp/response_<id>.json`, then deletes the command file.
- `mcp.registerState(fn)`: extensibility hook so a specific game can expose
  its own debug state (player position, score, current level, entity list) —
  what makes `get_game_state` useful beyond generic engine internals.

### 2. Go MCP server

```
open-crank-mcp/
  go.mod
  main.go
  internal/simulator/   # process management: pdc build, launch/stop PlaydateSimulator, log capture
  internal/harness/      # file-based IPC client talking to mcp_harness.lua
  internal/tools/        # MCP tool registrations (github.com/modelcontextprotocol/go-sdk/mcp)
  lua/mcp_harness.lua
  docs/ROADMAP.md
  Dockerfile
  docker-compose.yml
  Makefile
  LICENSE
  README.md
```

Tools exposed:
- `build_game(source_dir)` — runs `pdc`, returns compile errors/warnings
- `launch_simulator(pdx_path)` / `stop_simulator()` / `restart_simulator()`
- `get_logs(tail_n)` — buffered stdout/stderr from the Simulator child
  process (where `print()` output and Lua tracebacks land)
- `press_button(button, duration_ms)` / `set_crank(angle_or_delta, docked)`
- `get_screenshot()` — round-trips through the harness, reads the resulting
  PNG off disk, returns it as an MCP image content block
- `get_game_state()` — round-trips through the harness's registered
  inspector, returns JSON
- `read_save_data(filename?)` / `write_save_data(filename, json)` — direct
  file access to `Disk/Data/<bundleID>/`, no harness round-trip needed
- `get_status()` — simulator running?, bundleID, harness reachable?

IPC mechanism: the Go side writes `mcp/command_<id>.json`, then polls (short
ticker, e.g. every 10ms) for `mcp/response_<id>.json` to appear, reads and
deletes it. Correlated by request ID so concurrent tool calls don't cross
wires — simple, cross-platform, and fast enough relative to the Simulator's
own frame rate (~30-50fps).

## Checkpoints

- [x] **Checkpoint 1** — Repo + container scaffolding: LICENSE, README,
  `docs/ROADMAP.md`, `go.mod`, `Dockerfile` (Debian/Ubuntu + Go + Xvfb +
  GTK/WebKit libs + SDK fetched from Panic's URL at build time),
  `docker-compose.yml` (headless default + Linux/Windows-WSLg/universal-VNC
  visual+audio profiles), `Makefile` wrapping every command. No server or
  harness logic yet.
- [ ] **Checkpoint 2** — Lua harness (`lua/mcp_harness.lua`): command/response
  file protocol, input-override monkey-patching, `mcp.registerState(fn)`.
- [ ] **Checkpoint 3** — Go server core: `internal/simulator` (pdc build,
  launch/stop, log capture) and `internal/harness` (file-based IPC client).
- [ ] **Checkpoint 4** — MCP tool registrations (`internal/tools`) wiring
  everything together via `github.com/modelcontextprotocol/go-sdk/mcp`.
- [ ] **Checkpoint 5** — End-to-end verification: build one of the SDK's
  `Examples/` games with the harness wired in, run it through the full
  containerized stack, confirm every tool against real gameplay.
