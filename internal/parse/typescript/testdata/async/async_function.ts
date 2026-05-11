// W-B W2 fixture — top-level async function.
// Expectations:
//   - `fetchUser` Function with SubKind="async"
//   - 2 NodeAwaitPoint emits inside `fetchUser` (await fetch + await res.json)
//   - 2 EdgeAwaits from fetchUser → AwaitPoint
//   - `sync` Function with SubKind="" (no async modifier)
//   - 0 AwaitPoint inside `sync`

export async function fetchUser(id: string): Promise<unknown> {
  const res = await fetch(`/users/${id}`)
  return await res.json()
}

export function sync(id: string): string {
  return `user-${id}`
}
