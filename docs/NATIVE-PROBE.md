# Probing a platform for native mode

## Run this, and nothing else

**macOS is done. Nothing outstanding there.** Parts 1 and 3 between them
confirmed every value, and `internal/sdk` has been corrected to match. The raw
output is kept below as the evidence.

Windows is done too. Its values are corrected in `internal/sdk`, and one of its
answers settled the scope question permanently: the SDK ships as an interactive
installer `.exe`, with no archive. CI fetches and extracts a `.tar.gz` to
provision Linux, so a Windows runner cannot provision itself, which means
Windows-native could never get the per-PR verification the other platforms have.
That turns "unsupported for now" into "unsupported". Recorded in
`docs/ROADMAP.md`.

Nothing on this page is outstanding.

### What Windows changed

| Value | First guess | Real |
|---|---|---|
| `~/.Playdate/config` | assumed present | **does not exist at all** |
| SDK install location | `%LOCALAPPDATA%\Programs\PlaydateSDK` | `%USERPROFILE%\Documents\PlaydateSDK` |
| Simulator, `pdc` | `PlaydateSimulator.exe`, `pdc.exe` | correct |
| Simulator support dir | `%APPDATA%` | **`%LOCALAPPDATA%`**, and `%APPDATA%` holds nothing |

The missing config file is the notable one. On macOS `~/.Playdate/config` is the
primary signal and the installer writes it; on Windows there is no such file, so
resolution has nothing but the default location to go on. Two platforms, two
different primary signals, which is a good argument for the resolution order
being a list rather than a single lookup.

`bin/` also holds `crashpad_handler.exe`, the same trap as macOS: anything
picking an executable out of that directory by listing it gets the crash reporter.

## Why this exists

Native mode runs the server directly against an SDK the developer installed, with
no container. Where the SDK lives, and where things sit inside it, differs per
platform. Linux is the only layout verified by running it. The macOS and Windows
values in `internal/sdk` were written from Panic's documentation and platform
convention.

This is the checklist for checking one against a real install. Everything is
read-only except part 3, which builds a copy of the test fixture into `/tmp` and
launches it for ten seconds.

## Do not run the server binary yet

The point of this document is to collect facts about the machine, not to test
the implementation. The values it corrects are the ones that decide whether the
implementation can work at all, so they come first. Once they are right, a
cross-compiled binary is easy to hand over and there is something worth running.

## Status

macOS part 1 is done. Four of the five values are confirmed against a real
SDK 3.1.1 install, and every one matched what `internal/sdk` already guessed:

| Value | Confirmed as | Matched the guess? |
|---|---|---|
| SDK install location | `~/Developer/PlaydateSDK` | yes |
| `~/.Playdate/config` | `SDKRoot\t<path>\n`, tab-separated | yes |
| Simulator bundle | `bin/Playdate Simulator.app` | yes |
| Simulator executable | `.../Contents/MacOS/Playdate Simulator` | yes |
| `pdc` | `bin/pdc`, no extension | yes |
| Sandboxed data directory | `<sdk>/Disk/Data/<bundleID>` | **no, and it mattered** |
| Lua `print()` on real stdout | never, same as Linux | yes |

The data directory is the interesting one. macOS convention says an app keeps
that kind of state under `~/Library/Application Support`, so the original guess
put two paths there ahead of the in-SDK one. The Simulator does not: a game run
on macOS left `mcp/game_logs.json` at `<sdk>/Disk/Data/<bundleID>/`, and nothing
whatsoever appeared under Application Support or Containers. Reasoning from
convention had the right answer listed last. The candidate order is now corrected
and pinned by a test.

The `print()` result settles a question `docs/GOTCHAS.md` could only answer for
Linux. The count was zero and the captured output was completely empty, so the
Simulator withholds Lua console output from real stdout on macOS as well, and
`get_game_logs` is necessary on both platforms rather than being a Linux
workaround.

Two incidental findings worth keeping: that install has no plain
`bin/PlaydateSimulator` beside the bundle, and `Contents/MacOS` also contains
`crashpad_handler`, which sorts alphabetically *before* the Simulator.

Part 2 answered nothing because the script had three bugs. Part 3 is the
corrected version, and the data directory is all it is still after.

## What is uncertain, and what it costs to get wrong

| Value | If it is wrong |
|---|---|
| SDK default install location | Detection falls through to an error. Loud and easy to work around by setting `PLAYDATE_SDK_PATH`. |
| `~/.Playdate/config` format | Same. The config is skipped and detection moves on. |
| The Simulator executable path | `launch_simulator` fails immediately. Loud. |
| **The sandboxed data directory** | **Silent.** Launching succeeds, the game runs, and then every tool that talks to the harness times out after five seconds with nothing naming the cause. This is the one worth the most care. |

The data directory has three fallbacks behind it (candidate probing, a bounded
search, then a warning naming `OPEN_CRANK_DATA_ROOT`), so a wrong guess should
degrade rather than break. Confirming the real value removes the need for any of
that to fire.

## macOS, part 1: the layout (DONE, do not re-run)

Kept for the raw output, which is the evidence behind the confirmed values in
the Status table. The script and its response are below; nothing here needs
running again.

```bash
{
echo "=== 1. the SDK's own config, byte-exact ==="
if [ -f "$HOME/.Playdate/config" ]; then
  od -c "$HOME/.Playdate/config" | head -20
else
  echo "MISSING: $HOME/.Playdate/config"
fi

echo; echo "=== 2. candidate SDK roots ==="
for d in "$HOME/Developer/PlaydateSDK" "$HOME/PlaydateSDK" "/Applications/PlaydateSDK"; do
  if [ -d "$d" ]; then echo "EXISTS  $d"; else echo "absent  $d"; fi
done

SDK=$(awk '$1=="SDKRoot"{print $2; exit}' "$HOME/.Playdate/config" 2>/dev/null)
[ -z "$SDK" ] && SDK="$HOME/Developer/PlaydateSDK"
echo; echo "using SDK=$SDK"

echo; echo "=== 3. bin/ ==="
if [ -d "$SDK/bin" ]; then ls -la "$SDK/bin" | head -30; else echo "MISSING: $SDK/bin"; fi

echo; echo "=== 4. the Simulator bundle, and the executable inside it ==="
find "$SDK" -maxdepth 3 -name "*.app" 2>/dev/null
find "$SDK" -maxdepth 6 -path "*.app/Contents/MacOS/*" 2>/dev/null

echo; echo "=== 5. candidate data directories ==="
for d in "$HOME/Library/Application Support"/*laydate*; do
  if [ -d "$d" ]; then echo "EXISTS  $d"; find "$d" -maxdepth 3 2>/dev/null | head -20; fi
done
echo "--- in-SDK Disk/ ---"
if [ -d "$SDK/Disk" ]; then find "$SDK/Disk" -maxdepth 3 2>/dev/null | head -20; else echo "absent  $SDK/Disk"; fi
} 2>&1 | tee "$HOME/playdate-macos-report.txt"
```

RESPONSE ON MAC:
```shell
 @admin  {
echo "=== 1. the SDK's own config, byte-exact ==="
if [ -f "$HOME/.Playdate/config" ]; then
  od -c "$HOME/.Playdate/config" | head -20
else
  echo "MISSING: $HOME/.Playdate/config"
fi

echo; echo "=== 2. candidate SDK roots ==="
for d in "$HOME/Developer/PlaydateSDK" "$HOME/PlaydateSDK" "/Applications/PlaydateSDK"; do
  if [ -d "$d" ]; then echo "EXISTS  $d"; else echo "absent  $d"; fi
done

SDK=$(awk '$1=="SDKRoot"{print $2; exit}' "$HOME/.Playdate/config" 2>/dev/null)
[ -z "$SDK" ] && SDK="$HOME/Developer/PlaydateSDK"
echo; echo "using SDK=$SDK"

echo; echo "=== 3. bin/ ==="
if [ -d "$SDK/bin" ]; then ls -la "$SDK/bin" | head -30; else echo "MISSING: $SDK/bin"; fi

echo; echo "=== 4. the Simulator bundle, and the executable inside it ==="
find "$SDK" -maxdepth 3 -name "*.app" 2>/dev/null
find "$SDK" -maxdepth 6 -path "*.app/Contents/MacOS/*" 2>/dev/null

echo; echo "=== 5. candidate data directories ==="
for d in "$HOME/Library/Application Support"/*laydate*; do
  if [ -d "$d" ]; then echo "EXISTS  $d"; find "$d" -maxdepth 3 2>/dev/null | head -20; fi
done
echo "--- in-SDK Disk/ ---"
if [ -d "$SDK/Disk" ]; then find "$SDK/Disk" -maxdepth 3 2>/dev/null | head -20; else echo "absent  $SDK/Disk"; fi
} 2>&1 | tee "$HOME/playdate-macos-report.txt"
=== 1. the SDK's own config, byte-exact ===
0000000    S   D   K   R   o   o   t  \t   /   U   s   e   r   s   /   a
0000020    d   m   i   n   /   D   e   v   e   l   o   p   e   r   /   P
0000040    l   a   y   d   a   t   e   S   D   K  \n
0000053

=== 2. candidate SDK roots ===
EXISTS  /Users/admin/Developer/PlaydateSDK
absent  /Users/admin/PlaydateSDK
absent  /Applications/PlaydateSDK

using SDK=/Users/admin/Developer/PlaydateSDK

=== 3. bin/ ===
total 4240
drwxr-xr-x   7 admin  staff      224 Jul 27 15:23 .
drwxr-xr-x  15 admin  staff      480 Jul 22 15:35 ..
-rw-r--r--@  1 admin  staff     6148 Jul 27 15:23 .DS_Store
-rw-r--r--   1 admin  staff     1076 Jul 22 15:30 firmware_symbolizer.py
-rwxr-xr-x   1 admin  staff  2015760 Jul 22 15:30 pdc
-rwxr-xr-x   1 admin  staff   136224 Jul 22 15:30 pdutil
drwxr-xr-x@  3 admin  staff       96 Jul 22 15:32 Playdate Simulator.app

=== 4. the Simulator bundle, and the executable inside it ===
/Users/admin/Developer/PlaydateSDK/bin/Playdate Simulator.app
/Users/admin/Developer/PlaydateSDK/bin/Playdate Simulator.app/Contents/MacOS/crashpad_handler
/Users/admin/Developer/PlaydateSDK/bin/Playdate Simulator.app/Contents/MacOS/Playdate Simulator

=== 5. candidate data directories ===
zsh: no matches found: /Users/admin/Library/Application Support/*laydate*
```

Section 5 is worth more once a game has run at least once, since the Simulator
may not create the directory until then. If you have any `.pdx`, open it in the
Simulator, quit, and run section 5 again.

## macOS, part 2: superseded by part 3, do not re-run

This produced no usable answer. Three bugs, all mine, all listed at the top of
part 3. Kept because the failure output is what identified them: the
`crashpad_handler` line in particular is a trap worth being able to point at.

`docs/GOTCHAS.md` records that on Linux the Simulator's Lua console output never
reaches the process's real stdout. That finding is the entire reason
`get_game_logs` exists. It was only ever checked inside the container, so it is
a Linux result, not a platform-independent one.

If macOS pipes `print()` to stdout, then `get_logs` alone covers Lua there and
the rule is narrower than the docs currently say. Either answer is useful.

Clone this branch on the Mac, then from the repo root:

```bash
SDK=$(awk '$1=="SDKRoot"{print $2; exit}' "$HOME/.Playdate/config" 2>/dev/null)
[ -z "$SDK" ] && SDK="$HOME/Developer/PlaydateSDK"

"$SDK/bin/pdc" lua/test-fixture/Source /tmp/fixture.pdx || echo "pdc failed"

SIM=$(find "$SDK" -maxdepth 6 -path "*.app/Contents/MacOS/*" 2>/dev/null | head -1)
echo "simulator: $SIM"

"$SIM" /tmp/fixture.pdx > /tmp/sim-stdout.txt 2>&1 &
SIMPID=$!
sleep 8
# -9 because the Simulator ignores SIGTERM. That is a real finding, not caution:
# see internal/simulator and docs/GOTCHAS.md.
kill -9 "$SIMPID" 2>/dev/null

echo "--- occurrences of the fixture's print() in real stdout ---"
grep -c "fixture print line" /tmp/sim-stdout.txt
echo "--- first 40 lines of what the process actually wrote ---"
head -40 /tmp/sim-stdout.txt
```

RESPONSE ON MACOS:
```shell
@admin  SDK=$(awk '$1=="SDKRoot"{print $2; exit}' "$HOME/.Playdate/config" 2>/dev/null)
[ -z "$SDK" ] && SDK="$HOME/Developer/PlaydateSDK"

"$SDK/bin/pdc" lua/test-fixture/Source /tmp/fixture.pdx || echo "pdc failed"

SIM=$(find "$SDK" -maxdepth 6 -path "*.app/Contents/MacOS/*" 2>/dev/null | head -1)
echo "simulator: $SIM"

"$SIM" /tmp/fixture.pdx > /tmp/sim-stdout.txt 2>&1 &
SIMPID=$!
sleep 8
# -9 because the Simulator ignores SIGTERM. That is a real finding, not caution:
# see internal/simulator and docs/GOTCHAS.md.
kill -9 "$SIMPID" 2>/dev/null

echo "--- occurrences of the fixture's print() in real stdout ---"
grep -c "fixture print line" /tmp/sim-stdout.txt
echo "--- first 40 lines of what the process actually wrote ---"
head -40 /tmp/sim-stdout.txt
error: lua/test-fixture/Source/main.lua:2: No such file: mcp_harness
pdc failed
simulator: /Users/admin/Developer/PlaydateSDK/bin/Playdate Simulator.app/Contents/MacOS/crashpad_handler
[1] 13954
[1]  + 13954 exit 1     "$SIM" /tmp/fixture.pdx > /tmp/sim-stdout.txt 2>&1
--- occurrences of the fixture's print() in real stdout ---
0
--- first 40 lines of what the process actually wrote ---
crashpad_handler: --handshake-fd or --mach-service is required
Try 'crashpad_handler --help' for more information.
```

A count above zero means macOS behaves differently from Linux here. Zero
confirms the existing finding holds on both.

While that ran, the fixture also wrote save data, so this is the best moment to
re-check where it landed:

```bash
find "$HOME/Library/Application Support" "$SDK/Disk" -maxdepth 4 -name "*contractcheck*" 2>/dev/null
find "$HOME/Library/Application Support" "$SDK/Disk" -maxdepth 5 -name "game_logs.json" 2>/dev/null
```
RESPONSE:
```bash
 find "$HOME/Library/Application Support" "$SDK/Disk" -maxdepth 4 -name "*contractcheck*" 2>/dev/null
find "$HOME/Library/Application Support" "$SDK/Disk" -maxdepth 5 -name "game_logs.json" 2>/dev/null
```

Those two paths are the answer to the riskiest value in the table above.

## macOS, part 3: the data directory, corrected

**This is the one to run.** Everything else on this page is already answered.

Part 1 confirmed four values and part 2 answered nothing, because the part 2
script had three bugs. All three are worth naming, since two of them are traps
anyone writing a probe on macOS will hit.

1. **`pdc` cannot build the fixture on its own.** `lua/test-fixture/Source/main.lua`
   imports `mcp_harness`, and that file is only there after `setup` copies it in.
   The fixture is not a standalone game. Copy the harness in first.
2. **`Contents/MacOS` holds more than one executable.** It also has
   `crashpad_handler`, which sorts first, so `find ... | head -1` picked the crash
   reporter and ran that instead of the Simulator. Name the file instead of
   listing the directory.
3. **zsh aborts on an unmatched glob.** `for d in .../*laydate*` does not fall
   through to the literal string the way bash does; zsh reports
   `no matches found` and stops the whole block, which is why section 5 never
   printed the in-SDK part. Run the block under `bash`, or avoid the glob.

Corrected, and it answers the one value still open. Run from the repo root:

```bash
bash <<'PROBE'
SDK=$(awk '$1=="SDKRoot"{print $2; exit}' "$HOME/.Playdate/config" 2>/dev/null)
[ -z "$SDK" ] && SDK="$HOME/Developer/PlaydateSDK"

# Name the Simulator rather than listing the directory: crashpad_handler lives
# next to it and sorts first.
SIM="$SDK/bin/Playdate Simulator.app/Contents/MacOS/Playdate Simulator"
echo "simulator: $SIM"
[ -x "$SIM" ] || echo "NOT EXECUTABLE: $SIM"

# The fixture imports mcp_harness, so the harness has to be beside it before pdc
# will build it. Built in a copy so the repo is left alone.
rm -rf /tmp/probe-game && mkdir -p /tmp/probe-game
cp -R lua/test-fixture/Source /tmp/probe-game/
cp lua/mcp_harness.lua /tmp/probe-game/Source/
"$SDK/bin/pdc" /tmp/probe-game/Source /tmp/probe-game/probe.pdx || echo "pdc failed"

"$SIM" /tmp/probe-game/probe.pdx > /tmp/sim-stdout.txt 2>&1 &
SIMPID=$!
sleep 10
kill -9 "$SIMPID" 2>/dev/null

echo "=== did Lua print() reach real stdout? (0 means no) ==="
grep -c "fixture print line" /tmp/sim-stdout.txt
echo "=== what the process actually wrote ==="
head -40 /tmp/sim-stdout.txt

echo
echo "=== WHERE DID THE DATA GO? this is the value still missing ==="
for base in "$HOME/Library/Application Support" "$HOME/Library/Containers" "$SDK/Disk"; do
  if [ -d "$base" ]; then
    echo "--- under $base ---"
    find "$base" -maxdepth 5 -name "game_logs.json" 2>/dev/null
    find "$base" -maxdepth 4 -iname "*contractcheck*" 2>/dev/null
    find "$base" -maxdepth 2 -iname "*laydate*" 2>/dev/null
  else
    echo "absent  $base"
  fi
done
PROBE
```

PROBE RESPONSE
```
>....

# Name the Simulator rather than listing the directory: crashpad_handler lives
# next to it and sorts first.
SIM="$SDK/bin/Playdate Simulator.app/Contents/MacOS/Playdate Simulator"
echo "simulator: $SIM"
[ -x "$SIM" ] || echo "NOT EXECUTABLE: $SIM"

# The fixture imports mcp_harness, so the harness has to be beside it before pdc
# will build it. Built in a copy so the repo is left alone.
rm -rf /tmp/probe-game && mkdir -p /tmp/probe-game
cp -R lua/test-fixture/Source /tmp/probe-game/
cp lua/mcp_harness.lua /tmp/probe-game/Source/
"$SDK/bin/pdc" /tmp/probe-game/Source /tmp/probe-game/probe.pdx || echo "pdc failed"

"$SIM" /tmp/probe-game/probe.pdx > /tmp/sim-stdout.txt 2>&1 &
SIMPID=$!
sleep 10
kill -9 "$SIMPID" 2>/dev/null

echo "=== did Lua print() reach real stdout? (0 means no) ==="
grep -c "fixture print line" /tmp/sim-stdout.txt
echo "=== what the process actually wrote ==="
head -40 /tmp/sim-stdout.txt

echo
echo "=== WHERE DID THE DATA GO? this is the value still missing ==="
for base in "$HOME/Library/Application Support" "$HOME/Library/Containers" "$SDK/Disk"; do
  if [ -d "$base" ]; then
    echo "--- under $base ---"
    find "$base" -maxdepth 5 -name "game_logs.json" 2>/dev/null
    find "$base" -maxdepth 4 -iname "*contractcheck*" 2>/dev/null
    find "$base" -maxdepth 2 -iname "*laydate*" 2>/dev/null
  else
    echo "absent  $base"
  fi
done
PROBE
simulator: /Users/admin/Developer/PlaydateSDK/bin/Playdate Simulator.app/Contents/MacOS/Playdate Simulator
=== did Lua print() reach real stdout? (0 means no) ===
0
bash: line 23: 14460 Killed: 9               "$SIM" /tmp/probe-game/probe.pdx > /tmp/sim-stdout.txt 2>&1
=== what the process actually wrote ===

=== WHERE DID THE DATA GO? this is the value still missing ===
--- under /Users/admin/Library/Application Support ---
--- under /Users/admin/Library/Containers ---
--- under /Users/admin/Developer/PlaydateSDK/Disk ---
/Users/admin/Developer/PlaydateSDK/Disk/Data/dev.open-crank-mcp.contractchecklua/mcp/game_logs.json
/Users/admin/Developer/PlaydateSDK/Disk/Data/dev.open-crank-mcp.contractchecklua
```

The `game_logs.json` line is the answer. The harness writes it inside whatever
directory the Simulator sandboxes for that game, so wherever it turns up is the
real data directory, and its parent is what `OPEN_CRANK_DATA_ROOT` would be set
to.

## Windows, optional

Windows-native is deliberately unsupported: WSL2 already serves Windows users
through the container, and supporting it natively would roughly double the
untested surface. See `docs/ROADMAP.md`. So these answers do not change current
behaviour. They only say whether promoting Windows later would be cheap.

PowerShell, read-only:

```powershell
Get-Content "$env:USERPROFILE\.Playdate\config" -ErrorAction SilentlyContinue

foreach ($d in @("$env:LOCALAPPDATA\Programs\PlaydateSDK",
                 "$env:USERPROFILE\Documents\PlaydateSDK",
                 "C:\Program Files\PlaydateSDK")) {
  if (Test-Path $d) { "EXISTS  $d" } else { "absent  $d" }
}

Get-ChildItem "$env:USERPROFILE\Documents\PlaydateSDK\bin" -ErrorAction SilentlyContinue |
  Select-Object Name

Get-ChildItem "$env:APPDATA" -Filter "*laydate*" -ErrorAction SilentlyContinue
Get-ChildItem "$env:LOCALAPPDATA" -Filter "*laydate*" -ErrorAction SilentlyContinue
```

response windows

```powershell
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp> Get-Content "$env:USERPROFILE\.Playdate\config" -ErrorAction SilentlyContinue
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp>
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp> foreach ($d in @("$env:LOCALAPPDATA\Programs\PlaydateSDK",
>>                  "$env:USERPROFILE\Documents\PlaydateSDK",
>>                  "C:\Program Files\PlaydateSDK")) {
>>   if (Test-Path $d) { "EXISTS  $d" } else { "absent  $d" }
>> }
absent  C:\Users\nc_ci\AppData\Local\Programs\PlaydateSDK
EXISTS  C:\Users\nc_ci\Documents\PlaydateSDK
absent  C:\Program Files\PlaydateSDK
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp>
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp> Get-ChildItem "$env:USERPROFILE\Documents\PlaydateSDK\bin" -ErrorAction SilentlyContinue |
>>   Select-Object Name

Name
----
crashpad_handler.exe
firmware_symbolizer.py
fmt.dll
gamecontrollerdb.txt
jpeg62.dll
jsoncpp.dll
libcrypto-3-x64.dll
libcurl.dll
libpng16.dll
libssl-3-x64.dll
pcre2-16.dll
pdc.exe
pdutil.exe
PlaydateSimulator.exe
SDL2.dll
sentry.dll
sqlite3.dll
symbols.db
WebView2Loader.dll
wxbase331u_net_vc_x64_custom.dll
wxbase331u_vc_x64_custom.dll
wxmsw331u_core_vc_x64_custom.dll
wxmsw331u_webview_vc_x64_custom.dll
z.dll


PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp>
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp> Get-ChildItem "$env:APPDATA" -Filter "*laydate*" -ErrorAction SilentlyContinue
PS C:\Users\nc_ci\GitProjects\NickSpaghetti\open-crank-mcp> Get-ChildItem "$env:LOCALAPPDATA" -Filter "*laydate*" -ErrorAction SilentlyContinue


    Directory: C:\Users\nc_ci\AppData\Local


Mode                 LastWriteTime         Length Name
----                 -------------         ------ ----
d-----         7/29/2026   5:55 PM                Playdate Simulator
```

the installer is an .exe

One question no script can answer: **is the Windows SDK an installer `.exe` or an
archive?** CI provisions the Linux SDK by fetching a `.tar.gz` and extracting it.
If Windows only ships an interactive installer, a Windows CI job cannot set
itself up, which is an argument for leaving Windows unsupported permanently
rather than temporarily.

## Sending results back

Paste raw output rather than a summary. The exact bytes of a config file and the
exact path of an executable are the parts that matter, and both are easy to
normalise by accident when retyping. An error message that names a path is just
as useful as a success.

## Where each part stands

| Part | State | Answers |
|---|---|---|
| macOS 1 | done | Install location, config format, bundle name, inner executable, `pdc`. All five matched what the code guessed. |
| macOS 2 | superseded | Nothing. Three bugs, replaced by part 3. |
| **macOS 3** | **outstanding** | The sandboxed data directory, and the Lua-stdout question part 2 failed to answer. |
| Windows | optional | Whether promoting Windows-native later would be cheap. Nothing is blocked on it. |

## What is already verified, and what is not

The Go side of this is not a draft. On a Linux machine:

| Check | State |
|---|---|
| `go build ./...`, `go vet` | Clean |
| `go test ./internal/sdk/` | Passing |
| macOS and Windows path logic | Exercised, on Linux, against a synthetic filesystem |
| `make go-build-cross` | Builds for linux, darwin and windows |
| Real macOS and Windows path *values* | **Unverified. That is what this document is for.** |

The layouts are ordinary values selected at runtime rather than build-tagged
files, specifically so all three can be tested from one machine. Two traps found
while doing that, both worth knowing before editing `internal/sdk`:

- Go applies an implicit build constraint to any file whose name ends in
  `_darwin.go`, `_windows.go` or `_linux.go`, with or without a `//go:build`
  line. That is why those files are named `macos_layout.go`,
  `windows_layout.go` and `linux_layout.go`. Renaming them back would silently
  stop the cross-platform tests from compiling the code they test.
- The bounded search in `datadir.go` has a depth limit and a node budget. Both
  are what make a wrong candidate list degrade into "slower" rather than "hangs
  while walking a home directory", so both have tests pinning them. Mutation
  testing is what pointed out that changing either limit originally broke no
  test at all.

## After this

The values get corrected in `internal/sdk`, the `fstest` suite gains cases built
from the real layout, and `docs/GOTCHAS.md` gets whatever part 2 found. Then a
`darwin/arm64` binary and an MCP client config, so the Simulator on that machine
can be driven by an agent for real. That is Checkpoint 11 in `docs/ROADMAP.md`.
