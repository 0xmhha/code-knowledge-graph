// W-C W10 V2 fixture for Yul-level low-level call receiver resolution.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IImpl {
    function exec(bytes calldata data) external returns (bytes memory);
}

contract Proxy {
    IImpl public impl;

    function delegate(bytes memory data) external returns (bool) {
        bool ok;
        assembly {
            let r := delegatecall(gas(), impl, add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }

    function readImpl(bytes memory data) external view returns (bool) {
        bool ok;
        assembly {
            let r := staticcall(gas(), impl, add(data, 32), mload(data), 0, 0)
            ok := r
        }
        return ok;
    }
}
