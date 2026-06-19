package golang_test

import (
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestCanonicalID_DistinguishesSameNameAcrossPackages guards Phase 1 of the
// symbol-identity design: the same method name in two different packages must
// get distinct, import-path-qualified canonical ids, even though their short
// qualified_name leaves (coll1.Set.Size / coll2.Other.Size) only differ by the
// leaf package — and leaf packages themselves collide on real codebases.
func TestCanonicalID_DistinguishesSameNameAcrossPackages(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var c1, c2 string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.Set.Size":
			c1 = n.CanonicalID
		case "coll2.Other.Size":
			c2 = n.CanonicalID
		}
	}
	if c1 == "" || c2 == "" {
		t.Fatalf("missing canonical ids: coll1.Set.Size=%q coll2.Other.Size=%q", c1, c2)
	}
	if c1 == c2 {
		t.Errorf("same-name methods in different packages share canonical id %q", c1)
	}
	if want := "ckgresolve.test/coll1.(*Set).Size"; c1 != want {
		t.Errorf("coll1.Set.Size canonical id = %q, want %q", c1, want)
	}
}

// TestCanonicalID_AllGoNodeKinds guards symbol-identity Phase 1: canonical_id is
// now emitted for types/structs/interfaces, fields, package-level const/var, and
// interface methods — not just funcs/methods. Each id is import-path-qualified,
// and an interface method's id is distinct from a same-named concrete method's.
func TestCanonicalID_AllGoNodeKinds(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	cidByQname := map[string]string{}
	for _, n := range g.Nodes {
		cidByQname[n.QualifiedName] = n.CanonicalID
	}
	cases := map[string]string{
		"coll1.Set":         "ckgresolve.test/coll1.Set",         // struct type
		"coll1.Set.n":       "ckgresolve.test/coll1.Set.n",       // struct field
		"coll1.Box.Val":     "ckgresolve.test/coll1.Box.Val",     // exported field
		"coll1.Hasher":      "ckgresolve.test/coll1.Hasher",      // interface type
		"coll1.Hasher.Hash": "ckgresolve.test/coll1.Hasher.Hash", // interface method
		"coll1.MaxItems":    "ckgresolve.test/coll1.MaxItems",    // package const
		"coll1.defaultName": "ckgresolve.test/coll1.defaultName", // package var
	}
	for qname, want := range cases {
		got, ok := cidByQname[qname]
		if !ok {
			t.Errorf("node %q not found", qname)
			continue
		}
		if got != want {
			t.Errorf("%s canonical id = %q, want %q", qname, got, want)
		}
	}
	// interface method vs same-named concrete method must differ.
	if iface, concrete := cidByQname["coll1.Hasher.Hash"], cidByQname["coll1.Thing.Hash"]; iface == concrete || concrete == "" {
		t.Errorf("interface vs concrete Hash share/empty canonical id: iface=%q concrete=%q", iface, concrete)
	}
	if want := "ckgresolve.test/coll1.(Thing).Hash"; cidByQname["coll1.Thing.Hash"] != want {
		t.Errorf("coll1.Thing.Hash canonical id = %q, want %q", cidByQname["coll1.Thing.Hash"], want)
	}
}

// TestCanonicalID_BlankIdentifierSkipped guards B1: the blank identifier `_`
// (package-level `var _ = …`, struct padding fields) must NOT receive a
// canonical id — `<pkg>._` is intentionally non-unique and pollutes uniqueness.
func TestCanonicalID_BlankIdentifierSkipped(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	for _, n := range g.Nodes {
		if n.Name == "_" && n.CanonicalID != "" {
			t.Errorf("blank identifier got canonical id %q (type=%s qname=%s) — want empty",
				n.CanonicalID, n.Type, n.QualifiedName)
		}
	}
	// sanity: the non-blank field X on Padded still gets a canonical id.
	var xc string
	for _, n := range g.Nodes {
		if n.QualifiedName == "coll1.Padded.X" {
			xc = n.CanonicalID
		}
	}
	if xc != "ckgresolve.test/coll1.Padded.X" {
		t.Errorf("coll1.Padded.X canonical id = %q, want ckgresolve.test/coll1.Padded.X", xc)
	}
}

// TestCanonicalID_LocalVarSkipped guards B2: a function-local `var` declaration
// must NOT receive a canonical id (only package-level const/var do), since
// <pkg>.<localName> collides across the many functions that reuse a name.
func TestCanonicalID_LocalVarSkipped(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	sawLocal := false
	for _, n := range g.Nodes {
		if n.Name == "localOnly" {
			sawLocal = true
			if n.CanonicalID != "" {
				t.Errorf("function-local var localOnly got canonical id %q, want empty", n.CanonicalID)
			}
		}
		// package-level var must still carry one.
		if n.QualifiedName == "coll1.defaultName" && n.CanonicalID != "ckgresolve.test/coll1.defaultName" {
			t.Errorf("package-level defaultName canonical id = %q, want ckgresolve.test/coll1.defaultName", n.CanonicalID)
		}
	}
	if !sawLocal {
		t.Fatal("local var node localOnly not found in graph")
	}
}

// TestResolveSameNameMethodPrefersReceiverType guards the name-collision fix:
// coll1.Set.Quorum calls its own receiver's Size(), while coll2.Other.Size is a
// same-named decoy in another package. The typed resolver must bind the call to
// coll1.Set.Size and never to coll2.Other.Size (the V0 bare-name resolver bound
// such calls to whichever same-named node was indexed last).
func TestResolveSameNameMethodPrefersReceiverType(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, wantDst, decoyDst string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.Set.Quorum":
			srcID = n.ID
		case "coll1.Set.Size":
			wantDst = n.ID
		case "coll2.Other.Size":
			decoyDst = n.ID
		}
	}
	if srcID == "" || wantDst == "" || decoyDst == "" {
		t.Fatalf("missing nodes: src=%q want=%q decoy=%q", srcID, wantDst, decoyDst)
	}
	var toWant, toDecoy bool
	for _, e := range g.Edges {
		if e.Src != srcID || (e.Type != types.EdgeCalls && e.Type != types.EdgeInvokes) {
			continue
		}
		switch e.Dst {
		case wantDst:
			toWant = true
		case decoyDst:
			toDecoy = true
		}
	}
	if !toWant {
		t.Errorf("expected coll1.Set.Quorum -calls-> coll1.Set.Size (same receiver type)")
	}
	if toDecoy {
		t.Errorf("coll1.Set.Quorum must NOT bind to coll2.Other.Size (bare-name collision)")
	}
}

// TestResolveInterfaceMethodNotBareName guards defect-A fix #1: an interface
// dispatch call (h.Hash() where h is the Hasher interface) must bind to
// coll1.Hasher.Hash, never to the same-named decoy coll1.Thing.Hash. The bare
// "interface_method" path used to keep the bare callee name, which the V0
// resolver bound to whichever ".Hash" node was indexed last.
func TestResolveInterfaceMethodNotBareName(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, wantDst, decoyDst string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.UseHasher":
			srcID = n.ID
		case "coll1.Hasher.Hash":
			wantDst = n.ID
		case "coll1.Thing.Hash":
			decoyDst = n.ID
		}
	}
	if srcID == "" || wantDst == "" || decoyDst == "" {
		t.Fatalf("missing nodes: src=%q want=%q decoy=%q", srcID, wantDst, decoyDst)
	}
	var toWant, toDecoy bool
	for _, e := range g.Edges {
		if e.Src != srcID || (e.Type != types.EdgeCalls && e.Type != types.EdgeInvokes) {
			continue
		}
		switch e.Dst {
		case wantDst:
			toWant = true
		case decoyDst:
			toDecoy = true
		}
	}
	if !toWant {
		t.Errorf("expected coll1.UseHasher -invokes-> coll1.Hasher.Hash (interface method)")
	}
	if toDecoy {
		t.Errorf("coll1.UseHasher must NOT bind to coll1.Thing.Hash (bare-name collision)")
	}
}

// TestResolveBuiltinEmitsNoCallEdge guards defect-A fix #2: a builtin call
// (len(xs)) must not produce a call edge to a same-named method node
// (coll1.counter.len). Builtins have no graph node, so guessing one is a
// false edge — the kind of cross-subsystem noise that pollutes find_callees.
func TestResolveBuiltinEmitsNoCallEdge(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, builtinDecoy string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.CountBuiltin":
			srcID = n.ID
		case "coll1.counter.len":
			builtinDecoy = n.ID
		}
	}
	if srcID == "" || builtinDecoy == "" {
		t.Fatalf("missing nodes: src=%q decoy=%q", srcID, builtinDecoy)
	}
	for _, e := range g.Edges {
		if e.Src == srcID && e.Dst == builtinDecoy &&
			(e.Type == types.EdgeCalls || e.Type == types.EdgeInvokes) {
			t.Errorf("builtin len() must NOT bind to coll1.counter.len")
		}
	}
}

// TestPromotedMethodNode guards defect-C: a type that embeds another in-module
// type promotes its methods, so coll1.Derived must carry a Derived.Ping method
// node pointing at Base.Ping's implementation — find_symbol("Derived.Ping")
// would otherwise miss it (Go method promotion isn't a declared node).
func TestPromotedMethodNode(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var promoted, base *types.Node
	for i := range g.Nodes {
		switch g.Nodes[i].QualifiedName {
		case "coll1.Derived.Ping":
			promoted = &g.Nodes[i]
		case "coll1.Base.Ping":
			base = &g.Nodes[i]
		}
	}
	if base == nil {
		t.Fatal("missing coll1.Base.Ping (declaring method)")
	}
	if promoted == nil {
		t.Fatal("missing promoted node coll1.Derived.Ping")
	}
	if promoted.Type != types.NodeMethod {
		t.Errorf("promoted node type = %v, want Method", promoted.Type)
	}
	// The promoted node points at the real implementation (Base.Ping's site).
	if promoted.FilePath != base.FilePath || promoted.StartLine != base.StartLine {
		t.Errorf("promoted node should point at Base.Ping (%s:%d), got %s:%d",
			base.FilePath, base.StartLine, promoted.FilePath, promoted.StartLine)
	}
	// A defines edge links the embedding type to the promoted method.
	var derivedID, promotedID string
	for _, n := range g.Nodes {
		if n.QualifiedName == "coll1.Derived" {
			derivedID = n.ID
		}
	}
	promotedID = promoted.ID
	var linked bool
	for _, e := range g.Edges {
		if e.Type == types.EdgeDefines && e.Src == derivedID && e.Dst == promotedID {
			linked = true
		}
	}
	if !linked {
		t.Errorf("expected coll1.Derived -defines-> coll1.Derived.Ping")
	}
}

// TestFieldWriteEdge guards defect-E: a Go assignment `x.F = v` must emit a
// writes_field edge from the enclosing function to the field node, so an agent
// can find who mutates a field (e.g. who fills Receipt.EffectiveGasPrice). A
// read-only accessor must NOT emit writes_field.
func TestFieldWriteEdge(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var setID, getID, boxValID string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.setBox":
			setID = n.ID
		case "coll1.getBox":
			getID = n.ID
		case "coll1.Box.Val":
			boxValID = n.ID
		}
	}
	if setID == "" || getID == "" || boxValID == "" {
		t.Fatalf("missing nodes: set=%q get=%q boxVal=%q", setID, getID, boxValID)
	}
	var writeFromSetter, writeFromGetter bool
	for _, e := range g.Edges {
		if e.Type != types.EdgeWritesField || e.Dst != boxValID {
			continue
		}
		switch e.Src {
		case setID:
			writeFromSetter = true
		case getID:
			writeFromGetter = true
		}
	}
	if !writeFromSetter {
		t.Errorf("expected coll1.setBox -writes_field-> coll1.Box.Val")
	}
	if writeFromGetter {
		t.Errorf("coll1.getBox only reads Box.Val; must not emit writes_field")
	}
}

func TestResolveCrossFileCall(t *testing.T) {
	root := "testdata/resolve"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, dstID string
	for _, n := range g.Nodes {
		if n.QualifiedName == "b.Hello" {
			srcID = n.ID
		}
		if n.QualifiedName == "a.Greet" {
			dstID = n.ID
		}
	}
	if srcID == "" || dstID == "" {
		t.Fatalf("missing nodes: srcID=%q dstID=%q", srcID, dstID)
	}
	found := false
	for _, e := range g.Edges {
		if e.Type == types.EdgeCalls && e.Src == srcID && e.Dst == dstID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected edge b.Hello -calls-> a.Greet")
	}
}
