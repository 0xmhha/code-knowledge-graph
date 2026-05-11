// Sol W3 fixture — chained / nested interface dispatch.
//
// Expected EdgeInvokes (all AMBIGUOUS — §5.0 Q5):
//   Router.route invokes IFoo.bar       (outer call)
//   Router.route invokes IBar.baz       (nested argument)
//   Router.proxy invokes IFoo.something (single inner)
//
// Notes:
//   - Chained `IFoo(a).bar().qux()` would NOT match for the outer `.qux`
//     because its object is a call_expression whose function is a
//     member_expression (not an identifier). Only the inner `IFoo(a).bar`
//     emits.
//   - `address(0)` is a primitive cast, must NOT emit.
pragma solidity ^0.8.20;

interface IFoo {
    function bar(uint256 x) external returns (uint256);
    function something(address a) external view returns (uint256);
}

interface IBar {
    function baz(address a) external view returns (uint256);
}

contract Router {
    function route(address fooAddr, address barAddr, address subject) external returns (uint256) {
        // Outer .bar invokes IFoo.bar; nested IBar(barAddr).baz invokes IBar.baz.
        return IFoo(fooAddr).bar(IBar(barAddr).baz(subject));
    }

    function proxy(address fooAddr) external view returns (uint256) {
        return IFoo(fooAddr).something(address(0));
    }
}
