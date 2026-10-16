# Installing as a plugin

Your editor installs the server. No clone, no `make`, and no absolute path to
write down.

It does **not** install the Playdate SDK, and cannot. Panic's licence forbids
redistributing it. See [Requirements](../README.md#requirements). Everything
below assumes you have an SDK, or are about to install one.

The plugin runs the server in native mode on Linux x64, macOS and Windows x64.
Windows is experimental and needs the Windows Playdate SDK. Windows on ARM64 and
Linux on ARM64 are not supported.

## Claude Code

```
/plugin marketplace add NickSpaghetti/open-crank-mcp
/plugin install open-crank-mcp@open-crank-mcp
```

Then install the server:

```
/open-crank-mcp:ocm-install-server
```

The plugin is a git repository and does not carry the roughly 10 MB server
binary, so the skill downloads it from the GitHub release for the plugin's
version. Until the skill has run, **the server shows as failing to connect**.
That is expected. Run `/reload-plugins` afterwards, because the client already
tried to start the server before the binary existed.

Then `/open-crank-mcp:ocm-doctor` to check the Playdate SDK, which is a separate thing
the plugin cannot install for you.

## Cursor

```
cursor-agent plugin marketplace add https://github.com/NickSpaghetti/open-crank-mcp
```

Any git URL works, self-hosted included.

Then run the `ocm-install-server` skill, the same as on Claude Code. Cursor has no
plugin data directory, so the binary goes somewhere on your `PATH` and the skill
reports which directory it used. Until that has run the server shows as failing to
connect. Restart Cursor afterwards.

Then run the `ocm-doctor` skill to check the Playdate SDK.

## OpenCode

OpenCode has plugins, but its v1 plugin API has no config or MCP hook, so a plugin
cannot register a server. Its config is written by hand. The server prints
the exact block:

```
open-crank-mcp -print-config opencode
```

Paste that into `opencode.json`. The command is the binary's own resolved
path, so run it from the binary you want OpenCode to use.

The skills are a separate step, because OpenCode does not read a plugin
directory:

```
npx skills add -g -a '*' -s '*' NickSpaghetti/open-crank-mcp
```

Three parts of that matter. `-a '*'` makes the install use **symlinks**. With
a single target directory the CLI copies instead, and a copy goes stale the next
time the skills change. `-g` installs at user level. Without it, a run from
inside an agent session is silently project-scoped. `--copy` is left off on
purpose.

That CLI has telemetry on by default; `DISABLE_TELEMETRY` or `DO_NOT_TRACK`
turns it off.

**Use one channel per client.** A skill installed by `npx skills` is a personal
skill (`/ocm-playtesting-a-playdate-game`); the same file inside the plugin is a
plugin skill (`/open-crank-mcp:ocm-playtesting-a-playdate-game`). Installing both
gives you one skill under two names.

## When it does not work

```
open-crank-mcp -doctor
```

It reports the resolved SDK and every path it considered, whether the
Simulator's shared libraries resolve, what `pdc` says, and on macOS whether the
Simulator appears never to have run. It **exits 0 even when it finds problems**,
because "no SDK found" is the ordinary state on a fresh install. Read the text,
not the exit code. `-doctor -doctor-launch` additionally starts the Simulator, which
puts a window on your desktop.

Two common cases:

- **macOS, first ever run.** A first-run dialog opens behind the Simulator
  window and the game waits on a click that never comes, while the launch looks
  fine. Dismiss it once by hand. `-doctor` warns when it can tell.
- **The SDK is installed and not found.** If you start your editor from a desktop
  icon rather than a shell, a `PLAYDATE_SDK_PATH` exported in your shell
  profile is not inherited. In Claude Code, set the plugin's Playdate SDK path
  option. Elsewhere, set the variable where the editor will see it.

[troubleshooting.md](troubleshooting.md) covers the rest.

## How the binary gets there

`/open-crank-mcp:ocm-install-server` does it. Every install failure is one of
these steps.

1. Reads the version from the plugin's own `plugin.json`. That version, not
   `latest`.
2. Fetches `checksums.txt` from that release and finds the line whose asset name
   ends with your platform's `_<os>_<arch>` (plus `.exe` on Windows). The name
   comes from that file rather than being built by the skill.
3. Downloads that asset and checks its SHA-256 against the digest on its line.
   **Mandatory.** A mismatch deletes the download and stops.
4. If the GitHub CLI is installed, runs `gh attestation verify`. That is a keyless
   Sigstore signature from this repository's release workflow, and it is the only
   check that says anything about *origin*. A checksum downloaded alongside the
   binary cannot. If `gh` is absent it continues and tells you provenance was not
   checked. If `gh` is present and the check *fails*, it refuses.
5. Installs it only after everything above passed, so an interrupted run never
   leaves a binary a later session would trust.

To run your own build instead, put your `make go-build` output where the
skill would have installed the binary: `$CLAUDE_PLUGIN_DATA/bin/` on Claude
Code, or the directory on your `PATH` on Cursor.

If your platform has no published binary the skill says so and stops. Binaries
are published for `linux/amd64`, `darwin/amd64`, `darwin/arm64` and
`windows/amd64`. There is no `linux/arm64` because Panic ships no ARM64 Linux
SDK.
