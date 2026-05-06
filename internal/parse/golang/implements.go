package golang

import (
	gotypes "go/types"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// implements.go (P0): emits implements / extends edges by walking each loaded
// package's top-level type names and asking go/types whether each concrete
// type satisfies each interface.
//
// Why a separate post-pass and not inline with declarations.go:
//   - Per-file Pass 1 only sees one file's AST. Cross-package satisfaction
//     (the common case — sqliteStore implements StoreReader where the
//     interface lives in another file or package) requires the *union* of
//     types across the whole module.
//   - go/types' Implements/AssignableTo only work on resolved *types.Named
//     values, which are only available after packages.Load completes.
//
// Edge ID resolution: we don't recompute Struct/Interface IDs via MakeID —
// the qname → ID map is built from rg.Nodes (the union of every per-file
// emit). This sidesteps the "byte offset must match the visitor's pos" hazard
// (cf. distributed.go's idForFunc fix) and stays correct under future
// declarations.go refactors.

// EmitImplementsEdges scans every loaded package's top-level type names,
// partitions them into interfaces vs concrete types, and emits implements
// edges (concrete → interface) for every satisfaction pair, plus extends
// edges (interface → interface) for every embedded interface relationship.
//
// Receiver shape: types.Implements is checked on BOTH the named type T
// and the pointer type *T so both value-receiver and pointer-receiver
// method sets count toward satisfaction (the standard Go semantics).
//
// Self-edges and "every type implements interface{}" noise are excluded.
// Cross-package satisfaction works naturally because pkg.Types.Scope()
// exposes the full package public surface; the emitted edge IDs come
// from a qname → ID map built over the supplied nodes (the union of all
// per-file Struct/Interface/TypeAlias/Enum nodes), so satisfaction across
// files in different packages still resolves correctly.
//
// Returns an empty slice (never nil) when pkgs is empty or the loaded
// packages contain no Types information — callers can append unconditionally.
func EmitImplementsEdges(pkgs []*packages.Package, nodes []types.Node) []types.Edge {
	if len(pkgs) == 0 {
		return []types.Edge{}
	}

	// Build qname → emitted node ID map. Only kinds that can legitimately
	// appear as src/dst of implements/extends are indexed:
	//   - Struct, TypeAlias, Enum: valid implements src
	//   - Interface: valid implements dst AND extends src/dst
	qnameToID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case types.NodeStruct, types.NodeInterface, types.NodeTypeAlias, types.NodeEnum:
			if _, exists := qnameToID[n.QualifiedName]; !exists {
				qnameToID[n.QualifiedName] = n.ID
			}
		}
	}

	// Collect every named type from every loaded package's top-level scope.
	// Dedupe by qname — the same package can appear twice (base + test
	// variant) under packages.Load(Tests:true).
	type typedNamed struct {
		qname string
		named *gotypes.Named
		iface *gotypes.Interface // non-nil iff this is an interface
	}
	seen := map[string]struct{}{}
	var concretes, ifaces []typedNamed
	for _, pkg := range pkgs {
		if pkg == nil || pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		if scope == nil {
			continue
		}
		pkgName := pkg.Types.Name()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*gotypes.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*gotypes.Named)
			if !ok {
				continue
			}
			qname := pkgName + "." + tn.Name()
			// Dedup key uses the full import path so two packages declaring
			// the same simple name (e.g. internal/parse/golang and
			// internal/parse/typescript both named "parser") don't collide
			// and silently drop one package's types. The stored qname keeps
			// the short form because that's what visitFuncDecl/emitTypeSpec
			// produce when emitting node IDs — downstream qnameToID lookup
			// must match those.
			seenKey := pkg.Types.Path() + "." + tn.Name()
			if _, dup := seen[seenKey]; dup {
				continue
			}
			seen[seenKey] = struct{}{}

			if iface, ok := named.Underlying().(*gotypes.Interface); ok {
				ifaces = append(ifaces, typedNamed{qname: qname, named: named, iface: iface})
			} else {
				concretes = append(concretes, typedNamed{qname: qname, named: named})
			}
		}
	}

	// Capacity hint: max-bound estimate (every concrete satisfies every
	// interface). Undershooting causes append re-grows on dense graphs.
	maxIfaces := len(ifaces)
	if maxIfaces == 0 {
		maxIfaces = 1
	}
	out := make([]types.Edge, 0, len(concretes)*maxIfaces)

	// Pair (concrete, interface) → emit implements when satisfied.
	for _, iface := range ifaces {
		// Skip the empty interface — every type satisfies it; emitting would
		// produce O(N) low-signal edges per concrete type.
		if iface.iface.Empty() {
			continue
		}
		ifaceID, hasIface := qnameToID[iface.qname]
		if !hasIface {
			continue
		}
		for _, impl := range concretes {
			if impl.qname == iface.qname {
				continue // self-edge guard (cannot trigger here since
				// concretes ∩ ifaces = ∅, but kept for symmetry with the
				// iface-vs-iface loop below)
			}
			implID, hasImpl := qnameToID[impl.qname]
			if !hasImpl {
				continue
			}
			// Check both T and *T — value-receiver methods belong to both
			// method sets, pointer-receiver methods only to *T's. Standard
			// Go interface satisfaction semantics.
			//
			// Generics caveat: uninstantiated *gotypes.Named with TypeParams()
			// != nil return false from Implements — go/types only resolves
			// satisfaction once type arguments are known. V0 doesn't track
			// instantiation sites, so generic-implementations of interfaces
			// are intentionally skipped here.
			if gotypes.Implements(impl.named, iface.iface) ||
				gotypes.Implements(gotypes.NewPointer(impl.named), iface.iface) {
				out = append(out, types.Edge{
					Src: implID, Dst: ifaceID,
					Type:       types.EdgeImplements,
					Count:      1,
					Confidence: types.ConfExtracted,
				})
			}
		}
	}

	// Pair (ifaceA, ifaceB) → emit extends when ifaceA *embeds* ifaceB.
	// We use NumEmbeddeds + EmbeddedType so we get the literal embed
	// relationship, not transitive "ifaceA's method set ⊇ ifaceB's"
	// (which would also be true for interface satisfaction but is not
	// what extends models).
	for _, a := range ifaces {
		for i := 0; i < a.iface.NumEmbeddeds(); i++ {
			emb := a.iface.EmbeddedType(i)
			embNamed, ok := emb.(*gotypes.Named)
			if !ok {
				continue
			}
			embObj := embNamed.Obj()
			if embObj == nil || embObj.Pkg() == nil {
				continue // builtin (e.g. comparable) — no node to point to
			}
			embQname := embObj.Pkg().Name() + "." + embObj.Name()
			if embQname == a.qname {
				continue // self-edge guard
			}
			srcID, srcOK := qnameToID[a.qname]
			dstID, dstOK := qnameToID[embQname]
			if !srcOK || !dstOK {
				continue
			}
			out = append(out, types.Edge{
				Src: srcID, Dst: dstID,
				Type:       types.EdgeExtends,
				Count:      1,
				Confidence: types.ConfExtracted,
			})
		}
	}

	return out
}
