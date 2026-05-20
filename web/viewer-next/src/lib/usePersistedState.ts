'use client';

import { useCallback, useEffect, useState } from 'react';

// usePersistedState family: reads localStorage AFTER mount so SSR
// (build-time static export) and the client's first render produce
// identical HTML. The initial render always returns `def`; a useEffect
// then reads storage and flips state if the stored value differs.
//
// Eliminates React #418 (text/attribute hydration mismatch) for
// localStorage-backed UI state at the cost of a one-frame post-hydrate
// update. The flicker is imperceptible in practice for collapsed/open
// /width-style state — and far less disruptive than the console error
// noise + degraded React tree consistency mismatches cause.
//
// SSR safety: every accessor guards `typeof localStorage` so the
// build-time render path (Next `output: 'export'`) never throws.

export function usePersistedBool(
  key: string,
  def: boolean,
): [boolean, (b: boolean) => void] {
  const [val, setVal] = useState<boolean>(def);
  useEffect(() => {
    if (typeof localStorage === 'undefined') return;
    try {
      const raw = localStorage.getItem(key);
      if (raw != null) setVal(raw === '1' || raw === 'true');
    } catch { /* localStorage may be blocked */ }
  }, [key]);
  const set = useCallback((b: boolean) => {
    setVal(b);
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(key, b ? '1' : '0');
      }
    } catch { /* ignore */ }
  }, [key]);
  return [val, set];
}

export function usePersistedNumber(
  key: string,
  def: number,
): [number, (n: number) => void] {
  const [val, setVal] = useState<number>(def);
  useEffect(() => {
    if (typeof localStorage === 'undefined') return;
    try {
      const raw = localStorage.getItem(key);
      if (raw != null) {
        const n = parseInt(raw, 10);
        if (!Number.isNaN(n)) setVal(n);
      }
    } catch { /* ignore */ }
  }, [key]);
  const set = useCallback((n: number) => {
    setVal(n);
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(key, String(n));
      }
    } catch { /* ignore */ }
  }, [key]);
  return [val, set];
}

// usePersistedJSON parses + serialises arbitrary JSON. The `validate`
// callback gives callers a chance to reject parsed data that doesn't
// match the expected shape (e.g. non-array when expecting an array)
// — a corrupted stored payload then degrades to `def` rather than
// crashing the component.
export function usePersistedJSON<T>(
  key: string,
  def: T,
  validate?: (parsed: unknown) => parsed is T,
): [T, (v: T) => void] {
  const [val, setVal] = useState<T>(def);
  useEffect(() => {
    if (typeof localStorage === 'undefined') return;
    try {
      const raw = localStorage.getItem(key);
      if (raw == null) return;
      const parsed = JSON.parse(raw);
      if (!validate || validate(parsed)) {
        setVal(parsed as T);
      }
    } catch { /* ignore */ }
    // validate is intentionally omitted: this effect is a one-shot localStorage
    // hydrate keyed off `key`, not a re-validation pipeline. Including validate
    // would re-hydrate on every render whose parent inlines the predicate.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  const set = useCallback((v: T) => {
    setVal(v);
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(key, JSON.stringify(v));
      }
    } catch { /* ignore */ }
  }, [key]);
  return [val, set];
}
