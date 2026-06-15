# Incremental Build Cache

> Operator-facing guide to the A3 file-level incremental cache (CKG v0.2,
> spec §4 Phase 1). Phase 2 (reverse-reference invalidation, partial Pass 2)
> is C1's job and not in this document.

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
| `schema_version` | `internal/buildpipe/cache.go: SchemaVersion` (currently `"1.15"`; current value: internal/buildpipe/cache.go SchemaVersion, see docs/SCHEMA.md) | extraction schema bump |

The schema_version is global — bumping it forces a full rebuild for every
file (silent corruption defense; see decision D9 in `spec-ckg-v0.2.md`).

## Build modes

```
ckg build --src=… --out=…           # default: use cache when available
ckg build --src=… --out=… --no-cache             # force full rebuild
ckg build --src=… --out=… --rebuild-metrics      # force PageRank/Leiden recompute
```

Routing inside `buildpipe.Run`:

| Condition | Path |
|---|---|
| `--no-cache`, OR no prior manifest, OR schema/version mismatch | **cold** — wipe DB + parse all files |
| All discovered files match cache, no removals | **short-circuit** — refresh manifest timestamp only |
| Mixed dirty/cached/removed | **cold (fallback for correctness)** — see "Partial-cache fallback" below |

The short-circuit log line (`Cache: H hits, M misses, R removed; parsed N files`)
fires for the all-cached case. Partial-cache cases emit
`Cache: partial hit; falling back to cold rebuild for correctness`.

### Partial-cache fallback (correctness over speed)

The original A3 design routed mixed dirty/cached/removed cases through
`runIncremental` (parse dirty only, reload cached node sets, rerun Pass 2).
Empirical testing surfaced a silent edge-loss class: cross-file `calls`
edges where the **caller is cached and callee is dirty** were dropped
because cached files are not re-parsed and therefore do not re-emit their
pending refs; meanwhile the dirty callee's new node IDs (content-hash
based) don't match the cached caller's recorded edge endpoints, so
`reloadCachedEdges` correctly drops the stale edge — leaving no one to
re-emit it.

Until a reverse-reference index (WORK-PLAN C1) or persisted pending refs
restore correctness, partial cache cases fall back to cold rebuild.
`runIncremental` and its helpers are kept as dead code (referenced via
`_runIncrementalRef` to satisfy gopls) so the eventual re-enable lands
as a single routing change, not a rewrite.

The genuinely load-bearing speedup — full cache hit on a CI re-run with
zero source changes — is the **short-circuit** path and remains intact.
Measured on go-stablenet-latest (2142 files): 40s cold → 1s short-circuit.

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

## Phase 1 limitations (intentional)

- **Partial-cache deferred (D4, 2026-05-04).** Three v1/v2/v3 attempts to
  implement partial-cache (parse dirty only, reload cached nodes + pending
  refs, reuse cached edge sets) all failed the § 7 validation gate on the
  real corpus (go-stablenet, 2142 files). v3 root cause (H3): `NodesByFilePath`
  returns nodes in DB rowid/ID-sorted order, not AST declaration order.
  For ambiguous simple names with multiple candidates, the qIndex winner
  differs between cold and partial paths — both edges survive dedup because
  Dst differs, producing +2675 phantom edges. Fix direction for a future
  v4 attempt: sort `NodesByFilePath` by `start_line ASC`. Partial-cache
  requires B3 (tree-sitter `Tree.Edit()`) or C1 (reverse-reference index)
  as a prerequisite to be economical. The `runIncremental` function and
  `pending_refs` table (schema 1.5) are preserved as dead code.
- **Pass 2 always re-runs** when any file is dirty. Cross-file edges from
  cached files are reloaded from DB (not re-derived), and pending refs
  from dirty files are re-resolved against the merged node set. Phase 2
  (C1) will introduce a reverse-reference index for partial Pass 2.
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
