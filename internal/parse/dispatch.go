package parse

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Registry maps file extensions to parser instances.
type Registry struct {
	parsers map[string]Parser
}

// NewRegistry returns an empty registry. Use Register to add parsers.
func NewRegistry() *Registry {
	return &Registry{parsers: map[string]Parser{}}
}

// Register associates each of p.Extensions() with p.
// Returns an error if any extension is already registered.
func (r *Registry) Register(p Parser) error {
	for _, ext := range p.Extensions() {
		ext = strings.ToLower(ext)
		if _, ok := r.parsers[ext]; ok {
			return fmt.Errorf("extension %s registered twice", ext)
		}
		r.parsers[ext] = p
	}
	return nil
}

// For returns the parser registered for path's extension, or nil if none.
func (r *Registry) For(path string) Parser {
	return r.parsers[strings.ToLower(filepath.Ext(path))]
}
