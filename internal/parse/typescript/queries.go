package typescript

// Tree-sitter queries derived from spec §4.6.2. Captures use `@name` markers
// that the parser reads positionally.
const (
	queryClass     = `(class_declaration name: (type_identifier) @name) @decl`
	queryInterface = `(interface_declaration name: (type_identifier) @name) @decl`
	queryFunction  = `(function_declaration name: (identifier) @name) @decl`
	queryMethod    = `(method_definition name: (property_identifier) @name) @decl`
	queryImport    = `(import_statement source: (string) @path) @decl`
	queryExport    = `(export_statement) @decl`
	queryDecorator = `(decorator (call_expression function: (identifier) @name)) @decl`
	queryTypeAlias = `(type_alias_declaration name: (type_identifier) @name) @decl`
	queryEnum      = `(enum_declaration name: (identifier) @name) @decl`
)
