import { test, expect } from '@playwright/test';
import { seedFirstTimeSeen } from './_lib.js';

// Smoke test: verifies the viewer-next bundle mounts against a real
// `ckg serve` + graph.db. Intentionally does NOT assert node count
// or content — only chrome render, canvas mount, and manifest fetch.
test.beforeEach(async ({ page }) => {
  await seedFirstTimeSeen(page);
});

test('viewer loads and shows package nodes', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.topbar strong')).toHaveText('ckg');
  // Wait for force-graph to mount (canvas appears).
  await expect(page.locator('.canvas-host canvas')).toBeVisible({ timeout: 30000 });
  // src-info populated → manifest fetched.
  await page.waitForFunction(
    () => (document.querySelector('.topbar .src-info')?.textContent ?? '') !== ''
  );
});
