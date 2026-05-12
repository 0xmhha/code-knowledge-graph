// W-C W6 V1.8 fixture — depth-5 same-contract chain. Generic walker
// must handle arbitrary depth (not just 4).
// Expectations:
//   - 1 EdgeUsesFor: Deep → DeepLib
//   - 1 EdgeCalls (V1.8 generic walker): Deep.run → DeepLib.tag

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library DeepLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract D4 {
    function m5() external pure returns (uint256) {
        return 1;
    }
}

contract D3 {
    function m4() external pure returns (D4) {
        return D4(address(0));
    }
}

contract D2 {
    function m3() external pure returns (D3) {
        return D3(address(0));
    }
}

contract D1 {
    function m2() external pure returns (D2) {
        return D2(address(0));
    }
}

contract Deep {
    using DeepLib for uint256;

    function m1() internal pure returns (D1) {
        return D1(address(0));
    }

    function run() external pure returns (uint256) {
        return m1().m2().m3().m4().m5().tag();
    }
}
