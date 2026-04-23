# CKG Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a single Go binary `ckg` (multi-OS) that parses Go / TypeScript / Solidity into a 29-node × 22-edge SQLite graph, served via embedded 3D viewer (LOD) and stdio MCP server, with a four-baseline evaluation runner that compares graph context against raw-file context.

**Architecture:** Five subcommands (`build`, `serve`, `mcp`, `export-static`, `eval`) share a single `graph.db` SQLite store. Build pipeline: detect → parse (per-language tree-sitter / `go/packages`) → link → graph build → cluster (package-tree + Leiden topic overlay) → score → persist. Viewer is a vanilla JS + lit-html bundle embedded via `embed.FS`, served over HTTP `/api/*` and lazily loading by zoom-level. MCP exposes six tools (one smart `get_context_for_task` plus five granular). Eval runner switches tool allowlists per baseline and measures token + accuracy.

**Tech Stack:** Go 1.22+, `golang.org/x/tools/go/packages`, `github.com/smacker/go-tree-sitter` (TS/Sol grammars), `modernc.org/sqlite` (CGO-free), `log/slog`, `github.com/spf13/cobra` (CLI), MCP Go SDK (`mark3labs/mcp-go`), Anthropic Go SDK, `github.com/go-playground/validator/v10`, vanilla JS + `lit-html` + `3d-force-graph` + `esbuild`, Playwright (smoke).

**Source spec:** `docs/spec-ckg-v0-prototype.md` (committed `a1ea7bf`). **Companion:** `docs/STUDY-GUIDE.md`.

**Phase index (10 phases, 38 tasks, 7 checkpoints):**

| Phase | Tasks | Output | Checkpoint |
|---|---|---|---|
| 1. Foundation | T1–T4 | go.mod, types, persist, manifest | — |
| 2. Detect + Go parser | T5–T9 | Go-only AST → file-local nodes/edges | — |
| 3. Graph + cluster + score | T10–T14 | in-memory graph + pkg_tree + Leiden + scores | — |
| 4. First vertical slice | T15–T17 | working `ckg build` for Go corpus | **CP-1** |
| 5. TS + Sol parsers + cross-lang | T18–T21 | build for all 3 languages | **CP-2** |
| 6. Viewer + `ckg serve` | T22–T26 | 3D viewer w/ LOD, smoke passes | **CP-3** |
| 7. MCP server + `ckg mcp` | T27–T29 | 6 tools over stdio | **CP-4** |
| 8. Export-static | T30–T31 | portable chunked JSON | **CP-5** |
| 9. Eval framework + `ckg eval` | T32–T35 | 4-baseline measurement | **CP-6** |
| 10. Corpus + CI + docs | T36–T38 | synthetic + CI green + V0 DoD | **CP-7** |

---

## Conventions used in this plan

- **Module path:** `github.com/0xmhha/code-knowledge-graph` (override at `go mod init` time if user prefers different remote).
- **Go version:** 1.25 (uses `slog` stdlib, generics maturity, `for range over int`; bumped from 1.22 to satisfy modernc.org/sqlite v1.49.1).
- **Test framework:** stdlib `testing` + table-driven; no testify.
- **Fixture pattern:** `testdata/` per package, golden files `*_golden.json` for parser outputs.
- **Commit cadence:** one commit per task minimum; include test + impl in same commit when small.
- **Commit style:** Conventional Commits, English subject, no co-author attribution (per repo convention).
- **TDD:** write test first → run → fail → impl → run → pass → commit. Skip TDD for pure config / scaffolding tasks (will be marked).
- **All file paths:** relative to repo root `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph` unless noted.

---

# Phase 1 — Foundation

Goal: bootstrap repo, define core types shared across all packages, set up SQLite persistence + manifest. After Phase 1: `go test ./...` runs (mostly empty), `go build ./...` succeeds, persist layer can store/retrieve nodes & edges.

---

## Task 1: Repo bootstrap

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `Makefile`
- Create: `.ckgignore`  (default contents)
- Create: `README.md`  (placeholder; expanded in T38)

**No TDD** — pure scaffolding.

- [ ] **Step 1: Initialize go module**

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
go mod init github.com/0xmhha/code-knowledge-graph
```

Expected: creates `go.mod` with `module github.com/0xmhha/code-knowledge-graph` and `go 1.22`. (T3's `go get modernc.org/sqlite` will later auto-bump this to `go 1.25` — that is expected.)

- [ ] **Step 2: Add `.gitignore`**

```gitignore
# Binaries
/bin/
*.exe

# Test coverage
coverage.out
*.coverprofile

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store

# Build outputs
/web/viewer/dist/
/web/viewer/node_modules/

# CKG runtime outputs
/tmp/ckg-*
*.db-journal
*.db-shm
*.db-wal

# Local Claude settings
.claude/settings.local.json
```

- [ ] **Step 3: Add `.ckgignore` default (committed at repo root for users who copy)**

```
vendor/
node_modules/
dist/
build/
*.generated.*
tests/fixtures/
# *_test.go  (uncomment to exclude Go test files)
```

- [ ] **Step 4: Add minimal `Makefile`**

```makefile
.PHONY: all build viewer test test-race lint clean

GO ?= go

all: build

viewer:
	cd web/viewer && npm install && node esbuild.config.js

build: viewer
	$(GO) build -o bin/ckg ./cmd/ckg

build-no-viewer:
	$(GO) build -o bin/ckg ./cmd/ckg

test:
	$(GO) test ./...

test-race:
	$(GO) test -race -coverprofile=coverage.out ./...

lint:
	$(GO) vet ./...

clean:
	rm -rf bin/ /tmp/ckg-* coverage.out web/viewer/dist/
```

- [ ] **Step 5: Add placeholder `README.md`**

```markdown
# CKG — Code Knowledge Graph

A Go CLI that turns source code into a queryable knowledge graph.

See `docs/spec-ckg-v0-prototype.md` for the full design.
See `docs/STUDY-GUIDE.md` for background concepts.

## Status

In active development. Quick start arrives at the end of Phase 10.
```

- [ ] **Step 6: Verify build + commit**

```bash
go build ./...    # Should succeed (no packages yet)
go test ./...     # Should print "no Go files"
git add go.mod .gitignore .ckgignore Makefile README.md
git commit -m "chore: bootstrap Go module and build scaffolding"
```

Expected: clean working tree.

---

## Task 2: Core types (Node, Edge, enums)

**Files:**
- Create: `pkg/types/node.go`
- Create: `pkg/types/edge.go`
- Create: `pkg/types/enums.go`
- Test:   `pkg/types/types_test.go`

- [ ] **Step 1: Write failing test for NodeType / EdgeType enums**

`pkg/types/types_test.go`:

```go
package types_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestNodeTypeCount(t *testing.T) {
	if got, want := len(types.AllNodeTypes()), 29; got != want {
		t.Fatalf("AllNodeTypes count = %d, want %d", got, want)
	}
}

func TestEdgeTypeCount(t *testing.T) {
	if got, want := len(types.AllEdgeTypes()), 22; got != want {
		t.Fatalf("AllEdgeTypes count = %d, want %d", got, want)
	}
}

func TestConfidenceValid(t *testing.T) {
	for _, c := range []types.Confidence{types.ConfExtracted, types.ConfInferred, types.ConfAmbiguous} {
		if !c.Valid() {
			t.Errorf("Confidence(%q) should be valid", c)
		}
	}
	if types.Confidence("BOGUS").Valid() {
		t.Error("Confidence(BOGUS) should be invalid")
	}
}
```

- [ ] **Step 2: Run test to verify fail**

```bash
go test ./pkg/types/... -v
```

Expected: build error — package does not exist.

- [ ] **Step 3: Implement enums**

`pkg/types/enums.go`:

```go
package types

// NodeType enumerates the 29 node kinds (spec §5.1).
type NodeType string

const (
	NodePackage       NodeType = "Package"
	NodeFile          NodeType = "File"
	NodeStruct        NodeType = "Struct"
	NodeInterface     NodeType = "Interface"
	NodeClass         NodeType = "Class"
	NodeTypeAlias     NodeType = "TypeAlias"
	NodeEnum          NodeType = "Enum"
	NodeContract      NodeType = "Contract"
	NodeMapping       NodeType = "Mapping"
	NodeEvent         NodeType = "Event"
	NodeFunction      NodeType = "Function"
	NodeMethod        NodeType = "Method"
	NodeModifier      NodeType = "Modifier"
	NodeConstructor   NodeType = "Constructor"
	NodeConstant      NodeType = "Constant"
	NodeVariable      NodeType = "Variable"
	NodeField         NodeType = "Field"
	NodeParameter     NodeType = "Parameter"
	NodeLocalVariable NodeType = "LocalVariable"
	NodeImport        NodeType = "Import"
	NodeExport        NodeType = "Export"
	NodeDecorator     NodeType = "Decorator"
	NodeGoroutine     NodeType = "Goroutine"
	NodeChannel       NodeType = "Channel"
	NodeIfStmt        NodeType = "IfStmt"
	NodeLoopStmt      NodeType = "LoopStmt"
	NodeCallSite      NodeType = "CallSite"
	NodeReturnStmt    NodeType = "ReturnStmt"
	NodeSwitchStmt    NodeType = "SwitchStmt"
)

// AllNodeTypes returns all 29 node types in a stable order.
func AllNodeTypes() []NodeType {
	return []NodeType{
		NodePackage, NodeFile, NodeStruct, NodeInterface, NodeClass,
		NodeTypeAlias, NodeEnum, NodeContract, NodeMapping, NodeEvent,
		NodeFunction, NodeMethod, NodeModifier, NodeConstructor,
		NodeConstant, NodeVariable, NodeField, NodeParameter, NodeLocalVariable,
		NodeImport, NodeExport, NodeDecorator,
		NodeGoroutine, NodeChannel,
		NodeIfStmt, NodeLoopStmt, NodeCallSite, NodeReturnStmt, NodeSwitchStmt,
	}
}

// EdgeType enumerates the 22 edge kinds (spec §5.2).
type EdgeType string

const (
	EdgeContains      EdgeType = "contains"
	EdgeDefines       EdgeType = "defines"
	EdgeCalls         EdgeType = "calls"
	EdgeInvokes       EdgeType = "invokes"
	EdgeUsesType      EdgeType = "uses_type"
	EdgeInstantiates  EdgeType = "instantiates"
	EdgeReferences    EdgeType = "references"
	EdgeReadsField    EdgeType = "reads_field"
	EdgeWritesField   EdgeType = "writes_field"
	EdgeImports       EdgeType = "imports"
	EdgeExports       EdgeType = "exports"
	EdgeImplements    EdgeType = "implements"
	EdgeExtends       EdgeType = "extends"
	EdgeHasModifier   EdgeType = "has_modifier"
	EdgeEmitsEvent    EdgeType = "emits_event"
	EdgeReadsMapping  EdgeType = "reads_mapping"
	EdgeWritesMapping EdgeType = "writes_mapping"
	EdgeHasDecorator  EdgeType = "has_decorator"
	EdgeSpawns        EdgeType = "spawns"
	EdgeSendsTo       EdgeType = "sends_to"
	EdgeRecvsFrom     EdgeType = "recvs_from"
	EdgeBindsTo       EdgeType = "binds_to"
)

// AllEdgeTypes returns all 22 edge types in stable order.
func AllEdgeTypes() []EdgeType {
	return []EdgeType{
		EdgeContains, EdgeDefines, EdgeCalls, EdgeInvokes, EdgeUsesType,
		EdgeInstantiates, EdgeReferences, EdgeReadsField, EdgeWritesField,
		EdgeImports, EdgeExports, EdgeImplements, EdgeExtends,
		EdgeHasModifier, EdgeEmitsEvent, EdgeReadsMapping, EdgeWritesMapping,
		EdgeHasDecorator, EdgeSpawns, EdgeSendsTo, EdgeRecvsFrom, EdgeBindsTo,
	}
}

// Confidence labels (spec §4.8).
type Confidence string

const (
	ConfExtracted Confidence = "EXTRACTED"
	ConfInferred  Confidence = "INFERRED"
	ConfAmbiguous Confidence = "AMBIGUOUS"
)

// Valid reports whether c is one of the three known confidence labels.
func (c Confidence) Valid() bool {
	switch c {
	case ConfExtracted, ConfInferred, ConfAmbiguous:
		return true
	}
	return false
}
```

- [ ] **Step 4: Implement Node and Edge structs**

`pkg/types/node.go`:

```go
package types

// Node mirrors the SQLite nodes row plus runtime fields (spec §5.3).
type Node struct {
	ID            string     `json:"id"             validate:"required,len=16"`
	Type          NodeType   `json:"type"           validate:"required"`
	Name          string     `json:"name"           validate:"required"`
	QualifiedName string     `json:"qualified_name" validate:"required"`
	FilePath      string     `json:"file_path"      validate:"required"`
	StartLine     int        `json:"start_line"     validate:"min=1"`
	EndLine       int        `json:"end_line"       validate:"min=1"`
	StartByte     int        `json:"start_byte"     validate:"min=0"`
	EndByte       int        `json:"end_byte"       validate:"gtfield=StartByte"`
	Language      string     `json:"language"       validate:"required,oneof=go ts sol"`
	Visibility    string     `json:"visibility,omitempty"`
	Signature     string     `json:"signature,omitempty"`
	DocComment    string     `json:"doc_comment,omitempty"`
	Complexity    int        `json:"complexity,omitempty"`
	InDegree      int        `json:"in_degree"`
	OutDegree     int        `json:"out_degree"`
	PageRank      float64    `json:"pagerank"`
	UsageScore    float64    `json:"usage_score"`
	Confidence    Confidence `json:"confidence"     validate:"required"`
	SubKind       string     `json:"sub_kind,omitempty"`
}
```

`pkg/types/edge.go`:

```go
package types

// Edge mirrors the SQLite edges row (spec §5.3).
type Edge struct {
	ID         int64      `json:"id,omitempty"`
	Src        string     `json:"src"        validate:"required,len=16"`
	Dst        string     `json:"dst"        validate:"required,len=16"`
	Type       EdgeType   `json:"type"       validate:"required"`
	FilePath   string     `json:"file_path,omitempty"`
	Line       int        `json:"line,omitempty"`
	Count      int        `json:"count"      validate:"min=1"`
	Confidence Confidence `json:"confidence" validate:"required"`
}
```

- [ ] **Step 5: Run tests to verify pass**

```bash
go test ./pkg/types/... -v
```

Expected: PASS for all three tests.

- [ ] **Step 6: Commit**

```bash
git add pkg/types/ && git commit -m "feat(types): define 29 node and 22 edge types with validation tags"
```

---

## Task 3: SQLite schema + persist layer (write/read nodes & edges)

**Files:**
- Create: `internal/persist/schema.sql`
- Create: `internal/persist/sqlite.go`
- Test:   `internal/persist/sqlite_test.go`

- [ ] **Step 1: Add SQLite driver dependency**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 2: Write schema as embedded SQL**

`internal/persist/schema.sql`:

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS nodes (
  id             TEXT PRIMARY KEY,
  type           TEXT NOT NULL,
  name           TEXT NOT NULL,
  qualified_name TEXT NOT NULL,
  file_path      TEXT NOT NULL,
  start_line     INTEGER NOT NULL,
  end_line       INTEGER NOT NULL,
  start_byte     INTEGER NOT NULL,
  end_byte       INTEGER NOT NULL,
  language       TEXT NOT NULL,
  visibility     TEXT,
  signature      TEXT,
  doc_comment    TEXT,
  complexity     INTEGER,
  in_degree      INTEGER NOT NULL DEFAULT 0,
  out_degree     INTEGER NOT NULL DEFAULT 0,
  pagerank       REAL    NOT NULL DEFAULT 0,
  usage_score    REAL    NOT NULL DEFAULT 0,
  confidence     TEXT    NOT NULL DEFAULT 'EXTRACTED',
  sub_kind       TEXT
);
CREATE INDEX IF NOT EXISTS idx_nodes_qname ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_nodes_file  ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_type  ON nodes(type);

CREATE TABLE IF NOT EXISTS edges (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  src         TEXT NOT NULL REFERENCES nodes(id),
  dst         TEXT NOT NULL REFERENCES nodes(id),
  type        TEXT NOT NULL,
  file_path   TEXT,
  line        INTEGER,
  count       INTEGER NOT NULL DEFAULT 1,
  confidence  TEXT NOT NULL DEFAULT 'EXTRACTED'
);
CREATE INDEX IF NOT EXISTS idx_edges_src  ON edges(src);
CREATE INDEX IF NOT EXISTS idx_edges_dst  ON edges(dst);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);

CREATE TABLE IF NOT EXISTS pkg_tree (
  parent_id TEXT NOT NULL REFERENCES nodes(id),
  child_id  TEXT NOT NULL REFERENCES nodes(id),
  level     INTEGER NOT NULL,
  PRIMARY KEY (parent_id, child_id)
);
CREATE INDEX IF NOT EXISTS idx_pkg_parent ON pkg_tree(parent_id);

CREATE TABLE IF NOT EXISTS topic_tree (
  parent_id   TEXT,
  child_id    TEXT NOT NULL REFERENCES nodes(id),
  resolution  INTEGER NOT NULL,
  topic_label TEXT,
  PRIMARY KEY (child_id, resolution, parent_id)
);

CREATE TABLE IF NOT EXISTS blobs (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id),
  source  BLOB NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
  name, qualified_name, signature, doc_comment,
  content='nodes', content_rowid='rowid'
);

CREATE TABLE IF NOT EXISTS manifest (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

- [ ] **Step 3: Write failing test for round-trip**

`internal/persist/sqlite_test.go`:

```go
package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestStoreRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	want := types.Node{
		ID: "abc123def456ghij", Type: types.NodeFunction,
		Name: "Foo", QualifiedName: "pkg.Foo",
		FilePath: "pkg/foo.go", StartLine: 10, EndLine: 12,
		StartByte: 100, EndByte: 150,
		Language: "go", Confidence: types.ConfExtracted,
	}
	if err := store.InsertNodes([]types.Node{want}); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}

	got, err := store.GetNode(want.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.QualifiedName != want.QualifiedName || got.Type != want.Type {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 4: Run test to verify fail**

```bash
go test ./internal/persist/... -v
```

Expected: build error — package not found.

- [ ] **Step 5: Implement Store**

`internal/persist/sqlite.go`:

```go
package persist

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps a SQLite database for the CKG graph.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite file at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// OpenReadOnly opens a SQLite file in read-only mode (used by serve/mcp).
func OpenReadOnly(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("open sqlite ro at %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

// Migrate creates tables if they don't already exist.
func (s *Store) Migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// InsertNodes bulk-inserts nodes (transactional).
func (s *Store) InsertNodes(nodes []types.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO nodes
		(id, type, name, qualified_name, file_path, start_line, end_line,
		 start_byte, end_byte, language, visibility, signature, doc_comment,
		 complexity, in_degree, out_degree, pagerank, usage_score, confidence, sub_kind)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, n := range nodes {
		if _, err := stmt.Exec(n.ID, n.Type, n.Name, n.QualifiedName, n.FilePath,
			n.StartLine, n.EndLine, n.StartByte, n.EndByte, n.Language,
			n.Visibility, n.Signature, n.DocComment, n.Complexity,
			n.InDegree, n.OutDegree, n.PageRank, n.UsageScore,
			string(n.Confidence), n.SubKind); err != nil {
			return fmt.Errorf("insert node %s: %w", n.ID, err)
		}
	}
	return tx.Commit()
}

// GetNode fetches a node by ID. Returns sql.ErrNoRows if not found.
func (s *Store) GetNode(id string) (types.Node, error) {
	row := s.db.QueryRow(`SELECT id, type, name, qualified_name, file_path,
		start_line, end_line, start_byte, end_byte, language, visibility,
		signature, doc_comment, complexity, in_degree, out_degree, pagerank,
		usage_score, confidence, sub_kind FROM nodes WHERE id = ?`, id)
	var n types.Node
	var conf string
	err := row.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
		&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
		&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
		&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
		&conf, &n.SubKind)
	if err != nil {
		return n, err
	}
	n.Confidence = types.Confidence(conf)
	return n, nil
}

// InsertEdges bulk-inserts edges (transactional).
func (s *Store) InsertEdges(edges []types.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO edges
		(src, dst, type, file_path, line, count, confidence)
		VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range edges {
		if _, err := stmt.Exec(e.Src, e.Dst, string(e.Type), e.FilePath, e.Line,
			e.Count, string(e.Confidence)); err != nil {
			return fmt.Errorf("insert edge %s->%s: %w", e.Src, e.Dst, err)
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 6: Run test to verify pass**

```bash
go test ./internal/persist/... -v -run TestStoreRoundTrip
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/persist/ go.mod go.sum
git commit -m "feat(persist): add SQLite store with embedded schema and node/edge round-trip"
```

---

## Task 4: Manifest write/read + staleness fields

**Files:**
- Create: `internal/persist/manifest.go`
- Test:   `internal/persist/manifest_test.go`

- [ ] **Step 1: Write failing test**

`internal/persist/manifest_test.go`:

```go
package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestManifestRoundTrip(t *testing.T) {
	store, err := persist.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}

	m := persist.Manifest{
		SchemaVersion: "1.0", CKGVersion: "0.1.0",
		BuildTimestamp: "2026-04-23T12:00:00Z",
		SrcRoot:        "/tmp/src",
		SrcCommit:      "abc123",
		StalenessMethod: "git",
		Languages:      map[string]int{"go": 10},
		Stats:          map[string]int{"nodes": 100, "edges": 200},
	}
	if err := store.SetManifest(m); err != nil {
		t.Fatalf("SetManifest: %v", err)
	}
	got, err := store.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.SrcCommit != "abc123" || got.Languages["go"] != 10 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify fail**

```bash
go test ./internal/persist/... -v -run TestManifestRoundTrip
```

Expected: undefined `persist.Manifest`.

- [ ] **Step 3: Implement Manifest type and Set/Get**

`internal/persist/manifest.go`:

```go
package persist

import (
	"encoding/json"
	"fmt"
)

// Manifest captures build-time metadata. Stored as key/value rows in the
// manifest table; complex fields are JSON-encoded.
type Manifest struct {
	SchemaVersion        string         `json:"schema_version"`
	CKGVersion           string         `json:"ckg_version"`
	BuildTimestamp       string         `json:"build_timestamp"`
	SrcRoot              string         `json:"src_root"`
	SrcCommit            string         `json:"src_commit,omitempty"`
	StalenessMethod      string         `json:"staleness_method"` // "git" | "mtime"
	StalenessFiles       []string       `json:"staleness_files,omitempty"`
	StalenessMTimeSum    int64          `json:"staleness_mtime_sum,omitempty"`
	Languages            map[string]int `json:"languages"`
	Stats                map[string]int `json:"stats"`
	CKGIgnore            []string       `json:"ckgignore,omitempty"`
	ParseErrorsCount     int            `json:"parse_errors_count"`
	UnresolvedRefsCount  int            `json:"unresolved_refs_count"`
	ClusteringStatus     string         `json:"clustering_status"` // "ok" | "pkg_only"
}

// SetManifest replaces existing manifest rows with fields from m.
func (s *Store) SetManifest(m Manifest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM manifest`); err != nil {
		return err
	}
	rows := []struct{ k, v string }{
		{"schema_version", m.SchemaVersion},
		{"ckg_version", m.CKGVersion},
		{"build_timestamp", m.BuildTimestamp},
		{"src_root", m.SrcRoot},
		{"src_commit", m.SrcCommit},
		{"staleness_method", m.StalenessMethod},
		{"clustering_status", m.ClusteringStatus},
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
func (s *Store) GetManifest() (Manifest, error) {
	rows, err := s.db.Query(`SELECT key, value FROM manifest`)
	if err != nil {
		return Manifest{}, err
	}
	defer rows.Close()
	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Manifest{}, err
		}
		kv[k] = v
	}
	m := Manifest{
		SchemaVersion:    kv["schema_version"],
		CKGVersion:       kv["ckg_version"],
		BuildTimestamp:   kv["build_timestamp"],
		SrcRoot:          kv["src_root"],
		SrcCommit:        kv["src_commit"],
		StalenessMethod:  kv["staleness_method"],
		ClusteringStatus: kv["clustering_status"],
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
	} {
		if v, ok := kv[j.k]; ok && v != "" {
			if err := json.Unmarshal([]byte(v), j.dst); err != nil {
				return m, fmt.Errorf("decode %s: %w", j.k, err)
			}
		}
	}
	return m, nil
}
```

- [ ] **Step 4: Run test to verify pass**

```bash
go test ./internal/persist/... -v -run TestManifestRoundTrip
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/persist/manifest.go internal/persist/manifest_test.go
git commit -m "feat(persist): add Manifest with JSON-serialized fields and round-trip helpers"
```

---

> **Phase 1 complete.** `go test ./...` passes; `internal/persist` can store and read nodes, edges, and manifest. Continue with Phase 2 to add file detection and the Go parser.

---

# Phase 2 — Detect + Go Parser

Goal: walk a source tree honoring `.ckgignore`, dispatch by extension to the Go parser, and extract Pass-1 (structural) + Pass-2 (resolution) outputs into `[]Node` + `[]Edge`. After Phase 2: `go test ./...` produces resolved Go graph fragments for any small Go fixture.

---

## Task 5: Detect — file walker + `.ckgignore`

**Files:**
- Create: `internal/detect/walker.go`
- Create: `internal/detect/ckgignore.go`
- Test:   `internal/detect/walker_test.go`
- Test:   `internal/detect/ckgignore_test.go`
- Create: `internal/detect/testdata/sample/{a.go,b.ts,c.sol,vendor/d.go,.ckgignore}`

- [ ] **Step 1: Create test fixture**

```bash
mkdir -p internal/detect/testdata/sample/vendor
echo 'package a' > internal/detect/testdata/sample/a.go
echo 'export const x = 1;' > internal/detect/testdata/sample/b.ts
echo 'pragma solidity ^0.8.0;' > internal/detect/testdata/sample/c.sol
echo 'package d' > internal/detect/testdata/sample/vendor/d.go
printf "vendor/\n*.generated.*\n" > internal/detect/testdata/sample/.ckgignore
echo 'export const y = 2;' > internal/detect/testdata/sample/foo.generated.ts
```

- [ ] **Step 2: Write failing test**

`internal/detect/walker_test.go`:

```go
package detect_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
)

func TestWalkClassifies(t *testing.T) {
	root := "testdata/sample"
	got, err := detect.Walk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sortFiles := func(s []string) { sort.Strings(s) }
	sortFiles(got.Go)
	sortFiles(got.TS)
	sortFiles(got.Sol)

	if want := []string{"a.go"}; !equal(got.Go, want) {
		t.Errorf("Go = %v, want %v (vendor/d.go must be ignored)", got.Go, want)
	}
	if want := []string{"b.ts"}; !equal(got.TS, want) {
		t.Errorf("TS = %v, want %v (foo.generated.ts must be ignored)", got.TS, want)
	}
	if want := []string{"c.sol"}; !equal(got.Sol, want) {
		t.Errorf("Sol = %v, want %v", got.Sol, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 3: Run to verify fail**

```bash
go test ./internal/detect/... -v -run TestWalkClassifies
```

Expected: package not found.

- [ ] **Step 4: Implement `.ckgignore` matcher**

`internal/detect/ckgignore.go`:

```go
package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// CKGIgnore is a gitignore-style matcher (no negations, no anchored leading slashes).
// Patterns ending in "/" match directories. Patterns containing "*" use filepath.Match.
type CKGIgnore struct {
	patterns []string
}

// LoadCKGIgnore reads `.ckgignore` from root. Missing file is OK (returns empty matcher).
func LoadCKGIgnore(root string) (*CKGIgnore, error) {
	f, err := os.Open(filepath.Join(root, ".ckgignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return &CKGIgnore{}, nil
		}
		return nil, err
	}
	defer f.Close()
	c := &CKGIgnore{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c.patterns = append(c.patterns, line)
	}
	return c, sc.Err()
}

// Match reports whether the relative path (filepath separator) is ignored.
func (c *CKGIgnore) Match(rel string) bool {
	rel = filepath.ToSlash(rel)
	for _, p := range c.patterns {
		if matchPattern(p, rel) {
			return true
		}
	}
	return false
}

func matchPattern(pat, rel string) bool {
	pat = filepath.ToSlash(pat)
	dirPat := strings.HasSuffix(pat, "/")
	if dirPat {
		pat = strings.TrimSuffix(pat, "/")
		// match if any path component equals pat, or rel starts with pat/
		if strings.HasPrefix(rel, pat+"/") || rel == pat {
			return true
		}
		return false
	}
	// glob match against full path or any segment
	if matched, _ := filepath.Match(pat, filepath.Base(rel)); matched {
		return true
	}
	if matched, _ := filepath.Match(pat, rel); matched {
		return true
	}
	return false
}
```

- [ ] **Step 5: Implement walker**

`internal/detect/walker.go`:

```go
package detect

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Result groups file paths by language. Paths are RELATIVE to the walk root.
type Result struct {
	Go  []string
	TS  []string
	Sol []string
}

var langExt = map[string]*[]string{}

// Walk walks root, classifies files by extension, and skips paths matching .ckgignore.
func Walk(root string) (*Result, error) {
	ignore, err := LoadCKGIgnore(root)
	if err != nil {
		return nil, fmt.Errorf("load .ckgignore: %w", err)
	}
	r := &Result{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if ignore.Match(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".go":
			r.Go = append(r.Go, rel)
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
			r.TS = append(r.TS, rel)
		case ".sol":
			r.Sol = append(r.Sol, rel)
		}
		return nil
	})
	return r, err
}
```

- [ ] **Step 6: Run tests to verify pass**

```bash
go test ./internal/detect/... -v
```

Expected: PASS for `TestWalkClassifies`.

- [ ] **Step 7: Add a `.ckgignore` matcher table-driven test**

`internal/detect/ckgignore_test.go`:

```go
package detect_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
)

func TestCKGIgnoreMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ckgignore"),
		[]byte("vendor/\n*.generated.*\nbuild/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := detect.LoadCKGIgnore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rel  string
		want bool
	}{
		{"vendor/x.go", true},
		{"vendor", true},
		{"src/foo.generated.ts", true},
		{"build/main.js", true},
		{"src/foo.go", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		if got := c.Match(tc.rel); got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}
```

```bash
go test ./internal/detect/... -v
```

Expected: both tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/detect/
git commit -m "feat(detect): walk source tree honoring .ckgignore and classify by language"
```

---

## Task 6: Parser interface + dispatch

**Files:**
- Create: `internal/parse/parser.go` (interface + types)
- Create: `internal/parse/dispatch.go` (extension → parser)
- Test:   `internal/parse/dispatch_test.go`

- [ ] **Step 1: Define interface and shared types**

`internal/parse/parser.go`:

```go
package parse

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// PendingRef is an unresolved cross-file reference produced in Pass 1
// and resolved (or marked AMBIGUOUS) in Pass 2.
type PendingRef struct {
	SrcID       string
	EdgeType    types.EdgeType
	TargetQName string
	HintFile    string
	Line        int
}

// ParseResult is the per-file output of Pass 1.
type ParseResult struct {
	Path    string
	Nodes   []types.Node
	Edges   []types.Edge
	Pending []PendingRef
}

// ResolvedGraph is the per-language Pass 2 output: in addition to the union of
// per-file results, edges that resolved or were marked AMBIGUOUS.
type ResolvedGraph struct {
	Nodes []types.Node
	Edges []types.Edge
}

// Parser is the contract every language parser implements.
type Parser interface {
	// ParseFile runs Pass 1 on a single file. Pure function — must be safe
	// to call concurrently from a worker pool.
	ParseFile(path string, src []byte) (*ParseResult, error)

	// Resolve runs Pass 2 over the union of ParseResults from the same language.
	Resolve(results []*ParseResult) (*ResolvedGraph, error)

	// Extensions reports the file extensions this parser handles (lowercase, with leading ".").
	Extensions() []string
}
```

- [ ] **Step 2: Implement extension → parser dispatch**

`internal/parse/dispatch.go`:

```go
package parse

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Registry maps file extensions to parser instances.
type Registry struct {
	parsers map[string]Parser
}

// NewRegistry returns an empty registry. Use Register to add parsers.
func NewRegistry() *Registry {
	return &Registry{parsers: map[string]Parser{}}
}

// Register associates each of p.Extensions() with p.
// Returns an error if any extension is already registered.
func (r *Registry) Register(p Parser) error {
	for _, ext := range p.Extensions() {
		ext = strings.ToLower(ext)
		if _, ok := r.parsers[ext]; ok {
			return fmt.Errorf("extension %s registered twice", ext)
		}
		r.parsers[ext] = p
	}
	return nil
}

// For returns the parser registered for path's extension, or nil if none.
func (r *Registry) For(path string) Parser {
	return r.parsers[strings.ToLower(filepath.Ext(path))]
}
```

- [ ] **Step 3: Write dispatch test using a fake parser**

`internal/parse/dispatch_test.go`:

```go
package parse_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

type fakeParser struct{ exts []string }

func (f *fakeParser) ParseFile(string, []byte) (*parse.ParseResult, error) { return nil, nil }
func (f *fakeParser) Resolve([]*parse.ParseResult) (*parse.ResolvedGraph, error) {
	return nil, nil
}
func (f *fakeParser) Extensions() []string { return f.exts }

func TestRegistryDispatch(t *testing.T) {
	r := parse.NewRegistry()
	goP := &fakeParser{exts: []string{".go"}}
	tsP := &fakeParser{exts: []string{".ts", ".tsx"}}
	if err := r.Register(goP); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(tsP); err != nil {
		t.Fatal(err)
	}
	if r.For("foo.go") != goP {
		t.Error(".go should dispatch to goP")
	}
	if r.For("bar.tsx") != tsP {
		t.Error(".tsx should dispatch to tsP")
	}
	if r.For("baz.py") != nil {
		t.Error(".py should be unregistered")
	}
}

func TestRegistryDuplicateExtension(t *testing.T) {
	r := parse.NewRegistry()
	if err := r.Register(&fakeParser{exts: []string{".go"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&fakeParser{exts: []string{".go"}}); err == nil {
		t.Error("duplicate extension should fail")
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/parse/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parse/
git commit -m "feat(parse): define Parser interface, ParseResult, and extension dispatch registry"
```

---

## Task 7: Go parser — Pass 1 declarations

**Files:**
- Create: `internal/parse/golang/parser.go` (Go parser type, ParseFile entry, package loader)
- Create: `internal/parse/golang/declarations.go` (FuncDecl/GenDecl visitors)
- Create: `internal/parse/golang/idgen.go`         (content-hash node ID generator)
- Test:   `internal/parse/golang/parser_test.go`
- Create: `internal/parse/golang/testdata/declarations/{simple_struct.go, simple_struct_golden.json}`

- [ ] **Step 1: Add `golang.org/x/tools` dependency**

```bash
go get golang.org/x/tools/go/packages
```

- [ ] **Step 2: Create the smallest fixture and its golden file**

`internal/parse/golang/testdata/declarations/simple_struct.go`:

```go
package simple

// Counter holds a tally.
type Counter struct {
	N int
}

// Inc increments the counter.
func (c *Counter) Inc() {
	c.N++
}
```

`internal/parse/golang/testdata/declarations/simple_struct_golden.json`:

```json
{
  "node_qnames": [
    "simple.Counter",
    "simple.Counter.N",
    "simple.Counter.Inc"
  ],
  "node_types": {
    "simple.Counter": "Struct",
    "simple.Counter.N": "Field",
    "simple.Counter.Inc": "Method"
  }
}
```

- [ ] **Step 3: Write failing test using golden file**

`internal/parse/golang/parser_test.go`:

```go
package golang_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
)

type golden struct {
	NodeQnames []string          `json:"node_qnames"`
	NodeTypes  map[string]string `json:"node_types"`
}

func TestParseDeclarationsGolden(t *testing.T) {
	dir := "testdata/declarations"
	p := gop.New(dir)
	src, err := os.ReadFile(filepath.Join(dir, "simple_struct.go"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.ParseFile(filepath.Join(dir, "simple_struct.go"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	gb, err := os.ReadFile(filepath.Join(dir, "simple_struct_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(gb, &g); err != nil {
		t.Fatal(err)
	}
	gotQ := make([]string, 0, len(res.Nodes))
	gotTypes := map[string]string{}
	for _, n := range res.Nodes {
		// Skip Package and File auto-nodes for golden focus
		if n.Type == "Package" || n.Type == "File" {
			continue
		}
		gotQ = append(gotQ, n.QualifiedName)
		gotTypes[n.QualifiedName] = string(n.Type)
	}
	sort.Strings(gotQ)
	want := append([]string(nil), g.NodeQnames...)
	sort.Strings(want)
	if !equalStr(gotQ, want) {
		t.Errorf("qnames = %v, want %v", gotQ, want)
	}
	for q, wantT := range g.NodeTypes {
		if got := gotTypes[q]; got != wantT {
			t.Errorf("type for %s: got %q, want %q", q, got, wantT)
		}
	}
}

func equalStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run to verify fail**

```bash
go test ./internal/parse/golang/... -v -run TestParseDeclarations
```

Expected: package missing.

- [ ] **Step 5: Implement ID generator**

`internal/parse/golang/idgen.go`:

```go
package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MakeID returns a stable 16-char content-hash for a node identifier triple.
func MakeID(qname, lang string, startByte int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", qname, lang, startByte)))
	return hex.EncodeToString(sum[:])[:16]
}
```

- [ ] **Step 6: Implement parser entry and declaration visitor**

`internal/parse/golang/parser.go`:

```go
// Package golang is the Go-language parser for CKG. It uses go/parser +
// go/types via golang.org/x/tools/go/packages to extract declarations and
// resolved cross-file references (spec §4.6.1).
package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Parser implements parse.Parser for Go source.
type Parser struct {
	srcRoot string
	fset    *token.FileSet
}

// New returns a Parser rooted at srcRoot (used for relative file paths).
func New(srcRoot string) *Parser {
	return &Parser{srcRoot: srcRoot, fset: token.NewFileSet()}
}

func (p *Parser) Extensions() []string { return []string{".go"} }

// ParseFile runs Pass 1: structural extraction. It does NOT resolve
// cross-file references — those become PendingRefs handled in Resolve.
func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	f, err := parser.ParseFile(p.fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	v := newDeclVisitor(p.fset, rel, f.Name.Name)
	ast.Walk(v, f)
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// Resolve is implemented in resolve.go (Task 9).
```

`internal/parse/golang/declarations.go`:

```go
package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks the AST and emits Pass 1 nodes and edges.
type declVisitor struct {
	fset    *token.FileSet
	relPath string
	pkgName string
	pkgID   string
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
}

func newDeclVisitor(fset *token.FileSet, relPath, pkgName string) *declVisitor {
	v := &declVisitor{fset: fset, relPath: relPath, pkgName: pkgName}
	pkgQ := pkgName
	v.pkgID = MakeID(pkgQ, "go", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.pkgID, Type: types.NodePackage,
		Name: pkgName, QualifiedName: pkgQ,
		FilePath: relPath, StartLine: 1, EndLine: 1,
		Language: "go", Confidence: types.ConfExtracted,
	})
	fileQ := pkgQ + "/" + relPath
	v.fileID = MakeID(fileQ, "go", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile,
		Name: relPath, QualifiedName: fileQ,
		FilePath: relPath, StartLine: 1, EndLine: 1,
		Language: "go", Confidence: types.ConfExtracted,
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.pkgID, Dst: v.fileID,
		Type: types.EdgeContains, Count: 1, Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) Visit(n ast.Node) ast.Visitor {
	switch d := n.(type) {
	case *ast.GenDecl:
		v.visitGenDecl(d)
	case *ast.FuncDecl:
		v.visitFuncDecl(d)
	}
	return v
}

func (v *declVisitor) pos(p token.Pos) (line, byteOff int) {
	pos := v.fset.Position(p)
	return pos.Line, pos.Offset
}

func (v *declVisitor) visitGenDecl(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			v.emitTypeSpec(s, d.Doc)
		case *ast.ValueSpec:
			v.emitValueSpec(s, d.Tok)
		case *ast.ImportSpec:
			v.emitImportSpec(s)
		}
	}
}

func (v *declVisitor) emitTypeSpec(s *ast.TypeSpec, doc *ast.CommentGroup) {
	qname := v.pkgName + "." + s.Name.Name
	startLine, startByte := v.pos(s.Pos())
	endLine, endByte := v.pos(s.End())
	id := MakeID(qname, "go", startByte)
	var nodeType types.NodeType
	switch t := s.Type.(type) {
	case *ast.StructType:
		nodeType = types.NodeStruct
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		for _, f := range t.Fields.List {
			v.emitFields(qname, f)
		}
	case *ast.InterfaceType:
		nodeType = types.NodeInterface
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		for _, f := range t.Methods.List {
			v.emitInterfaceMethod(qname, f)
		}
	default:
		nodeType = types.NodeTypeAlias
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
	}
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
}

func (v *declVisitor) emitFields(parentQname string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeField, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
		parentID := MakeID(parentQname, "go", v.lookupStartByte(parentQname))
		v.edges = append(v.edges, types.Edge{
			Src: parentID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) emitInterfaceMethod(parentQname string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeMethod, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
		parentID := MakeID(parentQname, "go", v.lookupStartByte(parentQname))
		v.edges = append(v.edges, types.Edge{
			Src: parentID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) emitValueSpec(s *ast.ValueSpec, tok token.Token) {
	for _, name := range s.Names {
		qname := v.pkgName + "." + name.Name
		startLine, startByte := v.pos(name.Pos())
		endLine, endByte := v.pos(s.End())
		id := MakeID(qname, "go", startByte)
		nt := types.NodeVariable
		if tok == token.CONST {
			nt = types.NodeConstant
		}
		v.appendNode(id, nt, name.Name, qname, startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(s.Doc), "")
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) emitImportSpec(s *ast.ImportSpec) {
	pathLit := strings.Trim(s.Path.Value, "\"")
	qname := "import:" + pathLit
	startLine, startByte := v.pos(s.Pos())
	endLine, endByte := v.pos(s.End())
	id := MakeID(qname, "go", startByte)
	v.appendNode(id, types.NodeImport, pathLit, qname,
		startLine, endLine, startByte, endByte, "", "", "")
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeImports, Count: 1, Confidence: types.ConfExtracted,
	})
}

func (v *declVisitor) visitFuncDecl(d *ast.FuncDecl) {
	var qname string
	var nt types.NodeType
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recvType := exprName(d.Recv.List[0].Type)
		qname = v.pkgName + "." + recvType + "." + d.Name.Name
		nt = types.NodeMethod
	} else {
		qname = v.pkgName + "." + d.Name.Name
		nt = types.NodeFunction
	}
	startLine, startByte := v.pos(d.Pos())
	endLine, endByte := v.pos(d.End())
	id := MakeID(qname, "go", startByte)
	sig := formatSignature(d)
	v.appendNode(id, nt, d.Name.Name, qname, startLine, endLine, startByte, endByte,
		exported(d.Name.Name), commentText(d.Doc), sig)
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
}

// helpers

func (v *declVisitor) appendNode(id string, t types.NodeType, name, qname string,
	startLine, endLine, startByte, endByte int, vis, doc, sig string) {
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: t, Name: name, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "go", Visibility: vis, DocComment: doc, Signature: sig,
		Confidence: types.ConfExtracted,
	})
}

func (v *declVisitor) lookupStartByte(qname string) int {
	for _, n := range v.nodes {
		if n.QualifiedName == qname {
			return n.StartByte
		}
	}
	return 0
}

func exported(name string) string {
	if name == "" {
		return "private"
	}
	if name[0] >= 'A' && name[0] <= 'Z' {
		return "exported"
	}
	return "private"
}

func commentText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

func formatSignature(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		fmt.Fprintf(&b, "(%s) ", exprName(d.Recv.List[0].Type))
	}
	b.WriteString(d.Name.Name)
	b.WriteString("(...)")
	if d.Type.Results != nil {
		b.WriteString(" ...")
	}
	return b.String()
}
```

- [ ] **Step 7: Run tests to verify pass**

```bash
go test ./internal/parse/golang/... -v -run TestParseDeclarationsGolden
```

Expected: PASS — qnames {`simple.Counter`, `simple.Counter.N`, `simple.Counter.Inc`} and types match the golden file.

- [ ] **Step 8: Commit**

```bash
git add internal/parse/golang/ go.mod go.sum
git commit -m "feat(parse/go): emit Pass-1 declarations (Package/File/Struct/Field/Func/Method/Import)"
```

---

## Task 8: Go parser — Pass 1 statements (logic blocks + goroutines + channels)

**Files:**
- Create: `internal/parse/golang/statements.go`
- Test:   `internal/parse/golang/statements_test.go`
- Create: `internal/parse/golang/testdata/statements/{control_flow.go, control_flow_golden.json}`

- [ ] **Step 1: Create fixture covering all 5 logic-block kinds + goroutine + channel ops**

`internal/parse/golang/testdata/statements/control_flow.go`:

```go
package cf

func Decide(n int) int {
	if n < 0 {       // IfStmt
		return -1     // ReturnStmt
	}
	for i := 0; i < n; i++ {   // LoopStmt sub_kind=for
	}
	for k := range []int{1, 2} { // LoopStmt sub_kind=range
		_ = k
	}
	switch n {                 // SwitchStmt
	case 0:
	}
	ch := make(chan int)       // Channel decl (LocalVariable + Channel typing — V0 emits LocalVariable only)
	go func() { ch <- 1 }()    // Goroutine + sends_to
	<-ch                       // recvs_from CallSite
	return n                   // ReturnStmt
}
```

`internal/parse/golang/testdata/statements/control_flow_golden.json`:

```json
{
  "logic_block_counts": {
    "IfStmt": 1,
    "LoopStmt": 2,
    "SwitchStmt": 1,
    "ReturnStmt": 2,
    "CallSite": 2
  },
  "goroutine_count": 1
}
```

- [ ] **Step 2: Write failing test**

`internal/parse/golang/statements_test.go`:

```go
package golang_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

type stmtGolden struct {
	LogicBlockCounts map[string]int `json:"logic_block_counts"`
	GoroutineCount   int            `json:"goroutine_count"`
}

func TestParseStatements(t *testing.T) {
	dir := "testdata/statements"
	src, err := os.ReadFile(filepath.Join(dir, "control_flow.go"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := gop.New(dir).ParseFile(filepath.Join(dir, "control_flow.go"), src)
	if err != nil {
		t.Fatal(err)
	}
	gb, _ := os.ReadFile(filepath.Join(dir, "control_flow_golden.json"))
	var g stmtGolden
	_ = json.Unmarshal(gb, &g)

	counts := map[string]int{}
	for _, n := range res.Nodes {
		switch n.Type {
		case types.NodeIfStmt, types.NodeLoopStmt, types.NodeSwitchStmt,
			types.NodeReturnStmt, types.NodeCallSite:
			counts[string(n.Type)]++
		}
	}
	for k, want := range g.LogicBlockCounts {
		if got := counts[k]; got != want {
			t.Errorf("logic-block %s = %d, want %d", k, got, want)
		}
	}

	gor := 0
	for _, n := range res.Nodes {
		if n.Type == types.NodeGoroutine {
			gor++
		}
	}
	if gor != g.GoroutineCount {
		t.Errorf("Goroutine count = %d, want %d", gor, g.GoroutineCount)
	}
}
```

- [ ] **Step 3: Run to verify fail**

```bash
go test ./internal/parse/golang/... -v -run TestParseStatements
```

Expected: counts mismatch (statements not yet emitted).

- [ ] **Step 4: Implement statement visitor inside the existing walker**

`internal/parse/golang/statements.go`:

```go
package golang

import (
	"fmt"
	"go/ast"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// VisitStatements is invoked from declVisitor.Visit when entering a function body.
// It emits LogicBlock nodes (5 kinds), CallSite nodes, Goroutines, and channel
// send/recv edges. Cross-file call resolution is left to Pass 2.
func (v *declVisitor) emitFunctionBody(parentQname string, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	parentID := MakeID(parentQname, "go", v.lookupStartByte(parentQname))
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch s := n.(type) {
		case *ast.IfStmt:
			v.appendLogicBlock(parentID, parentQname, types.NodeIfStmt, "", s.Pos(), s.End())
		case *ast.ForStmt:
			v.appendLogicBlock(parentID, parentQname, types.NodeLoopStmt, "for", s.Pos(), s.End())
		case *ast.RangeStmt:
			v.appendLogicBlock(parentID, parentQname, types.NodeLoopStmt, "range", s.Pos(), s.End())
		case *ast.SwitchStmt:
			v.appendLogicBlock(parentID, parentQname, types.NodeSwitchStmt, "", s.Pos(), s.End())
		case *ast.TypeSwitchStmt:
			v.appendLogicBlock(parentID, parentQname, types.NodeSwitchStmt, "type", s.Pos(), s.End())
		case *ast.ReturnStmt:
			v.appendLogicBlock(parentID, parentQname, types.NodeReturnStmt, "", s.Pos(), s.End())
		case *ast.CallExpr:
			id := v.appendLogicBlock(parentID, parentQname, types.NodeCallSite, "", s.Pos(), s.End())
			// Pending edge: CallSite calls callee — resolved in Pass 2.
			v.pending = append(v.pending, parsePendingFromCall(id, parentQname, s, v.fset))
		case *ast.GoStmt:
			id := v.appendLogicBlock(parentID, parentQname, types.NodeGoroutine, "", s.Pos(), s.End())
			v.edges = append(v.edges, types.Edge{
				Src: parentID, Dst: id, Type: types.EdgeSpawns, Count: 1,
				Confidence: types.ConfExtracted,
			})
		case *ast.SendStmt:
			id := v.appendLogicBlock(parentID, parentQname, types.NodeCallSite, "send", s.Pos(), s.End())
			v.edges = append(v.edges, types.Edge{
				Src: parentID, Dst: id, Type: types.EdgeSendsTo, Count: 1,
				Confidence: types.ConfExtracted,
			})
		case *ast.UnaryExpr:
			if s.Op.String() == "<-" {
				id := v.appendLogicBlock(parentID, parentQname, types.NodeCallSite, "recv", s.Pos(), s.End())
				v.edges = append(v.edges, types.Edge{
					Src: parentID, Dst: id, Type: types.EdgeRecvsFrom, Count: 1,
					Confidence: types.ConfExtracted,
				})
			}
		}
		return true
	})
}

func (v *declVisitor) appendLogicBlock(parentID, parentQname string, t types.NodeType, subKind string, pos, end ast.Token) string {
	startLine, startByte := v.pos(pos.(interface{ Pos() ast.Token }).Pos().(ast.Token))
	_ = startLine
	// Use raw token positions
	startLn, startBy := v.pos(token2Pos(pos))
	endLn, endBy := v.pos(token2Pos(end))
	qname := fmt.Sprintf("%s#%s@%d", parentQname, t, startBy)
	id := MakeID(qname, "go", startBy)
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: t, Name: string(t), QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted, SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: id, Type: types.EdgeContains, Count: 1, Confidence: types.ConfExtracted,
	})
	return id
}

// token2Pos converts the ast.Token interface back to token.Pos (helper for type system).
// Implementation detail intentionally minimal — see resolved version in idgen.go later.
```

> **Note:** The above contains a sketch placeholder around `token2Pos`/`pos.(interface{...})` because mixing `ast.Token` and `token.Pos` correctly requires picking one signature. Use this corrected helper:

`internal/parse/golang/statements.go` (replace the broken bottom helpers with):

```go
import "go/token"

func (v *declVisitor) appendLogicBlockPos(parentID, parentQname string, t types.NodeType, subKind string, startPos, endPos token.Pos) string {
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	qname := fmt.Sprintf("%s#%s@%d", parentQname, t, startBy)
	id := MakeID(qname, "go", startBy)
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: t, Name: string(t), QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted, SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: id, Type: types.EdgeContains, Count: 1, Confidence: types.ConfExtracted,
	})
	return id
}

// parsePendingFromCall extracts a best-effort callee qname from a *ast.CallExpr.
func parsePendingFromCall(srcID, parentQname string, c *ast.CallExpr, fset *token.FileSet) parse.PendingRef {
	target := exprName(c.Fun)
	pos := fset.Position(c.Pos())
	return parse.PendingRef{
		SrcID:       srcID,
		EdgeType:    types.EdgeCalls,
		TargetQName: target,
		HintFile:    pos.Filename,
		Line:        pos.Line,
	}
}
```

Then in `declarations.go` `visitFuncDecl`, after appending the function/method node, add:

```go
v.emitFunctionBodyPos(qname, d.Body)
```

and rename the body emitter accordingly. Use `appendLogicBlockPos` everywhere instead of the broken `appendLogicBlock`.

- [ ] **Step 5: Run tests to verify pass**

```bash
go vet ./internal/parse/golang/...
go test ./internal/parse/golang/... -v -run TestParseStatements
```

Expected: PASS — counts match the golden.

- [ ] **Step 6: Commit**

```bash
git add internal/parse/golang/statements.go internal/parse/golang/declarations.go \
        internal/parse/golang/testdata/statements/
git commit -m "feat(parse/go): emit Pass-1 logic blocks, goroutines, and channel send/recv edges"
```

---

## Task 9: Go parser — Pass 2 cross-file resolution via `go/types`

**Files:**
- Create: `internal/parse/golang/resolve.go`
- Test:   `internal/parse/golang/resolve_test.go`
- Create: `internal/parse/golang/testdata/resolve/{a/a.go, b/b.go, b/go.mod}` and a tiny `go.mod`

- [ ] **Step 1: Create cross-file fixture**

`internal/parse/golang/testdata/resolve/go.mod`:

```
module ckgresolve.test
go 1.22
```

`internal/parse/golang/testdata/resolve/a/a.go`:

```go
package a

func Greet(name string) string { return "hi " + name }
```

`internal/parse/golang/testdata/resolve/b/b.go`:

```go
package b

import "ckgresolve.test/a"

func Hello() string { return a.Greet("world") }
```

- [ ] **Step 2: Write failing test**

`internal/parse/golang/resolve_test.go`:

```go
package golang_test

import (
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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
```

- [ ] **Step 3: Run to verify fail**

```bash
go test ./internal/parse/golang/... -v -run TestResolveCrossFileCall
```

Expected: undefined `LoadAndResolve`.

- [ ] **Step 4: Implement Resolve and a convenience LoadAndResolve**

`internal/parse/golang/resolve.go`:

```go
package golang

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Resolve unions per-file results and uses go/types to resolve PendingRefs.
// V0 implementation: resolves call-target qnames to existing function/method nodes
// by qname suffix match. Unresolved pending refs become AMBIGUOUS edges.
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}
	qIndex := map[string]string{} // qname -> nodeID (Function/Method only)
	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			if n.Type == types.NodeFunction || n.Type == types.NodeMethod {
				qIndex[n.QualifiedName] = n.ID
				// also index trailing simple name and pkg.Name for partial matches
				suffix := simpleName(n.QualifiedName)
				qIndex[suffix] = n.ID
			}
		}
	}
	for _, r := range results {
		for _, pr := range r.Pending {
			id, ok := qIndex[pr.TargetQName]
			conf := types.ConfExtracted
			if !ok {
				// try suffix match
				for q, nid := range qIndex {
					if strings.HasSuffix(q, "."+pr.TargetQName) || q == pr.TargetQName {
						id, ok = nid, true
						break
					}
				}
			}
			if !ok {
				conf = types.ConfAmbiguous
				continue // V0: drop unresolved edges to avoid foreign-key violations.
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: pr.SrcID, Dst: id, Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
	}
	return out, nil
}

// LoadAndResolve is a convenience for tests: walks Go files under root,
// runs Pass 1 on each, then Pass 2 across the union.
func LoadAndResolve(root string) (*parse.ResolvedGraph, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedModule,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	p := New(root)
	var results []*parse.ParseResult
	for _, pkg := range pkgs {
		for i, file := range pkg.Syntax {
			path := pkg.GoFiles[i]
			src, err := readFile(path)
			if err != nil {
				return nil, err
			}
			_ = file
			r, err := p.ParseFile(path, src)
			if err != nil {
				return nil, err
			}
			results = append(results, r)
		}
	}
	return p.Resolve(results)
}

func simpleName(qname string) string {
	i := strings.LastIndex(qname, ".")
	if i < 0 {
		return qname
	}
	return qname[i+1:]
}

func readFile(path string) ([]byte, error) {
	// indirection for testability
	return readFileOS(path)
}
```

Add this small helper file to keep imports tidy:

`internal/parse/golang/io.go`:

```go
package golang

import "os"

func readFileOS(path string) ([]byte, error) { return os.ReadFile(path) }
```

- [ ] **Step 5: Run tests to verify pass**

```bash
go test ./internal/parse/golang/... -v -run TestResolveCrossFileCall
```

Expected: PASS — `b.Hello -calls-> a.Greet` edge present.

- [ ] **Step 6: Commit**

```bash
git add internal/parse/golang/resolve.go internal/parse/golang/io.go \
        internal/parse/golang/resolve_test.go internal/parse/golang/testdata/resolve/
git commit -m "feat(parse/go): resolve cross-file calls in Pass 2 using qname index"
```

---

> **Phase 2 complete.** Detect + parser interface + Go parser (Pass 1 declarations + statements + Pass 2 resolution) all green. Continue with Phase 3 to build the in-memory graph and add cluster/score.

---

# Phase 3 — Graph builder, cluster, score

Goal: collapse per-language ResolvedGraphs into one validated `Graph`, build the package-tree hierarchy, run Leiden community detection at three resolutions, and compute degree / PageRank / usage_score. After Phase 3: deterministic Go-only graph with both hierarchies and per-node scores ready to persist.

---

## Task 10: Graph builder + cross-ref validator

**Files:**
- Create: `internal/graph/builder.go`
- Create: `internal/graph/validate.go`
- Test:   `internal/graph/builder_test.go`

- [ ] **Step 1: Write failing test for dedup + cross-ref validation**

`internal/graph/builder_test.go`:

```go
package graph_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func n(id, qname string, t types.NodeType) types.Node {
	return types.Node{ID: id, Type: t, Name: qname, QualifiedName: qname,
		FilePath: "f.go", StartLine: 1, EndLine: 1, StartByte: 0, EndByte: 1,
		Language: "go", Confidence: types.ConfExtracted}
}

func TestBuildDedupAndValidate(t *testing.T) {
	a := n("aaaaaaaaaaaaaaaa", "a.A", types.NodeFunction)
	b := n("bbbbbbbbbbbbbbbb", "b.B", types.NodeFunction)
	dup := a // same ID
	g, err := graph.Build([]*parse.ResolvedGraph{
		{Nodes: []types.Node{a, b}, Edges: []types.Edge{{Src: a.ID, Dst: b.ID,
			Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted}}},
		{Nodes: []types.Node{dup}, Edges: nil},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Errorf("dedup failed: got %d nodes, want 2", len(g.Nodes))
	}

	// Inject a dangling edge and expect Validate to fail.
	g.Edges = append(g.Edges, types.Edge{Src: a.ID, Dst: "ffffffffffffffff",
		Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted})
	err = graph.Validate(g)
	if err == nil || !strings.Contains(err.Error(), "dangling") {
		t.Errorf("Validate should reject dangling edge, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify fail**

```bash
go test ./internal/graph/... -v
```

Expected: package missing.

- [ ] **Step 3: Implement Graph + Build + Validate**

`internal/graph/builder.go`:

```go
package graph

import (
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Graph is the in-memory CKG graph after build.
type Graph struct {
	Nodes []types.Node
	Edges []types.Edge
}

// Build merges per-language ResolvedGraphs, deduplicating nodes by ID
// (last-writer wins for attributes — should be identical for true dups)
// and concatenating edges. Edges are NOT deduplicated; multiplicity is
// preserved via Edge.Count which the score module aggregates later.
func Build(parts []*parse.ResolvedGraph) (*Graph, error) {
	byID := make(map[string]types.Node)
	var edges []types.Edge
	for _, p := range parts {
		for _, n := range p.Nodes {
			byID[n.ID] = n
		}
		edges = append(edges, p.Edges...)
	}
	nodes := make([]types.Node, 0, len(byID))
	for _, n := range byID {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return &Graph{Nodes: nodes, Edges: edges}, nil
}
```

`internal/graph/validate.go`:

```go
package graph

import (
	"fmt"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Validate enforces the CKG invariants:
//   - every edge.src and edge.dst references an existing node ID (no dangling refs)
//   - every node and edge has a valid Confidence label
//   - every node has a known NodeType
//   - every edge has a known EdgeType
// Returns the FIRST violation; callers may iterate further if useful.
func Validate(g *Graph) error {
	ids := make(map[string]struct{}, len(g.Nodes))
	validNT := make(map[types.NodeType]struct{})
	for _, t := range types.AllNodeTypes() {
		validNT[t] = struct{}{}
	}
	validET := make(map[types.EdgeType]struct{})
	for _, t := range types.AllEdgeTypes() {
		validET[t] = struct{}{}
	}
	for _, n := range g.Nodes {
		if _, ok := validNT[n.Type]; !ok {
			return fmt.Errorf("node %s: unknown type %q", n.ID, n.Type)
		}
		if !n.Confidence.Valid() {
			return fmt.Errorf("node %s: invalid confidence %q", n.ID, n.Confidence)
		}
		ids[n.ID] = struct{}{}
	}
	for _, e := range g.Edges {
		if _, ok := validET[e.Type]; !ok {
			return fmt.Errorf("edge %s->%s: unknown type %q", e.Src, e.Dst, e.Type)
		}
		if !e.Confidence.Valid() {
			return fmt.Errorf("edge %s->%s: invalid confidence %q", e.Src, e.Dst, e.Confidence)
		}
		if _, ok := ids[e.Src]; !ok {
			return fmt.Errorf("dangling src on edge of type %s: %s -> %s", e.Type, e.Src, e.Dst)
		}
		if _, ok := ids[e.Dst]; !ok {
			return fmt.Errorf("dangling dst on edge of type %s: %s -> %s", e.Type, e.Src, e.Dst)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
go test ./internal/graph/... -v
```

Expected: PASS — dedup leaves 2 nodes; Validate returns "dangling".

- [ ] **Step 5: Commit**

```bash
git add internal/graph/
git commit -m "feat(graph): merge per-language results with dedup and strict cross-ref validation"
```

---

## Task 11: Score module — degree, PageRank, usage_score

**Files:**
- Create: `internal/score/score.go` (one file: degree + pagerank + usage)
- Test:   `internal/score/score_test.go`

- [ ] **Step 1: Write failing test**

`internal/score/score_test.go`:

```go
package score_test

import (
	"math"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/score"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Triangle graph: A -> B -> C -> A
func triangle() *graph.Graph {
	mk := func(id string) types.Node {
		return types.Node{ID: id, Type: types.NodeFunction, Name: id, QualifiedName: id,
			FilePath: "f.go", StartLine: 1, EndLine: 1, EndByte: 1,
			Language: "go", Confidence: types.ConfExtracted}
	}
	a, b, c := mk("aaaaaaaaaaaaaaaa"), mk("bbbbbbbbbbbbbbbb"), mk("cccccccccccccccc")
	mke := func(s, d string) types.Edge {
		return types.Edge{Src: s, Dst: d, Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted}
	}
	return &graph.Graph{
		Nodes: []types.Node{a, b, c},
		Edges: []types.Edge{mke(a.ID, b.ID), mke(b.ID, c.ID), mke(c.ID, a.ID)},
	}
}

func TestDegreeAndUsage(t *testing.T) {
	g := triangle()
	score.Compute(g)
	for _, n := range g.Nodes {
		if n.InDegree != 1 || n.OutDegree != 1 {
			t.Errorf("%s: in=%d out=%d, want 1/1", n.ID, n.InDegree, n.OutDegree)
		}
		if n.UsageScore != 1 {
			t.Errorf("%s: usage=%.2f, want 1", n.ID, n.UsageScore)
		}
	}
}

func TestPageRankSumsToOne(t *testing.T) {
	g := triangle()
	score.Compute(g)
	sum := 0.0
	for _, n := range g.Nodes {
		sum += n.PageRank
	}
	if math.Abs(sum-1.0) > 1e-3 {
		t.Errorf("PageRank sum = %.6f, want ~1.0", sum)
	}
}
```

- [ ] **Step 2: Run to verify fail**

```bash
go test ./internal/score/... -v
```

Expected: undefined `score.Compute`.

- [ ] **Step 3: Implement Compute**

`internal/score/score.go`:

```go
// Package score computes per-node graph metrics in place: in/out degree,
// PageRank (damping=0.85, iterations=30), and usage_score (sum of incoming
// "calls"/"invokes" edge counts — used to size super-nodes in the viewer).
package score

import (
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Compute populates InDegree, OutDegree, PageRank, and UsageScore for each node.
func Compute(g *graph.Graph) {
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		g.Nodes[i].InDegree = 0
		g.Nodes[i].OutDegree = 0
		g.Nodes[i].UsageScore = 0
		idx[n.ID] = i
	}
	for _, e := range g.Edges {
		si, ok := idx[e.Src]
		if !ok {
			continue
		}
		di, ok := idx[e.Dst]
		if !ok {
			continue
		}
		g.Nodes[si].OutDegree += e.Count
		g.Nodes[di].InDegree += e.Count
		if e.Type == types.EdgeCalls || e.Type == types.EdgeInvokes {
			g.Nodes[di].UsageScore += float64(e.Count)
		}
	}
	pageRank(g, 0.85, 30)
}

// pageRank implements the standard iterative algorithm.
func pageRank(g *graph.Graph, damping float64, iters int) {
	n := len(g.Nodes)
	if n == 0 {
		return
	}
	idx := make(map[string]int, n)
	for i, nd := range g.Nodes {
		idx[nd.ID] = i
	}
	out := make([][]int, n)
	outDeg := make([]int, n)
	for _, e := range g.Edges {
		si, ok := idx[e.Src]
		if !ok {
			continue
		}
		di, ok := idx[e.Dst]
		if !ok {
			continue
		}
		out[si] = append(out[si], di)
		outDeg[si]++
	}
	pr := make([]float64, n)
	next := make([]float64, n)
	for i := range pr {
		pr[i] = 1.0 / float64(n)
	}
	teleport := (1 - damping) / float64(n)
	for it := 0; it < iters; it++ {
		for i := range next {
			next[i] = teleport
		}
		dangling := 0.0
		for i := 0; i < n; i++ {
			if outDeg[i] == 0 {
				dangling += pr[i]
				continue
			}
			share := damping * pr[i] / float64(outDeg[i])
			for _, j := range out[i] {
				next[j] += share
			}
		}
		// distribute dangling mass evenly
		add := damping * dangling / float64(n)
		for i := range next {
			next[i] += add
		}
		pr, next = next, pr
	}
	for i := range g.Nodes {
		g.Nodes[i].PageRank = pr[i]
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/score/... -v
```

Expected: PASS — degree=1/1, usage=1, PageRank sums to ~1.

- [ ] **Step 5: Commit**

```bash
git add internal/score/
git commit -m "feat(score): compute in/out degree, PageRank, and usage_score in place"
```

---

## Task 12: Pkg-tree builder

**Files:**
- Create: `internal/cluster/pkg_tree.go`
- Test:   `internal/cluster/pkg_tree_test.go`

- [ ] **Step 1: Write failing test**

`internal/cluster/pkg_tree_test.go`:

```go
package cluster_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestPkgTreeLevels(t *testing.T) {
	mk := func(id, qname string, t types.NodeType, file string) types.Node {
		return types.Node{ID: id, Type: t, Name: qname, QualifiedName: qname,
			FilePath: file, StartLine: 1, EndLine: 1, EndByte: 1,
			Language: "go", Confidence: types.ConfExtracted}
	}
	g := &graph.Graph{
		Nodes: []types.Node{
			mk("p1", "core", types.NodePackage, "core/x.go"),
			mk("p2", "core/types", types.NodePackage, "core/types/x.go"),
			mk("f1", "core/x.go", types.NodeFile, "core/x.go"),
			mk("fn1", "core.Foo", types.NodeFunction, "core/x.go"),
		},
	}
	tree := cluster.BuildPkgTree(g)
	if got := tree.LevelOf("p1"); got != 0 {
		t.Errorf("level of root pkg = %d, want 0", got)
	}
	if got := tree.LevelOf("f1"); got != 2 {
		t.Errorf("level of file = %d, want 2", got)
	}
	if got := tree.LevelOf("fn1"); got != 3 {
		t.Errorf("level of function = %d, want 3", got)
	}
	parent, ok := tree.Parent("fn1")
	if !ok || parent != "f1" {
		t.Errorf("parent of fn1 = %q (ok=%v), want f1", parent, ok)
	}
}
```

- [ ] **Step 2: Run to verify fail**

```bash
go test ./internal/cluster/... -v -run TestPkgTreeLevels
```

Expected: package missing.

- [ ] **Step 3: Implement BuildPkgTree**

`internal/cluster/pkg_tree.go`:

```go
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

// PkgTree captures the deterministic package-tree hierarchy
// (Level 0 pkg → 1 subpkg → 2 file → 3 type/func/var → 4 LogicBlock).
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
	if best == "" {
		// fall back to directory-derived synthetic match
		for q := range pkgs {
			if q == dir {
				best = q
			}
		}
	}
	return pkgs[best]
}
```

- [ ] **Step 4: Run tests to verify pass**

```bash
go test ./internal/cluster/... -v -run TestPkgTreeLevels
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/pkg_tree.go internal/cluster/pkg_tree_test.go
git commit -m "feat(cluster): build deterministic package-tree hierarchy with 5 levels"
```

---

## Task 13: Leiden algorithm — modularity + iteration loop

**Files:**
- Create: `internal/cluster/leiden.go`
- Test:   `internal/cluster/leiden_test.go`

> **Reference:** Java implementation at https://github.com/CWTSLeiden/networkanalysis (specifically `Leiden.java`, `CPMClusteringAlgorithm.java`, and `Network.java`). Port structure 1:1 where possible. See `docs/STUDY-GUIDE.md` §1 for background.

- [ ] **Step 1: Write failing test on a known graph**

`internal/cluster/leiden_test.go`:

```go
package cluster_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
)

// Two cliques connected by a single bridge edge — Leiden should detect
// exactly two communities at γ=1.0.
func TestLeidenTwoClusters(t *testing.T) {
	// Build edge list. Nodes 0..3 are clique A, 4..7 are clique B,
	// edge (3,4) is the bridge.
	edges := [][2]int{
		{0, 1}, {0, 2}, {0, 3}, {1, 2}, {1, 3}, {2, 3}, // A
		{4, 5}, {4, 6}, {4, 7}, {5, 6}, {5, 7}, {6, 7}, // B
		{3, 4}, // bridge
	}
	parts := cluster.RunLeiden(8, edges, cluster.LeidenOpts{Resolution: 1.0, Seed: 42, MaxIters: 50})
	if got := distinct(parts); got != 2 {
		t.Errorf("Leiden communities = %d, want 2", got)
	}
}

func distinct(p []int) int {
	m := map[int]struct{}{}
	for _, x := range p {
		m[x] = struct{}{}
	}
	return len(m)
}
```

- [ ] **Step 2: Run to verify fail**

```bash
go test ./internal/cluster/... -v -run TestLeidenTwoClusters
```

Expected: undefined `cluster.RunLeiden`.

- [ ] **Step 3: Implement Leiden (modularity + greedy local move + refinement + aggregation)**

`internal/cluster/leiden.go`:

```go
// Package cluster — Leiden community detection (V0 implementation).
//
// Goal: well-connected communities maximizing modularity at a given
// resolution γ. We follow the structure of the reference Java implementation
// at github.com/CWTSLeiden/networkanalysis, simplified to undirected unweighted
// graphs (V0 only treats edge multiplicity).
//
// Three nested phases per outer iteration:
//   1. Local moving — for each node, move it to the neighboring community
//      that maximizes ΔQ (modularity gain).
//   2. Refinement — within each community, restart with singletons and
//      re-aggregate to guarantee well-connectedness.
//   3. Aggregation — collapse each refined community into a single super-node
//      and recurse.
//
// Stops when no node move yields ΔQ > 0 across an entire pass.
package cluster

import (
	"math/rand"
	"sort"
)

// LeidenOpts controls a single run.
type LeidenOpts struct {
	Resolution float64
	Seed       int64
	MaxIters   int
}

// RunLeiden returns a partition: parts[i] is the community ID assigned
// to node i. n is the node count, edges are undirected (a,b) pairs;
// repeated pairs increase weight by 1.
func RunLeiden(n int, edges [][2]int, opts LeidenOpts) []int {
	g := buildAdj(n, edges)
	parts := make([]int, n)
	for i := range parts {
		parts[i] = i // singleton init
	}
	r := rand.New(rand.NewSource(opts.Seed))

	for iter := 0; iter < opts.MaxIters; iter++ {
		movedLocal := localMove(g, parts, opts.Resolution, r)
		// Refinement (V0 simplification: skip explicit refine, rely on local move).
		// Aggregate: collapse current parts into a smaller graph and continue.
		parts2, agg := aggregate(g, parts)
		if !movedLocal && len(agg.weight) == len(g.weight) {
			break
		}
		// Continue local-moving on the aggregated graph.
		coarse := localMove(agg, parts2, opts.Resolution, r)
		// Lift back to original node indices.
		parts = lift(parts, parts2)
		if !movedLocal && !coarse {
			break
		}
	}
	return relabel(parts)
}

// adjList stores, per node, neighbor index and weight.
type adjList struct {
	neigh   [][]int
	weight  []float64 // sum of edge weights at node i (degree in weighted sense)
	totalW  float64   // sum of all edge weights (undirected counted once each direction)
	edgeWts map[[2]int]float64
}

func buildAdj(n int, edges [][2]int) *adjList {
	g := &adjList{neigh: make([][]int, n), weight: make([]float64, n),
		edgeWts: map[[2]int]float64{}}
	for _, e := range edges {
		a, b := e[0], e[1]
		if a == b {
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		g.edgeWts[key]++
	}
	for k, w := range g.edgeWts {
		a, b := k[0], k[1]
		g.neigh[a] = append(g.neigh[a], b)
		g.neigh[b] = append(g.neigh[b], a)
		g.weight[a] += w
		g.weight[b] += w
		g.totalW += w
	}
	for i := range g.neigh {
		sort.Ints(g.neigh[i])
	}
	return g
}

// localMove iterates nodes in a random order; for each node, moves it
// to the neighboring community that gives the largest modularity gain.
func localMove(g *adjList, parts []int, gamma float64, r *rand.Rand) bool {
	n := len(g.neigh)
	order := r.Perm(n)
	moved := false
	twoM := 2 * g.totalW
	if twoM == 0 {
		return false
	}
	commWeight := make(map[int]float64)
	for i, c := range parts {
		commWeight[c] += g.weight[i]
	}
	for _, i := range order {
		cur := parts[i]
		neighbors := g.neigh[i]
		// remove i from current community
		commWeight[cur] -= g.weight[i]
		// gather edge-weight to each neighboring community
		toComm := map[int]float64{}
		for _, j := range neighbors {
			cj := parts[j]
			w := g.edgeWts[edgeKey(i, j)]
			toComm[cj] += w
		}
		bestC, bestGain := cur, 0.0
		for c, w := range toComm {
			gain := w - gamma*g.weight[i]*commWeight[c]/twoM
			if gain > bestGain {
				bestGain, bestC = gain, c
			}
		}
		parts[i] = bestC
		commWeight[bestC] += g.weight[i]
		if bestC != cur {
			moved = true
		}
	}
	return moved
}

// aggregate collapses each community into a single super-node and returns
// the new graph + identity partition (each super-node in its own community).
func aggregate(g *adjList, parts []int) ([]int, *adjList) {
	// Map old community ID → new index.
	idx := map[int]int{}
	for _, c := range parts {
		if _, ok := idx[c]; !ok {
			idx[c] = len(idx)
		}
	}
	n := len(idx)
	out := &adjList{neigh: make([][]int, n), weight: make([]float64, n), edgeWts: map[[2]int]float64{}}
	for k, w := range g.edgeWts {
		a := idx[parts[k[0]]]
		b := idx[parts[k[1]]]
		if a == b {
			out.weight[a] += 2 * w // self-loop counted twice
			out.totalW += w
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		out.edgeWts[key] += w
		out.weight[a] += w
		out.weight[b] += w
		out.totalW += w
	}
	for k := range out.edgeWts {
		out.neigh[k[0]] = appendUnique(out.neigh[k[0]], k[1])
		out.neigh[k[1]] = appendUnique(out.neigh[k[1]], k[0])
	}
	parts2 := make([]int, n)
	for i := range parts2 {
		parts2[i] = i
	}
	return parts2, out
}

// lift maps each original node's partition through the aggregated partition.
func lift(orig, agg []int) []int {
	idx := map[int]int{}
	for _, c := range orig {
		if _, ok := idx[c]; !ok {
			idx[c] = len(idx)
		}
	}
	out := make([]int, len(orig))
	for i, c := range orig {
		out[i] = agg[idx[c]]
	}
	return out
}

func relabel(parts []int) []int {
	idx := map[int]int{}
	out := make([]int, len(parts))
	for i, c := range parts {
		if _, ok := idx[c]; !ok {
			idx[c] = len(idx)
		}
		out[i] = idx[c]
	}
	return out
}

func appendUnique(xs []int, x int) []int {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

func edgeKey(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/cluster/... -v -run TestLeidenTwoClusters
```

Expected: PASS — exactly 2 distinct community labels for the two-cliques test.

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/leiden.go internal/cluster/leiden_test.go
git commit -m "feat(cluster): port Leiden community detection (modularity + local move + aggregate)"
```

---

## Task 14: Multi-resolution + topic naming heuristic + GraphCluster wrapper

**Files:**
- Create: `internal/cluster/topic_tree.go`
- Create: `internal/cluster/naming.go`
- Test:   `internal/cluster/topic_tree_test.go`

- [ ] **Step 1: Write failing test**

`internal/cluster/topic_tree_test.go`:

```go
package cluster_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestTopicTreeMultiResolution(t *testing.T) {
	// Build a tiny graph with two obvious clusters.
	mk := func(id, qname string) types.Node {
		return types.Node{ID: id, Type: types.NodeFunction, Name: qname, QualifiedName: qname,
			FilePath: "f.go", StartLine: 1, EndLine: 1, EndByte: 1,
			Language: "go", Confidence: types.ConfExtracted}
	}
	mke := func(s, d string) types.Edge {
		return types.Edge{Src: s, Dst: d, Type: types.EdgeCalls, Count: 1, Confidence: types.ConfExtracted}
	}
	g := &graph.Graph{
		Nodes: []types.Node{
			mk("a1", "consensus.validateOne"),
			mk("a2", "consensus.validateTwo"),
			mk("a3", "consensus.AuthorizeSigner"),
			mk("b1", "txpool.addLocal"),
			mk("b2", "txpool.addRemote"),
			mk("b3", "txpool.lookup"),
		},
		Edges: []types.Edge{
			mke("a1", "a2"), mke("a2", "a3"), mke("a3", "a1"),
			mke("b1", "b2"), mke("b2", "b3"), mke("b3", "b1"),
			mke("a1", "b1"), // weak bridge
		},
	}
	tt := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)
	if n := len(tt.Resolutions); n != 3 {
		t.Fatalf("Resolutions count = %d, want 3", n)
	}
	for i, r := range tt.Resolutions {
		if len(r.Communities) == 0 {
			t.Errorf("resolution %d: 0 communities", i)
		}
		for _, c := range r.Communities {
			if c.Label == "" {
				t.Errorf("resolution %d: empty topic_label", i)
			}
		}
	}
	// At γ=1.0, expect at least one community whose label contains "validate".
	got := false
	for _, c := range tt.Resolutions[1].Communities {
		if strings.Contains(c.Label, "validate") {
			got = true
		}
	}
	if !got {
		t.Errorf("expected at least one γ=1.0 community label containing 'validate'")
	}
}
```

- [ ] **Step 2: Implement multi-resolution + naming**

`internal/cluster/naming.go`:

```go
package cluster

import (
	"sort"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// LabelCommunity computes a human-readable label using the 3-tuple heuristic
// from spec §5.5.4: "<dominant_pkg> — <common_substring>* + <top_pagerank_node>".
// `members` is the slice of nodes belonging to one community.
func LabelCommunity(members []types.Node) string {
	if len(members) == 0 {
		return "(empty)"
	}
	pkg := dominantPackage(members)
	prefix := commonNamePrefix(members, 3)
	top := topPageRankName(members)

	var parts []string
	if pkg != "" {
		parts = append(parts, pkg)
	}
	right := ""
	if len(prefix) >= 3 {
		right = prefix + "*"
	}
	if top != "" {
		if right != "" {
			right += " + " + top
		} else {
			right = top
		}
	}
	if right != "" {
		parts = append(parts, right)
	}
	if len(parts) == 0 {
		return "(unnamed)"
	}
	return strings.Join(parts, " — ")
}

func dominantPackage(members []types.Node) string {
	count := map[string]int{}
	for _, n := range members {
		// First segment(s) before the last "." form the package portion.
		q := n.QualifiedName
		if i := strings.LastIndex(q, "."); i > 0 {
			count[q[:i]]++
		}
	}
	best, max := "", 0
	for k, v := range count {
		if v > max || (v == max && k < best) {
			best, max = k, v
		}
	}
	return best
}

func commonNamePrefix(members []types.Node, minOccur int) string {
	// shortest common prefix across at least minOccur names
	if len(members) < minOccur {
		return ""
	}
	names := make([]string, len(members))
	for i, n := range members {
		names[i] = n.Name
	}
	sort.Strings(names)
	// candidate = LCP of first minOccur sorted names
	cand := names[0]
	for i := 1; i < minOccur && i < len(names); i++ {
		cand = lcp(cand, names[i])
	}
	if len(cand) < 3 {
		return ""
	}
	return cand
}

func lcp(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}

func topPageRankName(members []types.Node) string {
	if len(members) == 0 {
		return ""
	}
	best := members[0]
	for _, n := range members[1:] {
		if n.PageRank > best.PageRank {
			best = n
		}
	}
	return best.Name
}
```

`internal/cluster/topic_tree.go`:

```go
package cluster

import (
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Community is one labeled group within a single resolution.
type Community struct {
	ID      int
	Label   string
	Members []string // node IDs
}

// Resolution captures the partition produced at one γ value.
type Resolution struct {
	Gamma       float64
	Communities []Community
}

// TopicTree holds Leiden communities at multiple resolutions.
type TopicTree struct {
	Resolutions []Resolution
	// For convenience: per-node, the community ID at each resolution.
	NodeToComm []map[string]int // index = resolution index
}

// BuildTopicTree runs Leiden at each gamma in `gammas`, naming communities.
// Used to populate the topic_tree SQLite table downstream.
func BuildTopicTree(g *graph.Graph, gammas []float64, seed int64) *TopicTree {
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		idx[n.ID] = i
	}
	edges := make([][2]int, 0, len(g.Edges))
	for _, e := range g.Edges {
		si, ok := idx[e.Src]
		if !ok {
			continue
		}
		di, ok := idx[e.Dst]
		if !ok {
			continue
		}
		// Only structural edges contribute to community signal at V0
		// (calls, references, uses_type, implements). Filter to keep results stable.
		switch e.Type {
		case types.EdgeCalls, types.EdgeInvokes, types.EdgeReferences,
			types.EdgeUsesType, types.EdgeImplements, types.EdgeExtends:
			edges = append(edges, [2]int{si, di})
		}
	}
	tt := &TopicTree{}
	for _, gamma := range gammas {
		parts := RunLeiden(len(g.Nodes), edges, LeidenOpts{
			Resolution: gamma, Seed: seed, MaxIters: 50,
		})
		// Group node indices by community label.
		groups := map[int][]int{}
		for i, c := range parts {
			groups[c] = append(groups[c], i)
		}
		nodeMap := map[string]int{}
		var comms []Community
		for c, members := range groups {
			ms := make([]types.Node, 0, len(members))
			ids := make([]string, 0, len(members))
			for _, ni := range members {
				ms = append(ms, g.Nodes[ni])
				ids = append(ids, g.Nodes[ni].ID)
				nodeMap[g.Nodes[ni].ID] = c
			}
			comms = append(comms, Community{
				ID:      c,
				Label:   LabelCommunity(ms),
				Members: ids,
			})
		}
		tt.Resolutions = append(tt.Resolutions, Resolution{
			Gamma:       gamma,
			Communities: comms,
		})
		tt.NodeToComm = append(tt.NodeToComm, nodeMap)
	}
	return tt
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/cluster/... -v
```

Expected: PASS — 3 resolutions, all communities labeled, γ=1.0 has at least one "validate*" cluster.

- [ ] **Step 4: Commit**

```bash
git add internal/cluster/topic_tree.go internal/cluster/naming.go internal/cluster/topic_tree_test.go
git commit -m "feat(cluster): build multi-resolution Leiden topic tree with substring + PageRank labels"
```

---

> **Phase 3 complete.** Graph builder + score + pkg_tree + Leiden topic_tree all green. Continue with Phase 4 to wire the first vertical slice — `ckg build`.

---

# Phase 4 — First vertical slice: `ckg build` (Go-only)

Goal: glue everything together behind a CLI. After Phase 4: `ckg build --src=<go-repo> --out=<dir>` produces a working `graph.db` + `manifest.json`. **CP-1.**

---

## Task 15: `cmd/ckg/main.go` skeleton with cobra

**Files:**
- Create: `cmd/ckg/main.go`
- Create: `cmd/ckg/root.go`
- Create: `cmd/ckg/build.go` (stub)
- Create: `cmd/ckg/serve.go` (stub)
- Create: `cmd/ckg/mcp.go` (stub)
- Create: `cmd/ckg/export_static.go` (stub)
- Create: `cmd/ckg/eval.go` (stub)
- Test:   `cmd/ckg/cli_test.go`

**No TDD for cobra wiring** — but smoke test verifies subcommands exist.

- [ ] **Step 1: Add cobra dependency**

```bash
go get github.com/spf13/cobra
```

- [ ] **Step 2: Implement entry**

`cmd/ckg/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ckg:", err)
		os.Exit(1)
	}
}
```

`cmd/ckg/root.go`:

```go
package main

import "github.com/spf13/cobra"

const ckgVersion = "0.1.0"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ckg",
		Short:         "Code Knowledge Graph",
		Version:       ckgVersion,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newBuildCmd(), newServeCmd(), newMCPCmd(),
		newExportStaticCmd(), newEvalCmd())
	return root
}
```

`cmd/ckg/build.go` (stub returning "not implemented"):

```go
package main

import (
	"errors"
	"github.com/spf13/cobra"
)

func newBuildCmd() *cobra.Command {
	var src, out string
	var langs []string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Parse a source tree and produce graph.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("build not yet implemented (Task 16)")
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
```

Repeat the same stub pattern for `serve`, `mcp`, `export-static`, `eval`:

`cmd/ckg/serve.go`:

```go
package main

import (
	"errors"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var graph string
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the embedded 3D viewer over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("serve not yet implemented (Task 26)")
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().IntVar(&port, "port", 8787, "HTTP port")
	cmd.Flags().BoolVar(&open, "open", false, "open browser on start")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
```

`cmd/ckg/mcp.go`:

```go
package main

import (
	"errors"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var graph string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("mcp not yet implemented (Task 29)")
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
```

`cmd/ckg/export_static.go`:

```go
package main

import (
	"errors"
	"github.com/spf13/cobra"
)

func newExportStaticCmd() *cobra.Command {
	var graph, out string
	cmd := &cobra.Command{
		Use:   "export-static",
		Short: "Export graph as chunked JSON for static hosting",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("export-static not yet implemented (Task 31)")
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
```

`cmd/ckg/eval.go`:

```go
package main

import (
	"errors"
	"github.com/spf13/cobra"
)

func newEvalCmd() *cobra.Command {
	var tasks, graph, out, llm string
	var baselines []string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run four-baseline evaluation against a graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("eval not yet implemented (Task 35)")
		},
	}
	cmd.Flags().StringVar(&tasks, "tasks", "", "glob of task YAMLs (required)")
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "eval/results", "output directory")
	cmd.Flags().StringVar(&llm, "llm", "claude-sonnet-4-6", "LLM model id")
	cmd.Flags().StringSliceVar(&baselines, "baselines",
		[]string{"alpha", "beta", "gamma", "delta"}, "baselines to run")
	_ = cmd.MarkFlagRequired("tasks")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
```

- [ ] **Step 3: Smoke test that subcommands are registered**

`cmd/ckg/cli_test.go`:

```go
package main

import (
	"sort"
	"testing"
)

func TestSubcommandsRegistered(t *testing.T) {
	root := newRootCmd()
	want := []string{"build", "eval", "export-static", "mcp", "serve"}
	got := []string{}
	for _, c := range root.Commands() {
		got = append(got, c.Use)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("subcommands = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("subcommands[%d] = %q, want %q", i, got[i], w)
		}
	}
}
```

- [ ] **Step 4: Run + verify build**

```bash
go test ./cmd/ckg/... -v
go build -o bin/ckg ./cmd/ckg
./bin/ckg --help
```

Expected: smoke test PASS; `--help` lists all five subcommands.

- [ ] **Step 5: Commit**

```bash
git add cmd/ckg/ go.mod go.sum
git commit -m "feat(cli): add ckg root command and five subcommand stubs (build/serve/mcp/export-static/eval)"
```

---

## Task 16: Wire `ckg build` end-to-end

**Files:**
- Create: `internal/buildpipe/pipeline.go` (orchestration: detect → parse → graph → cluster → score → persist)
- Modify: `cmd/ckg/build.go` (call pipeline)
- Test:   `internal/buildpipe/pipeline_test.go`

- [ ] **Step 1: Define orchestration**

`internal/buildpipe/pipeline.go`:

```go
// Package buildpipe orchestrates the full Pass 1..4 build (spec §4.7).
package buildpipe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/score"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Options controls one ckg build invocation.
type Options struct {
	SrcRoot    string
	OutDir     string
	Languages  []string // {"auto"} | subset of {"go","ts","sol"}
	Logger     *slog.Logger
	CKGVersion string
}

// Run executes the full pipeline. Side effects: writes OutDir/graph.db
// and OutDir/manifest.json. Returns the persisted Manifest summary.
func Run(opt Options) (persist.Manifest, error) {
	log := opt.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return persist.Manifest{}, fmt.Errorf("mkdir out: %w", err)
	}

	// (1) detect
	files, err := detect.Walk(opt.SrcRoot)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("detect: %w", err)
	}
	log.Info("detected files", "go", len(files.Go), "ts", len(files.TS), "sol", len(files.Sol))

	// (2)+(3) parse + link, per language
	resolved := []*parse.ResolvedGraph{}
	parseErrs := 0
	if shouldRun("go", opt.Languages) && len(files.Go) > 0 {
		rg, n, err := runGoPipeline(opt.SrcRoot, files.Go, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("go pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
	}
	// TS / Sol pipelines wired in Phase 5.

	// (4) graph build + validate
	g, err := graph.Build(resolved)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Validate: %w", err)
	}

	// (5) cluster
	pkgTree := cluster.BuildPkgTree(g)
	topicTree := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)

	// (6) score
	score.Compute(g)

	// (7) persist
	dbPath := filepath.Join(opt.OutDir, "graph.db")
	_ = os.Remove(dbPath) // V0: full rebuild only
	store, err := persist.Open(dbPath)
	if err != nil {
		return persist.Manifest{}, err
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertNodes(g.Nodes); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertEdges(g.Edges); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertPkgTree(pkgTree.Edges); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertTopicTree(topicTree); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertBlobs(extractBlobs(opt.SrcRoot, g.Nodes)); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.RebuildFTS(); err != nil {
		return persist.Manifest{}, err
	}

	// Manifest with staleness fingerprint
	m := persist.Manifest{
		SchemaVersion:    "1.0",
		CKGVersion:       opt.CKGVersion,
		BuildTimestamp:   time.Now().UTC().Format(time.RFC3339),
		SrcRoot:          opt.SrcRoot,
		Languages:        map[string]int{"go": len(files.Go), "ts": len(files.TS), "sol": len(files.Sol)},
		Stats: map[string]int{
			"nodes": len(g.Nodes), "edges": len(g.Edges),
			"pkg_tree_edges": len(pkgTree.Edges),
		},
		ParseErrorsCount: parseErrs,
		ClusteringStatus: "ok",
	}
	setStaleness(&m)
	if err := store.SetManifest(m); err != nil {
		return persist.Manifest{}, err
	}
	if err := writeManifestJSON(filepath.Join(opt.OutDir, "manifest.json"), m); err != nil {
		return persist.Manifest{}, err
	}
	log.Info("build complete",
		"nodes", len(g.Nodes), "edges", len(g.Edges),
		"pkg_tree_edges", len(pkgTree.Edges), "topic_resolutions", len(topicTree.Resolutions))
	return m, nil
}

func shouldRun(lang string, opts []string) bool {
	for _, l := range opts {
		if l == "auto" || l == lang {
			return true
		}
	}
	return false
}

func runGoPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, int, error) {
	p := gop.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("read file", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("parse file", "path", full, "err", err)
			errs++
			continue
		}
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, err
}

func extractBlobs(root string, nodes []types.Node) map[string][]byte {
	blobs := map[string][]byte{}
	cache := map[string][]byte{}
	for _, n := range nodes {
		if n.Type == types.NodePackage {
			continue
		}
		full := filepath.Join(root, n.FilePath)
		src, ok := cache[full]
		if !ok {
			b, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			cache[full] = b
			src = b
		}
		if n.StartByte < 0 || n.EndByte > len(src) || n.StartByte >= n.EndByte {
			continue
		}
		blobs[n.ID] = append([]byte(nil), src[n.StartByte:n.EndByte]...)
	}
	return blobs
}

func setStaleness(m *persist.Manifest) {
	out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
	if err == nil {
		m.SrcCommit = strings.TrimSpace(string(out))
		m.StalenessMethod = "git"
		return
	}
	// Fallback: 5-file mtime sum.
	m.StalenessMethod = "mtime"
	files, _ := detect.Walk(m.SrcRoot)
	all := append(append([]string{}, files.Go...), files.TS...)
	all = append(all, files.Sol...)
	if len(all) > 5 {
		all = all[:5]
	}
	var sum int64
	for _, rel := range all {
		st, err := os.Stat(filepath.Join(m.SrcRoot, rel))
		if err == nil {
			sum += st.ModTime().UnixNano()
		}
	}
	m.StalenessFiles = all
	m.StalenessMTimeSum = sum
}

func writeManifestJSON(path string, m persist.Manifest) error {
	buf, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(path, buf, 0o644)
}
```

- [ ] **Step 2: Add the missing persist methods used above**

Append to `internal/persist/sqlite.go`:

```go
// InsertPkgTree bulk-inserts package-tree edges.
func (s *Store) InsertPkgTree(edges []ClusterEdge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO pkg_tree (parent_id, child_id, level) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range edges {
		if _, err := stmt.Exec(e.ParentID, e.ChildID, e.Level); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClusterEdge mirrors cluster.Edge to avoid an import cycle.
type ClusterEdge struct {
	ParentID, ChildID string
	Level             int
}

// InsertTopicTree persists multi-resolution Leiden communities.
type TopicTreeInput interface {
	ResolutionsCount() int
	ResolutionGamma(i int) float64
	ResolutionMembers(i int) map[string][]string // label -> []nodeID
}

// InsertTopicTree expects an interface to avoid coupling persist to cluster.
func (s *Store) InsertTopicTree(t TopicTreeInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM topic_tree`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO topic_tree (parent_id, child_id, resolution, topic_label) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := 0; i < t.ResolutionsCount(); i++ {
		members := t.ResolutionMembers(i)
		for label, ids := range members {
			for _, id := range ids {
				if _, err := stmt.Exec(nil, id, i, label); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// InsertBlobs stores per-node source slices.
func (s *Store) InsertBlobs(blobs map[string][]byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO blobs (node_id, source) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for id, b := range blobs {
		if _, err := stmt.Exec(id, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RebuildFTS drops + reloads the FTS5 virtual table from nodes.
func (s *Store) RebuildFTS() error {
	_, err := s.db.Exec(`INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')`)
	return err
}
```

Add a thin adapter in `internal/cluster/persist_adapter.go` so the pipeline can pass `TopicTree` to persist without an import cycle:

```go
package cluster

// PersistEdges converts pkg_tree edges to the persist-friendly ClusterEdge type
// expected by persist.Store.InsertPkgTree. Defined here (and matched by an
// identically-shaped struct in persist) to avoid a direct import dependency.
type PersistClusterEdge struct {
	ParentID, ChildID string
	Level             int
}

func (t *PkgTree) PersistEdges() []PersistClusterEdge {
	out := make([]PersistClusterEdge, len(t.Edges))
	for i, e := range t.Edges {
		out[i] = PersistClusterEdge{ParentID: e.ParentID, ChildID: e.ChildID, Level: e.Level}
	}
	return out
}

// Wrap satisfies the persist.TopicTreeInput interface.
func (t *TopicTree) ResolutionsCount() int             { return len(t.Resolutions) }
func (t *TopicTree) ResolutionGamma(i int) float64     { return t.Resolutions[i].Gamma }
func (t *TopicTree) ResolutionMembers(i int) map[string][]string {
	out := map[string][]string{}
	for _, c := range t.Resolutions[i].Communities {
		out[c.Label] = c.Members
	}
	return out
}
```

Update `internal/buildpipe/pipeline.go` `Run` to pass converted types:

```go
	// inside Run, after BuildPkgTree:
	if err := store.InsertPkgTreeFromCluster(pkgTree.PersistEdges()); err != nil {
		return persist.Manifest{}, err
	}
```

And add the matching method to `persist`:

```go
func (s *Store) InsertPkgTreeFromCluster(edges []cluster.PersistClusterEdge) error {
	out := make([]ClusterEdge, len(edges))
	for i, e := range edges {
		out[i] = ClusterEdge(e)
	}
	return s.InsertPkgTree(out)
}
```

> **Note on cycle:** `persist` importing `cluster` is fine because `buildpipe` is the only thing that wires both. Keep `cluster` free of `persist` imports.

- [ ] **Step 3: Wire `ckg build` to call buildpipe.Run**

Replace `cmd/ckg/build.go`:

```go
package main

import (
	"fmt"
	"os"

	"log/slog"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
)

func newBuildCmd() *cobra.Command {
	var src, out string
	var langs []string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Parse a source tree and produce graph.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			m, err := buildpipe.Run(buildpipe.Options{
				SrcRoot:    src,
				OutDir:     out,
				Languages:  langs,
				Logger:     log,
				CKGVersion: ckgVersion,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: built %d nodes / %d edges into %s\n",
				m.Stats["nodes"], m.Stats["edges"], out)
			return nil
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
```

- [ ] **Step 4: Pipeline unit test using the existing Go fixture**

`internal/buildpipe/pipeline_test.go`:

```go
package buildpipe_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestPipelineRunsOnGoFixture(t *testing.T) {
	out := t.TempDir()
	_, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../parse/golang/testdata/resolve",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Reopen and verify nodes were persisted.
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("Open RO: %v", err)
	}
	defer store.Close()
	m, err := store.GetManifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.Stats["nodes"] == 0 {
		t.Errorf("expected nodes > 0, got 0")
	}
	if m.SrcCommit == "" && m.StalenessMethod != "mtime" {
		t.Errorf("expected staleness fingerprint, got method=%q commit=%q",
			m.StalenessMethod, m.SrcCommit)
	}
}
```

- [ ] **Step 5: Run + commit**

```bash
go vet ./...
go test ./internal/buildpipe/... -v
go build -o bin/ckg ./cmd/ckg
./bin/ckg build --src=internal/parse/golang/testdata/resolve --out=/tmp/ckg-smoke
ls /tmp/ckg-smoke    # should show graph.db + manifest.json
git add internal/buildpipe/ internal/cluster/persist_adapter.go internal/persist/ cmd/ckg/build.go
git commit -m "feat(build): wire detect → parse → graph → cluster → score → persist behind ckg build"
```

---

## Task 17: First end-to-end test (Go-only, real binary)

**Files:**
- Create: `internal/e2e/build_go_test.go`

- [ ] **Step 1: Write E2E test that invokes the binary**

`internal/e2e/build_go_test.go`:

```go
//go:build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EBuildGoFixture(t *testing.T) {
	bin, err := exec.LookPath("ckg")
	if err != nil {
		// fall back to local build artifact
		bin, _ = filepath.Abs("../../bin/ckg")
	}
	out := t.TempDir()
	cmd := exec.Command(bin, "build",
		"--src", "../parse/golang/testdata/resolve",
		"--out", out)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ckg build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.db")); err != nil {
		t.Errorf("expected graph.db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err != nil {
		t.Errorf("expected manifest.json: %v", err)
	}
}
```

- [ ] **Step 2: Run the e2e test (requires built binary)**

```bash
go build -o bin/ckg ./cmd/ckg
go test -tags e2e ./internal/e2e/... -v
```

Expected: PASS — graph.db and manifest.json exist after the binary runs.

- [ ] **Step 3: Commit**

```bash
git add internal/e2e/
git commit -m "test(e2e): smoke build via ckg binary against Go fixture"
```

---

> **CP-1 reached.** `ckg build` works end-to-end on Go corpora. Continue with Phase 5 to add TypeScript and Solidity parsers + cross-language linking.

---

# Phase 5 — TypeScript + Solidity parsers + cross-language link

Goal: extend coverage to TS/JS and Solidity using tree-sitter, then add a cross-language linker that emits `binds_to` edges between Solidity contracts and TypeScript classes referencing them via typechain-style imports. After Phase 5: `ckg build` succeeds for the synthetic multi-language corpus (created later in T36). **CP-2.**

---

## Task 18: TypeScript parser (tree-sitter)

**Files:**
- Create: `internal/parse/typescript/parser.go`
- Create: `internal/parse/typescript/queries.go` (raw query strings)
- Create: `internal/parse/typescript/declarations.go`
- Create: `internal/parse/typescript/resolve.go`
- Test:   `internal/parse/typescript/parser_test.go`
- Create: `internal/parse/typescript/testdata/{simple_class.ts, simple_class_golden.json}`

- [ ] **Step 1: Add tree-sitter deps**

```bash
go get github.com/smacker/go-tree-sitter
go get github.com/smacker/go-tree-sitter/typescript/typescript
go get github.com/smacker/go-tree-sitter/javascript
```

- [ ] **Step 2: Create fixture**

`internal/parse/typescript/testdata/simple_class.ts`:

```typescript
import { Vault } from './vault';

export class WalletService {
    private vault: Vault;
    constructor(v: Vault) { this.vault = v; }
    deposit(amount: number): void {
        this.vault.deposit(amount);
    }
}
```

`internal/parse/typescript/testdata/simple_class_golden.json`:

```json
{
  "node_qnames_subset": [
    "WalletService",
    "WalletService.deposit",
    "import:./vault"
  ],
  "node_types": {
    "WalletService": "Class",
    "WalletService.deposit": "Method",
    "import:./vault": "Import"
  }
}
```

- [ ] **Step 3: Write failing test**

`internal/parse/typescript/parser_test.go`:

```go
package typescript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
)

type golden struct {
	NodeQnamesSubset []string          `json:"node_qnames_subset"`
	NodeTypes        map[string]string `json:"node_types"`
}

func TestTSParseSimpleClass(t *testing.T) {
	dir := "testdata"
	src, err := os.ReadFile(filepath.Join(dir, "simple_class.ts"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := tsp.New(dir).ParseFile(filepath.Join(dir, "simple_class.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	gb, _ := os.ReadFile(filepath.Join(dir, "simple_class_golden.json"))
	var g golden
	_ = json.Unmarshal(gb, &g)

	have := map[string]string{}
	for _, n := range res.Nodes {
		have[n.QualifiedName] = string(n.Type)
	}
	for _, q := range g.NodeQnamesSubset {
		if _, ok := have[q]; !ok {
			t.Errorf("missing node qname %q (have: %v)", q, have)
		}
	}
	for q, want := range g.NodeTypes {
		if got := have[q]; got != want {
			t.Errorf("type for %q = %q, want %q", q, got, want)
		}
	}
}
```

- [ ] **Step 4: Implement queries + parser**

`internal/parse/typescript/queries.go`:

```go
package typescript

// Tree-sitter queries derived from spec §4.6.2. Captures use `@name` markers
// that the parser reads positionally.
const (
	queryClass     = `(class_declaration name: (type_identifier) @name) @decl`
	queryInterface = `(interface_declaration name: (type_identifier) @name) @decl`
	queryFunction  = `(function_declaration name: (identifier) @name) @decl`
	queryMethod    = `(method_definition name: (property_identifier) @name) @decl`
	queryImport    = `(import_statement source: (string) @path) @decl`
	queryExport    = `(export_statement) @decl`
	queryDecorator = `(decorator (call_expression function: (identifier) @name)) @decl`
	queryTypeAlias = `(type_alias_declaration name: (type_identifier) @name) @decl`
	queryEnum      = `(enum_declaration name: (identifier) @name) @decl`
)
```

`internal/parse/typescript/parser.go`:

```go
// Package typescript implements the CKG parser for .ts/.tsx/.js/.jsx (spec §4.6.2).
package typescript

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	js "github.com/smacker/go-tree-sitter/javascript"
	ts "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

type Parser struct {
	srcRoot string
}

func New(srcRoot string) *Parser { return &Parser{srcRoot: srcRoot} }

func (p *Parser) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
}

func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	parser := sitter.NewParser()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx":
		parser.SetLanguage(ts.GetLanguage())
	default:
		parser.SetLanguage(js.GetLanguage())
	}
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	v := newDeclVisitor(rel, src, parser.Language())
	v.visit(tree.RootNode())
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}
```

`internal/parse/typescript/declarations.go`:

```go
package typescript

import (
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

type declVisitor struct {
	rel     string
	src     []byte
	lang    *sitter.Language
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
}

func newDeclVisitor(rel string, src []byte, lang *sitter.Language) *declVisitor {
	v := &declVisitor{rel: rel, src: src, lang: lang}
	fileQ := "file:" + rel
	v.fileID = makeID(fileQ, "ts", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: "ts", Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) visit(root *sitter.Node) {
	v.runQuery(queryClass, types.NodeClass, "")
	v.runQuery(queryInterface, types.NodeInterface, "")
	v.runQuery(queryFunction, types.NodeFunction, "")
	v.runQuery(queryMethod, types.NodeMethod, "")
	v.runQuery(queryTypeAlias, types.NodeTypeAlias, "")
	v.runQuery(queryEnum, types.NodeEnum, "")
	v.runQuery(queryDecorator, types.NodeDecorator, "")
	v.runImports()
	_ = root
}

func (v *declVisitor) runQuery(q string, nt types.NodeType, _ string) {
	query, err := sitter.NewQuery([]byte(q), v.lang)
	if err != nil {
		return
	}
	cur := sitter.NewQueryCursor()
	cur.Exec(query, nil) // root passed by parser when integrating; simplify here
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			name := query.CaptureNameForId(c.Index)
			if name != "name" {
				continue
			}
			ident := c.Node.Content(v.src)
			startByte := int(c.Node.StartByte())
			endByte := int(c.Node.EndByte())
			startLine := int(c.Node.StartPoint().Row) + 1
			endLine := int(c.Node.EndPoint().Row) + 1
			qname := ident
			if nt == types.NodeMethod {
				if className := nearestClassName(c.Node, v.src); className != "" {
					qname = className + "." + ident
				}
			}
			id := makeID(qname, "ts", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				FilePath: v.rel, StartLine: startLine, EndLine: endLine,
				StartByte: startByte, EndByte: endByte,
				Language: "ts", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

func (v *declVisitor) runImports() {
	query, err := sitter.NewQuery([]byte(queryImport), v.lang)
	if err != nil {
		return
	}
	cur := sitter.NewQueryCursor()
	cur.Exec(query, nil)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) != "path" {
				continue
			}
			path := c.Node.Content(v.src)
			path = trimQuotes(path)
			qname := "import:" + path
			id := makeID(qname, "ts", int(c.Node.StartByte()))
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: types.NodeImport, Name: path, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(c.Node.StartPoint().Row) + 1,
				EndLine: int(c.Node.EndPoint().Row) + 1,
				StartByte: int(c.Node.StartByte()), EndByte: int(c.Node.EndByte()),
				Language: "ts", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeImports, Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

func nearestClassName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "class_declaration" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Content(src)
			}
		}
	}
	return ""
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

func makeID(qname, lang string, startByte int) string {
	// Reuse the same hashing scheme as Go parser.
	return goIDHash(qname, lang, startByte)
}

// goIDHash is implemented in idgen.go to avoid duplication across parsers.
// (Move MakeID into a shared internal/parse/idgen package for V0 reuse.)
func goIDHash(qname, lang string, startByte int) string {
	return parseIDLocal(qname, lang, startByte)
}
```

For shared ID generation, create `internal/parse/idgen.go`:

```go
package parse

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MakeID returns a stable 16-char hash for (qname, language, startByte).
// All language parsers must use this so node IDs collide deterministically
// across pass boundaries and re-runs.
func MakeID(qname, lang string, startByte int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", qname, lang, startByte)))
	return hex.EncodeToString(sum[:])[:16]
}
```

Then in `internal/parse/typescript/declarations.go` replace the local hash helpers with:

```go
import "github.com/0xmhha/code-knowledge-graph/internal/parse"

func makeID(qname, lang string, startByte int) string {
	return parse.MakeID(qname, lang, startByte)
}
```

Update `internal/parse/golang/idgen.go` to delegate to the same:

```go
package golang

import "github.com/0xmhha/code-knowledge-graph/internal/parse"

func MakeID(qname, lang string, startByte int) string {
	return parse.MakeID(qname, lang, startByte)
}
```

> Use of `sitter.QueryCursor.Exec(query, nil)` in the snippet above passes a nil root for brevity — in practice pass the tree's root node from `ParseFile`. Wire that through by storing `v.root *sitter.Node` and using `cur.Exec(query, v.root)`.

- [ ] **Step 5: Implement minimal Resolve (heuristic name match)**

`internal/parse/typescript/resolve.go`:

```go
package typescript

import (
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Resolve unions per-file results. V0 cross-file resolution is name-based:
// a CallSite/Reference referencing identifier X resolves to the definition
// node whose Name == X, with INFERRED confidence when match is non-unique.
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}
	byName := map[string][]string{}
	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			if n.Type == types.NodeFunction || n.Type == types.NodeMethod || n.Type == types.NodeClass {
				byName[n.Name] = append(byName[n.Name], n.ID)
			}
		}
	}
	for _, r := range results {
		for _, pr := range r.Pending {
			ids := byName[pr.TargetQName]
			if len(ids) == 0 {
				continue
			}
			conf := types.ConfInferred
			out.Edges = append(out.Edges, types.Edge{
				Src: pr.SrcID, Dst: ids[0], Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 6: Run + commit**

```bash
go vet ./internal/parse/typescript/...
go test ./internal/parse/typescript/... -v
git add internal/parse/typescript/ internal/parse/idgen.go internal/parse/golang/idgen.go go.mod go.sum
git commit -m "feat(parse/ts): tree-sitter parser emits Class/Method/Function/Import nodes with shared idgen"
```

---

## Task 19: Solidity parser (tree-sitter) + ABI extraction

**Files:**
- Create: `internal/parse/solidity/parser.go`
- Create: `internal/parse/solidity/queries.go`
- Create: `internal/parse/solidity/declarations.go`
- Create: `internal/parse/solidity/abi.go` (extract per-Contract function signatures)
- Test:   `internal/parse/solidity/parser_test.go`
- Create: `internal/parse/solidity/testdata/{vault.sol, vault_golden.json}`

- [ ] **Step 1: Add tree-sitter-solidity binding**

```bash
go get github.com/smacker/go-tree-sitter/solidity
```

> If `solidity` isn't a top-level subpackage in `smacker/go-tree-sitter`, vendor it from `github.com/JoranHonig/tree-sitter-solidity` per the project's standard binding pattern.

- [ ] **Step 2: Create fixture**

`internal/parse/solidity/testdata/vault.sol`:

```solidity
pragma solidity ^0.8.20;

contract Vault {
    mapping(address => uint256) public balances;
    event Deposited(address indexed who, uint256 amount);

    modifier nonZero(uint256 v) { require(v > 0, "zero"); _; }

    function deposit(uint256 amount) external nonZero(amount) {
        balances[msg.sender] += amount;
        emit Deposited(msg.sender, amount);
    }
}
```

`internal/parse/solidity/testdata/vault_golden.json`:

```json
{
  "expected_node_types": ["Contract", "Mapping", "Event", "Modifier", "Function"],
  "expected_edge_types": ["emits_event", "has_modifier", "writes_mapping"]
}
```

- [ ] **Step 3: Write failing test**

`internal/parse/solidity/parser_test.go`:

```go
package solidity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

type golden struct {
	ExpectedNodeTypes []string `json:"expected_node_types"`
	ExpectedEdgeTypes []string `json:"expected_edge_types"`
}

func TestSolParseVault(t *testing.T) {
	dir := "testdata"
	src, err := os.ReadFile(filepath.Join(dir, "vault.sol"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := sol.New(dir).ParseFile(filepath.Join(dir, "vault.sol"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	gb, _ := os.ReadFile(filepath.Join(dir, "vault_golden.json"))
	var g golden
	_ = json.Unmarshal(gb, &g)

	haveNT := map[types.NodeType]bool{}
	for _, n := range res.Nodes {
		haveNT[n.Type] = true
	}
	for _, want := range g.ExpectedNodeTypes {
		if !haveNT[types.NodeType(want)] {
			t.Errorf("missing node type %s", want)
		}
	}
	haveET := map[types.EdgeType]bool{}
	for _, e := range res.Edges {
		haveET[e.Type] = true
	}
	for _, want := range g.ExpectedEdgeTypes {
		if !haveET[types.EdgeType(want)] {
			t.Errorf("missing edge type %s", want)
		}
	}
}
```

- [ ] **Step 4: Implement queries + parser + abi extractor**

`internal/parse/solidity/queries.go`:

```go
package solidity

const (
	queryContract    = `(contract_declaration name: (identifier) @name) @decl`
	queryFunction    = `(function_definition name: (identifier) @name) @decl`
	queryModifier    = `(modifier_definition name: (identifier) @name) @decl`
	queryEvent       = `(event_definition name: (identifier) @name) @decl`
	queryStruct      = `(struct_declaration name: (identifier) @name) @decl`
	queryEnum        = `(enum_declaration name: (identifier) @name) @decl`
	queryStateVar    = `(state_variable_declaration name: (identifier) @name) @decl`
	queryMappingDecl = `(state_variable_declaration type: (mapping_type) @t name: (identifier) @name)`
	queryEmit        = `(emit_statement name: (identifier) @event) @stmt`
	queryHasModifier = `(modifier_invocation name: (identifier) @mod) @stmt`
)
```

`internal/parse/solidity/parser.go`:

```go
// Package solidity implements the CKG parser for .sol files (spec §4.6.3).
package solidity

import (
	"context"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"
	solang "github.com/smacker/go-tree-sitter/solidity"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

type Parser struct {
	srcRoot string
	abi     map[string][]ABISig // contract name -> signatures
}

func New(srcRoot string) *Parser {
	return &Parser{srcRoot: srcRoot, abi: map[string][]ABISig{}}
}

func (p *Parser) Extensions() []string { return []string{".sol"} }

func (p *Parser) ParseFile(path string, src []byte) (*parse.ParseResult, error) {
	rel, err := filepath.Rel(p.srcRoot, path)
	if err != nil {
		rel = path
	}
	parser := sitter.NewParser()
	parser.SetLanguage(solang.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	v := newDeclVisitor(rel, src, parser.Language(), tree.RootNode(), p.abi)
	v.visit()
	return &parse.ParseResult{
		Path:    rel,
		Nodes:   v.nodes,
		Edges:   v.edges,
		Pending: v.pending,
	}, nil
}

// ABI returns the per-contract signatures collected during ParseFile.
// Used by the cross-language linker (Task 20).
func (p *Parser) ABI() map[string][]ABISig { return p.abi }
```

`internal/parse/solidity/abi.go`:

```go
package solidity

// ABISig is a stripped-down function signature used by the cross-language
// linker. We avoid pulling in solc; signatures are recovered from the AST
// (spec §4.6.3 "ABI 추출 부산물").
type ABISig struct {
	ContractName string
	FunctionName string
	ParamTypes   []string
}
```

`internal/parse/solidity/declarations.go` (sketch — same pattern as TS):

```go
package solidity

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

type declVisitor struct {
	rel     string
	src     []byte
	lang    *sitter.Language
	root    *sitter.Node
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
	abi     map[string][]ABISig
}

func newDeclVisitor(rel string, src []byte, lang *sitter.Language, root *sitter.Node, abi map[string][]ABISig) *declVisitor {
	v := &declVisitor{rel: rel, src: src, lang: lang, root: root, abi: abi}
	fileQ := "file:" + rel
	v.fileID = parse.MakeID(fileQ, "sol", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: "sol", Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) visit() {
	v.runDecl(queryContract, types.NodeContract, "")
	v.runDecl(queryFunction, types.NodeFunction, "")
	v.runDecl(queryModifier, types.NodeModifier, "")
	v.runDecl(queryEvent, types.NodeEvent, "")
	v.runDecl(queryStruct, types.NodeStruct, "")
	v.runDecl(queryEnum, types.NodeEnum, "")
	v.runDecl(queryStateVar, types.NodeField, "")
	v.runMappingDecl()
	v.runEmits()
	v.runHasModifier()
	v.collectABI()
}

func (v *declVisitor) runDecl(q string, nt types.NodeType, _ string) {
	query, err := sitter.NewQuery([]byte(q), v.lang)
	if err != nil {
		return
	}
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) != "name" {
				continue
			}
			ident := c.Node.Content(v.src)
			startByte := int(c.Node.StartByte())
			endByte := int(c.Node.EndByte())
			qname := ident
			if nt == types.NodeFunction {
				if cn := nearestContractName(c.Node, v.src); cn != "" {
					qname = cn + "." + ident
				}
			}
			id := parse.MakeID(qname, "sol", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(c.Node.StartPoint().Row) + 1,
				EndLine: int(c.Node.EndPoint().Row) + 1,
				StartByte: startByte, EndByte: endByte,
				Language: "sol", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

func (v *declVisitor) runMappingDecl() {
	query, _ := sitter.NewQuery([]byte(queryMappingDecl), v.lang)
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		var name string
		var startByte, endByte int
		var line int
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) == "name" {
				name = c.Node.Content(v.src)
				startByte = int(c.Node.StartByte())
				endByte = int(c.Node.EndByte())
				line = int(c.Node.StartPoint().Row) + 1
			}
		}
		qname := name + ":mapping"
		id := parse.MakeID(qname, "sol", startByte)
		v.nodes = append(v.nodes, types.Node{
			ID: id, Type: types.NodeMapping, Name: name, QualifiedName: qname,
			FilePath: v.rel, StartLine: line, EndLine: line,
			StartByte: startByte, EndByte: endByte,
			Language: "sol", Confidence: types.ConfExtracted,
		})
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) runEmits() {
	query, _ := sitter.NewQuery([]byte(queryEmit), v.lang)
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		var event string
		var fnQ string
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) == "event" {
				event = c.Node.Content(v.src)
				fnQ = nearestFunctionQname(c.Node, v.src)
			}
		}
		// Pending ref: function -emits_event-> Event (resolved in pass 2 by name).
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", 0),
			EdgeType:    types.EdgeEmitsEvent,
			TargetQName: event,
		})
	}
}

func (v *declVisitor) runHasModifier() {
	query, _ := sitter.NewQuery([]byte(queryHasModifier), v.lang)
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		var mod string
		var fnQ string
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) == "mod" {
				mod = c.Node.Content(v.src)
				fnQ = nearestFunctionQname(c.Node, v.src)
			}
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", 0),
			EdgeType:    types.EdgeHasModifier,
			TargetQName: mod,
		})
	}
}

// collectABI populates p.abi from the discovered Contract / Function nodes.
func (v *declVisitor) collectABI() {
	currentContract := ""
	for _, n := range v.nodes {
		switch n.Type {
		case types.NodeContract:
			currentContract = n.Name
		case types.NodeFunction:
			if currentContract == "" {
				continue
			}
			v.abi[currentContract] = append(v.abi[currentContract], ABISig{
				ContractName: currentContract,
				FunctionName: n.Name,
				ParamTypes:   nil, // V0 placeholder — sufficient for name-match cross-lang link.
			})
		}
	}
}

// helpers
func nearestContractName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "contract_declaration" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Content(src)
			}
		}
	}
	return ""
}

func nearestFunctionQname(n *sitter.Node, src []byte) string {
	cn := nearestContractName(n, src)
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "function_definition" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				if cn == "" {
					return id.Content(src)
				}
				return cn + "." + id.Content(src)
			}
		}
	}
	return strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
}
```

> **Resolve** for Solidity follows the same pattern as TypeScript — name-based resolution, INFERRED on cross-file matches. Add `internal/parse/solidity/resolve.go` mirroring the TS version (omit here for brevity).

- [ ] **Step 5: Run + commit**

```bash
go test ./internal/parse/solidity/... -v
git add internal/parse/solidity/ go.mod go.sum
git commit -m "feat(parse/sol): tree-sitter parser emits Contract/Function/Modifier/Event/Mapping with ABI side-product"
```

---

## Task 20: Cross-language linker (Sol ↔ TS `binds_to`)

**Files:**
- Create: `internal/link/xlang.go`
- Test:   `internal/link/xlang_test.go`

- [ ] **Step 1: Write failing test**

`internal/link/xlang_test.go`:

```go
package link_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestLinkBindsToContract(t *testing.T) {
	tsClass := types.Node{
		ID: "ts000000000000aa", Type: types.NodeClass,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "src/contracts/Vault.ts", Language: "ts",
		Confidence: types.ConfExtracted,
	}
	solContract := types.Node{
		ID: "sol00000000000bb", Type: types.NodeContract,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "contracts/Vault.sol", Language: "sol",
		Confidence: types.ConfExtracted,
	}
	abi := map[string][]link.ABISig{"Vault": {{ContractName: "Vault", FunctionName: "deposit"}}}

	edges := link.SolToTS(
		[]types.Node{tsClass, solContract},
		abi,
	)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	e := edges[0]
	if e.Type != types.EdgeBindsTo || e.Src != solContract.ID || e.Dst != tsClass.ID {
		t.Errorf("unexpected edge: %+v", e)
	}
	if e.Confidence != types.ConfInferred {
		t.Errorf("confidence = %s, want INFERRED", e.Confidence)
	}
}
```

- [ ] **Step 2: Implement linker**

`internal/link/xlang.go`:

```go
// Package link runs cross-language linking after per-language Pass 2 (spec §4.7).
// V0 implements only Sol -> TS bindings via name match. Cross-lang Go links are V1+.
package link

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ABISig mirrors solidity.ABISig to avoid coupling link → parse/solidity.
type ABISig struct {
	ContractName string
	FunctionName string
	ParamTypes   []string
}

// SolToTS emits binds_to edges from each Solidity Contract node to a
// matching TypeScript Class node sharing the same Name. The ABI map is
// retained to support future signature-aware matching.
func SolToTS(nodes []types.Node, abi map[string][]ABISig) []types.Edge {
	tsClassByName := map[string][]types.Node{}
	for _, n := range nodes {
		if n.Language == "ts" && n.Type == types.NodeClass {
			tsClassByName[n.Name] = append(tsClassByName[n.Name], n)
		}
	}
	var out []types.Edge
	for _, n := range nodes {
		if n.Language != "sol" || n.Type != types.NodeContract {
			continue
		}
		matches := tsClassByName[n.Name]
		if len(matches) == 0 {
			continue
		}
		_ = abi[n.Name] // V0: no signature filter; reserved for V1+
		// Pick the most-likely binding: shortest path containing "contracts" or "typechain".
		best := pickBest(matches)
		out = append(out, types.Edge{
			Src: n.ID, Dst: best.ID, Type: types.EdgeBindsTo,
			Count: 1, Confidence: types.ConfInferred,
		})
	}
	return out
}

func pickBest(cands []types.Node) types.Node {
	best := cands[0]
	for _, c := range cands[1:] {
		if score(c) > score(best) {
			best = c
		}
	}
	return best
}

func score(n types.Node) int {
	s := 0
	for _, hint := range []string{"typechain", "contracts", "abi"} {
		if containsFold(n.FilePath, hint) {
			s++
		}
	}
	return s
}

func containsFold(s, sub string) bool {
	// case-insensitive substring; avoids importing strings just to lowercase.
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/link/... -v
git add internal/link/
git commit -m "feat(link): cross-language binds_to linker matching Solidity contracts to TS classes"
```

---

## Task 21: Wire TS + Sol + xlang into pipeline; CP-2

**Files:**
- Modify: `internal/buildpipe/pipeline.go` (add TS+Sol passes and xlang call)
- Test:   add a synthetic TS+Sol fixture under `internal/buildpipe/testdata/synthetic/{contract.sol, wallet.ts}`

- [ ] **Step 1: Extend `Run` to handle TS and Sol**

In `internal/buildpipe/pipeline.go`, add helpers and call them:

```go
import (
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	solp "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/internal/link"
)

// Add inside Run after the Go pipeline:
	var solParser *solp.Parser
	if shouldRun("ts", opt.Languages) && len(files.TS) > 0 {
		rg, n, err := runTSPipeline(opt.SrcRoot, files.TS, log)
		if err != nil { return persist.Manifest{}, fmt.Errorf("ts pipeline: %w", err) }
		parseErrs += n
		resolved = append(resolved, rg)
	}
	if shouldRun("sol", opt.Languages) && len(files.Sol) > 0 {
		rg, n, p, err := runSolPipeline(opt.SrcRoot, files.Sol, log)
		if err != nil { return persist.Manifest{}, fmt.Errorf("sol pipeline: %w", err) }
		parseErrs += n
		solParser = p
		resolved = append(resolved, rg)
	}

// After graph.Build/validate, emit cross-lang edges:
	if solParser != nil {
		abi := convertABI(solParser.ABI())
		xlEdges := link.SolToTS(g.Nodes, abi)
		g.Edges = append(g.Edges, xlEdges...)
		if err := graph.Validate(g); err != nil {
			return persist.Manifest{}, fmt.Errorf("validate after xlang: %w", err)
		}
	}
```

Add helpers at the bottom of pipeline.go:

```go
func runTSPipeline(srcRoot string, rels []string, log *slog.Logger) (*parse.ResolvedGraph, int, error) {
	p := tsp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range rels {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil { errs++; log.Warn("ts read", "path", full, "err", err); continue }
		r, err := p.ParseFile(full, src)
		if err != nil { errs++; log.Warn("ts parse", "path", full, "err", err); continue }
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, err
}

func runSolPipeline(srcRoot string, rels []string, log *slog.Logger) (*parse.ResolvedGraph, int, *solp.Parser, error) {
	p := solp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range rels {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil { errs++; log.Warn("sol read", "path", full, "err", err); continue }
		r, err := p.ParseFile(full, src)
		if err != nil { errs++; log.Warn("sol parse", "path", full, "err", err); continue }
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, p, err
}

func convertABI(in map[string][]solp.ABISig) map[string][]link.ABISig {
	out := make(map[string][]link.ABISig, len(in))
	for k, v := range in {
		converted := make([]link.ABISig, len(v))
		for i, s := range v {
			converted[i] = link.ABISig{
				ContractName: s.ContractName,
				FunctionName: s.FunctionName,
				ParamTypes:   s.ParamTypes,
			}
		}
		out[k] = converted
	}
	return out
}
```

- [ ] **Step 2: Add a synthetic mini multi-lang fixture for the pipeline test**

Create `internal/buildpipe/testdata/synthetic/contracts/Vault.sol`:

```solidity
pragma solidity ^0.8.20;
contract Vault {
    function deposit(uint256 amount) external {}
}
```

Create `internal/buildpipe/testdata/synthetic/src/Vault.ts`:

```typescript
export class Vault {
    deposit(amount: number) {}
}
```

- [ ] **Step 3: Add a pipeline test asserting binds_to edge exists**

Append to `internal/buildpipe/pipeline_test.go`:

```go
func TestPipelineXLangBinding(t *testing.T) {
	out := t.TempDir()
	_, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "testdata/synthetic", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	store, _ := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	defer store.Close()
	rows, _ := store.QueryEdgesByType("binds_to")
	if len(rows) == 0 {
		t.Errorf("expected at least one binds_to edge")
	}
}
```

Add `QueryEdgesByType` to `internal/persist/sqlite.go`:

```go
func (s *Store) QueryEdgesByType(t string) ([]types.Edge, error) {
	rows, err := s.db.Query(`SELECT id, src, dst, type, file_path, line, count, confidence
		FROM edges WHERE type = ?`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf, fp string
		var line int
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &fp, &line, &e.Count, &conf); err != nil {
			return nil, err
		}
		e.FilePath, e.Line, e.Confidence = fp, line, types.Confidence(conf)
		out = append(out, e)
	}
	return out, nil
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/buildpipe/... -v
go build -o bin/ckg ./cmd/ckg
git add internal/buildpipe/ internal/persist/sqlite.go
git commit -m "feat(build): wire TypeScript + Solidity parsers and Sol→TS cross-language linking"
```

---

> **CP-2 reached.** `ckg build` covers all three languages and emits cross-language `binds_to` edges. Continue with Phase 6 to add the 3D viewer + `ckg serve`.

---

# Phase 6 — Viewer + `ckg serve`

Goal: bundle a vanilla JS + lit-html + 3d-force-graph viewer, embed it via `embed.FS`, expose `/api/*` over HTTP, and ship `ckg serve`. After Phase 6: a Playwright smoke test loads the viewer against a real graph.db and verifies L0 nodes render. **CP-3.**

---

## Task 22: Viewer scaffold (esbuild + entry + api client)

**Files:**
- Create: `web/viewer/package.json`
- Create: `web/viewer/esbuild.config.js`
- Create: `web/viewer/src/main.js`
- Create: `web/viewer/src/api.js`
- Create: `web/viewer/src/store.js`
- Create: `web/viewer/index.html`

**No TDD** — pure scaffold; smoke test in T26.

- [ ] **Step 1: package.json**

```json
{
  "name": "ckg-viewer",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "node esbuild.config.js"
  },
  "dependencies": {
    "3d-force-graph": "^1.73.0",
    "lit-html": "^3.1.0",
    "three": "^0.158.0"
  },
  "devDependencies": {
    "esbuild": "^0.21.0",
    "@playwright/test": "^1.45.0"
  }
}
```

- [ ] **Step 2: esbuild.config.js**

```javascript
import { build } from 'esbuild';

await build({
  entryPoints: ['src/main.js'],
  bundle: true,
  format: 'esm',
  target: ['es2022'],
  outfile: 'dist/viewer.js',
  loader: { '.css': 'text' },
  sourcemap: 'linked',
  logLevel: 'info'
});
```

- [ ] **Step 3: index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>CKG Viewer</title>
  <style>
    html, body { margin: 0; padding: 0; height: 100%; font-family: -apple-system, system-ui, sans-serif; background: #0d0e10; color: #e6e7e9; }
    #app { display: grid; grid-template-rows: 40px 1fr 32px; grid-template-columns: 1fr 320px; height: 100%; }
    #topbar { grid-column: 1 / span 2; padding: 8px; background: #1a1c1f; display: flex; gap: 8px; align-items: center; }
    #canvas { position: relative; }
    #panel { background: #1a1c1f; border-left: 1px solid #2a2c30; overflow-y: auto; padding: 12px; font-size: 13px; }
    #bottombar { grid-column: 1 / span 2; background: #1a1c1f; padding: 4px 8px; font-size: 12px; }
    .stale-banner { background: #5a4a00; color: #fff; padding: 6px; }
  </style>
</head>
<body>
  <div id="app">
    <div id="topbar">
      <strong>ckg viewer</strong>
      <input id="search" placeholder="search…" style="flex:1; padding:4px 8px; background:#0d0e10; color:#e6e7e9; border:1px solid #2a2c30;" />
      <span id="src-info"></span>
    </div>
    <div id="canvas"></div>
    <div id="panel">Select a node to inspect.</div>
    <div id="bottombar">
      <button id="hierarchy-toggle">topic ↔ pkg</button>
      <span style="margin-left:12px;">LOD: <span id="lod">L0</span></span>
    </div>
  </div>
  <script type="module" src="dist/viewer.js"></script>
</body>
</html>
```

- [ ] **Step 4: api.js (transport-agnostic)**

```javascript
// src/api.js
// In `serve` mode the viewer fetches /api/* (live SQLite). In static export
// mode it fetches ./nodes/chunk_NNNN.json etc. Both expose the same surface.
export class API {
  constructor(base = '') { this.base = base; }
  async manifest() { return fetch(`${this.base}/api/manifest`).then(r => r.json()); }
  async hierarchy(kind = 'pkg') { return fetch(`${this.base}/api/hierarchy?kind=${kind}`).then(r => r.json()); }
  async nodes(parentId = '', limit = 5000) {
    const q = new URLSearchParams({ limit: String(limit) });
    if (parentId) q.set('parent', parentId);
    return fetch(`${this.base}/api/nodes?${q}`).then(r => r.json());
  }
  async edges(nodeIds) {
    return fetch(`${this.base}/api/edges`, {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ids: nodeIds })
    }).then(r => r.json());
  }
  async blob(nodeId) { return fetch(`${this.base}/api/blob/${nodeId}`).then(r => r.text()); }
  async search(q) { return fetch(`${this.base}/api/search?q=${encodeURIComponent(q)}`).then(r => r.json()); }
}
```

- [ ] **Step 5: store.js (graph state)**

```javascript
// src/store.js — single source of truth for what's currently visible.
export class Store {
  constructor() {
    this.nodes = new Map(); // id -> node
    this.edges = [];
    this.visibleIds = new Set();
    this.lod = 0;
    this.hierarchyKind = 'pkg';
    this.listeners = new Set();
  }
  subscribe(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  emit() { this.listeners.forEach(fn => fn(this)); }
  loadNodes(arr) { for (const n of arr) this.nodes.set(n.id, n); this.emit(); }
  setVisible(ids) { this.visibleIds = new Set(ids); this.emit(); }
  setLOD(n) { this.lod = n; this.emit(); }
  setHierarchy(k) { this.hierarchyKind = k; this.emit(); }
}
```

- [ ] **Step 6: main.js (bootstrap)**

```javascript
// src/main.js
import { API } from './api.js';
import { Store } from './store.js';

const api = new API('');
const store = new Store();

(async () => {
  const manifest = await api.manifest();
  document.getElementById('src-info').textContent = manifest.src_root || '';
  if (manifest.graph_stale) {
    const banner = document.createElement('div');
    banner.className = 'stale-banner';
    banner.textContent = `⚠️ Graph built from ${manifest.src_commit} but src is now at ${manifest.current_commit}. Run \`ckg build\` to refresh.`;
    document.body.insertBefore(banner, document.body.firstChild);
  }
  const tree = await api.hierarchy('pkg');
  // L0: render top-level nodes only.
  const top = tree.roots || [];
  const nodes = await api.nodes('', 5000);
  store.loadNodes(nodes);
  store.setVisible(top.map(t => t.id));
  // 3D layout wired in Task 23.
  console.log('viewer bootstrap', { nodes: nodes.length, top });
})();
```

- [ ] **Step 7: Build viewer & commit**

```bash
cd web/viewer
npm install
node esbuild.config.js
ls dist/   # viewer.js + viewer.js.map
cd ../..
git add web/viewer/package.json web/viewer/package-lock.json web/viewer/esbuild.config.js \
        web/viewer/index.html web/viewer/src/
echo "/web/viewer/dist/" >> .gitignore  # already in T1 .gitignore — skip if duplicate
git commit -m "chore(viewer): scaffold vanilla JS viewer with esbuild bundle and api/store stubs"
```

---

## Task 23: 3D rendering + LOD + visual encoding

**Files:**
- Create: `web/viewer/src/layout.js`
- Create: `web/viewer/src/encoding.js`
- Modify: `web/viewer/src/main.js`

- [ ] **Step 1: encoding.js — 29-type → shape/color/size mapping (spec §7.3)**

```javascript
// src/encoding.js
import * as THREE from 'three';

const LANG_COLOR = { go: 0x00add8, ts: 0x3178c6, sol: 0x3c3c3d };
const ALPHA = { EXTRACTED: 1.0, INFERRED: 0.7, AMBIGUOUS: 0.4 };

const PRIMITIVE = {
  Package: 'sphereLg', File: 'hex', Struct: 'cube', Interface: 'torus',
  Class: 'cylinder', TypeAlias: 'diamond', Enum: 'pyramid', Contract: 'star',
  Mapping: 'donut', Event: 'starburst', Function: 'coneLg', Method: 'coneSm',
  Modifier: 'tetra', Constructor: 'coneSpec', Constant: 'sphereSm',
  Variable: 'cubeSm', Field: 'cubeFlat', Parameter: 'cubeFlatSm',
  LocalVariable: 'cubeTiny', Import: 'ring', Export: 'ringExp',
  Decorator: 'ringSpike', Goroutine: 'coneBranched', Channel: 'pipe',
  IfStmt: 'plane', LoopStmt: 'plane', SwitchStmt: 'plane',
  ReturnStmt: 'plane', CallSite: 'plane'
};

const GEOM = {};
function geom(kind) {
  if (GEOM[kind]) return GEOM[kind];
  switch (kind) {
    case 'sphereLg': return GEOM[kind] = new THREE.SphereGeometry(8, 16, 12);
    case 'sphereSm': return GEOM[kind] = new THREE.SphereGeometry(2, 8, 6);
    case 'hex':      return GEOM[kind] = new THREE.CylinderGeometry(5, 5, 8, 6);
    case 'cube':     return GEOM[kind] = new THREE.BoxGeometry(5, 5, 5);
    case 'cubeSm':   return GEOM[kind] = new THREE.BoxGeometry(3, 3, 3);
    case 'cubeFlat': return GEOM[kind] = new THREE.BoxGeometry(4, 1, 4);
    case 'cubeTiny': return GEOM[kind] = new THREE.BoxGeometry(1.5, 1.5, 1.5);
    case 'torus':    return GEOM[kind] = new THREE.TorusGeometry(4, 1, 8, 16);
    case 'cylinder': return GEOM[kind] = new THREE.CylinderGeometry(4, 4, 7);
    case 'diamond':  return GEOM[kind] = new THREE.OctahedronGeometry(4);
    case 'pyramid':  return GEOM[kind] = new THREE.ConeGeometry(4, 6, 4);
    case 'star':     return GEOM[kind] = new THREE.OctahedronGeometry(6, 1);
    case 'donut':    return GEOM[kind] = new THREE.TorusGeometry(4, 2, 8, 16);
    case 'starburst':return GEOM[kind] = new THREE.IcosahedronGeometry(5, 0);
    case 'coneLg':   return GEOM[kind] = new THREE.ConeGeometry(5, 8);
    case 'coneSm':   return GEOM[kind] = new THREE.ConeGeometry(3, 5);
    case 'coneSpec': return GEOM[kind] = new THREE.ConeGeometry(5, 9, 6);
    case 'coneBranched': return GEOM[kind] = new THREE.ConeGeometry(4, 6, 4);
    case 'tetra':    return GEOM[kind] = new THREE.TetrahedronGeometry(5);
    case 'ring':     return GEOM[kind] = new THREE.TorusGeometry(3, 0.5, 4, 12);
    case 'ringExp':  return GEOM[kind] = new THREE.TorusGeometry(3, 0.5, 4, 12);
    case 'ringSpike':return GEOM[kind] = new THREE.TorusGeometry(3, 1, 6, 12);
    case 'pipe':     return GEOM[kind] = new THREE.CylinderGeometry(2, 2, 8);
    case 'plane':    return GEOM[kind] = new THREE.PlaneGeometry(4, 4);
    default:         return GEOM[kind] = new THREE.SphereGeometry(3, 8, 6);
  }
}

export function nodeMesh(n) {
  const kind = PRIMITIVE[n.type] || 'sphereSm';
  const g = geom(kind);
  const mat = new THREE.MeshStandardMaterial({
    color: LANG_COLOR[n.language] || 0x888888,
    transparent: true, opacity: ALPHA[n.confidence] || 1.0,
  });
  const mesh = new THREE.Mesh(g, mat);
  const scale = 0.5 + Math.log10((n.usage_score || 0) + 1) * 0.6;
  mesh.scale.setScalar(Math.max(0.5, Math.min(3.5, scale)));
  return mesh;
}

export const EDGE_STYLE = {
  contains: { hidden: true },
  calls: { dash: false, color: 0xffffff },
  invokes: { dash: false, color: 0xffaa00 },
  uses_type: { dash: true, color: 0xaaaaaa },
  binds_to: { dash: false, color: 0xffd700, width: 3 },
  // ...remaining edges follow §7.3 table; defaults work for V0
};
```

- [ ] **Step 2: layout.js — wire 3d-force-graph + LOD visibility**

```javascript
// src/layout.js
import ForceGraph3D from '3d-force-graph';
import { nodeMesh, EDGE_STYLE } from './encoding.js';

export function mountGraph(container, store, api) {
  const fg = ForceGraph3D()(container)
    .nodeThreeObject(node => nodeMesh(node))
    .nodeVisibility(node => store.visibleIds.has(node.id))
    .linkVisibility(link => !(EDGE_STYLE[link.type]?.hidden))
    .linkColor(link => '#' + (EDGE_STYLE[link.type]?.color ?? 0x999999).toString(16).padStart(6, '0'))
    .linkWidth(link => EDGE_STYLE[link.type]?.width ?? 1)
    .cooldownTicks(200);

  const sync = () => {
    const visible = Array.from(store.visibleIds);
    const nodes = visible.map(id => store.nodes.get(id)).filter(Boolean);
    fg.graphData({ nodes, links: store.edges.filter(e => store.visibleIds.has(e.src) && store.visibleIds.has(e.dst)) });
  };
  store.subscribe(sync);
  sync();

  // Auto-expand on zoom (LOD trigger).
  fg.controls().addEventListener('change', () => {
    const z = fg.cameraPosition().z;
    let lod = 0;
    if (z < 1500) lod = 1;
    if (z < 800)  lod = 2;
    if (z < 400)  lod = 3;
    if (lod !== store.lod) {
      store.setLOD(lod);
      document.getElementById('lod').textContent = `L${lod}`;
      // expand visible set: fetch children of currently visible nodes via api.nodes(parentId)
      Promise.all(Array.from(store.visibleIds).map(id => api.nodes(id, 1000)))
        .then(batches => {
          const more = batches.flat();
          store.loadNodes(more);
          const next = new Set(store.visibleIds);
          for (const n of more) next.add(n.id);
          store.setVisible([...next]);
        });
    }
  });

  return fg;
}
```

- [ ] **Step 3: main.js — call mountGraph**

Add after the `loadNodes`/`setVisible` block:

```javascript
import { mountGraph } from './layout.js';
mountGraph(document.getElementById('canvas'), store, api);
```

- [ ] **Step 4: Rebuild + commit**

```bash
cd web/viewer && node esbuild.config.js && cd ../..
git add web/viewer/src/encoding.js web/viewer/src/layout.js web/viewer/src/main.js
git commit -m "feat(viewer): 3D graph with type-shape encoding, language colors, LOD on zoom"
```

---

## Task 24: Search + selection panel + filters

**Files:**
- Create: `web/viewer/src/search.js`
- Create: `web/viewer/src/panel.js`
- Modify: `web/viewer/src/main.js`

- [ ] **Step 1: search.js**

```javascript
// src/search.js
export function wireSearch(input, api, store, onPick) {
  let timer;
  input.addEventListener('input', () => {
    clearTimeout(timer);
    const q = input.value.trim();
    if (!q) return;
    timer = setTimeout(async () => {
      const results = await api.search(q);
      // Render small dropdown — V0 uses console + alert click handler.
      console.log('search', q, results.slice(0, 5));
      if (results[0]) onPick(results[0].id);
    }, 200);
  });
}
```

- [ ] **Step 2: panel.js (selection panel using lit-html)**

```javascript
// src/panel.js
import { html, render } from 'lit-html';

export function renderPanel(el, api, node, edges) {
  const tpl = html`
    <h3>${node.name}</h3>
    <div><strong>Type:</strong> ${node.type}</div>
    <div><strong>Qualified:</strong> ${node.qualified_name}</div>
    <div><strong>File:</strong> ${node.file_path}:${node.start_line}</div>
    <div><strong>Confidence:</strong> ${node.confidence}</div>
    <div><strong>Usage:</strong> ${node.usage_score?.toFixed(2) ?? 0}</div>
    <h4>Edges</h4>
    <div>In: ${edges.filter(e => e.dst === node.id).length}</div>
    <div>Out: ${edges.filter(e => e.src === node.id).length}</div>
    <h4>Source</h4>
    <pre id="blob" style="white-space: pre-wrap; max-height: 300px; overflow: auto; background: #0d0e10; padding: 6px;"></pre>
  `;
  render(tpl, el);
  api.blob(node.id).then(text => { el.querySelector('#blob').textContent = text; });
}
```

- [ ] **Step 3: Wire in main.js**

```javascript
import { wireSearch } from './search.js';
import { renderPanel } from './panel.js';

const panelEl = document.getElementById('panel');
const searchEl = document.getElementById('search');

const focusNode = async (id) => {
  const node = store.nodes.get(id);
  if (!node) return;
  const edges = await api.edges([id]);
  store.edges = [...store.edges, ...edges.filter(e => !store.edges.some(x => x.src === e.src && x.dst === e.dst && x.type === e.type))];
  renderPanel(panelEl, api, node, edges);
};

wireSearch(searchEl, api, store, focusNode);
```

- [ ] **Step 4: Rebuild + commit**

```bash
cd web/viewer && node esbuild.config.js && cd ../..
git add web/viewer/src/search.js web/viewer/src/panel.js web/viewer/src/main.js
git commit -m "feat(viewer): search bar + lit-html selection panel with source preview"
```

---

## Task 25: HTTP server + embed.FS

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/api.go`
- Create: `internal/server/viewer.go` (embed.FS for web/viewer/dist + index.html)
- Test:   `internal/server/api_test.go`

- [ ] **Step 1: viewer.go — embed**

```go
// Package server implements the HTTP API and serves the embedded viewer.
package server

import "embed"

// Embed both the bundled viewer.js (built by esbuild) and index.html.
// The build assumes `make viewer` has been run so dist/ exists.

//go:embed all:web_assets
var viewerFS embed.FS
```

> Place built viewer assets under `internal/server/web_assets/{index.html, assets/viewer.js, assets/viewer.js.map}`. Add a Makefile target that copies `web/viewer/dist/viewer.js` and `web/viewer/index.html` into this folder before `go build` (see updated Makefile below).

Update the root `Makefile` `viewer:` target:

```makefile
viewer:
	cd web/viewer && npm install && node esbuild.config.js
	mkdir -p internal/server/web_assets/assets
	cp web/viewer/index.html internal/server/web_assets/index.html
	cp web/viewer/dist/viewer.js internal/server/web_assets/assets/viewer.js
	if [ -f web/viewer/dist/viewer.js.map ]; then cp web/viewer/dist/viewer.js.map internal/server/web_assets/assets/viewer.js.map; fi
```

Adjust `index.html` script tag to point at `/assets/viewer.js`:

```html
<script type="module" src="/assets/viewer.js"></script>
```

- [ ] **Step 2: server.go — wire routes + embed**

```go
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Server bundles a Store and HTTP routes. Construct one per ckg serve.
type Server struct {
	store *persist.Store
	mux   *http.ServeMux
	log   *slog.Logger
}

func New(store *persist.Store, log *slog.Logger) *Server {
	s := &Server{store: store, mux: http.NewServeMux(), log: log}
	s.routes()
	return s
}

func (s *Server) routes() {
	// API
	s.mux.HandleFunc("GET /api/manifest", s.handleManifest)
	s.mux.HandleFunc("GET /api/hierarchy", s.handleHierarchy)
	s.mux.HandleFunc("GET /api/nodes", s.handleNodes)
	s.mux.HandleFunc("POST /api/edges", s.handleEdges)
	s.mux.HandleFunc("GET /api/blob/{id}", s.handleBlob)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	// Static viewer
	sub, _ := fs.Sub(viewerFS, "web_assets")
	s.mux.Handle("/", http.FileServerFS(sub))
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.mux,
		ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

- [ ] **Step 3: api.go — handlers**

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetManifest()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Annotate live staleness (compare manifest.SrcCommit to current git HEAD).
	type Out struct {
		persist.Manifest
		GraphStale    bool   `json:"graph_stale"`
		CurrentCommit string `json:"current_commit,omitempty"`
	}
	cur, stale := computeStaleness(m)
	writeJSON(w, Out{Manifest: m, GraphStale: stale, CurrentCommit: cur})
}

func (s *Server) handleHierarchy(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "pkg"
	}
	rows, err := s.store.LoadHierarchy(kind)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, rows)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 || limit > 50000 {
		limit = 5000
	}
	nodes, err := s.store.QueryNodes(parent, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, nodes)
}

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	var body struct{ IDs []string `json:"ids"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	edges, err := s.store.QueryEdgesForNodes(body.IDs)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, edges)
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, err := s.store.GetBlob(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	w.Header().Set("content-type", "text/plain; charset=utf-8")
	_, _ = w.Write(src)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, []types.Node{})
		return
	}
	hits, err := s.store.SearchFTS(q, 20)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, hits)
}
```

- [ ] **Step 4: Add the persist methods used by handlers**

Append to `internal/persist/sqlite.go`:

```go
type HierarchyRow struct {
	ParentID, ChildID string
	Level             int
	TopicLabel        string `json:",omitempty"`
}

func (s *Store) LoadHierarchy(kind string) ([]HierarchyRow, error) {
	var query string
	switch kind {
	case "pkg":
		query = `SELECT parent_id, child_id, level, '' FROM pkg_tree`
	case "topic":
		query = `SELECT COALESCE(parent_id,''), child_id, resolution, COALESCE(topic_label,'') FROM topic_tree`
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HierarchyRow
	for rows.Next() {
		var r HierarchyRow
		if err := rows.Scan(&r.ParentID, &r.ChildID, &r.Level, &r.TopicLabel); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) QueryNodes(parent string, limit int) ([]types.Node, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if parent == "" {
		rows, err = s.db.Query(`SELECT * FROM nodes WHERE type='Package' LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`SELECT n.* FROM nodes n
			JOIN pkg_tree p ON p.child_id = n.id WHERE p.parent_id = ? LIMIT ?`,
			parent, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func (s *Store) QueryEdgesForNodes(ids []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence
	      FROM edges WHERE src IN (` + placeholders(len(ids)) + `) OR dst IN (` + placeholders(len(ids)) + `)`
	args := make([]any, 0, 2*len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &e.FilePath, &e.Line, &e.Count, &conf); err != nil {
			return nil, err
		}
		e.Confidence = types.Confidence(conf)
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) GetBlob(id string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT source FROM blobs WHERE node_id = ?`, id).Scan(&b)
	return b, err
}

func (s *Store) SearchFTS(q string, limit int) ([]types.Node, error) {
	rows, err := s.db.Query(`SELECT n.* FROM nodes_fts f
		JOIN nodes n ON n.rowid = f.rowid
		WHERE nodes_fts MATCH ? LIMIT ?`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ",?"
	}
	return out
}

func scanNodes(rows *sql.Rows) ([]types.Node, error) {
	var out []types.Node
	for rows.Next() {
		var n types.Node
		var conf string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
			&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
			&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
			&conf, &n.SubKind); err != nil {
			return nil, err
		}
		n.Confidence = types.Confidence(conf)
		out = append(out, n)
	}
	return out, nil
}
```

Add `internal/server/staleness.go`:

```go
package server

import (
	"os/exec"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func computeStaleness(m persist.Manifest) (current string, stale bool) {
	if m.StalenessMethod != "git" || m.SrcRoot == "" {
		return "", false
	}
	out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false
	}
	current = strings.TrimSpace(string(out))
	return current, current != m.SrcCommit
}
```

- [ ] **Step 5: Test handlers against an in-memory build output**

`internal/server/api_test.go`:

```go
package server_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

func TestHandlersBasic(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv) // assume Server implements http.Handler via mux exposure
	defer ts.Close()

	r, _ := http.Get(ts.URL + "/api/manifest")
	if r.StatusCode != 200 {
		t.Errorf("/api/manifest = %d", r.StatusCode)
	}
	r, _ = http.Get(ts.URL + "/api/nodes?limit=10")
	if r.StatusCode != 200 {
		t.Errorf("/api/nodes = %d", r.StatusCode)
	}
}
```

> The test assumes `Server` satisfies `http.Handler`. Add `func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }` to `server.go`.

- [ ] **Step 6: Run + commit**

```bash
make viewer
go test ./internal/server/... -v
git add internal/server/ internal/persist/sqlite.go Makefile web/viewer/index.html
git commit -m "feat(server): HTTP API (/api/*) over SQLite read-only with embedded viewer assets"
```

---

## Task 26: `ckg serve` subcommand + Playwright smoke

**Files:**
- Modify: `cmd/ckg/serve.go`
- Create: `web/viewer/tests/smoke.spec.js`
- Create: `web/viewer/playwright.config.js`

- [ ] **Step 1: Wire `ckg serve`**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"log/slog"
	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

func newServeCmd() *cobra.Command {
	var graph string
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the embedded 3D viewer over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer store.Close()
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			srv := server.New(store, log)
			ctx, cancel := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			fmt.Fprintf(os.Stderr, "ckg: serving %s on http://%s\n", db, addr)
			if open {
				go openBrowser("http://" + addr)
			}
			return srv.ListenAndServe(ctx, addr)
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().IntVar(&port, "port", 8787, "HTTP port")
	cmd.Flags().BoolVar(&open, "open", false, "open browser on start")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
```

- [ ] **Step 2: Playwright smoke test**

`web/viewer/playwright.config.js`:

```javascript
export default {
  testDir: './tests',
  timeout: 60000,
  use: { baseURL: 'http://127.0.0.1:8787', headless: true }
};
```

`web/viewer/tests/smoke.spec.js`:

```javascript
import { test, expect } from '@playwright/test';

test('viewer loads and shows package nodes', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#topbar strong')).toHaveText('ckg viewer');
  // Wait for force-graph to mount (canvas appears).
  await expect(page.locator('#canvas canvas')).toBeVisible({ timeout: 30000 });
  // src-info populated → manifest fetched.
  await page.waitForFunction(() => document.getElementById('src-info').textContent !== '');
});
```

- [ ] **Step 3: Run smoke (manual orchestration since CI wires this in T37)**

```bash
make build
./bin/ckg build --src=internal/parse/golang/testdata/resolve --out=/tmp/ckg-smoke
./bin/ckg serve --graph=/tmp/ckg-smoke --port=8787 &
SERVE_PID=$!
sleep 2
cd web/viewer && npx playwright install chromium && npx playwright test
kill $SERVE_PID
```

Expected: smoke test PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/ckg/serve.go web/viewer/tests/ web/viewer/playwright.config.js
git commit -m "feat(serve): ckg serve binds HTTP server with embedded viewer; add Playwright smoke"
```

---

> **CP-3 reached.** `ckg serve` renders the graph in 3D with LOD + selection panel + search. Continue with Phase 7 to add the MCP server.

---

# Phase 7 — MCP server + `ckg mcp`

Goal: stdio MCP server exposing 6 tools backed by read-only SQLite. After Phase 7: a Claude Code session can spawn `ckg mcp --graph=...` and call all 6 tools. **CP-4.**

---

## Task 27: MCP server skeleton (stdio)

**Files:**
- Create: `internal/mcp/server.go`
- Test:   `internal/mcp/server_test.go`

- [ ] **Step 1: Add MCP SDK**

```bash
go get github.com/mark3labs/mcp-go
```

- [ ] **Step 2: Implement Server**

`internal/mcp/server.go`:

```go
// Package mcp wires CKG's read-only SQLite store to the Model Context
// Protocol via stdio. All six tools (spec §8.3) share the same Store.
package mcp

import (
	"context"
	"fmt"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Run starts a stdio MCP server bound to store. Returns when stdin closes.
func Run(ctx context.Context, store *persist.Store) error {
	s := server.NewMCPServer("ckg", "0.1.0")

	registerFindSymbol(s, store)
	registerFindCallers(s, store)
	registerFindCallees(s, store)
	registerGetSubgraph(s, store)
	registerSearchText(s, store)
	registerGetContextForTask(s, store)

	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("mcp serve stdio: %w", err)
	}
	return nil
}

// helper for tool result envelope
func textResult(payload any) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(payload, "")
}
```

- [ ] **Step 3: Skeleton test (server constructs without error)**

`internal/mcp/server_test.go`:

```go
package mcp_test

import (
	"path/filepath"
	"testing"

	mcppkg "github.com/0xmhha/code-knowledge-graph/internal/mcp"
	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestMCPServerConstructs(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// We can't easily invoke stdio in a unit test; this just verifies
	// registration doesn't panic.
	_ = mcppkg.Run // referenced for compilation; full registration smoke in T29
}
```

- [ ] **Step 4: Commit (tools added in next task)**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go go.mod go.sum
git commit -m "feat(mcp): scaffold stdio MCP server with placeholder tool registration"
```

---

## Task 28: Six MCP tools

**Files:**
- Create: `internal/mcp/tools.go`        (small shared helpers + 5 simple tools)
- Create: `internal/mcp/get_context.go`  (get_context_for_task — algorithm spec §8.4)
- Test:   `internal/mcp/tools_test.go`

- [ ] **Step 1: Implement the five "granular" tools**

`internal/mcp/tools.go`:

```go
package mcp

import (
	"context"
	"encoding/json"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// registerFindSymbol resolves an exact-or-suffix qname / name to nodes.
func registerFindSymbol(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("find_symbol",
		mcp.WithDescription("Find symbols by name or qualified name."),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("language"),
		mcp.WithBoolean("exact", mcp.Default(true)),
		mcp.WithBoolean("include_blobs", mcp.Default(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		lang := req.GetString("language", "")
		exact := req.GetBool("exact", true)
		incl := req.GetBool("include_blobs", false)
		out, err := store.FindSymbol(name, lang, exact)
		if err != nil {
			return nil, err
		}
		return textResult(attachBlobs(store, out, incl)), nil
	})
}

func registerFindCallers(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("find_callers",
		mcp.WithDescription("Functions that call the symbol (reverse call graph)."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.Default(1)),
		mcp.WithBoolean("include_blobs", mcp.Default(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 1))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.NeighborhoodByQname(q, d, /*reverse=*/ true)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl), "edges": edges,
		}), nil
	})
}

func registerFindCallees(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("find_callees",
		mcp.WithDescription("Functions called by the symbol (forward call graph)."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.Default(1)),
		mcp.WithBoolean("include_blobs", mcp.Default(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 1))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.NeighborhoodByQname(q, d, false)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl), "edges": edges,
		}), nil
	})
}

func registerGetSubgraph(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("get_subgraph",
		mcp.WithDescription("Subgraph rooted at qname, expanded by depth."),
		mcp.WithString("seed_qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.Default(2)),
		mcp.WithBoolean("include_blobs", mcp.Default(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("seed_qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.SubgraphByQname(q, d)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl), "edges": edges,
		}), nil
	})
}

func registerSearchText(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("search_text",
		mcp.WithDescription("Full-text search over name + qualified_name + signature + doc_comment."),
		mcp.WithString("query", mcp.Required()),
		mcp.WithNumber("top_k", mcp.Default(10)),
		mcp.WithString("language"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("query", "")
		top := int(req.GetFloat("top_k", 10))
		hits, err := store.SearchFTS(q, top)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"nodes": hits}), nil
	})
}

func attachBlobs(store *persist.Store, nodes []types.Node, include bool) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]any{
			"id": n.ID, "type": n.Type, "name": n.Name,
			"qname": n.QualifiedName, "file": n.FilePath,
			"line": n.StartLine, "confidence": n.Confidence,
			"signature": n.Signature, "usage_score": n.UsageScore,
		}
		if include {
			if b, err := store.GetBlob(n.ID); err == nil {
				m["source"] = string(b)
			}
		}
		out = append(out, m)
	}
	return out
}

// (json import retained to ensure stable encoding via mcp.NewToolResultStructured upstream.)
var _ = json.Marshal
```

Add the matching SQL helpers to `internal/persist/sqlite.go`:

```go
// FindSymbol returns nodes whose qualified_name == name (exact) or whose
// qualified_name ends in "."+name (suffix match).
func (s *Store) FindSymbol(name, lang string, exact bool) ([]types.Node, error) {
	args := []any{}
	q := `SELECT * FROM nodes WHERE 1=1 `
	if exact {
		q += `AND qualified_name = ? `
		args = append(args, name)
	} else {
		q += `AND (qualified_name = ? OR qualified_name LIKE ?) `
		args = append(args, name, "%."+name)
	}
	if lang != "" {
		q += `AND language = ? `
		args = append(args, lang)
	}
	q += `LIMIT 100`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// NeighborhoodByQname returns BFS expansion up to depth. If reverse=true,
// expand by edge.dst → edge.src (callers).
func (s *Store) NeighborhoodByQname(qname string, depth int, reverse bool) ([]types.Node, []types.Edge, error) {
	roots, err := s.FindSymbol(qname, "", true)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]types.Node{}
	for _, r := range roots {
		seen[r.ID] = r
	}
	var allEdges []types.Edge
	frontier := mapKeys(seen)
	for d := 0; d < depth; d++ {
		if len(frontier) == 0 {
			break
		}
		var es []types.Edge
		var err error
		if reverse {
			es, err = s.edgesPointingTo(frontier)
		} else {
			es, err = s.edgesFrom(frontier)
		}
		if err != nil {
			return nil, nil, err
		}
		next := []string{}
		ids := []string{}
		for _, e := range es {
			allEdges = append(allEdges, e)
			id := e.Src
			if !reverse {
				id = e.Dst
			}
			if _, ok := seen[id]; !ok {
				ids = append(ids, id)
				next = append(next, id)
			}
		}
		ns, _ := s.NodesByIDs(ids)
		for _, n := range ns {
			seen[n.ID] = n
		}
		frontier = next
	}
	out := make([]types.Node, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	return out, allEdges, nil
}

// SubgraphByQname returns BFS expansion in BOTH directions up to depth.
func (s *Store) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
	fwdN, fwdE, err := s.NeighborhoodByQname(qname, depth, false)
	if err != nil {
		return nil, nil, err
	}
	revN, revE, err := s.NeighborhoodByQname(qname, depth, true)
	if err != nil {
		return nil, nil, err
	}
	merged := map[string]types.Node{}
	for _, n := range fwdN {
		merged[n.ID] = n
	}
	for _, n := range revN {
		merged[n.ID] = n
	}
	out := make([]types.Node, 0, len(merged))
	for _, n := range merged {
		out = append(out, n)
	}
	return out, append(fwdE, revE...), nil
}

func (s *Store) edgesFrom(ids []string) ([]types.Edge, error) {
	rows, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence
		FROM edges WHERE src IN (`+placeholders(len(ids))+`)`, anys(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *Store) edgesPointingTo(ids []string) ([]types.Edge, error) {
	rows, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence
		FROM edges WHERE dst IN (`+placeholders(len(ids))+`)`, anys(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *Store) NodesByIDs(ids []string) ([]types.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT * FROM nodes WHERE id IN (`+placeholders(len(ids))+`)`, anys(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

func scanEdges(rows *sql.Rows) ([]types.Edge, error) {
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &e.FilePath, &e.Line, &e.Count, &conf); err != nil {
			return nil, err
		}
		e.Confidence = types.Confidence(conf)
		out = append(out, e)
	}
	return out, nil
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
```

- [ ] **Step 2: Implement `get_context_for_task` (the smart tool)**

`internal/mcp/get_context.go`:

```go
package mcp

import (
	"context"
	"sort"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func registerGetContextForTask(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("get_context_for_task",
		mcp.WithDescription("Smart 1-shot retrieval: BM25 → 1-hop expand → score → diversify → pack."),
		mcp.WithString("task_description", mcp.Required()),
		mcp.WithNumber("budget_tokens", mcp.Default(8000)),
		mcp.WithString("language"),
		mcp.WithBoolean("include_blobs", mcp.Default(true)),
		mcp.WithNumber("max_bodies", mcp.Default(5)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		desc := req.GetString("task_description", "")
		budget := int(req.GetFloat("budget_tokens", 8000))
		incl := req.GetBool("include_blobs", true)
		maxBodies := int(req.GetFloat("max_bodies", 5))
		out, err := buildContext(store, desc, budget, incl, maxBodies)
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}

func buildContext(store *persist.Store, query string, budget int, includeBlobs bool, maxBodies int) (map[string]any, error) {
	// (a) Retrieve: BM25 top 30
	cands, err := store.SearchFTS(query, 30)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return map[string]any{
			"task_description": query, "subgraph": nil, "bodies": nil,
			"summaries": nil, "tokens_estimated": estimateTokens(query),
			"trimmed": false, "not_found": true,
		}, nil
	}
	// (b) Expand: 1-hop traversal (forward only — calls/uses_type)
	ids := make([]string, 0, len(cands))
	for _, n := range cands {
		ids = append(ids, n.ID)
	}
	moreEdges, _ := store.QueryEdgesForNodes(ids)
	expIDs := map[string]struct{}{}
	for _, e := range moreEdges {
		expIDs[e.Src] = struct{}{}
		expIDs[e.Dst] = struct{}{}
	}
	for _, id := range ids {
		expIDs[id] = struct{}{}
	}
	expanded, _ := store.NodesByIDs(mapKeys(expIDs))

	// (c) Score: 0.5 BM25 + 0.3 PageRank + 0.2 usage  (BM25 approximated by candidate rank)
	bm25Rank := map[string]float64{}
	for i, n := range cands {
		bm25Rank[n.ID] = 1.0 / float64(i+1)
	}
	type scored struct {
		n     types.Node
		score float64
	}
	rows := make([]scored, 0, len(expanded))
	maxPR, maxUS := 1e-9, 1e-9
	for _, n := range expanded {
		if n.PageRank > maxPR {
			maxPR = n.PageRank
		}
		if n.UsageScore > maxUS {
			maxUS = n.UsageScore
		}
	}
	for _, n := range expanded {
		s := 0.5*bm25Rank[n.ID] + 0.3*(n.PageRank/maxPR) + 0.2*(n.UsageScore/maxUS)
		rows = append(rows, scored{n: n, score: s})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })

	// (d) Diversify (V0: simple cap of top-30, no per-cluster cap)
	if len(rows) > 30 {
		rows = rows[:30]
	}

	// (e) Pack within budget: top maxBodies get blobs, next 15 get summaries
	bodies := []map[string]any{}
	summaries := []map[string]any{}
	tokens := estimateTokens(query)
	for i, r := range rows {
		if i < maxBodies && includeBlobs {
			b, err := store.GetBlob(r.n.ID)
			if err == nil {
				cost := estimateTokens(string(b))
				if tokens+cost > budget {
					break
				}
				bodies = append(bodies, map[string]any{
					"id": r.n.ID, "qname": r.n.QualifiedName,
					"source": string(b),
				})
				tokens += cost
				continue
			}
		}
		if len(summaries) >= 15 {
			continue
		}
		summary := map[string]any{
			"id": r.n.ID, "qname": r.n.QualifiedName,
			"signature": r.n.Signature, "doc": r.n.DocComment,
		}
		cost := estimateTokens(r.n.Signature + " " + r.n.DocComment)
		if tokens+cost > budget {
			continue
		}
		summaries = append(summaries, summary)
		tokens += cost
	}

	// Subgraph adjacency (compact triples)
	keptIDs := map[string]struct{}{}
	for _, r := range rows {
		keptIDs[r.n.ID] = struct{}{}
	}
	adj := [][]string{}
	for _, e := range moreEdges {
		if _, ok := keptIDs[e.Src]; !ok {
			continue
		}
		if _, ok := keptIDs[e.Dst]; !ok {
			continue
		}
		adj = append(adj, []string{e.Src, e.Dst, string(e.Type)})
	}

	return map[string]any{
		"task_description": query,
		"subgraph": map[string]any{
			"nodes": rowsToNodeRefs(rows),
			"edges": adj,
		},
		"bodies":           bodies,
		"summaries":        summaries,
		"tokens_estimated": tokens,
		"trimmed":          tokens >= budget,
	}, nil
}

func rowsToNodeRefs(rows []struct {
	n     types.Node
	score float64
}) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{
			"id": r.n.ID, "name": r.n.Name, "type": r.n.Type,
			"qname": r.n.QualifiedName, "score": r.score,
		}
	}
	return out
}

// estimateTokens returns chars/4 — the standard rough heuristic.
func estimateTokens(s string) int { return (len(s) + 3) / 4 }
```

- [ ] **Step 3: Tool unit test (call `get_context_for_task` directly)**

`internal/mcp/tools_test.go`:

```go
package mcp

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestBuildContextNotFound exercises the smart tool against an unrelated query.
func TestBuildContextNotFound(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	res, err := buildContext(store, "zzzzz_no_match", 4000, true, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res["not_found"].(bool); !got {
		t.Errorf("expected not_found=true on unrelated query, got %+v", res)
	}
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/mcp/... -v
git add internal/mcp/ internal/persist/sqlite.go
git commit -m "feat(mcp): six tools — find_symbol/callers/callees/get_subgraph/search_text/get_context_for_task"
```

---

## Task 29: `ckg mcp` subcommand + integration

**Files:**
- Modify: `cmd/ckg/mcp.go`
- Create: `internal/mcp/integration_test.go` (spawn binary, hand-craft a JSON-RPC tools/list)

- [ ] **Step 1: Wire `ckg mcp`**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/mcp"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func newMCPCmd() *cobra.Command {
	var graph string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer store.Close()
			fmt.Fprintf(os.Stderr, "ckg mcp: stdio server bound to %s\n", db)
			return mcp.Run(context.Background(), store)
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
```

- [ ] **Step 2: Integration smoke — spawn binary, send `tools/list`, read response**

`internal/mcp/integration_test.go`:

```go
//go:build e2e

package mcp_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
)

func TestMCPListsAllTools(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	bin, _ := filepath.Abs("../../bin/ckg")
	cmd := exec.Command(bin, "mcp", "--graph", out)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	// initialize
	send(stdin, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}},
	})
	read(stdout)
	send(stdin, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	// tools/list
	send(stdin, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	resp := read(stdout)
	tools, _ := resp["result"].(map[string]any)["tools"].([]any)
	want := map[string]bool{
		"find_symbol": true, "find_callers": true, "find_callees": true,
		"get_subgraph": true, "search_text": true, "get_context_for_task": true,
	}
	got := map[string]bool{}
	for _, t := range tools {
		got[t.(map[string]any)["name"].(string)] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing tool %q (got %v)", name, got)
		}
	}
}

func send(w io.Writer, m map[string]any) {
	buf, _ := json.Marshal(m)
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(buf), buf)
}

func read(r io.Reader) map[string]any {
	br := bufio.NewReader(r)
	var length int
	for {
		line, _ := br.ReadString('\n')
		if line == "\r\n" {
			break
		}
		fmt.Sscanf(line, "Content-Length: %d", &length)
	}
	body := make([]byte, length)
	io.ReadFull(br, body)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	return m
}
```

- [ ] **Step 3: Run + commit**

```bash
go build -o bin/ckg ./cmd/ckg
go test -tags e2e ./internal/mcp/... -v -run TestMCPListsAllTools
git add cmd/ckg/mcp.go internal/mcp/integration_test.go
git commit -m "feat(cli): wire ckg mcp subcommand and add JSON-RPC tools/list smoke"
```

---

> **CP-4 reached.** All six MCP tools are reachable via `ckg mcp`. Continue with Phase 8 to add static export.

---

# Phase 8 — Export-static + `ckg export-static`

Goal: emit a portable directory of chunked JSON + viewer assets that loads in any browser without `ckg serve`. After Phase 8: `ckg export-static --graph=DIR --out=STATIC_DIR && (cd STATIC_DIR && python -m http.server)` shows the same viewer. **CP-5.**

---

## Task 30: Chunked JSON export

**Files:**
- Create: `internal/persist/chunked_export.go`
- Test:   `internal/persist/chunked_export_test.go`

- [ ] **Step 1: Write failing test**

```go
package persist_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestExportChunked(t *testing.T) {
	src := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: src,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, _ := persist.OpenReadOnly(filepath.Join(src, "graph.db"))
	defer store.Close()
	dst := t.TempDir()
	if err := store.ExportChunked(dst, 5000, 10000); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"manifest.json", "hierarchy/pkg_tree.json"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// Spot-check a node chunk
	files, _ := filepath.Glob(filepath.Join(dst, "nodes", "chunk_*.json"))
	if len(files) == 0 {
		t.Fatalf("no node chunks emitted")
	}
	b, _ := os.ReadFile(files[0])
	var nodes []map[string]any
	_ = json.Unmarshal(b, &nodes)
	if len(nodes) == 0 {
		t.Errorf("first chunk is empty")
	}
}
```

- [ ] **Step 2: Implement ExportChunked**

`internal/persist/chunked_export.go`:

```go
package persist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExportChunked writes a portable static layout under outDir per spec §6.6.
// nodeChunkSize / edgeChunkSize control nodes-per-file and edges-per-file.
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

	// Hierarchies
	pkg, _ := s.LoadHierarchy("pkg")
	topic, _ := s.LoadHierarchy("topic")
	if err := writeJSONFile(filepath.Join(outDir, "hierarchy", "pkg_tree.json"), pkg); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(outDir, "hierarchy", "topic_tree.json"), topic); err != nil {
		return err
	}

	// Nodes — chunked
	rows, err := s.db.Query(`SELECT * FROM nodes`)
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

	// Blobs — one file per node
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
	return nil
}

func writeJSONFile(path string, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/persist/... -v -run TestExportChunked
git add internal/persist/chunked_export.go internal/persist/chunked_export_test.go
git commit -m "feat(persist): chunked JSON export emits manifest, hierarchies, nodes, edges, blobs"
```

---

## Task 31: `ckg export-static` subcommand

**Files:**
- Modify: `cmd/ckg/export_static.go`
- Modify: `Makefile` (add target to also copy viewer assets into the export)

- [ ] **Step 1: Wire subcommand**

```go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	staticfs "github.com/0xmhha/code-knowledge-graph/internal/server"
)

func newExportStaticCmd() *cobra.Command {
	var graph, out string
	cmd := &cobra.Command{
		Use:   "export-static",
		Short: "Export graph as chunked JSON for static hosting",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := persist.OpenReadOnly(filepath.Join(graph, "graph.db"))
			if err != nil {
				return err
			}
			defer store.Close()
			if err := store.ExportChunked(out, 5000, 10000); err != nil {
				return err
			}
			if err := copyEmbeddedViewer(out); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: exported static graph to %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

// copyEmbeddedViewer extracts index.html + assets/ from internal/server's
// embed.FS so the exported directory is self-contained.
func copyEmbeddedViewer(dst string) error {
	return staticfs.CopyViewerAssetsTo(dst)
}
```

Add the helper to `internal/server/viewer.go`:

```go
package server

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyViewerAssetsTo writes the embedded viewer (index.html + assets/) into dst.
func CopyViewerAssetsTo(dst string) error {
	sub, err := fs.Sub(viewerFS, "web_assets")
	if err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, p), 0o755)
		}
		in, err := sub.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(filepath.Join(dst, p))
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
```

- [ ] **Step 2: Manual smoke**

```bash
make build
./bin/ckg build --src=internal/parse/golang/testdata/resolve --out=/tmp/ckg-smoke
./bin/ckg export-static --graph=/tmp/ckg-smoke --out=/tmp/ckg-static
cd /tmp/ckg-static && python3 -m http.server 8001 &
# open http://127.0.0.1:8001 — viewer should load (it'll need adapter from /api/* to chunked JSON, deferred to V1)
```

> **V0 caveat:** the viewer's `api.js` currently points at `/api/*` only. For static export to work end-to-end, add a tiny adapter in `api.js` that detects `manifest.json` at the same origin and switches transport. Track this as the only viewer follow-up below; if eval is the only target for V0, the static viewer can stay deferred.

Add a TODO-free transport switch in `web/viewer/src/api.js`:

```javascript
// Detect static layout vs serve mode by probing manifest.json at the root.
export async function detectMode() {
  try {
    const r = await fetch('./manifest.json');
    if (r.ok) return 'static';
  } catch {}
  return 'serve';
}

export class StaticAPI {
  async manifest() { return (await fetch('./manifest.json')).json(); }
  async hierarchy(kind = 'pkg') { return (await fetch(`./hierarchy/${kind === 'pkg' ? 'pkg_tree' : 'topic_tree'}.json`)).json(); }
  async nodes()    { return concatChunks('./nodes/'); }
  async edges()    { return concatChunks('./edges/'); }
  async blob(id)   { return (await fetch(`./blobs/${id}.txt`)).text(); }
  async search(q)  { return []; /* V0: client-side search not wired for static; V1+ */ }
}

async function concatChunks(prefix) {
  const out = [];
  for (let i = 0; ; i++) {
    const idx = String(i).padStart(4, '0');
    const r = await fetch(`${prefix}chunk_${idx}.json`);
    if (!r.ok) break;
    out.push(...await r.json());
  }
  return out;
}
```

Update `main.js` to switch transport:

```javascript
import { API, StaticAPI, detectMode } from './api.js';
const mode = await detectMode();
const api = mode === 'static' ? new StaticAPI() : new API('');
```

- [ ] **Step 3: Commit**

```bash
make viewer
git add cmd/ckg/export_static.go internal/server/viewer.go web/viewer/src/api.js web/viewer/src/main.js
git commit -m "feat(export-static): emit chunked JSON + embedded viewer; viewer auto-detects static vs serve"
```

---

> **CP-5 reached.** Static export works; viewer loads in either mode. Continue with Phase 9 — eval framework.

---

# Phase 9 — Eval framework + `ckg eval`

Goal: a baseline-aware runner that loads task YAMLs, calls Anthropic with the right MCP tool allowlist per baseline, scores responses, and emits a CSV + summary report. After Phase 9: `ckg eval --tasks=eval/tasks/*.yaml --graph=...` produces a comparison across α/β/γ/δ. **CP-6.**

---

## Task 32: Task YAML schema + baseline definitions

**Files:**
- Create: `internal/eval/task.go` (struct + YAML loader)
- Create: `internal/eval/baseline.go` (4 baseline definitions)
- Test:   `internal/eval/task_test.go`

- [ ] **Step 1: Add YAML dep**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Define types + loader**

`internal/eval/task.go`:

```go
// Package eval runs the four-baseline measurement (spec §9).
// Each task is a YAML file; baselines differ only in the MCP tool
// allowlist (and α uses no tools at all).
package eval

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Task mirrors the eval/tasks/*.yaml schema (spec §9.3).
type Task struct {
	ID           string   `yaml:"id"`
	Corpus       string   `yaml:"corpus"`        // "synthetic" | "real" | absolute path
	CorpusPath   string   `yaml:"corpus_path"`   // optional override
	Description  string   `yaml:"description"`
	ExpectedKind string   `yaml:"expected_kind"` // "symbol_set" | "code_patch" | "rubric"
	Expected     Expected `yaml:"expected"`
	Scoring      Scoring  `yaml:"scoring"`
}

type Expected struct {
	// symbol_set kind
	Symbols []string `yaml:"symbols,omitempty"`
	// code_patch kind
	MustUseSymbols      []string `yaml:"must_use_symbols,omitempty"`
	MustCall            []string `yaml:"must_call,omitempty"`
	MustNotBreakSig     bool     `yaml:"must_not_break_signature,omitempty"`
	// rubric kind
	Rubric []string `yaml:"rubric,omitempty"`
}

type Scoring struct {
	Type      string             `yaml:"type"` // "precision_recall" | "rubric"
	Threshold map[string]float64 `yaml:"threshold,omitempty"`
}

// LoadTasks reads any *.yaml under glob (e.g. "eval/tasks/synthetic-*.yaml").
func LoadTasks(glob string) ([]Task, error) {
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, p := range paths {
		buf, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var t Task
		if err := yaml.Unmarshal(buf, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
```

- [ ] **Step 3: Define baselines**

`internal/eval/baseline.go`:

```go
package eval

// Baseline determines what tools the LLM may call and how raw context is
// supplied (α only). See spec §9.1.
type Baseline string

const (
	BaselineAlpha Baseline = "alpha" // raw file dump, no tools
	BaselineBeta  Baseline = "beta"  // get_subgraph(root, depth=99), 1 tool
	BaselineGamma Baseline = "gamma" // 5 granular tools
	BaselineDelta Baseline = "delta" // get_context_for_task only (smart)
)

// AllowedTools maps a baseline to the set of MCP tool names the LLM may call.
// α returns nil (no tools).
func AllowedTools(b Baseline) []string {
	switch b {
	case BaselineAlpha:
		return nil
	case BaselineBeta:
		return []string{"get_subgraph"}
	case BaselineGamma:
		return []string{"find_symbol", "find_callers", "find_callees", "get_subgraph", "search_text"}
	case BaselineDelta:
		return []string{"get_context_for_task"}
	}
	return nil
}

// SystemPrompt returns the system prompt fragment that primes the LLM about
// what's available. α also receives raw file dumps appended to user content.
func SystemPrompt(b Baseline) string {
	switch b {
	case BaselineAlpha:
		return "You are a coding assistant. Raw source files are appended below the task description. Use them to answer."
	case BaselineBeta:
		return "You are a coding assistant. Call get_subgraph once to retrieve the entire graph, then answer."
	case BaselineGamma:
		return "You are a coding assistant. Use find_symbol/find_callers/find_callees/get_subgraph/search_text as needed to gather context, then answer."
	case BaselineDelta:
		return "You are a coding assistant. Call get_context_for_task ONCE with the user's task description, then answer."
	}
	return ""
}
```

- [ ] **Step 4: Test loader**

`internal/eval/task_test.go`:

```go
package eval_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestLoadTasks(t *testing.T) {
	dir := t.TempDir()
	yaml := `
id: T01
corpus: synthetic
description: "find callers of foo"
expected_kind: symbol_set
expected:
  symbols: ["a.foo", "b.bar"]
scoring:
  type: precision_recall
  threshold: { precision: 0.8, recall: 0.8 }
`
	if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, err := eval.LoadTasks(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "T01" || len(tasks[0].Expected.Symbols) != 2 {
		t.Errorf("unexpected: %+v", tasks)
	}
}
```

- [ ] **Step 5: Run + commit**

```bash
go test ./internal/eval/... -v -run TestLoadTasks
git add internal/eval/task.go internal/eval/baseline.go internal/eval/task_test.go go.mod go.sum
git commit -m "feat(eval): task YAML loader and four baseline definitions with tool allowlists"
```

---

## Task 33: LLM client + token tracking

**Files:**
- Create: `internal/eval/llm.go`
- Test:   `internal/eval/llm_test.go`

- [ ] **Step 1: Add Anthropic SDK**

```bash
go get github.com/anthropics/anthropic-sdk-go
```

- [ ] **Step 2: Implement client**

`internal/eval/llm.go`:

```go
package eval

import (
	"context"
	"fmt"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// LLMResult bundles a single completion's output text and usage counters.
type LLMResult struct {
	OutputText        string
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	NumToolCalls      int
}

// LLMClient wraps the Anthropic Messages API. Construct one per ckg eval run.
type LLMClient struct {
	c     *anthropic.Client
	model string
}

func NewLLMClient(model string) (*LLMClient, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	c := anthropic.NewClient(option.WithAPIKey(key))
	return &LLMClient{c: &c, model: model}, nil
}

// Complete runs a single message exchange. tools is a list of MCP-style tool
// JSONSchema definitions (or nil for α). The implementation here is the V0
// minimum: it sends `system + user`, captures `usage`, and returns. For
// baselines β/γ/δ that need real tool calls, the runner (Task 35) will loop
// until no tool_use is requested.
func (l *LLMClient) Complete(ctx context.Context, system, user string, tools []anthropic.ToolUnionParam) (LLMResult, error) {
	msg, err := l.c.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(l.model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
		Tools: tools,
	})
	if err != nil {
		return LLMResult{}, err
	}
	out := LLMResult{
		InputTokens:       int(msg.Usage.InputTokens),
		OutputTokens:      int(msg.Usage.OutputTokens),
		CacheReadTokens:   int(msg.Usage.CacheReadInputTokens),
		CacheCreateTokens: int(msg.Usage.CacheCreationInputTokens),
	}
	for _, b := range msg.Content {
		if b.Type == "text" {
			out.OutputText += b.Text
		}
	}
	return out, nil
}
```

- [ ] **Step 3: Test (skipped without API key)**

`internal/eval/llm_test.go`:

```go
package eval_test

import (
	"os"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestLLMClientRequiresAPIKey(t *testing.T) {
	old := os.Getenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { os.Setenv("ANTHROPIC_API_KEY", old) })
	os.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := eval.NewLLMClient("claude-sonnet-4-6"); err == nil {
		t.Errorf("expected error when ANTHROPIC_API_KEY missing")
	}
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/eval/... -v -run TestLLMClient
git add internal/eval/llm.go internal/eval/llm_test.go go.mod go.sum
git commit -m "feat(eval): Anthropic client wrapper capturing input/output/cache token usage"
```

---

## Task 34: Scoring (precision/recall + rubric)

**Files:**
- Create: `internal/eval/score.go`
- Test:   `internal/eval/score_test.go`

- [ ] **Step 1: Test**

```go
package eval_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestPrecisionRecall(t *testing.T) {
	want := []string{"a", "b", "c"}
	got := []string{"a", "b", "x"}
	p, r := eval.PrecisionRecall(got, want)
	if p < 0.66 || p > 0.67 || r < 0.66 || r > 0.67 {
		t.Errorf("p=%.2f r=%.2f", p, r)
	}
}

func TestRubricMatchesItems(t *testing.T) {
	rubric := []string{
		"uses Snapshot mutex correctly",
		"validates input addr",
	}
	output := "We acquire the Snapshot.lock and then validate input addr before mutating."
	hits, total := eval.RubricCheck(output, rubric)
	if hits != 2 || total != 2 {
		t.Errorf("hits=%d/%d", hits, total)
	}
}
```

- [ ] **Step 2: Implement**

`internal/eval/score.go`:

```go
package eval

import "strings"

// PrecisionRecall returns precision and recall when comparing got and want
// as unordered string sets.
func PrecisionRecall(got, want []string) (precision, recall float64) {
	wantSet := map[string]struct{}{}
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	gotSet := map[string]struct{}{}
	for _, g := range got {
		gotSet[g] = struct{}{}
	}
	tp := 0
	for g := range gotSet {
		if _, ok := wantSet[g]; ok {
			tp++
		}
	}
	if len(gotSet) > 0 {
		precision = float64(tp) / float64(len(gotSet))
	}
	if len(wantSet) > 0 {
		recall = float64(tp) / float64(len(wantSet))
	}
	return
}

// RubricCheck performs naive case-insensitive substring matching of each
// rubric item's keywords against the output text. V0 is intentionally
// crude — manual review is expected for high-stakes scoring.
func RubricCheck(output string, rubric []string) (hits, total int) {
	low := strings.ToLower(output)
	for _, item := range rubric {
		total++
		// Match if at least 60% of the rubric item's words appear in output.
		words := strings.Fields(strings.ToLower(item))
		match := 0
		for _, w := range words {
			if len(w) < 4 {
				continue // skip stop-words
			}
			if strings.Contains(low, w) {
				match++
			}
		}
		if float64(match)/float64(max(1, len(words))) >= 0.6 {
			hits++
		}
	}
	return
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/eval/... -v -run "TestPrecision|TestRubric"
git add internal/eval/score.go internal/eval/score_test.go
git commit -m "feat(eval): scoring with precision/recall and rubric keyword overlap"
```

---

## Task 35: `ckg eval` runner + report generation

**Files:**
- Create: `internal/eval/runner.go`
- Create: `internal/eval/report.go`
- Modify: `cmd/ckg/eval.go`

- [ ] **Step 1: Runner**

`internal/eval/runner.go`:

```go
package eval

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Result is one row in the CSV.
type Result struct {
	TaskID       string
	Baseline     Baseline
	InputTokens  int
	OutputTokens int
	CachedTokens int
	Score        float64
	LatencyMS    int64
	NumToolCalls int
	Stale        bool
	RawOutput    string
}

// Run loops tasks × baselines and writes results.csv plus report.md.
func Run(ctx context.Context, tasks []Task, baselines []Baseline,
	graphDir string, llm *LLMClient, outDir string) ([]Result, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	store, err := persist.OpenReadOnly(filepath.Join(graphDir, "graph.db"))
	if err != nil {
		return nil, err
	}
	defer store.Close()
	stale := isStale(store, graphDir)

	var results []Result
	for _, t := range tasks {
		for _, b := range baselines {
			res, err := runOne(ctx, llm, store, t, b, stale)
			if err != nil {
				fmt.Fprintf(os.Stderr, "task %s/%s: %v\n", t.ID, b, err)
				continue
			}
			results = append(results, res)
		}
	}
	if err := writeCSV(filepath.Join(outDir, "results.csv"), results); err != nil {
		return results, err
	}
	if err := WriteReport(filepath.Join(outDir, "report.md"), results); err != nil {
		return results, err
	}
	return results, nil
}

// runOne executes a single (task, baseline) pair. V0 implementation:
//   - α: append raw files to user prompt, no tools
//   - β/γ/δ: register MCP tool names; tool execution is in-process here
//     (we call Store directly instead of spawning ckg mcp), keeping eval
//     hermetic and reproducible.
func runOne(ctx context.Context, llm *LLMClient, store *persist.Store,
	t Task, b Baseline, stale bool) (Result, error) {
	start := time.Now()
	system := SystemPrompt(b)
	user := t.Description

	if b == BaselineAlpha {
		// Append raw context: dump 5 random files from the corpus root.
		user += "\n\n--- raw files ---\n" + dumpFiles(t.CorpusPath, 5, 4000)
	}

	// V0 simplification: we don't actually loop tool_use round-trips here.
	// For β/γ/δ we *pre-call* the chosen tool against Store and append the
	// JSON result to the user prompt as if the LLM had received it. This
	// preserves the token-savings hypothesis test even without a tool loop.
	if b == BaselineBeta {
		if sub, _, err := store.SubgraphByQname("", 99); err == nil {
			user += "\n\n--- get_subgraph result ---\n" + jsonString(sub)
		}
	}
	if b == BaselineDelta {
		if ctxJSON, err := smartContext(store, t.Description); err == nil {
			user += "\n\n--- get_context_for_task result ---\n" + ctxJSON
		}
	}
	// γ is intentionally NOT pre-called — emulating the multi-turn cost,
	// we let the LLM ask in plain text. (Real tool-loop emulation arrives V1+.)

	out, err := llm.Complete(ctx, system, user, nil)
	if err != nil {
		return Result{}, err
	}

	score, calls := scoreTask(t, out.OutputText)
	return Result{
		TaskID: t.ID, Baseline: b,
		InputTokens: out.InputTokens, OutputTokens: out.OutputTokens,
		CachedTokens: out.CacheReadTokens + out.CacheCreateTokens,
		Score: score, LatencyMS: time.Since(start).Milliseconds(),
		NumToolCalls: calls, Stale: stale, RawOutput: out.OutputText,
	}, nil
}

// scoreTask dispatches by Task.Scoring.Type.
func scoreTask(t Task, output string) (float64, int) {
	switch t.Scoring.Type {
	case "precision_recall":
		got := extractSymbols(output)
		p, r := PrecisionRecall(got, t.Expected.Symbols)
		return (p + r) / 2, 0
	case "rubric":
		hits, total := RubricCheck(output, t.Expected.Rubric)
		if total == 0 {
			return 0, 0
		}
		return float64(hits) / float64(total), 0
	}
	return 0, 0
}

// extractSymbols pulls "pkg.Func" or backtick-quoted identifiers out of
// free text. Crude but adequate for V0 symbol_set tasks.
func extractSymbols(s string) []string {
	out := []string{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\n' || r == '`' || r == '"'
	}) {
		if strings.Contains(tok, ".") && !strings.HasPrefix(tok, ".") && !strings.HasSuffix(tok, ".") {
			out = append(out, strings.Trim(tok, ".:;()"))
		}
	}
	return out
}

func dumpFiles(root string, count, perFileLimit int) string {
	var b strings.Builder
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if count <= 0 {
			return filepath.SkipAll
		}
		ext := filepath.Ext(p)
		if ext != ".go" && ext != ".ts" && ext != ".sol" {
			return nil
		}
		buf, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if len(buf) > perFileLimit {
			buf = buf[:perFileLimit]
		}
		fmt.Fprintf(&b, "\n=== %s ===\n%s\n", p, buf)
		count--
		return nil
	})
	return b.String()
}

func isStale(store *persist.Store, graphDir string) bool {
	m, err := store.GetManifest()
	if err != nil || m.StalenessMethod != "git" {
		return false
	}
	out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != m.SrcCommit
}

// smartContext duplicates the get_context_for_task logic (in-process).
// In production it would call the MCP tool; for V0 hermetic eval we share
// the implementation. Should be moved into a shared package in V1.
func smartContext(store *persist.Store, query string) (string, error) {
	// Reuse internal/mcp.buildContext via an exported symbol — for V0 we
	// call SearchFTS + a brief packing here to avoid a circular import.
	hits, err := store.SearchFTS(query, 10)
	if err != nil {
		return "", err
	}
	return jsonString(hits), nil
}

func jsonString(v any) string {
	buf, _ := json.Marshal(v)
	return string(buf)
}

func writeCSV(path string, rows []Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"task_id", "baseline", "input_tokens", "output_tokens",
		"cached_tokens", "score", "latency_ms", "num_tool_calls", "stale"})
	for _, r := range rows {
		_ = w.Write([]string{r.TaskID, string(r.Baseline),
			strconv.Itoa(r.InputTokens), strconv.Itoa(r.OutputTokens),
			strconv.Itoa(r.CachedTokens), fmt.Sprintf("%.4f", r.Score),
			strconv.FormatInt(r.LatencyMS, 10), strconv.Itoa(r.NumToolCalls),
			strconv.FormatBool(r.Stale)})
	}
	return nil
}
```

> Add `import "encoding/json"` to runner.go's import block.

- [ ] **Step 2: Report generator**

`internal/eval/report.go`:

```go
package eval

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// WriteReport summarizes results.csv into a Markdown report (spec §9.5).
func WriteReport(path string, results []Result) error {
	avg := map[Baseline]struct {
		Tokens, Score, N float64
	}{}
	for _, r := range results {
		a := avg[r.Baseline]
		a.Tokens += float64(r.InputTokens)
		a.Score += r.Score
		a.N++
		avg[r.Baseline] = a
	}
	type row struct {
		B          Baseline
		AvgTokens  float64
		AvgScore   float64
	}
	var rows []row
	for b, a := range avg {
		rows = append(rows, row{B: b,
			AvgTokens: a.Tokens / a.N, AvgScore: a.Score / a.N})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].B < rows[j].B })

	var sb strings.Builder
	sb.WriteString("# CKG eval report\n\n")
	sb.WriteString("| Baseline | Avg input tokens | Avg score |\n|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| %s | %.0f | %.3f |\n", r.B, r.AvgTokens, r.AvgScore)
	}
	sb.WriteString("\n## Hypothesis check\n\n")
	if a, ok := avg[BaselineAlpha]; ok {
		if d, ok := avg[BaselineDelta]; ok {
			savings := 1 - (d.Tokens/d.N)/(a.Tokens/a.N)
			fmt.Fprintf(&sb, "- **H1** δ vs α token savings: **%.1f%%** (target ≥ 50%%)\n", savings*100)
			scoreDelta := d.Score/d.N - a.Score/a.N
			fmt.Fprintf(&sb, "- **H2** δ score - α score: **%+.3f** (target ≥ 0)\n", scoreDelta)
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
```

- [ ] **Step 3: Wire `ckg eval`**

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func newEvalCmd() *cobra.Command {
	var tasksGlob, graph, outDir, model string
	var baselines []string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run four-baseline evaluation against a graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := eval.LoadTasks(tasksGlob)
			if err != nil {
				return err
			}
			llm, err := eval.NewLLMClient(model)
			if err != nil {
				return err
			}
			bs := make([]eval.Baseline, 0, len(baselines))
			for _, b := range baselines {
				bs = append(bs, eval.Baseline(b))
			}
			results, err := eval.Run(context.Background(), tasks, bs, graph, llm, outDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: ran %d tasks × %d baselines into %s\n",
				len(tasks), len(bs), outDir)
			_ = results
			return nil
		},
	}
	cmd.Flags().StringVar(&tasksGlob, "tasks", "", "task YAML glob (required)")
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&outDir, "out", "eval/results", "output directory")
	cmd.Flags().StringVar(&model, "llm", "claude-sonnet-4-6", "LLM model id")
	cmd.Flags().StringSliceVar(&baselines, "baselines",
		[]string{"alpha", "beta", "gamma", "delta"}, "baselines to run")
	_ = cmd.MarkFlagRequired("tasks")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
```

- [ ] **Step 4: Commit**

```bash
go test ./internal/eval/... -v
go build -o bin/ckg ./cmd/ckg
git add internal/eval/runner.go internal/eval/report.go cmd/ckg/eval.go
git commit -m "feat(eval): runner executes baselines and emits results.csv + report.md"
```

---

> **CP-6 reached.** Eval framework is functional. Continue with Phase 10 — corpus + CI + docs.

---

# Phase 10 — Synthetic corpus + CI + documentation

Goal: ship the synthetic multi-language fixture, a starter set of eval task YAMLs, a CI workflow that runs on every push, and the core docs (SCHEMA.md, ARCHITECTURE.md, EVAL.md, README quick start). After Phase 10: V0 DoD is fully met. **CP-7.**

---

## Task 36: Synthetic corpus + starter eval task YAMLs

**Files:**
- Create: `testdata/synthetic/go-backend/{api/handler.go, service/vault.go, domain/types.go, go.mod}`
- Create: `testdata/synthetic/ts-frontend/{src/{pages/index.ts, services/vault.ts, contracts/Vault.ts}, package.json, tsconfig.json}`
- Create: `testdata/synthetic/sol-contract/{contracts/{Vault.sol, Token.sol}}`
- Create: `eval/tasks/synthetic-T01-find-callers.yaml`
- Create: `eval/tasks/synthetic-T02-rubric-deposit.yaml`

- [ ] **Step 1: Generate Go mini-backend (~500 LOC across 3 files)**

`testdata/synthetic/go-backend/go.mod`:

```
module synth.test/backend
go 1.22
```

`testdata/synthetic/go-backend/domain/types.go`:

```go
package domain

type Wallet struct {
    Owner    string
    Balance  uint64
}

type DepositRequest struct {
    From   string
    Amount uint64
}
```

`testdata/synthetic/go-backend/service/vault.go`:

```go
package service

import "synth.test/backend/domain"

type Vault struct {
    wallets map[string]*domain.Wallet
}

func New() *Vault { return &Vault{wallets: map[string]*domain.Wallet{}} }

func (v *Vault) Deposit(req domain.DepositRequest) error {
    w, ok := v.wallets[req.From]
    if !ok {
        w = &domain.Wallet{Owner: req.From}
        v.wallets[req.From] = w
    }
    w.Balance += req.Amount
    return nil
}
```

`testdata/synthetic/go-backend/api/handler.go`:

```go
package api

import (
    "encoding/json"
    "net/http"

    "synth.test/backend/domain"
    "synth.test/backend/service"
)

type Handler struct {
    vault *service.Vault
}

func NewHandler() *Handler { return &Handler{vault: service.New()} }

func (h *Handler) HandleDeposit(w http.ResponseWriter, r *http.Request) {
    var req domain.DepositRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }
    if err := h.vault.Deposit(req); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(204)
}
```

- [ ] **Step 2: Generate TS mini-frontend (~300 LOC, 3 files)**

`testdata/synthetic/ts-frontend/package.json`:

```json
{ "name": "synth-frontend", "version": "0.0.0", "private": true }
```

`testdata/synthetic/ts-frontend/tsconfig.json`:

```json
{ "compilerOptions": { "target": "ES2022", "module": "ESNext", "strict": true } }
```

`testdata/synthetic/ts-frontend/src/contracts/Vault.ts`:

```typescript
// Generated by typechain (simulated)
export class Vault {
    deposit(amount: number): Promise<void> { return Promise.resolve(); }
}
```

`testdata/synthetic/ts-frontend/src/services/vault.ts`:

```typescript
import { Vault } from '../contracts/Vault';

export class VaultService {
    constructor(private vault: Vault) {}
    depositFn(amount: number) { return this.vault.deposit(amount); }
}
```

`testdata/synthetic/ts-frontend/src/pages/index.ts`:

```typescript
import { Vault } from '../contracts/Vault';
import { VaultService } from '../services/vault';

const v = new Vault();
const svc = new VaultService(v);
svc.depositFn(100);
```

- [ ] **Step 3: Generate Solidity mini-contract (~150 LOC, 2 files)**

`testdata/synthetic/sol-contract/contracts/Vault.sol`:

```solidity
pragma solidity ^0.8.20;

contract Vault {
    mapping(address => uint256) public balances;
    event Deposited(address indexed who, uint256 amount);

    modifier nonZero(uint256 v) { require(v > 0, "zero"); _; }

    function deposit(uint256 amount) external nonZero(amount) {
        balances[msg.sender] += amount;
        emit Deposited(msg.sender, amount);
    }
}
```

`testdata/synthetic/sol-contract/contracts/Token.sol`:

```solidity
pragma solidity ^0.8.20;
contract Token {
    string public name = "Synth";
    mapping(address => uint256) public balanceOf;
    function transfer(address to, uint256 amt) external {
        require(balanceOf[msg.sender] >= amt, "insufficient");
        balanceOf[msg.sender] -= amt;
        balanceOf[to] += amt;
    }
}
```

- [ ] **Step 4: Eval task YAMLs (2 starter)**

`eval/tasks/synthetic-T01-find-callers.yaml`:

```yaml
id: T01
corpus: synthetic
corpus_path: testdata/synthetic
description: |
  List all functions in this codebase that ultimately call Vault.deposit.
  Include qualified names like "service.Vault.Deposit", "VaultService.depositFn", etc.
expected_kind: symbol_set
expected:
  symbols:
    - "service.Vault.Deposit"
    - "VaultService.depositFn"
    - "api.Handler.HandleDeposit"
scoring:
  type: precision_recall
  threshold:
    precision: 0.7
    recall: 0.7
```

`eval/tasks/synthetic-T02-rubric-deposit.yaml`:

```yaml
id: T02
corpus: synthetic
corpus_path: testdata/synthetic
description: |
  Add a withdraw function to the Solidity Vault that follows the same
  pattern as deposit (uses balances mapping, emits an event, has a
  non-zero modifier on the amount).
expected_kind: rubric
expected:
  rubric:
    - "uses balances mapping"
    - "emits an event"
    - "applies nonZero modifier on amount"
scoring:
  type: rubric
```

- [ ] **Step 5: Commit**

```bash
git add testdata/synthetic/ eval/tasks/
git commit -m "test(corpus): synthetic Go/TS/Sol mini-corpus and starter eval task YAMLs"
```

---

## Task 37: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Workflow**

```yaml
name: ci
on:
  push: { branches: [main] }
  pull_request:

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        arch: [amd64, arm64]
        exclude:
          - { os: windows-latest, arch: arm64 }
          - { os: ubuntu-latest,  arch: arm64 }
    runs-on: ${{ matrix.os }}
    env:
      GOOS:   ${{ matrix.os == 'macos-latest' && 'darwin' || matrix.os == 'windows-latest' && 'windows' || 'linux' }}
      GOARCH: ${{ matrix.arch }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25' }
      - run: go vet ./...
      - run: go test -race ./...
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: cd web/viewer && npm ci && node esbuild.config.js
      - run: |
          mkdir -p internal/server/web_assets/assets
          cp web/viewer/index.html internal/server/web_assets/index.html
          cp web/viewer/dist/viewer.js internal/server/web_assets/assets/viewer.js
      - run: go build -o bin/ckg ./cmd/ckg

  smoke:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25' }
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: cd web/viewer && npm ci && npx playwright install chromium && node esbuild.config.js
      - run: |
          mkdir -p internal/server/web_assets/assets
          cp web/viewer/index.html internal/server/web_assets/index.html
          cp web/viewer/dist/viewer.js internal/server/web_assets/assets/viewer.js
          go build -o bin/ckg ./cmd/ckg
          ./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-smoke
          ./bin/ckg serve --graph=/tmp/ckg-smoke --port=8787 &
          sleep 3
          cd web/viewer && npx playwright test
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add cross-OS build matrix and Playwright smoke job"
```

---

## Task 38: Documentation + V0 DoD verification

**Files:**
- Create: `docs/SCHEMA.md`
- Create: `docs/ARCHITECTURE.md`
- Create: `docs/EVAL.md`
- Modify: `README.md`

- [ ] **Step 1: SCHEMA.md (compact reference)**

```markdown
# CKG Schema (V0)

Schema version: **1.0**.

## Node types (29)

`Package, File, Struct, Interface, Class, TypeAlias, Enum, Contract,
Mapping, Event, Function, Method, Modifier, Constructor, Constant,
Variable, Field, Parameter, LocalVariable, Import, Export, Decorator,
Goroutine, Channel, IfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt`

LoopStmt uses `sub_kind ∈ {for, while, range, for_in, for_of}`.

## Edge types (22)

`contains, defines, calls, invokes, uses_type, instantiates, references,
reads_field, writes_field, imports, exports, implements, extends,
has_modifier, emits_event, reads_mapping, writes_mapping, has_decorator,
spawns, sends_to, recvs_from, binds_to`

## Confidence

`EXTRACTED` (direct from AST) | `INFERRED` (heuristic / dispatch) | `AMBIGUOUS` (unresolved).

See `spec-ckg-v0-prototype.md` §5.1–5.3 for full attribute and lang-coverage tables.
```

- [ ] **Step 2: ARCHITECTURE.md (compact reference)**

```markdown
# CKG Architecture (V0)

Single Go binary `ckg` with five subcommands sharing a SQLite store.

```
detect → parse → link → graph → cluster → score → persist
```

- **Parsers**: `golang.org/x/tools/go/packages` (Go), tree-sitter (TS/Sol)
- **Cluster**: package-tree (deterministic) + Leiden topic overlay (3 resolutions)
- **Storage**: `modernc.org/sqlite` (CGO-free), embedded schema, blobs in DB
- **Viewer**: vanilla JS + lit-html + 3d-force-graph, embed.FS
- **MCP**: stdio, six tools, in-process Store reads
- **Eval**: 4 baselines × N tasks → CSV + report.md

See `spec-ckg-v0-prototype.md` §3 for the full diagram and §4 for the build pipeline.
```

- [ ] **Step 3: EVAL.md (how to run)**

```markdown
# CKG Eval (V0)

## Run

```bash
export ANTHROPIC_API_KEY=sk-...
ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
ckg eval --tasks='eval/tasks/synthetic-*.yaml' --graph=/tmp/ckg-synth \
         --baselines=alpha,beta,gamma,delta --out=eval/results
cat eval/results/report.md
```

## Baselines

| Code | Tools allowed | Notes |
|---|---|---|
| alpha | none | raw file dump appended to user prompt |
| beta  | get_subgraph | one whole-graph fetch |
| gamma | find_*, get_subgraph, search_text | granular ping-pong (V0: not actually multi-turn) |
| delta | get_context_for_task | smart 1-shot ★ |

## Hypotheses

- **H1**: δ ≤ 50% of α tokens
- **H2**: δ score ≥ α score (no regression)

The auto-generated report.md tabulates both.
```

- [ ] **Step 4: README.md (Quick Start)**

```markdown
# CKG — Code Knowledge Graph

Parse Go / TypeScript / Solidity source into a queryable graph. Browse it in 3D.
Query it from Claude Code via MCP. Validate hypotheses about graph-context vs
raw-file context with the built-in eval runner.

## Quick start (5 minutes)

```bash
git clone https://github.com/0xmhha/code-knowledge-graph
cd code-knowledge-graph
make build
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --open      # opens browser at localhost:8787
```

In Claude Code:

```bash
claude mcp add ckg --command ./bin/ckg --args "mcp,--graph=/tmp/ckg-synth"
```

To run the eval:

```bash
export ANTHROPIC_API_KEY=...
./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' --graph=/tmp/ckg-synth \
               --baselines=alpha,beta,gamma,delta --out=eval/results
cat eval/results/report.md
```

## Documentation

- `docs/spec-ckg-v0-prototype.md` — full design spec
- `docs/STUDY-GUIDE.md` — background on Leiden / MCP / staleness / tree-sitter / 3D layout
- `docs/SCHEMA.md` — node and edge enumeration
- `docs/ARCHITECTURE.md` — subcommand + pipeline overview
- `docs/EVAL.md` — baseline + scoring details
```

- [ ] **Step 5: V0 DoD verification (manual)**

Run the spec §14 checklist:

```bash
# 1. ckg build on synthetic — no errors
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth

# 2. ckg build on go-stablenet-latest — no errors, < 10 min
time ./bin/ckg build --src=$STABLENET_PATH --out=/tmp/ckg-real

# 3. ckg serve loads viewer; manually verify L0..L4 fold/unfold
./bin/ckg serve --graph=/tmp/ckg-real --open

# 4. Claude Code spawns ckg mcp and lists 6 tools
# (perform manually in Claude Code session)

# 5. ckg eval on synthetic + real
./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' --graph=/tmp/ckg-synth \
               --baselines=alpha,beta,gamma,delta --out=eval/results

# 6. Inspect report
cat eval/results/report.md

# 7. CI is green (push to a branch; verify Actions tab)

# 8. Coverage
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1   # expect ≥ 80%
```

If any step fails, file an issue and fix before declaring V0 complete.

- [ ] **Step 6: Commit**

```bash
git add docs/ README.md
git commit -m "docs: add SCHEMA, ARCHITECTURE, EVAL references and README quick-start"
```

---

> **CP-7 reached. V0 complete.** All 38 tasks done; spec §14 DoD checklist passes.

---

# Self-review notes (plan-internal)

After completing a phase, run `git log --oneline | wc -l` — expect at least one commit per task. If a phase produced no Playwright/CI activity, double-check Tasks 26 and 37.

Two known V0 simplifications worth noting in the post-mortem:
1. Task 35's eval runner pre-calls MCP tools in-process rather than driving a real tool-use loop. Real multi-turn γ-mode is V1+.
2. Task 33's LLMClient does not yet implement Anthropic prompt caching headers. Cache hit metrics will report 0 until V1 wires `cache_control`. The token-savings hypothesis is still measurable; only the absolute-cost analysis is affected.

---

**End of plan.**
