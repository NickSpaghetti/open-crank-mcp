# open-crank-mcp

An MCP server for the Playdate Simulator. Lets an AI agent see the screen,
press buttons, turn the crank, and read game state and logs, so it can
playtest and debug a game instead of only reading source code.

Works with Lua, C, or a mix of both.

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full plan and checkpoints.

## Status

Checkpoint 4 done: harnesses, Go server core, and MCP tool registrations
all work. See [`docs/ROADMAP.md`](docs/ROADMAP.md) for exactly what's
built and what's left.

## Requirements

- Docker
- A Playdate account, to accept the [Playdate SDK license](https://play.date/dev/sdk-license/). The image fetches the SDK itself, see below.

## Building

```
make build
```

The `Dockerfile` downloads the Playdate SDK from Panic's own download server
(`download.panic.com`) at build time, pinned to a version via the
`PLAYDATE_SDK_VERSION` build arg. This repo never bundles or redistributes
SDK files. Only the Dockerfile, which fetches them on your machine under
your own acceptance of Panic's SDK license.

**Don't publish a built image** to Docker Hub, GHCR, or anywhere else. That
would redistribute Panic's SDK to everyone who pulls it, which the
[Playdate SDK License](https://play.date/dev/sdk-license/) doesn't allow.
Build locally. Don't push the image anywhere.

## Running

```
make up
```

Runs the Simulator headlessly under Xvfb by default. No display required.
Audio uses SDL2's `dummy` driver, since nothing needs real sound in
headless/automated use. Screenshots come from the Playdate API's real
framebuffer, not a window capture. Headless mode covers every tool this
server exposes.

For a visual, sighted spot check with real audio, there are three profiles.
Pick the one that matches how you run Docker. All three are optional.
Nothing here is required for the MCP server's own tools, which only use the
headless path above.

### Linux (native X11/XWayland)

```
make up-visual
```

Forwards your host's X11 socket, so the Simulator window shows up, and
routes audio to your host's PulseAudio/PipeWire server, so you can hear it.
Needs an X11 auth cookie for your display. `scripts/ensure-xauth.sh`
generates one automatically the first time (creates or reuses
`~/.Xauthority` via `xauth generate`). No manual `xhost` step.

### Windows 11 (WSL2 + Docker Desktop)

```
make up-visual-wsl
```

Run this from inside a WSL2 distro's shell, not PowerShell. Windows 11's
WSLg already exposes a display and a PulseAudio-compatible audio socket at
`/mnt/wslg` for this. This profile mounts them through.

Untested against a real Windows machine. Built from documented WSLg
integration patterns; no Windows/WSL2 environment available to test
against. If it doesn't work, the mounts and env vars in the
`simulator-visual-wsl` service in `docker-compose.yml` are the place to fix
it.

### Any OS, universal fallback (VNC + audio stream)

```
make up-vnc
```

No host display or audio integration at all. Works the same on Linux,
Windows, and macOS through Docker Desktop's normal port publishing.
Trade-off: a browser tab instead of a native window. Open
`http://localhost:6080/vnc.html` for video, `http://localhost:8000/stream.mp3`
for audio.

Fully self-contained. The container runs its own PulseAudio daemon with a
null sink, `x11vnc` bridges the Xvfb display, `ffmpeg` re-streams the null
sink's monitor as MP3. Use this on macOS. The native alternative, XQuartz
plus a PulseAudio-over-TCP bridge, is real ongoing complexity and slow.

## Connecting from Claude Code, OpenCode, and Cursor

All three speak the same underlying MCP transport, so the command is
identical everywhere - only the config file's shape differs. This starts
Xvfb, then runs the server itself over stdio inside the container built
above, with your game's directory bind-mounted so `build_game`/
`launch_simulator` can see it:

```
docker compose -f /absolute/path/to/open-crank-mcp/docker-compose.yml \
  run --rm -T \
  -v /absolute/path/to/your-game:/your-game \
  simulator bash -c \
  "Xvfb :99 -screen 0 1280x800x24 & sleep 1 && DISPLAY=:99 PLAYDATE_SDK_PATH=/opt/playdate-sdk open-crank-mcp"
```

`-T` disables pseudo-TTY allocation - required for stdio JSON-RPC, a real
TTY corrupts the framing. `PLAYDATE_SDK_VERSION` in the image, and both
absolute paths, are the only things you need to adjust per-machine.

**Claude Code**: a `.mcp.json` at your game project's root:

```json
{
  "mcpServers": {
    "open-crank-mcp": {
      "command": "docker",
      "args": [
        "compose", "-f", "/absolute/path/to/open-crank-mcp/docker-compose.yml",
        "run", "--rm", "-T",
        "-v", "/absolute/path/to/your-game:/your-game",
        "simulator", "bash", "-c",
        "Xvfb :99 -screen 0 1280x800x24 & sleep 1 && DISPLAY=:99 PLAYDATE_SDK_PATH=/opt/playdate-sdk open-crank-mcp"
      ]
    }
  }
}
```

Or register it without a file, via `claude mcp add`:

```
claude mcp add open-crank-mcp -- docker compose -f /absolute/path/to/open-crank-mcp/docker-compose.yml run --rm -T -v /absolute/path/to/your-game:/your-game simulator bash -c "Xvfb :99 -screen 0 1280x800x24 & sleep 1 && DISPLAY=:99 PLAYDATE_SDK_PATH=/opt/playdate-sdk open-crank-mcp"
```

**OpenCode**: the `mcp` key in `opencode.jsonc`/`opencode.json` (project
config, or `~/.config/opencode/opencode.jsonc` for global), matching
`McpLocalConfig`'s shape (`type`/`command`/`environment`) from
`@opencode-ai/sdk`:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "open-crank-mcp": {
      "type": "local",
      "command": [
        "docker", "compose", "-f", "/absolute/path/to/open-crank-mcp/docker-compose.yml",
        "run", "--rm", "-T",
        "-v", "/absolute/path/to/your-game:/your-game",
        "simulator", "bash", "-c",
        "Xvfb :99 -screen 0 1280x800x24 & sleep 1 && DISPLAY=:99 PLAYDATE_SDK_PATH=/opt/playdate-sdk open-crank-mcp"
      ]
    }
  }
}
```

Or `opencode mcp add open-crank-mcp` interactively.

**Cursor**: `.cursor/mcp.json` in your game project (or `~/.cursor/mcp.json`
globally) - the same `mcpServers`/`command`/`args` shape as Claude Code:

```json
{
  "mcpServers": {
    "open-crank-mcp": {
      "command": "docker",
      "args": [
        "compose", "-f", "/absolute/path/to/open-crank-mcp/docker-compose.yml",
        "run", "--rm", "-T",
        "-v", "/absolute/path/to/your-game:/your-game",
        "simulator", "bash", "-c",
        "Xvfb :99 -screen 0 1280x800x24 & sleep 1 && DISPLAY=:99 PLAYDATE_SDK_PATH=/opt/playdate-sdk open-crank-mcp"
      ]
    }
  }
}
```

## Setting up a game

Once the server is connected (see above), the fastest way to wire the
harness into a game is the `setup` tool: call it with `source_dir`
pointing at your project, and it detects whether the project is Lua, C,
or a hybrid of both, then copies the harness file(s) in and patches
`main.lua`/`CMakeLists.txt`/your `eventHandler` for you - no manual glue
code for most projects. Pass `language` (`"lua"|"c"|"hybrid"`) to
override detection on the rare project it guesses wrong.

`setup` reports exactly what it did: `files_copied`, `files_patched`, and
(C only) `manual_steps` for anything it found but couldn't safely
automate - e.g. no confidently-identifiable `PlaydateAPI*` variable
reachable from your update callback - rather than guessing. It's
idempotent: re-running it against an already-set-up project is always
safe, and each already-current file is reported as unchanged.

The paired `teardown` tool reverses this: strips exactly what `setup`
added and removes the copied harness files. It's deliberately
conservative - if it finds any harness reference (an `#include`, a
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
  wrapper functions - see below). That rewrite can't be marked or
  reversed the way a whole-line insertion can, so once `setup` has
  touched a C project's input calls, `teardown` for it is permanent from
  then on - it becomes a no-op rather than leaving input calls pointing
  at wrapper functions that no longer exist.
- `CMakeLists.txt` can't use marker comments at all (a CMake `#` comment
  runs to end of line, so one placed mid-argument-list would comment out
  the rest of that call) - its `src/mcp_harness.c` source entry is
  recognized by exact text instead.

Both tools work purely on the filesystem. No simulator needs to be
running, and neither touches anything outside `source_dir`.

## Wiring a game into the harness by hand

`setup` (above) automates everything below for most projects. What
follows is what it does under the hood - useful for understanding the
wiring, resolving a `manual_steps` entry, or wiring a game up yourself.

There's no package manager for Playdate projects. `pdc` only ever
compiles what's physically under your own `Source/` directory, and
CMake's `add_library`/`add_executable` need locally-relative source
paths. So integrating either harness is a two-step copy-then-call, not
just a call:

**Lua games:**
1. Copy [`lua/mcp_harness.lua`](lua/mcp_harness.lua) into your project's
   `Source/` directory.
2. In `main.lua`: `import "mcp_harness"`, write your per-frame logic as a
   plain function, and pass it to `mcp.run(yourUpdateFn)` once - instead
   of assigning `playdate.update` yourself. This is what makes
   `get_game_logs` (see below) actually reliable: `mcp.run` wraps your
   function in `xpcall`, so the harness's own command polling keeps
   running every frame even if your code throws, instead of the whole
   update loop freezing silently. See
   [`lua/test-fixture/Source/main.lua`](lua/test-fixture/Source/main.lua)
   for a minimal, complete example. (Manually assigning `playdate.update`
   and calling `mcp.update()` yourself each frame still works, for
   backward compatibility, but doesn't get this protection.)
3. Lua `print()` output and unhandled-error tracebacks never reach the
   Simulator's real stdout/stderr - a platform limitation, not a bug in
   this project, see [`docs/GOTCHAS.md`](docs/GOTCHAS.md). `mcp.run`
   captures both into `mcp/game_logs.json` automatically; read it back
   with the `get_game_logs` tool rather than `get_logs` (which only sees
   the Simulator process's own OS-level output).
4. `press_button` synthesizes real button-down/up edges, so
   `buttonJustPressed`/`buttonJustReleased` and the SDK's
   `AButtonDown`/`leftButtonDown`/etc callbacks all fire correctly from
   an MCP-driven press, not just `buttonIsPressed`'s "currently held"
   bit. `AButtonHeld`/`BButtonHeld` (fired after a continuous 1-second
   hold) are a separate mechanism and aren't synthesized.

**C games:**
1. Copy [`c-harness/mcp_harness.h`](c-harness/mcp_harness.h) and
   [`c-harness/mcp_harness.c`](c-harness/mcp_harness.c) into your
   project, and add `mcp_harness.c` to your CMakeLists.txt's source list
   (alongside your own `.c` files, in the same `add_library`/
   `add_executable` call).
2. In your `eventHandler`: `#include "mcp_harness.h"`, call
   `mcp_harness_init(pd)` once on `kEventInit`, and call
   `mcp_harness_update(pd)` once per frame from your update callback. See
   [`c-harness/test/fixture-game/`](c-harness/test/fixture-game/) for a
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
   [`c-harness/mcp_harness.h`](c-harness/mcp_harness.h) for why.
4. `mcp_get_button_state`'s `pushed`/`released` outputs synthesize real
   edges from an active override, not just the `current` "currently
   held" bit - so a game reading `pushed & kButtonA` for a one-shot
   action (fire, jump) works correctly from an MCP-driven press.

Either way, the game builds through the SDK's own CMake support
(`cmake -S . -B build && cmake --build build`, which invokes `pdc` itself
as a post-build step) or plain `pdc` for Lua-only projects. The image
already has `cmake` and `build-essential`. No ARM toolchain needed, since
this project only targets the Simulator, never real hardware.

**Hybrid C+Lua games** (C for hot loops, Lua for UI, an officially
supported pattern) can use the Lua harness alone, since a real Lua VM is
still running.

## License

This project's own code is MIT (see `LICENSE`). The Playdate SDK is
licensed separately by Panic, Inc., see the
[Playdate SDK License](https://play.date/dev/sdk-license/). Not affiliated
with or endorsed by Panic.
