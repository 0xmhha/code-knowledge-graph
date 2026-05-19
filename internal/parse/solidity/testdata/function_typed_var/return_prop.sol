// W-C W8 V9 fixture — return-position function pointer propagation.
//
//   - returnCb: returns a fn-typed param. V9 detects the return as
//                propagation (HasFunctionPointerPropagation=true,
//                HasFunctionPointerCall=false).
//
//   - returnStored: returns a fn-typed state-var. Same marker —
//                    even though the state-var is contract-scoped,
//                    the return propagates it to the caller.
//
//   - noReturn: returns a primitive, no propagation.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ReturnProp {
    function(uint256) external returns (uint256) stored;

    function returnCb(function(uint256) external returns (uint256) cb)
        external
        returns (function(uint256) external returns (uint256))
    {
        return cb;
    }

    function returnStored()
        external
        view
        returns (function(uint256) external returns (uint256))
    {
        return stored;
    }

    function noReturn(uint256 x) external pure returns (uint256) {
        return x + 1;
    }
}
