// W-C W10 V1.1 fixture — Yul EVM builtin detection inside
// assembly blocks.
//
// Detected (security-relevant ops; sorted, deduped):
//   call, delegatecall, sload, selfdestruct, sstore, staticcall

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Proxy {
    bytes32 constant SLOT = keccak256("eip1967.impl");

    // YulBuiltins = ["delegatecall"] (and also "calldatacopy" etc but
    // those are excluded — the test filters to security-critical ops).
    function delegate(address impl) internal {
        assembly {
            calldatacopy(0, 0, calldatasize())
            let result := delegatecall(gas(), impl, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch result
            case 0 { revert(0, returndatasize()) }
            default { return(0, returndatasize()) }
        }
    }

    // YulBuiltins = ["sload"]
    function read() internal view returns (address impl) {
        assembly {
            impl := sload(SLOT)
        }
    }

    // YulBuiltins = ["sstore"]
    function write(address impl) internal {
        assembly {
            sstore(SLOT, impl)
        }
    }

    // YulBuiltins = ["selfdestruct"]
    function kill(address payable beneficiary) external {
        assembly {
            selfdestruct(beneficiary)
        }
    }

    // YulBuiltins = sorted union of multiple ops: ["call","selfdestruct","sload","sstore"]
    function multiOp(address payable beneficiary, address impl) external {
        assembly {
            let r := sload(0)
            sstore(0, impl)
            pop(call(gas(), impl, 0, 0, 0, 0, 0))
            selfdestruct(beneficiary)
        }
    }

    // YulBuiltins = nil — assembly contains only non-critical ops.
    function safe() internal pure returns (uint256 r) {
        assembly {
            r := add(1, 2)
        }
    }

    // YulBuiltins = nil — no assembly at all.
    function plain(uint256 x) external pure returns (uint256) {
        return x + 1;
    }
}
