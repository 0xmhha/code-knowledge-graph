// Sol W1 fixture — contract implementing an interface, plus mixed
// inheritance (contract base + 2 interfaces). Expected edges (same file,
// ConfExtracted):
//   Impl  implements IERC20
//   Mixed extends    BaseContract
//   Mixed implements IFoo
//   Mixed implements IBar
//   IB    extends    IA          (interface-to-interface inheritance)
pragma solidity ^0.8.20;

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
}

interface IFoo { function foo() external; }
interface IBar { function bar() external; }

interface IA { function a() external; }
interface IB is IA { function b() external; }

contract BaseContract {
    uint256 public x;
}

contract Impl is IERC20 {
    function transfer(address to, uint256 amount) external returns (bool) {
        // body irrelevant for W1
        return true;
    }
}

contract Mixed is BaseContract, IFoo, IBar {
    function foo() external {}
    function bar() external {}
}
