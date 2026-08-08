# Setting up a game

Wiring the harness into a game with the `setup` tool.

Do this once per project. The server can build and launch a game without it, but
screenshots, state and input all need the harness.

Once the server is connected (see [connecting.md](connecting.md)), the fastest way to wire the
harness into a game is the `setup` tool: call it with `source_dir`
pointing at your project, and it detects whether the project is Lua, C,
or a hybrid of both, then copies the harness file(s) in and patches
`main.lua`/`CMakeLists.txt`/your `eventHandler` for you. No manual glue
code for most projects. Pass `language` (`"lua"|"c"|"hybrid"`) to
override detection on the rare project it guesses wrong.

`setup` reports exactly what it did: `files_copied`, `files_patched`, and
(C only) `manual_steps` for anything it found but couldn't safely
automate, such as no confidently-identifiable `PlaydateAPI*` variable
reachable from your update callback. It does not guess. It is
idempotent: re-running it against an already-set-up project is always
safe, and each already-current file is reported as unchanged.

The paired `teardown` tool reverses this. It strips exactly what `setup` added
and removes the copied harness files. It is deliberately conservative. If it
finds any harness reference it cannot confidently attribute to its own
insertion, it leaves the whole project untouched rather than risk a partial
teardown. A hand-wired project, and any C project whose input calls `setup` has
rewritten, therefore make `teardown` a no-op. See
[harness-wiring.md](harness-wiring.md#why-teardown-refuses-to-do-a-partial-job).

Both tools work purely on the filesystem. No simulator needs to be
running, and neither touches anything outside `source_dir`.
