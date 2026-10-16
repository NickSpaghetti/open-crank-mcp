# Running in container mode

Everything runs in a Docker image that carries its own SDK. Nothing is installed
on your machine but Docker.

This is the default mode. See [native-mode.md](native-mode.md) for the other.

An MCP client starts its own headless container per connection, with
`docker compose run` (see [connecting.md](connecting.md)). You only build the
image:

```
make build
```

The container runs the Simulator under Xvfb, so no display is required. Audio
uses SDL2's `dummy` driver. Screenshots come from the Playdate API's
framebuffer, not a window capture. Headless mode covers every tool this server
exposes. `make up` starts the same headless container by hand.

## Three constraints that come with the container

None has a fix. Native mode has none of them.

**The game directory is fixed when the container starts.** It is a bind mount: the
`-v` path in the client's command, or `GAME_DIR` for the
[shared session](shared-session.md). Switching games means changing that path and
starting a new container. For the shared session that is `make down`, then
`make up-shared` with a new `GAME_DIR`.

**Output is written as root.** The container runs as root, so `build_game` leaves
root-owned `build/` and `.pdx` output inside your game directory, and `.shared-data/` is
root-owned too. Both are readable without `sudo`; deleting them needs `sudo`, or a
`docker compose run --rm` doing the `rm` from inside a container. Natively everything is
written as you.

**The image is always `linux/amd64`, so an arm64 host emulates.** The SDK baked into
these images is Panic's **Linux** SDK, and Panic ships that one for x86_64 only. There
is no ARM64 Linux build, so there is nothing for an arm64 image to drive. Every service
in `docker-compose.yml` pins the platform for that reason. On an amd64 host the pin
costs nothing. On an Apple Silicon Mac or an arm64 Linux box, the whole container runs
under emulation. That works but is slower. Expect the first `make build` to take
several minutes, most of it fetching the SDK and the Go toolchain.

This is about the Linux SDK in the container, not about macOS. Panic ships the
**macOS** SDK as a universal binary, so [native mode](native-mode.md) on an Apple
Silicon Mac runs the Simulator without Rosetta. If the emulation overhead bothers
you, native mode avoids it.

Emulation has to be available, or the container build fails outright:

- **macOS (Apple Silicon):** turn on "Use Rosetta for x86_64/amd64 emulation on Apple
  Silicon" in Docker Desktop's settings, General tab.
- **Linux (arm64):** install qemu-user binfmt support, e.g.
  `docker run --privileged --rm tonistiigi/binfmt --install amd64`.

Without the pin, Docker builds an arm64 image around the amd64 Go toolchain, and the
cgo step fails with `gcc: error: unrecognized command-line option '-m64'`.

## Seeing and hearing it

To watch and listen, there are three profiles. Pick the one that matches how you
run Docker. All three are optional. The MCP tools only use the headless path
above.

None of this applies to native mode, where the Simulator is already a window on
your desktop using your own audio.

### Linux (X11 socket forwarding)

```
make up-visual
```

Forwards your host's X11 socket, so the Simulator window shows up, and
routes audio to your host's PulseAudio/PipeWire server. The Simulator is still in
the container. This is not native mode.

Needs an X11 auth cookie for your display. `scripts/ensure-xauth.sh` creates or
reuses `~/.Xauthority` with `xauth generate`. No manual `xhost` step.

### Windows 11 (WSL2 + Docker Desktop)

```
make up-visual-wsl
```

Run this from inside a WSL2 distro's shell, not PowerShell. Windows 11's
WSLg already exposes a display and a PulseAudio-compatible audio socket at
`/mnt/wslg` for this. This profile mounts them through.

Untested on a real Windows machine. If it does not work, the mounts and env vars
in the `simulator-visual-wsl` service in `docker-compose.yml` are the place to fix
it.

### Any OS, universal fallback (VNC + audio stream)

```
make up-vnc
```

No host display or audio integration at all. Works the same on Linux,
Windows, and macOS through Docker Desktop's normal port publishing.
Trade-off: a browser tab instead of a native window. Open
`http://localhost:6080/` for both video and audio. The audio player sits in
the bottom right corner of that page. `audio.html` on the same port serves
the player on its own if you want sound without the video.

Audio is an endless MP3 stream on port 8000. Use the pages above rather than
pointing a browser at the stream directly. A fresh encoder starts per listener,
which is what keeps the lag under a second. See
[shared-session.md](shared-session.md#audio-why-the-stream-restarts-per-listener).

Both ports are published on `127.0.0.1` only. The VNC server runs with no
password and allows multiple simultaneous clients, so anyone who reaches
port 6080 can drive the Simulator, not just watch it. Publishing on
`0.0.0.0` would also slip past a host firewall, since Docker's iptables
rules sit ahead of the filter table.

To reach it from another machine, forward the ports over SSH:

```
ssh -L 6080:localhost:6080 -L 8000:localhost:8000 user@host
```

`BIND_ADDR` overrides the bind address if you want it on the network
directly. Use it on a network you trust.

| Variable | Default | Effect |
|---|---|---|
| `BIND_ADDR` | `127.0.0.1` | Host address for both published ports. `0.0.0.0` serves every interface. |
| `VNC_PORT` | `6080` | Host port for the browser view. The URLs here assume the default. |
| `AUDIO_PORT` | `8000` | Host port for the MP3 stream. |

### Simulator defaults

The display is a workspace, not a frame around the Simulator. The Simulator
opens at its normal size, pinned to the top-left corner, and everything left
over is room to drag its console and other debugging windows into.

| Setting | Default | Why |
|---|---|---|
| `SIM_ZOOM` | `1` | The Simulator's own default size. `Ctrl-1`/`Ctrl-2`/`Ctrl-3` change it live. |
| `VNC_GEOMETRY` | `1280x800` | Workspace size. The Simulator's window is `400*zoom+82` by `240*zoom+466`, that 466 being fixed chrome, so the rest is free space. |

A window manager runs in the container, which is what lets you move, resize
and focus windows. Two of openbox's defaults are disabled because both of
them make the Simulator look like it vanished: the four virtual desktops,
where a stray scroll over the background switches to an empty one, and the
keybindings, one of which unmaps every window at once. They also collided
with the Simulator's own `Ctrl-1`/`Ctrl-2`/`Ctrl-3` and `Shift-Ctrl-C`.

The "test on real hardware" and "join the developer email list" dialogs are
turned off through the Simulator's own INI.

### Volume

The Playdate's own volume slider is the volume control, and the browser follows
it. Turn it up in the VNC view and sound starts at that level; turn it down and
it stops. Nothing in the page decides when to play, so clicking the game, working
the crank or pressing a d-pad never makes noise on its own.

It works by scanning the slider off the framebuffer once a second, because that
is the only place the Simulator's volume exists. See
[shared-session.md](shared-session.md#volume-why-the-slider-is-read-off-the-framebuffer).

Browsers refuse to start audio until the page has seen a user gesture. Clicking
to connect to the display is enough. The gesture only unlocks the browser. The
slider still decides.

On macOS, VNC is the way to see a containerized Simulator. The alternative is
XQuartz plus a PulseAudio-over-TCP bridge. In native mode none of this applies.
