// Identifies whether a file_path belongs to test code across the
// languages we currently parse (Go, TypeScript/JavaScript, Solidity).
// Centralised so backend response filtering and any future server-side
// query parameter agree on the same patterns.
//
// Patterns intentionally lean broad ("contains /test/ anywhere") so a
// repo's convention variations (pkg/foo/test_helpers.go, frontend/tests/,
// contracts/test/) all collapse to the same axis. Files outside these
// patterns are treated as production code.
const TEST_PATTERNS: ReadonlyArray<RegExp> = [
  /_test\.go$/i,            // Go (canonical)
  /(?:^|\/)tests?\//,       // any /test/ or /tests/ segment
  /\.test\.[tj]sx?$/i,      // *.test.ts / *.test.tsx / *.test.js / *.test.jsx
  /\.spec\.[tj]sx?$/i,      // *.spec.ts / *.spec.tsx / *.spec.js / *.spec.jsx
  /(?:^|\/)__tests__\//,    // Jest convention
  /\.t\.sol$/i,             // Foundry contract tests
  /(?:^|\/)mock(?:s)?\//i,  // mock/mocks directories sometimes counted with tests
];

export function isTestPath(filePath: string | null | undefined): boolean {
  if (!filePath) return false;
  return TEST_PATTERNS.some(p => p.test(filePath));
}
