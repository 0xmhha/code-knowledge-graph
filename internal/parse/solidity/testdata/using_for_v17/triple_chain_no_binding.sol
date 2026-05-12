// W-C W6 V1.7 fixture — depth-3 chain resolves all links but the
// final return type has no binding. Resolver must drop at binding
// lookup.
// Expectations:
//   - 1 EdgeUsesFor: Producer → UintLib (uint256-only)
//   - 0 V1.7 EdgeCalls (mkC()'s return is address; only uint256 bound)

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

library UintLib {
    function tag(uint256 self) internal pure returns (uint256) {
        return self;
    }
}

contract Stage2 {
    function mkC() external view returns (address) {
        return address(this);
    }
}

contract Stage1 {
    function mkB() external pure returns (Stage2) {
        return Stage2(address(0));
    }
}

contract Producer {
    using UintLib for uint256;

    function mkA() internal pure returns (Stage1) {
        return Stage1(address(0));
    }

    function run() external pure returns (uint256) {
        // Final return type is address; bindings only cover uint256 →
        // V1.7 step 8 drops.
        return mkA().mkB().mkC().tag();
    }
}
