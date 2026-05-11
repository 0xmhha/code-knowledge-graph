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
)
