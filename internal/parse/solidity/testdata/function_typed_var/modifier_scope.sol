// W-C W8 V15 fixture — function-typed local inside a modifier
// body. nearestFunctionQnameAndStart recognises
// modifier_definition (W6 V1.22), and runDecl emits NodeModifier
// with the same (qname, startByte) used by parse.MakeID — so the
// V3 marker walker's HasFunctionTypedVar mark should land on the
// NodeModifier row.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract ModHolder {
    function(uint256) internal pure returns (uint256) handler;

    // V15 audit target: modifier with a function-typed local.
    modifier withFnLocal() {
        function(uint256) internal pure returns (uint256) local = handler;
        require(local(1) >= 0, "ok");
        _;
    }

    function guarded() external view withFnLocal returns (uint256) {
        return handler(0);
    }
}
