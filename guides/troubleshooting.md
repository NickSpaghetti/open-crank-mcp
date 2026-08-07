# Troubleshooting

Symptoms first, in the order they tend to bite.

Each entry says what you see, what causes it, and what to do. The investigation
behind most of them is in [`docs/GOTCHAS.md`](../docs/GOTCHAS.md).

## Every tool times out after five seconds

`get_status` reports `harness_reachable: false`. Screenshots, state and input all
hang and then fail.

The harness is not answering. In order of likelihood:

1. **The game was never set up.** Run `setup` against the project, rebuild, and
   relaunch. Building and launching work without the harness, so this failure
   arrives late.
2. **The game crashed on startup.** Check `get_game_logs` for a traceback. A game
   that dies before its first frame never starts polling.
3. **The build is stale.** `setup` patches source. If you did not rebuild after
   it, the running `.pdx` is the old one.
4. **Two Simulators are running.** See below.

## get_game_state returns nothing

The tool succeeds and the state is empty.

Your game has no registered state function, or it returns something that is not
a JSON string. There is no default state. See
[exposing-game-state.md](exposing-game-state.md), which lists the two ways this
fails silently.

## press_button or set_crank does nothing, in a C game

The tool reports success and the game does not react.

C games read input through wrapper functions. `pd->system` is write-protected in
the real Simulator, so an override can only take effect if your code calls
`mcp_get_button_state` and friends instead of `pd->system->getButtonState`.

`setup` rewrites those calls for you. When it cannot, it says so. Re-read the
`manual_steps` in the `setup` response. An entry names the file, the line and
the wrapper to use.

Two shapes it declines to rewrite: a receiver it cannot reach, like
`api()->pd->system->getButtonState(...)`, and spacing it does not accept, like
`pd -> system -> getCrankAngle()`.

## get_game_logs is empty, or complains about game_logs.json

The game was set up before the log file was renamed. Its vendored harness copy
is old and still writes the previous format.

Re-run `setup`. It is safe to re-run and this is the whole fix.

## get_status reports a harness_warning

The harness answering is not the one this server ships. The harness is a copy in
your game's source tree, so it drifts when the server updates.

Re-run `setup` and rebuild.

## Two Simulators answer at once

Responses look inconsistent. State does not match the screenshot.

Nothing supervises a Simulator the agent launched. If a client disconnects
without `stop_simulator`, it keeps running, and the next `launch_simulator`
starts a second one alongside it. Both poll the same command file.

```
pkill -9 -f 'bin/PlaydateSimulato[r]'
```

The bracket is not a typo. `pkill -f` matches every process's whole command
line, including the shell running the `pkill`. Without it you kill your own
shell instead, silently.

## The Simulator exits immediately, headless

It reports `dsp: No such audio device` and stops before doing anything.

The Simulator treats SDL initialisation as fatal and will not start without an
audio driver. On a CI runner or a server over SSH there is none.

```
SDL_AUDIODRIVER=dummy
```

The container image already sets this. A desktop with real audio needs none of
it.

## cmake fails on a stale CMakeCache.txt

You built the same game in both container and native mode. cmake records
absolute paths, and the container sees your game at `/workspace` while a native
run sees its real path.

`build_game` detects this specific failure, clears the cache, says so in its
output, and reconfigures. The message is expected rather than alarming.

## No SDK found, or the wrong one

```
make sdk-path
```

That prints the SDK it resolved, which of the three sources found it, and every
candidate it considered. Detection is silent when it succeeds, so this is the
first thing to reach for.

Resolution order is `PLAYDATE_SDK_PATH`, then `SDKRoot` in `~/.Playdate/config`,
then the per-OS default location.

## macOS: the game loads and never runs

A first-run dialog is open behind the Simulator window, and the game is waiting
on a click that never comes. `Loading: <game>.pdx/` appears on stdout, so the
launch looks fine.

Dismiss it once by hand. The documented setting does not suppress it, and the
symptoms differ between C and Lua games, which makes it easy to misdiagnose.
See the macOS entry in [`docs/GOTCHAS.md`](../docs/GOTCHAS.md).

## Still stuck

`get_status` first. It reports whether the Simulator is running, its bundle ID,
and whether the harness answers, which separates "nothing is running" from
"running but not wired".

Then `get_logs` for the Simulator's own output, and `get_game_logs` for your
game's. [reading-the-logs.md](reading-the-logs.md) explains which carries what.
