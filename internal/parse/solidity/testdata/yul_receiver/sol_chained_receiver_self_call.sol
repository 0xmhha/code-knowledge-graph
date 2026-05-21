// W-C W10 V21 fixture — chained-receiver self-call negative case.
//
// Documents an intentional false negative in V19's isSelfRef
// heuristic. `getTarget(this).foo()` parses as
//
//   call_expression
//     function: expression → identifier "getTarget"   ← lower-case
//     arguments: call_argument → expression → identifier "this"
//   . property: identifier "foo"
//
// isSelfRef's call_expression branch requires the leading
// identifier to start upper-case (Solidity's type-name convention).
// `getTarget` is lower-case, so the branch returns false and the
// call is treated as a normal method-on-return-value invocation
// rather than a typed self-cast. The semantic is correct in
// general (the helper might return any address, not necessarily
// the caller) but it produces a false negative when the helper
// *does* in fact return `this`.
//
// We accept the trade — false negatives in security marking are
// recoverable (the analyst inspects the code anyway), while false
// positives on every `helper(this).foo()` would noise out the
// signal. This fixture pins the current behaviour so:
//
//   1. A future heuristic change (accepting lower-case identifiers)
//      flips this test and forces an explicit design discussion.
//   2. A future *correct* recognition path (e.g. cross-procedural
//      analysis of helper return types) flips this test and the
//      maintainer can update the expected outcome.
//
//   - ChainedSelf.invoke : getTarget(this).foo()
//     The helper returns `this`, so semantically this is a self-
//     call; ckg marks it false on purpose.

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IFoo {
    function foo() external;
}

contract ChainedSelf is IFoo {
    function invoke() external {
        getTarget(this).foo();
    }

    function getTarget(IFoo x) internal pure returns (IFoo) {
        return x;
    }

    function foo() external override {}
}
