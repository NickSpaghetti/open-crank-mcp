# Probing a platform for native mode

Native mode runs the server directly against an SDK the developer installed,
with no container. Where the SDK lives, and where things sit inside it, differs
per platform. Linux is the only layout verified by running it. The macOS and
Windows values in `internal/sdk` were written from Panic's documentation and
platform convention, never checked against a real install.

This is the checklist for checking one. Everything here is read-only except one
optional step that builds a fixture into `/tmp`.

Run it on a machine with the Playdate SDK installed, then paste the output back.

## Do not run the server binary yet

The point of this document is to collect facts about the machine, not to test
the implementation. The values it corrects are the ones that decide whether the
implementation can work at all, so they come first. Once they are right, a
cross-compiled binary is easy to hand over and there is something worth running.

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

## macOS, part 1: the layout

Read-only. Paste into Terminal.

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

## macOS, part 2: does Lua print() reach stdout

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

One question no script can answer: **is the Windows SDK an installer `.exe` or an
archive?** CI provisions the Linux SDK by fetching a `.tar.gz` and extracting it.
If Windows only ships an interactive installer, a Windows CI job cannot set
itself up, which is an argument for leaving Windows unsupported permanently
rather than temporarily.

## Sending results back

Paste the contents of `~/playdate-macos-report.txt` plus the output of part 2.
Raw output is better than a summary: the exact bytes of the config file and the
exact bundle path are the parts that matter, and both are easy to normalise by
accident when retyping.

## After this

The values get corrected in `internal/sdk`, the `fstest` suite gets cases built
from the real layout, and `docs/GOTCHAS.md` gets whatever part 2 found. Then a
`darwin/arm64` binary and an MCP client config, so the Simulator on that machine
can be driven by an agent for real. That is Checkpoint 11 in `docs/ROADMAP.md`.
