# open-crank-mcp

An MCP server for the Playdate Simulator. Lets an AI agent see the screen,
press buttons, turn the crank, and read game state and logs, so it can
playtest and debug a game instead of only reading source code.

Works with Lua, C, or a mix of both.

## Guides

Start with [getting started](guides/getting-started.md). It runs install to
first playtest against a game that ships with this repo, and every step in it
has been executed.

**Running the server**

| Guide | What it covers |
|---|---|
| [container mode](guides/container-mode.md) | Docker, carrying its own SDK, headless. The default, and the only mode CI exercises end to end. |
| [native mode](guides/native-mode.md) | Your own SDK, a real Simulator window, no container and no bind mounts. |
| [connecting a client](guides/connecting.md) | Pointing Claude Code, OpenCode or Cursor at either mode. |
| [shared session](guides/shared-session.md) | One long-lived container an agent drives and you watch at the same time, over VNC, with audio. Optional. |

**Wiring up a game**

| Guide | What it covers |
|---|---|
| [setting up a game](guides/setting-up-a-game.md) | The `setup` tool. Detects Lua, C or hybrid, copies the harness in, patches the glue. |
| [wiring it by hand](guides/harness-wiring.md) | What `setup` does under the hood, for when it leaves you a `manual_steps` entry. |
| [exposing game state](guides/exposing-game-state.md) | `get_game_state` returns nothing until your game registers a state function. The highest-value thing you can add for an agent. |

**Using it**

| Guide | What it covers |
|---|---|
| [playtesting with an agent](guides/playtesting-with-an-agent.md) | The loop, the input model, and what to put in a prompt. |
| [reading the logs](guides/reading-the-logs.md) | Three channels carry output and only two are readable. Which one you want depends on whether your game is Lua or C. |
| [troubleshooting](guides/troubleshooting.md) | Symptoms first. Start here when a tool times out or does nothing. |

Why things work the way they do is in [`docs/`](docs/), starting with
[GOTCHAS.md](docs/GOTCHAS.md). The plan and checkpoints are in
[`docs/ROADMAP.md`](docs/ROADMAP.md).

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

Checkpoints 1-11 done. Both modes work: the harnesses, the Go server, all the MCP
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

Windows-native is not supported: WSL2 covers Windows through container mode, and
nobody here can run or debug a native Windows path. Its layout values in
`internal/sdk` are correct and covered by tests, so the code compiles and the
logic is exercised on every platform. It is simply not verified by running.

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

## Tools

Sixteen, in roughly the order you use them.

| Tool | What it does |
|---|---|
| `setup` | Copies the harness into a game and patches `main.lua`/`CMakeLists.txt`/your `eventHandler` to call it. Detects Lua, C, or hybrid. Safe to re-run. Check `manual_steps` in the response for anything it could not automate. |
| `teardown` | Reverses `setup`. Removes the copied harness and strips what it patched. |
| `build_game` | Detects the project type and builds a `.pdx`. Returns compile errors and warnings either way. |
| `launch_simulator` | Launches the Simulator with a `.pdx`. |
| `stop_simulator` | Stops the running Simulator. |
| `restart_simulator` | Stops and relaunches with the same `.pdx`. |
| `get_status` | Whether the Simulator is running, its bundle ID, and whether the harness answers. |
| `get_screenshot` | The current screen as a PNG. |
| `press_button` | Presses `a`, `b`, `up`, `down`, `left` or `right`. Omit `duration_ms` for a tap. |
| `set_crank` | Overrides the crank angle and delta. Omit `duration_ms` and it holds. `crank_dock` takes `docked` or `undocked`. |
| `get_game_state` | Your game's registered state inspector, as JSON. The shape is entirely game-defined. |
| `list_entities` | Sprites in the display list: position, bounds, tag, z-index, visibility. Complete for Lua. For C only sprites with a collision rect are included, so check the `complete` field. |
| `get_logs` | The Simulator process's own stdout and stderr. |
| `get_game_logs` | A Lua game's `print()` output and tracebacks, written to disk per entry. Not applicable to C games. |
| `read_save_data` | Reads a JSON save file from the game's data directory. Omit `filename` to list what is there. |
| `write_save_data` | Writes a JSON value to a save file in the game's data directory. |

The two log tools are not interchangeable. See
[guides/reading-the-logs.md](guides/reading-the-logs.md).

`press_button` and `set_crank` treat an omitted `duration_ms` differently. A
button is an event and taps. The crank is a position and stays where you put it.


## Rough edges

**Both modes**

- Nothing supervises the Simulator an agent launched. If a client disconnects
  without calling `stop_simulator`, the Simulator keeps running: reparented to
  the container's init, or orphaned on your desktop. The next connection's
  `launch_simulator` then starts a second one alongside it. Both run the same
  `.pdx`, so both resolve to the same bundle ID and the same Data directory, and
  both harnesses poll the same `mcp/command.json`. A tool response can come back
  from either instance. Worth clearing before you trust what you are seeing:

  ```
  pkill -9 -f 'bin/PlaydateSimulato[r]'                                   # native
  docker compose exec simulator-shared pkill -9 -f 'bin/PlaydateSimulato[r]'   # container
  ```

  `-9` is required. The Simulator ignores `SIGTERM`, which is why the server's own
  `stop_simulator` uses `SIGKILL` too. Natively, note this also kills a Simulator
  you started by hand.

  The bracket is not a typo. `pkill -f` matches every process's whole command
  line, including the shell running the `pkill`. Without the bracket it kills
  your own shell, silently, and you conclude the Simulator was never running.
- A button cannot be held open-endedly. `press_button` is a tap: omit
  `duration_ms` and it holds for a default that is long enough for the game to
  see a real press, then releases. Ask for a long one and it still releases when
  that elapses, because nothing exposes a release, so a button held with no
  expiry could never be let go.

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
  on a machine with no audio device, such as a CI runner or a server over SSH, it exits
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

## Local development

Every command is a make target, and Docker and Go cover almost everything. The
split is not container versus host, though. `go-build`, `go-build-cross`,
`go-test`, `no-regex`, `test-shared-unit` and `mutation-test` all run on this
machine and need no container at all. The `-native` targets and `sdk-path` go
further and need a Playdate SDK installed here. Each row's "Needs" column below
says which is which.

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

`make test` runs the four host-only checks first, so a broken parser or a Go typo
fails in seconds instead of after several container boots. The order is
`no-regex`, `go-test`, `test-shared-unit`, `mutation-test`, then everything that
needs Docker. It skips `shared-check` and `test-shared-types` only because
`test-shared-browser` already runs both. `go-build-cross`, `sdk-path`, the
`-native` targets and `mutation-test-diff` are not part of it.

| Command | Needs | Covers |
|---|---|---|
| `make go-build` / `go-test` | Go | The server, tools and harness IPC. `go-build` also emits `./open-crank-mcp`, which is what a native client runs. |
| `make go-build-cross` | Go | Builds and vets for linux, darwin and windows, so a platform-specific construct outside a build-tag file fails here rather than on someone else's machine. |
| `make no-regex` | git, grep | Fails if any Go file imports `regexp`. There is no allowlist. Patterns are replaced by `internal/scan`, which reads source a byte at a time and knows a comment from code; see the note above the target for why. Grep on the command line is fine. |
| `make test-shared-unit` | awk, bash | The volume-slider parser and the window geometry formula, against synthetic pixel columns. No container. |
| `make mutation-test` | Go | Mutates the code and checks the tests notice, so a line that runs without being asserted on doesn't pass for covered. Thresholds in `.gremlins.yaml`. |
| `make mutation-test-scan` / `mutation-test-rest` | Go | The same run split in two, which is what CI uses. `internal/scan` is byte-loop code where a flipped comparison hangs the loop instead of failing it, and gremlins sizes its per-mutant timeout from the scope's own baseline. Split out, a hang there costs 3s instead of 60s. `make mutation-test` locally still does the whole module. |
| `make mutation-test-diff` | Go, git | Mutates only the lines that changed against `MUTATION_DIFF_REF`. Seconds instead of minutes, which is what the pre-commit hook runs. Not a substitute for the full run: a change can weaken a test for code it does not touch. |
| `make hooks` | git | Points `core.hooksPath` at `.githooks`, enabling the pre-commit hook. Bypass one commit with `--no-verify`. |
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
| `BIND_ADDR` | `127.0.0.1` | Host address for the published ports |
| `VNC_PORT` | `6080` | `up-vnc`, `up-shared`. Every URL in this README assumes the default. |
| `AUDIO_PORT` | `8000` | `up-vnc`, `up-shared`. The MP3 stream. |
| `SHARED_DATA_DIR` | `./.shared-data` | `up-shared`. Where the Simulator's sandboxed Data directory is mounted, so `game_logs.jsonl` is readable from the host. |
| `SOURCE_DIR` / `PDX_PATH` | `/your-game/Source`, `/your-game/your-game.pdx` | `shared-watch`, if your project isn't laid out that way |
| `PLAYWRIGHT_IMAGE` | pinned to `@playwright/test` | `test-shared-types`, `test-shared-browser` |
| `SHARED_URL` | `http://localhost:6080` | The browser tests, when the container isn't on localhost |

## Tests

`make test` runs everything. The table above says what each command covers.
[`docs/TESTING.md`](docs/TESTING.md) says why each one exists.


## License

This project's own code is MIT (see `LICENSE`). The Playdate SDK is
licensed separately by Panic, Inc., see the
[Playdate SDK License](https://play.date/dev/sdk-license/). Not affiliated
with or endorsed by Panic.
