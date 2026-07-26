# open-crank-mcp

An MCP server that lets an AI agent play and debug a [Playdate](https://play.date)
game running in the desktop Simulator: screenshots, button/crank input, game
state, logs, and save data — for playtesting, debugging, and level design.

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full vision and checkpoint
plan.

## Status

Checkpoint 1: repo and container scaffolding. No server or harness logic yet.

## Requirements

- Docker
- A Playdate account to accept the [Playdate SDK license](https://play.date/dev/sdk-license/) (the image fetches the SDK itself, see below)

## Building

```
make build
```

The `Dockerfile` downloads the Playdate SDK directly from Panic's own
download server (`download.panic.com`) at build time, pinned to a specific
version via the `PLAYDATE_SDK_VERSION` build arg (see `Makefile`). This repo
never bundles or redistributes any SDK files itself — only a Dockerfile that
fetches them on your machine, under your own acceptance of Panic's SDK
license.

**Do not publish a built image** (Docker Hub, GHCR, or any other registry).
Doing so would redistribute Panic's SDK to everyone who pulls it, which the
[Playdate SDK License](https://play.date/dev/sdk-license/) does not permit.
Build locally; don't push the image anywhere.

## Running

```
make up
```

Runs the Simulator headlessly under Xvfb by default — no display required,
and audio uses SDL2's `dummy` driver (no consumer needs real sound in
headless/automated use). Screenshots are captured through the Playdate Lua
API's real framebuffer, not a window capture, so headless mode is sufficient
for every tool this server exposes.

For a visual, sighted spot-check with real audio, there are three profiles —
pick the one matching how you're running Docker. All of them are strictly
optional; nothing here is required for the MCP server's own tools, which
only ever use the headless path above.

### Linux (native X11/XWayland)

```
make up-visual
```

Forwards your host's X11 socket so the Simulator window is visible, and
routes audio to your host's PulseAudio/PipeWire server so you can actually
hear it. Needs an X11 auth cookie for your display, which
`scripts/ensure-xauth.sh` generates automatically the first time (creates or
reuses `~/.Xauthority` via `xauth generate`) — no manual `xhost` step needed.

### Windows 11 (WSL2 + Docker Desktop)

```
make up-visual-wsl
```

Run this from inside a WSL2 distro's shell (not PowerShell directly) —
Windows 11's WSLg already exposes a display and a PulseAudio-compatible
audio socket at `/mnt/wslg` for exactly this purpose, and this profile just
mounts them through. **This profile is built from documented WSLg
integration patterns but has not been verified against a real Windows
machine** — there's no Windows/WSL2 environment available to test it in.
If it doesn't work as-is, the mounts/env vars in the
`simulator-visual-wsl` service in `docker-compose.yml` are the place to
adjust.

### Any OS — universal fallback (VNC + audio stream)

```
make up-vnc
```

No host display/audio integration of any kind — works identically on
Linux, Windows, and macOS via Docker Desktop's normal port publishing, at
the cost of needing a browser tab instead of a native window. Open
`http://localhost:6080/vnc.html` for video (noVNC), and
`http://localhost:8000/stream.mp3` for audio (any player or a browser tab).
Fully self-contained: the container runs its own PulseAudio daemon with a
null sink, `x11vnc` bridges the Xvfb display, and `ffmpeg` re-streams the
null sink's monitor as MP3. This is the one to reach for on macOS, where
the native alternative (XQuartz + a PulseAudio-over-TCP bridge) is real
ongoing complexity and notably slower.

## Wiring a game into the harness

Not yet implemented (Checkpoint 2). Will require adding one `import` line and
one per-frame call to your game's `main.lua`.

## License

This project's own code is MIT-licensed (see `LICENSE`). The Playdate SDK
itself is licensed separately by Panic, Inc. — see
[the Playdate SDK License](https://play.date/dev/sdk-license/). This project
is not affiliated with or endorsed by Panic.
