// Browser behaviour for the shared VNC pages. These cover the parts that only
// exist in the browser, so nothing else can check them: the scaling redirect,
// whether the audio player is in the way, and the mapping from a click on the
// Playdate's volume slider to the player's volume.
//
// Needs a running shared container with a launched Simulator:
//   bash scripts/shared-check.sh --keep
import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// What the container publishes to pd-layout.json, in framebuffer pixels. It
// republishes on every Simulator launch, and writes {} when there's no window,
// which is why every field is optional here.
interface Layout {
  window?: { x: number; y: number; w: number; h: number };
  troughX?: number;
  troughTop?: number;
  troughBottom?: number;
  muteY?: number;
}

// Narrowed once, so the tests aren't threading non-null assertions through
// every coordinate.
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

// Skips rather than fails when no Simulator is running: the workspace tests are
// still meaningful without one, and a missing Simulator is a setup problem
// rather than a regression in the page.
async function requireLayout(request: APIRequestContext): Promise<KnownLayout> {
  const layout = await readLayout(request);
  test.skip(
    layout.troughX === undefined || layout.window === undefined,
    'no Simulator running, so no slider to click',
  );
  return layout as KnownLayout;
}

// Converts a framebuffer pixel to a page coordinate, inverting what the page
// itself does. noVNC scales the canvas with CSS, so the two only agree after
// accounting for that ratio.
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
    // Not just "audio": noVNC's own page ships an <audio id="noVNC_bell">, and
    // an unqualified query finds that one, whose default volume of 1 looks
    // convincingly like a working player.
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
    // noVNC ships with scaling off, which renders the display 1:1 and gives
    // scrollbars instead of a display that fits the tab.
    await page.goto('/vnc.html');
    await expect(page).toHaveURL(/resize=scale/);
    await expect(page).toHaveURL(/autoconnect=true/);
  });

  test('the short URL lands on the same place', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/vnc\.html\?.*resize=scale/);
  });

  test('the audio player exists but is out of the way', async ({ page }) => {
    // The element has to be in the document to play at all, and it has to be
    // invisible so it isn't covering the workspace. The stream is stubbed
    // because the player deliberately reveals itself when the stream fails, and
    // the container's encoder serves only one listener.
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
  // The container reads the slider off the framebuffer and publishes it, so the
  // page's whole job is to follow that number. These serve it directly rather
  // than moving a real slider: a browser can't reach into the Simulator, and the
  // container side is covered by shared-check.
  async function serveVolume(page: Page, volume: () => number): Promise<void> {
    await page.route('**/pd-volume.json', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ volume: volume() }),
      });
    });

    // The stream is served locally too, from a silent fixture. The container's
    // encoder hands out one listener at a time, so a browser tab someone left
    // open would otherwise make these fail for a reason that has nothing to do
    // with the logic under test. The real stream is covered by shared-check.
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

    // A gesture, because browsers refuse to start audio without one. It is not
    // a request for sound: the slider is.
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
    // -1 is the container saying the scan failed. Acting on it would silence
    // working audio, and a failed read looks exactly like silence.
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
    // The regression that started all of this: playback used to begin on any
    // click, so aiming a crank made noise. A gesture only unlocks the browser.
    const layout = await requireLayout(request);
    await serveVolume(page, () => 0);
    await openWorkspace(page);

    const point = await pagePoint(page, layout.window.x + 100, layout.window.y + 150);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(1500);

    expect((await requireAudioState(page)).paused).toBe(true);
  });
});
