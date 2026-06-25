package impact

import (
	"sort"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// nid pads a short label to the 16-char node-id width the schema expects.
func nid(s string) string {
	if len(s) >= 16 {
		return s[:16]
	}
	return s + strings.Repeat("_", 16-len(s))
}

// newImpactStore builds a small reverse-dependency graph exercising every
// output bucket plus the tricky cases: a node reached by two edge classes
// (lands in both buckets), a node reachable only via a mixed-edge path (lands
// in none — per-group traversal is edge-class-restricted), and a node missing
// a citation. Reverse impact: an edge X --type--> S means "X depends on S", so
// reverse traversal from S reaches X.
//
//	S   seed (pkg.S)
//	C1  --calls-->      S          callers, depth 1
//	C2  --calls-->      C1         callers, depth 2
//	R1  --references--> S          other_refs, depth 1
//	T1  --uses_type-->  S          type_users, depth 1
//	I1  --implements--> S          interface_impact, depth 1
//	MB  --calls-->      S          callers AND
//	MB  --references--> S          other_refs   (same node, two buckets)
//	M   --references--> C1         mixed path (refs->calls): in NO bucket at d2
//	NC  --calls-->      S          callers, but FilePath="" -> missing-citation
func newImpactStore(t *testing.T) persist.StoreReader {
	t.Helper()
	dbPath := t.TempDir() + "/impact.db"
	s, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	fn := func(id, qname, file string, line int) types.Node {
		return types.Node{
			ID: nid(id), Type: types.NodeFunction, Name: id,
			QualifiedName: qname, FilePath: file, StartLine: line, EndLine: line + 1,
			StartByte: 0, EndByte: 10, Language: "go", Confidence: types.ConfExtracted,
		}
	}
	nodes := []types.Node{
		fn("S", "pkg.S", "pkg/s.go", 10),
		fn("C1", "pkg.C1", "pkg/c1.go", 5),
		fn("C2", "pkg.C2", "pkg/c2.go", 5),
		fn("R1", "pkg.R1", "pkg/r1.go", 5),
		fn("T1", "pkg.T1", "pkg/t1.go", 5),
		fn("I1", "pkg.I1", "pkg/i1.go", 5),
		fn("MB", "pkg.MB", "pkg/mb.go", 5),
		fn("M", "pkg.M", "pkg/m.go", 5),
		fn("NC", "pkg.NC", "", 0), // missing citation
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	e := func(src, dst string, et types.EdgeType) types.Edge {
		return types.Edge{Src: nid(src), Dst: nid(dst), Type: et, Count: 1, Confidence: types.ConfExtracted}
	}
	edges := []types.Edge{
		e("C1", "S", types.EdgeCalls),
		e("C2", "C1", types.EdgeCalls),
		e("R1", "S", types.EdgeReferences),
		e("T1", "S", types.EdgeUsesType),
		e("I1", "S", types.EdgeImplements),
		e("MB", "S", types.EdgeCalls),
		e("MB", "S", types.EdgeReferences),
		e("M", "C1", types.EdgeReferences),
		e("NC", "S", types.EdgeCalls),
	}
	if err := s.InsertEdges(edges); err != nil {
		t.Fatalf("InsertEdges: %v", err)
	}
	if err := s.RebuildFTS(); err != nil {
		t.Fatalf("RebuildFTS: %v", err)
	}
	return s
}

// bucketIDs returns the sorted node ids in resp["impact"][key].
func bucketIDs(t *testing.T, resp map[string]any, key string) []string {
	t.Helper()
	impact, ok := resp["impact"].(map[string]any)
	if !ok {
		t.Fatalf("resp[impact] missing or wrong type: %T", resp["impact"])
	}
	entries, ok := impact[key].([]map[string]any)
	if !ok {
		t.Fatalf("bucket %q missing or wrong type: %T", key, impact[key])
	}
	var ids []string
	for _, m := range entries {
		ids = append(ids, m["id"].(string))
	}
	sort.Strings(ids)
	return ids
}

func eqIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	w := make([]string, len(want))
	for i, x := range want {
		w[i] = nid(x)
	}
	sort.Strings(w)
	if strings.Join(got, ",") != strings.Join(w, ",") {
		t.Errorf("ids = %v; want %v", got, w)
	}
}

func TestCompute_BucketsByEdgeClass(t *testing.T) {
	s := newImpactStore(t)
	resp, err := Compute(s, "pkg.S", "", Options{Depth: 2})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	// callers follows calls/invokes only: C1, C2 (transitive), MB, NC.
	eqIDs(t, bucketIDs(t, resp, "callers"), "C1", "C2", "MB", "NC")
	// other_refs follows references: R1, MB.
	eqIDs(t, bucketIDs(t, resp, "other_refs"), "R1", "MB")
	eqIDs(t, bucketIDs(t, resp, "type_users"), "T1")
	eqIDs(t, bucketIDs(t, resp, "interface_impact"), "I1")
	// empty buckets are present (no nil-check burden on consumers).
	eqIDs(t, bucketIDs(t, resp, "concurrent"))
	eqIDs(t, bucketIDs(t, resp, "distributed"))
}

func TestCompute_MixedPathNotBucketed(t *testing.T) {
	s := newImpactStore(t)
	resp, err := Compute(s, "pkg.S", "", Options{Depth: 2})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// M reaches S only via references->calls (mixed). No single edge-class
	// path exists, so M must appear in no bucket and not in totals.
	for _, key := range []string{"callers", "other_refs", "type_users", "interface_impact", "concurrent", "distributed"} {
		for _, id := range bucketIDs(t, resp, key) {
			if id == nid("M") {
				t.Errorf("mixed-path node M leaked into bucket %q", key)
			}
		}
	}
	totals := resp["totals"].(map[string]any)
	if got := totals["nodes"].(int); got != 7 {
		t.Errorf("totals.nodes = %d; want 7 (C1,C2,R1,T1,I1,MB,NC — not seed, not M)", got)
	}
}

func TestCompute_SeedExcludedAndDepthClamp(t *testing.T) {
	s := newImpactStore(t)

	// Depth over the cap clamps to DepthCap; seed never appears in a bucket.
	resp, err := Compute(s, "pkg.S", "", Options{Depth: 99})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got := resp["depth"].(int); got != DepthCap {
		t.Errorf("depth = %d; want clamp to %d", got, DepthCap)
	}
	for _, id := range bucketIDs(t, resp, "callers") {
		if id == nid("S") {
			t.Error("seed leaked into callers bucket")
		}
	}

	// Depth below 1 clamps up to 1: only direct callers (C1, MB, NC), not C2.
	resp1, err := Compute(s, "pkg.S", "", Options{Depth: 0})
	if err != nil {
		t.Fatalf("Compute d0: %v", err)
	}
	if got := resp1["depth"].(int); got != 1 {
		t.Errorf("depth = %d; want clamp to 1", got)
	}
	eqIDs(t, bucketIDs(t, resp1, "callers"), "C1", "MB", "NC")
}

func TestCompute_MissingCitationWarning(t *testing.T) {
	s := newImpactStore(t)
	resp, err := Compute(s, "pkg.S", "", Options{Depth: 1})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	meta := resp["metadata"].(map[string]any)
	warns := meta["warnings"].([]map[string]any)
	var found bool
	for _, w := range warns {
		if w["node_id"].(string) == nid("NC") && w["code"] == "missing-citation" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing-citation warning for NC, got %v", warns)
	}
	// NC is still reported in the bucket (recall preserved).
	if ids := bucketIDs(t, resp, "callers"); !contains(ids, nid("NC")) {
		t.Errorf("NC dropped from callers despite warn-mode contract: %v", ids)
	}
}

func TestCompute_NotFound(t *testing.T) {
	s := newImpactStore(t)
	resp, err := Compute(s, "pkg.DoesNotExist", "", Options{Depth: 2})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if resp["not_found"] != true {
		t.Errorf("expected not_found=true, got %v", resp["not_found"])
	}
	if resp["seed_qname"] != "pkg.DoesNotExist" {
		t.Errorf("expected echoed seed_qname, got %v", resp["seed_qname"])
	}
}

func TestCompute_FileSeedMode(t *testing.T) {
	s := newImpactStore(t)
	resp, err := Compute(s, "", "pkg/c1.go", Options{Depth: 1})
	if err != nil {
		t.Fatalf("Compute file-mode: %v", err)
	}
	if resp["seed_file"] != "pkg/c1.go" {
		t.Errorf("expected seed_file echo, got %v", resp["seed_file"])
	}
	seeds, ok := resp["seeds"].([]map[string]any)
	if !ok || len(seeds) == 0 {
		t.Fatalf("expected non-empty seeds list, got %T %v", resp["seeds"], resp["seeds"])
	}
	// C2 calls C1, so reverse impact of C1 at depth 1 includes C2.
	eqIDs(t, bucketIDs(t, resp, "callers"), "C2")
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
