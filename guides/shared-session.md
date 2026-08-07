# Shared, watchable session

One long-lived container that an agent drives and you watch at the same time.

This is the `shared` compose profile. It is optional. Container mode and native
mode both work without it. See the readme's
[container-mode.md](container-mode.md) for the modes
themselves.

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

## Audio: why the stream restarts per listener

The MP3 stream is served by `socat`, which starts a fresh encoder per listener.
That is not an implementation detail.

A single long-lived `ffmpeg -listen 1` opens its pulse input before it opens its
output. It captures into a queue while waiting for someone to connect, then
hands over the backlog when they do. Measured with a beep played at a known
moment, that was **22 seconds** of lag after a few idle minutes, held for the
whole session. The symptom is late audio that keeps playing after you pause the
game.

Starting the encoder at connect time keeps it under a second. As a side effect,
several listeners can share the stream.

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

Geometry is re-read on every scan, so dragging the Simulator window or changing
its zoom cannot leave it reading the wrong pixels. A scan that cannot find the
slider publishes `-1`, and the page leaves the audio exactly as it is. Acting on
a failed read would silence working audio, which looks identical to a broken
pipeline.

The stack is self-contained. The container runs its own PulseAudio daemon with a
null sink, `x11vnc` bridges the Xvfb display, and `ffmpeg` re-streams the null
sink's monitor as MP3.
