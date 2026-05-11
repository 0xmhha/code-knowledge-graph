// W-B W1 fixture — interface extends (single-parent + multi-parent).
// Expectations: 3 EdgeExtends total, ConfExtracted.
//   IChild → IBase                  (single-parent)
//   IUnion → IFoo, IBar             (multi-parent, source-order)

interface IBase {
  id(): string
}

interface IFoo {
  foo(): void
}

interface IBar {
  bar(): void
}

interface IChild extends IBase {
  child(): void
}

interface IUnion extends IFoo, IBar {
  union(): void
}
