// ESLint flat config (ESLint v9+).
//
// Scope: enforce react-hooks/rules-of-hooks across all viewer source.
// The viewer hit React error #310 once because a useCallback landed
// below an early return (commit 50ee9f7); this config closes that
// class of regression — every PR that introduces a hooks-rule
// violation now fails CI before reaching the user.
//
// We deliberately do NOT extend eslint-config-next because its v15
// release ships a config object with circular references that ESLint 9
// flat-config can't serialise (FlatCompat fails too). The project
// already runs `next build` and `tsc --noEmit` for the syntax/type
// surface; dropping eslint-config-next gives up Next.js's a11y +
// jsx-no-target rules but those weren't enforced before this commit
// either. If we want them back, eslint-config-next v17 (planned
// flat-native) is the right time to revisit.
import js from '@eslint/js';
import tseslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';

export default [
  // Ignore patterns must come first in flat config — placing them
  // anywhere else makes ESLint scan the artefacts before throwing.
  {
    // Playwright tests run in the Playwright runner with browser globals
    // (document, window) — separate scope from the React app. Linting them
    // here would require a second config block with browser globals just
    // for this directory; not worth the complexity for two smoke tests.
    ignores: ['.next/**', 'out/**', 'node_modules/**', 'next-env.d.ts', 'tests/**'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    plugins: { 'react-hooks': reactHooks },
    rules: {
      // Hard error: no exceptions. The cost of a hooks-rule violation
      // is a user-facing crash with a minified React error code.
      'react-hooks/rules-of-hooks': 'error',
      // Warn-only: missing-deps reports are useful, but several of our
      // intentional patterns (event handlers reading useStore.getState()
      // synchronously rather than subscribing) would auto-fix into bugs.
      'react-hooks/exhaustive-deps': 'warn',
    },
  },
  {
    // Project-specific carve-outs. The `any` rule is too noisy for the
    // existing codebase (force-graph types use unknown extensively); we
    // keep typescript-eslint's other recommended rules and turn off only
    // the ones that would require a refactor outside this commit's
    // scope. Revisit each in a typed-strictness pass.
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
      'no-empty': ['warn', { allowEmptyCatch: true }],
    },
  },
];
