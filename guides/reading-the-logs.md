# Reading the logs

Three channels carry output. They are not interchangeable, and which one you
want depends on whether your game is Lua or C. This holds in both run modes;
only where the files live differs.

| Channel | Carries | Readable by |
|---|---|---|
| `get_logs` | The Simulator process's stdout and stderr: GTK warnings, startup diagnostics, and a C game's `printf` output. | The agent only. Held in memory by the server, never written to disk. |
| `get_game_logs` | A Lua game's `print()` output and uncaught-error tracebacks. | The agent, and you (see below). |
| The Simulator's console | The SDK's own console pane in the Simulator window. Closed by default. In container mode you reach it through the VNC view. | You only. |

**Lua: use `get_game_logs`.** A Lua game's `print()` does not reliably show up
in `get_logs`. The Simulator's stdout is block-buffered, so a quiet game shows
nothing, and whatever is still buffered is lost when the Simulator is killed.
The harness writes each entry to disk as it happens, so `get_game_logs` is
complete the moment you ask. It also carries tracebacks from your update
function and from the button callbacks the harness invokes.

**C: use `get_logs`.** A C game's `printf` goes to the Simulator's stdout. The C
harness does not write `game_logs.jsonl`.

## Where the file is

The Lua harness appends one JSON object per line to `mcp/game_logs.jsonl` under
the game's data directory. At 256KB it is renamed to `game_logs.1.jsonl` and a
fresh one starts. `get_game_logs` reads both, oldest first. `tail -f` follows
only the current one.

`launch_simulator` returns the `bundle_id` and the `data_dir` it resolved, so
the agent can tell you the exact path.

**Native mode**: the file is on your machine, normally at
`<SDK>/Disk/Data/<bundle-id>/mcp/game_logs.jsonl`.

**Container mode**: the `shared` profile bind-mounts the Simulator's Data
directory to `.shared-data/` in this repo, so the file is readable from the
host while the game runs:

```
tail -f .shared-data/<bundle-id>/mcp/game_logs.jsonl
```

The `data_dir` that `launch_simulator` returns is the path inside the container.

## An older harness copy

The harness is a copy in your game's own source tree. A copy older than the
JSONL format writes `game_logs.json` instead. `get_game_logs` fails with a
message saying so, and `get_status` reports a `harness_warning` for any game
whose harness differs from the one this server ships. Re-run `setup` to fix
both.

[`docs/GOTCHAS.md`](../docs/GOTCHAS.md) has the detail behind all of this.
