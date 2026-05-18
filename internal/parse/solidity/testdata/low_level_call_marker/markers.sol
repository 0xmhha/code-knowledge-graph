// W-C W8 V1 fixture — HasLowLevelCall and HasValueTransfer markers.
//
// W7.1 V0 emits EdgeInvokes only when the receiver of a `.call`
// family invocation resolves to a Contract or Interface. W8 V1 adds
// presence markers that flip independently of resolvability:
//
//   HasLowLevelCall   — function body contains .call / .delegatecall /
//                       .staticcall on any receiver shape (bare
//                       identifier OR address(x) cast OR chained).
//   HasValueTransfer  — function body contains .send / .transfer.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Markers {
    address payable public sink;
    address public target;

    // HasLowLevelCall = true (bare identifier receiver, address type
    // — currently W7.1 drops the edge, but the marker still flips).
    function callBare(bytes memory data) external {
        target.call(data);
    }

    // HasLowLevelCall = true (address(x) cast receiver — W7.1
    // explicitly drops this in V0 per W7-D2).
    function callCast(address t, bytes memory data) external {
        address(t).delegatecall(data);
    }

    // HasValueTransfer = true (.send returns bool).
    function transferSend(uint256 amt) external {
        bool ok = sink.send(amt);
        require(ok, "send failed");
    }

    // HasValueTransfer = true (.transfer reverts on failure).
    function transferTransfer(uint256 amt) external {
        sink.transfer(amt);
    }

    // Neither flag set — plain Solidity.
    function plain(uint256 x) external pure returns (uint256) {
        return x + 1;
    }
}
