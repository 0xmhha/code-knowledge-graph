// clients.ts — W2 fixture: TypeScript HTTP client call sites. Each call
// below emits an `http_calls` edge from the enclosing function to an
// AMBIGUOUS placeholder Endpoint with qname `http:METHOD /path`. The
// downstream link pass (internal/link/http_match.go) may rewire the edge
// to a real server-side Endpoint or leave the placeholder in place as an
// external-API marker.
//
// Expected placeholder Endpoint qnames (before link-pass cascade):
//
//   http:GET /api/users           (fetch default + axios.get + useSWR + useQuery — dedup to one node)
//   http:POST /api/users          (fetch with method:POST option)
//   http:PUT /api/users/:id       (axios.put)
//   http:DELETE /api/users/:id    (axios.delete)
//   http:* /api/list              (useSWR — method unknown → wildcard)
//   http:GET /external/foo        (absolute URL stripped to path)
//   http:* /api/query             (useQuery({ url }))
//   http:GET /api/users/all       (axios({ method:'GET', url }))

import axios from 'axios';
import useSWR from 'swr';
import { useQuery } from '@tanstack/react-query';

export async function fetchUsersDefault(): Promise<unknown> {
  const r = await fetch('/api/users');
  return r.json();
}

export async function fetchUsersPost(body: unknown): Promise<unknown> {
  const r = await fetch('/api/users', { method: 'POST', body: JSON.stringify(body) });
  return r.json();
}

export async function axiosGetUsers(): Promise<unknown> {
  const r = await axios.get('/api/users');
  return r.data;
}

export async function axiosPutUser(id: string): Promise<unknown> {
  const r = await axios.put('/api/users/:id', { id });
  return r.data;
}

export async function axiosDeleteUser(id: string): Promise<unknown> {
  const r = await axios.delete('/api/users/:id');
  return r.data;
}

export async function axiosObjectArg(): Promise<unknown> {
  const r = await axios({ method: 'GET', url: '/api/users/all' });
  return r.data;
}

export function useUserList(): unknown {
  // useSWR — method unknown, falls into wildcard cascade.
  return useSWR('/api/list', (u: string) => fetch(u).then(r => r.json()));
}

export function useFancyQuery(): unknown {
  return useQuery({ queryKey: ['users-q'], url: '/api/query' } as any);
}

export async function callExternalAPI(): Promise<unknown> {
  // Absolute URL — host stripped, path kept. No server in this fixture
  // listens on /external/foo so the placeholder should be retained.
  const r = await fetch('https://api.external.example.com/external/foo');
  return r.json();
}
