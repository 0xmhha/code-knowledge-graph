// W-C W8 V11 fixture — try/catch with function-typed returns
// parameter propagation.
//
// `try contract.method() returns (function(...) cb) { ... } catch
// { ... }` declares `cb` as a fn-typed local visible inside the
// success block. Assigning that cb to a state variable inside the
// success block is propagation — the same security signal as the
// V8/V9/V10 shapes.
//
// Expected markers on Caller.captureCallback:
//   HasFunctionTypedVar=true        (V3 — declares fn-typed cb via try-returns)
//   HasFunctionPointerPropagation=true (V8/V11 — assigns cb to handler)
//   HasFunctionPointerCall=false    (cb never invoked)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IProvider {
    function getCallback() external returns (function(uint256) external returns (uint256));
}

contract Caller {
    IProvider provider;
    function(uint256) external returns (uint256) handler;

    function captureCallback() external {
        try provider.getCallback() returns (function(uint256) external returns (uint256) cb) {
            handler = cb;
        } catch {
            // ignore failure
        }
    }
}
