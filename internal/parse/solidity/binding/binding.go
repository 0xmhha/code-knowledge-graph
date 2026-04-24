// Package binding wraps the tree-sitter-solidity grammar (vendored from
// github.com/JoranHonig/tree-sitter-solidity v1.2.11, MIT-licensed) into a
// *sitter.Language usable by github.com/smacker/go-tree-sitter.
//
// We vendor instead of importing because the upstream repo ships only C/Node/
// Python/Rust/Swift bindings — no Go binding is published, and smacker/
// go-tree-sitter does not include a top-level `solidity` subpackage. See
// LICENSE in this directory for the upstream MIT terms.
package binding

// #include "tree_sitter/parser.h"
// const TSLanguage *tree_sitter_solidity();
import "C"

import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

// GetLanguage returns the Solidity *sitter.Language singleton.
func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_solidity())
	return sitter.NewLanguage(ptr)
}
