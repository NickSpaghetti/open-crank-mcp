---
name: ocm-install-server
description: Download and verify the open-crank-mcp server binary for this machine, so the plugin's MCP server can start. Run this once after installing the plugin, and again after the plugin updates to a new version.
---

# Install the open-crank-mcp server

The plugin does not carry the server binary. It is about 10 MB per platform and the
plugin is distributed as a git repository, so the binary is fetched from a GitHub
release instead. This skill does that fetch, verifies it, and puts it where the
plugin's MCP configuration expects it.

Until it has run, the `open-crank-mcp` MCP server will fail to start. That is
expected, not a bug.

## What to do

### 1. Read the version

Read `plugin.json` at the plugin root and take its `version` field. That is the
release to install — do not guess a version and do not use `latest`. If it is
missing or unreadable, stop and say so; everything below depends on it.

### 2. Work out this machine's platform

Map the OS and architecture onto Go's names, because that is what release assets
use:

| Machine | `goos` | `goarch` |
| --- | --- | --- |
| Linux x64 | `linux` | `amd64` |
| macOS Intel | `darwin` | `amd64` |
| macOS Apple Silicon | `darwin` | `arm64` |
| Windows x64 | `windows` | `amd64` |

On Linux and macOS, use `uname -s` and `uname -m`. On Windows, use PowerShell:
`$env:OS` is `Windows_NT`; use `$env:PROCESSOR_ARCHITEW6432` when set, otherwise
`$env:PROCESSOR_ARCHITECTURE`. `AMD64` maps to `amd64`. If it reports ARM64,
stop: there is no Windows ARM64 asset yet.

### 3. Fetch the checksums file

```
https://github.com/NickSpaghetti/open-crank-mcp/releases/download/v<version>/checksums.txt
```

Use `gh release download` if the GitHub CLI is available, otherwise `curl -fsSL`
on Linux/macOS or `curl.exe -fL` in PowerShell.
**`-f` matters**: without it curl exits 0 on a 404 and writes GitHub's error page
into the file, which then looks like a release with no matching assets rather than a
release that does not exist.

If this fetch fails, say that there appears to be no release for that version
rather than reporting a platform problem.

### 4. Pick the asset from that file, do not construct its name

`checksums.txt` holds one `<digest>  <name>` line per published asset. Find the
line whose name ends in `_<goos>_<goarch>`; the Windows asset has an additional
`.exe` suffix, so match `_windows_amd64.exe`.

The name comes from that file so the release stays the only place asset names are
defined. Building the name yourself and requesting it is how you get a 404 that
explains nothing.

- **No matching line**: this release publishes no binary for this platform. Say so
  and stop. On `linux/arm64` this is the correct and expected answer — Panic ships
  no ARM64 Linux SDK, so there is nothing for a server to drive there.
- **More than one matching line**: stop and report how many matched. Do not pick
  one.

### 5. Download it and verify the digest — this step is not optional

Download that asset from the same release, then compare its SHA-256 against the
digest on its line in `checksums.txt`. Use `sha256sum` on Linux,
`shasum -a 256` on macOS, or `Get-FileHash -Algorithm SHA256` on Windows.

If it does not match, delete the download and stop. Do not install it, and do not
retry silently.

This proves the bytes arrived intact. It does **not** prove where they came from:
`checksums.txt` travelled the same channel as the binary it describes. That is what
step 6 is for.

### 6. Verify provenance if you can, and say either way

If `gh` is on `PATH`:

```
gh attestation verify <file> --repo NickSpaghetti/open-crank-mcp
```

This is a keyless Sigstore signature made by the release workflow in this
repository, so it is the only check that speaks to origin rather than integrity.

- **It passes**: say provenance was verified.
- **It fails**: delete the download and stop. A failed check is not a warning.
- **`gh` is not installed**: continue, and tell the user plainly that the checksum
  passed but provenance was not checked, and that installing the GitHub CLI would
  let it be. **Never report this as if it were a successful verification** — "could
  not check" and "checked, and it is fine" are different outcomes and must read
  differently.

### 7. Install it

Move it into place only after every check above has passed, so an interrupted run
never leaves a binary a later session would trust and run. On Unix, also make the
file executable with `chmod +x`.

- **Claude Code**: `$CLAUDE_PLUGIN_DATA/bin/open-crank-mcp`, with `.exe` appended on
  Windows. Create the directory if needed.

  `.claude-plugin/mcp.json` names the extensionless path on every platform, and that
  is deliberate rather than an oversight to fix. Windows process launch appends an
  extension when the named file has none, trying `.com`, `.exe`, `.cmd` and `.bat`,
  so one token resolves `open-crank-mcp` on Unix and `open-crank-mcp.exe` on Windows.
  That is what makes a single `command` work on both, which the config format
  otherwise has no way to express.

  Installing it extensionless on Windows is the mistake: the search then has nothing
  to find, and the server never connects.
- **Cursor or another bare-command client**: put the binary on the user's `PATH`.
  Name it `open-crank-mcp` on Linux/macOS and `open-crank-mcp.exe` on Windows;
  Windows command lookup adds `.exe` through `PATHEXT`. Confirm the directory is
  actually on `PATH` before using it, and if it is not, say what the user needs to
  add.

### 8. Report what happened

Say the version installed, the path, the platform, and whether provenance was
verified or only the checksum. Then tell the user to run `/reload-plugins`, or
restart their editor, because the MCP server was already spawned — and failed —
before this skill ran.

Finish by suggesting `/open-crank-mcp:ocm-doctor`, which checks the Playdate SDK. This
skill installs the server; it does not install the SDK, and it cannot.
