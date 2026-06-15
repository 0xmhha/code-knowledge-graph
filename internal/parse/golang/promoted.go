package golang

import (
	gotypes "go/types"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// promoted.go (defect C): materialises Go *promoted* methods as method nodes.
//
// When a struct T embeds a type E, T's method set includes E's methods
// (Go method promotion). The per-file parser only emits a node for the
// method where it is DECLARED (E.M), so a lookup of the promoted qname
// (T.M) — the form an agent naturally tries for `t.M()` — returns nothing.
//
// EmitPromotedMethods runs as a post-resolve pass (like EmitImplementsEdges):
// it has the loaded packages (go/types method sets, which already account
// for promotion, multi-level embedding, and overrides) plus the union of
// emitted nodes. For every in-module struct it emits a method node under the
// embedding type's qname pointing at the declaring method's source position,
// plus a `defines` edge from the embedding type — so find_symbol("T.M")
// resolves and get_subgraph(T) shows the promoted method.
//
// Bounding: a promoted method is emitted only when the DECLARING method
// already has a node in the graph (i.e. it is in-module). Methods promoted
// from stdlib / external embeds (e.g. sync.Mutex.Lock) have no node, so they
// are skipped — this keeps the graph from ballooning with third-party methods.
//
// Returns empty slices (never nil) so callers can append unconditionally.
func EmitPromotedMethods(pkgs []*packages.Package, nodes []types.Node) ([]types.Node, []types.Edge) {
	outNodes := []types.Node{}
	outEdges := []types.Edge{}
	if len(pkgs) == 0 {
		return outNodes, outEdges
	}

	// qname -> node (for the declaring method's position + the embedding
	// type's ID, and to detect a qname that already exists).
	byQname := make(map[string]types.Node, len(nodes))
	for _, n := range nodes {
		if _, dup := byQname[n.QualifiedName]; !dup {
			byQname[n.QualifiedName] = n
		}
	}

	emitted := map[string]bool{}
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*gotypes.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*gotypes.Named)
			if !ok {
				continue
			}
			if _, isStruct := named.Underlying().(*gotypes.Struct); !isStruct {
				continue
			}
			if named.Obj().Pkg() == nil {
				continue
			}
			embedType := named.Obj().Pkg().Name() + "." + named.Obj().Name()
			embedNode, haveEmbed := byQname[embedType]
			if !haveEmbed {
				continue
			}

			mset := gotypes.NewMethodSet(gotypes.NewPointer(named))
			for i := 0; i < mset.Len(); i++ {
				sel := mset.At(i)
				// A direct (non-promoted) method has a selection path of
				// length 1; promotion through an embedded field makes it >1.
				if len(sel.Index()) <= 1 {
					continue
				}
				fn, ok := sel.Obj().(*gotypes.Func)
				if !ok {
					continue
				}
				declNamed := receiverNamed(fn)
				if declNamed == nil || declNamed.Obj().Pkg() == nil {
					continue
				}
				declQname := declNamed.Obj().Pkg().Name() + "." + declNamed.Obj().Name() + "." + fn.Name()
				declNode, inModule := byQname[declQname]
				if !inModule {
					continue // stdlib / external embed — no node to point at
				}
				promotedQname := embedType + "." + fn.Name()
				if _, exists := byQname[promotedQname]; exists {
					continue // the type declares/overrides this method itself
				}
				if emitted[promotedQname] {
					continue
				}
				emitted[promotedQname] = true

				id := MakeID(promotedQname, "go", declNode.StartByte)
				outNodes = append(outNodes, types.Node{
					ID:            id,
					Type:          types.NodeMethod,
					Name:          fn.Name(),
					QualifiedName: promotedQname,
					FilePath:      declNode.FilePath,
					StartLine:     declNode.StartLine,
					EndLine:       declNode.EndLine,
					StartByte:     declNode.StartByte,
					EndByte:       declNode.EndByte,
					Language:      "go",
					Visibility:    declNode.Visibility,
					Signature:     declNode.Signature,
					DocComment:    "promoted from " + declQname,
					Confidence:    types.ConfExtracted,
				})
				outEdges = append(outEdges, types.Edge{
					Src: embedNode.ID, Dst: id, Type: types.EdgeDefines,
					Count: 1, Confidence: types.ConfExtracted,
				})
			}
		}
	}
	return outNodes, outEdges
}

// receiverNamed returns the named base type of fn's receiver (dereferencing a
// pointer receiver), or nil when fn has no receiver.
func receiverNamed(fn *gotypes.Func) *gotypes.Named {
	sig, ok := fn.Type().(*gotypes.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	t := sig.Recv().Type()
	if ptr, ok := t.(*gotypes.Pointer); ok {
		t = ptr.Elem()
	}
	named, _ := t.(*gotypes.Named)
	return named
}
