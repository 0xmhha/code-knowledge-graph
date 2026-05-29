package persist

import (
	"database/sql"
	"fmt"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// nodeColumns is the explicit column list used by every SELECT that feeds
// scanNodes. Keeping it in one place avoids SELECT * surprises if the
// schema gains a column later.
const nodeColumns = `id, type, name, qualified_name, file_path,
	start_line, end_line, start_byte, end_byte, language,
	COALESCE(visibility,''), COALESCE(signature,''), COALESCE(doc_comment,''),
	COALESCE(complexity,0), in_degree, out_degree, pagerank, usage_score,
	confidence, COALESCE(sub_kind,''), COALESCE(attrs,'')`

// queryEdgesChunk is the per-chunk size for QueryEdgesForNodes. SQLite's
// SQLITE_MAX_VARIABLE_NUMBER caps single-statement parameters; default is 999
// on older builds, 32766 on modernc.org/sqlite — but go-stablenet's 217 K
// nodes would breach either limit when each chunk emits 2N params (src + dst).
// 400 ids per chunk = 800 params, comfortably below the conservative 999
// ceiling and well under the modern 32 K. Per § 3 Q5 in the G6 v3 redesign:
// "chunked QueryEdgesForNodes" is the named fix for this exact bottleneck.
const queryEdgesChunk = 400

// placeholders returns a comma-separated `?,?,?` of length n. n<=0 returns
// "" so callers can detect the empty case before building a malformed IN().
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*n-1)
	out = append(out, '?')
	for i := 1; i < n; i++ {
		out = append(out, ',', '?')
	}
	return string(out)
}

// scanNodes drains rows assuming the SELECT projects nodeColumns in order.
// All nullable columns are pre-COALESCE'd at the SQL layer so we can scan
// directly into string/int fields without sql.NullString plumbing.
func scanNodes(rows *sql.Rows) ([]types.Node, error) {
	var out []types.Node
	for rows.Next() {
		var n types.Node
		var conf, attrs string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
			&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
			&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
			&conf, &n.SubKind, &attrs); err != nil {
			return nil, fmt.Errorf("scan node row: %w", err)
		}
		n.Confidence = types.Confidence(conf)
		unmarshalNodeAttrs(attrs, &n)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node rows: %w", err)
	}
	return out, nil
}

// scanSearchHits drains rows that project the standard nodeColumns set
// followed by a trailing raw_score column (a float). Score is populated
// later by normalizeSearchHits — leaving it zero here keeps the scan
// branch-free.
func scanSearchHits(rows *sql.Rows) ([]SearchHit, error) {
	var out []SearchHit
	for rows.Next() {
		var n types.Node
		var conf, attrs string
		var raw float64
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
			&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
			&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
			&conf, &n.SubKind, &attrs, &raw); err != nil {
			return nil, fmt.Errorf("scan search hit row: %w", err)
		}
		n.Confidence = types.Confidence(conf)
		unmarshalNodeAttrs(attrs, &n)
		out = append(out, SearchHit{Node: n, RawScore: raw})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hit rows: %w", err)
	}
	return out, nil
}

// scanEdges drains rows produced by QueryEdgesForNodes (file_path/line are
// COALESCE'd in the SELECT, so direct scan is safe here too). dispatch_kind
// is the trailing column added in schema 1.7 (Track C P1b) — empty string
// for every non-`invokes` edge.
func scanEdges(rows *sql.Rows) ([]types.Edge, error) {
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &e.FilePath, &e.Line, &e.Count, &conf, &e.DispatchKind); err != nil {
			return nil, fmt.Errorf("scan edge row: %w", err)
		}
		e.Confidence = types.Confidence(conf)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge rows: %w", err)
	}
	return out, nil
}

// mapKeys is a generic helper that returns the keys of a map as a slice.
// Used by NeighborhoodByQname to convert the seen-set into a frontier.
func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// anys converts a []string into []any so it can be spread as variadic args
// to (*sql.DB).Query without callers writing the conversion every time.
func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
