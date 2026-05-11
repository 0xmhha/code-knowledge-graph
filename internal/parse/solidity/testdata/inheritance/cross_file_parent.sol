// Sol W1 fixture (cross-file, parent side) — declares `BaseToken` and
// `IExternal` so cross_file_child.sol can inherit them across file
// boundaries. Resolution → ConfInferred (different FilePath in Pass 2).
pragma solidity ^0.8.20;

interface IExternal {
    function external_method() external;
}

contract BaseToken {
    string public name = "Base";
}
