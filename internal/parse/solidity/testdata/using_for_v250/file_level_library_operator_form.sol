// W-C W6 V2.5 fixture — file-level operator-form using directive with
// library-method target. Sol 0.8.19+ syntax allows binding library
// methods as user-defined operators at file scope:
//
//   using {Math.add as +, Math.sub as -} for uint256 global;
//
// Vendored tree-sitter-solidity v1.2.11 parses this as an ERROR child
// of source_file whose braced body is consumed by the ERROR text but
// not exposed as named children. The V2.5 walker parses the raw text
// to recover the library names and bound type, then emits one
// EdgeUsesFor pair per container in the file (file-level binding
// semantics apply to every contract / interface regardless of the
// `global` qualifier).
//
// Expectations:
//   - 1 EdgeUsesFor: User → Math (the library-method form reduces to
//     the library name, matching V2.20's contract-scope reduction).
//   - Math library and User contract both index normally.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

library Math {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }

    function sub(uint256 a, uint256 b) internal pure returns (uint256) {
        return a - b;
    }
}

using {Math.add as +, Math.sub as -} for uint256 global;

contract User {
    function compute(uint256 a, uint256 b) external pure returns (uint256) {
        return a + b - a;
    }
}
