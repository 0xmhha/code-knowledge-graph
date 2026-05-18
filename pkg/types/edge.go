package types

// Edge mirrors the SQLite edges row (spec §5.3).
//
// DispatchKind (schema 1.7, Track C P1b): optional metadata column for the
// `invokes` edge type that disambiguates the dispatch mechanism. Valid values:
//
//	"interface_method" — callee is an interface method (virtual dispatch via
//	                     types.Selection on a *types.Interface receiver)
//	"func_value"       — callee is a function-typed variable / parameter
//	"method_value"     — callee is a struct field of function type
//	"closure"          — inline closure literal call: `func(){...}()`
//
// Empty string for every non-`invokes` edge AND for `invokes` edges that
// resolve as static (which shouldn't happen by construction — invokes is
// reserved for non-static dispatch). Old readers (schema ≤1.6) ignore the
// column gracefully because the SQLite ALTER ADD COLUMN keeps it nullable
// and SELECT projections in those readers don't reference it.
type Edge struct {
	ID           int64      `json:"id,omitempty"`
	Src          string     `json:"src"        validate:"required,len=16"`
	Dst          string     `json:"dst"        validate:"required,len=16"`
	Type         EdgeType   `json:"type"       validate:"required"`
	FilePath     string     `json:"file_path,omitempty"`
	Line         int        `json:"line,omitempty"`
	Count        int        `json:"count"      validate:"min=1"`
	Confidence   Confidence `json:"confidence" validate:"required"`
	DispatchKind string     `json:"dispatch_kind,omitempty"`
	// Order (W-C W7.3, 2026-05-18): source-order index for edges where
	// position is part of the relationship. Currently used by
	// EdgeHasModifier so multi-modifier functions preserve the
	// application sequence (Solidity applies modifiers outer-to-inner
	// in source order — `nonReentrant onlyOwner` wraps differently
	// from `onlyOwner nonReentrant`). Zero is omitted from JSON so
	// other edge types stay unaffected.
	Order int `json:"order,omitempty"`
}
