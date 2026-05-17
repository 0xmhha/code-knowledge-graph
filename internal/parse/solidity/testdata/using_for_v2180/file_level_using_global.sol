// W-C W6 V2.18 fixture — file-level using directive with `global`
// qualifier (Sol 0.8.13+ at source_file scope, V2.16 row 1 closure).
//
// Grammar-block evidence (V2.18 AST probe 2026-05-17): vendored
// tree-sitter-solidity v1.2.11 misparses the directive into an
// ERROR node at source_file scope:
//
//   source_file ★HasError
//     ERROR "using SafeMath for uint256 global;" ★HasError
//       type_name
//         user_defined_type
//           identifier "using"      ← keyword misclassified
//       identifier "SafeMath"        ← LIBRARY NAME (recoverable)
//       type_name
//         primitive_type "uint256"  ← bound type (recoverable)
//       identifier "global"          ← optional qualifier
//
// V2.18 walker (runFileLevelUsingFor in using_for.go) recovers the
// recoverable identifiers and emits the same PendingRef pair
// (dispatchKindUsingFor + dispatchKindUsingForTypeBind) per
// contract/library/interface in the file. Sol semantics: file-level
// using applies to every container in the source file (with or
// without `global` qualifier).
//
// Expectations (post-V2.18):
//   - 1 EdgeUsesFor: Vault → SafeMath
//   - Vault.compute uses `x.add(1)` and resolves through file-level
//     binding → 1 EdgeCalls: Vault.compute → SafeMath.add
//
// V2.5 (file-level operator-form) is a separate grammar-block from
// row 2 and stays at 0 edges — operator-form's `as +` syntax has no
// recoverable shape (V2.17 analysis).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.13;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

using SafeMath for uint256 global;

contract Vault {
    uint256 public x;

    function compute() external view returns (uint256) {
        return x.add(1);
    }
}
