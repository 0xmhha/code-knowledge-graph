// Minimal Playwright config for the ckg viewer smoke test.
// CI (T37) reuses this without modification.
export default {
  testDir: './tests',
  timeout: 60000,
  use: {
    baseURL: 'http://127.0.0.1:8787',
    headless: true,
  },
};
