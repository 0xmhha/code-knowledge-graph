// Sol W1 fixture (cross-file, child side) — inherits from BaseToken
// (contract) and IExternal (interface) declared in cross_file_parent.sol.
// Expected edges (different file, ConfInferred):
//   ChildToken extends    BaseToken
//   ChildToken implements IExternal
pragma solidity ^0.8.20;

contract ChildToken is BaseToken, IExternal {
    function external_method() external {}
}
