// W-B W1 fixture — class extends class (same-file).
// Expectations: 1 EdgeExtends edge, Child → Parent, ConfExtracted.

class Parent {
  doWork(): void {}
}

class Child extends Parent {
  override doWork(): void {}
}
