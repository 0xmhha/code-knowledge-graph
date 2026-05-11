// W-B W2 fixture — no async modifiers, no awaits.
// Expectations:
//   - Both Functions have SubKind="" (empty).
//   - 0 AwaitPoint, 0 EdgeAwaits.
// Negative control: verifies the detector doesn't false-positive on
// regular synchronous code (no "async" keyword anywhere in the source).

export function add(a: number, b: number): number {
  return a + b
}

export function mul(a: number, b: number): number {
  return a * b
}
