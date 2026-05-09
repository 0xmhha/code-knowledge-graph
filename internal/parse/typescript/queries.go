package typescript

// Tree-sitter queries derived from spec §4.6.2. Captures use `@name` markers
// that the parser reads positionally.
const (
	queryClass     = `(class_declaration name: (type_identifier) @name) @decl`
	queryInterface = `(interface_declaration name: (type_identifier) @name) @decl`
	queryFunction  = `(function_declaration name: (identifier) @name) @decl`
	queryMethod    = `(method_definition name: (property_identifier) @name) @decl`
	queryImport    = `(import_statement source: (string) @path) @decl`
	// TODO(T18+): export_statement support requires a distinct visitor (no @name capture).
	// queryExport = `(export_statement) @decl`
	queryDecorator = `(decorator (call_expression function: (identifier) @name)) @decl`
	queryTypeAlias = `(type_alias_declaration name: (type_identifier) @name) @decl`
	queryEnum      = `(enum_declaration name: (identifier) @name) @decl`

	// P3 (TS body walk): captures every call expression in the parse tree
	// regardless of which function/method contains it. The body-walk pass
	// then maps each call's byte position back to the smallest enclosing
	// Function/Method node we already emitted, anchoring the pending ref
	// on the caller. Two callee shapes:
	//
	//   - bare identifier:       foo()         → @callee = foo
	//   - selector expression:   obj.foo()     → @callee = foo (the property)
	//
	// Receiver of the selector (the `obj` part) is not captured — V0 TS
	// resolution is name-based, the same way Go's pending_refs queue works
	// before its Pass-2 type-aware union. Type-aware dispatch (interface
	// method / func value / closure) is out of scope until a TS LSP server
	// is embedded; Track C did the equivalent for Go via go/packages.
	queryCallExpression = `(call_expression
  function: [
    (identifier) @callee
    (member_expression property: (property_identifier) @callee)
  ]
) @call`
)
