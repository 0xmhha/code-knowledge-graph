// W-C W6 V1.2 fixture — multi-level inheritance. Grand → Parent → Child.
// Only Grand declares `using GrandLib for uint256;`; both Parent and
// Child inherit. V1.2 BFS must walk through Parent to find Grand's
// binding when resolving Child's dispatch.
// Expectations:
//   - 1 EdgeUsesFor: Grand → GrandLib
//   - 2 EdgeCalls:
//       Parent.tap   → GrandLib.tap   (inherited from Grand)
//       Child.tap2   → GrandLib.tap   (inherited via Parent → Grand)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library GrandLib {
    function tap(uint256 self) internal pure returns (uint256) {
        return self + 7;
    }
}

contract Grand {
    using GrandLib for uint256;
}

contract Parent is Grand {
    uint256 public pv;

    function tap() external {
        pv = pv.tap();
    }
}

contract Child is Parent {
    uint256 public cv;

    function tap2() external {
        cv = cv.tap();
    }
}
