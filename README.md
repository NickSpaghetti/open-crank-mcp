# open-crank-mcp

An MCP server for the Playdate Simulator. Lets an AI agent see the screen,
press buttons, turn the crank, and read game state and logs, so it can
playtest and debug a game instead of only reading source code.

Works with Lua, C, or a mix of both.

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full plan and checkpoints.

## Two ways to run it

The server does the same job either way. What differs is where the Simulator
lives.

**Container mode** builds an image that carries its own SDK and runs the
Simulator headlessly inside it. Nothing is installed on your machine but Docker.
This is the default, and the only mode CI exercises end to end on every change.

**Native mode** runs the server directly against a Playdate SDK you installed
yourself, with no container at all. The Simulator is an ordinary window on your
desktop, with your audio, and no bind mounts or root-owned build output.

Container mode if you want it reproducible or you are on a machine that cannot
run the Simulator. Native mode if you already have the SDK and want to watch a
game while an agent plays it. Both are supported; neither is a migration path to
the other.

## Status

Checkpoints 1-9 done. Both modes work: the harnesses, the Go server, all the MCP
tools, the container profiles, and native SDK detection with per-OS paths. Native
mode is verified on Linux. macOS path values come from a probe on a real install
rather than from running there; Windows-native is not supported, see below. See
[`docs/ROADMAP.md`](docs/ROADMAP.md) for exactly what is built, what is verified
and how, and what is left.

## Requirements

**Container mode**

- Docker
- A Playdate account, to accept the [Playdate SDK license](https://play.date/dev/sdk-license/). The image fetches the SDK itself, see below.

**Native mode**

- The Playdate SDK, installed by you. `~/.Playdate/config` or the default
  install location is enough; no environment variable is required.
- Go, to build the server binary.
- `cmake` and a C compiler, but only for building C games. A Lua-only game needs
  neither.
- Whatever shared libraries the Simulator itself needs. The authoritative list is
  the `apt` line in the `native` job of
  [`.github/workflows/ci.yml`](.github/workflows/ci.yml), which is kept correct
  because that job would fail otherwise. On Arch that is one package,
  `webkit2gtk-4.1` from `extra`.

Windows-native is not supported. WSL2 covers Windows through container mode, and
the Windows SDK ships as an interactive installer rather than an archive, so CI
could never provision a runner to verify a native path there.

## Building

**Container mode**

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

**Native mode**

```
make go-build
```

Produces `./open-crank-mcp`. This repo never touches the SDK in this mode: you
installed it, under your own acceptance of Panic's licence, and the server only
reads the path. The redistribution question is structurally absent rather than
mitigated.

Check what it found before wiring anything up:

```
make sdk-path
```

That prints the SDK, which of the three sources located it, and every candidate
it considered. It is the first thing to reach for if detection picks the wrong
SDK or none.

## Running: container mode

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
[further down](#running-native-mode).
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

Audio is an endless MP3 stream on port 8000. Point a browser straight at that
stream and you get a media viewer that reports it as 0:00 long and often won't
start, which is why the pages above wrap it in an `<audio>` element instead.

The stream is served by `socat`, which starts a fresh encoder per listener. That
is not an implementation detail: a single long-lived `ffmpeg -listen 1` opens its
pulse input before it opens its output, so it captures into a queue while waiting
for someone to connect, and hands over the backlog when they do. Measured with a
beep played at a known moment, that was **22 seconds** of lag after a few idle
minutes, held for the whole session - late audio that keeps playing after you
pause the game. Starting the encoder at connect time keeps it under a second, and
as a side effect several listeners can share the stream.

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
| `BIND_ADDR` | `127.0.0.1` | Host address for ports 6080 and 8000. `0.0.0.0` serves every interface. |

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

That works by reading the slider off the framebuffer, because the slider is the
only place the Simulator's volume exists: the SDK exposes `getVolume()` on its
system API with no setter, every `setVolume()` in the sound API is per-source,
and the Simulator's INI has no key for it. So a one-pixel-wide column down the
device frame gets scanned once a second. It crosses the LOCK button, the MENU
button, the volume track, its knob and the mute icon, and each reads as either
dark or light against the yellow frame. The knob's position along the track is
the volume, published to `pd-volume.json` for the page to poll.

Geometry is re-read on every scan, so dragging the Simulator window or changing
its zoom can't leave it reading the wrong pixels. A scan that can't find the
slider publishes `-1`, and the page leaves the audio exactly as it is: acting on
a failed read would silence working audio, which looks identical to a broken
pipeline.

One thing no design can avoid: browsers refuse to start audio until the page has
seen a user gesture. Clicking to connect to the display is enough, and in
practice you have clicked something before you care about sound. The gesture only
unlocks the browser; the slider still decides.

Fully self-contained. The container runs its own PulseAudio daemon with a
null sink, `x11vnc` bridges the Xvfb display, `ffmpeg` re-streams the null
sink's monitor as MP3.

Use this on macOS *if you are running the containerized Simulator*. Getting a
picture and sound out of a Linux VM on macOS otherwise means XQuartz plus a
PulseAudio-over-TCP bridge, which is real ongoing complexity and slow. That
comparison is only about reaching into a container, and is not an argument
against native mode: there, the Simulator is a macOS application with a window
and CoreAudio, and none of this apparatus exists.

## Running: native mode

No `make up`. There is no container to start: an MCP client launches the server
binary directly, and the server launches the Simulator.

```
make go-build     # produces ./open-crank-mcp
make sdk-path     # confirm it finds your SDK
```

Then point a client at the binary, as below. The Simulator appears as an ordinary
window when a game launches, using your display and your audio, so none of the
container display profiles above apply.

Two checks worth running once, if you want to know the environment is sound
before involving an agent:

```
make smoke-check-native        # libraries resolve, pdc runs, the Simulator starts
make sdk-contract-check-native # the MCP tools driving a real Simulator
```

## Connecting from Claude Code, OpenCode, and Cursor

All three speak the same MCP transport over stdio. What differs between them is
the config file's shape; what differs between modes is the command. Those are two
separate axes, so they are listed separately rather than as one block per
combination.

### The command

**Native.** Nothing but the binary.

```
/absolute/path/to/open-crank-mcp/open-crank-mcp
```

No arguments. Your game's path is passed to the tools at call time rather than
mounted, so there is nothing per-project to configure. If your SDK is somewhere
`make sdk-path` does not find, add `PLAYDATE_SDK_PATH` to the client's `env`.

**Container, one per connection.** Starts Xvfb, runs the server over stdio inside
the image, with your game bind-mounted so `build_game` and `launch_simulator` can
see it.

```
docker compose -f /absolute/path/to/open-crank-mcp/docker-compose.yml \
  run --rm -T \
  -v /absolute/path/to/your-game:/your-game \
  simulator bash -c \
  "Xvfb :99 -screen 0 1280x800x24 & sleep 1 && DISPLAY=:99 PLAYDATE_SDK_PATH=/opt/playdate-sdk open-crank-mcp"
```

`-T` disables pseudo-TTY allocation, which stdio JSON-RPC requires: a real TTY
corrupts the framing. The two absolute paths are the only per-machine parts.

**Container, shared with a human.** Attaches to the long-lived container from
[Shared, watchable session](#shared-watchable-session) instead of creating one.

```
docker compose -f /absolute/path/to/open-crank-mcp/docker-compose.yml \
  exec -T simulator-shared open-crank-mcp
```

### The config shape

Take the command from above and drop it in. `<COMMAND>` is the executable,
`<ARGS>` the rest as separate strings. Native mode has no `<ARGS>` at all.

**Claude Code**: a `.mcp.json` at your game project's root.

```json
{
  "mcpServers": {
    "open-crank-mcp": {
      "command": "<COMMAND>",
      "args": [<ARGS>]
    }
  }
}
```

Or without a file:

```
claude mcp add open-crank-mcp -- <COMMAND> <ARGS>
```

**Cursor**: `.cursor/mcp.json` in your game project, or `~/.cursor/mcp.json`
globally. Identical to Claude Code's shape.

**OpenCode**: the `mcp` key in `opencode.jsonc`/`opencode.json`, or
`~/.config/opencode/opencode.jsonc` globally. Matches `McpLocalConfig` from
`@opencode-ai/sdk`, where `command` is one list rather than a command plus args:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "open-crank-mcp": {
      "type": "local",
      "command": ["<COMMAND>", <ARGS>]
    }
  }
}
```

Or `opencode mcp add open-crank-mcp` interactively.

Worth knowing for native mode specifically: a client launches its server with a
minimal `PATH` and an arbitrary working directory. That is why `build_game`
preflights `cmake` rather than assuming it, and why the harness sources are
embedded in the binary rather than read from this repo.


## Shared, watchable session

Every profile above and the "Connecting" command above it are two
independent things. `docker compose run` (what an MCP client uses) always
creates a brand new container per connection. So even if you separately
had `make up-vnc` running, you'd never be looking at the same Simulator
process an agent is driving through MCP tool calls. Two unrelated
containers, two unrelated displays.

`make up-shared` fixes that. One persistent container, shared by both. Start
it once, pointed at your game's directory:

```
GAME_DIR=/absolute/path/to/your-game make up-shared
```

`GAME_DIR` is required and must be absolute. The target refuses to start
without it, rather than silently mounting this repo as your game.

Unlike every other `up*` target, this one runs detached. It keeps running
in the background instead of occupying your terminal. Open
`http://localhost:6080/` for video and audio in one tab, with the same
Simulator defaults and the same `127.0.0.1` port binding described under
`up-vnc` above. The binding matters more here. This container is detached,
so anything published stays reachable in the background.

Then point your MCP client at the *same* container instead of spinning up
a new one. That means `docker compose exec` instead of `docker compose
run`, with no inline `Xvfb ... && DISPLAY=...` dance, since the persistent
container already has Xvfb running and `DISPLAY`/`PLAYDATE_SDK_PATH`
already set:

```json
{
  "mcpServers": {
    "open-crank-mcp": {
      "command": "docker",
      "args": [
        "compose", "-f", "/absolute/path/to/open-crank-mcp/docker-compose.yml",
        "exec", "-T", "simulator-shared", "open-crank-mcp"
      ]
    }
  }
}
```

(The same `args` shape works for OpenCode's `command` list and
`claude mcp add`/`opencode mcp add`, matching the "Connecting" section
above. Only `run --rm -v ... simulator bash -c "..."` changes to
`exec -T simulator-shared open-crank-mcp`.)

Once both are connected, an agent calling `launch_simulator` makes the
game appear in the browser tab you already have open. Click or type into
that window and your input drives the same live process alongside
whatever the agent's `press_button`/`set_crank` calls are doing. Real
input and harness overrides are two independent mechanisms feeding the
same running Simulator, so neither one blocks the other.

### Loading a game, and reloading on save

`up-shared` starts a container with a display in it. Something still has to build
a game and launch it, and two commands do that without an MCP client:

```
make shared-load     # build and launch, once
make shared-watch    # rebuild and reload on every save
```

`shared-load` drives the same MCP tools a client would, over the same stdio
transport, and stops any Simulator already running first so you can't end up
with two on one display.

`shared-watch` is the loop worth having. It watches your game's `Source`
directory, runs `pdc` on change, and presses the Simulator's own `Ctrl-R`,
which re-reads the `.pdx` from disk **in the same process**. Nothing restarts:
not the container, not the display, not your browser tab, and the volume you set
on the Playdate's slider survives, which it doesn't across a relaunch.

Two limits, both from the SDK rather than this setup. The game starts over on
every reload, because Reset is the only reload the Simulator has. And a failed
build leaves the previous `.pdx` in place, so the running game keeps working
rather than being replaced by nothing.

One surprise worth knowing if you write your own tooling against the MCP tools:
**a Simulator dies if the session that launched it exits too quickly.** It needs
its launching session to stay alive a few seconds; exit immediately after
`launch_simulator` returns and the game disappears about a second later. Also,
`get_status` can report the harness reachable straight away, because that check
reads a file in the data directory and a previous run's response may still be
sitting there, so it isn't a reliable readiness signal on its own. `shared-load`
holds on for five seconds for exactly this reason.

## Reading the logs

Applies to both modes: these are SDK and harness behaviours, not container ones.
Where the file lives differs, and that is called out below.

Three separate channels carry different output. They are not
interchangeable, and only two of them are readable by a human.

| Channel | Carries | Readable by |
|---|---|---|
| `get_logs` | The Simulator process's real stdout/stderr. GTK warnings, native startup diagnostics, and a C game's `printf` output. | The agent only. Buffered in memory by the server, never written to disk. |
| `get_game_logs` | Lua `print()` calls and uncaught-error tracebacks. | The agent, and you (see below). |
| The Simulator's own console | The Simulator GUI itself. Per `docs/GOTCHAS.md` this is the only place the SDK renders Lua console output, though no console pane is open by default. In container mode you reach it through the VNC view; natively it is just the window. | You only. |

**Native mode**: the file is already on your machine, at
`$PLAYDATE_SDK_PATH/Disk/Data/<bundle-id>/mcp/game_logs.jsonl`. Nothing is
mounted and nothing is root-owned. `launch_simulator` reports the exact directory
it resolved as `data_dir`, so you never have to guess.

**Container mode**: the `shared` profile bind-mounts the Simulator's sandboxed
Data directory to `.shared-data/` in this repo, so the same file is readable from
the host while the game runs:

```
tail -f .shared-data/<bundle-id>/mcp/game_logs.jsonl
```

At 256KB that file is renamed to `game_logs.1.jsonl` and a fresh one starts, so
a rotation never leaves you with no history. `get_game_logs` reads both, oldest
first; `tail -f` follows only the current one.

`launch_simulator` returns the `bundle_id` and the container-side
`data_dir`, so the agent can tell you the exact path. The file is written
by the Lua harness on every `print()` call, not batched, so a log from the
frame before a crash still lands. One JSON object per line, appended.

**A game set up before this file was renamed needs `setup` re-run.** The
harness is a *copy* in your game's own source tree, and an older copy writes
`game_logs.json` (a single JSON array) instead. `get_game_logs` fails with a
message saying exactly that rather than returning an empty list, and
`get_status` reports a `harness_warning` for any game whose harness copy
differs from the one this server ships. `setup` is safe to re-run and is the
whole fix.

Two asymmetries worth knowing, both covered in detail in
`docs/GOTCHAS.md`:

- Use `get_game_logs`, not `get_logs`, for your game's own output. The harness
  writes each entry to disk immediately, so it is complete the moment you ask,
  and you get one timestamped stream of just your game's lines. `get_logs` shows
  the Simulator's own stdout, which is block-buffered: the server launches it
  line-buffered so startup diagnostics arrive, but a Lua game's `print()` still
  does not reliably show up there. Measured; see `docs/GOTCHAS.md`.
  `get_game_logs` also captures tracebacks from your update function *and* from
  the button callbacks the harness invokes.
- A C game's `printf` does reach real stdout, so `get_logs` already covers
  it. The C harness doesn't write `game_logs.jsonl` at all, and doesn't
  need to.

## Rough edges

**Both modes**

- Nothing supervises the Simulator an agent launched. If a client disconnects
  without calling `stop_simulator`, the Simulator keeps running: reparented to
  the container's init, or orphaned on your desktop. The next connection's
  `launch_simulator` then starts a second one alongside it. Both run the same
  `.pdx`, so both resolve to the same bundle ID and the same Data directory, and
  both harnesses poll the same `mcp/command.json` - a tool response can come back
  from either instance. Worth clearing before you trust what you are seeing:

  ```
  pkill -9 -f bin/PlaydateSimulator                                   # native
  docker compose exec simulator-shared pkill -9 -f bin/PlaydateSimulator   # container
  ```

  `-9` is required. The Simulator ignores `SIGTERM`, which is why the server's own
  `stop_simulator` uses `SIGKILL` too. Natively, note this also kills a Simulator
  you started by hand.
- `set_crank` with no `duration_ms` returns success and has no visible effect: the
  override expires before the next frame, so a following `get_game_state` reads
  the real crank. Pass a duration you actually want it held for.

**Container mode only**

- The game directory is fixed when the container starts. Switching games means
  `make down` and starting over with a new `GAME_DIR`.
- The container runs as root, so `build_game` leaves root-owned `build/` and
  `.pdx` output inside your game directory, and `.shared-data/` is root-owned
  too. Both are readable without `sudo`. Deleting them needs `sudo`, or a
  `docker compose run --rm` doing the `rm` from inside a container. Native mode
  has neither problem: everything is written as you.
- Audio starts when you click the Playdate's volume slider, because nothing in
  the SDK can set that slider and nothing can read it. See "Volume" above. It is
  the first thing to suspect if a future SDK version rearranges the device frame.
  Native mode has real audio and needs none of this.

**Native mode only**

- Running headless needs `SDL_AUDIODRIVER=dummy`. The Simulator treats SDL
  initialisation as fatal and will not start without a working audio driver, so
  on a machine with no audio device - a CI runner, a server over SSH - it exits
  before doing anything, reporting `dsp: No such audio device`. Nothing about
  that message suggests audio is optional, which it is. A desktop with real audio
  needs none of this, and the container image already sets it.
- Building the same game in both modes will hit a stale `CMakeCache.txt`, because
  cmake records absolute paths and the container sees your game at `/workspace`
  while a native run sees its real path. `build_game` detects that specific
  failure, clears the cache, says so in its output, and reconfigures. Worth
  knowing so the message is not alarming.
- Detection is silent when it succeeds. If a tool reports no SDK, or the wrong
  one, `make sdk-path` prints what was found and every candidate considered.

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
3. `mcp.run` captures both `print()` output and unhandled-error tracebacks
   into `mcp/game_logs.jsonl` automatically; read them back with the
   `get_game_logs` tool. Tracebacks are the reason it matters - `mcp.run`
   catches them with `xpcall`, so they never reach the Simulator's console
   at all. `print()` output *does* also reach the Simulator's real stdout
   and so shows up in `get_logs` too, contrary to what this README and
   [`docs/GOTCHAS.md`](docs/GOTCHAS.md) used to claim; `get_game_logs` is
   still the better read, since it gives one timestamped stream of just
   your game's output rather than your lines mixed into the Simulator's.
4. `press_button` synthesizes real button-down/up edges, so
   `buttonJustPressed`/`buttonJustReleased` and the SDK's
   `AButtonDown`/`leftButtonDown`/etc callbacks all fire correctly from
   an MCP-driven press, not just `buttonIsPressed`'s "currently held"
   bit. `AButtonHeld`/`BButtonHeld` (fired after a continuous 1-second
   hold) are a separate mechanism and aren't synthesized.
5. `set_crank` overrides the angle and delta, and only touches the docked
   state if you ask it to with `crank_dock` (`"docked"` or `"undocked"`).
   Omit it and `playdate.isCrankDocked()` keeps reading whatever the game
   would really see - which is *docked*, in the Simulator at rest. Before
   this was a three-valued field it was a bool, so every `set_crank` call
   silently forced the crank to read undocked.

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
as a post-build step) or plain `pdc` for Lua-only projects. The container image
already has `cmake` and `build-essential`; natively they are yours to install,
and only a C game needs them. Either way, no ARM toolchain: this project only
targets the Simulator, never real hardware, so a C game builds as a shared
library with the host compiler.

**Hybrid C+Lua games** (C for hot loops, Lua for UI, an officially
supported pattern) can use the Lua harness alone, since a real Lua VM is
still running.

## Local development

Every command is a make target. Most run in the container, so Docker and Go cover
almost everything. The `-native` targets and `sdk-path` are the exceptions: they
run against an SDK on this machine and need one installed.

### Containers

| Command | Does |
|---|---|
| `make build` | Builds the image. Fetches the Playdate SDK, pinned by `PLAYDATE_SDK_VERSION`. |
| `make up` | Headless container under Xvfb. What the MCP tools need and nothing more. |
| `make up-visual` | Linux, native X11 window and host audio. |
| `make up-visual-wsl` | Windows 11 through WSL2, using WSLg's display and audio. |
| `make up-vnc` | Browser fallback, any OS. Occupies the terminal. |
| `make up-shared` | Persistent shared session, detached, for an agent and a human at once. Needs `GAME_DIR`. |
| `make down` | Stops every profile, including the shared one. |
| `make shell` | A bash prompt in the image. |

Note that only `up-vnc` and `up-shared` publish ports, and only on `127.0.0.1`
unless you set `BIND_ADDR`.

### Running a game in the shared container

`up-shared` gives you a container with a display in it. These put a game inside:

| Command | Does |
|---|---|
| `make shared-load` | Builds and launches the mounted game, once. Stops any Simulator already running first. |
| `make shared-watch` | Watches the game's `Source`, rebuilds on save, and reloads in place with the Simulator's own `Ctrl-R`. |

```
GAME_DIR=/absolute/path/to/your-game make up-shared
make shared-load
```

### Tests and checks

`make test` runs all of it, in the order below, which is fastest-to-fail first:
the three host-only suites go before anything that boots a container. It skips
`shared-check` and `test-shared-types` only because `test-shared-browser`
already runs both.

| Command | Needs | Covers |
|---|---|---|
| `make go-build` / `go-test` | Go | The server, tools and harness IPC. `go-build` also emits `./open-crank-mcp`, which is what a native client runs. |
| `make go-build-cross` | Go | Builds and vets for linux, darwin and windows, so a platform-specific construct outside a build-tag file fails here rather than on someone else's machine. |
| `make test-shared-unit` | awk, bash | The volume-slider parser and the window geometry formula, against synthetic pixel columns. No container. |
| `make mutation-test` | Go | Mutates the code and checks the tests notice, so a line that runs without being asserted on doesn't pass for covered. Thresholds in `.gremlins.yaml`. About 15s. |
| `make test-c-harness` | Docker | The C harness, compiled and exercised against the SDK. |
| `make smoke-check` | Docker | The SDK is where the image expects it and the Simulator starts. |
| `make sdk-contract-check` | Docker | The MCP tools driving a real Simulator, so an SDK release that changes behaviour shows up here. |
| `make shared-check` | Docker | The VNC workspace: pages served, window manager config, where the slider was found, and that clicking it works. |
| `make test-shared-types` | Docker | Typechecks the browser tests with `tsgo`, the Go port of `tsc`. |
| `make test-shared-browser` | Docker | Browser behaviour of the VNC pages, via Playwright in its own container. Runs the typecheck and `shared-check` first. |
| `make sdk-path` | A host SDK | Prints the resolved SDK, which source found it, and every candidate considered. The first thing to run when detection surprises you. |
| `make smoke-check-native` | A host SDK | Same subject as `smoke-check`, no container: libraries resolve, `pdc` runs, the Simulator starts. |
| `make sdk-contract-check-native` | A host SDK | Same subject as `sdk-contract-check`, no container. Sets `OPEN_CRANK_SDK_CONTRACT`, which is what those tests skip without. |

### Environment variables

Native mode first, since none of it is required and that is easy to miss:

| Variable | Default | Applies to |
|---|---|---|
| `PLAYDATE_SDK_PATH` | detected | Both modes. The container sets it; natively it is optional, since detection also reads `SDKRoot` from `~/.Playdate/config` and then the default install location. Set it for an SDK somewhere unusual. |
| `OPEN_CRANK_SIMULATOR_BIN` | detected | Native. Overrides the Simulator executable outright. The escape hatch if the macOS `.app` name is ever wrong. |
| `OPEN_CRANK_DATA_ROOT` | detected | Native. Overrides the *parent* of the per-game data directory, for a Simulator that sandboxes somewhere none of the candidates name. |
| `OPEN_CRANK_SDK_CONTRACT` | unset | Opts the contract tests in. They skip without it, which is what keeps them from failing on a host that has an SDK but no display. |

Container mode:

| Variable | Default | Applies to |
|---|---|---|
| `PLAYDATE_SDK_VERSION` | `3.1.1` | `build`, and every target that builds |
| `GAME_DIR` | required | `up-shared`. Must be absolute. |
| `SIM_ZOOM` | `1` | `up-vnc`, `up-shared`. Simulator zoom level. |
| `VNC_GEOMETRY` | `1280x800` | `up-vnc`, `up-shared`. Workspace size. |
| `BIND_ADDR` | `127.0.0.1` | Published ports 6080 and 8000 |
| `SOURCE_DIR` / `PDX_PATH` | `/your-game/Source`, `/your-game/your-game.pdx` | `shared-watch`, if your project isn't laid out that way |
| `PLAYWRIGHT_IMAGE` | pinned to `@playwright/test` | `test-shared-types`, `test-shared-browser` |
| `SHARED_URL` | `http://localhost:6080` | The browser tests, when the container isn't on localhost |

## Tests

What each command is above. This section is why they exist.

The container tests run in their own compose project, on ports 6180 and 8100,
with their own data directory. That isolation matters more than it sounds: the
suite needs a known game mounted, so without it a test run unmounts the game you
were playing and then fights your browser tab for the single-listener audio
stream. Isolated, you can run the whole suite mid-game and nothing moves.

The shared-session tests exist because that layer is easy to break invisibly. Each of
these asserts something that has actually broken in practice:

- Every page returns 200. A partial edit once deleted two of them, and a
  missing page is invisible until someone opens that exact URL.
- The patched openbox config keeps every mouse binding the shipped one has.
  openbox doesn't merge a partial config with its defaults, so a hand-written
  `rc.xml` silently dropped all 59 of them, taking window dragging with it.
- No keybinding can hide or close the Simulator, since only an MCP client can
  start one.
- The volume slider is found by relationships, not pixel values: inside the
  window, running downwards a plausible distance, mute icon below it. The exact
  numbers move with zoom and theme.
- Clicking the slider changes the framebuffer, which covers the whole input
  path: X, GTK's click-to-warp, and the coordinates being right.
- Audio playback starts only from the slider, never from a click elsewhere.

The browser tests are TypeScript, run by Playwright's own runner, which
transpiles them directly. `tsgo` typechecks them separately and emits nothing,
so there's no build step in front of the tests.

Node is the runtime, inside the Playwright container. Playwright's test runner
requires it: Bun and Deno can drive `playwright-core`, but not this runner.
`PLAYWRIGHT_IMAGE` in the `Makefile` has to stay on the same version as
`@playwright/test` in `tests/browser/package.json`, since the image carries the
browsers and the package drives them.

Two things are deliberately not tested. Game audio end to end needs a
sound-producing fixture and level thresholds, which is a flake factory, so the
audio chain is checked with a synthetic tone into the sink instead. And nothing
compares screenshots: the game animates, so image diffing would fail for
reasons unrelated to the code.

## If you used the byos profile

The `shared` profile was called `byos`, for "bring your own simulator", until
Checkpoint 6. It runs its own Simulator in a container and shares it, which is
not bringing your own anything, so the name described something it never did.
`byos` is now retired entirely rather than reused, so there is no version of
this repo where the word means two things.

Nothing carries over automatically, and nothing warns you either: the old `make`
targets briefly existed as tombstones pointing at this section, and were removed
at Checkpoint 11 as planned, so they now fail the way any unknown target does.

| Old | New | What you see if you don't change it |
|---|---|---|
| `make up-byos` | `make up-shared` | `No rule to make target 'up-byos'` |
| `make byos-load` / `byos-watch` | `make shared-load` / `shared-watch` | Same |
| `make byos-check` | `make shared-check` | Same |
| `make test-byos-unit` / `-types` / `-browser` | `make test-shared-*` | Same |
| `simulator-byos` in your MCP client config | `simulator-shared` | `no such service: simulator-byos` |
| `--profile byos` | `--profile shared` | Compose starts nothing and says nothing |
| `BYOS_URL` | `SHARED_URL` | The browser tests fall back to `localhost:6080` |

The bind-mounted Data directory moved from `.byos-data/` to `.shared-data/`. It
holds each game's save data and the `mcp/game_logs.jsonl` behind
`get_game_logs`, so it's worth moving rather than deleting if you have anything
in there you care about. Everything in it is root-owned, since the container
runs as root, which is why this goes through a container instead of `sudo`:

```
docker run --rm -v "$PWD/.byos-data:/old" -v "$PWD/.shared-data:/new" \
  open-crank-mcp-simulator:latest \
  sh -c 'for d in /old/*; do [ -e "/new/$(basename "$d")" ] || mv "$d" /new/; done'
docker run --rm -v "$PWD:/repo" open-crank-mcp-simulator:latest rm -rf /repo/.byos-data
```

The guard matters if you have run anything since the rename: a bundle that
already exists under the new name is the live one, and the copy under the old
name is stale. Skipping those keeps the newer data.

If you don't want any of it, `docker run --rm -v "$PWD:/repo"
open-crank-mcp-simulator:latest rm -rf /repo/.byos-data` on its own is enough.
Either way `.byos-data/` stays in `.gitignore`, so leaving it in place costs
nothing but disk.

## License

This project's own code is MIT (see `LICENSE`). The Playdate SDK is
licensed separately by Panic, Inc., see the
[Playdate SDK License](https://play.date/dev/sdk-license/). Not affiliated
with or endorsed by Panic.
