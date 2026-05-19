// W-C W11 V6 fixture (Sol half) — exercises the W6-W10 marker
// surface so the end-to-end buildpipe.Run pipeline can be tested
// for marker persistence in graph.db.
//
// Markers expected on the persisted NodeFunction rows:
//   - Wallet.relay     -> HasExternalCall=true (cast-shape low-level call)
//   - Wallet.trigger   -> HasFunctionPointerCall=true (fn-typed state var call)
//   - Wallet.plain     -> no markers
//
// Cross-language binding: a TS class named "Wallet" exists in
// src/Wallet.ts so the linker emits a binds_to edge from the Sol
// contract to the TS class.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Wallet {
    // Function-typed state-var — IsFunctionTyped marker (W8 V2).
    function(uint256) external returns (uint256) onAction;

    // W8 V5: invoke a function-typed state-var as a bare identifier.
    function trigger(uint256 x) external returns (uint256) {
        return onAction(x);
    }

    // W10 V5: cast-shape low-level call. HasExternalCall=true.
    function relay(address target, bytes memory data) external returns (bool) {
        (bool ok, ) = payable(target).call(data);
        return ok;
    }

    // No markers. HasExternalCall stays false.
    function plain(uint256 x) external pure returns (uint256) {
        return x + 1;
    }
}
