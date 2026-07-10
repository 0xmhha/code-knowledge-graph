package persist

import (
	"encoding/json"
	"fmt"
)

// Manifest captures build-time metadata. Stored as key/value rows in the
// manifest table; complex fields are JSON-encoded.
//
// SchemaVersion policy: bumped on BREAKING changes only — i.e. changes that
// existing readers cannot transparently tolerate (renamed/removed fields,
// changed semantics, incompatible row layout). Additive optional fields with
// `omitempty` do NOT bump SchemaVersion: old readers ignore unknown JSON
// fields and unset optional fields decode as zero values. Example: the
// SrcRelPath field was added without a bump (1.0 → still 1.0) because empty
// SrcRelPath triggers the legacy back-compat branch in callers. Resist the
// urge to over-bump; spurious bumps force unnecessary rebuilds across all
// existing graph DBs.
type Manifest struct {
	SchemaVersion       string         `json:"schema_version"`
	CKGVersion          string         `json:"ckg_version"`
	BuildTimestamp      string         `json:"build_timestamp"`
	SrcRoot             string         `json:"src_root"`
	SrcRelPath          string         `json:"src_rel_path,omitempty"` // src_root relative to git repo root; enables path-aware staleness
	SrcCommit           string         `json:"src_commit,omitempty"`
	StalenessMethod     string         `json:"staleness_method"` // "git" | "mtime"
	StalenessFiles      []string       `json:"staleness_files,omitempty"`
	StalenessMTimeSum   int64          `json:"staleness_mtime_sum,omitempty"`
	Languages           map[string]int `json:"languages"`
	Stats               map[string]int `json:"stats"`
	CKGIgnore           []string       `json:"ckgignore,omitempty"`
	ParseErrorsCount    int            `json:"parse_errors_count"`
	UnresolvedRefsCount int            `json:"unresolved_refs_count"`
	ClusteringStatus    string         `json:"clustering_status"` // "ok" | "pkg_only"
	// GraphDigest is a deterministic, build-invariant hash of the CODE graph —
	// the coordinate pin anchor CKV/CKS use to assert they are aligned to the
	// same graph (see docs/coordination-reindex-migration-2026-07-10.md § Q1).
	// It hashes id-sorted node identity lines + sorted (Type,Src,Dst,Line) edge
	// lines, EXCLUDING derived metrics (pagerank/degrees) and temporal
	// (Commit/Hunk) nodes/edges, so it is identical across cold/incremental
	// builds and machines (ADR-0002). Additive + omitempty: pre-digest manifests
	// decode it as "" and old readers ignore it — no SchemaVersion bump.
	GraphDigest string `json:"graph_digest,omitempty"`
	// Files is the per-file incremental-cache record (A3 Phase 1, schema 1.2).
	// Each entry tracks the SHA256 + cache key of one source file plus the
	// node/edge IDs it produced, enabling subsequent builds to skip parsing
	// for unchanged files. omitempty so pre-1.2 manifests reload as nil and
	// trigger a full rebuild on the next ckg build invocation.
	Files []FileEntry `json:"files,omitempty"`
}

// FileEntry records the cache fingerprint and produced node/edge IDs for one
// source file. CacheKey covers content + ckg_version + parser_version +
// schema_version (see internal/buildpipe/cache.go ComputeCacheKey) so any
// upstream change correctly invalidates the entry.
type FileEntry struct {
	Path          string   `json:"path"`           // srcRoot-relative slash form
	Language      string   `json:"language"`       // "go" | "ts" | "sol"
	SHA256        string   `json:"sha256"`         // hex of file content
	CacheKey      string   `json:"cache_key"`      // hex of full key
	MTime         int64    `json:"mtime"`          // unix nanoseconds (fast path)
	NodeIDs       []string `json:"node_ids"`       // IDs this file produced
	EdgeIDs       []int64  `json:"edge_ids"`       // edge row IDs
	ParserVersion string   `json:"parser_version"` // see ComputeCacheKey
}

// SetManifest replaces existing manifest rows with fields from m.
func (s *sqliteStore) SetManifest(m Manifest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM manifest`); err != nil {
		return err
	}
	rows := []struct{ k, v string }{
		{"schema_version", m.SchemaVersion},
		{"ckg_version", m.CKGVersion},
		{"build_timestamp", m.BuildTimestamp},
		{"src_root", m.SrcRoot},
		{"src_rel_path", m.SrcRelPath},
		{"src_commit", m.SrcCommit},
		{"staleness_method", m.StalenessMethod},
		{"clustering_status", m.ClusteringStatus},
		// graph_digest is the coordinate pin anchor. It MUST live in the in-db
		// manifest table (not just manifest.json) because CKV/CKS read it via
		// `SELECT value FROM manifest WHERE key='graph_digest'` alongside
		// schema_version / src_commit. Empty string when a pre-digest build
		// produced this graph.
		{"graph_digest", m.GraphDigest},
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO manifest (key, value) VALUES (?, ?)`, r.k, r.v); err != nil {
			return err
		}
	}
	jsonRows := []struct {
		k string
		v any
	}{
		{"staleness_files", m.StalenessFiles},
		{"staleness_mtime_sum", m.StalenessMTimeSum},
		{"languages", m.Languages},
		{"stats", m.Stats},
		{"ckgignore", m.CKGIgnore},
		{"parse_errors_count", m.ParseErrorsCount},
		{"unresolved_refs_count", m.UnresolvedRefsCount},
		// "files" is the A3 incremental-cache fingerprint blob. It can be large
		// on big repos (~120 bytes per file), but JSON-blob storage in the
		// existing kv table is fine for v0.2 — A3 Phase 2/3 may move this to
		// per-file rows for partial query performance.
		{"files", m.Files},
	}
	for _, r := range jsonRows {
		buf, err := json.Marshal(r.v)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO manifest (key, value) VALUES (?, ?)`, r.k, string(buf)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetManifest reads all manifest rows and reassembles the struct.
func (s *sqliteStore) GetManifest() (Manifest, error) {
	rows, err := s.db.Query(`SELECT key, value FROM manifest`)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = rows.Close() }()
	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Manifest{}, err
		}
		kv[k] = v
	}
	// A driver-level iteration error (e.g. a truncated read) ends the loop
	// early; without this check it would be swallowed and a partial manifest
	// returned as success — a silently incomplete index.
	if err := rows.Err(); err != nil {
		return Manifest{}, err
	}
	m := Manifest{
		SchemaVersion:    kv["schema_version"],
		CKGVersion:       kv["ckg_version"],
		BuildTimestamp:   kv["build_timestamp"],
		SrcRoot:          kv["src_root"],
		SrcRelPath:       kv["src_rel_path"],
		SrcCommit:        kv["src_commit"],
		StalenessMethod:  kv["staleness_method"],
		ClusteringStatus: kv["clustering_status"],
		GraphDigest:      kv["graph_digest"],
	}
	for _, j := range []struct {
		k   string
		dst any
	}{
		{"staleness_files", &m.StalenessFiles},
		{"staleness_mtime_sum", &m.StalenessMTimeSum},
		{"languages", &m.Languages},
		{"stats", &m.Stats},
		{"ckgignore", &m.CKGIgnore},
		{"parse_errors_count", &m.ParseErrorsCount},
		{"unresolved_refs_count", &m.UnresolvedRefsCount},
		// pre-1.2 manifests have no "files" key; the empty branch leaves
		// m.Files as nil, which buildpipe interprets as "no cache available".
		{"files", &m.Files},
	} {
		if v, ok := kv[j.k]; ok && v != "" {
			if err := json.Unmarshal([]byte(v), j.dst); err != nil {
				return m, fmt.Errorf("decode %s: %w", j.k, err)
			}
		}
	}
	return m, nil
}
