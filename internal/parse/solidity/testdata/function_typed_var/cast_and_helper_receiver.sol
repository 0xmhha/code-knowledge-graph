// W-C W8 V27 fixture — cast-receiver / helper-return chain (negative lock).
//
// V26 locked the call-chain shape `s.getCb()(x)` where the outer
// callee is a call_expression. V27 locks the two remaining shapes
// V6 (cross_contract_fn_pointer.go) explicitly documents as
// deferred:
//
//	V6 limitations:
//	  - Chained / cast receivers (`getHub().onAction(x)`,
//	    `Hub(addr).onAction(x)`) drop — matchStateVarMethodCall
//	    requires a bare-identifier receiver.
//
// Cell A — `Hub(addr).onAction(x)`: contract-cast receiver. The
// receiver expression is `Hub(addr)` (call_expression with a
// type-name callee), not a bare identifier resolvable to a
// state-var / param / local. V6 short-circuits at
// matchStateVarMethodCall; the fn-pointer NodeField on Hub is
// reachable in principle (Pass 2 has the cross-file index) but the
// Pass 1 walker never queues a PendingRef for it.
//
// Cell B — `getHub().onAction(x)`: helper-return receiver. The
// receiver expression is `getHub()` (call_expression with an
// identifier callee). V6 again drops; even though Pass 2 has the
// fn-pointer return type for getHub, the Pass 1 walker only
// matches direct member_expression on a bare-identifier receiver.
//
// Reference row — `h.onAction(x)`: V6 positive baseline. With a
// bare state-var receiver, V6 fires and HasFunctionPointerCall =
// true. Pinning the reference makes the negative cells *specifically*
// about receiver-shape variance, not about V6 propagation generally.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Hub {
    function(uint256) external returns (uint256) onAction;
}

contract Caller {
    Hub h;

    // Internal helper used by Cell B. Returns the state-var-typed
    // Hub instance. V6 ignores helpers entirely.
    function getHub() internal view returns (Hub) {
        return h;
    }

    // Reference row: bare-identifier receiver. V6 cover, fn-pointer
    // call detected. HasFunctionPointerCall=true.
    function bareInvoke(uint256 x) external returns (uint256) {
        return h.onAction(x);
    }

    // Cell A: cast-receiver chain. HasFunctionPointerCall=false
    // (V6 deferred limitation, cast-receiver shape).
    function castInvoke(address a, uint256 x) external returns (uint256) {
        return Hub(a).onAction(x);
    }

    // Cell B: helper-return chain. HasFunctionPointerCall=false
    // (V6 deferred limitation, helper-return-receiver shape).
    function helperInvoke(uint256 x) external returns (uint256) {
        return getHub().onAction(x);
    }
}
