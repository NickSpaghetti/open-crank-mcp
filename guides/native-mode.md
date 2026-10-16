# Running in native mode

The server runs directly against a Playdate SDK you installed yourself. No
container.

The Simulator is an ordinary window on your desktop, with your audio, and no
bind mounts or root-owned build output. See
[container-mode.md](container-mode.md) for the other mode.

There is no `make up`. An MCP client launches the server binary directly, and
the server launches the Simulator.

Use a binary from the
[releases page](https://github.com/NickSpaghetti/open-crank-mcp/releases), or
build one:

```
make go-build     # produces ./open-crank-mcp (open-crank-mcp.exe on Windows)
make sdk-path     # confirm it finds your SDK
```

Then point a client at the binary: see [connecting.md](connecting.md). None of
the container display profiles in [container-mode.md](container-mode.md) apply.

To check the environment before involving an agent:

```
make smoke-check-native        # libraries resolve, pdc runs, the Simulator starts
make sdk-contract-check-native # the MCP tools driving a real Simulator
```

On Windows, run the PowerShell script instead:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-windows-native.ps1
```
