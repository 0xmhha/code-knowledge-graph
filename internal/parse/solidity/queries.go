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
	queryContract    = `(contract_declaration name: (identifier) @name) @decl`
	queryLibrary     = `(library_declaration name: (identifier) @name) @decl`
	queryInterface   = `(interface_declaration name: (identifier) @name) @decl`
	queryFunction    = `(function_definition name: (identifier) @name) @decl`
	queryModifier    = `(modifier_definition name: (identifier) @name) @decl`
	queryEvent       = `(event_definition name: (identifier) @name) @decl`
	queryStruct      = `(struct_declaration name: (identifier) @name) @decl`
	queryEnum        = `(enum_declaration name: (identifier) @name) @decl`
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
	// tree-sitter-solidity v1.2.13 grammar:
	//   using_directive
	//     - `type_alias`     (legacy form: `using SafeMath for uint256`)
	//        └── identifier  ← library name
	//     - `using_alias`    (0.8.13+ free-function form `using {f} for T`)
	//     - source field     (type_name OR any_source_type for `for *`)
	//
	// V0 captures only the legacy form's library identifier via type_alias.
	// The free-function form (using_alias child) is dropped per §4.6.6 V0
	// limit. The source field (typeName / *) is parsed but not surfaced —
	// one EdgeUsesFor per directive regardless of bound type.
	//
	// contract_body is the `body:` field of contract_declaration /
	// library_declaration / interface_declaration; using_directive nests
	// inside it.
	queryUsingFor = `[
		(contract_declaration
			name: (identifier) @container
			body: (contract_body
				(using_directive
					(type_alias (identifier) @lib))))
		(library_declaration
			name: (identifier) @container
			body: (contract_body
				(using_directive
					(type_alias (identifier) @lib))))
		(interface_declaration
			name: (identifier) @container
			body: (contract_body
				(using_directive
					(type_alias (identifier) @lib))))
	]`
)
