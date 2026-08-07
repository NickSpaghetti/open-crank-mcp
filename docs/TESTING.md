# Testing

Why the test suite is shaped the way it is.

What each command does is in the readme's
[Tests and checks](../README.md#tests-and-checks) table. This is the reasoning
behind them.

The container tests run in their own compose project, on ports 6180 and 8100,
with their own data directory. That isolation matters more than it sounds: the
suite needs a known game mounted, so without it a test run unmounts the game you
were playing and then fights your browser tab for the single-listener audio
stream. Isolated, you can run the whole suite mid-game and nothing moves.

The shared-session tests exist because that layer is easy to break invisibly. Each of
these asserts something that has actually broken in practice:

- Every page returns 200. A partial edit once deleted two of them, and a
  missing page is invisible until someone opens that exact URL.
- The patched openbox config keeps every mouse binding the shipped one has.
  openbox doesn't merge a partial config with its defaults, so a hand-written
  `rc.xml` silently dropped all 59 of them, taking window dragging with it.
- No keybinding can hide or close the Simulator, since only an MCP client can
  start one.
- The volume slider is found by relationships, not pixel values: inside the
  window, running downwards a plausible distance, mute icon below it. The exact
  numbers move with zoom and theme.
- Clicking the slider changes the framebuffer, which covers the whole input
  path: X, GTK's click-to-warp, and the coordinates being right.
- Audio playback starts only from the slider, never from a click elsewhere.

The browser tests are TypeScript, run by Playwright's own runner, which
transpiles them directly. `tsgo` typechecks them separately and emits nothing,
so there's no build step in front of the tests.

Node is the runtime, inside the Playwright container. Playwright's test runner
requires it: Bun and Deno can drive `playwright-core`, but not this runner.
`PLAYWRIGHT_IMAGE` in the `Makefile` has to stay on the same version as
`@playwright/test` in `tests/browser/package.json`, since the image carries the
browsers and the package drives them.

Two things are deliberately not tested. Game audio end to end needs a
sound-producing fixture and level thresholds, which is a flake factory, so the
audio chain is checked with a synthetic tone into the sink instead. And nothing
compares screenshots: the game animates, so image diffing would fail for
reasons unrelated to the code.
