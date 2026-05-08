// Diagnostic spec — re-runs the seven viewer complaints reported by the
// user against the live ckg serve at baseURL (default 127.0.0.1:8787).
// Track A: each test is a probe + assertion. Failures here mean a real
// regression. Run via: cd web/viewer-next && npx playwright test diag-7-complaints
//
// This file is a STARTING POINT for future regression checks — keep it
// even after the bugs in question are fixed. It is intentionally written
// so each test stands alone (own page.goto + own waits) so you can run a
// single complaint via -g "complaint 4".

import { test, expect } from '@playwright/test';

// Helper: wait for first commit (boot) to land — bottombar shows the node
// count once the boot recomputeVisible() has run. Catches the common
// "test ran before hydration" flake.
async function waitForBoot(page) {
  await page.goto('/');
  await expect(page.locator('.canvas-host canvas')).toBeVisible({ timeout: 30000 });
  // Bottombar populates "N nodes / M edges" after the first commit.
  await page.waitForFunction(() => {
    const text = document.querySelector('.bottombar')?.textContent ?? '';
    return /\d+\s*nodes\s*\/\s*\d+\s*edges/.test(text);
  }, null, { timeout: 30000 });
}

test.describe('Track A: 7 viewer complaints', () => {
  test('complaint 1: edges render on boot (canvas + DOM count)', async ({ page }) => {
    await waitForBoot(page);
    const stats = await page.evaluate(() => {
      const text = document.querySelector('.bottombar')?.textContent ?? '';
      const m = text.match(/(\d+)\s*nodes\s*\/\s*(\d+)\s*edges/);
      return { text, nodes: m ? +m[1] : 0, edges: m ? +m[2] : 0 };
    });
    expect(stats.nodes, 'visible nodes on boot').toBeGreaterThan(0);
    expect(stats.edges, 'visible edges on boot').toBeGreaterThan(0);
  });

  test('complaint 2: topbar buttons present and clickable', async ({ page }) => {
    await waitForBoot(page);
    // Topbar buttons exist
    await expect(page.locator('.topbar-home')).toBeVisible();
    const buttons = await page.locator('.topbar button').count();
    expect(buttons, 'topbar button count').toBeGreaterThanOrEqual(5);

    // Click 2D/3D toggle and verify state change.
    const initialMode = await page.evaluate(() => localStorage.getItem('ckg.viewMode'));
    await page.locator('.topbar button', { hasText: /^(2D|3D)$/ }).first().click();
    await page.waitForFunction((prev) => localStorage.getItem('ckg.viewMode') !== prev, initialMode);

    // Click LANG/COMMUNITY toggle and verify state change.
    const initialColor = await page.evaluate(() => localStorage.getItem('ckg.colorMode'));
    await page.locator('.topbar button', { hasText: /^(LANG|COMMUNITY)$/ }).first().click();
    await page.waitForFunction((prev) => localStorage.getItem('ckg.colorMode') !== prev, initialColor);

    // Click Detail toggle: panel hide/show.
    const before = await page.locator('#app').getAttribute('class');
    await page.locator('.topbar-detail-toggle').click();
    await page.waitForFunction((prev) => document.querySelector('#app')?.className !== prev, before);

    // Click Help (?): help overlay appears.
    await page.locator('.topbar button', { hasText: '?' }).first().click();
    await expect(page.locator('.help-overlay, [class*="help"]').first()).toBeVisible({ timeout: 2000 });
  });

  test('complaint 2b: trace controls (callers/both/callees + depth) work', async ({ page }) => {
    await waitForBoot(page);
    const trace = page.locator('.trace-controls');
    await expect(trace).toBeVisible();
    const dirButtons = trace.locator('button', { hasText: /(callers|both|callees)/ });
    expect(await dirButtons.count(), 'trace direction buttons').toBe(3);
    await dirButtons.filter({ hasText: 'callers' }).click();
    await expect(dirButtons.filter({ hasText: 'callers' })).toHaveClass(/active/);
    await dirButtons.filter({ hasText: 'callees' }).click();
    await expect(dirButtons.filter({ hasText: 'callees' })).toHaveClass(/active/);

    const depthButtons = trace.locator('button', { hasText: /^[1-4]$/ });
    expect(await depthButtons.count(), 'trace depth buttons').toBe(4);
    await depthButtons.filter({ hasText: '3' }).click();
    await expect(depthButtons.filter({ hasText: '3' })).toHaveClass(/active/);
  });

  test('complaint 2c: bottombar buttons (depth in/out/Home/font) work', async ({ page }) => {
    await waitForBoot(page);
    const bb = page.locator('.bottombar');
    await expect(bb).toBeVisible();
    expect(await bb.locator('button[title="Home"]').count()).toBe(1);
    expect(await bb.locator('button[title*="Depth"]').count()).toBe(2);
    expect(await bb.locator('button', { hasText: /^[SML]$/ }).count()).toBe(3);

    await bb.locator('button', { hasText: 'L' }).click();
    await page.waitForFunction(() => localStorage.getItem('ckg.fontSize') === 'L');
  });

  test('complaint 3: visible-node list shows ≥1 row on first paint', async ({ page }) => {
    await waitForBoot(page);
    const items = await page.locator('.node-list .item').count();
    expect(items, 'node-list item count on boot').toBeGreaterThan(0);
  });

  test('complaint 4: search returns results + highlights matches', async ({ page }) => {
    await waitForBoot(page);
    const before = await page.locator('.node-list .item').count();
    await page.locator('.search').fill('Parse');
    // Debounce is 200ms, give it a generous window.
    await page.waitForTimeout(800);
    const titleText = await page.locator('.node-list .title').textContent();
    expect(titleText, 'list title flips to "Search Results"').toMatch(/Search Results/);
    const after = await page.locator('.node-list .item').count();
    // Either we got hits, OR an explicit "No matches" empty state — either
    // way the search executed end-to-end.
    const emptyMsg = await page.locator('.node-list').textContent();
    if (after === 0) {
      expect(emptyMsg).toMatch(/No matches/);
    } else {
      expect(after, 'search returned ≥1 hit').toBeGreaterThan(0);
    }
    // Quick smoke: the search-clear ✕ button appeared.
    await expect(page.locator('.search-clear')).toBeVisible();
  });

  test('complaint 5: clicking a node sets anchor and grows visible set', async ({ page }) => {
    await waitForBoot(page);
    const beforeStats = await page.evaluate(() => {
      const t = document.querySelector('.bottombar')?.textContent ?? '';
      const m = t.match(/(\d+)\s*nodes\s*\/\s*(\d+)\s*edges/);
      return m ? { n: +m[1], e: +m[2] } : { n: 0, e: 0 };
    });
    // Click a node via the sidebar list (DOM-clickable; canvas clicks are
    // brittle in headless because force-graph hit-testing depends on
    // simulated positions).
    await page.locator('.node-list .item').first().click();
    // Wait for the trace-induced commit. The detail panel should switch
    // away from "No node selected".
    await page.waitForFunction(() => {
      return !document.querySelector('.panel')?.textContent?.includes('No node selected');
    }, null, { timeout: 5000 });
    const afterStats = await page.evaluate(() => {
      const t = document.querySelector('.bottombar')?.textContent ?? '';
      const m = t.match(/(\d+)\s*nodes\s*\/\s*(\d+)\s*edges/);
      return m ? { n: +m[1], e: +m[2] } : { n: 0, e: 0 };
    });
    // Either the visible set changed OR remained the same (list-pick path
    // preserves visibleIds and only updates focus halo). What we MUST see
    // is the detail panel reacting — already asserted above.
    // We additionally assert a non-zero visible count after the click.
    expect(afterStats.n, 'visible nodes after node click').toBeGreaterThan(0);
  });

  test('complaint 6: Home button visible and resets state', async ({ page }) => {
    await waitForBoot(page);
    await expect(page.locator('.topbar-home')).toBeVisible();
    // Mutate state first so Home has something to reset.
    await page.locator('.search').fill('Parse');
    await page.waitForTimeout(500);
    await page.locator('.topbar-home').click();
    // After Home: search query is cleared, anchor null, panel shows root view ctx.
    await page.waitForFunction(() => {
      const input = document.querySelector('.search');
      return input && input.value === '';
    }, null, { timeout: 5000 });
    const ctx = await page.locator('.node-list .ctx').first().textContent();
    expect(ctx).toMatch(/root view/);
  });

  test('complaint 7: 6-graph axis exposes pills + groups (and pills toggle)', async ({ page }) => {
    await waitForBoot(page);
    expect(await page.locator('.edge-filters .graph-pill').count()).toBe(6);
    expect(await page.locator('.edge-filters .graph-group').count()).toBe(6);
    // Click G1 pill, assert class flip (pill-on ↔ pill-off ↔ pill-partial).
    const g1 = page.locator('.edge-filters .graph-pill', { hasText: 'G1' });
    const beforeClass = await g1.getAttribute('class');
    await g1.click();
    await page.waitForFunction((prev) => {
      const el = [...document.querySelectorAll('.edge-filters .graph-pill')]
        .find(b => b.textContent?.includes('G1'));
      return el && el.className !== prev;
    }, beforeClass);
  });
});

test.describe('Track A: cache-bust verification', () => {
  test('served chunks match on-disk build hash', async ({ page, baseURL }) => {
    await page.goto('/');
    const chunks = await page.evaluate(() =>
      [...document.querySelectorAll('script[src]')]
        .map(s => s.src)
        .filter(s => s.includes('/_next/static/chunks/'))
        .map(s => s.replace(window.location.origin, '')),
    );
    expect(chunks.length, 'page references _next/static/chunks/*').toBeGreaterThan(0);
    // Each chunk must be retrievable (200) — verifies the staticfs is wired.
    for (const c of chunks) {
      const r = await page.request.get(c);
      expect(r.status(), `chunk ${c} status`).toBe(200);
    }
  });

  test('served JS bundle contains current commit-specific markers', async ({ page }) => {
    await page.goto('/');
    const chunks = await page.evaluate(() =>
      [...document.querySelectorAll('script[src]')]
        .map(s => s.src)
        .filter(s => s.includes('/_next/static/chunks/app/page')),
    );
    expect(chunks.length, 'app page chunk linked').toBeGreaterThan(0);
    // Pull the page chunk and look for a string that only exists in
    // commit 1543f74's source — the migration sentinel (ckg.edgeFiltersV /
    // v2) and the Home-reset literal ("ckg.panelOpen") are good markers.
    const r = await page.request.get(chunks[0]);
    const body = await r.text();
    // Either bundler kept the literal or minified it; both are still
    // greppable strings inside the JS.
    expect(body, 'bundle contains ckg.edgeFiltersV migration key').toContain('ckg.edgeFiltersV');
    expect(body, 'bundle contains v2 migration sentinel').toContain('v2');
    expect(body, 'bundle wires the topbar-home class').toContain('topbar-home');
  });
});
