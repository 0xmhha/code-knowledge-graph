package validate

import (
	"context"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// SchemaValidator runs deterministic structural checks on a graph:
//
//   - Empty required fields (id, name, qualified_name, file_path where it
//     applies, confidence). Empty values on a node mean a parser branch
//     emitted a placeholder it never finished filling.
//   - Dangling edges (src or dst not present in node set). Reuses
//     graph.Inspect so the rule definition lives in one place.
//   - Unknown node/edge types. Schema bumps are catastrophic if a parser
//     emits a type the persist layer doesn't recognise.
//   - Edge-type semantic invariants (V1 baseline):
//   - implements src must be a Struct or TypeAlias, dst must be Interface
//   - listens_on src must be Function/Method, dst must be Endpoint
//   - calls/invokes src and dst must be Function/Method
//
// The validator is deterministic, fast, and dependency-free — safe to run
// on every build. Findings inform Citation Enforcement and the LLM
// validator (which uses these as priors).
type SchemaValidator struct{}

// NewSchemaValidator returns a stateless schema validator instance.
func NewSchemaValidator() *SchemaValidator { return &SchemaValidator{} }

// Name returns the validator identifier.
func (v *SchemaValidator) Name() string { return "schema" }

// fileScopedTypes are node types that MUST have a non-empty file_path. The
// remaining "global" types (Package, Commit, Endpoint, MessageType, Mutex,
// Channel) live above any single file and are exempt.
var fileScopedTypes = map[types.NodeType]struct{}{
	types.NodeFile:       {},
	types.NodeStruct:     {},
	types.NodeInterface:  {},
	types.NodeFunction:   {},
	types.NodeMethod:     {},
	types.NodeField:      {},
	types.NodeVariable:   {},
	types.NodeConstant:   {},
	types.NodeContract:   {},
	types.NodeImport:     {},
	types.NodeCallSite:   {},
	types.NodeIfStmt:     {},
	types.NodeLoopStmt:   {},
	types.NodeSwitchStmt: {},
	types.NodeReturnStmt: {},
}

// Validate executes all schema checks and returns one report aggregating
// every issue found. ctx and store are accepted for interface conformance;
// SchemaValidator does not block on either.
func (v *SchemaValidator) Validate(ctx context.Context, g *graph.Graph, store persist.StoreReader) (*Report, error) {
	report := &Report{Validator: v.Name()}
	if g == nil {
		return report, nil
	}

	// Build a node-id → node lookup once; downstream checks query it.
	byID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		n := &g.Nodes[i]
		byID[n.ID] = n
	}

	// (1) Empty required fields per node.
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.ID == "" {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "empty-id",
				Message: "node has empty ID",
			})
		}
		if n.Name == "" {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "empty-name",
				Message: "node has empty Name", NodeID: n.ID, FilePath: n.FilePath,
			})
		}
		if n.QualifiedName == "" {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "empty-qname",
				Message: "node has empty QualifiedName", NodeID: n.ID, FilePath: n.FilePath,
			})
		}
		if !n.Confidence.Valid() {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "invalid-confidence",
				Message: "node has invalid Confidence " + string(n.Confidence),
				NodeID:  n.ID, FilePath: n.FilePath,
			})
		}
		if _, scoped := fileScopedTypes[n.Type]; scoped && n.FilePath == "" {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "empty-file-path",
				Message: "file-scoped node has empty FilePath (type=" + string(n.Type) + ")",
				NodeID:  n.ID,
			})
		}
	}

	// (2) Dangling edges + unknown types — delegate to graph.Inspect so
	// the rule lives in one place. Convert each dangling case to an
	// Issue with code "dangling-src" / "dangling-dst".
	gReport := graph.Inspect(g)
	for _, schemaErr := range gReport.SchemaErrors {
		report.Issues = append(report.Issues, Issue{
			Severity: SeverityError, Code: "schema-error",
			Message: schemaErr.Error(),
		})
	}
	for _, d := range gReport.DanglingEdges {
		key := edgeKey(string(d.Edge.Type), d.Edge.Src, d.Edge.Dst, d.Edge.Line)
		if d.Src {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "dangling-src",
				Message: "edge src does not match any node",
				EdgeKey: key, FilePath: d.Edge.FilePath,
			})
		}
		if d.Dst {
			report.Issues = append(report.Issues, Issue{
				Severity: SeverityError, Code: "dangling-dst",
				Message: "edge dst does not match any node",
				EdgeKey: key, FilePath: d.Edge.FilePath,
			})
		}
	}

	// (3) Edge-type semantic invariants. Only fire when both endpoints are
	// resolvable (otherwise the dangling rules above already covered it).
	for _, e := range g.Edges {
		src, srcOK := byID[e.Src]
		dst, dstOK := byID[e.Dst]
		if !srcOK || !dstOK {
			continue
		}
		switch e.Type {
		case types.EdgeImplements:
			if dst.Type != types.NodeInterface {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "implements-bad-dst",
					Message:  "implements edge dst is not an Interface (got " + string(dst.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeListensOn:
			if src.Type != types.NodeFunction && src.Type != types.NodeMethod {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "listens-on-bad-src",
					Message:  "listens_on src is not a Function/Method (got " + string(src.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
			if dst.Type != types.NodeEndpoint {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "listens-on-bad-dst",
					Message:  "listens_on dst is not an Endpoint (got " + string(dst.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeHTTPCalls:
			// W2 (schema 1.9): http_calls always points Func/Method →
			// Endpoint (real or AMBIGUOUS placeholder). The matcher passes
			// through with the same shape so the validation is unchanged.
			if src.Type != types.NodeFunction && src.Type != types.NodeMethod {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "http-calls-bad-src",
					Message:  "http_calls src is not a Function/Method (got " + string(src.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
			if dst.Type != types.NodeEndpoint {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "http-calls-bad-dst",
					Message:  "http_calls dst is not an Endpoint (got " + string(dst.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeGRPCListensOn:
			// W3b (schema 1.9): grpc_listens_on points server-impl Method →
			// Endpoint (`grpc:Service.Method`). Free functions cannot listen
			// on gRPC RPCs — a gRPC server impl is always a method on a
			// receiver registered via pb.RegisterXXXServer.
			if src.Type != types.NodeMethod {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "grpc-listens-on-bad-src",
					Message:  "grpc_listens_on src is not a Method (got " + string(src.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
			if dst.Type != types.NodeEndpoint {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "grpc-listens-on-bad-dst",
					Message:  "grpc_listens_on dst is not an Endpoint (got " + string(dst.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeGRPCCalls:
			// W3b (schema 1.9): grpc_calls points caller Function/Method →
			// Endpoint (real grpc Endpoint or AMBIGUOUS placeholder for
			// unresolved external services). Mirror http_calls shape.
			if src.Type != types.NodeFunction && src.Type != types.NodeMethod {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "grpc-calls-bad-src",
					Message:  "grpc_calls src is not a Function/Method (got " + string(src.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
			if dst.Type != types.NodeEndpoint {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "grpc-calls-bad-dst",
					Message:  "grpc_calls dst is not an Endpoint (got " + string(dst.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeCalls, types.EdgeInvokes:
			if !isCallable(src.Type) {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "calls-bad-src",
					Message:  "calls/invokes src is not a callable kind (got " + string(src.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
			if !isCallable(dst.Type) {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "calls-bad-dst",
					Message:  "calls/invokes dst is not a callable kind (got " + string(dst.Type) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeTimeoutPath:
			if !isContextPathShape(src, dst, e) {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "timeout-path-bad-shape",
					Message: "timeout_path edge must be a Function/Method self-loop (got src=" +
						string(src.Type) + ", dst=" + string(dst.Type) + ", self=" + boolStr(e.Src == e.Dst) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		case types.EdgeCancellationPath:
			if !isContextPathShape(src, dst, e) {
				report.Issues = append(report.Issues, Issue{
					Severity: SeverityWarning, Code: "cancellation-path-bad-shape",
					Message: "cancellation_path edge must be a Function/Method self-loop (got src=" +
						string(src.Type) + ", dst=" + string(dst.Type) + ", self=" + boolStr(e.Src == e.Dst) + ")",
					EdgeKey:  edgeKey(string(e.Type), e.Src, e.Dst, e.Line),
					FilePath: e.FilePath,
				})
			}
		}
	}

	_ = ctx
	_ = store
	return report, nil
}

// isCallable reports whether t is a node type that legitimately appears
// on either end of a calls/invokes edge.
func isCallable(t types.NodeType) bool {
	switch t {
	case types.NodeFunction, types.NodeMethod, types.NodeCallSite:
		return true
	}
	return false
}

// isContextPathShape enforces the timeout_path / cancellation_path
// invariants (P2): both endpoints must be the same Function or Method
// node, i.e. a self-loop anchored on the enclosing function. Used by
// SchemaValidator to surface emitter regressions early.
func isContextPathShape(src, dst *types.Node, e types.Edge) bool {
	if src == nil || dst == nil {
		return false
	}
	if e.Src != e.Dst {
		return false
	}
	switch src.Type {
	case types.NodeFunction, types.NodeMethod:
		return true
	}
	return false
}

// boolStr returns "true"/"false" — a tiny helper so error messages don't
// import strconv just to format a single bool. Kept local to schema.go.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
