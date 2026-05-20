package proto

import (
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// languageTag is the value stamped into Node.Language for every proto node.
// Distinct from "go"/"ts"/"sol" so downstream tools (viewer, validate) can
// scope queries to .proto-derived nodes without inspecting file paths.
const languageTag = "proto"

// Parser implements parse.Parser for `.proto` files. Stateless across files —
// safe for concurrent ParseFile dispatch from buildpipe.
//
// V0 produces a flat node set (Service, Method, MessageType, Field, Enum,
// Package, File) with `defines` + `contains` parent/child wiring. Cross-file
// resolution (`uses_type` from rpc Request/Response → MessageType nodes in
// other proto files) happens in Resolve.
type Parser struct {
	srcRoot string
}

// New returns a Parser rooted at srcRoot (used for relative file paths so
// Node.FilePath is consistent with the other language parsers).
func New(srcRoot string) *Parser { return &Parser{srcRoot: srcRoot} }

// Extensions reports the file extensions this parser handles.
func (p *Parser) Extensions() []string { return []string{".proto"} }

// ParseFile runs Pass 1 over a single `.proto` file.
//
// Recovery strategy: malformed declarations are skipped at the top-level
// recovery boundary (skipToTopLevel). The parser never returns an error for
// in-file syntax — it falls through to the next top-level keyword. This
// matches the "best-effort INFERRED" handling spec §6.4 calls for, and means
// a single bad proto file in a monorepo doesn't break the build.
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	v := newVisitor(rel, src)
	v.parse()

	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// Resolve unions per-file results and wires `uses_type` edges from rpc
// methods to MessageType nodes whose qualified_name matches the pending
// target. Same-file matches stay EXTRACTED; cross-file matches are tagged
// INFERRED (mirrors the Solidity / TypeScript resolvers).
//
// proto3 type names inside a service block can be:
//   - bare ("EchoRequest"): resolves against the file's own package first,
//     then any other proto's package.
//   - fully-qualified (".pkg.sub.EchoRequest" or "pkg.sub.EchoRequest"):
//     resolves by exact qname match.
//
// We try the package-prefixed form first, then bare-name fallback. Multiple
// candidates are kept ordered (deterministic — sorted by ID) and the first
// wins. Ambiguity beyond that is tagged AMBIGUOUS so audits can surface it.
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}

	// Index MessageType nodes by qualified_name AND by bare name (last
	// dotted segment) so both lookup paths are O(1).
	nodeFile := map[string]string{}
	byQName := map[string][]string{}
	byBareName := map[string][]string{}

	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			nodeFile[n.ID] = n.FilePath
			if n.Type != types.NodeMessageType {
				continue
			}
			byQName[n.QualifiedName] = append(byQName[n.QualifiedName], n.ID)
			// Bare name = last path component of `proto:pkg.sub.Name`.
			bare := bareNameFromQName(n.QualifiedName)
			if bare != "" {
				byBareName[bare] = append(byBareName[bare], n.ID)
			}
		}
	}

	for _, r := range results {
		for _, pr := range r.Pending {
			if pr.EdgeType != types.EdgeUsesType {
				continue
			}
			ids := byQName[pr.TargetQName]
			if len(ids) == 0 {
				// Fallback: extract the trailing bare name and retry against
				// byBareName. Captures cross-namespace refs like
				// `rpc X(Outer.Inner) returns (...)` where candidateForType
				// produces `proto:Outer.Inner` but the actual nested message
				// node carries `proto:pkg.Outer.Inner`. Reviewer (W3a
				// Important #1) caught the desync — without this, nested type
				// references resolve only when the source already spells out
				// the full package-qualified path. byBareName's key is the
				// trailing dotted segment so this single lookup catches both
				// (a) bare unqualified refs (`MessageName`) and (b) partial
				// path refs (`Outer.Inner`, `Outer.Mid.Inner`) — both narrow
				// to the same trailing segment ("Inner") and resolve.
				bare := bareNameFromQName(pr.TargetQName)
				if bare != "" {
					ids = byBareName[bare]
				}
			}
			if len(ids) == 0 {
				continue
			}
			conf := types.ConfExtracted
			if nodeFile[pr.SrcID] != "" && nodeFile[ids[0]] != "" && nodeFile[pr.SrcID] != nodeFile[ids[0]] {
				conf = types.ConfInferred
			}
			if len(ids) > 1 {
				conf = types.ConfAmbiguous
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: pr.SrcID, Dst: ids[0], Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
	}
	return out, nil
}

// bareNameFromQName extracts the trailing identifier from a `proto:pkg.Name`
// style qualified name. Returns "" if the input is malformed (no colon).
func bareNameFromQName(q string) string {
	idx := strings.Index(q, ":")
	if idx < 0 {
		return ""
	}
	tail := q[idx+1:]
	if dot := strings.LastIndex(tail, "."); dot >= 0 {
		return tail[dot+1:]
	}
	return tail
}

// Compile-time check that *Parser satisfies parse.Parser.
var _ parse.Parser = (*Parser)(nil)
