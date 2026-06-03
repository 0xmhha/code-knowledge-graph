// Feature smoke test for #4 (Call Flow side panel).
//
// Run via: cd web/viewer-next && npx playwright test call-flow
// Requires `ckg serve` running at baseURL (default 127.0.0.1:8787).
//
// Asserts the boot-state behaviour: with no anchor set, the .call-flow
// element is absent (CallFlow returns null) and the grid does not have
// the .has-callflow class. The full lifecycle (anchor set → flow rows →
// click row triggers onPick) requires driving the zustand store from
// the page context, which lives in a richer integration suite. Here we
// stick to what's directly observable so the smoke job stays reliable.

import { test, expect } from '@playwright/test';
import { seedFirstTimeSeen } from './_lib.js';

test.beforeEach(async ({ page }) => {
  await seedFirstTimeSeen(page);
});

async function waitForBoot(page) {
  await page.goto('/');
  await expect(page.locator('.canvas-host canvas')).toBeVisible({ timeout: 30000 });
  // Wait for the boot commit to populate at least one node.
  await page.waitForFunction(() => {
    const m = document.querySelector('.bottombar')?.textContent?.match(/(\d+)\s*nodes/);
    return !!m && parseInt(m[1], 10) > 0;
  }, null, { timeout: 30000 });
}

test.describe('Feature #4 — CallFlow side panel', () => {
  test('callflow column absent on boot (no anchor)', async ({ page }) => {
    await waitForBoot(page);
    expect(await page.locator('.call-flow').count()).toBe(0);
    expect(await page.evaluate(
      () => document.querySelector('#app')?.classList.contains('has-callflow') ?? false,
    )).toBe(false);
  });
});
