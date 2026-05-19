// W-C W10 V7 fixture — depth-2 chained receiver.
//
// `getHub().getTarget().call(data)` is a two-link chain:
//   getHub()   -> returns Hub
//   .getTarget() -> Hub method returning address
//   .call(data) -> low-level call on address
//
// V6 only matched single-level chains. V7 walks two return-type
// hops via funcReturnTypes and marks HasExternalCall when the
// second hop's return type is address.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Hub {
    address stored;

    function getTarget() external view returns (address) {
        return stored;
    }
}

contract Caller {
    Hub h;

    function getHub() internal view returns (Hub) {
        return h;
    }

    // V7: depth-2 chained external call. Marker fires.
    function relay(bytes memory data) external returns (bool) {
        (bool ok, ) = getHub().getTarget().call(data);
        return ok;
    }
}
