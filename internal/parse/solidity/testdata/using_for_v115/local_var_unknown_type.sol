// W-C W6 V1.15 fixture — local-var with a declared type that has no
// using-for binding. Resolver must drop (no false-positive EdgeCalls).
// Expectations:
//   - 1 EdgeUsesFor: Caller → SafeMath (for uint256, the bound type)
//   - 0 V1.15 EdgeCalls (`addr` is `address`, not bound to anything)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract Caller {
    using SafeMath for uint256; // binds uint256 only

    function run() external view returns (address) {
        address addr = msg.sender;
        // addr is `address`, not uint256 → no SafeMath binding → drop.
        // (Calling .add on an address would not compile in real Solidity,
        // but the parser handles syntactic shapes uniformly; the binding
        // miss is what guarantees no false-positive.)
        return addr;
    }
}
