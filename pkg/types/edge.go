package types

// Edge mirrors the SQLite edges row (spec §5.3).
type Edge struct {
	ID         int64      `json:"id,omitempty"`
	Src        string     `json:"src"        validate:"required,len=16"`
	Dst        string     `json:"dst"        validate:"required,len=16"`
	Type       EdgeType   `json:"type"       validate:"required"`
	FilePath   string     `json:"file_path,omitempty"`
	Line       int        `json:"line,omitempty"`
	Count      int        `json:"count"      validate:"min=1"`
	Confidence Confidence `json:"confidence" validate:"required"`
}
