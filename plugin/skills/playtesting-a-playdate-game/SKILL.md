---
name: playtesting-a-playdate-game
description: How to drive a Playdate game through the open-crank-mcp tools - the look/read/act/check loop, which input verb matches which intention, and how to tell that an input actually reached the game rather than merely reaching the harness. Use when playtesting, debugging or exploring a Playdate game in the Simulator.
---

# Playtesting a Playdate game

Tools are named bare here (`press_button`, `get_screenshot`). Your client may
namespace them; use whatever form it shows you.

## The loop

1. **Look** — `get_screenshot` for the frame, `list_entities` for where sprites are.
2. **Read** — `get_game_state` for what the game thinks is happening.
3. **Act** — `press_button`, `hold_button`/`release_button`, or `set_crank`.
4. **Check** — `get_game_state` again, and `get_game_logs` if something looked wrong.

Step 2 decides whether any of this works. Without a registered state function
`get_game_state` returns nothing and you are reading pixels and guessing. If that
is the situation, say so rather than working around it — the fix is a few lines
in the game, and it is the single highest-value thing its author can add.

## Pick the verb that matches the intention

| Tool | What it does | Use it for |
|---|---|---|
| `press_button` | taps and releases | menus, confirming, firing once |
| `hold_button` | stays down until released | walking, steering, charging |
| `release_button` | lets go | ending a hold |
| `set_crank` | holds an angle until replaced | a crank is a position, not an event |
| `reset_input` | drops every override | handing the game back |

`press_button` with a `duration_ms` holds and releases on its own, which covers
most short holds. Reach for `hold_button` when the hold has to outlast the call —
steering while you take a screenshot, read state, and steer again. It stays down
until `release_button` or `reset_input`, so pair it with one.

`set_crank` with no duration stays put until the next `set_crank`. `reset_input`
is the only way to let go of a crank set that way.

## Two things that will mislead you

**A successful tool result means the harness received the command. It does not
mean the game reacted.** The reliable check is a counter in the game's state
function, incremented in the button callback, read before and after. Without one,
a C game that never rewired its input calls looks exactly like a game that chose
to ignore the press.

**The Simulator reports the crank docked at rest.** A game that only reads the
crank while undocked will ignore an angle you set. Pass `crank_dock: "undocked"`
when that matters.

## The two log tools are not interchangeable

- `get_logs` is the Simulator process's own stdout and stderr: GTK warnings,
  startup messages, why it died.
- `get_game_logs` is a Lua game's `print()` output and tracebacks, written to
  disk per entry. Not applicable to C games.

Reach for `get_game_logs` when the game misbehaved, `get_logs` when the Simulator
did.

## What this is bad at

Say so rather than producing a confident answer:

- **Anything on a timer.** You act between frames, not on them, and cannot
  reliably hit a one-frame window.
- **Feel.** Whether a jump is floaty or a crank is too sensitive needs a human.
- **Visual regressions.** Screenshots are readable but nothing compares them
  across runs, and the game animates.

## Further reading

- [Playtesting with an agent](https://github.com/NickSpaghetti/open-crank-mcp/blob/main/guides/playtesting-with-an-agent.md)
- [Exposing game state](https://github.com/NickSpaghetti/open-crank-mcp/blob/main/guides/exposing-game-state.md)
- [Reading the logs](https://github.com/NickSpaghetti/open-crank-mcp/blob/main/guides/reading-the-logs.md)
- [Troubleshooting](https://github.com/NickSpaghetti/open-crank-mcp/blob/main/guides/troubleshooting.md)
