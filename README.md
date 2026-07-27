# open-crank-mcp

An MCP server for the Playdate Simulator. Lets an AI agent see the screen,
press buttons, turn the crank, and read game state and logs, so it can
playtest and debug a game instead of only reading source code.

Works with Lua, C, or a mix of both.

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full plan and checkpoints.

## Status

Checkpoint 1 done: repo and container scaffolding. No server or harness code
yet.

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

## Wiring a game into the harness

Not built yet (Checkpoint 2).

- Lua games: one `import` line and one per-frame call in `main.lua`.
- C games: one `#include` and two calls (init and one per-frame) in the
  `eventHandler`/update callback. C games build through the SDK's own CMake
  support (`cmake -S . -B build && cmake --build build`, which invokes `pdc`
  itself as a post-build step). The image already has `cmake` and
  `build-essential`. No ARM toolchain needed, since this project only
  targets the Simulator, never real hardware.
- Hybrid C+Lua games (C for hot loops, Lua for UI, an officially supported
  pattern) can use the Lua harness alone, since a real Lua VM is still
  running.

## License

This project's own code is MIT (see `LICENSE`). The Playdate SDK is
licensed separately by Panic, Inc., see the
[Playdate SDK License](https://play.date/dev/sdk-license/). Not affiliated
with or endorsed by Panic.
