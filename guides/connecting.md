# Connecting a client

How to point Claude Code, OpenCode, or Cursor at the server.

Do this after you can build and run in one of the two modes. See
[getting-started.md](getting-started.md) if you have not got that far.

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
[shared-session.md](shared-session.md) instead of creating one.

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
