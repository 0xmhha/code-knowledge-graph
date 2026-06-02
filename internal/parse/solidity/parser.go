// Package solidity implements the CKG parser for .sol files (spec §4.6.3).
//
// We use a vendored copy of github.com/JoranHonig/tree-sitter-solidity v1.2.11
// (LANGUAGE_VERSION=14). Upstream github.com/tree-sitter/go-tree-sitter v0.25
// supports the ABI window MIN_COMPATIBLE_LANGUAGE_VERSION..LANGUAGE_VERSION
// (13..15 at the time of writing), so the vendored grammar loads without
// regeneration.
package solidity

import (
	"fmt"
	"path/filepath"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	solang "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity/binding"
)

// Parser implements parse.Parser for Solidity source.
//
// abi accumulates per-contract function signatures across ParseFile calls
// so that the cross-language linker (T20) can match them to TypeScript
// classes by name. Mutated in place from declVisitor.collectABI under
// abiMu — buildpipe runs ParseFile concurrently across files.
type Parser struct {
	srcRoot string
	abiMu   sync.Mutex
	abi     map[string][]ABISig
	// W-C W9 V6 (2026-05-19): cross-file struct byte footprints.
	// ParseFile runs per-file Pass 1 which computes structSizes from
	// the file's own struct declarations. Cross-file refs (state-var
	// typed as a struct declared in another file) need the union of
	// every file's sizes; ParseFile merges its local map here under
	// structMu so Resolve can read a complete table when correcting
	// NodeField SlotIndex for cross-file references.
	structMu    sync.Mutex
	structSizes map[string]int
}

// New returns a Parser rooted at srcRoot (used for relative file paths).
func New(srcRoot string) *Parser {
	return &Parser{
		srcRoot:     srcRoot,
		abi:         map[string][]ABISig{},
		structSizes: map[string]int{},
	}
}

// Extensions reports the file extensions this parser handles.
func (p *Parser) Extensions() []string { return []string{".sol"} }

// ParseFile runs Pass 1 over a single .sol file.
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	lang := solang.GetLanguage()
	parser := sitter.NewParser()
	defer func() { parser.Close() }()
	if err := parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("solidity: SetLanguage: %w", err)
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("solidity: parser returned nil tree for %s", rel)
	}
	defer func() { tree.Close() }()
	v := newDeclVisitor(rel, src, lang, tree.RootNode())
	v.visit()
	// Merge per-visitor abi into the shared Parser.abi under lock —
	// ParseFile is dispatched concurrently across files (buildpipe).
	if len(v.abi) > 0 {
		p.abiMu.Lock()
		for contract, sigs := range v.abi {
			p.abi[contract] = append(p.abi[contract], sigs...)
		}
		p.abiMu.Unlock()
	}
	// W-C W9 V6 (2026-05-19): merge per-file struct sizes so cross-
	// file refs can be resolved during Pass 2. Conflicts (same struct
	// name with different sizes across files) prefer the larger
	// value — defensive against incomplete declarations.
	if len(v.structSizes) > 0 {
		p.structMu.Lock()
		for name, size := range v.structSizes {
			if existing, has := p.structSizes[name]; !has || size > existing {
				p.structSizes[name] = size
			}
		}
		p.structMu.Unlock()
	}
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// ABI returns the per-contract signatures collected during ParseFile.
// Used by the cross-language linker (T20). Caller is expected to invoke
// ABI only after all ParseFile calls have completed (buildpipe enforces
// this by collecting all results before reading ABI). The mutex guard is
// defensive — under the documented call sequence there is no contention.
func (p *Parser) ABI() map[string][]ABISig {
	p.abiMu.Lock()
	defer p.abiMu.Unlock()
	out := make(map[string][]ABISig, len(p.abi))
	for k, v := range p.abi {
		out[k] = append([]ABISig(nil), v...)
	}
	return out
}

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
