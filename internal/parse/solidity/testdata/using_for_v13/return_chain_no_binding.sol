// W-C W6 V1.3 fixture — chained call but no binding for the inner
// function's return type. Resolver must drop (no false-positive
// EdgeCalls).
// Expectations:
//   - 0 V1.3 EdgeCalls
//   - The factory()'s return type is `address` but no `using ... for
//     address;` declaration exists.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library AddrLib {
    function ping(address a) internal pure returns (address) {
        return a;
    }
}

contract NoBindReturn {
    // Note: only uint256 is bound — `factory()` returns address, so
    // `.ping()` chain should NOT resolve to AddrLib.ping (no binding).
    using AddrLib for uint256;

    function factory() internal pure returns (address) {
        return address(0);
    }

    function run() external pure returns (address) {
        return factory().ping();
    }
}
