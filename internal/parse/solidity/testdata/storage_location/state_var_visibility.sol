// W-C W7.2 V0 fixture — state-var visibility + immutable detection.
//
// Vendored tree-sitter-solidity v1.2.11 grammar exposes:
//   - `visibility` named child for public / private / internal / external
//   - `immutable` named child for the immutable keyword
//
// Does NOT expose (V0 limitation, deferred to W7.2 V1+):
//   - `constant` keyword (silently dropped from AST)
//   - parameter `memory` / `calldata` / `storage` keywords

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract C {
    uint256 public a;              // SubKind = "storage_public"
    uint256 private b;             // SubKind = "storage_private"
    uint256 internal c;            // SubKind = "storage_internal"
    uint256 immutable d;           // SubKind = "immutable"
    uint256 e;                     // SubKind = "" (no visibility → Sol default internal, but AST doesn't tell us)

    constructor() { d = 0; }
}
