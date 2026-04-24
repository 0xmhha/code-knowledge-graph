package persist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExportChunked writes a portable static layout under outDir per spec §6.6.
// nodeChunkSize / edgeChunkSize control nodes-per-file and edges-per-file.
//
// Directory layout:
//
//	outDir/
//	  manifest.json
//	  hierarchy/pkg_tree.json
//	  hierarchy/topic_tree.json
//	  nodes/chunk_NNNN.json
//	  edges/chunk_NNNN.json
//	  blobs/<nodeID>.txt
func (s *Store) ExportChunked(outDir string, nodeChunkSize, edgeChunkSize int) error {
	if err := os.MkdirAll(filepath.Join(outDir, "nodes"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "edges"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "hierarchy"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "blobs"), 0o755); err != nil {
		return err
	}

	// Manifest
	m, err := s.GetManifest()
	if err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(outDir, "manifest.json"), m); err != nil {
		return err
	}

	// Hierarchies — LoadHierarchy returns []HierarchyRow; missing rows yield
	// an empty slice which marshals to `null` unless we normalize. Keep as-is
	// so downstream static viewer sees the same shape as the HTTP API.
	pkg, _ := s.LoadHierarchy("pkg")
	topic, _ := s.LoadHierarchy("topic")
	if err := writeJSONFile(filepath.Join(outDir, "hierarchy", "pkg_tree.json"), pkg); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(outDir, "hierarchy", "topic_tree.json"), topic); err != nil {
		return err
	}

	// Nodes — chunked. Use nodeColumns (with COALESCE) because scanNodes
	// scans into non-nullable string fields.
	rows, err := s.db.Query(`SELECT ` + nodeColumns + ` FROM nodes`)
	if err != nil {
		return err
	}
	defer rows.Close()
	nodes, err := scanNodes(rows)
	if err != nil {
		return err
	}
	for i, chunkIdx := 0, 0; i < len(nodes); i, chunkIdx = i+nodeChunkSize, chunkIdx+1 {
		end := i + nodeChunkSize
		if end > len(nodes) {
			end = len(nodes)
		}
		path := filepath.Join(outDir, "nodes", fmt.Sprintf("chunk_%04d.json", chunkIdx))
		if err := writeJSONFile(path, nodes[i:end]); err != nil {
			return err
		}
	}

	// Edges — chunked
	er, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence FROM edges`)
	if err != nil {
		return err
	}
	defer er.Close()
	edges, err := scanEdges(er)
	if err != nil {
		return err
	}
	for i, chunkIdx := 0, 0; i < len(edges); i, chunkIdx = i+edgeChunkSize, chunkIdx+1 {
		end := i + edgeChunkSize
		if end > len(edges) {
			end = len(edges)
		}
		path := filepath.Join(outDir, "edges", fmt.Sprintf("chunk_%04d.json", chunkIdx))
		if err := writeJSONFile(path, edges[i:end]); err != nil {
			return err
		}
	}

	// Blobs — one raw-text file per node (viewer fetches these as plain text,
	// not JSON, so no wrapping).
	br, err := s.db.Query(`SELECT node_id, source FROM blobs`)
	if err != nil {
		return err
	}
	defer br.Close()
	for br.Next() {
		var id string
		var b []byte
		if err := br.Scan(&id, &b); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "blobs", id+".txt"), b, 0o644); err != nil {
			return err
		}
	}
	if err := br.Err(); err != nil {
		return fmt.Errorf("iterate blob rows: %w", err)
	}
	return nil
}

// writeJSONFile marshals v (no indent — static export is size-sensitive) and
// writes to path with 0o644. Used for manifest, hierarchies, node/edge chunks.
func writeJSONFile(path string, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}
