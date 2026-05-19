// W-C W9 V8 fixture — inheritance graph with no consistent C3
// linearization. A's MRO is [A, X, Y]; B's MRO is [B, Y, X];
// Z inherits A,B which would force X before Y (via A) AND Y
// before X (via B) simultaneously. Sol's reference compiler
// rejects this; our parser falls back to the depth-first walk
// and stamps HasInheritanceMROFallback=true on Z so downstream
// tooling can surface the diagnostic.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract X {
    uint256 x1;
}

contract Y {
    uint256 y1;
}

contract A is X, Y {
    uint256 a1;
}

contract B is Y, X {
    uint256 b1;
}

contract Z is A, B {
    uint256 z1;
}

// Reference contract with a clean linear chain — should NOT carry
// the fallback marker.
contract Plain is X {
    uint256 p1;
}
