// W-C W10 V15 fixture — constructor self-cast. The cast walker
// (W10 V5/V8/V12) reaches the enclosing callable via
// nearestFunctionQnameAndStart, which W6 V1.23 extended to
// recognise constructor_definition with the synthetic
// "constructor" identifier. runConstructorDecl emits NodeFunction
// + SubKind="constructor" using the same (qname, startByte) pair
// so parse.MakeID lines up.
//
//   - InitSelf.constructor calls payable(this).call(...) — V5/V8
//     should mark HasSelfReentrantCall on the constructor row.
//   - InitTransfer.constructor calls payable(this).transfer(0) —
//     V12 added value-transfer methods to the self-cast path, so
//     the constructor row should also light up.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract InitSelf {
    constructor() payable {
        (bool ok, ) = payable(this).call("");
        require(ok, "self-init failed");
    }
}

contract InitTransfer {
    constructor() payable {
        payable(this).transfer(0);
    }
}
