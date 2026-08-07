# Playtesting with an agent

How to get useful work out of an agent driving your game.

The tools are only half of it. The other half is giving the agent something to
read and telling it what you want tested.

## The loop

An agent playtests by repeating four steps.

1. **Look.** `get_screenshot` for the frame, `list_entities` for where sprites
   are.
2. **Read.** `get_game_state` for what the game thinks is happening.
3. **Act.** `press_button` or `set_crank`.
4. **Check.** `get_game_state` again, and `get_game_logs` if something went
   wrong.

Step 2 is the one that decides whether any of this works. Without a registered
state function the agent is reading pixels and guessing. See
[exposing-game-state.md](exposing-game-state.md).

## Input: a button taps, the crank holds

The two input tools treat an omitted `duration_ms` differently, and it catches
people out.

| Tool | Omit `duration_ms` | Why |
|---|---|---|
| `press_button` | taps and releases | Nothing exposes a release, so a button held with no expiry could never be let go |
| `set_crank` | holds until replaced | A crank is a position. A real one stays where you left it |

So `press_button` with no duration is a tap, which is what you want almost
always. Pass a duration only when you need a hold, and know it still releases
when that elapses.

`set_crank` with no duration stays put until the next `set_crank`. Pass a
duration when you want it to lapse back to what the game would really read.

One more thing about the crank. `crank_dock` defaults to leaving the dock alone,
and the Simulator reports the crank **docked** at rest. A game that only reads
the crank while undocked will ignore an angle you set. Pass
`crank_dock: "undocked"` in that case.

## Verifying an input landed

Reporting success means the command reached the harness. It does not mean the
game reacted.

The reliable check is a counter in your state function. Increment it in the
button callback, then read the state before and after. The fixture games do
exactly this with `a_down_count` and `a_up_count`.

Without a counter, an unrewritten input call in a C game looks identical to a
game that chose to ignore the press. See
[troubleshooting.md](troubleshooting.md).

## Save data

`read_save_data` and `write_save_data` reach the game's data directory, which
makes save and load testable without playing to the point where a save happens.

- `read_save_data` with no `filename` lists what is there.
- `write_save_data` puts a JSON value in place.

Useful patterns:

- Write a late-game save, launch, and check the game restores it.
- Write a deliberately malformed save and check the game survives it.
- Play, read the save back, and confirm what was written matches the state.

The game has to be running, since the harness resolves the data directory.

## Writing the prompt

What you tell the agent matters more than the tool list. Some things worth
saying explicitly.

**Name the state fields.** An agent that knows `lives` and `level` exist will
use them. One that has to discover them by calling `get_game_state` and reading
JSON wastes turns.

**Say what a bug looks like.** "The score should never go down" is checkable.
"Make sure it works" is not.

**Give it a stopping condition.** Turn count, a level reached, or a specific
state. Otherwise it plays until it runs out of budget.

**Tell it to read logs on anything unexpected.** Agents tend to retry rather
than diagnose. `get_game_logs` carries tracebacks from your update function and
from the button callbacks the harness invokes.

A prompt that works:

> Play until level 2 or 40 turns, whichever comes first. State has `score`,
> `lives`, `level` and `phase`. After each input, check the state changed the way
> you expected. If the score drops or lives go up, stop and read
> `get_game_logs`. Report what you did and anything that looked wrong.

## What it is bad at

Worth knowing so you check these yourself.

- **Anything on a timer.** The agent acts between frames, not on them. It cannot
  reliably test something with a one-frame window.
- **Feel.** Whether a jump is floaty or the crank is too sensitive needs a human.
- **Visual regressions.** Screenshots are readable but nothing compares them
  across runs. The game animates, so a diff would fail for reasons unrelated to
  your change.
