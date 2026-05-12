// W-C W6 V1.8 fixture — depth-3 cross-contract generic chain.
// `obj.foo().bar().baz().add(1)` — receiver is state var; chain has
// 3 fn links after the receiver.
// Expectations:
//   - 1 EdgeUsesFor: Caller → CrossLib
//   - 1 EdgeCalls (V1.8 generic walker): Caller.run → CrossLib.add
//     (chain: obj receiver → Producer.foo()'s S2 → S2.bar()'s S3 →
//      S3.baz()'s uint256 → CrossLib via uint256 binding)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library CrossLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract S3 {
    function baz() external pure returns (uint256) {
        return 1;
    }
}

contract S2 {
    function bar() external pure returns (S3) {
        return S3(address(0));
    }
}

contract Producer {
    function foo() external pure returns (S2) {
        return S2(address(0));
    }
}

contract Caller {
    using CrossLib for uint256;

    Producer public obj;

    function run() external view returns (uint256) {
        return obj.foo().bar().baz().add(1);
    }
}
