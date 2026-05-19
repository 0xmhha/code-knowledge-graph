package solidity

// Sol W-C W9 V7 (2026-05-19) — C3 linearization for inheritance
// slot offset deduplication.
//
// W9 V1 cumulated parent slot counts via a depth-first walk of the
// inheritance adjacency. Diamond inheritance (two parents that
// both extend the same grandparent) over-counted the shared
// ancestor's slots. Solidity solves this with C3 linearization,
// the same algorithm Python and Dylan use for multiple-
// inheritance method resolution order (MRO). Sol's reference
// compiler applies C3 to storage layout: the storage of the most-
// base class lands first, then each next class in the
// linearization, then the derived contract's own state vars.
//
// computeC3Linearization returns each contract's full MRO list
// (self first, then bases derived -> base). The caller dedupes
// inherited slot counts by summing slotCount(c) over the
// linearization excluding the contract itself.
//
// Reference: Barrett et al. "A Monotonic Superclass Linearization
// for Dylan" (OOPSLA '96) — the algorithm Python adopted in
// PEP 3119 and Solidity's reference implementation in libsolidity.
//
// V7 fallback behaviour: if a class hierarchy is inconsistent
// (no C3 linearization exists — Sol's compiler rejects this, but
// the grammar accepts it), the function falls back to the V1
// depth-first order so storage layout stays computable. The
// caller's tests still observe a deterministic answer.

// computeC3Linearization returns mro[contractID] = [self, parent1,
// parent2, grandparent, …] for every contractID reachable from
// `parents`. The returned slice is owned by the caller; mutation
// is allowed but unnecessary.
func computeC3Linearization(parents map[string][]string) map[string][]string {
	mro, _ := computeC3LinearizationWithFallbacks(parents)
	return mro
}

// computeC3LinearizationWithFallbacks is the W-C W9 V8 (2026-05-19)
// variant that additionally returns the set of contract IDs whose
// hierarchy had no consistent C3 linearization and degraded to the
// depth-first fallback. Downstream tooling can flag those nodes
// (HasInheritanceMROFallback) so the would-be-rejected hierarchy
// is visible without re-running solc.
func computeC3LinearizationWithFallbacks(parents map[string][]string) (map[string][]string, map[string]bool) {
	out := make(map[string][]string, len(parents))
	fallback := map[string]bool{}
	visiting := map[string]bool{}
	var lin func(id string) []string
	lin = func(id string) []string {
		if cached, ok := out[id]; ok {
			return cached
		}
		if visiting[id] {
			// Cycle defence: degrade to the V1 walk (just self).
			return []string{id}
		}
		visiting[id] = true
		defer delete(visiting, id)

		ps := parents[id]
		if len(ps) == 0 {
			res := []string{id}
			out[id] = res
			return res
		}
		// Build the list of parent linearizations plus the bare
		// parent order — the canonical C3 inputs.
		lists := make([][]string, 0, len(ps)+1)
		for _, p := range ps {
			parentLin := lin(p)
			lists = append(lists, append([]string(nil), parentLin...))
		}
		lists = append(lists, append([]string(nil), ps...))

		merged := mergeC3(lists)
		if merged == nil {
			// Inconsistent hierarchy. Fall back to the V1 depth-
			// first walk (self + every reachable ancestor in
			// source order, no dedup) and flag the diagnostic.
			merged = depthFirstAncestors(id, parents)
			fallback[id] = true
		}
		res := append([]string{id}, merged...)
		out[id] = res
		return res
	}
	for id := range parents {
		lin(id)
	}
	return out, fallback
}

// mergeC3 runs the C3 merge step over a list of candidate lists.
// At each iteration it picks a "good head" — the first element of
// some list that doesn't appear in the tail of any other list —
// appends it to the result, and removes it from the front of any
// list whose head matches. Returns nil when no good head exists
// (inconsistent MRO).
func mergeC3(lists [][]string) []string {
	var out []string
	for {
		// Drop empty lists.
		filtered := lists[:0]
		for _, L := range lists {
			if len(L) > 0 {
				filtered = append(filtered, L)
			}
		}
		lists = filtered
		if len(lists) == 0 {
			return out
		}
		var head string
		good := false
		for _, L := range lists {
			cand := L[0]
			inTail := false
			for _, K := range lists {
				for i := 1; i < len(K); i++ {
					if K[i] == cand {
						inTail = true
						break
					}
				}
				if inTail {
					break
				}
			}
			if !inTail {
				head = cand
				good = true
				break
			}
		}
		if !good {
			return nil
		}
		out = append(out, head)
		// Remove `head` from the front of every list that starts
		// with it.
		for i, L := range lists {
			if len(L) > 0 && L[0] == head {
				lists[i] = L[1:]
			}
		}
	}
}

// depthFirstAncestors walks parents[id] recursively in source
// order, returning every reachable ancestor (excluding self). Used
// as the C3 fallback when no consistent linearization exists; it
// reproduces the W9 V1 walk order so the inconsistent-hierarchy
// case stays observably identical to pre-V7 behaviour.
func depthFirstAncestors(id string, parents map[string][]string) []string {
	var out []string
	seen := map[string]bool{id: true}
	var rec func(string)
	rec = func(cid string) {
		for _, p := range parents[cid] {
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
			rec(p)
		}
	}
	rec(id)
	return out
}
