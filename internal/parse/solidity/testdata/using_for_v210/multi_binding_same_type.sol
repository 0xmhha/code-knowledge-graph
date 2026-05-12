// W-C W6 V2.1 fixture — multi-binding for same type. Two `using`
// directives bind the same type (uint256) to two different libraries.
// V0 binding map is single-value (bindings[contractID][typeName] =
// libraryName), so the second directive overwrites the first. If a
// receiver calls a method only present in the FIRST library, the
// dispatch drops (false-negative).
//
// This fixture exercises the case: A.tag(uint256) is only in LibA,
// LibB has a different method (no `tag`). After two `using` directives,
// V0 binds uint256 → LibB (overwrite), so `x.tag(...)` resolution
// drops because LibB.tag doesn't exist. Real Sol semantics: both
// bindings apply; methods unique to each library are resolved
// individually.
//
// Expectations (V0 behavior — known limitation):
//   - 2 EdgeUsesFor: Vault → LibA, Vault → LibB
//   - 0 EdgeCalls from x.tag (false-negative, LibA.tag dispatch drops)
//
// V2.2+: multi-value binding (bindings[contractID][typeName] = []library)
// with resolver trying each library's funcByQName until hit.
//
// This fixture LOCKS THE KNOWN LIMITATION — when V2.2 fixes multi-
// binding, this test will need to be updated to assert the EdgeCalls
// surfaces instead.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library LibA {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

library LibB {
    function bump(uint256 self) internal pure returns (uint256) {
        return self + 1;
    }
}

contract Vault {
    using LibA for uint256;
    using LibB for uint256; // overwrites uint256 binding

    function run() external pure returns (uint256) {
        uint256 x = 0;
        // V0: uint256 → LibB (second directive wins). LibB.tag doesn't
        // exist → drop. Real Sol: LibA.tag should resolve.
        return x.tag();
    }
}
