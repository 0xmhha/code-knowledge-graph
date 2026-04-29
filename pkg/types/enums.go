package types

// NodeType enumerates the 30 node kinds (spec §5.1; v0.2 schema 1.1 added Mutex).
type NodeType string

const (
	NodePackage       NodeType = "Package"
	NodeFile          NodeType = "File"
	NodeStruct        NodeType = "Struct"
	NodeInterface     NodeType = "Interface"
	NodeClass         NodeType = "Class"
	NodeTypeAlias     NodeType = "TypeAlias"
	NodeEnum          NodeType = "Enum"
	NodeContract      NodeType = "Contract"
	NodeMapping       NodeType = "Mapping"
	NodeEvent         NodeType = "Event"
	NodeFunction      NodeType = "Function"
	NodeMethod        NodeType = "Method"
	NodeModifier      NodeType = "Modifier"
	NodeConstructor   NodeType = "Constructor"
	NodeConstant      NodeType = "Constant"
	NodeVariable      NodeType = "Variable"
	NodeField         NodeType = "Field"
	NodeParameter     NodeType = "Parameter"
	NodeLocalVariable NodeType = "LocalVariable"
	NodeImport        NodeType = "Import"
	NodeExport        NodeType = "Export"
	NodeDecorator     NodeType = "Decorator"
	NodeGoroutine     NodeType = "Goroutine"
	NodeChannel       NodeType = "Channel"
	// NodeMutex: schema 1.1 slot reservation (spec v0.2 §2). B1 (Wave 5) will
	// emit; the parser does not produce Mutex nodes yet. Kept adjacent to
	// the other concurrency nodes (Goroutine/Channel) for grouping.
	NodeMutex      NodeType = "Mutex"
	NodeIfStmt     NodeType = "IfStmt"
	NodeLoopStmt   NodeType = "LoopStmt"
	NodeCallSite   NodeType = "CallSite"
	NodeReturnStmt NodeType = "ReturnStmt"
	NodeSwitchStmt NodeType = "SwitchStmt"
)

// AllNodeTypes returns all 30 node types in a stable order.
// NOTE: order is load-bearing for downstream tests/IDs — append-only.
// NodeMutex is inserted after NodeChannel (concurrency family group);
// statement nodes (NodeIfStmt..NodeSwitchStmt) keep their relative order.
func AllNodeTypes() []NodeType {
	return []NodeType{
		NodePackage, NodeFile, NodeStruct, NodeInterface, NodeClass,
		NodeTypeAlias, NodeEnum, NodeContract, NodeMapping, NodeEvent,
		NodeFunction, NodeMethod, NodeModifier, NodeConstructor,
		NodeConstant, NodeVariable, NodeField, NodeParameter, NodeLocalVariable,
		NodeImport, NodeExport, NodeDecorator,
		NodeGoroutine, NodeChannel, NodeMutex,
		NodeIfStmt, NodeLoopStmt, NodeCallSite, NodeReturnStmt, NodeSwitchStmt,
	}
}

// EdgeType enumerates the 25 edge kinds (spec §5.2; v0.2 schema 1.1 added 3 lock edges).
type EdgeType string

const (
	EdgeContains      EdgeType = "contains"
	EdgeDefines       EdgeType = "defines"
	EdgeCalls         EdgeType = "calls"
	EdgeInvokes       EdgeType = "invokes"
	EdgeUsesType      EdgeType = "uses_type"
	EdgeInstantiates  EdgeType = "instantiates"
	EdgeReferences    EdgeType = "references"
	EdgeReadsField    EdgeType = "reads_field"
	EdgeWritesField   EdgeType = "writes_field"
	EdgeImports       EdgeType = "imports"
	EdgeExports       EdgeType = "exports"
	EdgeImplements    EdgeType = "implements"
	EdgeExtends       EdgeType = "extends"
	EdgeHasModifier   EdgeType = "has_modifier"
	EdgeEmitsEvent    EdgeType = "emits_event"
	EdgeReadsMapping  EdgeType = "reads_mapping"
	EdgeWritesMapping EdgeType = "writes_mapping"
	EdgeHasDecorator  EdgeType = "has_decorator"
	EdgeSpawns        EdgeType = "spawns"
	EdgeSendsTo       EdgeType = "sends_to"
	EdgeRecvsFrom     EdgeType = "recvs_from"
	EdgeBindsTo       EdgeType = "binds_to"
	// Schema 1.1 slot reservations (spec v0.2 §2): concurrency lock semantics.
	// B1 (Wave 5) will emit these from the Go Mutex AST pass; the parser does
	// not produce them yet. Appended (not interleaved) so existing edge-type
	// hash positions / test snapshots stay stable.
	EdgeAcquiresLock      EdgeType = "acquires_lock"
	EdgeReleasesLock      EdgeType = "releases_lock"
	EdgeAccessedUnderLock EdgeType = "accessed_under_lock"
)

// AllEdgeTypes returns all 25 edge types in stable order.
// Append-only: existing positions are load-bearing for hash-derived IDs.
func AllEdgeTypes() []EdgeType {
	return []EdgeType{
		EdgeContains, EdgeDefines, EdgeCalls, EdgeInvokes, EdgeUsesType,
		EdgeInstantiates, EdgeReferences, EdgeReadsField, EdgeWritesField,
		EdgeImports, EdgeExports, EdgeImplements, EdgeExtends,
		EdgeHasModifier, EdgeEmitsEvent, EdgeReadsMapping, EdgeWritesMapping,
		EdgeHasDecorator, EdgeSpawns, EdgeSendsTo, EdgeRecvsFrom, EdgeBindsTo,
		EdgeAcquiresLock, EdgeReleasesLock, EdgeAccessedUnderLock,
	}
}

// Confidence labels (spec §4.8).
type Confidence string

const (
	ConfExtracted Confidence = "EXTRACTED"
	ConfInferred  Confidence = "INFERRED"
	ConfAmbiguous Confidence = "AMBIGUOUS"
)

// Valid reports whether c is one of the three known confidence labels.
func (c Confidence) Valid() bool {
	switch c {
	case ConfExtracted, ConfInferred, ConfAmbiguous:
		return true
	}
	return false
}
