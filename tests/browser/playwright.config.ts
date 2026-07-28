import { defineConfig } from '@playwright/test';

// The container is expected to be up already, started by byos-check.sh --keep,
// so there's no webServer block here. BYOS_URL exists for the case where the
// container is reached somewhere other than localhost.
export default defineConfig({
  testDir: '.',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  // Two browsers fighting over a single-listener audio stream would be a
  // pointless source of flakes.
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: process.env.BYOS_URL ?? 'http://localhost:6080',
    // Without this the audio element can never start, since headless Chromium
    // blocks playback until a real user gesture. The click mapping is what's
    // under test, not the browser's autoplay policy.
    launchOptions: {
      args: [
        '--autoplay-policy=no-user-gesture-required',
        '--use-fake-device-for-media-stream',
      ],
    },
  },
});
