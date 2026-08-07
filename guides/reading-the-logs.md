# Reading the logs

Three separate channels carry output, and only two are readable by a human.

Which one you want depends on whether your game is Lua or C. This applies to
both run modes.

Applies to both modes: these are SDK and harness behaviours, not container ones.
Where the file lives differs, and that is called out below.

Three separate channels carry different output. They are not
interchangeable, and only two of them are readable by a human.

| Channel | Carries | Readable by |
|---|---|---|
| `get_logs` | The Simulator process's real stdout/stderr. GTK warnings, native startup diagnostics, and a C game's `printf` output. | The agent only. Buffered in memory by the server, never written to disk. |
| `get_game_logs` | Lua `print()` calls and uncaught-error tracebacks. | The agent, and you (see below). |
| The Simulator's own console | The Simulator GUI itself, where the SDK renders Lua console output. No console pane is open by default. This is not the only place that output goes: `print()` also reaches the process's real stdout, so `get_logs` sees it too, subject to the buffering described below. In container mode you reach the console through the VNC view; natively it is just the window. | You only. |

**Native mode**: the file is already on your machine, at
`$PLAYDATE_SDK_PATH/Disk/Data/<bundle-id>/mcp/game_logs.jsonl`. Nothing is
mounted and nothing is root-owned. `launch_simulator` reports the exact directory
it resolved as `data_dir`, so you never have to guess.

**Container mode**: the `shared` profile bind-mounts the Simulator's sandboxed
Data directory to `.shared-data/` in this repo, so the same file is readable from
the host while the game runs:

```
tail -f .shared-data/<bundle-id>/mcp/game_logs.jsonl
```

At 256KB that file is renamed to `game_logs.1.jsonl` and a fresh one starts, so
a rotation never leaves you with no history. `get_game_logs` reads both, oldest
first; `tail -f` follows only the current one.

`launch_simulator` returns the `bundle_id` and the container-side
`data_dir`, so the agent can tell you the exact path. The file is written
by the Lua harness on every `print()` call, not batched, so a log from the
frame before a crash still lands. One JSON object per line, appended.

**A game set up before this file was renamed needs `setup` re-run.** The
harness is a *copy* in your game's own source tree, and an older copy writes
`game_logs.json` (a single JSON array) instead. `get_game_logs` fails with a
message saying exactly that rather than returning an empty list, and
`get_status` reports a `harness_warning` for any game whose harness copy
differs from the one this server ships. `setup` is safe to re-run and is the
whole fix.

Two asymmetries worth knowing. Both are covered in detail in
[`docs/GOTCHAS.md`](../docs/GOTCHAS.md).

- **Lua: use `get_game_logs`.** A game's `print()` does reach real stdout, so
  `get_logs` sees it, but stdout is block-buffered. A quiet game shows nothing,
  and whatever is still buffered is lost when the Simulator is killed. The
  harness writes each entry to disk immediately instead, so it is complete the
  moment you ask. It also captures tracebacks from your update function and from
  the button callbacks the harness invokes.
- **C: use `get_logs`.** A C game's `printf` reaches real stdout, so `get_logs`
  already covers it. The C harness does not write `game_logs.jsonl` at all.
