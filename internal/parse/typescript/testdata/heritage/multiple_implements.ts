// W-B W1 fixture — class extends parent + implements 3 interfaces (same-file).
// Expectations:
//   1 EdgeExtends:    Combined → Base
//   3 EdgeImplements: Combined → IAlpha / IBeta / IGamma  (source-order)
// All edges ConfExtracted.

interface IAlpha {
  alpha(): void
}

interface IBeta {
  beta(): void
}

interface IGamma {
  gamma(): void
}

class Base {
  base(): void {}
}

class Combined extends Base implements IAlpha, IBeta, IGamma {
  alpha(): void {}
  beta(): void {}
  gamma(): void {}
}
