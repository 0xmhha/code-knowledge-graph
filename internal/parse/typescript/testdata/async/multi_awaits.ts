// W-B W2 fixture — three distinct awaits in one async function (line-distinguished).
// Expectations:
//   - `pipeline` SubKind="async"
//   - 3 AwaitPoint nodes, each at a different startByte/line:
//       await stepA()  →  Name="await stepA"
//       await stepB(a) →  Name="await stepB"
//       await stepC(b) →  Name="await stepC"
//   - 3 EdgeAwaits  (pipeline → each AwaitPoint)
// Dedup is intentionally absent — each call site is a distinct suspension.

async function stepA(): Promise<number> { return 1 }
async function stepB(_a: number): Promise<number> { return 2 }
async function stepC(_b: number): Promise<number> { return 3 }

export async function pipeline(): Promise<number> {
  const a = await stepA()
  const b = await stepB(a)
  const c = await stepC(b)
  return c
}
