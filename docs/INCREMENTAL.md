# Incremental Build Cache

> Operator-facing guide to the A3 file-level incremental cache. Covers the cold
> / short-circuit / partial (`runIncremental`) routing. The partial path is live
> and includes C1 reverse-reference invalidation — see "Partial-cache" below.
> Schema version + cache-key contract: authoritative in `internal/buildpipe/cache.go`
> (mirrored in `docs/SCHEMA.md`).

## What it does

`ckg build` records a SHA256 fingerprint per source file in
`OutDir/manifest.json`. On the next build, files whose fingerprints match
are SKIPPED (no parsing, no DB rewrite). Only changed and newly-removed
files trigger work.

For the canonical large corpus (`go-stablenet-latest`, 1259 .go + 320 .ts
+ 563 .sol = 2142 files), this turns a ~40-second cold rebuild into a
~1-second warm rebuild when nothing changed.

## Cache key

```
cache_key = sha256(
    file_content
    + "|ckg:" + ckg_version
    + "|parser:" + parser_version
    + "|schema:" + schema_version
)
```

Any change in any contributor invalidates the cache for that file:

| Contributor | Source | When it changes |
|---|---|---|
| `file_content` | the file bytes | every edit |
| `ckg_version` | `cmd/ckg/root.go: ckgVersion` | every CKG release |
| `parser_version` | `runtime.Version()` for Go; tree-sitter module version for TS/Sol | toolchain or grammar bump |
| `schema_version` | `internal/buildpipe/cache.go: SchemaVersion` (authoritative value lives there — see `docs/SCHEMA.md`; do not hardcode it here) | extraction schema bump |

The schema_version is global — bumping it forces a full rebuild for every
file (silent corruption defense; see decision D9 in `archive/spec-ckg-v0.2.md`).

## Build modes

```
ckg build --src=… --out=…           # default: use cache when available
ckg build --src=… --out=… --no-cache             # force full rebuild
ckg build --src=… --out=… --rebuild-metrics      # force PageRank/Leiden recompute
```

Routing inside `buildpipe.Run`:

| Condition | Path |
|---|---|
| `--no-cache`, OR no prior manifest, OR schema/version mismatch | **cold** — wipe DB + parse all files (`runCold`) |
| All discovered files match cache, no removals | **short-circuit** — refresh manifest timestamp only (`runShortCircuit`) |
| Mixed dirty/cached/removed | **partial (incremental)** — `runIncremental`: parse dirty only, reverse-ref invalidation |

Routing lives in `internal/buildpipe/pipeline.go` (the all-cached branch calls
`runShortCircuit`; the mixed branch calls `runIncremental` at `pipeline.go:260`).

### Partial-cache (`runIncremental`, G6 v4 + C1) — LIVE

The mixed dirty/cached/removed case is served by `runIncremental`
(`internal/buildpipe/incremental.go:154`): parse only the dirty files, reload
cached node sets from the DB, then rerun Pass 2 / cluster / score across the
merged graph. This path is **live**, not a cold fallback.

The original A3 attempt dropped cross-file `calls` edges when the **caller was
cached and callee dirty** (cached files aren't re-parsed, so their pending refs
aren't re-emitted). Two fixes closed that class:

1. **C1 reverse-reference invalidation (IMPLEMENTED).** `runIncremental` queries
   `store.ReverseDepsForFiles`, so cached files whose `pending_refs` target a
   dirty/removed file get their refs re-resolved; unaffected files keep their DB
   edges (`incremental.go:8`).
2. **H3 phantom-edge fix.** `NodesByFilePath` now returns nodes in
   `ORDER BY start_line` (`internal/persist/sqlite_reader.go:592`) so the qIndex
   winner for ambiguous simple names matches the cold path — no +phantom edges.

**Determinism caveat (ADR-0002):** incremental aims for the same logical graph
as a cold rebuild, but the guaranteed-identical artifact is the cold build.
**Canonical measurement graphs must be built cold** (`--no-cache` or a fresh out
dir); incremental is for `serve` freshness. Known perf caveat: mass file removal
can stall `ReverseDepsForFiles` — prefer `--no-cache` when the file set shrinks
sharply.

The load-bearing speedup on a zero-change CI re-run is the **short-circuit**
path: measured on go-stablenet-latest (2142 files), 40s cold → 1s short-circuit.

## Manifest v2 schema

```jsonc
{
  "schema_version": "1.2",
  "ckg_version":    "0.1.0",
  "build_timestamp": "...",
  "files": [
    {
      "path":           "internal/foo/bar.go",
      "language":       "go",
      "sha256":         "abc123…",
      "cache_key":      "def456…",
      "mtime":          1714291200000000000,
      "parser_version": "go/go1.25.5",
      "node_ids":       ["n_aaaa…","n_bbbb…"],
      "edge_ids":       [42, 43, 44]
    }
    // … one entry per discovered source file
  ]
}
```

`files` is added by A3 and absent on pre-1.2 manifests; old manifests
reload as `files: nil` and force the next build through the cold path.

## Phase 1 simplifications (intentional, current)

Partial-cache is now live (see above); the D4 deferral and the v1/v2/v3 phantom-
edge saga are history (the H3 `start_line` fix + C1 reverse-ref invalidation
closed them). What remains intentionally simplified:

- **Pass 2 always re-runs** when any file is dirty. Cross-file edges from cached
  files are reloaded from DB (not re-derived), and pending refs from dirty files
  are re-resolved against the merged node set; C1 reverse-ref invalidation
  re-resolves cached files that pointed at a now-dirty/removed file.
- **Cluster + score recompute on any dirt.** PageRank/Leiden are not
  preserved across incremental rebuilds. The `<1% change-ratio reuse`
  optimisation in spec §4 is deferred. `--rebuild-metrics` exists as a
  forward-compatible escape hatch — currently a no-op when nothing is
  dirty.
- **Cross-language `binds_to` rebuild on any TS/Sol dirt.** The xlang
  linker has no per-file granularity; we drop & re-emit the binds_to
  set whenever any TS or Sol file changes.
- **Concurrent builds undefined.** Two `ckg build` invocations against
  the same OutDir race for the SQLite file. A future advisory-lock task
  can address this.

## Deletion semantics (FK CASCADE)

Schema 1.2 added `ON DELETE CASCADE` to:

- `edges.src REFERENCES nodes(id) ON DELETE CASCADE`
- `edges.dst REFERENCES nodes(id) ON DELETE CASCADE`
- `blobs.node_id REFERENCES nodes(id) ON DELETE CASCADE`
- `pkg_tree.parent_id / child_id REFERENCES nodes(id) ON DELETE CASCADE`
- `topic_tree.child_id REFERENCES nodes(id) ON DELETE CASCADE`

So `DeleteNodesByFilePath(path)` is one SQL statement that wipes a file's
nodes plus every dependent row. Pre-1.2 DBs without CASCADE silently leak
edge/blob rows on incremental rebuild — open such a DB, log a warning,
and force `--no-cache` on first build to migrate.

## What invalidates the cache

| Change | Effect |
|---|---|
| Edit a file's content | that file → dirty |
| Touch a file (mtime changes, content unchanged) | slow path, then cache hit |
| Delete a file | that file → removed |
| Add a new file | that file → dirty (treated as cache miss) |
| Bump `cmd/ckg/root.go: ckgVersion` | every file → dirty (cache discarded) |
| Bump `internal/buildpipe/cache.go: SchemaVersion` | every file → dirty (cache discarded) |
| Bump Go toolchain (e.g. 1.25 → 1.26) | every Go file → dirty |
| Bump tree-sitter module pseudo-version | every TS/Sol file → dirty |

## Verifying it works

```bash
# Cold rebuild
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-test --no-cache
# expect: "Cache: bypassed (--no-cache); full rebuild"

# Warm rebuild — should be near-instant
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-test
# expect: "Cache: 8 hits, 0 misses, 0 removed; parsed 0 files (no source changes; …)"

# Modify one file
echo "// noop" >> testdata/synthetic/go-backend/api/handler.go
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-test
# expect: "Cache: 7 hits, 1 misses, 0 removed; parsed 1 files"
```
