package solidity

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W6 — `using For` library extension binding detection.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §4.6
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-C W6.
//
// Scope (V0 — Q9-1 (b), Q9-2 (a), Q9-3 (a), 2026-05-12):
//
//	contract Foo { using SafeMath for uint256; }   → EdgeUsesFor (Foo → SafeMath)
//	contract Foo { using SafeMath for *; }         → EdgeUsesFor (same shape)
//	contract Foo {
//	  using SafeMath for uint256;
//	  using Address  for address;
//	}                                              → 2 EdgeUsesFor (Foo→SafeMath, Foo→Address)
//
// Per §4.6 V0 limitations:
//   - free-function form `using {Lib.f1, Lib.f2} for T;` is dropped
//     (separate AST shape; V1 follow-up).
//   - file-level using directive (Solidity 0.8.13+ global binding) is
//     out of scope; V0 only recognises directives nested inside a
//     contract / library / interface body.
//   - typeName is parsed by the grammar but V0 does not expose it on
//     the EdgeUsesFor — one edge per directive regardless of typeName
//     ("`using A for X; using A for Y;` ⇒ two A edges, not deduped").
//
// Method-call dispatch resolution (`balance.add(...)` → SafeMath.add
// EdgeCalls) is V1 follow-up — requires receiver type inference
// infrastructure (state-var / parameter declared-type index).
// See §4.6.6 for the V1 carry-over list.
//
// Pass 1 → Pass 2 split: same idiom as W1 inheritance.
//   - Pass 1 emits PendingRef with DispatchKind="using_for", SrcID
//     hashed off the enclosing container's name identifier (matches
//     emitContractLikeNode's ID derivation in abstract_library.go),
//     TargetQName = libraryName.
//   - Pass 2 (resolveUsingForRef in resolve.go) resolves libraryName
//     against byName[NodeContract] (libraries are emitted as
//     NodeContract+SubKind="library" by W4) and emits one EdgeUsesFor
//     edge per match. same-file → ConfExtracted, cross-file →
//     ConfInferred, unresolved → drop.

// runUsingFor walks every `using_directive` match nested inside a
// contract / library / interface body and queues two PendingRefs per
// directive (V1.0 addition 2026-05-12):
//
//  1. dispatchKindUsingFor (V0) — TargetQName=libraryName. Drives the
//     EdgeUsesFor (Contract → Library) emission in Pass 2.
//  2. dispatchKindUsingForTypeBind (V1.0) — TargetQName encodes
//     `<libraryName>|<typeName>`. Carries the bound type so Pass 2 can
//     build a per-contract binding map for method-call resolution.
//     Does not produce a graph edge — pure side-channel data.
//
// Container identifier comes from the same query capture so both
// PendingRefs' SrcID line up with the container's existing node ID.
func (v *declVisitor) runUsingFor() {
	query, qErr := sitter.NewQuery(v.lang, queryUsingFor)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		var containerNode *sitter.Node
		var libNode *sitter.Node
		var typeNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "container":
				n := c.Node
				containerNode = &n
			case "lib":
				n := c.Node
				libNode = &n
			case "type":
				n := c.Node
				typeNode = &n
			}
		}
		if containerNode == nil || libNode == nil {
			continue
		}
		// SrcID must align with the container node emitted by
		// runContractDecl / runLibraryDecl / runInterfaceDecl — all
		// hash on (name, "sol", name-node startByte). containerNode is
		// the same `@name` identifier these emit paths use.
		containerName := containerNode.Utf8Text(v.src)
		containerStart := int(containerNode.StartByte())
		srcID := parse.MakeID(containerName, "sol", containerStart)
		libName := libNode.Utf8Text(v.src)
		// W-C W6 V1.29 (2026-05-12): skip leading namespace-alias
		// identifiers. tree-sitter emits one query match per
		// type_alias child identifier, so `using L.SafeMath for ...`
		// fans out into two matches (libName="L" and libName="SafeMath").
		// The "L" match is a namespace prefix from
		// `import "./util.sol" as L` — registered in namespaceAliases
		// by runImportAliases. Without this skip Pass 2's byName
		// lookup could surface an unrelated contract named L as a
		// false-positive EdgeUsesFor.
		if v.namespaceAliases[libName] {
			continue
		}
		// W-C W6 V1.28 (2026-05-12): apply per-file alias map populated
		// by runImportAliases. `import {SafeMath as SM} from "..."` +
		// `using SM for ...` → SM is resolved to SafeMath here so the
		// downstream binding pipeline keys match the actual library
		// node name.
		if orig, hit := v.importAliases[libName]; hit {
			libName = orig
		}
		line := int(libNode.StartPosition().Row) + 1
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        srcID,
			EdgeType:     types.EdgeUsesFor,
			TargetQName:  libName,
			Line:         line,
			DispatchKind: dispatchKindUsingFor,
		})
		// V1.0 typebind PendingRef. Source field is either type_name
		// (specific binding) or any_source_type (`for *` wildcard); we
		// normalise both into a string token used as the bind-map key
		// (typeName "*" for wildcard, raw text otherwise — matched
		// against NodeField.Signature in Pass 2).
		typeName := normaliseUsingForType(typeNode, v.src)
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        srcID,
			EdgeType:     types.EdgeUsesFor, // unused for typebind — Resolve routes by DispatchKind
			TargetQName:  libName + "|" + typeName,
			Line:         line,
			DispatchKind: dispatchKindUsingForTypeBind,
		})
	}
}

// runFileLevelUsingFor — W-C W6 V2.18 (2026-05-17). ERROR-tolerant
// recovery for file-level `using LibName for T [global];` directives
// (V2.16 row 1 closure). Sol 0.8.13+ syntax is grammar-blocked in
// vendored tree-sitter-solidity v1.2.11 — the directive parses into
// a single ERROR child of source_file with the `using` keyword
// misclassified as an identifier inside a fake user_defined_type.
//
// AST shape this walks (per V2.18 probe 2026-05-17):
//
//	source_file
//	  ERROR "using SafeMath for uint256 [global];"
//	    type_name
//	      user_defined_type
//	        identifier "using"          ← keyword misinterpreted
//	    identifier "<LibName>"          ← library name (recoverable)
//	    type_name
//	      primitive_type / user_defined_type / mapping  ← bound type
//	    [identifier "global"]           ← optional qualifier
//
// Recovery heuristic:
//   - Only ERROR children of source_file (not nested ERRORs).
//   - text must start with "using " (case-sensitive Sol keyword).
//   - Library name = first identifier child whose text != "using".
//   - Bound type = first type_name child (skips the fake one
//     wrapping `using`) — extractTypeNameText handles all variants.
//
// Per-container fan-out: Sol file-level using semantics ("applies to
// all contracts in this file", whether or not the `global` qualifier
// is present) mean we emit one PendingRef pair per container node in
// v.nodes (NodeContract / NodeLibrary / NodeInterface). Each emit
// produces the same (dispatchKindUsingFor + dispatchKindUsingForTypeBind)
// pair that runUsingFor produces for contract-body directives — so
// downstream binding and dispatch resolution paths reuse the existing
// infrastructure unchanged.
//
// Must run after runContractDecl / runLibraryDecl / runInterfaceDecl
// so v.nodes is populated. visit() schedules it right after
// runUsingFor.
//
// Skip silently when:
//   - text doesn't start with "using " (some other ERROR).
//   - no identifier (other than "using") found (malformed beyond
//     recovery).
//   - no type_name found (incomplete directive).
//
// Trade-offs:
//   - `global` / non-global distinction is not preserved on the edge
//     (Sol semantics make file-level binding apply to all contracts
//     either way; the qualifier only matters for cross-file scope,
//     which V0 cross-file resolution already handles via NodeFile).
//   - This is a recovery walker, not a real parser. If the vendored
//     grammar upgrades to support file-level using natively, the
//     resulting using_directive nodes will be picked up by runUsingFor
//     (whose 3-arm query covers contract / library / interface bodies
//     but NOT source_file scope yet — a future query addition would
//     pick them up there too). At that point this walker can be
//     retired or hardened to a NOP guard.
func (v *declVisitor) runFileLevelUsingFor() {
	if v.root == nil {
		return
	}
	// Collect every container ID emitted by prior runContractDecl /
	// runLibraryDecl / runInterfaceDecl. File-level binding fans out
	// to each per Sol semantics.
	// Libraries are also NodeContract (SubKind="library" per W4 /
	// abstract_library.go) but they shouldn't bind to themselves —
	// filter them out so we don't get `Math → Math` phantom edges.
	var containerIDs []string
	for _, n := range v.nodes {
		switch n.Type {
		case types.NodeContract:
			if n.SubKind != "library" {
				containerIDs = append(containerIDs, n.ID)
			}
		case types.NodeInterface:
			containerIDs = append(containerIDs, n.ID)
		}
	}
	if len(containerIDs) == 0 {
		return
	}
	for i := uint(0); i < v.root.NamedChildCount(); i++ {
		child := v.root.NamedChild(i)
		if child == nil || child.Kind() != "ERROR" {
			continue
		}
		text := child.Utf8Text(v.src)
		if !strings.HasPrefix(text, "using ") {
			continue
		}
		var libName, typeName string
		for j := uint(0); j < child.NamedChildCount(); j++ {
			grand := child.NamedChild(j)
			if grand == nil {
				continue
			}
			switch grand.Kind() {
			case "identifier":
				t := grand.Utf8Text(v.src)
				if t != "using" && libName == "" {
					libName = t
				}
			case "type_name":
				// Skip the fake type_name wrapping the `using` keyword.
				// The recoverable bound type comes after the library
				// identifier in source order.
				if libName != "" && typeName == "" {
					typeName = extractTypeNameText(grand, v.src)
				}
			}
		}
		if libName == "" || typeName == "" {
			continue
		}
		// Apply the same alias normalisations runUsingFor does so
		// downstream binding keys line up regardless of which walker
		// recovered the directive.
		if v.namespaceAliases[libName] {
			continue
		}
		if orig, hit := v.importAliases[libName]; hit {
			libName = orig
		}
		line := int(child.StartPosition().Row) + 1
		byteOff := int(child.StartByte())
		for _, srcID := range containerIDs {
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        srcID,
				EdgeType:     types.EdgeUsesFor,
				TargetQName:  libName,
				Line:         line,
				ByteOffset:   byteOff,
				DispatchKind: dispatchKindUsingFor,
			})
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        srcID,
				EdgeType:     types.EdgeUsesFor, // routed by DispatchKind in Pass 2
				TargetQName:  libName + "|" + typeName,
				Line:         line,
				ByteOffset:   byteOff,
				DispatchKind: dispatchKindUsingForTypeBind,
			})
		}
	}
}

// normaliseUsingForType returns the bind-map key for the source field of
// a using_directive. Handles three cases:
//
//   - any_source_type (`*` wildcard) → "*" sentinel.
//   - type_name wrapping primitive_type / user_defined_type → the
//     declared type text (matches NodeField.Signature output of
//     extractTypeNameText).
//   - nil or unknown shape → "" (binding map entry is created but won't
//     match any real receiver — Pass 2 binding lookup naturally drops).
func normaliseUsingForType(typeNode *sitter.Node, src []byte) string {
	if typeNode == nil {
		return ""
	}
	if typeNode.Kind() == "any_source_type" {
		return "*"
	}
	// type_name shape — same idiom as extractTypeNameText so the
	// stored binding key compares 1:1 against NodeField.Signature.
	return extractTypeNameText(typeNode, src)
}

// runUsingForCalls — W6 V1.0 method-call dispatch detector (2026-05-12).
// Scans every member_expression that fits the `<identifier>.<identifier>(...)`
// shape (state-variable receiver, V0 limitation §4.6.6 Q9-2 (a)) and
// queues a PendingRef tagged dispatchKindUsingForCall. Pass 2 resolves
// these against the (contractID, typeName) → libraryName binding map
// built from dispatchKindUsingForTypeBind refs.
//
// Predicate: object is identifier, property is identifier, the
// member_expression's parent (or grandparent through `expression`) is a
// call_expression. Anything else (chained calls, parenthesised receivers,
// type casts) is V1.1 follow-up.
//
// Encoding: TargetQName=`<receiverName>|<methodName>`. Pass 2 splits on
// `|`, resolves receiverName against the state-var name table, joins
// with the binding map.
//
// Note: this runs *in addition to* the existing call-site emission that
// produces NodeCallSite via the body-walk passes. EdgeCalls is added
// only when binding resolution succeeds; mismatched receivers (no state
// var of that name, no binding for the type) silently drop, matching
// the strict-purge policy used by W1/W2/W3.
func (v *declVisitor) runUsingForCalls() {
	const query = `(member_expression) @member`
	q, qErr := sitter.NewQuery(v.lang, query)
	if qErr != nil {
		return
	}
	defer func() { q.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(q, v.root, v.src)
	names := q.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "member" {
				continue
			}
			memberNode := c.Node
			// W1.0/V1.1: state-variable / parameter receiver. Try first
			// because it's the most common shape.
			if receiverName, methodName, ok := matchStateVarMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  receiverName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForCall,
				})
				continue
			}
			// W6 V1.9: `this.<stateVar>.<method>(...)` — explicit-this
			// equivalent of V1.0's bare-name shape. Reuses V1.0's
			// dispatch kind + resolver (encoding is identical).
			if stateVarName, methodName, ok := matchThisReceiverMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  stateVarName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForCall,
				})
				continue
			}
			// W6 V1.13: `this.<stateVar>.<f1>.<f2>...<fN>.<method>(...)` —
			// this-prefixed nested member chain. N ≥ 1 struct-field hops
			// after `this.<stateVar>`. depth-0 (`this.<stateVar>.<method>`)
			// is V1.9. Resolver uses callerContainerID as implicit `this`
			// target — looks up stateVar in stateVarTypes only (no
			// paramTypes), then walks structFieldTypes for each hop.
			if stateVar, fields, methodName, ok := matchThisPrefixedNestedChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				encoded := stateVar
				for _, f := range fields {
					encoded += "|" + f
				}
				encoded += "|" + methodName
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  encoded,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForThisNestedChainCall,
				})
				continue
			}
			// W6 V1.10: `<obj>.<field>.<method>(...)` — struct-field
			// receiver. obj is a state-var/parameter whose type is a
			// struct; field is one of the struct's members. Resolver
			// walks structFieldTypes to find the field's type, then
			// uses that as the binding lookup key.
			if objName, fieldName, methodName, ok := matchStructFieldReceiverMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  objName + "|" + fieldName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForStructFieldCall,
				})
				continue
			}
			// W6 V1.11: `<obj>.<field1>.<field2>.<method>(...)` —
			// nested struct field receiver. obj's struct has a nested
			// struct field; field2 is a member of that nested struct.
			// Resolver walks structFieldTypes twice.
			if objName, f1, f2, methodName, ok := matchNestedStructFieldReceiverMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  objName + "|" + f1 + "|" + f2 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForNestedStructFieldCall,
				})
				continue
			}
			// W6 V1.12: generic member-chain walker — fallback for
			// arbitrary-depth (≥ 3) pure member access chains.
			// V1.10/V1.11 catch depth 1/2 hardcoded.
			if objName, fields, methodName, ok := matchGenericMemberChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				encoded := objName
				for _, f := range fields {
					encoded += "|" + f
				}
				encoded += "|" + methodName
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  encoded,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForGenericMemberChainCall,
				})
				continue
			}
			// V1.3: chained call shape `<fn>().<method>(...)`. Inner
			// expression is a plain function call (function-position
			// identifier); resolver looks up the inner function's
			// return type and treats it as the receiver type.
			if innerFnName, methodName, ok := matchChainedMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  innerFnName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForChainCall,
				})
				continue
			}
			// V1.4: cross-contract chained shape `<obj>.<innerFn>().<method>(...)`.
			// Inner expression is a member call on a state-var / parameter
			// receiver. Resolver follows the receiver's contract type to
			// look up the inner function's declaration, then uses that
			// function's return type as the V1.3-style chain receiver.
			if receiverObj, innerFnName, methodName, ok := matchCrossContractChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  receiverObj + "|" + innerFnName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForCrossChainCall,
				})
				continue
			}
			// V1.5: depth-2 chained shape `<innerFn1>().<innerFn2>().<method>(...)`.
			// Each chain link is a plain function call (function-position
			// identifier at the innermost level). Resolver walks two
			// levels of funcReturnTypes to reach the binding type.
			if innerFn1, innerFn2, methodName, ok := matchDeepChainedMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  innerFn1 + "|" + innerFn2 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForDeepChainCall,
				})
				continue
			}
			// V1.6: deep cross-contract chained shape
			// `<obj>.<innerFn1>().<innerFn2>().<method>(...)`. Combines
			// V1.4 (receiver-typed inner method) with V1.5 (depth-2
			// chain). Resolver walks: receiverObj → receiverType →
			// innerFn1 in receiverType → returnType1 → innerFn2 in
			// returnType1 → returnType2 → library binding.
			if recvObj, innerFn1, innerFn2, methodName, ok := matchDeepCrossContractChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  recvObj + "|" + innerFn1 + "|" + innerFn2 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForDeepCrossChainCall,
				})
				continue
			}
			// V1.7: depth-3 same-contract chained shape
			// `<innerFn1>().<innerFn2>().<innerFn3>().<method>(...)`.
			// Three levels of funcReturnTypes walks. V1.8+ promotes this
			// to a generic depth-N walker.
			if innerFn1, innerFn2, innerFn3, methodName, ok := matchTripleChainedMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  innerFn1 + "|" + innerFn2 + "|" + innerFn3 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForTripleChainCall,
				})
				continue
			}
			// V1.8: generic iterative walker — fallback for chains
			// deeper than V1.7's hardcoded patterns. Covers depth ≥ 4
			// same-contract chains and depth ≥ 3 cross-contract chains.
			// V1.3-V1.7 catch shorter shapes via earlier dispatch
			// branches; V1.8 only fires when the previous predicates
			// have rejected.
			if mode, recvObj, segs, methodName, ok := matchGenericChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				// Encode as `mode|<recv>|<seg1>|...|<segN>|method`
				// (recv part is empty string for same-contract — kept
				// in the encoding so the resolver's SplitN positions
				// remain consistent across modes).
				encoded := mode + "|" + recvObj
				for _, s := range segs {
					encoded += "|" + s
				}
				encoded += "|" + methodName
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  encoded,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindUsingForGenericChainCall,
				})
				continue
			}
		}
	}
}

// matchThisPrefixedNestedChain — W-C W6 V1.13 (2026-05-12). Tests
// whether a member_expression fits the this-prefixed nested chain
// shape `this.<stateVar>.<f1>.<f2>....<fN>.<method>(...)` for N ≥ 1
// struct-field hops after `this.<stateVar>`.
//
// V1.9 already catches depth-0 (`this.<stateVar>.<method>`); V1.13
// only fires for N ≥ 1.
//
// Returns (stateVar, fields, methodName, true) on match. fields[0] is
// the innermost hop (immediately after stateVar); fields[N-1] is the
// outermost hop (immediately before method).
//
// AST shape (N = 2, `this.x.f1.f2.method`):
//
//	call_expression                            ← outer .method(...)
//	  function: expression
//	    member_expression                      ← outer
//	      object: expression
//	        member_expression                  ← this.x.f1.f2
//	          object: expression
//	            member_expression              ← this.x.f1
//	              object: expression
//	                member_expression          ← this.x
//	                  object: identifier "this"
//	                  property: identifier (stateVar)
//	              property: identifier (f1)
//	          property: identifier (f2)
//	      property: identifier (methodName)
//
// Disambiguation:
//   - V1.9 catches depth-0 (matchThisReceiverMethodCall fires first).
//   - V1.10/V1.11/V1.12 explicitly reject `this` as innermost object —
//     V1.13 is the dedicated cousin for the this-prefixed shape.
//
// Caller dispatch order: state-var → V1.9 → V1.13 → V1.10 → V1.11 →
// V1.12 → V1.3-V1.8.
func matchThisPrefixedNestedChain(member *sitter.Node, src []byte) (string, []string, string, bool) {
	if member == nil {
		return "", nil, "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", nil, "", false
	}
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", nil, "", false
	}
	methodName := property.Utf8Text(src)
	// Walk back through the member-expression chain, collecting properties
	// outer-first. Bail when we hit a non-member, non-`this` innermost.
	var revFields []string
	cur := unwrapExpression(member.ChildByFieldName("object"))
	for cur != nil && cur.Kind() == "member_expression" {
		curProp := cur.ChildByFieldName("property")
		if curProp == nil || curProp.Kind() != "identifier" {
			return "", nil, "", false
		}
		revFields = append(revFields, curProp.Utf8Text(src))
		innerObj := unwrapExpression(cur.ChildByFieldName("object"))
		if innerObj == nil {
			return "", nil, "", false
		}
		if innerObj.Kind() == "identifier" {
			if innerObj.Utf8Text(src) != "this" {
				return "", nil, "", false
			}
			// revFields collected outer→inner. V1.13 floor is N ≥ 1
			// struct hop: revFields length ≥ 2 (1 stateVar + ≥ 1 field).
			// length == 1 → V1.9 territory; reject.
			if len(revFields) < 2 {
				return "", nil, "", false
			}
			rev := reverseStrSlice(revFields)
			return rev[0], rev[1:], methodName, true
		}
		if innerObj.Kind() == "member_expression" {
			cur = innerObj
			continue
		}
		return "", nil, "", false
	}
	return "", nil, "", false
}

// matchGenericMemberChain — W-C W6 V1.12 (2026-05-12). Generic
// iterative walker for arbitrary-depth pure member access chain
// `<obj>.<field1>.<field2>....<fieldN>.<method>(...)`. No call
// expressions in between — pure struct field traversal.
//
// Used as fallback after V1.10/V1.11 hardcoded predicates have
// rejected (depth ≥ 3 member chain).
//
// Returns (objName, fields, methodName, true) on match. fields[0] is
// the innermost field (closest to obj), fields[N-1] is the outermost.
//
// Disambiguation: V1.10 catches depth-1, V1.11 catches depth-2.
// V1.12 only fires when depth ≥ 3. this-prefixed nested chain drops
// (consistent with V1.10/V1.11 — "this" handled by V1.9 cousin).
func matchGenericMemberChain(member *sitter.Node, src []byte) (string, []string, string, bool) {
	if member == nil {
		return "", nil, "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", nil, "", false
	}
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", nil, "", false
	}
	methodName := property.Utf8Text(src)
	var revFields []string
	cur := unwrapExpression(member.ChildByFieldName("object"))
	for cur != nil && cur.Kind() == "member_expression" {
		curProp := cur.ChildByFieldName("property")
		if curProp == nil || curProp.Kind() != "identifier" {
			return "", nil, "", false
		}
		revFields = append(revFields, curProp.Utf8Text(src))
		innerObj := unwrapExpression(cur.ChildByFieldName("object"))
		if innerObj == nil {
			return "", nil, "", false
		}
		if innerObj.Kind() == "identifier" {
			objText := innerObj.Utf8Text(src)
			if objText == "this" {
				return "", nil, "", false
			}
			if len(revFields) < 3 {
				return "", nil, "", false
			}
			return objText, reverseStrSlice(revFields), methodName, true
		}
		if innerObj.Kind() == "member_expression" {
			cur = innerObj
			continue
		}
		return "", nil, "", false
	}
	return "", nil, "", false
}

// matchNestedStructFieldReceiverMethodCall — W-C W6 V1.11 (2026-05-12).
// Tests whether a member_expression fits the depth-2 nested struct
// field receiver shape `<obj>.<field1>.<field2>.<method>(...)`. obj is
// state-var/parameter whose declared type is a struct; field1 is a
// nested-struct member of that type; field2 is a member of field1's
// struct type; method is the using-for dispatch target.
//
// Returns (objName, field1Name, field2Name, methodName, true) on match.
//
// AST shape (4 levels of member_expression nesting, no call_expressions
// in between — pure member access chain):
//
//	call_expression                              ← outer .method(...)
//	  function: expression
//	    member_expression                        ← outer
//	      object: expression
//	        member_expression                    ← mid <obj>.<field1>.<field2>
//	          object: expression
//	            member_expression                ← inner <obj>.<field1>
//	              object: identifier (objName)
//	              property: identifier (field1Name)
//	          property: identifier (field2Name)
//	      property: identifier (methodName)
//
// Disambiguation:
//   - V1.10 (`<obj>.<field>.<method>`): one less member layer.
//   - V1.4 (`<obj>.<fn>().<method>`): middle layer is call_expression,
//     not member_expression.
//   - V1.9 (`this.<state-var>.<method>`): innermost is `this`, not
//     state-var/parameter identifier.
//
// Caller dispatch order: state-var → V1.9 → V1.10 → V1.11 → V1.3-V1.8.
func matchNestedStructFieldReceiverMethodCall(member *sitter.Node, src []byte) (string, string, string, string, bool) {
	if member == nil {
		return "", "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", "", false
	}
	object := member.ChildByFieldName("object")
	midMember := unwrapExpression(object)
	if midMember == nil || midMember.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	midProperty := midMember.ChildByFieldName("property")
	if midProperty == nil || midProperty.Kind() != "identifier" {
		return "", "", "", "", false
	}
	midObject := midMember.ChildByFieldName("object")
	innerMember := unwrapExpression(midObject)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", "", "", false
	}
	objText := innerObjIdent.Utf8Text(src)
	if objText == "this" {
		// `this.<field1>.<field2>.<method>` — V1.11.x cousin of V1.9,
		// covered when V1.10's `this` -> state-var idiom isn't enough.
		// V1.11 V0 only handles non-this receivers to keep scope tight;
		// `this.<field1>.<field2>.<method>` is V1.12+.
		return "", "", "", "", false
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", "", "", false
	}
	// Outer must be invoked.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	return objText,
		innerProperty.Utf8Text(src),
		midProperty.Utf8Text(src),
		property.Utf8Text(src),
		true
}

// matchStructFieldReceiverMethodCall — W-C W6 V1.10 (2026-05-12). Tests
// whether a member_expression fits the struct-field-receiver shape
// `<obj>.<field>.<method>(...)` where:
//   - `<obj>` is an identifier (state-var or parameter receiver).
//   - `<obj>` is NOT "this" — that's V1.9.
//   - `<field>` is a struct member name (resolver verifies against the
//     struct field type index).
//   - `<method>` is the using-for dispatch target.
//
// Returns (objName, fieldName, methodName, true) on match.
//
// AST shape (same as V1.9 except inner.object is any non-"this"
// identifier):
//
//	call_expression                            ← outer .method(...)
//	  function: expression
//	    member_expression                      ← outer
//	      object: expression
//	        member_expression                  ← inner <obj>.<field>
//	          object: identifier (objName, != "this")
//	          property: identifier (fieldName)
//	      property: identifier (method)
//
// Disambiguation:
//   - V1.9 (`this.<field>.<method>`): caller dispatch tries V1.9 first;
//     V1.9's matchThisReceiverMethodCall requires inner.object text =
//     "this", so V1.10 only fires for non-this receivers.
//   - V1.4 (`<obj>.<fn>().<method>`): V1.4's middle is a call, not a
//     member access. Shape-disjoint.
//
// Caller dispatch order: state-var → V1.9 → V1.10 → V1.3-V1.8.
func matchStructFieldReceiverMethodCall(member *sitter.Node, src []byte) (string, string, string, bool) {
	if member == nil {
		return "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", false
	}
	object := member.ChildByFieldName("object")
	innerMember := unwrapExpression(object)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", "", false
	}
	objText := innerObjIdent.Utf8Text(src)
	if objText == "this" {
		return "", "", "", false // V1.9 handles this case
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", "", false
	}
	// Outer must be invoked.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", false
	}
	return objText, innerProperty.Utf8Text(src), property.Utf8Text(src), true
}

// matchThisReceiverMethodCall — W-C W6 V1.9 (2026-05-12). Tests whether
// a member_expression fits the `this.<stateVar>.<method>(...)` shape.
// `this` is implicit current-contract reference; the resolver treats
// `<stateVar>` as a state variable on the caller's container — same
// dispatch as V1.0's bare-name shape `<stateVar>.<method>(...)`.
//
// Returns (stateVarName, methodName, true) on match.
//
// AST shape:
//
//	call_expression                         ← outer .method(...)
//	  function: expression
//	    member_expression                   ← outer
//	      object: expression
//	        member_expression               ← inner this.<stateVar>
//	          object: identifier "this"
//	          property: identifier (stateVarName)
//	      property: identifier (method)
//
// Reuses V1.0's dispatch kind + resolver: PendingRef encoding
// `<stateVarName>|<method>` is identical to V1.0's, so the resolver
// path is single-source. V1.9 only adds a new predicate; no new
// resolver helper required.
//
// Caller dispatch order ensures simpler V1.0 (bare `<x>.<method>`)
// claims the call when shapes don't include `this`. V1.9 sits after
// V1.7's hardcoded predicates and before V1.8's generic walker —
// `this.x.method` would also match V1.4's cross-contract pattern
// structurally (innerObj=identifier "this"), but V1.4 would look up
// "this" in stateVarTypes / paramTypes and miss; V1.9 short-circuits
// that wasted lookup by recognising the `this` keyword directly.
func matchThisReceiverMethodCall(member *sitter.Node, src []byte) (string, string, bool) {
	if member == nil {
		return "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", false
	}
	object := member.ChildByFieldName("object")
	innerMember := unwrapExpression(object)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", false
	}
	if innerObjIdent.Utf8Text(src) != "this" {
		return "", "", false
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", false
	}
	// Outer must be invoked.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", false
	}
	return innerProperty.Utf8Text(src), property.Utf8Text(src), true
}

// matchStateVarMethodCall tests whether a member_expression fits the
// state-variable method-call shape `<identifier>.<identifier>` AND its
// parent context is a call_expression (i.e. it's actually being called,
// not just member-accessed for a property read).
//
// Returns (receiverName, methodName, true) on match. Rejects chained
// shapes (`foo().bar`, `IFoo(x).bar`), member receivers (`a.b.c`), and
// pure property reads (`obj.field` outside a call) — those are V1.1
// follow-up or not in scope.
func matchStateVarMethodCall(member *sitter.Node, src []byte) (string, string, bool) {
	if member == nil {
		return "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", false
	}
	object := member.ChildByFieldName("object")
	innerObj := unwrapExpression(object)
	if innerObj == nil || innerObj.Kind() != "identifier" {
		return "", "", false
	}
	// Must be the function-position of a call_expression. The member
	// node is wrapped in an expression node, which is itself a child of
	// the call_expression's `function` field.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", false
	}
	return innerObj.Utf8Text(src), property.Utf8Text(src), true
}

// matchDeepChainedMethodCall — W-C W6 V1.5 (2026-05-12). Tests whether
// a member_expression fits the depth-2 chained shape
// `<innerFn1>().<innerFn2>().<method>(...)`. Each link in the chain is
// a plain function call whose function-position is an identifier; the
// chain unwinds left-to-right by resolving each function's return type.
//
// Returns (innerFn1, innerFn2, method, true) on match.
//
// AST shape:
//
//	call_expression                                  ← outer .method(...)
//	  function: expression
//	    member_expression                            ← outer
//	      object: expression
//	        call_expression                          ← middle .innerFn2(...)
//	          function: expression
//	            member_expression                    ← middle
//	              object: expression
//	                call_expression                  ← inner .innerFn1(...)
//	                  function: expression
//	                    identifier (innerFn1)        ← V1.5 captures this
//	              property: identifier (innerFn2)
//	      property: identifier (method)
//
// Rejects:
//   - `obj.foo().bar().baz()` — innermost identifier is preceded by
//     `obj.` (member_expression). That's V1.6+ (deep cross-contract).
//   - depth >= 3 (`f().g().h().i()`) — outer's chain has one more
//     layer wrapping than V1.5 handles. V1.6+.
//
// Disambiguation from V1.4 (`<obj>.<innerFn>().<method>`): V1.4 has
// inner member_expression.object = identifier (state-var / param); V1.5
// has inner member_expression.object = call_expression (further chain).
// Caller dispatches in order (V1.4 first, then V1.5) so V1.4's predicate
// claims the call when shapes overlap on simpler chains.
func matchDeepChainedMethodCall(member *sitter.Node, src []byte) (string, string, string, bool) {
	if member == nil {
		return "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", false
	}
	object := member.ChildByFieldName("object")
	middleCall := unwrapExpression(object)
	if middleCall == nil || middleCall.Kind() != "call_expression" {
		return "", "", "", false
	}
	middleFn := middleCall.ChildByFieldName("function")
	middleMember := unwrapExpression(middleFn)
	if middleMember == nil || middleMember.Kind() != "member_expression" {
		return "", "", "", false
	}
	middleProperty := middleMember.ChildByFieldName("property")
	if middleProperty == nil || middleProperty.Kind() != "identifier" {
		return "", "", "", false
	}
	middleObject := middleMember.ChildByFieldName("object")
	innerCall := unwrapExpression(middleObject)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerIdent := unwrapExpression(innerFn)
	if innerIdent == nil || innerIdent.Kind() != "identifier" {
		return "", "", "", false
	}
	// Outer must itself be the function-position of a call_expression.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", false
	}
	return innerIdent.Utf8Text(src), middleProperty.Utf8Text(src), property.Utf8Text(src), true
}

// matchGenericChain — W-C W6 V1.8 (2026-05-12). Generic iterative
// walker for arbitrary-depth chained dispatch. Used as the final
// fallback after V1.3-V1.7's hardcoded predicates have rejected.
//
// Two chain shapes recognised:
//
//   - same-contract: `<fn1>().<fn2>()....<fnN>().<method>(...)`
//     The innermost function-position is a plain identifier (local
//     function reference). Returns mode="same", recvObj="".
//
//   - cross-contract: `<obj>.<fn1>().<fn2>()....<fnN>().<method>(...)`
//     The innermost function-position is a member_expression whose
//     object is a plain identifier (receiver var / parameter).
//     Returns mode="cross", recvObj=identifier text.
//
// Segments returned in source order: segs[0] is the innermost chain
// link (called first in the runtime chain), segs[N-1] is the outermost
// chain link (called last before method).
//
// Returns (mode, recvObj, segs, method, true) on match. Drops shapes:
//   - empty chain (`<obj>.<method>(...)` — covered by V1.0/V1.1).
//   - non-call-expression inner shapes (`obj.field.method()`).
//   - outermost wrapper not a call_expression (member-read, not call).
//
// Caller dispatch order ensures V1.8 only fires after V1.3-V1.7 reject:
// V1.3 catches depth-1 same-contract, V1.5 depth-2, V1.7 depth-3,
// V1.4 depth-1 cross-contract, V1.6 depth-2 cross-contract. V1.8
// handles depth ≥ 4 same-contract and depth ≥ 3 cross-contract.
func matchGenericChain(member *sitter.Node, src []byte) (string, string, []string, string, bool) {
	if member == nil {
		return "", "", nil, "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", nil, "", false
	}
	// Outer must be invoked (it's a method call, not just a read).
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", nil, "", false
	}
	method := property.Utf8Text(src)
	// Walk down: outer.object → call → fn → ...
	// segs collected in REVERSE order (outermost first), reversed at end.
	var revSegs []string
	cur := unwrapExpression(member.ChildByFieldName("object"))
	for cur != nil && cur.Kind() == "call_expression" {
		fn := unwrapExpression(cur.ChildByFieldName("function"))
		if fn == nil {
			return "", "", nil, "", false
		}
		switch fn.Kind() {
		case "identifier":
			// Innermost — same-contract chain. Done.
			fnName := fn.Utf8Text(src)
			revSegs = append(revSegs, fnName)
			return "same", "", reverseStrSlice(revSegs), method, true
		case "member_expression":
			seg := fn.ChildByFieldName("property")
			if seg == nil || seg.Kind() != "identifier" {
				return "", "", nil, "", false
			}
			revSegs = append(revSegs, seg.Utf8Text(src))
			innerObj := unwrapExpression(fn.ChildByFieldName("object"))
			if innerObj == nil {
				return "", "", nil, "", false
			}
			if innerObj.Kind() == "identifier" {
				// Innermost member.object is identifier — cross-contract.
				return "cross", innerObj.Utf8Text(src), reverseStrSlice(revSegs), method, true
			}
			if innerObj.Kind() == "call_expression" {
				cur = innerObj
				continue
			}
			// Unknown inner shape (e.g. member-of-member `a.b.c`) — V1.x.
			return "", "", nil, "", false
		default:
			return "", "", nil, "", false
		}
	}
	return "", "", nil, "", false
}

// reverseStrSlice reverses a string slice in place and returns it.
// Helper for matchGenericChain — segments collected outermost-first
// during the walk, reversed at the end so callers see source order.
func reverseStrSlice(s []string) []string {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// matchTripleChainedMethodCall — W-C W6 V1.7 (2026-05-12). Tests
// whether a member_expression fits the depth-3 chained shape
// `<innerFn1>().<innerFn2>().<innerFn3>().<method>(...)`. Same-contract
// chain only (cross-contract variant + generic depth-N walker are
// V1.8+).
//
// Returns (innerFn1, innerFn2, innerFn3, method, true) on match.
//
// AST shape (4 levels of call_expression / member_expression nesting):
//
//	call_expression                           ← outer .method(...)
//	  function: expression
//	    member_expression                     ← outer
//	      object: expression
//	        call_expression                   ← L3 .innerFn3(...)
//	          function: expression
//	            member_expression             ← L3
//	              object: expression
//	                call_expression           ← L2 .innerFn2(...)
//	                  function: expression
//	                    member_expression     ← L2
//	                      object: expression
//	                        call_expression   ← L1 .innerFn1(...)
//	                          function: expression
//	                            identifier (innerFn1)
//	                      property: identifier (innerFn2)
//	              property: identifier (innerFn3)
//	      property: identifier (method)
//
// Disambiguation:
//   - V1.5 (`a().b().c()`): one less call_expression nesting layer.
//   - V1.6 (`obj.a().b().c()`): same nesting depth but innermost
//     function-position is member_expression with identifier receiver,
//     not bare identifier.
//   - V1.8+: even deeper chains, generic walker.
//
// Caller dispatch order (state-var → V1.3 → V1.4 → V1.5 → V1.6 → V1.7)
// ensures simpler predicates reject before V1.7 fires.
func matchTripleChainedMethodCall(member *sitter.Node, src []byte) (string, string, string, string, bool) {
	if member == nil {
		return "", "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", "", false
	}
	object := member.ChildByFieldName("object")
	l3Call := unwrapExpression(object)
	if l3Call == nil || l3Call.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	l3Fn := l3Call.ChildByFieldName("function")
	l3Member := unwrapExpression(l3Fn)
	if l3Member == nil || l3Member.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	l3Property := l3Member.ChildByFieldName("property")
	if l3Property == nil || l3Property.Kind() != "identifier" {
		return "", "", "", "", false
	}
	l3Object := l3Member.ChildByFieldName("object")
	l2Call := unwrapExpression(l3Object)
	if l2Call == nil || l2Call.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	l2Fn := l2Call.ChildByFieldName("function")
	l2Member := unwrapExpression(l2Fn)
	if l2Member == nil || l2Member.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	l2Property := l2Member.ChildByFieldName("property")
	if l2Property == nil || l2Property.Kind() != "identifier" {
		return "", "", "", "", false
	}
	l2Object := l2Member.ChildByFieldName("object")
	l1Call := unwrapExpression(l2Object)
	if l1Call == nil || l1Call.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	l1Fn := l1Call.ChildByFieldName("function")
	l1Ident := unwrapExpression(l1Fn)
	if l1Ident == nil || l1Ident.Kind() != "identifier" {
		return "", "", "", "", false
	}
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	return l1Ident.Utf8Text(src),
		l2Property.Utf8Text(src),
		l3Property.Utf8Text(src),
		property.Utf8Text(src),
		true
}

// matchDeepCrossContractChain — W-C W6 V1.6 (2026-05-12). Tests
// whether a member_expression fits the deep cross-contract chained
// shape `<obj>.<innerFn1>().<innerFn2>().<method>(...)`. Two-link chain
// originating from a state-var / parameter receiver.
//
// Returns (receiverObj, innerFn1, innerFn2, method, true) on match.
//
// AST shape:
//
//	call_expression                                  ← outer .method(...)
//	  function: expression
//	    member_expression                            ← outer
//	      object: expression
//	        call_expression                          ← middle .innerFn2(...)
//	          function: expression
//	            member_expression                    ← middle
//	              object: expression
//	                call_expression                  ← inner .innerFn1(...)
//	                  function: expression
//	                    member_expression            ← inner
//	                      object: expression
//	                        identifier (obj)         ← V1.6 captures
//	                      property: identifier (innerFn1)
//	              property: identifier (innerFn2)
//	      property: identifier (method)
//
// Disambiguation:
//   - V1.4 (`obj.foo().bar()`): outer's chain stops one link shorter —
//     middle.object is identifier, not call_expression.
//   - V1.5 (`foo().bar().baz()`): innermost function-position is
//     identifier, not member_expression on an identifier.
//   - V1.7+ (`obj.foo().bar().baz().qux()`): even one link deeper —
//     outer's object is yet another call_expression wrapping the V1.6
//     pattern.
//
// Caller dispatch order (state-var → V1.3 → V1.4 → V1.5 → V1.6) ensures
// V1.6 only fires on shapes the simpler predicates rejected.
func matchDeepCrossContractChain(member *sitter.Node, src []byte) (string, string, string, string, bool) {
	if member == nil {
		return "", "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", "", false
	}
	object := member.ChildByFieldName("object")
	middleCall := unwrapExpression(object)
	if middleCall == nil || middleCall.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	middleFn := middleCall.ChildByFieldName("function")
	middleMember := unwrapExpression(middleFn)
	if middleMember == nil || middleMember.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	middleProperty := middleMember.ChildByFieldName("property")
	if middleProperty == nil || middleProperty.Kind() != "identifier" {
		return "", "", "", "", false
	}
	middleObject := middleMember.ChildByFieldName("object")
	innerCall := unwrapExpression(middleObject)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerMember := unwrapExpression(innerFn)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", "", "", false
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", "", "", false
	}
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	return innerObjIdent.Utf8Text(src),
		innerProperty.Utf8Text(src),
		middleProperty.Utf8Text(src),
		property.Utf8Text(src),
		true
}

// matchCrossContractChain — W-C W6 V1.4 (2026-05-12). Tests whether a
// member_expression fits the cross-contract chained shape
// `<obj>.<innerFn>().<method>` where the outer expression is invoking
// `<method>` on the return value of a method call on a state-var /
// parameter receiver.
//
// Returns (receiverObjName, innerFnName, methodName, true) on match.
//
// AST shape (verified via probe):
//
//	call_expression                                ← outer .method(...)
//	  function: expression
//	    member_expression                          ← outer
//	      object: expression
//	        call_expression                        ← inner .innerFn(...)
//	          function: expression
//	            member_expression                  ← inner
//	              object: expression
//	                identifier (receiverObjName)   ← state var / param
//	              property: identifier (innerFnName)
//	      property: identifier (methodName)
//
// Rejects:
//   - `factory().bar()` — inner function-position is identifier, not
//     member_expression (handled by matchChainedMethodCall, V1.3).
//   - `obj.field.bar()` — inner is just member access without call
//     (V1.x property-chain support).
//   - Deeper chains like `obj.foo().baz().bar()` — outer's object is
//     itself a call_expression whose function is a member_expression on
//     another call_expression. V1.5+ recursive chains.
//
// Edge case: the receiverObjName might also match an interface variable
// (`IFoo iface; iface.fn()`) — the resolver re-checks against
// stateVarTypes / paramTypes, where the typeName recorded by V1.0/V1.1
// would be "IFoo". The resolver step 3 maps that to the interface's
// declared methods.
func matchCrossContractChain(member *sitter.Node, src []byte) (string, string, string, bool) {
	if member == nil {
		return "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", false
	}
	object := member.ChildByFieldName("object")
	innerCall := unwrapExpression(object)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerMember := unwrapExpression(innerFn)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", "", false
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", "", false
	}
	// Outer must be a call_expression (the chained `.method()` is itself
	// invoked).
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", false
	}
	return innerObjIdent.Utf8Text(src), innerProperty.Utf8Text(src), property.Utf8Text(src), true
}

// matchChainedMethodCall — W-C W6 V1.3 (2026-05-12). Tests whether a
// member_expression fits the chained-call shape `<fn>(...).<method>`
// where the inner expression is a *plain function call* (bare
// identifier in the function position), not a type cast.
//
// Returns (innerFnName, methodName, true) on match.
//
// Shape (verified via AST dump):
//
//	call_expression                       ← outer .method(...)
//	  function: expression
//	    member_expression
//	      object: expression
//	        call_expression               ← inner fn() call
//	          function: expression
//	            identifier (innerFnName)  ← what V1.3 captures
//	      property: identifier (methodName)
//
// Rejects shapes already covered or out of scope for V1.3:
//   - `IFoo(addr).bar()` (W3 interface dispatch — inner identifier is
//     a TYPE name; V3's matchInterfaceDispatch handles this).
//   - `obj.foo().bar()` (member-receiver chain — inner function-position
//     is itself a member_expression, not a bare identifier). V1.4+.
//   - `obj.field.bar()` (pure property access chain). V1.4+.
//
// V1.3 vs W3 disambiguation: the runUsingForCalls walker calls this
// AFTER matchStateVarMethodCall has rejected. To avoid emitting both a
// W3 EdgeInvokes and a V1.3 EdgeCalls for the same site, callers should
// also verify the resolved inner identifier maps to a *function* (not
// an interface) — that's Pass 2's job via funcByQName, so the parser
// emits the chained PendingRef unconditionally and the resolver drops
// when no funcID matches.
func matchChainedMethodCall(member *sitter.Node, src []byte) (string, string, bool) {
	if member == nil {
		return "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", false
	}
	object := member.ChildByFieldName("object")
	innerCall := unwrapExpression(object)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerIdent := unwrapExpression(innerFn)
	if innerIdent == nil || innerIdent.Kind() != "identifier" {
		return "", "", false
	}
	// Outer must be a call_expression (the chained `.method()` is itself
	// invoked, not just read as a property).
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", false
	}
	return innerIdent.Utf8Text(src), property.Utf8Text(src), true
}

// dispatchKindUsingFor tags PendingRefs originating from W6 V0 using-for
// binding detection. String literal matches the existing idiom (W1
// "inherit", W2 "override"/"override_explicit", W3 "interface_dispatch").
const dispatchKindUsingFor = "using_for"

// dispatchKindUsingForTypeBind (V1.0) carries the bound-type information
// for binding-map construction. Does not produce a graph edge — Pass 2
// reads these to populate (contractID, typeName) → libraryName map.
const dispatchKindUsingForTypeBind = "using_for_typebind"

// dispatchKindUsingForCall (V1.0) tags PendingRefs that resolve to
// EdgeCalls (caller function → library function) once the binding map
// has been built. TargetQName encodes `<receiverName>|<methodName>`.
const dispatchKindUsingForCall = "using_for_call"

// dispatchKindUsingForChainCall (V1.3) tags PendingRefs for chained
// call dispatch: `<fn>().<method>(...)`. TargetQName encodes
// `<innerFnName>|<methodName>` — same shape as using_for_call but the
// receiver lookup goes through funcReturnTypes instead of stateVarTypes
// / paramTypes (Pass 2 splits the resolver paths by DispatchKind).
const dispatchKindUsingForChainCall = "using_for_chain_call"

// dispatchKindUsingForCrossChainCall (V1.4) tags PendingRefs for
// cross-contract chained dispatch: `<obj>.<innerFn>().<method>(...)`.
// TargetQName encodes `<receiverObjName>|<innerFnName>|<methodName>`
// (three parts, '|'-separated). Pass 2 splits the chain across
// stateVarTypes / paramTypes (obj→type) + byName (type→contract) +
// funcByQName (contract.innerFn→funcID) + funcReturnTypes (funcID→
// returnType) + bindings (callerContractID, returnType→library).
const dispatchKindUsingForCrossChainCall = "using_for_cross_chain_call"

// dispatchKindUsingForDeepChainCall (V1.5) tags PendingRefs for depth-2
// chained dispatch: `<innerFn1>().<innerFn2>().<method>(...)`.
// TargetQName encodes `<innerFn1>|<innerFn2>|<method>` (three parts).
// Resolver walks two levels of funcReturnTypes (innerFn1 → returnType1
// → resolve innerFn2 in returnType1's namespace → returnType2 → binding
// lookup on returnType2) before reaching the library function.
const dispatchKindUsingForDeepChainCall = "using_for_deep_chain_call"

// dispatchKindUsingForDeepCrossChainCall (V1.6) tags PendingRefs for
// the deep cross-contract chained dispatch
// `<obj>.<innerFn1>().<innerFn2>().<method>(...)`. TargetQName encodes
// `<obj>|<innerFn1>|<innerFn2>|<method>` (four parts). Resolver
// combines V1.4 (receiver type lookup) with V1.5 (depth-2 return
// chain) — 8-step total chain.
const dispatchKindUsingForDeepCrossChainCall = "using_for_deep_cross_chain_call"

// dispatchKindUsingForTripleChainCall (V1.7) tags PendingRefs for
// depth-3 same-contract chained dispatch:
// `<innerFn1>().<innerFn2>().<innerFn3>().<method>(...)`. TargetQName
// encodes `<innerFn1>|<innerFn2>|<innerFn3>|<method>` (four parts).
// Resolver walks three levels of funcReturnTypes — V1.5's pattern
// with one more link.
const dispatchKindUsingForTripleChainCall = "using_for_triple_chain_call"

// dispatchKindUsingForStructFieldCall (V1.10) tags PendingRefs for
// struct-field-receiver dispatch `<obj>.<field>.<method>(...)`.
// TargetQName encodes `<objName>|<fieldName>|<methodName>`.
// Resolver chain: obj → typeName (stateVarTypes / paramTypes) →
// (typeName, fieldName) → fieldType (structFieldTypes) →
// (callerContractID, fieldType) → libraryName (bindings) → method.
const dispatchKindUsingForStructFieldCall = "using_for_struct_field_call"

// dispatchKindUsingForNestedStructFieldCall (V1.11) tags PendingRefs
// for depth-2 nested struct field dispatch
// `<obj>.<field1>.<field2>.<method>(...)`. TargetQName encodes
// `<objName>|<field1>|<field2>|<methodName>` (4 parts). Resolver walks
// structFieldTypes twice — obj's struct field1 → its struct's field2 →
// binding lookup on field2's type.
const dispatchKindUsingForNestedStructFieldCall = "using_for_nested_struct_field_call"

// dispatchKindUsingForThisNestedChainCall (V1.13) — this-prefixed
// nested member chain. PendingRef target encoding:
// `<stateVar>|<f1>|...|<fN>|<method>` (N ≥ 1). Resolver uses
// callerContainerID as implicit `this` target, looks up stateVar in
// stateVarTypes only (no paramTypes), then walks structFieldTypes.
const dispatchKindUsingForThisNestedChainCall = "using_for_this_nested_chain_call"

// dispatchKindUsingForGenericMemberChainCall (V1.12) — generic
// iterative member-chain dispatch (depth ≥ 3). TargetQName encodes
// `<obj>|<f1>|<f2>|...|<fN>|<method>` (variable parts). Resolver walks
// structFieldTypes N times.
const dispatchKindUsingForGenericMemberChainCall = "using_for_generic_member_chain_call"

// dispatchKindUsingForGenericChainCall (V1.8) tags PendingRefs from
// the generic iterative chain walker. Covers arbitrary-depth chains
// after V1.3-V1.7 hardcoded predicates reject — depth ≥ 4
// same-contract chains and depth ≥ 3 cross-contract chains.
//
// TargetQName encodes:
//
//	"same|<fn1>|<fn2>|...|<fnN>|<method>"  (same-contract, N ≥ 4)
//	"cross|<obj>|<fn1>|<fn2>|...|<fnN>|<method>"  (cross-contract, N ≥ 3)
//
// Resolver splits on `|`, dispatches on the leading mode token,
// iterates funcReturnTypes for each segment, and finishes with the
// using-for binding lookup on the last segment's return type.
const dispatchKindUsingForGenericChainCall = "using_for_generic_chain_call"
