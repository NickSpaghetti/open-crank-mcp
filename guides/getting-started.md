# Getting started

Install to first playtest, using a game that ships with this repo.

This uses native mode, which needs a Playdate SDK you installed yourself. It is
the shorter path and the Simulator is a real window you can watch. See
[container-mode.md](container-mode.md) if you would rather run in Docker.

Every step below has been run start to finish on Linux.

## 1. Build the server

```
make go-build
```

That produces `./open-crank-mcp` in the repo root. That path is what your MCP
client runs.

## 2. Check the SDK is found

```
make sdk-path
```

It prints the SDK it resolved and which of the three sources found it. If it
reports nothing, install the SDK or set `PLAYDATE_SDK_PATH`. Detection is silent
when it succeeds, so this is worth running once up front.

## 3. Make a copy of the example game

The repo ships a small Lua fixture. Copy it somewhere writable and remove its
vendored harness, so `setup` has real work to do.

```
cp -r lua/test-fixture /tmp/first-playtest
rm /tmp/first-playtest/Source/mcp_harness.lua
rm -rf /tmp/first-playtest/test-fixture.pdx
```

## 4. Connect a client

Point your client at the binary from step 1. For Claude Code, a `.mcp.json`:

```json
{
  "mcpServers": {
    "open-crank-mcp": {
      "command": "/absolute/path/to/open-crank-mcp/open-crank-mcp"
    }
  }
}
```

Native mode takes no arguments. See [connecting.md](connecting.md) for Cursor
and OpenCode, which differ only in file shape.

Restart the client so it picks the server up.

## 5. Wire the harness in

```
setup   source_dir=/tmp/first-playtest
```

It detects Lua, copies `mcp_harness.lua` into `Source/`, and adds
`import "mcp_harness"` to `main.lua`. The response lists what it copied and
patched.

## 6. Build and launch

```
build_game        source_dir=/tmp/first-playtest
launch_simulator  pdx_path=/tmp/first-playtest/first-playtest.pdx
```

`build_game` returns the `.pdx` path it produced. Pass that to
`launch_simulator`. A Simulator window opens.

Check the harness is answering before going further:

```
get_status
```

Want `running: true` and `harness_reachable: true`. If the harness is not
reachable, see [troubleshooting.md](troubleshooting.md).

## 7. Look at it

```
get_screenshot
```

Returns the current frame as a PNG, 400x240.

## 8. Press a button and watch the state change

```
get_game_state
press_button     button=a
get_game_state
```

The fixture registers a state function that reports button state and two
counters, `a_down_count` and `a_up_count`. The counters go up after the press.
That is the check that an input actually reached the game rather than just
being accepted by the server.

`press_button` with no `duration_ms` is a tap. It presses and releases. See
[playtesting-with-an-agent.md](playtesting-with-an-agent.md) for why the crank
behaves differently.

## 9. Stop

```
stop_simulator
```

## What to do next

- Your own game gets the same treatment. `setup`, `build_game`,
  `launch_simulator`.
- `get_game_state` returns nothing for a game that has not registered a state
  function. That is the single most useful thing to add, and
  [exposing-game-state.md](exposing-game-state.md) covers it.
- [playtesting-with-an-agent.md](playtesting-with-an-agent.md) covers the loop
  an agent runs and what to put in a prompt.
