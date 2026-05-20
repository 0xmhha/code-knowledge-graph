package solidity

// Tree-sitter queries for the JoranHonig/tree-sitter-solidity grammar
// (vendored under ./binding, v1.2.11). Captures use `@name` markers that the
// declVisitor reads positionally.
//
// Notes on this grammar's quirks (verified against node-types.json):
//   - `mapping` is an anonymous keyword, not a top-level node — mapping
//     state-vars are detected separately in runMappingDecl by walking the
//     state_variable_declaration's `type_name` field for key_type/value_type.
//   - `emit_statement` exposes its event reference under field `name` whose
//     type is `expression`, so we descend through the expression to grab the
//     leading identifier.
//   - `modifier_invocation` has no fields; the modifier name is the first
//     `identifier` named child.
const (
	queryContract  = `(contract_declaration name: (identifier) @name) @decl`
	queryLibrary   = `(library_declaration name: (identifier) @name) @decl`
	queryInterface = `(interface_declaration name: (identifier) @name) @decl`
	queryFunction  = `(function_definition name: (identifier) @name) @decl`
	queryModifier  = `(modifier_definition name: (identifier) @name) @decl`
	// W-C W6 V1.23 (2026-05-12): constructor_definition has no `name`
	// field — only `body` (function_body) + direct `parameter` children
	// + optional modifier_invocation. We synthesise "constructor" as the
	// identifier and pin id-hashing to the declaration node's StartByte.
	queryConstructor = `(constructor_definition) @decl`
	// W-C W6 V1.24 (2026-05-12): fallback_receive_definition is a single
	// node kind for both `fallback()` and `receive()` — tree-sitter does
	// not disambiguate them via fields. The walker reads the leading
	// source token at the declaration's StartByte to pick the synthetic
	// identifier ("fallback" or "receive").
	queryFallbackReceive = `(fallback_receive_definition) @decl`
	queryEvent           = `(event_definition name: (identifier) @name) @decl`
	queryStruct          = `(struct_declaration name: (identifier) @name) @decl`
	queryEnum            = `(enum_declaration name: (identifier) @name) @decl`
	// TODO(T19+): queryStateVar replaced by queryStateVarAll + runStateVarDecl
	// (mapping detection unified into one visitor pass).
	// queryStateVar = `(state_variable_declaration name: (identifier) @name) @decl`
	queryStateVarAll = `(state_variable_declaration) @decl`
	queryEmit        = `(emit_statement name: (expression (identifier) @event)) @stmt`
	queryHasModifier = `(modifier_invocation (identifier) @mod) @stmt`
	// W1 (Sol inheritance) — the `is`-clause exposes each parent as its own
	// inheritance_specifier sibling under contract_declaration /
	// interface_declaration (verified via AST dump 2026-05-11). Each
	// specifier wraps a user_defined_type whose first identifier is the
	// parent name (qualified names like `pkg.Type` are nested deeper but
	// the leading identifier still drives resolution).
	queryInheritance = `[
		(contract_declaration
			name: (identifier) @child
			(inheritance_specifier
				(user_defined_type (identifier) @parent)))
		(interface_declaration
			name: (identifier) @child
			(inheritance_specifier
				(user_defined_type (identifier) @parent)))
	]`
	// W6 (Sol using For) — `using LibName for TypeName;` directives inside
	// a contract / library / interface body.
	//
	// tree-sitter-solidity v1.2.11 grammar (vendored under ./binding):
	//   using_directive
	//     - `type_alias`     (legacy form: `using SafeMath for uint256`)
	//        └── identifier  ← library name
	//     - source field     (type_name OR any_source_type for `for *`)
	//
	// V0 captures only the legacy form's library identifier via type_alias.
	// V1.0 (2026-05-12) adds an additional `@type` capture on the source
	// field so the bound type can drive method-call dispatch resolution.
	//
	// W-C W6 V2.17 (2026-05-17) survey correction: prior V0/V2.5 spec
	// comments referenced a `using_alias` node type as the 0.8.13+ free-
	// function form child. Empirical AST dump 2026-05-17 against the
	// vendored grammar shows NO such node — `using_alias` is not a valid
	// node type, and any query referencing it fails to compile. The
	// operator-form variant `using {f as +} for T;` (Sol 0.8.19+) is
	// further grammar-blocked: the parser misinterprets the alias body
	// as a state_variable_declaration sequence wrapped in ERROR nodes.
	// Conclusion: operator-form is category A (grammar reject), NOT
	// category B (query gap) as V2.16 row 2 originally claimed.
	// V2.5/V2.7/V2.14 IOp 0-edge locks are correct as-is — the gap is
	// upstream of the query.
	//
	// V2.6's incidental capture of `using {Math.add, Math.sub} for T;`
	// works because the grammar interprets the qualified `Math.add` as
	// a type_alias-wrapped identifier (not a using_alias node). This
	// is also why the V0 query's existing `type_alias (identifier)`
	// pattern hits it.
	//
	// contract_body is the `body:` field of contract_declaration /
	// library_declaration / interface_declaration; using_directive nests
	// inside it.
	//
	// The `@stmt` capture is required because the matched node — the
	// using_directive itself — needs to be addressable for binding-map
	// PendingRef emission; without a separate capture, the runUsingFor
	// loop can only see the leaf identifiers.
	queryUsingFor = `[
		(contract_declaration
			name: (identifier) @container
			body: (contract_body
				(using_directive
					(type_alias (identifier) @lib)
					source: (_) @type) @stmt))
		(library_declaration
			name: (identifier) @container
			body: (contract_body
				(using_directive
					(type_alias (identifier) @lib)
					source: (_) @type) @stmt))
		(interface_declaration
			name: (identifier) @container
			body: (contract_body
				(using_directive
					(type_alias (identifier) @lib)
					source: (_) @type) @stmt))
	]`
)

// W6 V1.2 grammar limitation note (2026-05-12)
//
// File-level using directive (Solidity 0.8.13+, `using LibName for T;`
// at module scope outside any contract body) was attempted as the first
// V1.2 candidate but blocked by tree-sitter-solidity v1.2.13: the
// grammar's node-types.json lists `using_directive` as a valid
// source_file child, but the parser actually wraps such directives in
// ERROR nodes — meaning the lexer/grammar rules don't recognise this
// 0.8.13+ syntax yet (verified via AST dump 2026-05-12). Until the
// vendored grammar is bumped to a version that supports the new
// directive position, V1.2 lands the *Inherited using directive*
// carry-over instead. File-level handling deferred to V1.x once the
// grammar dependency upgrades.
