// W-B W1 fixture — cross-file child. Parent lives in cross_file_base.ts.
// Imported by qualified path; the heritage resolver uses bare name matching
// (V0 doesn't model qualified imports — same idiom as the calls resolver).
// Expectation: CrossChild → CrossBase EdgeExtends, ConfInferred.

import { CrossBase } from "./cross_file_base"

export class CrossChild extends CrossBase {
  override hello(): string {
    return "hi from child"
  }
}
