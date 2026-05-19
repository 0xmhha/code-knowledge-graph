// W-C W10 V12 fixture — payable(this).transfer/send is a self-
// reentrant value-transfer surface.
//
//   - selfTransfer: payable(this).transfer(amount) — V12 marks
//                    HasSelfReentrantCall=true even though the
//                    method is value-transfer, not low-level
//                    call.
//   - selfSend:     payable(this).send(amount) — same shape,
//                    same marker.
//   - externalSend: payable(target).send(amount) — V12 does
//                    NOT light HasExternalCall (value-transfer
//                    goes through W8 V1's HasValueTransfer
//                    marker; we don't double-count).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract SelfTransfer {
    receive() external payable {}

    function selfTransfer(uint256 amount) external {
        payable(this).transfer(amount);
    }

    function selfSend(uint256 amount) external returns (bool) {
        return payable(this).send(amount);
    }

    function externalSend(address payable target, uint256 amount) external returns (bool) {
        return payable(target).send(amount);
    }
}
