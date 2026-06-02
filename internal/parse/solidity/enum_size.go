package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Sol W-C W9 V14 (2026-05-19) — enum byte footprint computation.
//
// Sol enum runtime size depends on the variant count: ≤256 -> 1
// byte (uint8), ≤65536 -> 2 bytes (uint16), ≤2^32 -> 4 bytes,
// otherwise 8 bytes. Solidity caps variants at 256 in practice
// but the size lookup handles the larger cases for completeness.
//
// computeEnumSizes walks every enum_declaration in the file,
// counts its enum_value children, and records (name -> bytes) so
// runStateVarDecl's slot packing can call solValueTypeSize first
// and fall back to this index before the conservative full-slot
// path. Cross-file enum references stay on the conservative
// fallback in V14; cross-file propagation is W9 V15+.
//
// Bumping to ≤256 -> 1 byte unlocks the typical enum packing
// scenario (a few small enum fields with primitive neighbours
// sharing slot 0).

func (v *declVisitor) computeEnumSizes() {
	if v.root == nil {
		return
	}
	query, qErr := sitter.NewQuery(v.lang, queryEnum)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		var nameNode, declNode *sitter.Node
		for _, c := range m.Captures {
			n := c.Node
			switch names[c.Index] {
			case "name":
				nameNode = &n
			case "decl":
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		name := nameNode.Utf8Text(v.src)
		variantCount := 0
		for i := uint(0); i < declNode.NamedChildCount(); i++ {
			child := declNode.NamedChild(i)
			if child == nil {
				continue
			}
			if child.Kind() == "enum_value" {
				variantCount++
			}
		}
		v.enumSizes[name] = enumByteWidth(variantCount)
	}
}

// enumByteWidth returns the byte footprint Sol assigns to an enum
// with the given variant count. The mapping follows EVM packing:
//
//	  1..256 variants -> 1 byte  (uint8)
//	257..65536 variants -> 2 bytes (uint16)
//	65537..2^32 variants -> 4 bytes (uint32)
//	larger -> 8 bytes (uint64; Sol's compiler rejects this in
//	         practice but the lookup stays safe for any input)
//
// Zero or negative variant counts default to 1 byte — the parser
// always sees at least one variant for a well-formed enum, and
// the conservative-but-small default keeps regression risk near
// zero.
func enumByteWidth(variants int) int {
	switch {
	case variants <= 256:
		return 1
	case variants <= 65536:
		return 2
	case variants <= (1 << 32):
		return 4
	default:
		return 8
	}
}
