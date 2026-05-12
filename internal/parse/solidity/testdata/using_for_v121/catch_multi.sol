// W-C W6 V1.21 fixture — multiple catch clauses, each with named
// parameter. Confirms V1.21 emits across every catch_clause sibling.
//
// Expectations:
//   - 1 EdgeUsesFor: C → SafeMath
//   - At least 2 EdgeCalls in C.f → SafeMath.add (one per catch body
//     receiver). collectUsingForCalls deduplicates (caller, target) so
//     `want` lists once; the test confirms presence (not count).

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IExternal {
    function compute() external pure returns (uint256);
}

library SafeMath {
    function add(uint256 a, uint256 b) internal pure returns (uint256) {
        return a + b;
    }
}

contract C {
    using SafeMath for uint256;

    IExternal public ext;

    function f() external returns (uint256) {
        try ext.compute() returns (uint256 r) {
            return r;
        } catch Error(string memory) {
            return 0; // anonymous slot of Error catch — V1.21 emit skip
        } catch Panic(uint256 code) {
            // code is a named catch param, V1.21 captures
            return code.add(1);
        } catch (bytes memory data) {
            data; // unused; bytes not bound to SafeMath
            return 2;
        }
    }
}
