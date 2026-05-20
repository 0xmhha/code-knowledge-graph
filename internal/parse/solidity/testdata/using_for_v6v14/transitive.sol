// W-C W6 V14 fixture — transitive using-for chain. Library Outer
// declares `using Inner for uint256;` inside its own body, so
// methods on Outer can dispatch uint256 receivers through Inner.
//
// Expected behaviour from the W6 resolver:
//   - Two EdgeUsesFor are emitted:
//       (a) Outer  -> Inner   from `using Inner for uint256;`
//                              inside Outer's body.
//       (b) Caller -> Outer   from `using Outer for uint256;`
//                              at the file scope on Caller.
//   - Neither emit duplicates; resolveUsingForRef must keep the
//     two scopes distinct and not collapse them onto a single
//     binding.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library Inner {
    function addOne(uint256 x) internal pure returns (uint256) {
        return x + 1;
    }
}

library Outer {
    using Inner for uint256;

    function addTwo(uint256 x) internal pure returns (uint256) {
        return x.addOne() + 1;
    }
}

contract Caller {
    using Outer for uint256;

    function run(uint256 x) external pure returns (uint256) {
        return x.addTwo();
    }
}
