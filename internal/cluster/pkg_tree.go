// Package cluster builds two hierarchies for the CKG graph: pkg_tree
// (deterministic, derived from package paths) and topic_tree (Leiden,
// in leiden.go). Both expose a uniform Hierarchy interface.
package cluster

import (
	"sort"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Edge between parent and child in a hierarchy.
type Edge struct {
	ParentID, ChildID string
	Level             int
}

// PkgTree captures the structural-depth hierarchy derived from package paths.
// level semantics:
//   - root pkg (no slash in qname) = 0
//   - each nested subpkg adds +1
//   - file = parent_pkg.level + 1
//   - decl (type/func/var) = file.level + 1
//   - LogicBlock = function.level + 1
//
// IMPORTANT: this is structural depth, NOT the spec §5.4 L0..L4 LOD band.
// Deep monorepos (a/b/c/d package nesting) produce levels >4 (unbounded above:
// depth-N package nesting yields file at level N+1, function at level N+2, etc.).
// The viewer's LOD wiring (T23) must derive the LOD band from node.Type
// independently rather than reading PkgTree.level directly.
type PkgTree struct {
	parent map[string]string
	level  map[string]int
	Edges  []Edge
}

// BuildPkgTree derives the tree from node attributes (Type + FilePath).
// Logic block nodes inherit their function's level + 1.
func BuildPkgTree(g *graph.Graph) *PkgTree {
	t := &PkgTree{parent: map[string]string{}, level: map[string]int{}}
	pkgIDByName := map[string]string{}
	fileIDByPath := map[string]string{}
	funcIDByQname := map[string]string{}
	for _, n := range g.Nodes {
		switch n.Type {
		case types.NodePackage:
			pkgIDByName[n.QualifiedName] = n.ID
		case types.NodeFile:
			fileIDByPath[n.FilePath] = n.ID
		case types.NodeFunction, types.NodeMethod, types.NodeConstructor:
			funcIDByQname[n.QualifiedName] = n.ID
		}
	}
	// Determine package levels by depth of qualified name slash-segments.
	for q, id := range pkgIDByName {
		t.level[id] = strings.Count(q, "/")
	}
	// Files: level = (parent pkg level) + 1, default 2 if no parent matched.
	for _, n := range g.Nodes {
		if n.Type != types.NodeFile {
			continue
		}
		pid := nearestPkgIDForFile(n.FilePath, pkgIDByName)
		if pid != "" {
			t.parent[n.ID] = pid
			t.level[n.ID] = t.level[pid] + 1
			t.Edges = append(t.Edges, Edge{ParentID: pid, ChildID: n.ID, Level: t.level[n.ID]})
		} else {
			t.level[n.ID] = 2
		}
	}
	// type/func/var: parent = file by FilePath; level = file.level + 1.
	for _, n := range g.Nodes {
		switch n.Type {
		case types.NodeStruct, types.NodeInterface, types.NodeClass, types.NodeTypeAlias,
			types.NodeEnum, types.NodeContract, types.NodeMapping, types.NodeEvent,
			types.NodeFunction, types.NodeMethod, types.NodeConstructor,
			types.NodeConstant, types.NodeVariable, types.NodeField,
			types.NodeImport, types.NodeExport, types.NodeDecorator,
			types.NodeGoroutine, types.NodeChannel,
			types.NodeParameter, types.NodeLocalVariable:
			fid, ok := fileIDByPath[n.FilePath]
			if !ok {
				continue
			}
			t.parent[n.ID] = fid
			t.level[n.ID] = t.level[fid] + 1
			t.Edges = append(t.Edges, Edge{ParentID: fid, ChildID: n.ID, Level: t.level[n.ID]})
		}
	}
	// Logic blocks (L4): parent = enclosing function (qname prefix before '#').
	for _, n := range g.Nodes {
		switch n.Type {
		case types.NodeIfStmt, types.NodeLoopStmt, types.NodeSwitchStmt,
			types.NodeReturnStmt, types.NodeCallSite:
			i := strings.IndexByte(n.QualifiedName, '#')
			if i < 0 {
				continue
			}
			fnQ := n.QualifiedName[:i]
			fid, ok := funcIDByQname[fnQ]
			if !ok {
				continue
			}
			t.parent[n.ID] = fid
			t.level[n.ID] = t.level[fid] + 1
			t.Edges = append(t.Edges, Edge{ParentID: fid, ChildID: n.ID, Level: t.level[n.ID]})
		}
	}
	// stable sort for deterministic output
	sort.Slice(t.Edges, func(i, j int) bool {
		if t.Edges[i].ParentID != t.Edges[j].ParentID {
			return t.Edges[i].ParentID < t.Edges[j].ParentID
		}
		return t.Edges[i].ChildID < t.Edges[j].ChildID
	})
	return t
}

func (t *PkgTree) Parent(id string) (string, bool) {
	p, ok := t.parent[id]
	return p, ok
}
func (t *PkgTree) LevelOf(id string) int { return t.level[id] }

// nearestPkgIDForFile returns the longest matching package qname that prefixes
// the file path's directory portion.
func nearestPkgIDForFile(filePath string, pkgs map[string]string) string {
	dir := filePath
	if idx := strings.LastIndex(dir, "/"); idx >= 0 {
		dir = dir[:idx]
	}
	best := ""
	for q := range pkgs {
		if (dir == q || strings.HasPrefix(dir, q+"/")) && len(q) > len(best) {
			best = q
		}
	}
	return pkgs[best]
}
