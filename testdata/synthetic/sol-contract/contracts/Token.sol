pragma solidity ^0.8.20;
contract Token {
    string public name = "Synth";
    mapping(address => uint256) public balanceOf;
    function transfer(address to, uint256 amt) external {
        require(balanceOf[msg.sender] >= amt, "insufficient");
        balanceOf[msg.sender] -= amt;
        balanceOf[to] += amt;
    }
}
