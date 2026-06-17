package solidity

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks tree-sitter query matches and emits Pass 1 nodes/edges.
// Mirrors the TypeScript declVisitor structure for consistency.
type declVisitor struct {
	rel     string
	src     []byte
	lang    *sitter.Language
	root    *sitter.Node
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
	abi     map[string][]ABISig
	// W-C W6 V1.28 (2026-05-12): per-file alias → original-name map
	// from `import {Original as Alias} from "..."` directives. Used
	// during runUsingFor to resolve the library identifier through the
	// alias before pushing onto the binding pipeline. Populated by
	// runImportAliases prior to runUsingFor.
	importAliases map[string]string
	// W-C W6 V1.29 (2026-05-12): per-file set of whole-file namespace
	// aliases from `import "./util.sol" as L` directives — `L` is a
	// namespace prefix, not a library name. runUsingFor consults this
	// set when iterating the type_alias identifier sequence of
	// `using L.SafeMath for ...` so the leading namespace identifier
	// is skipped (otherwise Pass 2 byName[NodeContract] would emit a
	// false-positive EdgeUsesFor against any unrelated contract named L).
	namespaceAliases map[string]bool
	// W-C W6 V5 (2026-05-19): namespace alias -> source path text
	// captured from `import "./path.sol" as Alias;`. Used during
	// using-for resolution to disambiguate free-function homonyms:
	// when `using {M.mul as +}` reaches Pass 2 with multiple `mul`
	// candidates across files, the resolver prefers a candidate
	// whose file path matches M's recorded source path.
	namespacePaths map[string]string
	// W-C W6 V6 (2026-05-19): named-import alias -> source path
	// captured from `import {orig as alias} from "./path.sol";`.
	// Same lookup role as namespacePaths but for the named-import
	// form. resolveUsingForRef consults both maps when resolving a
	// using-for binding's leading identifier.
	importPaths map[string]string
	// W-C W9 V14 (2026-05-19): per-file enum name → byte footprint.
	// Sol enums with ≤256 variants compile to uint8 (1 byte), ≤65k
	// to uint16 (2 bytes), and so on. runStateVarDecl consults
	// this map before falling back to the conservative full-slot
	// path so enum-typed state vars pack with adjacent small
	// primitives.
	enumSizes map[string]int
	// W-C W9 V5 (2026-05-19): per-file struct → byte footprint when
	// declared as a storage state variable. Populated by
	// computeStructSizes (called once before runStateVarDecl) by
	// summing each member's solTypeSize / solFixedArrayBytes /
	// recursive struct lookup with the standard slotState packing
	// rules. Used by runStateVarDecl to route NodeField state-vars
	// whose user_defined_type matches a known struct through the
	// array-shaped advanceForStructField path.
	structSizes map[string]int
}

// newDeclVisitor allocates a per-file visitor with a local abi map. The
// caller (ParseFile) merges v.abi into the shared Parser.abi under lock
// after visit() returns — this keeps collectABI race-free under the
// concurrent ParseFile dispatch buildpipe now uses.
func newDeclVisitor(rel string, src []byte, lang *sitter.Language, root *sitter.Node) *declVisitor {
	v := &declVisitor{
		rel:              rel,
		src:              src,
		lang:             lang,
		root:             root,
		abi:              map[string][]ABISig{},
		importAliases:    map[string]string{},
		namespaceAliases: map[string]bool{},
		namespacePaths:   map[string]string{},
		importPaths:      map[string]string{},
		enumSizes:        map[string]int{},
		structSizes:      map[string]int{},
	}
	fileQ := "file:" + rel
	v.fileID = parse.MakeID(fileQ, "sol", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: "sol", Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) visit() {
	// W4: contract & library decls use SubKind-aware emit paths so the
	// graph distinguishes plain / abstract / library variants. See
	// abstract_library.go and docs/design/solidity-inheritance-and-
	// interface-dispatch.md §2.1 / §4.4.
	//
	// W1: interface_declaration gets its own emit path so it lands as
	// NodeInterface (Q1: reuse the existing NodeInterface enum, shared
	// with TS/Go idiom). Inheritance / implementation detection runs
	// after contract / interface decls have been emitted — the W1
	// PendingRefs are name-resolved in Pass 2 (resolveInheritance) so
	// they can see nodes from other files.
	v.runContractDecl()
	v.runLibraryDecl()
	v.runInterfaceDecl()
	// W2: function emit is SubKind-aware (virtual / override / virtual_override
	// / function). The generic runDecl path is bypassed for functions so the
	// modifier scan and EdgeOverrides PendingRef emission share a single AST
	// walk. Node IDs and the `defines` edge stay identical to runDecl, so
	// downstream consumers (ABI, mapping writes, emits, modifier_invocation)
	// continue to resolve against the same Function node.
	v.runFunctionDecl()
	v.runDecl(queryModifier, types.NodeModifier)
	// W-C W6 V1.22 (2026-05-12): emit parameter / local-var meta for
	// every modifier_definition so receivers used inside the modifier
	// body (params, locals) flow through the same lookupReceiverType
	// path as function bodies. runDecl(queryModifier, ...) creates the
	// NodeModifier; this pass adds the side-channel meta PendingRefs.
	v.runModifierMeta()
	// W-C W6 V1.23 (2026-05-12): emit NodeFunction (synthetic name
	// "constructor") + parameter / local-var meta for every
	// constructor_definition. Constructors share function_body shape
	// but have no `name` field — runConstructorDecl uses a synthetic
	// identifier and hashes the id off the declaration's StartByte.
	v.runConstructorDecl()
	// W-C W6 V1.24 (2026-05-12): emit NodeFunction (synthetic name
	// "fallback" / "receive") + parameter / local-var meta for every
	// fallback_receive_definition. Tree-sitter lumps both forms into
	// a single kind; the walker disambiguates by reading the leading
	// source token.
	v.runFallbackReceiveDecl()
	v.runDecl(queryEvent, types.NodeEvent)
	v.runDecl(queryStruct, types.NodeStruct)
	v.runDecl(queryEnum, types.NodeEnum)
	// W-C W9 V14 (2026-05-19): count enum variants per name so
	// runStateVarDecl can size enum-typed state-vars at 1 / 2 / 4
	// bytes (uint8/16/32) instead of the conservative full-slot
	// fallback. Sol enums with ≤256 variants compile to uint8.
	v.computeEnumSizes()
	// W-C W9 V5 (2026-05-19): compute per-struct byte footprint so
	// runStateVarDecl can size NodeField rows whose user_defined_type
	// is a known struct. Must run before runStateVarDecl.
	v.computeStructSizes()
	v.runStateVarDecl()
	v.runEmits()
	v.runHasModifier()
	// W-C W7.3 V0 (2026-05-18): detect `modifier m() override {}` and
	// emit EdgeOverrides PendingRefs. Re-uses W2's resolveOverridesRef
	// in Pass 2 (NodeType-agnostic — modifier qnames live in funcByQName
	// alongside functions).
	v.runModifierOverride()
	v.runInheritance()
	// W6 V1.10 (2026-05-12): struct field type metadata. Walks each
	// struct_declaration's struct_body children and emits a side-channel
	// PendingRef per struct_member carrying (structName, fieldName,
	// fieldType). Resolver builds the structFieldTypes index for
	// struct-field receiver dispatch (`obj.field.method()`).
	v.runStructFieldMeta()
	// W3 (interface dispatch): walks body member_expression nodes that
	// fit the `IFoo(addr).bar()` shape and queues EdgeInvokes PendingRefs
	// resolved in Pass 2 (resolveInterfaceDispatch). Confidence is always
	// ConfAmbiguous per §5.0 Q5 — see dispatch.go preamble.
	v.runDispatch()
	// W8 V0 (2026-05-18): contract-type cast sibling. Walks the same
	// AST shape but resolves the leading type against byName[NodeContract]
	// instead of NodeInterface. DispatchKind="contract_cast".
	v.runContractCastDispatch()
	// W-C W6 V1.28 (2026-05-12): walk import_directive nodes and fill
	// v.importAliases per-file. Must run before runUsingFor so the
	// using-for walker can resolve aliased library names.
	v.runImportAliases()
	// W6 (using For): emits EdgeUsesFor PendingRefs from `using LibName for
	// TypeName;` directives nested inside a contract / library / interface
	// body. Q9-1 (b) decision (2026-05-12). V0 scope: binding declaration.
	// V1.0 (same call) additionally emits typebind PendingRefs (for the
	// per-contract binding map) and method-call PendingRefs (via the
	// separate runUsingForCalls below).
	v.runUsingFor()
	// W6 V2.18 (2026-05-17): ERROR-tolerant recovery for file-level
	// `using LibName for T [global];` directives (Sol 0.8.13+). Vendored
	// grammar v1.2.11 misparses these into ERROR nodes at source_file
	// scope; runFileLevelUsingFor walks the recoverable shape and emits
	// the same PendingRef pair runUsingFor produces, fanned out per
	// container in the file (file-level binding applies to all). Closes
	// V2.16 row 1 grammar-block.
	v.runFileLevelUsingFor()
	// W6 V2.5 (2026-05-19): file-level operator-form using directive
	// ERROR recovery. `using {f as +, g as -} for T [global];` at
	// source_file scope parses to an ERROR child whose braced body
	// isn't surfaced as named children; the walker extracts the bound
	// function names and bound type from the raw ERROR text and emits
	// one binding pair per (container, function).
	v.runFileLevelOperatorForm()
	// W6 V2.20 (2026-05-18): operator-form using directive ERROR
	// recovery. `using {f as +} for T;` parses to a misclassified
	// state_variable_declaration; pattern-match the shape and emit
	// the same binding pair runUsingFor produces. Flips V2.7 /
	// V2.14 IOp / V2.17 locks from 0 → 1.
	v.runOperatorFormRecovery()
	// W6 V1.0 (2026-05-12): method-call dispatch detector. Walks every
	// member_expression that fits `<identifier>.<identifier>(...)` and
	// queues a PendingRef that Pass 2 resolves through the binding map.
	// State-variable receivers only (Q9-2 (a) V0 limit); parameter
	// receivers added in V1.1 via emitParameterMetaPending in overrides.go.
	v.runUsingForCalls()
	// W7.1 V0 (2026-05-17): low-level call dispatch detector. Walks
	// member_expression nodes matching `target.call/delegatecall/
	// staticcall(...)` and emits EdgeInvokes with DispatchKind=
	// "low_level_call" + ConfAmbiguous. Receiver resolution chain
	// re-uses W6 V1.0 lookupReceiverType (state-var / param / local-var).
	v.runLowLevelCalls()
	// W8 V1 (2026-05-18): mark HasLowLevelCall / HasValueTransfer on
	// callables containing any .call / .delegatecall / .staticcall or
	// .send / .transfer invocation, regardless of receiver resolvability.
	// Superset signal complementing runLowLevelCalls's edge emission.
	v.runLowLevelCallMarker()
	// W10 V0 (2026-05-18): mark NodeFunction / NodeModifier with
	// HasAssembly=true when the body contains `assembly { ... }`.
	// Post-Pass-1 sweep — mutates v.nodes in place.
	v.runAssemblyMarker()
	// W10 V1.1 (2026-05-18): enumerate security-relevant Yul EVM
	// builtins (delegatecall, sstore, sload, selfdestruct, call,
	// staticcall) per callable. Sorted, deduped slice on
	// Node.YulBuiltins.
	v.runYulBuiltins()
	// W10 V2 (2026-05-18): resolve the target address argument of
	// Yul delegatecall / call / staticcall to a Sol scope receiver.
	// Re-uses W7.1's resolveLowLevelCallRef so Yul and Sol low-level
	// calls produce identical edge shapes.
	v.runYulLowLevelCalls()
	// W8 V3 (2026-05-19): mark callables that own a function-typed
	// parameter or local variable with HasFunctionTypedVar=true.
	// Complements W8 V2's NodeField marker and surfaces indirect
	// dispatch surfaces for security tooling.
	v.runFunctionTypedVarMarker()
	// W8 V6 (2026-05-19): queue `<receiver>.<method>(...)` PendingRefs
	// so Pass 2 can resolve cross-contract function-pointer calls
	// (receiver typed as another contract whose method is a function-
	// typed state-var). Pass 2 marks HasFunctionPointerCall when the
	// chain resolves; the walker itself does not emit any edge.
	v.runCrossContractFnPointerCall()
	// W10 V5 (2026-05-19): mark HasExternalCall=true on callables
	// that perform `address(x).call(...)` / `payable(x).call(...)`
	// cast-shape low-level calls. Complements the V4 Pass-2 mark
	// for bare-identifier address-typed receivers.
	v.runExternalCallCastMarker()
	// W10 V19 (2026-05-21): mark HasHighLevelSelfCall=true on callables
	// that perform a typed self-call — `this.foo()`,
	// `MyContract(address(this)).foo()`, `IFoo(address(this)).bar()`,
	// or nested cast chains that bottom out at `this`. Parallel to
	// the low-level V8/V18 surface but independent: the EVM message-
	// call boundary still applies, so the callee can re-enter through
	// typed dispatch just as effectively as through `.call(...)`.
	v.runHighLevelSelfCallMarker()
	// W10 V6 (2026-05-19): queue PendingRefs for chained-call
	// shape (`getTarget().call(...)`). Pass 2 looks up the inner
	// function's first return type via funcReturnTypes and marks
	// HasExternalCall on the source callable when the type is
	// address / address payable.
	v.runChainedExternalCall()
	v.collectABI()

	// canonical id (ADR-0001): no import path in Solidity, so the relative file
	// path is the qualifier — <relpath>:<qualified_name>. Functions already set
	// their canonical id inline in runFunctionDecl (with the overload
	// parameter-type signature), so this fills the remaining symbol nodes
	// (contracts/interfaces/libraries/modifiers/events/structs/enums/fields/
	// mappings) uniformly. File and import nodes are not symbols and are skipped.
	for i := range v.nodes {
		n := &v.nodes[i]
		if n.CanonicalID != "" || n.Type == types.NodeFile || n.Type == types.NodeImport {
			continue
		}
		n.CanonicalID = v.rel + ":" + n.QualifiedName
	}
}

func (v *declVisitor) runDecl(q string, nt types.NodeType) {
	query, qErr := sitter.NewQuery(v.lang, q)
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
		for _, c := range m.Captures {
			if names[c.Index] != "name" {
				continue
			}
			node := c.Node
			ident := node.Utf8Text(v.src)
			startByte := int(node.StartByte())
			endByte := int(node.EndByte())
			qname := ident
			// W-C W6 V1.22 (2026-05-12): modifiers also get a
			// Container.<name> qname so containerIDByFuncID (Pass 1.5)
			// can resolve their enclosing contract from the qname
			// prefix — same idiom as NodeFunction. byName indexing
			// (resolve.go) keys NodeModifier on bare Name field, so
			// the qname change doesn't break EdgeHasModifier resolution.
			if nt == types.NodeFunction || nt == types.NodeModifier {
				if cn := nearestContractName(&node, v.src); cn != "" {
					qname = cn + "." + ident
				}
			}
			id := parse.MakeID(qname, "sol", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(node.StartPosition().Row) + 1,
				EndLine:   int(node.EndPosition().Row) + 1,
				StartByte: startByte, EndByte: endByte,
				Language: "sol", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

// runStateVarDecl walks all state_variable_declaration nodes once. Non-mapping
// state vars become Field nodes; declarations whose type_name has key_type +
// value_type fields are emitted as Mapping nodes. Unifying both kinds in one
// pass lets us avoid a separate queryMappingDecl (which the grammar doesn't
// expose as a distinct node type) and keeps mapping detection adjacent to its
// type-introspection logic.
func (v *declVisitor) runStateVarDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryStateVarAll)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	// W-C W9 V0/V2 (2026-05-18): per-contract packing slot state.
	// Each entry tracks the current slot index and bytes already used
	// in it. V0 emitted one slot per field; V2 uses solTypeSize plus
	// advanceForField to pack consecutive sub-32-byte primitives into
	// shared slots (Sol §11.1 layout rules). Mapping state-vars run
	// through advanceForMapping, which reserves a full slot without
	// producing a SlotIndex (NodeMapping path stays at zero default
	// in V2; W9 V3 will index mappings separately).
	slotPerContract := map[string]slotState{}
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "decl" {
				continue
			}
			declNode := c.Node
			// W-C W9 V9 (2026-05-19): operator-form using directives
			// (`using {f as +} for T;`) misparse as ERROR-wrapped
			// state_variable_declaration nodes in vendored tree-
			// sitter-solidity v1.2.11. V2.20's recovery walker emits
			// the binding edge for those — runStateVarDecl must NOT
			// also emit a NodeField for them, because doing so
			// inflates the per-contract slot count by one for every
			// operator-form directive (Sol §11.1 says using
			// directives don't consume storage). Skip when the
			// misparse signature matches.
			if _, _, ok := matchOperatorFormStateVar(&declNode, v.src); ok {
				continue
			}
			nameNode := declNode.ChildByFieldName("name")
			typeNode := declNode.ChildByFieldName("type")
			if nameNode == nil {
				continue
			}
			name := nameNode.Utf8Text(v.src)
			startByte := int(nameNode.StartByte())
			endByte := int(nameNode.EndByte())
			line := int(nameNode.StartPosition().Row) + 1
			isMapping := typeNode != nil && typeNameIsMapping(typeNode, v.src)
			var nt types.NodeType
			var qname string
			if isMapping {
				nt = types.NodeMapping
				qname = name + ":mapping"
			} else {
				nt = types.NodeField
				// W-C W6 V1.0 (2026-05-12): qualify NodeField qnames with
				// the enclosing container's name so Pass 2 can recover
				// the state-var → container relationship cheaply (same
				// idiom as runFunctionDecl's `Container.func` qname).
				// File-level state-var declarations are out of scope in
				// Solidity; nearestContractName returns "" for them and
				// the qname falls back to the bare name (extant V0
				// shape preserved for that edge case).
				qname = name
				if cn := nearestContractName(nameNode, v.src); cn != "" {
					qname = cn + "." + name
				}
			}
			id := parse.MakeID(qname, "sol", startByte)
			// W-C W6 V1.0 (2026-05-12): stash the declared type name on the
			// Field/Mapping node's Signature so Pass 2 can resolve
			// `<stateVar>.<method>(...)` callsites by looking up the
			// state-variable's type and matching it against the using-for
			// binding map. We use Signature (not SubKind) because the value
			// here is the raw user-written type expression — V0 graph
			// consumers already treat Signature as opaque metadata. Empty
			// typeNode (rare — degenerate parses) → Signature stays empty.
			signature := ""
			if typeNode != nil {
				signature = extractTypeNameText(typeNode, v.src)
			}
			// W-C W7.2 V0 (2026-05-17): state-var visibility / immutable
			// encoded into SubKind. Grammar v1.2.11 only exposes these
			// two keyword categories as named children; `constant` and
			// parameter locations are AST-invisible (deferred to V1+).
			// SubKind precedence: immutable > visibility > "" (default
			// stays empty rather than synthesising "internal" so consumers
			// can distinguish "explicitly internal" from "default/unknown").
			subKind := ""
			if !isMapping {
				for i := uint(0); i < declNode.NamedChildCount(); i++ {
					child := declNode.NamedChild(i)
					if child == nil {
						continue
					}
					switch child.Kind() {
					case "immutable":
						subKind = "immutable"
					case "visibility":
						if subKind == "" {
							subKind = "storage_" + child.Utf8Text(v.src)
						}
					}
				}
			}
			// W-C W9 V2 / V3 / V4 (2026-05-18..19): assign packed slot
			// index for every state-var. V2 introduced type-size aware
			// packing; V3 made mappings addressable by declaration slot;
			// V4 handles fixed-size value-type arrays (uint8[4],
			// uint256[2], uint8[4][2]) which Sol §11.1 lays out as
			// new-slot-aligned blocks of ceil(elementBytes * count / 32)
			// consecutive slots with post-slot alignment.
			slotIndex := 0
			containerKey := nearestContractName(nameNode, v.src)
			state := slotPerContract[containerKey]
			switch {
			case isMapping:
				slot, newState := advanceForMapping(state)
				slotIndex = slot
				slotPerContract[containerKey] = newState
			default:
				if arrayBytes, ok := solFixedArrayBytes(signature); ok {
					slot, newState := advanceForArrayField(state, arrayBytes)
					slotIndex = slot
					slotPerContract[containerKey] = newState
				} else if structBytes, ok := v.structSizes[signature]; ok {
					// W-C W9 V5: struct state-vars consume
					// ceil(sum_of_field_bytes / 32) slots, with
					// pre/post slot alignment same as arrays.
					slot, newState := advanceForArrayField(state, structBytes)
					slotIndex = slot
					slotPerContract[containerKey] = newState
				} else if enumBytes, ok := v.enumSizes[signature]; ok {
					// W-C W9 V14: enum-typed state-var packs
					// like the underlying uintN (1 byte for the
					// typical ≤256-variant case). Goes through
					// advanceForField so adjacent small
					// primitives share the slot.
					slot, newState := advanceForField(state, enumBytes)
					slotIndex = slot
					slotPerContract[containerKey] = newState
				} else {
					size := solTypeSize(signature)
					slot, newState := advanceForField(state, size)
					slotIndex = slot
					slotPerContract[containerKey] = newState
				}
			}
			// W-C W8 V2 (2026-05-18): function-typed state-var marker.
			// Detection: type_name contains `parameter` or `return_parameter`
			// as a named child (Sol grammar emits these for function-type
			// signatures instead of the usual primitive_type / user_defined_type
			// / mapping shape). NodeMapping path keeps IsFunctionTyped false
			// since mappings are a distinct grammar shape.
			// W-C W8 V12 (2026-05-19) / V13 (2026-05-19): delegate
			// to the shared typeNameIsFunctionTyped helper. V12
			// added array_type recursion; V13 drops the
			// !isMapping guard so `mapping(K => function(...))`
			// also lights up the marker. typeNameIsFunctionTyped
			// recurses into nested type_name children and
			// returns false for ordinary mappings whose value
			// is a primitive — no false positives from removing
			// the guard.
			isFunctionTyped := typeNameIsFunctionTyped(typeNode)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: name, QualifiedName: qname,
				FilePath: v.rel, StartLine: line, EndLine: line,
				StartByte: startByte, EndByte: endByte,
				Language: "sol", Confidence: types.ConfExtracted,
				Signature:       signature,
				SubKind:         subKind,
				SlotIndex:       slotIndex,
				IsFunctionTyped: isFunctionTyped,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines,
				Count: 1, Confidence: types.ConfExtracted,
			})
			if isMapping {
				// TODO(T19+): pass `id` here once writes_mapping can be emitted as
				// a same-file resolved edge directly (skip pending pipeline).
				v.queueMappingWrites(name)
			}
		}
	}
}

// queueMappingWrites scans every function in the current root for an
// augmented_assignment_expression whose LHS array_access targets the given
// mapping name, and queues a pending writes_mapping edge. V0 simplification:
// we treat any `name[...] = ...` or `name[...] += ...` as a write.
func (v *declVisitor) queueMappingWrites(mappingName string) {
	q := `(augmented_assignment_expression
	         (expression (array_access (expression (identifier) @arr))))
	      @stmt`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		// Fallback: try plain assignment_expression too.
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
		var arrName string
		var stmtNode *sitter.Node
		for _, c := range m.Captures {
			capName := names[c.Index]
			node := c.Node
			switch capName {
			case "arr":
				arrName = node.Utf8Text(v.src)
			case "stmt":
				// The capture's Node is a value type; we need a stable pointer
				// for the parent walk below. Take address of the local copy.
				stmtCopy := node
				stmtNode = &stmtCopy
			}
		}
		if arrName != mappingName || stmtNode == nil {
			continue
		}
		fnQ, fnStart, ok := nearestFunctionQnameAndStart(stmtNode, v.src)
		if !ok {
			continue
		}
		// SrcID must match the function node ID emitted in runDecl, which
		// hashes (qname, "sol", name-node startByte). Using offset 0 here would
		// produce an ID that never resolves to a real node and graph.Validate
		// would reject the resulting edge as dangling.
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", fnStart),
			EdgeType:    types.EdgeWritesMapping,
			TargetQName: mappingName + ":mapping",
			Line:        int(stmtNode.StartPosition().Row) + 1,
		})
	}
}

func (v *declVisitor) runEmits() {
	query, qErr := sitter.NewQuery(v.lang, queryEmit)
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
		var event string
		var fnQ string
		var fnStart int
		var fnOK bool
		var line int
		for _, c := range m.Captures {
			if names[c.Index] == "event" {
				node := c.Node
				event = node.Utf8Text(v.src)
				fnQ, fnStart, fnOK = nearestFunctionQnameAndStart(&node, v.src)
				line = int(node.StartPosition().Row) + 1
			}
		}
		if event == "" || !fnOK {
			continue
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", fnStart),
			EdgeType:    types.EdgeEmitsEvent,
			TargetQName: event,
			Line:        line,
		})
	}
}

func (v *declVisitor) runHasModifier() {
	query, qErr := sitter.NewQuery(v.lang, queryHasModifier)
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
		var mod string
		var fnQ string
		var fnStart int
		var fnOK bool
		var line int
		var stmtStart uint
		var stmtParent *sitter.Node
		for _, c := range m.Captures {
			if names[c.Index] == "mod" {
				node := c.Node
				mod = node.Utf8Text(v.src)
				fnQ, fnStart, fnOK = nearestFunctionQnameAndStart(&node, v.src)
				line = int(node.StartPosition().Row) + 1
			}
			if names[c.Index] == "stmt" {
				node := c.Node
				stmtStart = node.StartByte()
				stmtParent = node.Parent()
			}
		}
		if mod == "" || !fnOK {
			continue
		}
		// W-C W7.3 (2026-05-18): compute source-order index. Count
		// modifier_invocation siblings whose StartByte precedes the
		// current statement node. The grammar lists modifier_invocation
		// children of function_definition / constructor_definition in
		// source order, so the count equals the 0-indexed position.
		order := 0
		if stmtParent != nil {
			for i := uint(0); i < stmtParent.NamedChildCount(); i++ {
				sibling := stmtParent.NamedChild(i)
				if sibling == nil || sibling.Kind() != "modifier_invocation" {
					continue
				}
				if sibling.StartByte() >= stmtStart {
					break
				}
				order++
			}
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", fnStart),
			EdgeType:    types.EdgeHasModifier,
			TargetQName: mod,
			Line:        line,
			Order:       order,
		})
	}
}

// collectABI populates p.abi from the discovered Contract / Function nodes.
// Iteration order matches v.nodes (which is append order from visit()), so
// Contract nodes are seen before their methods because runDecl(Contract)
// runs before runDecl(Function). For nested contracts we'd need a smarter
// scope-tracking pass; V0 is single-level.
func (v *declVisitor) collectABI() {
	currentContract := ""
	for _, n := range v.nodes {
		switch n.Type {
		case types.NodeContract:
			currentContract = n.Name
		case types.NodeFunction:
			if currentContract == "" {
				continue
			}
			v.abi[currentContract] = append(v.abi[currentContract], ABISig{
				ContractName: currentContract,
				FunctionName: n.Name,
				ParamTypes:   nil, // V0 placeholder — name-match is sufficient.
			})
		}
	}
}

// helpers

// runModifierMeta — W-C W6 V1.22 (2026-05-12). Walks every
// modifier_definition node and emits the V1.1 / V1.15 meta PendingRefs
// against its modifier ID. modifier_definition has the same shape as
// function_definition for parameter children and body field, so the
// existing emit helpers work without modification. (Modifiers have no
// return_type, so emitFunctionReturnMetaPending / emit-named-return are
// not applicable here.)
//
// The modifier ID is recomputed from the same (qname, startByte) pair
// that runDecl(NodeModifier) used (qname is now `Container.modifier`
// per the V1.22 runDecl change). Two-pass split keeps runDecl generic
// and isolates the meta walk to its own function.
func (v *declVisitor) runModifierMeta() {
	query, qErr := sitter.NewQuery(v.lang, queryModifier)
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
		var nameNode, declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		ident := nameNode.Utf8Text(v.src)
		qname := ident
		if cn := nearestContractName(nameNode, v.src); cn != "" {
			qname = cn + "." + ident
		}
		id := parse.MakeID(qname, "sol", int(nameNode.StartByte()))
		emitParameterMetaPending(v, id, declNode)
		emitLocalVarMetaPending(v, id, declNode)
	}
}

// runImportAliases — W-C W6 V1.28 (2026-05-12) + V2.10 (2026-05-13).
// Walks every import_directive in the file and records
// (alias → originalName) pairs into v.importAliases. Tree-sitter-
// solidity v1.2.13 exposes the directive's `import_name` and `alias`
// as multiple-cardinality identifier fields.
//
// V2.10 fix: pairing is by source order, not by bucket index. The
// V1.28 V0 approach bucketed `import_name` and `alias` separately
// and zipped them at the end, which mis-paired heterogeneous
// statements like `import {SafeMath, Address as A}`:
//
//	buckets: importNames=[SafeMath, Address], aliases=[A]
//	zip:     A ↔ SafeMath  (WRONG — A actually aliases Address)
//
// The fix: walk identifier children in source order, keep the most
// recent `import_name` in a one-slot buffer, and pair it with the
// next `alias` when one appears. An `import_name` with no following
// `alias` is bare and needs no alias mapping (the bare name already
// resolves via the global byName index).
//
// Shapes covered:
//   - `import {SafeMath as SM} from "./util.sol"` — single aliased.
//     alias map: SM → SafeMath.
//   - `import {SafeMath as SM, Address as A} from "./util.sol"` —
//     all-aliased multi-entry.
//   - `import {SafeMath, Address as A} from "./util.sol"` — mixed
//     bare + aliased (V2.10 fix).
//   - `import {SafeMath} from "./util.sol"` — bare-only; no mapping
//     recorded, bare name resolves directly.
//   - `import "./util.sol" as L` — whole-file alias (V1.29).
//     Detected as alias-without-preceding-import_name, recorded in
//     namespaceAliases.
func (v *declVisitor) runImportAliases() {
	if v.root == nil {
		return
	}
	for i := uint(0); i < uint(v.root.NamedChildCount()); i++ {
		child := v.root.NamedChild(i)
		if child == nil || child.Kind() != "import_directive" {
			continue
		}
		// W-C W6 V5 (2026-05-19): capture the directive's source path
		// (string node) for namespace-alias correlation. The walk
		// below records the leading `string` child once and pairs it
		// with any `alias` that turns out to be a namespace alias
		// (whole-file form, no preceding `import_name`).
		sourcePath := ""
		for j := uint(0); j < uint(child.ChildCount()); j++ {
			c := child.Child(j)
			if c != nil && c.Kind() == "string" {
				sourcePath = trimStringQuotes(c.Utf8Text(v.src))
				break
			}
		}
		// Single-pass walk in source order. `lastImportName` holds
		// the most recently seen `import_name` (empty if none, or
		// if already consumed by a paired `alias`). An `alias`
		// encountered with `lastImportName == ""` is a whole-file
		// namespace alias (V1.29), recorded separately.
		var lastImportName string
		for j := uint(0); j < uint(child.ChildCount()); j++ {
			c := child.Child(j)
			if c == nil || c.Kind() != "identifier" {
				continue
			}
			switch child.FieldNameForChild(uint32(j)) {
			case "import_name":
				lastImportName = c.Utf8Text(v.src)
			case "alias":
				aliasName := c.Utf8Text(v.src)
				if aliasName == "" {
					continue
				}
				if lastImportName != "" {
					v.importAliases[aliasName] = lastImportName
					lastImportName = "" // consumed
					// W-C W6 V6 (2026-05-19): record the named-
					// import alias source path so resolveUsingFor
					// Ref can disambiguate cross-file free-
					// function homonyms the same way the V5
					// namespace-alias path hint does.
					if sourcePath != "" {
						v.importPaths[aliasName] = sourcePath
					}
				} else {
					// Whole-file namespace alias (V1.29).
					v.namespaceAliases[aliasName] = true
					if sourcePath != "" {
						v.namespacePaths[aliasName] = sourcePath
					}
				}
			}
		}
	}
}

// trimStringQuotes strips a single layer of surrounding single or
// double quotes from a Sol string literal text. Defensive against
// the rare case where tree-sitter exposes the literal with or
// without quote characters depending on grammar revision.
func trimStringQuotes(s string) string {
	if len(s) >= 2 {
		first := s[0]
		last := s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// runConstructorDecl — W-C W6 V1.23 (2026-05-12). Emits one
// NodeFunction per constructor_definition with synthetic name
// "constructor" and qname "<Container>.constructor", then drives the
// V1.22 meta-emission pipeline against the constructor body.
//
// Why a synthetic name? Solidity's grammar (verified via
// node-types.json v1.2.13) defines constructor_definition without a
// `name` field — only `body` (function_body) + direct `parameter`
// children + optional modifier_invocation. The Sol language reserves
// the keyword `constructor` for this role; we use that literal as the
// canonical identifier. ID hashing follows the existing
// MakeID(qname, "sol", startByte) idiom; startByte is the declaration
// node's StartByte (start of the `constructor` keyword), which is
// stable across builds.
//
// Why NodeFunction (not a new NodeConstructor)? Constructors share
// runtime semantics with regular functions (callable, parameters,
// body locals, modifier invocations) and existing consumers
// (containerIDByFuncID, lookupReceiverType, EdgeHasModifier
// resolution) already key off NodeFunction. Adding a sibling type
// would force every downstream consumer to special-case it. The
// SubKind field (W2) gets "constructor" to keep the role visible in
// the graph without breaking the type-level contract.
func (v *declVisitor) runConstructorDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryConstructor)
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
		var declNode *sitter.Node
		for _, c := range m.Captures {
			if names[c.Index] == "decl" {
				n := c.Node
				declNode = &n
			}
		}
		if declNode == nil {
			continue
		}
		const ident = "constructor"
		qname := ident
		if cn := nearestContractName(declNode, v.src); cn != "" {
			qname = cn + "." + ident
		}
		startByte := int(declNode.StartByte())
		endByte := int(declNode.EndByte())
		id := parse.MakeID(qname, "sol", startByte)
		v.nodes = append(v.nodes, types.Node{
			ID: id, Type: types.NodeFunction, Name: ident, QualifiedName: qname,
			FilePath: v.rel, StartLine: int(declNode.StartPosition().Row) + 1,
			EndLine:   int(declNode.EndPosition().Row) + 1,
			StartByte: startByte, EndByte: endByte,
			Language: "sol", Confidence: types.ConfExtracted,
			SubKind: "constructor",
		})
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines,
			Count: 1, Confidence: types.ConfExtracted,
		})
		// V1.22 meta pipeline — constructor has no return_type, so
		// V1.3 / V1.19 emits don't apply.
		emitParameterMetaPending(v, id, declNode)
		emitLocalVarMetaPending(v, id, declNode)
	}
}

// runFallbackReceiveDecl — W-C W6 V1.24 (2026-05-12). Emits one
// NodeFunction per fallback_receive_definition with synthetic name
// "fallback" or "receive", qname "<Container>.<name>", SubKind matches
// the name. Then runs the V1.22 meta-emission pipeline against the
// body.
//
// Tree-sitter quirk: the v1.2.13 grammar uses a single node kind for
// both `fallback() { ... }` and `receive() external payable { ... }`,
// with no field that disambiguates them. The leading keyword in the
// source text is the only reliable signal — we read up to the first
// whitespace / `(` after the node's StartByte to recover the name.
//
// Why NodeFunction (not new types)? Same rationale as V1.23: keep the
// type-level contract intact for containerIDByFuncID, lookupReceiverType,
// EdgeHasModifier consumers. SubKind carries the role distinction.
func (v *declVisitor) runFallbackReceiveDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryFallbackReceive)
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
		var declNode *sitter.Node
		for _, c := range m.Captures {
			if names[c.Index] == "decl" {
				n := c.Node
				declNode = &n
			}
		}
		if declNode == nil {
			continue
		}
		ident := fallbackOrReceiveKeyword(declNode, v.src)
		if ident == "" {
			continue // defensive — neither keyword found at node start
		}
		qname := ident
		if cn := nearestContractName(declNode, v.src); cn != "" {
			qname = cn + "." + ident
		}
		startByte := int(declNode.StartByte())
		endByte := int(declNode.EndByte())
		id := parse.MakeID(qname, "sol", startByte)
		v.nodes = append(v.nodes, types.Node{
			ID: id, Type: types.NodeFunction, Name: ident, QualifiedName: qname,
			FilePath: v.rel, StartLine: int(declNode.StartPosition().Row) + 1,
			EndLine:   int(declNode.EndPosition().Row) + 1,
			StartByte: startByte, EndByte: endByte,
			Language: "sol", Confidence: types.ConfExtracted,
			SubKind: ident,
		})
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines,
			Count: 1, Confidence: types.ConfExtracted,
		})
		// V1.22 meta pipeline — fallback can carry parameters (Sol
		// 0.6+, e.g. `fallback(bytes calldata input) external returns
		// (bytes memory)`); receive cannot by language rule, but the
		// walker is uniform — parameter children will be empty for
		// receive and the helper is a no-op.
		emitParameterMetaPending(v, id, declNode)
		emitLocalVarMetaPending(v, id, declNode)
	}
}

// fallbackOrReceiveKeyword reads the leading source keyword at the node
// (either "fallback" or "receive") and returns it, or "" when neither
// keyword matches (defensive — shouldn't happen for valid Sol code).
func fallbackOrReceiveKeyword(n *sitter.Node, src []byte) string {
	start := int(n.StartByte())
	if start >= len(src) {
		return ""
	}
	// Length 8 covers "fallback"; length 7 covers "receive".
	if start+len("fallback") <= len(src) && string(src[start:start+len("fallback")]) == "fallback" {
		return "fallback"
	}
	if start+len("receive") <= len(src) && string(src[start:start+len("receive")]) == "receive" {
		return "receive"
	}
	return ""
}

// nearestContractName walks the parent chain looking for an enclosing
// contract-like declaration and returns its name (empty if none).
//
// W4: also recognises `library_declaration` (Sol libraries hold function
// definitions just like contracts do; their methods should be qualified
// as "Library.func" the same way contract methods are). Reserved for
// future extension: `interface_declaration` (W1 — interface methods).
func nearestContractName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		switch cur.Kind() {
		case "contract_declaration", "library_declaration", "interface_declaration":
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Utf8Text(src)
			}
		}
	}
	return ""
}

// nearestFunctionQnameAndStart walks the parent chain to the enclosing
// function_definition and returns its qualified name (Contract.Func or just
// Func) plus the StartByte of the function's name identifier — the same
// (qname, startByte) pair that runDecl(NodeFunction) uses to mint the
// function node ID. Pending refs that build SrcID via parse.MakeID(fnQ,
// "sol", fnStart) will therefore resolve to a real node, avoiding dangling
// edges in graph.Validate.
//
// Returns ok=false when no enclosing function_definition exists or its
// name field is missing (defensive — every emit / modifier_invocation /
// mapping write in valid Solidity sits inside a function with a name).
func nearestFunctionQnameAndStart(n *sitter.Node, src []byte) (string, int, bool) {
	cn := nearestContractName(n, src)
	for cur := n; cur != nil; cur = cur.Parent() {
		// W-C W6 V1.22 / V1.23 (2026-05-12): function_definition,
		// modifier_definition, and constructor_definition all qualify
		// as enclosing-callable scopes. All three share Contract.<name>
		// qname and live in containerIDByFuncID (Pass 1.5), so using-
		// for PendingRefs emitted from inside any of these bodies
		// resolve through the same path as function-body emits.
		if cur.Kind() == "function_definition" || cur.Kind() == "modifier_definition" {
			id := cur.ChildByFieldName("name")
			if id == nil {
				return "", 0, false
			}
			ident := id.Utf8Text(src)
			qname := ident
			if cn != "" {
				qname = cn + "." + ident
			}
			return qname, int(id.StartByte()), true
		}
		if cur.Kind() == "constructor_definition" {
			// Synthetic identifier — mirrors runConstructorDecl's
			// canonical (qname, startByte) pair so SrcID hashing
			// aligns with the emitted NodeFunction.
			const ident = "constructor"
			qname := ident
			if cn != "" {
				qname = cn + "." + ident
			}
			return qname, int(cur.StartByte()), true
		}
		if cur.Kind() == "fallback_receive_definition" {
			// W-C W6 V1.24: synthetic identifier is read from the
			// source token at the node's start ("fallback" or
			// "receive"). Mirrors runFallbackReceiveDecl's
			// (qname, startByte) pair.
			ident := fallbackOrReceiveKeyword(cur, src)
			if ident == "" {
				return "", 0, false
			}
			qname := ident
			if cn != "" {
				qname = cn + "." + ident
			}
			return qname, int(cur.StartByte()), true
		}
	}
	return "", 0, false
}

// runStructFieldMeta — W-C W6 V1.10 (2026-05-12). Walks every
// struct_declaration in the file and emits a PendingRef per struct_member
// carrying (structName, fieldName, fieldType) so Pass 2 can build a
// structFieldTypes index for struct-field receiver dispatch.
//
// tree-sitter-solidity v1.2.13 shape:
//
//	struct_declaration
//	  name: identifier (structName)
//	  body: struct_body
//	    struct_member (multiple)
//	      name: identifier (fieldName)
//	      type: type_name (fieldType)
//
// Emit decisions:
//   - mapping fields drop (extractTypeNameText returns "" for them) —
//     they have no method-dispatch semantics.
//   - struct definitions outside any contract / library are still
//     captured (file-level struct declarations are common in Sol).
//   - SrcID = v.fileID; the index keys on the encoded TargetQName
//     `<structName>|<fieldName>|<fieldType>`, so multiple PendingRefs
//     per file/struct fan out naturally.
//
// Tree-sitter query reuses queryStruct (the existing NodeStruct emitter)
// to find struct_declaration nodes — saves a per-file query compilation
// cost. struct_body / struct_member walking is hand-rolled because the
// nested shape is awkward to express in a single query alternation.
func (v *declVisitor) runStructFieldMeta() {
	query, qErr := sitter.NewQuery(v.lang, queryStruct)
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
		var nameNode *sitter.Node
		var declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		structName := nameNode.Utf8Text(v.src)
		body := declNode.ChildByFieldName("body")
		if body == nil {
			continue
		}
		line := int(nameNode.StartPosition().Row) + 1
		for i := uint(0); i < uint(body.NamedChildCount()); i++ {
			memberNode := body.NamedChild(i)
			if memberNode == nil || memberNode.Kind() != "struct_member" {
				continue
			}
			fieldNameNode := memberNode.ChildByFieldName("name")
			fieldTypeNode := memberNode.ChildByFieldName("type")
			if fieldNameNode == nil || fieldTypeNode == nil {
				continue
			}
			fieldName := fieldNameNode.Utf8Text(v.src)
			fieldType := extractTypeNameText(fieldTypeNode, v.src)
			if fieldName == "" || fieldType == "" {
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        v.fileID,
				EdgeType:     types.EdgeUsesFor, // unused — routed by DispatchKind
				TargetQName:  structName + "|" + fieldName + "|" + fieldType,
				Line:         line,
				DispatchKind: dispatchKindUsingForStructField,
			})
		}
	}
}

// dispatchKindUsingForStructField (W-C W6 V1.10) carries struct field
// (structName, fieldName, fieldType) bindings as side-channel data.
// Resolver sweeps these into structFieldTypes index — no graph edge.
const dispatchKindUsingForStructField = "using_for_struct_field"

// extractTypeNameText returns the user-written type expression for a
// state_variable_declaration's type_name child. Used by W-C W6 V1.0 to
// stamp NodeField.Signature with the declared type so Pass 2 can resolve
// `<stateVar>.<method>(...)` against the using-for binding map.
//
// Three shapes covered:
//   - primitive_type / user_defined_type → identifier text (`uint256`,
//     `MyContract`).
//   - mapping → returns "" (mapping receivers don't participate in
//     using-for dispatch; the resolver naturally drops them).
//   - other compound types (array_type, function_type) → raw subtree text
//     normalised by stripping outer whitespace. This is a permissive
//     fallback so future receiver shapes don't crash the parse; the V0
//     binding map lookup will simply miss when the typeName doesn't match
//     any directive's bound type.
func extractTypeNameText(typeNode *sitter.Node, src []byte) string {
	if typeNode == nil {
		return ""
	}
	if typeNameIsMapping(typeNode, src) {
		return ""
	}
	// First, look for a direct named child (primitive_type /
	// user_defined_type). The grammar wraps these in type_name; the inner
	// named child is the one carrying the identifier we care about.
	for i := uint(0); i < uint(typeNode.NamedChildCount()); i++ {
		c := typeNode.NamedChild(i)
		switch c.Kind() {
		case "primitive_type":
			return strings.TrimSpace(string(src[c.StartByte():c.EndByte()]))
		case "user_defined_type":
			// user_defined_type may be `Foo` or `Ns.Foo` — V0 takes the
			// trailing identifier (same idiom as TS heritage resolution).
			if id := lastIdentifier(c); id != nil {
				return id.Utf8Text(src)
			}
			return strings.TrimSpace(string(src[c.StartByte():c.EndByte()]))
		}
	}
	// Permissive fallback — raw subtree text. Whitespace-trimmed so
	// downstream string compares stay clean.
	return strings.TrimSpace(string(src[typeNode.StartByte():typeNode.EndByte()]))
}

// lastIdentifier returns the rightmost identifier-like named child under n,
// flattening `Ns.Foo`-style user_defined_type references to their final
// segment.
func lastIdentifier(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := int(n.NamedChildCount()) - 1; i >= 0; i-- {
		c := n.NamedChild(uint(i))
		if c.Kind() == "identifier" {
			return c
		}
	}
	return nil
}

// typeNameIsMapping reports whether a type_name node represents a mapping
// declaration. The grammar models mappings as a hidden _mapping rule inlined
// into type_name, so we detect them by the presence of `key_type` /
// `value_type` fields, falling back to a textual `mapping(` prefix check.
func typeNameIsMapping(n *sitter.Node, src []byte) bool {
	if n == nil {
		return false
	}
	if n.ChildByFieldName("key_type") != nil || n.ChildByFieldName("value_type") != nil {
		return true
	}
	txt := strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
	return strings.HasPrefix(txt, "mapping")
}
