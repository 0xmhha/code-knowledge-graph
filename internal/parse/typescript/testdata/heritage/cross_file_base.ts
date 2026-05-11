// W-B W1 fixture — cross-file parent declaration.
// Paired with cross_file_child.ts. The resolver must produce a
// CrossChild → CrossBase EdgeExtends with ConfInferred (cross-file).

export class CrossBase {
  hello(): string {
    return "hi"
  }
}
