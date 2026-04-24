import { test, expect } from '@playwright/test';

// Smoke test: verifies the viewer mounts against a real ckg serve + graph.db.
// Intentionally does NOT assert node count or content — only that the chrome
// renders, the canvas appears, and the manifest fetch populated #src-info.
test('viewer loads and shows package nodes', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#topbar strong')).toHaveText('ckg viewer');
  // Wait for force-graph to mount (canvas appears).
  await expect(page.locator('#canvas canvas')).toBeVisible({ timeout: 30000 });
  // src-info populated → manifest fetched.
  await page.waitForFunction(() => document.getElementById('src-info').textContent !== '');
});
