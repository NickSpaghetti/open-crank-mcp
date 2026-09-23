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

Map `uname -s` and `uname -m` onto Go's names, because that is what the release
assets are named with:

| `uname -s` | use |
| --- | --- |
| `Linux` | `linux` |
| `Darwin` | `darwin` |

| `uname -m` | use |
| --- | --- |
| `x86_64`, `amd64` | `amd64` |
| `arm64`, `aarch64` | `arm64` |

On Windows, stop. Tell the user that native Windows is not published yet and that
WSL2 through container mode is the supported route, pointing at
`guides/connecting.md`.

### 3. Fetch the checksums file

```
https://github.com/NickSpaghetti/open-crank-mcp/releases/download/v<version>/checksums.txt
```

Use `gh release download` if the GitHub CLI is available, otherwise `curl -fsSL`.
**`-f` matters**: without it curl exits 0 on a 404 and writes GitHub's error page
into the file, which then looks like a release with no matching assets rather than a
release that does not exist.

If this fetch fails, say that there appears to be no release for that version
rather than reporting a platform problem.

### 4. Pick the asset from that file, do not construct its name

`checksums.txt` holds one `<digest>  <name>` line per published asset. Find the
line whose **name ends with** `_<goos>_<goarch>`.

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
digest on its line in `checksums.txt`. Use `sha256sum` on Linux or
`shasum -a 256` on macOS.

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

Make it executable, then move it into place. Move it only after every check above
has passed, so an interrupted run never leaves a binary a later session would trust
and run.

- **Claude Code**: `$CLAUDE_PLUGIN_DATA/bin/open-crank-mcp`. Create the directory if
  needed. This path is what `.claude-plugin/mcp.json` names, and it survives plugin
  updates.
- **Cursor, or anything else**: somewhere on the user's `PATH`, named exactly
  `open-crank-mcp`, because those configurations name it as a bare command.
  `~/.local/bin` is the usual choice on Linux and macOS. Confirm the directory is
  actually on `PATH` before using it, and if it is not, say which directory you used
  and what the user needs to add.

### 8. Report what happened

Say the version installed, the path, the platform, and whether provenance was
verified or only the checksum. Then tell the user to run `/reload-plugins`, or
restart their editor, because the MCP server was already spawned — and failed —
before this skill ran.

Finish by suggesting `/open-crank-mcp:ocm-doctor`, which checks the Playdate SDK. This
skill installs the server; it does not install the SDK, and it cannot.
