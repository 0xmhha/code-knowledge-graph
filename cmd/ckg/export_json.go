// cmd/ckg/export_json.go — single-file JSON export of the full graph.
//
// Use case: portable graph snapshot for downstream tools, AI assistants,
// or alternative viewers. Mirrors graphify's `graph.json` ergonomic
// (one file you can ship anywhere) while preserving CKG's richer schema
// (32 edge types, 34 node types, confidence labels, dispatch_kind).
//
// Default output is minimal: nodes + edges + manifest summary. The
// shipped JSON is <100MB for the go-stablenet-scale graph (~220K nodes
// / 1.9M edges) and parses in ~1s on commodity hardware. Pass --pretty
// for human-readable indentation; the default packs each row on one
// line for grep/jq friendliness on large files.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// jsonGraphHeader is the top-level envelope written before the streaming
// nodes/edges arrays. Mirrors persist.Manifest fields the user actually
// wants in a portable export (the build-internal Files[] manifest is
// dropped — it's only useful for incremental cache decisions).
type jsonGraphHeader struct {
	SchemaVersion  string         `json:"schema_version"`
	CKGVersion     string         `json:"ckg_version"`
	SrcRoot        string         `json:"src_root"`
	SrcCommit      string         `json:"src_commit,omitempty"`
	BuildTimestamp string         `json:"build_timestamp"`
	Languages      map[string]int `json:"languages,omitempty"`
	Stats          map[string]int `json:"stats,omitempty"`
}

// minimalNode is the JSON shape emitted under --minimal: drops the
// scoring metadata (PageRank / degree / complexity), the byte-range
// coordinates (StartByte / EndByte — only useful with the matching
// graph.db blob), and the optional textual fields (Visibility / Signature
// / DocComment / SubKind). Keeps the eight fields a downstream consumer
// actually needs to reason about the symbol: ID, Type, Name,
// QualifiedName, FilePath, StartLine/EndLine, Language, Confidence.
type minimalNode struct {
	ID            string           `json:"id"`
	Type          types.NodeType   `json:"type"`
	Name          string           `json:"name"`
	QualifiedName string           `json:"qualified_name"`
	FilePath      string           `json:"file_path"`
	StartLine     int              `json:"start_line"`
	EndLine       int              `json:"end_line"`
	Language      string           `json:"language"`
	Confidence    types.Confidence `json:"confidence"`
}

// minimalEdge keeps the join surface (Src/Dst/Type/Count/Confidence)
// + DispatchKind (the Track C invokes-classification — high signal,
// low cost) but drops the auto-increment ID, which is meaningful only
// inside the originating SQLite file.
type minimalEdge struct {
	Src          string           `json:"src"`
	Dst          string           `json:"dst"`
	Type         types.EdgeType   `json:"type"`
	FilePath     string           `json:"file_path,omitempty"`
	Line         int              `json:"line,omitempty"`
	Count        int              `json:"count"`
	Confidence   types.Confidence `json:"confidence"`
	DispatchKind string           `json:"dispatch_kind,omitempty"`
}

func toMinimalNode(n types.Node) minimalNode {
	return minimalNode{
		ID: n.ID, Type: n.Type, Name: n.Name, QualifiedName: n.QualifiedName,
		FilePath: n.FilePath, StartLine: n.StartLine, EndLine: n.EndLine,
		Language: n.Language, Confidence: n.Confidence,
	}
}

func toMinimalEdge(e types.Edge) minimalEdge {
	return minimalEdge{
		Src: e.Src, Dst: e.Dst, Type: e.Type, FilePath: e.FilePath,
		Line: e.Line, Count: e.Count, Confidence: e.Confidence,
		DispatchKind: e.DispatchKind,
	}
}

func newExportJSONCmd() *cobra.Command {
	var graph, out string
	var pretty, minimal bool
	cmd := &cobra.Command{
		Use:   "export-json",
		Short: "Export the full graph as a single portable JSON file",
		Long: `Export the full graph (nodes + edges + manifest summary) as one JSON
file. Suitable for downstream tools, AI assistants, or alternative
viewers that don't want to embed SQLite. Schema:

  {
    "schema_version": "1.8",
    "ckg_version": "...",
    "src_root": "...",
    "build_timestamp": "...",
    "stats":     {"nodes": N, "edges": M, ...},
    "languages": {"go": ..., "ts": ..., "sol": ...},
    "nodes":     [ {Node row} ],
    "edges":     [ {Edge row} ]
  }

Each Node / Edge row matches pkg/types/{node,edge}.go field names —
preserves confidence labels, dispatch_kind, in/out degree, PageRank,
and language so the export is lossless against the SQLite source.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer func() { _ = store.Close() }()

			manifest, err := store.GetManifest()
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}
			nodes, err := store.AllNodes()
			if err != nil {
				return fmt.Errorf("load nodes: %w", err)
			}
			edges, err := store.AllEdges()
			if err != nil {
				return fmt.Errorf("load edges: %w", err)
			}

			f, err := os.Create(out)
			if err != nil {
				return fmt.Errorf("create out: %w", err)
			}
			defer func() { _ = f.Close() }()
			w := bufio.NewWriter(f)
			defer func() { _ = w.Flush() }()

			if err := writeGraphJSON(w, manifest, nodes, edges, pretty, minimal); err != nil {
				return fmt.Errorf("write json: %w", err)
			}
			mode := "full"
			if minimal {
				mode = "minimal"
			}
			_, _ = fmt.Fprintf(os.Stderr,
				"ckg: exported %d nodes / %d edges to %s (%s)\n",
				len(nodes), len(edges), out, mode)
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "", "output JSON file path (required)")
	cmd.Flags().BoolVar(&pretty, "pretty", false,
		"pretty-print with 2-space indentation (larger file; default packs each row on one line)")
	cmd.Flags().BoolVar(&minimal, "minimal", false,
		"drop metadata fields (PageRank / degree / byte ranges / Visibility / Signature / DocComment / SubKind / Edge.ID) — typically 10-20% smaller, but the bigger win is a tighter schema for downstream consumers that don't have the originating graph.db handy")
	_ = cmd.MarkFlagRequired("graph")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

// writeGraphJSON streams the envelope + nodes + edges arrays. We hand-
// roll the array brackets so the encoder can flush each Node/Edge row
// independently — building the whole structure in memory would peak
// at ~3x file size on a 220K-node graph.
//
// minimal=true emits the slim minimalNode/minimalEdge shapes (see
// types defined above); minimal=false preserves the full pkg/types
// schema. Both modes share the same envelope + bracket scaffolding so
// the file is a single switch on the per-row Marshal call.
func writeGraphJSON(w *bufio.Writer, m persist.Manifest,
	nodes []types.Node, edges []types.Edge, pretty, minimal bool) error {
	hdr := jsonGraphHeader{
		SchemaVersion:  m.SchemaVersion,
		CKGVersion:     m.CKGVersion,
		SrcRoot:        m.SrcRoot,
		SrcCommit:      m.SrcCommit,
		BuildTimestamp: m.BuildTimestamp,
		Languages:      m.Languages,
		Stats:          m.Stats,
	}

	if _, err := w.WriteString("{\n"); err != nil {
		return err
	}
	if err := writeKeyValue(w, "header", hdr, pretty); err != nil {
		return err
	}
	if _, err := w.WriteString(",\n  \"nodes\": ["); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("    ", "  ")
	}
	for i, n := range nodes {
		if i > 0 {
			if _, err := w.WriteString(","); err != nil {
				return err
			}
		}
		if pretty {
			if _, err := w.WriteString("\n    "); err != nil {
				return err
			}
		}
		// json.Encoder appends a trailing newline; for non-pretty mode
		// that breaks the `[item,item]` shape. Use Marshal directly.
		var (
			b   []byte
			err error
		)
		if minimal {
			b, err = json.Marshal(toMinimalNode(n))
		} else {
			b, err = json.Marshal(n)
		}
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if pretty && len(nodes) > 0 {
		if _, err := w.WriteString("\n  "); err != nil {
			return err
		}
	}
	if _, err := w.WriteString("],\n  \"edges\": ["); err != nil {
		return err
	}
	for i, e := range edges {
		if i > 0 {
			if _, err := w.WriteString(","); err != nil {
				return err
			}
		}
		if pretty {
			if _, err := w.WriteString("\n    "); err != nil {
				return err
			}
		}
		var (
			b   []byte
			err error
		)
		if minimal {
			b, err = json.Marshal(toMinimalEdge(e))
		} else {
			b, err = json.Marshal(e)
		}
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if pretty && len(edges) > 0 {
		if _, err := w.WriteString("\n  "); err != nil {
			return err
		}
	}
	_, err := w.WriteString("]\n}\n")
	return err
}

// writeKeyValue emits "  key": value" with optional pretty indentation.
// Used for the header object only; arrays are streamed separately.
func writeKeyValue(w *bufio.Writer, key string, v any, pretty bool) error {
	if _, err := fmt.Fprintf(w, "  %q: ", key); err != nil {
		return err
	}
	var b []byte
	var err error
	if pretty {
		b, err = json.MarshalIndent(v, "  ", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
