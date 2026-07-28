// Browser behaviour for the byos VNC pages. These cover the parts that only
// exist in the browser, so nothing else can check them: the scaling redirect,
// whether the audio player is in the way, and the mapping from a click on the
// Playdate's volume slider to the player's volume.
//
// Needs a running byos container with a launched Simulator:
//   bash scripts/byos-check.sh --keep
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
    // invisible so it isn't covering the workspace.
    await page.goto('/vnc.html?resize=scale&autoconnect=true');
    expect((await requireAudioState(page)).hidden).toBe(true);
  });

  test('the audio-only page shows its player', async ({ page }) => {
    await page.goto('/audio.html');
    expect((await requireAudioState(page)).hidden).toBe(false);
  });
});

test.describe('the Playdate volume slider drives the player', () => {
  test('clicking anywhere else leaves the audio alone', async ({ page, request }) => {
    // The regression this exists for: playback used to start on any click, so
    // aiming a crank or pressing a d-pad made noise.
    const layout = await requireLayout(request);
    await openWorkspace(page);
    expect((await requireAudioState(page)).paused).toBe(true);

    // Well clear of the slider: the middle of the Playdate's screen.
    const point = await pagePoint(page, layout.window.x + 100, layout.window.y + 150);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(600);

    expect((await requireAudioState(page)).paused).toBe(true);
  });

  test('clicking near the top of the trough sets a high volume and plays', async ({ page, request }) => {
    const layout = await requireLayout(request);
    await openWorkspace(page);
    const point = await pagePoint(page, layout.troughX, layout.troughTop + 4);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(600);

    const state = await requireAudioState(page);
    expect(state.muted).toBe(false);
    expect(state.paused).toBe(false);
    // Near the top of the trough is near the top of the range. Asserted as a
    // band rather than a value, since the click lands a few pixels in.
    expect(state.volume).toBeGreaterThan(0.85);
  });

  test('clicking near the bottom of the trough sets a low volume', async ({ page, request }) => {
    const layout = await requireLayout(request);
    await openWorkspace(page);
    const point = await pagePoint(page, layout.troughX, layout.troughBottom - 4);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(600);

    expect((await requireAudioState(page)).volume).toBeLessThan(0.15);
  });

  test('the middle of the trough is roughly half volume', async ({ page, request }) => {
    // Proves the mapping is proportional rather than just on or off.
    const layout = await requireLayout(request);
    await openWorkspace(page);
    const middle = Math.round((layout.troughTop + layout.troughBottom) / 2);
    const point = await pagePoint(page, layout.troughX, middle);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(600);

    const state = await requireAudioState(page);
    expect(state.volume).toBeGreaterThan(0.35);
    expect(state.volume).toBeLessThan(0.65);
  });

  test('the very first click is not dropped', async ({ page, request }) => {
    // The page fetches the slider position asynchronously. It used to ignore
    // any click that arrived before that fetch came back, so the first touch of
    // the slider did nothing at all.
    const layout = await requireLayout(request);
    await page.goto('/vnc.html?resize=scale&autoconnect=true');
    await page.waitForFunction(() => {
      const canvas = document.querySelector<HTMLCanvasElement>('#noVNC_canvas, canvas');
      return canvas !== null && canvas.width > 0;
    });

    // No settling pause here, deliberately: that pause is what used to hide
    // this bug.
    const point = await pagePoint(page, layout.troughX, layout.troughTop + 4);
    await page.mouse.click(point.x, point.y);

    await expect
      .poll(async () => (await requireAudioState(page)).volume, { timeout: 5_000 })
      .toBeGreaterThan(0.85);
  });

  test('the mute icon toggles mute', async ({ page, request }) => {
    const layout = await requireLayout(request);
    await openWorkspace(page);
    const trough = await pagePoint(page, layout.troughX, layout.troughTop + 4);
    await page.mouse.click(trough.x, trough.y);
    await page.waitForTimeout(400);
    expect((await requireAudioState(page)).muted).toBe(false);

    const mute = await pagePoint(page, layout.troughX, layout.muteY);
    await page.mouse.click(mute.x, mute.y);
    await page.waitForTimeout(400);
    expect((await requireAudioState(page)).muted).toBe(true);
  });
});
