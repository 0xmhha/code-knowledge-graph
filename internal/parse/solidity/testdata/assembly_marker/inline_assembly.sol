// W10 V0 fixture — HasAssembly marker for callables containing
// `assembly { ... }` blocks.
//
// V0 detects presence only. Yul-internal op detection (delegatecall,
// sstore, selfdestruct) and receiver resolution are V1+.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Proxy {
    bytes32 constant SLOT = keccak256("eip1967.impl");

    // HasAssembly = true — body contains an assembly block.
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

    // HasAssembly = true — even a tiny assembly use sets the flag.
    function readSlot() internal view returns (address impl) {
        assembly {
            impl := sload(SLOT)
        }
    }

    // HasAssembly = false — plain Solidity, no assembly.
    function plain(uint256 x) internal pure returns (uint256) {
        return x + 1;
    }

    // HasAssembly = true — modifier body with assembly.
    modifier guard() {
        assembly {
            if iszero(calldatasize()) { revert(0, 0) }
        }
        _;
    }

    // HasAssembly = false — modifier with no assembly.
    modifier plainGuard() {
        require(msg.sender != address(0));
        _;
    }
}
