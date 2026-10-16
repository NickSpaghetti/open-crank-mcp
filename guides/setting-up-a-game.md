# Setting up a game

Wiring the harness into a game with the `setup` tool.

Do this once per project. The server can build and launch a game without it, but
screenshots, state and input all need the harness.

Once the server is connected (see [connecting.md](connecting.md)), call `setup`
with `source_dir` pointing at your project. It detects whether the project is
Lua, C or a hybrid of both, copies the harness in, and patches `main.lua`,
`CMakeLists.txt` or your `eventHandler`. Pass `language` (`lua`, `c` or
`hybrid`) to override detection.

The response says what it did:

| Field | What it holds |
|---|---|
| `language` | What it detected or was told. |
| `files_copied` | Harness files it copied in. |
| `files_patched` | Files it edited. An entry with `changed: false` was already current. |
| `manual_steps` | C only. Anything it found but could not safely automate, such as no `PlaydateAPI*` variable it can identify from your update callback. It does not guess. |

Re-running `setup` is safe. It is also how you update a game's copy of the
harness.

The `teardown` tool reverses it. It strips what `setup` added and removes the
copied harness files. If it finds any harness reference it cannot attribute to
its own insertion, it leaves the whole project untouched rather than undo half
of it. A hand-wired project, and any C project whose input calls `setup` has
rewritten, make `teardown` a no-op. See
[harness-wiring.md](harness-wiring.md#why-teardown-refuses-to-do-a-partial-job).

Both tools work on the filesystem only. No Simulator needs to be running, and
neither touches anything outside `source_dir`.
