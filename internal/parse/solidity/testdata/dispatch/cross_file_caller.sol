// Sol W3 fixture (cross-file, caller side) — invokes IExternalAPI
// declared in cross_file_iface.sol via interface dispatch.
//
// Expected:
//   ExternalCaller.run invokes IExternalAPI.execute  (AMBIGUOUS)
pragma solidity ^0.8.20;

contract ExternalCaller {
    function run(address target, uint256 amount) external returns (bool) {
        return IExternalAPI(target).execute(amount);
    }
}
