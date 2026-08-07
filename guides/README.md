# Guides

How to use open-crank-mcp, by task.

Start here if you have a goal. Why things work the way they do is in
[`../docs/`](../docs/), starting with [GOTCHAS.md](../docs/GOTCHAS.md).

## Start here

- [getting-started.md](getting-started.md). Install to first playtest, using a
  game that ships with this repo. Every step has been run.

## Running the server

- [container-mode.md](container-mode.md). Docker, its own SDK, headless. The
  default, and the only mode CI exercises end to end.
- [native-mode.md](native-mode.md). Your own SDK, a real window, no container.
- [connecting.md](connecting.md). Pointing Claude Code, OpenCode or Cursor at
  either one.
- [shared-session.md](shared-session.md). One long-lived container an agent
  drives and you watch at the same time.

## Wiring up a game

- [setting-up-a-game.md](setting-up-a-game.md). The `setup` tool.
- [harness-wiring.md](harness-wiring.md). What `setup` does under the hood, for
  when it leaves you a `manual_steps` entry.
- [exposing-game-state.md](exposing-game-state.md). `get_game_state` returns
  nothing until you register a state function. This is the highest-value thing
  you can add for an agent.

## Using it

- [playtesting-with-an-agent.md](playtesting-with-an-agent.md). The loop, the
  input model, and what to put in a prompt.
- [reading-the-logs.md](reading-the-logs.md). Three channels, and which one
  carries your game's output.
- [troubleshooting.md](troubleshooting.md). Symptoms first.
