// Shared test fixtures for the viewer-next Playwright suite.
//
// seedFirstTimeSeen suppresses the FirstTimeOverlay welcome card on a
// fresh browser context. The overlay is shown when localStorage has no
// `ckg.firstTimeSeen` entry, which is always the case on a CI runner.
// Because the overlay is a modal dialog covering the whole viewport,
// any click-based assertion below would hit the backdrop instead of the
// intended target and time out. addInitScript runs before every page
// navigation in the context, so the seed lands before the first paint
// and the overlay is never rendered.

export async function seedFirstTimeSeen(page) {
  await page.addInitScript(() => {
    try {
      localStorage.setItem('ckg.firstTimeSeen', '1');
    } catch { /* localStorage may be blocked */ }
  });
}
