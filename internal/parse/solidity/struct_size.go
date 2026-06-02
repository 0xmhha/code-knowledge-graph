package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Sol W-C W9 V5 (2026-05-19) — struct field aggregation for storage
// slot packing.
//
// Per Sol §11.1, a storage-located struct packs its fields the same
// way top-level state variables pack — each field consumes
// solTypeSize bytes, sub-32 fields share slots, fields that don't
// fit the remaining slot advance to the next. The struct as a whole
// begins on a slot boundary and the next variable following it also
// starts fresh.
//
// computeStructSizes walks every struct_declaration in the file and
// records the total byte footprint into v.structSizes (keyed by
// struct name). The byte total — not slot count — lets the same
// advanceForArrayField path that handles fixed-size arrays place
// struct state-vars by reusing the ceil(bytes / 32) slot math.
//
// V5 scope limitations (call out as separate V6+ work):
//   - Cross-file struct references resolve through user_defined_type
//     identifiers, but cross-file struct sizes aren't propagated yet
//     (each file computes its own structSizes from its local
//     declarations). State-vars typed as a cross-file struct fall
//     back to the conservative full-slot path.
//   - Mapping fields inside structs use the full-slot mapping
//     advance (Sol stores each mapping's data at hashed locations
//     but the declaration slot consumes one position).
//   - Dynamic-array, string, and bytes fields inside structs use the
//     conservative 32-byte slot, matching Sol's "reference types
//     occupy the declaration slot" rule.

func (v *declVisitor) computeStructSizes() {
	if v.root == nil {
		return
	}
	query, qErr := sitter.NewQuery(v.lang, queryStruct)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	// Collect (name, declNode) pairs first so the recursive size
	// calculation can resolve struct→struct references in two passes.
	type pending struct {
		name string
		body *sitter.Node
	}
	var queue []pending
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		var nameNode *sitter.Node
		var declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		body := declNode.ChildByFieldName("body")
		if body == nil {
			continue
		}
		queue = append(queue, pending{
			name: nameNode.Utf8Text(v.src),
			body: body,
		})
	}
	// Fixed-point loop: compute sizes for structs whose member types
	// are resolved. A struct whose member references another struct
	// declared later in the file becomes resolvable on the next
	// iteration. The loop terminates after at most len(queue) passes
	// because each pass either makes progress or all remaining
	// structs have cross-file dependencies that V5 doesn't follow.
	pendingByName := map[string]*sitter.Node{}
	for _, p := range queue {
		pendingByName[p.name] = p.body
	}
	for changed := true; changed; {
		changed = false
		for name, body := range pendingByName {
			size, ok := v.tryComputeStructBytes(body)
			if !ok {
				continue
			}
			v.structSizes[name] = size
			delete(pendingByName, name)
			changed = true
		}
	}
}

// tryComputeStructBytes returns the total byte footprint of a struct
// body if every member type is resolvable (primitive, fixed array of
// primitives, or another struct already in v.structSizes). Returns
// ok=false when at least one member references a struct whose size
// hasn't been computed yet — the caller retries on the next pass.
func (v *declVisitor) tryComputeStructBytes(body *sitter.Node) (int, bool) {
	if body == nil {
		return 0, false
	}
	state := slotState{}
	for i := uint(0); i < uint(body.NamedChildCount()); i++ {
		member := body.NamedChild(i)
		if member == nil || member.Kind() != "struct_member" {
			continue
		}
		typeNode := member.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		if typeNameIsMapping(typeNode, v.src) {
			_, state = advanceForMapping(state)
			continue
		}
		sig := extractTypeNameText(typeNode, v.src)
		if arrayBytes, ok := solFixedArrayBytes(sig); ok {
			_, state = advanceForArrayField(state, arrayBytes)
			continue
		}
		if size, ok := solValueTypeSize(sig); ok {
			_, state = advanceForField(state, size)
			continue
		}
		if nested, ok := v.structSizes[sig]; ok {
			_, state = advanceForArrayField(state, nested)
			continue
		}
		// Unknown type (could be a struct not yet computed, or a
		// cross-file reference). Bail so the caller retries; if
		// after fixed-point the size is still unknown, the field
		// reverts to the conservative full-slot advance at the
		// top level.
		return 0, false
	}
	if state.used > 0 {
		state.slot++
	}
	return state.slot * 32, true
}
