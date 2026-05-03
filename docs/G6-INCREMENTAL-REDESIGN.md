# G6 — Incremental Partial-Cache: Redesign Spec (v3)

> **Status**: design only. No code changes proposed by this document. Two prior
> implementation attempts (v1, v2) failed on the real corpus; this spec exists
> to gate the next attempt on a coherent answer to the five spec-level
> questions surfaced in `docs/HANDOFF.md` § 4.1.
>
> **Audience**: the next subagent or session that picks up G6. Read end-to-end
> before writing any code.
>
> **Scope**: re-enable the partial-cache routing in `internal/buildpipe/pipeline.go`
> Run() (currently falls back to cold rebuild for any non-full-hit case) so
> partial-hit builds reuse cached parse work and approach short-circuit speed.
>
> **Out of scope**: Pass 2 reverse-reference index in its own right (that is
> WORK-PLAN C1, but its data structure is a load-bearing dependency for v3 —
> see § 5).

---

## 1. Why partial-cache matters (and why we keep failing)

### 1.1 Current state

`buildpipe.Run()` routes builds through three paths:

| Path | Trigger | Cost (go-stablenet, 2142 files) |
|---|---|---|
| `runShortCircuit` | 100 % cache hit, no removals | ~1 s (manifest stamp only) |
| `runCold` | `--no-cache`, missing manifest, version mismatch, **OR partial hit** | ~40-55 s (full rebuild) |
| `runIncremental` | (intentionally unrouted; dead code) | — |

The genuinely load-bearing case in CI / dev re-runs is **short-circuit**, and it
works. The case partial-cache would help is **single-file edit** during dev:
today this costs 40-55 s on a real corpus instead of the 0.2 s we measured on
the synthetic fixture during v1/v2 work.

### 1.2 Failure history

| Attempt | Approach | Synthetic | go-stablenet | Outcome |
|---|---|---|---|---|
| v1 (`31a17f0` analysis) | Persist pending refs in manifest; cached files contribute pending refs from disk on next build | PASS | -92 201 calls (100 % loss); `changed_in` dangling | reverted |
| v2 (`8d5521c` analysis) | Cached files reload Nodes + Pending; temporal/xlang split into always-recompute passes | PASS | -347 986 edges (52 % loss); -576 calls; `changed_in` 0 emitted-vs-DB; **30 min runtime** (24× cold) | reverted |

Both attempts shared one structural flaw: **the synthetic fixture exercises
< 0.5 % of the resolve / dedup / cluster / temporal interactions that the real
corpus does**, so PASS on synthetic was no signal at all. Any v3 implementation
must measure on go-stablenet **before** any commit (see § 7).

---

## 2. Architectural reality

### 2.1 The four contracts incremental must preserve

Cold rebuild defines the ground truth. The post-build graph.db must have:

1. **Node parity** — `SELECT COUNT(*) FROM nodes` after partial == after cold for
   the same source state. Same for every type bucket.
2. **Edge parity** — same for `edges`, broken down by `type` (calls,
   acquires_lock, changed_in, binds_to, …). Cold dedup is "Edge.Count
   aggregation"; partial must produce the same Count distribution, not the same
   row count.
3. **No dangling references** — every `edges.src` / `edges.dst` resolves to a
   `nodes.id` in the same DB. Currently enforced by `graph.Validate()`.
4. **Cluster/score/temporal/xlang convergence** — these are derived from the
   merged Graph. Their outputs must match cold to within numeric tolerance
   (PageRank: ε = 1e-6 between cold and partial on identical input; topic
   resolution: identical when same RNG seed).

### 2.2 The four data structures involved

| Structure | Owner | Persistence | Per-file? |
|---|---|---|---|
| `parse.ParseResult{Nodes, Edges, Pending}` | per-language Pass 1 | none (in-memory) | yes |
| `qIndex map[qname]nodeID` | per-language `Resolve` | none (rebuilt each Resolve) | no — global to language |
| `Graph{Nodes, Edges}` | `graph.Build` | `nodes` + `edges` table | nodes yes via FilePath; edges yes via FilePath |
| Cluster / score / temporal / xlang outputs | post-Build passes | various (`pkg_tree`, `topic_tree`, `nodes.score`, dedicated edges) | no — global |

The key insight v1+v2 missed: **`qIndex` is unnamespaced and global to a
language**, so any function that incremental fails to put into qIndex becomes
unresolvable for any pending ref that refers to it — anywhere in the graph,
including refs from dirty files. v2 thought it had this covered by reloading
cached nodes via `NodesByFilePath` and synthesising `ParseResult{Nodes}`
entries, but **`PendingRefs` were not reloaded**, which is correct *for the
cached side* but means any future change in qname-resolution logic (e.g. a
heuristic that consults pending-ref siblings) silently breaks under partial.

---

## 3. The five spec-level questions, answered

These map 1-to-1 onto `docs/HANDOFF.md` § 4.1. Each gets a concrete answer
v3 must implement; "if X then we drop the partial path entirely" is an
acceptable answer where it appears.

### Q1 — Pass 2 Resolve qIndex namespace + qname-suffix matching across cached + dirty

**Question.** When `Resolve` builds its qIndex from a mix of (cached nodes
loaded from DB) and (dirty nodes freshly parsed), does the suffix-match
fallback at `resolve.go:62-67` produce the same edges as cold?

**Answer.** Yes, *iff* the cached node set passed in is byte-for-byte
identical to what cold would have produced for those files. This is true
under three conditions:

1. **Cached node IDs are stable across rebuilds** — see Q4.
2. **`NodesByFilePath` returns *every* attribute Pass 2 inspects** — the
   relevant ones are `ID`, `Type`, `QualifiedName`. Verify in
   `internal/persist/sqlite_extra.go`'s row scan.
3. **No cached file appears in `dirty[lang]` *and* `cached[lang]` simultaneously**
   — `partitionByLang` (incremental.go:176-188) prevents this by switching on
   `d.Class`, but a future code path that synthesises decisions must preserve
   the partition invariant.

**Action for v3.** Add `internal/buildpipe/incremental_test.go` that asserts:
for a fixture where cold produces N edges of each type, partial-cache rebuild
on the same source produces exactly N edges of each type. Bucket by
`(edge_type, src_node_type, dst_node_type)` so a regression in one bucket can't
be masked by another.

### Q2 — graph.Build dedup-by-ID for edges

**Question.** `graph.Build` (builder.go:20-35) dedups nodes by ID but
**concatenates edges with no dedup**. In incremental, we have edges from
two sources: (a) reloaded from DB via `EdgesByFilePath` + `QueryEdgesForNodes`,
(b) freshly emitted by `Resolve` over the dirty + cached merged node set. If
both sources produce the same logical edge, we double-count.

**Answer.** v2 tried to discriminate via `Edge.ID == 0` (fresh) vs `!= 0`
(reloaded) at insert time (`persistIncrementalArtifacts`, incremental.go:339-348).
This works for the *insert* side but **not for the in-memory `Graph.Edges`
slice** that cluster, score, and temporal passes consume. So PageRank sees
double weight on cached↔cached call edges, etc.

**Action for v3.** Add explicit edge dedup in `graph.Build` (or a new
`graph.MergeIncremental` variant) keyed on
`(Type, Src, Dst, Line)`. Choice:

- **Option A** (preferred): `graph.Build` always dedupes; cold path is
  unaffected because cold has no overlapping edges. One code path, simpler.
- **Option B**: separate `MergeIncremental(prev, fresh)` that the incremental
  path calls. Keeps Build fast but doubles the edge-handling code surface.

A is correct unless benchmarking shows cold rebuild slows by > 5 % on
go-stablenet (217K nodes, 669K edges). Bench first, decide.

### Q3 — Always-recompute passes (cluster, score, temporal, xlang) under incremental

**Question.** `runIncremental` (incremental.go:148-149) calls `BuildPkgTree`,
`BuildTopicTree`, `score.Compute` after merging. v2 added "always-recompute"
classification for temporal/xlang too. Does this produce the right edges in
the DB after `persistIncrementalArtifacts`?

**Answer.** Three sub-cases:

| Pass | Output goes to | DB write strategy in v3 |
|---|---|---|
| Cluster (pkg_tree, topic_tree) | dedicated tables | Drop table contents → re-insert. Tables are independent of the dirty/cached partition. |
| Score (PageRank, usage) | `nodes.score` column | UPDATE every node row (one statement). Cheap. |
| Temporal (`changed_in`, `blame`) | `edges` table, type='changed_in'\|'blame' | Drop by type → re-emit. **Must** run after merge so emit sees full node set, not just dirty. |
| Xlang (`binds_to`) | `edges` table, type='binds_to' | Same as v2 (`relinkXLang`, incremental.go:296-316): rebuild iff any TS/Sol dirty, else reload. |

**Action for v3.** Move temporal emission out of `runCold`'s body
(pipeline.go:182) into a shared helper `emitDerivedPasses(g, opt, store)` that
both cold and incremental call. Currently temporal lives only in cold —
v2's "always-recompute" assertion was true for the in-memory graph but
**didn't actually persist `changed_in` rows** because the DB-write path was
absent on the incremental side. This is the documented "0 emitted-vs-DB"
symptom in v2.

### Q4 — Node ID stability under unchanged file content

**Question.** A file's symbol IDs are content-hash based (see
`internal/parse/golang/parser.go:emit*`). What about IDs that depend on
*other* files? E.g. when an enclosing struct is renamed in `a.go`, do
methods declared in `b.go` (unchanged) get new IDs because their qualified
name now references the renamed struct?

**Answer.** No, but only because of an implementation accident. Method IDs
in `parse/golang` derive from `qname + start_byte`, where `qname` is built
from the receiver type spelling **as it appears in `b.go`** (the method
declaration), not from the resolved `types.Object.Type()`. So a struct
rename in `a.go` doesn't propagate to method qnames in `b.go` until `b.go`
is itself edited.

**Caveat.** This is fragile. If a future Go-parser change moves to type-aware
qname construction (e.g. for B1 concurrency improvements), method qnames
would start changing under upstream renames. The incremental cache would
become unsound silently.

**Action for v3.** Add `TestNodeIDStability_CrossFileRename` to
`internal/parse/golang`: write file `a.go` defining `type Foo struct{}`,
file `b.go` defining `func (f Foo) Bar()`. Hash B's nodes. Rename `a.go`'s
type to `Bar`. Re-parse. Assert B's node IDs are identical OR document the
expected drift. This test guards against the silent fragility above.

### Q5 — Resolve runtime complexity (root cause of v2's 30-minute runtime)

**Question.** v2 took 30 minutes on go-stablenet vs 1:15 cold (24×). Where?

**Answer.** Most likely candidates, ranked by suspicion:

1. **Pending-ref suffix-match loop** (`resolve.go:62-67`) is `O(P × Q)` where
   P = pending refs and Q = qIndex size. On the merged set, P ≈ 90 K refs
   and Q ≈ 200 K entries → 18 G iterations. Cold doesn't hit this because
   each Resolve runs on only one language's subset.

   **Fix**: build a reverse-suffix index once per Resolve call:
   `suffixIdx[short] = []nodeID`. O(Q) build + O(P) lookup.

2. **`QueryEdgesForNodes`** (incremental.go:270) runs `WHERE src IN (...) OR
   dst IN (...)` with up to 200 K params. SQLite breaks this internally into
   chunks but the round-trips are expensive.

   **Fix**: chunk to 999 params (SQLite's default `SQLITE_MAX_VARIABLE_NUMBER`)
   and run in batches. Or pre-load all edges and filter in Go.

3. **`extractBlobsForFiles`** uses a `wantSet` lookup but iterates every node
   (O(N) per call). On 217 K nodes this is fine in absolute time but might
   contribute under high-pressure.

**Action for v3.** Add a benchmark `BenchmarkIncrementalRebuild` that runs on
a fixture mid-sized between synthetic (8 files) and go-stablenet (2142 files).
Target: incremental of 1 dirty file in a 500-file corpus completes in under
3 s. If we can't hit that, the partial-cache path stays disabled and we
invest in C1 (reverse-reference index) instead, which gives partial
*resolve* not just partial *parse*.

---

## 4. Proposed v3 architecture

### 4.1 Routing

```text
Run()
├── short-circuit (100% hit)         ← unchanged
├── cold rebuild (no manifest, etc.) ← unchanged
└── partial rebuild (NEW v3 path)
    ├── parse dirty files only
    ├── reload cached nodes + cached pending refs from DB ← NEW
    ├── Resolve on merged ParseResults
    ├── reload cached intra-file edges; query cross-file edges affected
    ├── graph.Build with dedup-by-(type, src, dst, line) ← NEW
    ├── emitDerivedPasses: cluster, score, temporal, xlang ← UNIFIED with cold
    └── persist with explicit dirty/cached partition
```

### 4.2 Pending-ref persistence (replaces v1's manifest field)

v1 persisted pending refs in the **manifest** (JSON). This was lossy — the
manifest is human-readable and not designed for high-cardinality structured
data. v3 persists in a **new SQLite table**:

```sql
CREATE TABLE pending_refs (
    file_path     TEXT NOT NULL,
    src_id        TEXT NOT NULL,
    target_qname  TEXT NOT NULL,
    edge_type     TEXT NOT NULL,
    line          INTEGER NOT NULL,
    PRIMARY KEY (file_path, src_id, target_qname, edge_type, line),
    FOREIGN KEY (src_id) REFERENCES nodes(id) ON DELETE CASCADE
);
CREATE INDEX idx_pending_refs_file ON pending_refs(file_path);
```

Cold path writes every emitted pending ref. Partial path:
1. `DELETE FROM pending_refs WHERE file_path IN (dirty + removed)`
2. After Pass 1 of dirty, `INSERT` fresh pending refs from dirty parse output.
3. Reload all rows for cached files via `SELECT * WHERE file_path IN (cached)`.
4. Pass to `Resolve` as `ParseResult{Path, Pending: rows}` synthesised entries.

This makes the Resolve input identical to cold's input for the same source
state.

**Schema bump**: 1.4 → 1.5. Existing 1.4 manifests force a cold rebuild on
first 1.5 build (acceptable — same as every prior schema bump).

### 4.3 Edge dedup in graph.Build

Replace `edges = append(edges, p.Edges...)` with a keyed dedup that preserves
Edge.Count semantics:

```go
type edgeKey struct{ Type types.EdgeType; Src, Dst string; Line int }
edgesByKey := map[edgeKey]*types.Edge{}
for _, p := range parts {
    for _, e := range p.Edges {
        k := edgeKey{e.Type, e.Src, e.Dst, e.Line}
        if existing, ok := edgesByKey[k]; ok {
            existing.Count += e.Count    // multiplicity preserved
            continue
        }
        cp := e
        edgesByKey[k] = &cp
    }
}
```

Cold builds today produce no overlapping edges (each ResolvedGraph is from a
different language, edge keys can't collide), so this change is a no-op for
cold and a correctness fix for partial.

### 4.4 Unified derived-passes helper

Extract from `runCold` (pipeline.go:163-194) into a shared function:

```go
func emitDerivedPasses(g *graph.Graph, opt Options, store persist.Store, log *slog.Logger) error {
    // xlang (only when relevant — cold always; incremental conditional)
    // temporal (always — drop by type, re-emit, persist as edges)
    // cluster (always — full recompute, drop tables, re-insert)
    // score (always — UPDATE nodes.score)
    return graph.Validate(g)
}
```

Both `runCold` and `runIncremental` call this. v2's bug — temporal emitting
into in-memory graph but not persisting — disappears because the persist step
is part of the helper, not duplicated per path.

---

## 5. Coordination with C1 (reverse-reference index)

C1 (WORK-PLAN.md line 91) is "Item 4 Phase 2: reverse-reference invalidation".
Its data structure is a **reverse-reference index**: for every node, which
other nodes' resolution depended on it. This solves a different problem from
v3 above:

- v3: makes partial-cache build *correct*, at the cost of always re-running
  Pass 2 Resolve on the merged set (cached pending refs + dirty pending refs).
- C1: makes partial-cache build *fast* by re-running Pass 2 only on the
  pending-refs whose targets were dirty.

Therefore: **v3 should land first** (correctness), and C1 should layer on top
(speed). C1's reverse-reference index needs the `pending_refs` table v3
introduces — it's the natural source for "which pending refs targeted nodes
in file X". So:

1. Implement v3 first — correctness gate.
2. Validate parity on go-stablenet for ≥ 5 dirty-file scenarios.
3. Then C1 builds the reverse-reference index from `pending_refs`.
4. C1's partial-Resolve consults the index to skip re-resolving refs whose
   targets are unchanged.

---

## 6. What this design explicitly does **not** address

- **Concurrent builds** (two `ckg build` against the same OutDir): same
  caveat as today, deferred. Add SQLite advisory lock as separate task.
- **Cross-language pending refs** (TS importing a Sol-bound class): v3
  treats xlang as always-rebuild-when-TS-or-Sol-dirty, same as today.
  Granular xlang invalidation is a v4 concern.
- **G6 line-level blame** (E4 follow-up): orthogonal. Temporal pass remains
  file-level under v3.
- **B1 concurrency edges** in incremental: v3 reuses today's per-language
  `Resolve` flow. The Go pipeline already calls `SetPackages` in
  `runGoPipelineIncremental` (incremental.go:409), so concurrency works
  the same way under v3.

---

## 7. Validation gate (mandatory before merge)

This is the gate v1 and v2 lacked. **No commit to main until all four pass on
go-stablenet** (1259 .go + 320 .ts + 563 .sol = 2142 files):

### 7.1 Parity (correctness)

```bash
# Reference: cold rebuild
./bin/ckg build --src=$STABLENET --out=/tmp/cold --no-cache
sqlite3 /tmp/cold/graph.db ".dump nodes edges" | sort > /tmp/cold.dump

# Test: cold then 1-file edit then partial
./bin/ckg build --src=$STABLENET --out=/tmp/partial --no-cache
echo "// noop" >> $STABLENET/SOME_FILE.go
./bin/ckg build --src=$STABLENET --out=/tmp/partial   # partial-cache path

# Reference 2: cold from same end-state
./bin/ckg build --src=$STABLENET --out=/tmp/coldfinal --no-cache
sqlite3 /tmp/coldfinal/graph.db ".dump nodes edges" | sort > /tmp/coldfinal.dump
sqlite3 /tmp/partial/graph.db ".dump nodes edges" | sort > /tmp/partial.dump

# Must match exactly
diff /tmp/coldfinal.dump /tmp/partial.dump
```

Expected: zero-line diff for nodes (Count is in edges only). Edge diff
allowed iff every difference is in `Count` aggregation (sum of Counts must
match per `(type, src, dst, line)` key).

### 7.2 Edge-count buckets per type

```bash
sqlite3 /tmp/coldfinal/graph.db "SELECT type, COUNT(*) FROM edges GROUP BY type" > /tmp/cold.buckets
sqlite3 /tmp/partial/graph.db    "SELECT type, COUNT(*) FROM edges GROUP BY type" > /tmp/partial.buckets
diff /tmp/cold.buckets /tmp/partial.buckets   # must be empty
```

Specifically watch:
- `calls` (v1: 100 % loss; v2: -576)
- `changed_in` (v2: 0 in DB despite emit log saying 344 946)
- `binds_to` (xlang reload path)
- `acquires_lock` / `accessed_under_lock` (concurrency pass)

### 7.3 Runtime budget

Single-file edit in go-stablenet: **incremental rebuild ≤ 3 s** (vs cold
40-55 s, vs short-circuit ~1 s). v2's 30-min runtime would fail this gate
immediately.

### 7.4 audit zero-diff

```bash
./bin/ckg audit --src=$STABLENET --graph=/tmp/partial   # exit 0
```

This proves user condition #1 (no file dropped) survives partial.

---

## 8. Decision points the next session must explicitly answer

1. **Is the v3 approach (pending_refs table + dedup-by-key) acceptable?** If
   no, the alternative is: rewrite Pass 2 to be partition-aware from the
   ground up (much larger change). This document recommends v3.

2. **Bench Q5 fix first or build full v3 first?** Build full v3 first; if § 7.3
   fails, then do Q5 optimisation. This avoids designing optimisations for a
   path that may turn out to need a different shape entirely.

3. **C1 standalone, or paired with v3?** Recommendation: v3 first (correctness),
   then C1 (speed). Pairing risks v3 correctness bugs being masked by C1
   complexity.

4. **What if the 3-second budget can't be met?** Drop partial-cache from the
   roadmap. Document as "v0.x architectural limit, requires C1 or LSP-style
   incremental parsing (B3) to make economical". The short-circuit path
   (v0.2 today) is the genuine load-bearing speedup; partial is a "nice to
   have" by comparison.

---

## 9. Open architectural questions (track separately if v3 ships)

- **Schema 1.5 → 1.6 migration story**: every schema bump forces a cold
  rebuild for every existing graph.db. We've accepted this 4 times now.
  Should there be a formal migration helper instead?
- **Pending-ref table scale**: 90 K refs × ~80 B/row = 7 MB on go-stablenet.
  Acceptable. But on a 10× larger corpus (Go monorepo), is 70 MB on disk
  acceptable per build? Probably yes, but worth a `du` check.
- **Why does the synthetic fixture not catch real bugs?** Build a "medium"
  fixture (~200 files, 2-3 cross-file edges, 1 cross-language link) so PR
  CI can catch partial-cache regressions cheaply.

---

**End of design.** Implementation starts only after a session reviews this
doc, agrees on the four decisions in § 8, and commits to running § 7's
validation gate on every iteration.
