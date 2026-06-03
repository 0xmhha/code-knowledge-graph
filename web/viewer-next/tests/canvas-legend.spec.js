// Feature smoke test for #3 (Canvas Legend overlay).
//
// Run via: cd web/viewer-next && npx playwright test canvas-legend
// Requires `ckg serve` running at baseURL (default 127.0.0.1:8787).

import { test, expect } from '@playwright/test';
import { seedFirstTimeSeen } from './_lib.js';

test.beforeEach(async ({ page }) => {
  await seedFirstTimeSeen(page);
});

async function waitForBoot(page) {
  await page.goto('/');
  await expect(page.locator('.canvas-host canvas')).toBeVisible({ timeout: 30000 });
  // Wait for the boot commit to populate nodes — the empty "0 nodes /
  // 0 edges" string also matches \d+, so a permissive regex would pass
  // before the API fetch returns and the assertion below would race.
  await page.waitForFunction(() => {
    const m = document.querySelector('.bottombar')?.textContent?.match(/(\d+)\s*nodes/);
    return !!m && parseInt(m[1], 10) > 0;
  }, null, { timeout: 30000 });
}

test.describe('Feature #3 — CanvasLegend overlay', () => {
  test('legend overlay renders and toggle persists', async ({ page }) => {
    await waitForBoot(page);
    // Default open: both Node Shapes + Edge Styles sections render.
    const legend = page.locator('.canvas-legend');
    await expect(legend).toBeVisible();
    await expect(legend.locator('h5')).toHaveCount(2);
    // Close via the X button. Closing unmounts the panel (CanvasLegend
    // returns the .canvas-legend-trigger button instead). Stored as
    // '0' in localStorage.
    await page.locator('.canvas-legend-close').click();
    await expect(page.locator('.canvas-legend')).toHaveCount(0);
    await expect(page.locator('.canvas-legend-trigger')).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('ckg.canvasLegend.open'))).toBe('0');
    // Re-open via the trigger button.
    await page.locator('.canvas-legend-trigger').click();
    await expect(page.locator('.canvas-legend')).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('ckg.canvasLegend.open'))).toBe('1');
  });
});
