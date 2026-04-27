'use client';

import { useEffect, useRef, useState } from 'react';
import { useStore } from '@/store/store';
import type { IAPI } from '@/lib/api';

interface Props { api: IAPI; }

export default function SearchBox({ api }: Props) {
  const [q, setQ] = useState('');
  const setSearchResults = useStore(s => s.setSearchResults);
  const loadNodes = useStore(s => s.loadNodes);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handler = (ev: KeyboardEvent) => {
      if (ev.key === '/' && document.activeElement?.tagName !== 'INPUT') {
        ev.preventDefault();
        inputRef.current?.focus();
      }
      if (ev.key === 'Escape' && document.activeElement === inputRef.current) {
        inputRef.current?.blur();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  useEffect(() => {
    if (!q.trim()) {
      setSearchResults([]);
      return;
    }
    const t = setTimeout(async () => {
      try {
        const results = await api.search(q.trim());
        if (results.length) {
          loadNodes(results);
          setSearchResults(results);
        } else {
          setSearchResults([]);
        }
      } catch (e) {
        console.error('search failed', e);
        setSearchResults([]);
      }
    }, 200);
    return () => clearTimeout(t);
  }, [q, api, setSearchResults, loadNodes]);

  return (
    <input
      ref={inputRef}
      className="search"
      type="text"
      placeholder="search… (/) "
      value={q}
      onChange={e => setQ(e.target.value)}
      autoComplete="off"
    />
  );
}
