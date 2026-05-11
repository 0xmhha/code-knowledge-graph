// Sol W4 fixture — plain `contract` declaration (no abstract).
// Expected: NodeContract SubKind="contract" for Simple (explicit value,
// not empty string). Regression guard for the "abstract contract" path
// — the keyword detector must not false-positive on plain contracts.
pragma solidity ^0.8.20;

contract Simple {
    uint256 public value;

    function set(uint256 v) external {
        value = v;
    }
}
