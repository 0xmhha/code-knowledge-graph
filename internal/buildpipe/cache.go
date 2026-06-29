// Package buildpipe — cache.go implements the A3 file-level incremental
// cache (spec v0.2 § 4 Phase 1). Cache key composition, manifest diffing
// (hit/miss/removed classification) and parser-version derivation live here.
//
// Phase 1 scope: per-file SHA256 + cache key, skip parse on hit, full Pass 2
// re-run, full PageRank/Leiden recompute when ANY file is dirty. Phase 2
// (reverse-reference invalidation, partial Pass 2) is C1's job.
package buildpipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// SchemaVersion is the cache-key contributor for the extraction schema. Bumped
// from "1.1" to "1.2" by A3 (FK ON DELETE CASCADE on edges/blobs/pkg_tree/
// topic_tree). Bumped from "1.2" to "1.3" by E3 because new node kinds
// (Endpoint, MessageType) and edge kinds (listens_on, handles_message,
// rpc_calls) materially change the extraction surface — pre-1.3 DBs are
// missing those rows, so incremental invalidation must force a cold rebuild
// on first run with this binary. Bumped from "1.3" to "1.4" by E4 (CKS G6
// Temporal): NodeCommit + changed_in/blame edges are emitted by the new
// post-Build temporal pass; pre-1.4 DBs are missing those rows. Bumped from
// "1.4" to "1.5" by G6 v3 (pending_refs persistence): Pass 1's unresolved
// cross-file references are now persisted per-file so the partial-cache
// rebuild path can reconstruct Pass 2's input without re-parsing cached
// files. Pre-1.5 DBs are missing the table, so the first 1.5 build is forced
// cold by ManifestUsable's version check. Bumped from "1.5" to "1.6" by P2
// (CKS G3 control-flow context propagation): timeout_path /
// cancellation_path self-loop edges are emitted from Go context.With* call
// sites; pre-1.6 DBs are missing those rows so the first 1.6 build must run
// cold. Bumped from "1.6" to "1.7" by Track C (detector gap fill): the
// edges row gains an optional `dispatch_kind` TEXT column populated for
// `invokes` edges (P1b), plus three new emit sites — `uses_type` (P0),
// `instantiates` (P1c), and the lock-edge fix inside goroutine bodies
// (P1a). Pre-1.7 DBs are missing the column AND the new edges; opening
// such a DB triggers an idempotent ALTER ADD COLUMN via Migrate(), and
// ManifestUsable's version check forces a cold rebuild on first 1.7 run
// so the new edges land in their natural emission order.
// Bumped from "1.7" to "1.8" by Hunk-graph H1 (CKS G6 Temporal extension):
// new node type NodeHunk + new edges has_hunk / adjacent + gzip-compressed
// unified-diff blobs persisted under the existing blobs.node_id PK. No
// schema DDL change (the new rows reuse existing tables); pre-1.8 DBs are
// missing the rows + the new node/edge type literals so ManifestUsable's
// version check forces a cold rebuild on first 1.8 run.
// Bumped from "1.8" to "1.9" by W1 of schema-1.9-spec (cross-language interop
// expansion): TypeScript HTTP server endpoint detection (Express / Fastify
// / Hono / Next.js App Router). Reuses the existing NodeEndpoint type +
// `listens_on` edge — no new enum literals, no new columns. The bump is
// purely a cache-key contributor so pre-1.9 DBs don't carry forward a
// missing-Endpoint TS graph view on first 1.9 build. Per §6.1 of the
// design spec, future W2/W3/W4 stages (HTTP client matching, gRPC,
// message queue) will stay on 1.9 and append-only.
// Bumped from "1.9" to "1.10" by within-language semantics Phase 4
// (2026-05-11): slot reservation for W-B (`NodeAwaitPoint` + `EdgeAwaits`,
// TS async/await suspension flow) and W-C (`EdgeOverrides`, Solidity
// virtual/override semantics). detectors land in Phase 5 — this commit
// is slot-only, so pre-1.10 DBs are byte-identical in their existing
// rows but the cache key flip forces a cold rebuild on first 1.10 run
// for symmetry with prior bumps. No new DDL (the new enum literals
// fit existing nodes.type / edges.type TEXT columns); see
// docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 4 and
// docs/design/{ts-async-await-and-interface,solidity-inheritance-and-interface-dispatch}.md.
//
// Bumped from "1.10" to "1.11" by W-C W11 V7 (2026-05-19): the
// nodes table gains the `attrs` JSON-blob column that carries
// every types.Node marker without its own SQLite column (SlotIndex,
// HasAssembly, HasLowLevelCall, HasValueTransfer, YulBuiltins,
// IsFunctionTyped, HasFunctionTypedVar, HasFunctionPointerCall,
// HasFunctionPointerPropagation, HasExternalCall,
// HasInheritanceMROFallback, HasSelfReentrantCall,
// HasSelfDelegatecallDead). Migrate() ALTER-adds the column on
// pre-1.11 DBs via ensureAttrsColumn — incremental builds still
// upgrade — but the cache-key flip forces a cold rebuild on first
// 1.11 run so the new attrs land for every node, not just the
// ones touched by dirty files. See internal/persist/node_attrs.go
// for the JSON encoding.
//
// Bumped from "1.13" to "1.14" by P1 #4 (policy metadata): introduces
// NodePolicy + EdgeGovernedBy slots and the buildpipe Options.PolicyFile
// path that loads them from a YAML and resolves governs[] qnames
// against the parsed graph. No DDL change — the new enum literals fit
// the existing nodes.type / edges.type TEXT columns. The bump forces
// a cold rebuild on first 1.14 run so existing DBs that pre-date the
// PolicyFile flag don't carry forward a stale "no policy nodes" view
// when the operator subsequently turns the flag on. Empty PolicyFile
// is the no-op default; only operators who opt in see new rows. See
// pkg/policy + docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2 P1 #4.
//
// Bumped from "1.14" to "1.15" by P1 #5 (security pattern metadata):
// introduces NodeSecurityPattern + EdgeHasSecurityPattern slots and
// the buildpipe Options.SecurityPatternFile path that loads them
// from a YAML and resolves matches[] qnames against the parsed
// graph. Same shape as P1 #4 — no DDL change, additive, cold-only
// re-ingest. See pkg/security + docs/PROJECT-BLUEPRINT-ALIGNMENT.md
// §4.2 P1 #5.
//
// Kept here (not in pkg/types) because only the cache key needs it;
// pkg/types schema version bumps already trigger rebuilds via this constant.
//
// Bumped from "1.15" to "1.16" for two Go-extraction accuracy changes:
//  1. formatSignature now emits the full parameter and result lists (was the
//     "func name(...) ..." placeholder) — a stored node column.
//  2. call resolution no longer bare-name-binds builtin calls (len/cap/...) or
//     interface-method dispatch to same-named decoys; both are now qualified or
//     dropped — changes calls/invokes edges.
//
// Both repopulate stored rows, so existing graphs are stale until reindexed —
// bump to force a cold re-extraction on first run with this binary.
//
// Bumped from "1.16" to "1.17" (defect C): EmitPromotedMethods materialises a
// method node (+ defines edge) for each method an in-module struct promotes
// from an embedded type, so find_symbol("T.M") resolves promoted methods. New
// rows → existing graphs are stale until reindexed.
//
// Bumped from "1.17" to "1.18" (defect E): EmitFieldWriteEdges emits
// writes_field edges (function -> struct field it assigns) for Go, so an agent
// can find who mutates a field. New edges → reindex required.
//
// Bumped from "1.18" to "1.19" by symbol-identity Phase 1 (ADR-0001): every
// parser now populates nodes.canonical_id. Go covers types/structs/interfaces,
// fields, package-level const/var, and interface methods (previously
// func/method only); Solidity, TypeScript, and proto use the relative file path
// as the qualifier (<relpath>:<qname>), with Solidity appending the
// parameter-type signature to functions to separate overloads. The column
// already exists (idempotent ALTER); pre-1.19 DBs carry it empty, so the
// cache-key flip forces a cold rebuild to repopulate canonical_id graph-wide.
// Additive — qualified_name, node IDs, edges unchanged.
//
// Bumped from "1.19" to "1.20" by symbol-identity refinement B1: the blank
// identifier `_` (package-level `var _`, struct padding fields) no longer
// receives a canonical_id (it was a non-unique <pkg>._ before). Stored
// canonical_id values change for those nodes, so force a cold rebuild.
//
// Bumped from "1.20" to "1.21" by refinements B2 + B4 + B3: function-local
// `var` declarations no longer receive a canonical_id (only package-level
// const/var do — the local <pkg>.<name> id collided across functions); proto
// canonical ids drop the doubled `proto:` prefix (<relpath>:<pkg>.<Sym>); and
// a canonical_id shared by >1 node in the same file is line-qualified with
// "@<line>" (B3 — minified-JS `function t(){}`, multiple Go `init`). All change
// stored canonical_id values, so force a cold rebuild.
//
// Bumped from "1.21" to "1.22" by the simple_name column: nodes now store the
// short name (last dotted segment) so suffix lookups use an indexed equi-join
// instead of a leading-wildcard LIKE. The column is populated at write time, so
// pre-1.22 DBs must be rebuilt to fill it graph-wide (read-only opens cannot
// run the ALTER); the cache-key flip forces that cold rebuild.
//
// Bumped from "1.22" to "1.23" by ADR-0002 (staged graph composition): the Go
// loader's buildFileIndex now gives primary (non-test-variant) packages
// deterministic ownership of production files, instead of order-dependent
// first-seen-wins. ~17.5% of production files previously landed on a test
// variant (whose TypesInfo also feeds the concurrency / field-touch passes),
// making which package resolved them depend on packages.Load's unguaranteed
// order. A cold rebuild pins them to the primary package deterministically.
// (This does not change canonical_id values — test variants share the import
// path; it removes a latent non-reproducibility in resolution context.)
const SchemaVersion = "1.23"

// fileClass classifies one source file against the previous manifest.
type fileClass int

const (
	classDirty   fileClass = iota // miss: must reparse
	classCached                   // hit: reuse from DB
	classRemoved                  // present in old manifest, gone now
)

// FileDecision is the cache decision for one file in the current discovery.
// For classRemoved the Path comes from the OLD manifest (file is gone) and
// Language/SHA256/CacheKey/MTime are zero.
type FileDecision struct {
	Path     string
	Language string
	Class    fileClass
	// Populated for classDirty/classCached:
	SHA256        string
	CacheKey      string
	MTime         int64
	ParserVersion string
	// Populated for classCached only — the matching entry from the OLD
	// manifest, so the caller can pull NodeIDs out for reload.
	Cached *persist.FileEntry
}

// CacheDecisions is the sorted, fully-classified result of one diff pass.
// Sorted by Path for deterministic logging.
type CacheDecisions struct {
	Decisions []FileDecision
	Hits      int
	Misses    int
	Removed   int
}

// ComputeCacheKey returns the SHA256 of:
//
//	file_content + "|ckg:" + ckgVersion + "|parser:" + parserVersion + "|schema:" + SchemaVersion
//
// Any change in the four contributors invalidates the cache for that file
// and forces a reparse on next build (spec v0.2 § 4 design).
func ComputeCacheKey(content []byte, ckgVersion, parserVersion string) string {
	h := sha256.New()
	h.Write(content)
	h.Write([]byte("|ckg:"))
	h.Write([]byte(ckgVersion))
	h.Write([]byte("|parser:"))
	h.Write([]byte(parserVersion))
	h.Write([]byte("|schema:"))
	h.Write([]byte(SchemaVersion))
	return hex.EncodeToString(h.Sum(nil))
}

// SHA256Hex returns the hex SHA256 of content. Exposed separately because
// FileEntry.SHA256 stores content-only hash (used for the "mtime changed but
// content identical" fast/slow path), distinct from the full cache key.
func SHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// parserVersionFor returns a deterministic identifier for the parser used on
// language lang. V0 simplification per spec § 4: Go ties to runtime.Version()
// (the host toolchain that drives go/packages.Load); TS/Sol tie to the
// tree-sitter wrapper module pseudo-version read from BuildInfo, falling back
// to a constant if BuildInfo is unavailable (e.g. in unit tests via `go run`).
//
// Perfect introspection (e.g. embedded grammar SHAs) is C1+ scope. The bar
// for V0 is "any change to the parser binary changes this string".
func parserVersionFor(lang string) string {
	switch lang {
	case "go":
		// runtime.Version() flips when the toolchain that linked ckg
		// changes — Go builds use go/packages.Load with the same toolchain.
		return "go/" + runtime.Version()
	case "ts", "sol":
		return "tree-sitter/" + treeSitterModuleVersion()
	case "proto":
		// Hand-rolled lexer/parser (schema 1.9 §6.4) — ties to the host
		// toolchain so a rebuild after parser edits invalidates the cache.
		return "proto/handrolled/" + runtime.Version()
	default:
		return lang + "/unknown"
	}
}

// treeSitterModuleVersion reads the resolved version of the tree-sitter
// runtime binding from the embedded BuildInfo. Returns "unknown" when
// BuildInfo isn't available (test runs without -buildvcs, for instance).
//
// Pinned to the exact runtime path github.com/tree-sitter/go-tree-sitter
// (post-A1+A2 migration). An earlier substring match on "tree-sitter"
// matched THREE deps (the runtime + tree-sitter-typescript +
// tree-sitter-javascript grammar bindings) and returned whichever
// BuildInfo's Deps iteration happened to surface first — non-deterministic
// across builds. Pinning to the runtime gives a single deterministic
// version contributor to the cache key. The grammar bindings are
// transitively versioned through the runtime release; bumping either
// grammar without bumping the runtime is unusual.
//
// Cache invalidation across A1+A2: pre-migration manifests recorded
// "tree-sitter/v0.0.0-20240827..." (the smacker pseudo-version);
// post-migration records "tree-sitter/v0.25.0". The string mismatch
// fires the slow-path reparse on first build of any pre-A1+A2 graph dir.
const tsRuntimePath = "github.com/tree-sitter/go-tree-sitter"

func treeSitterModuleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range bi.Deps {
		if dep.Path != tsRuntimePath {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		if dep.Version != "" {
			return dep.Version
		}
	}
	return "unknown"
}

// DiscoveredFile describes one file produced by detect.* — used as input to
// the diff. Path is srcRoot-relative slash form.
type DiscoveredFile struct {
	Path     string
	Language string
}

// DiffManifest classifies every discovered file against the OLD manifest and
// emits a CacheDecisions in deterministic Path order. Files in old but not in
// the discovery are emitted as classRemoved.
//
// Fast/slow path (spec § 4 build flow): mtime-equal entries skip the SHA256
// recomputation and reuse the old hash; mtime-mismatched entries fall through
// to a full hash. Either way, the cache decision is byte-equal whether mtime
// changed or not — mtime is purely a perf hint.
func DiffManifest(srcRoot string, discovered []DiscoveredFile, old *persist.Manifest, ckgVersion string) (CacheDecisions, error) {
	oldByPath := map[string]persist.FileEntry{}
	if old != nil {
		for _, e := range old.Files {
			oldByPath[e.Path] = e
		}
	}
	seen := map[string]bool{}
	out := CacheDecisions{}
	for _, df := range discovered {
		seen[df.Path] = true
		full := filepath.Join(srcRoot, filepath.FromSlash(df.Path))
		st, statErr := os.Stat(full)
		var mtime int64
		if statErr == nil {
			mtime = st.ModTime().UnixNano()
		}
		parserVer := parserVersionFor(df.Language)
		oldE, hadOld := oldByPath[df.Path]

		// Fast path: mtime + parser_version unchanged AND old cache_key was
		// computed against the same ckg_version + schema_version. We can't
		// re-derive cache_key without rehashing content, so we recompute the
		// "expected key from the stored SHA" via a deterministic prefix check:
		// the old cache_key is valid iff the version triple matches. Since
		// SchemaVersion is a build-time constant in this binary, the only
		// version that can drift between builds is ckg_version. Compare it
		// implicitly: if the OLD entry's CacheKey was computed under the
		// CURRENT ckg+parser+schema combo, it's still valid; otherwise the
		// caller (full-rebuild guard in pipeline.Run) already invalidated it.
		// Here we trust mtime+parser as the fast-path discriminator.
		if hadOld && statErr == nil && oldE.MTime == mtime && oldE.ParserVersion == parserVer {
			e := oldE // copy
			out.Decisions = append(out.Decisions, FileDecision{
				Path:          df.Path,
				Language:      df.Language,
				Class:         classCached,
				SHA256:        oldE.SHA256,
				CacheKey:      oldE.CacheKey,
				MTime:         mtime,
				ParserVersion: parserVer,
				Cached:        &e,
			})
			out.Hits++
			continue
		}

		// Slow path: read file, hash content, derive full cache key. This
		// runs when mtime drifted (e.g. git checkout) OR the parser version
		// changed for this language. Hashing confirms whether the bytes
		// actually changed.
		content, err := os.ReadFile(full)
		if err != nil {
			// Treat as dirty — the parser will surface the read failure.
			out.Decisions = append(out.Decisions, FileDecision{
				Path: df.Path, Language: df.Language, Class: classDirty,
				MTime: mtime, ParserVersion: parserVer,
			})
			out.Misses++
			continue
		}
		sha := SHA256Hex(content)
		key := ComputeCacheKey(content, ckgVersion, parserVer)
		if hadOld && oldE.CacheKey == key {
			// Content identical (and full key matches) — cache hit despite mtime drift.
			e := oldE
			out.Decisions = append(out.Decisions, FileDecision{
				Path:          df.Path,
				Language:      df.Language,
				Class:         classCached,
				SHA256:        sha,
				CacheKey:      key,
				MTime:         mtime,
				ParserVersion: parserVer,
				Cached:        &e,
			})
			out.Hits++
			continue
		}
		out.Decisions = append(out.Decisions, FileDecision{
			Path:          df.Path,
			Language:      df.Language,
			Class:         classDirty,
			SHA256:        sha,
			CacheKey:      key,
			MTime:         mtime,
			ParserVersion: parserVer,
		})
		out.Misses++
	}
	// Removed files: present in old manifest, absent from discovery.
	for path, e := range oldByPath {
		if seen[path] {
			continue
		}
		out.Decisions = append(out.Decisions, FileDecision{
			Path: path, Language: e.Language, Class: classRemoved,
		})
		out.Removed++
	}
	sort.Slice(out.Decisions, func(i, j int) bool {
		return out.Decisions[i].Path < out.Decisions[j].Path
	})
	return out, nil
}

// ManifestUsable reports whether old can be used as the cache base for a
// build under (ckgVersion, SchemaVersion). Returns false when the global
// invariants drifted — caller must discard the cache and full-rebuild
// (silent reuse with stale schema would corrupt the DB).
//
// nil manifest → false. Empty schema/ckg version → false (defensive).
func ManifestUsable(old *persist.Manifest, ckgVersion string) bool {
	if old == nil {
		return false
	}
	if old.SchemaVersion != SchemaVersion {
		return false
	}
	if old.CKGVersion == "" || old.CKGVersion != ckgVersion {
		return false
	}
	if len(old.Files) == 0 {
		return false
	}
	return true
}

// IsAllCached returns true when every decision is classCached and there are
// no removals. Used by the build pipeline to short-circuit Pass 2 / metrics
// when nothing actually changed.
func (cd CacheDecisions) IsAllCached() bool {
	return cd.Misses == 0 && cd.Removed == 0
}

// DirtyPaths returns the srcRoot-relative paths of files needing reparse,
// in the discovery order they were emitted (deterministic).
func (cd CacheDecisions) DirtyPaths() []string {
	var out []string
	for _, d := range cd.Decisions {
		if d.Class == classDirty {
			out = append(out, d.Path)
		}
	}
	return out
}

// CachedPaths returns the srcRoot-relative paths whose cache hit, in sorted
// order. Caller uses these to load nodes/edges from the DB.
func (cd CacheDecisions) CachedPaths() []string {
	var out []string
	for _, d := range cd.Decisions {
		if d.Class == classCached {
			out = append(out, d.Path)
		}
	}
	return out
}

// RemovedPaths returns the srcRoot-relative paths that were in the old
// manifest but are not in the current discovery. Caller deletes their data.
func (cd CacheDecisions) RemovedPaths() []string {
	var out []string
	for _, d := range cd.Decisions {
		if d.Class == classRemoved {
			out = append(out, d.Path)
		}
	}
	return out
}

// FormatLogLine returns a single human-readable summary line. Stable phrasing
// so operator runbooks can grep for it.
func (cd CacheDecisions) FormatLogLine() string {
	dirty := len(cd.DirtyPaths())
	return fmt.Sprintf("Cache: %d hits, %d misses, %d removed; parsed %d files",
		cd.Hits, cd.Misses, cd.Removed, dirty)
}
