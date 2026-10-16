import { defineConfig } from '@playwright/test';

// Expects the container started by shared-check.sh --keep, so no webServer block.
export default defineConfig({
  testDir: '.',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: process.env.SHARED_URL ?? 'http://localhost:6080',
    // Headless Chromium otherwise blocks playback until a real user gesture.
    launchOptions: {
      args: [
        '--autoplay-policy=no-user-gesture-required',
        '--use-fake-device-for-media-stream',
      ],
    },
  },
});
