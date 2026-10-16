// Browser behaviour for the shared VNC pages: the scaling redirect, the hidden
// player, and the player following the published slider volume. Needs:
//   bash scripts/shared-check.sh --keep
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// pd-layout.json, in framebuffer pixels. {} when there's no Simulator window.
interface Layout {
  window?: { x: number; y: number; w: number; h: number };
  troughX?: number;
  troughTop?: number;
  troughBottom?: number;
  muteY?: number;
}

interface KnownLayout {
  window: { x: number; y: number; w: number; h: number };
  troughX: number;
  troughTop: number;
  troughBottom: number;
  muteY: number;
}

interface Point {
  x: number;
  y: number;
}

interface AudioState {
  paused: boolean;
  muted: boolean;
  volume: number;
  hidden: boolean;
}

async function readLayout(request: APIRequestContext): Promise<Layout> {
  const response = await request.get('/pd-layout.json');
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as Layout;
}

// Skips rather than fails with no Simulator running: that's setup, not a regression.
async function requireLayout(request: APIRequestContext): Promise<KnownLayout> {
  const layout = await readLayout(request);
  test.skip(
    layout.troughX === undefined || layout.window === undefined,
    'no Simulator running, so no slider to click',
  );
  return layout as KnownLayout;
}

// Framebuffer pixel to page coordinate; noVNC scales the canvas with CSS.
async function pagePoint(page: Page, x: number, y: number): Promise<Point> {
  return page.evaluate(
    ([fx, fy]): Point => {
      const canvas = document.querySelector<HTMLCanvasElement>('#noVNC_canvas, canvas');
      if (!canvas) throw new Error('no canvas on the page');
      const rect = canvas.getBoundingClientRect();
      return {
        x: rect.left + fx * (rect.width / canvas.width),
        y: rect.top + fy * (rect.height / canvas.height),
      };
    },
    [x, y] as const,
  );
}

async function audioState(page: Page): Promise<AudioState | null> {
  return page.evaluate((): AudioState | null => {
    // noVNC ships an <audio id="noVNC_bell"> that a bare "audio" query would find.
    const el = document.querySelector<HTMLAudioElement>('#pd-audio');
    if (!el) return null;
    const container = el.parentElement;
    return {
      paused: el.paused,
      muted: el.muted,
      volume: el.volume,
      hidden: container ? getComputedStyle(container).display === 'none' : false,
    };
  });
}

async function requireAudioState(page: Page): Promise<AudioState> {
  const state = await audioState(page);
  expect(state, 'the page should have built its audio player').not.toBeNull();
  return state as AudioState;
}

async function openWorkspace(page: Page): Promise<void> {
  await page.goto('/vnc.html?resize=scale&autoconnect=true');
  // The canvas needs a framebuffer before its size means anything.
  await page.waitForFunction(() => {
    const canvas = document.querySelector<HTMLCanvasElement>('#noVNC_canvas, canvas');
    return canvas !== null && canvas.width > 0;
  });
}

test.describe('the workspace page', () => {
  test('a bare vnc.html redirects itself to add scaling', async ({ page }) => {
    await page.goto('/vnc.html');
    await expect(page).toHaveURL(/resize=scale/);
    await expect(page).toHaveURL(/autoconnect=true/);
  });

  test('the short URL lands on the same place', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/vnc\.html\?.*resize=scale/);
  });

  test('the audio player exists but is out of the way', async ({ page }) => {
    // Stubbed, because the player reveals itself when the stream fails.
    await page.route('**/stream.mp3', async (route) => {
      await route.fulfill({ status: 200, contentType: 'audio/mpeg', path: 'fixtures/silence.mp3' });
    });
    await page.goto('/vnc.html?resize=scale&autoconnect=true');
    expect((await requireAudioState(page)).hidden).toBe(true);
  });

  test('the audio-only page shows its player', async ({ page }) => {
    await page.goto('/audio.html');
    expect((await requireAudioState(page)).hidden).toBe(false);
  });
});

test.describe('the player follows the Playdate volume slider', () => {
  // Serves the published volume directly; shared-check covers the container side.
  async function serveVolume(page: Page, volume: () => number): Promise<void> {
    await page.route('**/pd-volume.json', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ volume: volume() }),
      });
    });

    // A silent fixture, so these don't depend on the real stream.
    await page.route('**/stream.mp3', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'audio/mpeg',
        path: 'fixtures/silence.mp3',
      });
    });
  }

  test('a slider at zero leaves the audio paused', async ({ page }) => {
    await serveVolume(page, () => 0);
    await openWorkspace(page);
    await page.waitForTimeout(1500);
    expect((await requireAudioState(page)).paused).toBe(true);
  });

  test('raising the slider starts playback at that level', async ({ page }) => {
    let level = 0;
    await serveVolume(page, () => level);
    await openWorkspace(page);
    await page.waitForTimeout(1200);
    expect((await requireAudioState(page)).paused).toBe(true);

    // Browsers refuse to start audio without a gesture.
    await page.mouse.click(5, 5);
    level = 0.8;

    await expect
      .poll(async () => (await requireAudioState(page)).paused, { timeout: 8_000 })
      .toBe(false);
    expect((await requireAudioState(page)).volume).toBeCloseTo(0.8, 1);
  });

  test('lowering it to zero stops playback again', async ({ page }) => {
    let level = 0.9;
    await serveVolume(page, () => level);
    await openWorkspace(page);
    await page.mouse.click(5, 5);
    await expect
      .poll(async () => (await requireAudioState(page)).paused, { timeout: 8_000 })
      .toBe(false);

    level = 0;
    await expect
      .poll(async () => (await requireAudioState(page)).paused, { timeout: 8_000 })
      .toBe(true);
  });

  test('a mid-track slider tracks proportionally', async ({ page }) => {
    let level = 0.5;
    await serveVolume(page, () => level);
    await openWorkspace(page);
    await page.mouse.click(5, 5);
    await expect
      .poll(async () => (await requireAudioState(page)).volume, { timeout: 8_000 })
      .toBeCloseTo(0.5, 1);

    level = 0.25;
    await expect
      .poll(async () => (await requireAudioState(page)).volume, { timeout: 8_000 })
      .toBeCloseTo(0.25, 1);
  });

  test('an unreadable slider leaves the audio alone', async ({ page }) => {
    // -1 is a failed scan, which must not silence working audio.
    let level = 0.7;
    await serveVolume(page, () => level);
    await openWorkspace(page);
    await page.mouse.click(5, 5);
    await expect
      .poll(async () => (await requireAudioState(page)).paused, { timeout: 8_000 })
      .toBe(false);

    level = -1;
    await page.waitForTimeout(2500);
    const state = await requireAudioState(page);
    expect(state.paused).toBe(false);
    expect(state.volume).toBeCloseTo(0.7, 1);
  });

  test('clicking the game does not start audio on its own', async ({ page, request }) => {
    // A gesture only unlocks the browser; it isn't a request for sound.
    const layout = await requireLayout(request);
    await serveVolume(page, () => 0);
    await openWorkspace(page);

    const point = await pagePoint(page, layout.window.x + 100, layout.window.y + 150);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(1500);

    expect((await requireAudioState(page)).paused).toBe(true);
  });
});
