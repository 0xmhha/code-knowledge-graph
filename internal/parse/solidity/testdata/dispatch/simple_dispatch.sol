// Sol W3 fixture — single-file interface dispatch.
//
// Expected (W3 declaration-time):
//   Caller.send invokes IERC20.transfer   (AMBIGUOUS — §5.0 Q5)
//
// Notes:
//   - Same file, but confidence is still AMBIGUOUS — runtime address
//     determines actual dispatch target, not file boundary.
//   - `address(this)` is a primitive type cast, not interface dispatch
//     (identifier "address" is not a known interface) — must NOT emit
//     EdgeInvokes for it.
pragma solidity ^0.8.20;

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
    function balanceOf(address owner) external view returns (uint256);
}

contract Caller {
    function send(address tokenAddr, address to, uint256 amount) external {
        IERC20(tokenAddr).transfer(to, amount);
    }

    function check(address tokenAddr, address owner) external view returns (uint256) {
        return IERC20(tokenAddr).balanceOf(owner);
    }
}
