---
name: doctor
description: Diagnose an open-crank-mcp installation that is not working - no SDK, a Simulator that will not start, tools that time out - and say which fix applies.
disable-model-invocation: true
---

# Diagnose the setup

Run the server's own environment check first. It works even when the MCP
connection does not, which is the case that matters most:

```
open-crank-mcp -doctor
```

If `open-crank-mcp` is not on your `PATH`, the binary is wherever
`/open-crank-mcp:install-server` put it — under the plugin's data directory on
Claude Code. Run it from there with the same flag.

It exits 0 whether or not it finds problems, so read the text rather than the
status. Then, if the MCP connection is up, `get_status` for the running side.

## Reading the result

**`Playdate SDK: not found`** — the ordinary state on a fresh install, not a
fault. The report lists every path it looked at. Either install the SDK or set
`PLAYDATE_SDK_PATH` to where it already is. If the editor was started from a
desktop launcher rather than a shell, a `PLAYDATE_SDK_PATH` exported in a shell
profile will not have been inherited — in Claude Code the plugin's own SDK-path
setting fixes that without touching the environment.

**`shared libraries:` naming something not found** — the Simulator cannot start
at all, usually exiting 127 before any of Panic's code runs. On Arch that is one
package, `webkit2gtk-4.1`. On Debian and Ubuntu the authoritative list is the
`apt` line in the `native` job of the repository's CI workflow.

On macOS this line can only confirm the binary exists, because the dynamic loader
there resolves lazily. Use `-doctor -doctor-launch` to actually start it.

**`the Simulator appears never to have run on this machine`** (macOS only) — a
first-run dialog is going to open behind the Simulator window and the game will
wait on a click that never comes, while the launch looks fine. Dismiss it once by
hand. This is an inference from an absent preferences file, not a measurement.

**`harness_reachable: false` from `get_status`** — the game is running and
nothing is wired into it, so every other tool will time out five seconds at a
time. Re-run `setup` and check `manual_steps`.

**Every tool timing out, with `harness_reachable: true`** — usually two
Simulators answering one harness, because a previous session left one running.
Stop them and launch once.

Report which of these applies and the specific fix, rather than the whole output.
Anything the report does not cover is in
[troubleshooting](https://github.com/NickSpaghetti/open-crank-mcp/blob/main/guides/troubleshooting.md).
