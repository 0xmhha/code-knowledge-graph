package types

// Node mirrors the SQLite nodes row plus runtime fields (spec §5.3).
type Node struct {
	ID            string     `json:"id"             validate:"required,len=16"`
	Type          NodeType   `json:"type"           validate:"required"`
	Name          string     `json:"name"           validate:"required"`
	QualifiedName string     `json:"qualified_name" validate:"required"`
	FilePath      string     `json:"file_path"      validate:"required"`
	StartLine     int        `json:"start_line"     validate:"min=1"`
	EndLine       int        `json:"end_line"       validate:"min=1"`
	StartByte     int        `json:"start_byte"     validate:"min=0"`
	EndByte       int        `json:"end_byte"       validate:"gtfield=StartByte"`
	Language      string     `json:"language"       validate:"required,oneof=go ts sol proto"`
	Visibility    string     `json:"visibility,omitempty"`
	Signature     string     `json:"signature,omitempty"`
	DocComment    string     `json:"doc_comment,omitempty"`
	Complexity    int        `json:"complexity,omitempty"`
	InDegree      int        `json:"in_degree"`
	OutDegree     int        `json:"out_degree"`
	PageRank      float64    `json:"pagerank"`
	UsageScore    float64    `json:"usage_score"`
	Confidence    Confidence `json:"confidence"     validate:"required"`
	SubKind       string     `json:"sub_kind,omitempty"`
	// SlotIndex (W-C W9 V0, 2026-05-18): EVM storage slot index for
	// Solidity state variables (NodeField). V0 is per-contract
	// declaration-order index (0, 1, 2, ...) — bit-packing and
	// inheritance offsets are deferred to V1+. Omitted from JSON for
	// non-state-var nodes and for NodeField rows where the value is
	// the zero default.
	SlotIndex int `json:"slot_index,omitempty"`
	// HasAssembly (W-C W10 V0, 2026-05-18): true when a Solidity
	// callable (function / modifier / constructor / fallback) contains
	// at least one `assembly { ... }` block in its body. Lets
	// downstream consumers run a basic "show me all functions with
	// inline assembly" query without re-parsing source. V0 detects
	// presence only; Yul-internal op detection (delegatecall, sstore,
	// selfdestruct, …) and receiver resolution are deferred to V1+.
	HasAssembly bool `json:"has_assembly,omitempty"`
}
