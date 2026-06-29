package golang_test

import (
	"reflect"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
)

// TestCanonicalID_IntegrationContract_DeterministicAndAlignable locks the CKG↔CKV
// join contract agreed in the 2026-06-29 coordination (ADR-0001 identity +
// ADR-0002 determinism). ckv's internal/ckgalign attaches ckg's canonical_id to a
// chunk by looking the node up positionally (FilePath + StartLine/EndLine) and
// copying its canonical_id, then cks resolves on that build-stable key. Two
// properties must therefore hold on the CKG side:
//
//	(a) deterministic — the same source produces the same canonical_id on every
//	    rebuild, so the key is stable across builds (not order-dependent).
//	(b) alignable — every node that carries a canonical_id also carries a usable
//	    (FilePath, StartLine, EndLine) position; a canonical_id with no position
//	    could never be attached by ckgalign's positional lookup.
//
// This is the CKG half of the shared integration fixture; the ckv half asserts a
// chunk inherits the identical id for the same (file, line) span.
func TestCanonicalID_IntegrationContract_DeterministicAndAlignable(t *testing.T) {
	type pos struct {
		file       string
		start, end int
	}
	collect := func() (map[string]string, map[pos]string) {
		g, err := gop.LoadAndResolve("testdata/resolve")
		if err != nil {
			t.Fatalf("LoadAndResolve: %v", err)
		}
		byQname := map[string]string{}
		byPos := map[pos]string{}
		for _, n := range g.Nodes {
			if n.CanonicalID == "" {
				continue
			}
			byQname[n.QualifiedName] = n.CanonicalID
			// (b) alignment precondition: a canonical_id-bearing symbol must be
			// positionable, or ckgalign can never attach it.
			if n.FilePath == "" || n.StartLine <= 0 || n.EndLine < n.StartLine {
				t.Errorf("canonical_id %q on %q has no usable position: file=%q start=%d end=%d",
					n.CanonicalID, n.QualifiedName, n.FilePath, n.StartLine, n.EndLine)
			}
			byPos[pos{n.FilePath, n.StartLine, n.EndLine}] = n.CanonicalID
		}
		if len(byQname) == 0 {
			t.Fatal("fixture produced no canonical_id-bearing symbol nodes")
		}
		return byQname, byPos
	}

	q1, p1 := collect()
	q2, p2 := collect()

	// (a) determinism: two independent builds of the same source must agree on
	// canonical_id, both by qualified_name and by (file, line) span.
	if !reflect.DeepEqual(q1, q2) {
		t.Errorf("canonical_id-by-qname not deterministic across rebuilds (%d vs %d entries)", len(q1), len(q2))
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Errorf("canonical_id-by-position not deterministic across rebuilds (%d vs %d entries)", len(p1), len(p2))
	}
}
