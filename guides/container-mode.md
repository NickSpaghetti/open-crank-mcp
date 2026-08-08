# Running in container mode

Everything runs in a Docker image that carries its own SDK. Nothing is installed
on your machine but Docker.

This is the default mode and the only one CI exercises end to end. See
[native-mode.md](native-mode.md) for the other.

```
make up
```

Runs the Simulator headlessly under Xvfb by default. No display required.
Audio uses SDL2's `dummy` driver, since nothing needs real sound in
headless/automated use. Screenshots come from the Playdate API's real
framebuffer, not a window capture. Headless mode covers every tool this
server exposes.

## Two constraints that come with the container

Neither has a fix; both are worth knowing before they surprise you. Native mode has
neither.

**The game directory is fixed when the container starts.** It is a bind mount, chosen
by `GAME_DIR` at `make up` time, so switching to a different game means `make down` and
starting again with a new one.

**Output is written as root.** The container runs as root, so `build_game` leaves
root-owned `build/` and `.pdx` output inside your game directory, and `.shared-data/` is
root-owned too. Both are readable without `sudo`; deleting them needs `sudo`, or a
`docker compose run --rm` doing the `rm` from inside a container. Natively everything is
written as you.

## Seeing and hearing it

For a visual, sighted spot check with real audio, there are three profiles.
Pick the one that matches how you run Docker. All three are optional.
Nothing here is required for the MCP server's own tools, which only use the
headless path above.

None of this applies to native mode, where the Simulator is already a window on
your desktop using your own audio. These profiles exist to get a picture and a
sound *out of a container*, which is a problem native mode does not have.

### Linux (X11 socket forwarding)

```
make up-visual
```

Forwards your host's X11 socket, so the Simulator window shows up, and
routes audio to your host's PulseAudio/PipeWire server, so you can hear it.

Despite the older name for this section, it is not native mode: the Simulator is
still in the container, reaching out to your display. Native mode is
[native-mode.md](native-mode.md).
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
| `VNC_PORT` | `6080` | Host port for the browser view. Every URL below assumes the default. |
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

The "test on real hardware" and "join the developer email list" modals are
suppressed through the Simulator's own INI, using key names found in the
Simulator binary's strings.

### Volume

The Playdate's own volume slider is the volume control, and the browser follows
it. Turn it up in the VNC view and sound starts at that level; turn it down and
it stops. Nothing in the page decides when to play, so clicking the game, working
the crank or pressing a d-pad never makes noise on its own.

It works by scanning the slider off the framebuffer once a second, because that
is the only place the Simulator's volume exists. See
[shared-session.md](shared-session.md#volume-why-the-slider-is-read-off-the-framebuffer).

One thing no design can avoid. Browsers refuse to start audio until the page has
seen a user gesture. Clicking to connect to the display is enough, and in
practice you have clicked something before you care about sound. The gesture only
unlocks the browser. The slider still decides.

Use this on macOS *if you are running the containerized Simulator*. Getting a
picture and sound out of a Linux VM on macOS otherwise means XQuartz plus a
PulseAudio-over-TCP bridge, which is real ongoing complexity and slow. That
comparison is only about reaching into a container, and is not an argument
against native mode: there, the Simulator is a macOS application with a window
and CoreAudio, and none of this apparatus exists.
