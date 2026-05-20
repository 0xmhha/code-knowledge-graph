// W-C W10 V14 fixture — receive/fallback self-reentrant value
// transfer. W10 V12 admitted value-transfer methods (`send`,
// `transfer`) on the self-cast HasSelfReentrantCall path for
// regular external functions. V14 audits that receive() and
// fallback() get the same treatment — they're indexed as
// NodeFunction with SubKind="receive"/"fallback" and
// nearestFunctionQnameAndStart recognises
// fallback_receive_definition (W6 V1.24), so the marker walker
// should reach them and light up the reentrant signal.
//
// Security relevance: a receive/fallback that re-enters its own
// contract through payable(this).send / .transfer is a textbook
// reentrancy-loop shape — the inbound ether triggers the same
// hook recursively until gas runs out.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract LoopHolder {
    receive() external payable {
        payable(this).send(msg.value);
    }

    fallback() external payable {
        payable(this).transfer(msg.value);
    }
}
