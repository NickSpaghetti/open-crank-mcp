---
name: ocm-run-playtest
description: Play a Playdate game in the Simulator toward a goal, checking after each input that the game actually changed, and report what happened. Run it when the user asks for a playtest, not to answer a question about the game - it drives the game for as long as the goal takes.
---

# Run a play test

The goal and the stopping condition are whatever the user asked for. If they gave
no stopping condition, pick one and say what you picked — turn count, a level reached, a
state value — because without one this runs until it runs out of budget.

First `get_status`. If the Simulator is not running or the harness is not
reachable, stop and say so; the setup skill is what fixes that.

Then loop: `get_screenshot` and `get_game_state` to see where things are, one
input, then `get_game_state` again to confirm it changed the way you expected.

Three rules that make the report worth reading:

- **Verify, do not assume.** A successful tool result means the harness got the
  command, not that the game reacted. If the state did not change, say so instead
  of pressing harder.
- **Diagnose before retrying.** On anything unexpected, `get_game_logs` first —
  it carries tracebacks from the update function and from the button callbacks
  the harness invokes. Retrying a failing input three times produces no
  information.
- **Stop at the condition.** Then report: what you did, what the game did, and
  anything that looked wrong, with the state values that show it.

If you find a bug, give the shortest input sequence that reproduces it rather
than the whole session.

Call `reset_input` when you finish, so nothing is left held.
