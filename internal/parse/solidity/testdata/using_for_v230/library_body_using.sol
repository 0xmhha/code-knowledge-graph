// W-C W6 V2.3 fixture — library body holds its own `using` directive
// and dispatches via that binding inside its own function. queryUsingFor
// matches library_declaration body (per V0 query). lookupReceiverType
// uses containerIDByFuncID, which maps the library's function ID to
// the library itself as the container; bindings[libraryID][typeName]
// supplies the binding. Both library bodies and contract bodies are
// equivalent containers for using-for purposes.
//
// Expectations:
//   - 2 EdgeUsesFor: WrapperLib → InnerLib (binding inside WrapperLib)
//                  (no Contract-level using-for in this fixture)
//   - 1 EdgeCalls: WrapperLib.compute → InnerLib.add

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library InnerLib {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

library WrapperLib {
    using InnerLib for uint256;

    function compute(uint256 seed) internal pure returns (uint256) {
        return seed.add(1);
    }
}
