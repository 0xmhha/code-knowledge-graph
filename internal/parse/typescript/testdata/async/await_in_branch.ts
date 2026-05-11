// W-B W2 fixture — awaits nested inside control-flow blocks.
// Expectations:
//   - `branchy` SubKind="async"
//   - 3 AwaitPoint nodes:
//       await checkA()   inside `if (...)` body
//       await loopBody() inside `for (...)` body
//       await finalize() at top of function body
//   - All 3 EdgeAwaits anchor on `branchy` (not on the IfStmt/LoopStmt)
//     — V0 attaches AwaitPoint to the *Function* anchor, not the
//     enclosing block (spec §3.2: "enclosing function"). The IfStmt /
//     LoopStmt are still emitted by statements.go, but they don't own
//     the suspension edge.

async function checkA(): Promise<boolean> { return true }
async function loopBody(): Promise<void> {}
async function finalize(): Promise<string> { return "done" }

export async function branchy(): Promise<string> {
  await finalize()
  if (Math.random() > 0.5) {
    await checkA()
  }
  for (let i = 0; i < 3; i++) {
    await loopBody()
  }
  return "ok"
}
