# Running in native mode

The server runs directly against a Playdate SDK you installed yourself. No
container.

The Simulator is an ordinary window on your desktop, with your audio, and no
bind mounts or root-owned build output. See
[container-mode.md](container-mode.md) for the other mode.

No `make up`. There is no container to start: an MCP client launches the server
binary directly, and the server launches the Simulator.

```
make go-build     # produces ./open-crank-mcp
make sdk-path     # confirm it finds your SDK
```

Then point a client at the binary, as below. The Simulator appears as an ordinary
window when a game launches, using your display and your audio, so none of the
container display profiles above apply.

Two checks worth running once, if you want to know the environment is sound
before involving an agent:

```
make smoke-check-native        # libraries resolve, pdc runs, the Simulator starts
make sdk-contract-check-native # the MCP tools driving a real Simulator
```
