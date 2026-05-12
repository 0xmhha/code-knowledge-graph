// W-C W6 V1.4 fixture — receiver is a primitive-type state var
// (uint256), not a contract reference. The cross-contract chain
// predicate matches structurally but the resolver must drop because
// uint256 has no methods to chain through.
//
// Expectations:
//   - 1 EdgeUsesFor: PrimChain → PrimLib (V0 emit)
//   - The state-var receiver shape `x.foo()` is handled by V1.0
//     (state-var dispatch). The cross-chain shape only fires when the
//     pattern is `x.foo().bar()` and x's type isn't a known container.
//   - 0 V1.4 EdgeCalls (uint256 not in byName[NodeContract / Interface]
//     → funcByQName["uint256.something"] miss → drop)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library PrimLib {
    function noop(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract PrimChain {
    using PrimLib for uint256;

    uint256 public x;

    function probe() external view returns (uint256) {
        // x is uint256 (primitive). The cross-chain shape
        // `x.unknownFn().noop()` syntactically matches V1.4's predicate,
        // but no contract named "uint256" exists in the graph → drop.
        // (Solidity itself wouldn't compile this — tree-sitter parses
        // it; the test guards against false-positive emission.)
        return x.unknownFn().noop();
    }
}
