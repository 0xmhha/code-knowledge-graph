// Sol W3 fixture (cross-file, interface side) — declares IExternalAPI.
// The caller side lives in cross_file_caller.sol. Even cross-file
// resolution stays at AMBIGUOUS confidence (§5.0 Q5) — file boundary
// is not the dispatch-uncertainty source for W3.
pragma solidity ^0.8.20;

interface IExternalAPI {
    function execute(uint256 amount) external returns (bool);
}
