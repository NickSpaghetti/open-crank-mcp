# Shared, watchable session

One long-lived container that an agent drives and you watch at the same time.

This is the `shared` compose profile. It is optional. Container mode and native
mode both work without it. See [container-mode.md](container-mode.md) for the
modes themselves.

The usual container setup runs `docker compose run`, which creates a new
container per connection. Even with `make up-vnc` running, you would be watching
a different Simulator from the one the agent drives.

`make up-shared` runs one persistent container for both. Start it once, pointed
at your game's directory:

```
GAME_DIR=/absolute/path/to/your-game make up-shared
```

`GAME_DIR` is required and must be absolute. The target refuses to start
without it, rather than silently mounting this repo as your game.

Unlike every other `up*` target, this one runs detached. Open
`http://localhost:6080/` for video and audio in one tab, with the same
[Simulator defaults](container-mode.md#simulator-defaults) and the same
`127.0.0.1` port binding as
[`make up-vnc`](container-mode.md#any-os-universal-fallback-vnc--audio-stream).
The binding matters more here. This container is detached, so anything published
stays reachable in the background.

Then point your MCP client at the same container with `docker compose exec`.
It already has Xvfb running and `DISPLAY` and `PLAYDATE_SDK_PATH` set:

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

The same `args` work for OpenCode's `command` list and for
`claude mcp add`. See [connecting.md](connecting.md).

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

If you write your own tooling against the MCP tools: **a Simulator dies if the
session that launched it exits too quickly.** Exit right after
`launch_simulator` returns and the game disappears about a second later. Keep
the session alive a few seconds. `get_status` is not a reliable readiness signal
on its own either, because a previous run's response file can make the harness
look reachable straight away. `shared-load` waits at least five seconds for both
reasons.

## Audio: why the stream restarts per listener

The MP3 stream is served by `socat`, which starts a fresh encoder per listener.

A single long-lived `ffmpeg -listen 1` opens its pulse input before its output.
It queues audio while nobody is connected, then hands over the backlog, so a few
idle minutes become tens of seconds of lag for the whole session. Starting the
encoder at connect time keeps it under a second, and several listeners can share
the stream.

Pointing a browser straight at the raw stream gives you a media viewer that
reports it as 0:00 long and often will not start. That is why the served pages
wrap it in an `<audio>` element instead.

## Volume: why the slider is read off the framebuffer

The Playdate's own volume slider is the volume control, and the browser follows
it. That works by reading the slider off the framebuffer, because the slider is
the only place the Simulator's volume exists. The SDK exposes `getVolume()` on
its system API with no setter, every `setVolume()` in the sound API is
per-source, and the Simulator's INI has no key for it.

So a one-pixel-wide column down the device frame gets scanned once a second. It
crosses the LOCK button, the MENU button, the volume track, its knob and the
mute icon, and each reads as either dark or light against the yellow frame. The
knob's position along the track is the volume, published to `pd-volume.json` for
the page to poll.

Geometry is re-read on every scan, so moving the Simulator window or changing
its zoom does not break it. A scan that cannot find the slider publishes `-1`,
and the page leaves the audio as it is rather than silencing working audio.

The stack is self-contained. The container runs its own PulseAudio daemon with a
null sink, `x11vnc` bridges the Xvfb display, and `ffmpeg` re-streams the null
sink's monitor as MP3.
