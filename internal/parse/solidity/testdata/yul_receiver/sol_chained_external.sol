// W-C W10 V6 fixture — chained-call receiver shape.
//
// `getTarget()` returns an address; `getTarget().call(data)` is a
// chained low-level call whose receiver is the return value of an
// inner function. Pre-V6 the walker dropped the shape because
// matchStateVarMethodCall required a bare-identifier receiver and
// the cast-shape walker (V5) only matched address(...) /
// payable(...). V6 plugs matchChainedMethodCall into the marker
// path and consults funcReturnTypes during Pass 2.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ChainCaller {
    address stored;

    function getTarget() internal view returns (address) {
        return stored;
    }

    function getInt() internal pure returns (uint256) {
        return 42;
    }

    // V6: chained call on address-typed return. Marker fires.
    function relay(bytes memory data) external returns (bool) {
        (bool ok, ) = getTarget().call(data);
        return ok;
    }

    // Reference: chained call whose inner return is not address.
    // Wouldn't normally type-check, but locks the walker against
    // false positives.
    function noopChain() external pure returns (uint256) {
        return getInt();
    }
}
