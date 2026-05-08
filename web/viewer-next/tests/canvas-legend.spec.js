// Feature smoke test for #3 (Canvas Legend overlay).
//
// Run via: cd web/viewer-next && npx playwright test canvas-legend
// Requires `ckg serve` running at baseURL (default 127.0.0.1:8787).

import { test, expect } from '@playwright/test';

async function waitForBoot(page) {
  await page.goto('/');
  await expect(page.locator('.canvas-host canvas')).toBeVisible({ timeout: 30000 });
  await page.waitForFunction(() => {
    const text = document.querySelector('.bottombar')?.textContent ?? '';
    return /\d+\s*nodes\s*\/\s*\d+\s*edges/.test(text);
  }, null, { timeout: 30000 });
}

test.describe('Feature #3 — CanvasLegend overlay', () => {
  test('legend overlay renders and toggle persists', async ({ page }) => {
    await waitForBoot(page);
    // Default open: both Node Shapes + Edge Styles sections render.
    const legend = page.locator('.canvas-legend');
    await expect(legend).toBeVisible();
    await expect(legend.locator('h5')).toHaveCount(2);
    // Close via the X button. Stored as '0' in localStorage.
    await page.locator('.canvas-legend-close').click();
    await expect(page.locator('.canvas-legend.collapsed')).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('ckg.canvasLegend.open'))).toBe('0');
    // Re-open via the title click.
    await page.locator('.canvas-legend-title').click();
    await expect(page.locator('.canvas-legend').first()).not.toHaveClass(/collapsed/);
    expect(await page.evaluate(() => localStorage.getItem('ckg.canvasLegend.open'))).toBe('1');
  });
});
