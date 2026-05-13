// W-C W6 V2.13 fixture — diamond inheritance with conflicting
// using-for bindings inherited from two parents.
//
// Two libraries each contribute different methods on uint256:
//   - SafeMath provides `add`.
//   - OtherMath provides `mul`.
//
// Two parent contracts bind uint256 to a different library each:
//   - contract A { using SafeMath for uint256; }
//   - contract B { using OtherMath for uint256; }
//
// Child inherits from both. In Solidity semantics, both library
// bindings should be visible to the child for uint256 method
// lookups — `add` resolves through SafeMath, `mul` through
// OtherMath, with the receiver type being the discriminator.
// V2.2 already supports multi-binding for the same type within a
// single contract; the question this V cycle probes is whether
// V1.2's BFS-based inheritance propagation correctly merges
// (union) multiple ancestor bindings or drops the second one.
//
// Pre-V2.13 code path (resolve.go ~L385): the BFS skips writing
// to `bindings[childID][typeName]` if the slot already exists,
// which means the first ancestor's binding wins and subsequent
// ancestors' bindings for the same type are silently dropped.
// This fixture exercises that exact corner.
//
// Expectations after V2.13 fix:
//   - 1 EdgeUsesFor: A → SafeMath
//   - 1 EdgeUsesFor: B → OtherMath
//   - Child inherits both via EdgeExtends; bindings union to
//     ["SafeMath", "OtherMath"] on uint256.
//   - 1 EdgeCalls: Child.callAdd → SafeMath.add
//   - 1 EdgeCalls: Child.callMul → OtherMath.mul
//
// If BOTH EdgeCalls surface, the inherited binding propagation
// preserves the multi-parent union. If only one surfaces, V1.2
// has a hidden assumption (first-ancestor-wins) that V2.13 must
// repair to align with V2.2 multi-binding semantics.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library OtherMath {
    function mul(uint256 a, uint256 b) internal pure returns (uint256) {
        return a * b;
    }
}

contract A {
    using SafeMath for uint256;
}

contract B {
    using OtherMath for uint256;
}

contract Child is A, B {
    uint256 public x;

    function callAdd() external view returns (uint256) {
        return x.add(1);
    }

    function callMul() external view returns (uint256) {
        return x.mul(2);
    }
}
