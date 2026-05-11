# Hunk Graph — Track F follow-up design

Status: design / plan only. No implementation in this document.
Audience: contributors landing the H1..H4 stages.
Cross-references: `internal/temporal/git.go`, `internal/buildpipe/temporal.go`,
`internal/buildpipe/cache.go` (`SchemaVersion`), `pkg/types/enums.go`,
`internal/persist/schema.sql`, `internal/persist/sqlite.go`,
`web/viewer-next/src/lib/edges.ts`, `internal/server/api.go`,
`internal/mcp/server.go`, `pkg/smartctx/smartctx.go`, `pkg/bm25/scorer.go`.

This document specifies the graph-level extension that turns CKG's existing
G6 Temporal axis into a usable few-shot retrieval surface for a Coding
Agent. The current G6 implementation only stores the metadata of commits
(SHA, author-time, subject) and relates them to file/symbol nodes via two
edges — `changed_in` and `blame`. That is enough to *answer* "when was
this last touched", but it is not enough to *show* the model **what change
was made**. The Hunk graph closes that gap.

---

## 1. Motivation and user value

### 1.1 The retrieval gap a Coding Agent sees today

A Coding Agent's typical retrieval path against CKG today is:

1. Receive a task intent ("fix panel visibility regression").
2. Call `search_text` (`internal/mcp/tools.go:121` `registerSearchText`)
   or the smart context tool (`pkg/smartctx/smartctx.go` `BuildContext`).
3. Get back ranked CodeNodes (Function/Method/Type/Field) with bodies
   pulled from `blobs.source`.
4. Optionally look up callers/callees via the existing graph.

That is a **structural** retrieval — "show me the relevant code right
now". It is excellent for "understand how this works" tasks. It is poor
for two important agent flows:

- **Few-shot learning from past changes.** "How does this team usually
  fix panel visibility bugs? Show me the last three times someone solved
  a similar problem." A snapshot of the *current* code does not answer
  that question — the Agent needs the *delta* from "broken" to "fixed",
  i.e. the patch.

- **Context for risky edits.** "Before I touch the rate limiter, what
  has changed there recently?" Listing the most-recent commits is
  cheap, but the Agent then has to fetch each diff out-of-band (shell
  out to `git show`, parse, summarise) — which defeats the point of a
  single MCP-level retrieval surface.

### 1.2 Why patch-only is not enough

A naive design says "just store the unified diff as a blob, BM25 over
it, return top-K". That works for the few-shot case but loses three
things:

1. **Symbol pivots.** Given the qualified name of a function the user
   is editing, "show me hunks that modified this function" requires an
   index from CodeNode → Hunk. Pure patch-blob storage does not have
   that index — it would force a fanout query "find every hunk that
   *mentions* this function name", which is fragile (renamed symbols,
   shadowed names, comments that mention the function).

2. **Cross-axis lookups.** The Agent already uses G3 (calls/invokes)
   and G5 (rpc_calls/listens_on) to widen the neighbourhood around a
   seed. If hunks are first-class graph nodes, "for this seed function:
   list its callers AND the recent changes to its callers" becomes one
   subgraph query instead of two.

3. **Cluster-aware ranking.** `cluster.BuildPkgTree` and
   `cluster.BuildTopicTree` (see `internal/buildpipe/pipeline.go:58-59`)
   already partition the graph into communities. A hunk node naturally
   inherits the community of the CodeNode it modified, so retrieval can
   say "rank hunks higher if they live in the same topic cluster as the
   intent's seed" — patch-blob retrieval has no community signal.

### 1.3 Why AST-only is not enough either

The mirror approach — "for every commit, re-parse the changed files at
that revision and ingest those AST nodes" — is the most "correct"
shape, but it is unaffordable in practice:

| Concern | Cost |
|---|---|
| Storage | A 178-commit history × ~5 files/commit × ~200 nodes/file ≈ 178 K extra nodes. Today's self-graph has ~33 K nodes total at HEAD. The history would dominate. |
| Build time | One AST parse takes ~5 ms for a small Go file; 178 K parses = ~15 minutes per build. The current `parseConcurrent` worker pool (`internal/buildpipe/language_runners.go:55-119`) hits diminishing returns above ~8 workers, so parallelism does not save us here. |
| Schema churn | Past revisions of the same file would clash on the existing `nodes(id)` PK because `parse.MakeID` (see `internal/parse/idgen.go:12`) hashes only `(qname, lang, startByte)`. We would have to add a commit-SHA dimension to every Node ID, breaking every cross-reference in the existing 6 graphs. |
| Retrieval signal | An AST snapshot at commit C tells you *what existed*, not *what was changed*. The few-shot signal the Agent wants ("here's the diff that fixed it") still has to be reconstructed from a *pair* of snapshots, plus a diff pass — the work we tried to avoid is back, on top of all the storage. |

### 1.4 The hybrid: Hunk as a first-class node, AST left at HEAD

The design lands at: store the *patch* as a Hunk node (cheap, retrievable
by BM25, contains the few-shot signal directly), and link Hunks to the
*current* AST CodeNodes via a new `modifies` edge based on line overlap
(cheap join, gives us the symbol pivot). This trades a small amount of
historical fidelity (we cannot reconstruct the symbol graph at commit
C-3) for a large amount of practical retrieval power.

Token-efficiency comparison (rough numbers, self-graph at HEAD):

| Approach | Storage | Retrieval cost for "show me 3 recent fixes near function F" |
|---|---|---|
| AST snapshots per commit | ~178 K extra nodes; ~80 MB in nodes table; 15 min build | 1 query against `(commit_id, file_path, qname=F)` index → 3 node sets → reconstitute diff = expensive |
| Patch blobs only | ~700 hunks × ~1 KB = ~700 KB | 1 BM25 query against patch text → top-K → return blobs. No symbol-precise pivot ("F" might appear in a comment in an unrelated hunk). |
| **Hybrid (this design)** | **~700 hunks (~1 MB) + ~3 K modifies edges (~240 KB)** | 1 graph query: `Function(F) ← modifies ← Hunk → has_hunk → Commit ORDER BY commit.timestamp DESC LIMIT 3`. Returns the patch blobs directly. |

The hybrid keeps each retrieval to one round-trip *and* gives the
Agent symbol-precise filtering — that is the core of the user value.

### 1.5 EvidencePack flow (what the Agent sees)

End-state: the Coding Agent calls a single MCP tool (working name
`query_code` / `evidence_for_intent`) with an intent string and an
optional seed qname. It receives a JSON pack:

```
{
  "intent": "panel visibility flicker",
  "hits": [
    {
      "commit": { "sha": "abc123…", "subject": "fix panel re-mount jitter",
                  "author_time": 17146..., "issue_ids": ["INGEST-401"] },
      "hunks": [
        {
          "id": "abc123:web/viewer-next/Panel.tsx:1",
          "file_path": "web/viewer-next/Panel.tsx",
          "start_line": 42, "end_line": 71,
          "patch_text": "@@ -42,8 +42,12 @@ ...",
          "modifies": [
            { "qname": "Panel.render", "type": "Method",
              "file_path": "web/viewer-next/Panel.tsx",
              "start_line": 38, "end_line": 80 }
          ]
        }
      ]
    },
    ...
  ]
}
```

This is the artefact the Agent embeds into its system prompt as a
few-shot example: "look how this team solved a similar problem before;
do the same kind of edit". The shape is congruent with the existing
`pkg/smartctx.Pack` so the Agent can fuse the two retrievals (current
code context + historical changes) into one prompt frame.

---

## 2. Schema design

### 2.1 Constraint: extend, do not break

The existing schema (`internal/persist/schema.sql`) is tightly coupled
to ID composition (`parse.MakeID`), edge type enumeration
(`pkg/types/enums.go`), and the file-level cache key
(`internal/buildpipe/cache.go SchemaVersion`). The Hunk extension MUST:

- not change `nodes(id)` or `edges(id)` PK shape;
- not change any existing Node row's columns;
- not require a backfill of pre-existing rows;
- still let an old graph.db open under a 1.8-aware binary (graceful
  zero-Hunk read).

The design therefore reuses `nodes`, `edges`, and `blobs` as-is and
just introduces new `type` literals.

### 2.2 Hunk row in `nodes`

A Hunk is a row in the same `nodes` table everything else uses. The
row is filled like this:

| column | value | rationale |
|---|---|---|
| `id` | `parse.MakeID("hunk:" + sha + ":" + file_rel + ":" + idx, "git", 0)` | Same 16-char SHA-256 prefix as every other node. `idx` is the 0-based index of this hunk inside the unified diff for that (commit, file) pair, so two hunks in the same file in the same commit have distinct IDs. |
| `type` | `"Hunk"` (new `NodeType` literal `NodeHunk`) | Appended at index 33 in `AllNodeTypes()` to preserve positional stability of existing entries (same convention as `NodeCommit` at index 32 — see `pkg/types/enums.go:54`). |
| `name` | `<short-sha>:<file>:<idx>` (e.g. `abc12345:Panel.tsx:1`) | Display label in viewer / search. |
| `qualified_name` | `hunk:<full-sha>:<file_rel>:<idx>` | Stable, queryable. Mirrors the `commit:<sha>` convention of NodeCommit. |
| `file_path` | the target file's repo-rel slash path (post-rename if applicable) | So `idx_nodes_file` works for hunk lookup. |
| `start_line` | hunk's NEW-side first line | Drives the line-overlap join with CodeNodes. |
| `end_line` | hunk's NEW-side last line | Same. |
| `start_byte` | 0 (sentinel) | Hunks have no byte range in the working tree — the patch lives in `blobs.source` instead. |
| `end_byte` | 1 (sentinel; min for the `gtfield=StartByte` validator) | See `pkg/types/node.go:13`. |
| `language` | the file's language (`go` / `ts` / `sol`) when known; `git` sentinel if the file is outside any parsed language | Lets a "modifies" join pre-filter by `language`. The `git` sentinel mirrors NodeCommit (`internal/buildpipe/temporal.go:181`) and is excluded from per-language audit queries. |
| `visibility` | NULL | not applicable |
| `signature` | "+N -M" line-count summary (e.g. `"+12 -3"`) — keeps the manifest dump human-skimmable | small, fits the existing 100-char cap convention |
| `doc_comment` | NULL | the patch text lives in the blob, not here |
| `complexity` | NULL | not applicable |
| `confidence` | `EXTRACTED` | matches every other parse-derived node |
| `sub_kind` | one of `add` / `mod` / `del` / `rename` / `binary` (string), based on the diff status | Lets queries filter "show me only deletions" without parsing the patch |

Note that the `Node.Language` JSON tag carries `validate:"required,oneof=go ts sol"`
(`pkg/types/node.go:14`). Adding `git` to the oneof list (already done
implicitly for NodeCommit since 1.4) is a one-line widening. Hunks
inherit the same widening.

### 2.3 Hunk patch blob

The unified-diff text for one hunk is stored in the existing `blobs`
table:

| column | value |
|---|---|
| `node_id` | the Hunk node's id |
| `source` | gzip-compressed unified-diff text (typical 70 % size reduction; decompresses lazily on `GetBlob`) |

The existing `InsertBlobs` (`internal/persist/sqlite.go:190-208`) and
`GetBlob` (`internal/persist/sqlite.go:451`) work as-is. The compression
choice is discussed in §11 (open questions); the default is gzip.

The patch text we store is the *exact* `git diff <parent>..<commit> --
<file>` output for that hunk's line range, including the `@@ -a,b +c,d @@`
header. We store the full hunk header so that downstream tools can
reconstruct line numbers without re-parsing.

### 2.4 New edge types (G6 Temporal extension)

Three new `EdgeType` literals are appended to `AllEdgeTypes()` in
`pkg/types/enums.go` after `EdgeCancellationPath` (preserving positional
stability):

| EdgeType | direction | semantics |
|---|---|---|
| `has_hunk` | Commit → Hunk | every hunk emitted by a commit. Replaces nothing; coexists with the existing `changed_in` (which stays file-level). |
| `modifies` | Hunk → CodeNode | the hunk's `[start_line, end_line]` overlapped with CodeNode `[start_line, end_line]` on the SAME `file_path`. One hunk can modify multiple CodeNodes (a wide hunk that spans two functions); a hunk that lands in inter-symbol whitespace produces zero `modifies` edges (still valid). |
| `adjacent` | Hunk ↔ Hunk | two hunks in the same commit + same file with sequential, non-overlapping line ranges. Undirected in spirit but stored as a directed edge from the earlier-line hunk to the later-line one for canonical ordering. Used by retrieval to "expand the patch context" — when one hunk surfaces in BM25, return its adjacent siblings to give the Agent the surrounding edits. |

All three are emitted with `Confidence = EXTRACTED` and `Count = 1`.
None have a meaningful `line` value (set to 0).

### 2.5 6-graph axis classification

The Hunk extension is **G6 Temporal** — it captures historical change
relations, not structural / semantic / execution / concurrency / distributed
ones. The existing `GRAPH_GROUPS` map in
`web/viewer-next/src/lib/edges.ts:194-197` lists G6 as containing
`changed_in` + `blame`. We extend that list to:

```
{
  id: 'G6', label: 'Temporal', color: 0x888899,
  description: 'Git history: changed_in / blame; has_hunk / modifies / adjacent (Hunk graph)',
  edges: ['changed_in', 'blame', 'has_hunk', 'modifies', 'adjacent'],
}
```

Each new edge gets an `EDGE_STYLE` entry (low-contrast greys/browns,
matching the existing G6 palette so they don't dominate when toggled
on). All three are **off by default** in `DEFAULT_EDGE_TYPES`
(`web/viewer-next/src/lib/edges.ts:119-133`) — opt-in via the filter
UI, identical to `changed_in`'s policy.

### 2.6 Schema version bump

`internal/buildpipe/cache.go:42` defines `const SchemaVersion = "1.6"`.
Hunk graph bumps this to **"1.8"**. The doc comment around it
(`internal/buildpipe/cache.go:23-42`) gets a new paragraph noting:

> Bumped from "1.6" to "1.8" by Track F (Hunk graph) — coordinated with Track C which already bumped 1.6→1.7 for dispatch_kind: `Hunk` nodes plus
> `has_hunk` / `modifies` / `adjacent` edges are emitted by the
> post-Build temporal pass; pre-1.8 DBs are missing those rows so the
> first 1.8 build must run cold. The unified-diff blobs use the existing
> `blobs` table, so no new SQL DDL is required — the bump exists only
> to invalidate stale incremental caches.

Per the existing convention (every prior bump was cache-invalidating
only — see `internal/persist/SCHEMA.md` style), the bump triggers a
cold rebuild on first run. `MigrateTo17` is therefore **not a SQL
migration function** — it is a no-op / placeholder that exists only so
the manifest version field gets the right value. (Implementation note,
not a doc requirement: a one-line addition to whatever wires the
manifest's `SchemaVersion` field. The `internal/persist/manifest.go`
shape already supports it without code changes.)

### 2.7 Read-side compatibility

Pre-1.7 DBs opened under a 1.8-aware binary still serve API requests:

- `GET /api/nodes?parent=…`: Hunk nodes don't exist → response unchanged.
- `POST /api/edges`: returns whatever edges exist; `has_hunk`/`modifies`/
  `adjacent` simply don't appear.
- `GET /api/hunks?commit=…` (new endpoint, §6): returns `[]`.

The graceful-degradation contract matches how `temporal` already handles
non-git checkouts (`internal/buildpipe/temporal.go:54-65`).

---

## 3. Build pipeline (H1)

### 3.1 Where the new code lives

The new collector belongs in `internal/temporal/` next to `git.go`, in a
new file `hunks.go`. It exposes one function:

```go
// LoadHunks runs git diff per commit (relative to its first parent — or
// the empty tree for the root commit) and returns one HunkInfo per
// hunk per file per commit, scoped to srcRoot's subtree.
//
// Repos that aren't git checkouts return an empty slice + nil error,
// mirroring temporal.LoadHistory's degrade-gracefully contract.
func LoadHunks(repoRoot, srcRel string, opts LoadHunksOptions) ([]HunkInfo, error)
```

`HunkInfo` carries: commit SHA, file path (post-rename), `start_line` /
`end_line` (NEW side), `added` / `removed` line counts, raw patch text
(including the `@@` header), status (`add`/`mod`/`del`/`rename`/`binary`),
and an `idx` field — the 0-based per-(commit,file) ordinal so IDs are
stable.

### 3.2 Why a fresh collector, not an extension of LoadHistory

`LoadHistory` (`internal/temporal/git.go:56-84`) uses a single
`git log --raw --no-renames` invocation that yields metadata without
patch text. Adding `-p` to that invocation is tempting but breaks two
things: (1) the existing `parseGitLog` parser
(`internal/temporal/git.go:109-158`) expects a strict block format that
`-p` interleaves diff hunks into; (2) `--no-renames` is set to keep the
file-touch graph simple — we want renames *visible* on the Hunk side
so the post-rename file path is what `modifies` joins against.

The Hunk collector therefore makes a separate pass:

1. List commits via the existing `LoadHistory` invocation. The Hunk
   collector receives that same list, so we don't pay the `git log`
   cost twice.
2. For each commit, run `git diff <parent>..<commit> --unified=3
   --no-color --find-renames` for the files that touched srcRoot's
   subtree. Use the multi-revision form so we don't fork `N` git
   processes per commit; one process, one stream, one parser.
3. Stream-parse the unified diff into hunks.

For initial / root commits (no parent), use `git diff
4b825dc642cb6eb9a060e54bf8d69288fbee4904 <commit>` — that hash is
git's well-known empty-tree object and produces a "everything was
added" diff. This is the standard idiom for "diff against nothing".

### 3.3 Concurrency model

`parseConcurrent` (`internal/buildpipe/language_runners.go:55-119`)
already gives us a worker-pool primitive. The Hunk collector uses the
same pattern: one worker per CPU, capped at 8, each worker processes
one commit. Per-commit hunk extraction is genuinely independent (each
shells out to `git diff` for its own SHA), so the speedup is close to
linear up to the CPU count.

A single sync.Mutex guards the result slice, mirroring the existing
parser pool's "many parsers, single writer" idiom
(`internal/buildpipe/language_runners.go:42-52`). Output ordering is
non-deterministic during collection; a `sort.Slice` by `(sha, file_rel,
idx)` at the end gives a stable manifest.

### 3.4 Wiring into the pipeline

The natural call site is `emitTemporalEdges`
(`internal/buildpipe/temporal.go:48-91`), which already runs
post-Build and is the single owner of G6 emission. The H1 patch:

```
emitTemporalEdges(g, srcRoot, log, maxPerFile)
  └─ LoadHistory  (existing)              ──► commit metadata
  └─ remapToSrcRel + buildCommitNodes ...  (existing) ──► NodeCommit + changed_in + blame
  └─ NEW: LoadHunks(repoRoot, srcRel)     ──► HunkInfo per (commit, file, idx)
  └─ NEW: buildHunkNodes / buildHunkEdges  ──► NodeHunk + has_hunk + adjacent
  └─ NEW: buildModifiesEdges               ──► modifies (depends on g.Nodes — symbols already present)
```

Critically, `modifies` emission must happen *after* `buildCommitNodes`
appends its outputs to `g.Nodes` and *after* the language parsers'
CodeNodes are already in `g.Nodes` (which they are — `emitTemporalEdges`
is called *after* `graph.Build` in `runCold` at
`internal/buildpipe/pipeline.go:272`). The line-overlap join therefore
sees the full CodeNode set in memory, no DB round-trip required.

### 3.5 Cache and incremental routing

H1 piggy-backs on the existing 1.7 cache invalidation: the first 1.8
build is cold. After that, two regimes:

- **Short-circuit (all files cached, no removals).** The cache key
  matches; the existing path simply refreshes the manifest timestamp
  (`internal/buildpipe/pipeline.go:174`). We do NOT re-extract hunks —
  the prior commit list is the same, the prior hunk list is the same.
  This is correct as long as `HEAD` hasn't moved; we verify with the
  existing staleness check (`current_commit` field on the manifest),
  which will trigger a cold/incremental rebuild if HEAD advanced.

- **Incremental (some files dirty).** The dirty files have new content
  but typically the commit graph also advanced (HEAD moved). The
  simplest correct policy for H1: when the incremental path activates,
  **fully re-emit hunks** for all commits between the manifest's
  `current_commit` and the new HEAD, *plus* keep the hunks for unchanged
  history. In practice this means: query existing Hunk node IDs, drop
  hunks whose commit SHA is no longer reachable from HEAD (force-push /
  rebase scenario — see §11), and append hunks for new SHAs. The
  partial re-emit fits naturally because `emitTemporalEdges` already
  consumes a full commit list every build.

For H1's first cut, the simpler rule is acceptable: **on any incremental
build, drop all `Hunk` / `has_hunk` / `modifies` / `adjacent` rows and
re-emit from scratch.** The existing `DeleteEdgesByType` infrastructure
(used by `temporal` already in the cold and partial paths) handles this
in O(rows) time. With ~700 hunks and ~3 K modifies edges, the round-trip
is well under a second; not worth optimising prematurely.

### 3.6 Edge cases

- **Binary files.** `git diff --numstat` reports binary changes as
  `- - <path>`. The Hunk collector emits a single Hunk row per binary
  change with `sub_kind = "binary"`, `start_line = end_line = 1`,
  `added = removed = 0`, and an empty patch blob. A `modifies` edge is
  not emitted (no line overlap to compute). This way binary touches
  still surface in "what did this commit change", but they don't pollute
  the BM25 index with garbage tokens.

- **Renames / moves.** With `--find-renames`, git emits a header block
  noting the old → new path. The Hunk row's `file_path` is the *new*
  path (so `modifies` joins against the live AST), `sub_kind = "rename"`,
  and the patch blob retains the original `rename from` / `rename to`
  header lines so the Agent can see the move. The pre-rename path is
  not stored in a separate column — it can be parsed from the blob if
  needed; we're not adding a `before_path` column to nodes.

- **Pure mode-changes.** A `chmod` with no content change emits a hunk
  with empty body. We **skip** these — they have no few-shot value and
  would just bloat the count.

- **Empty / initial commit.** Diff against the empty-tree hash, as
  noted in §3.2. The root commit emits one Hunk per file with
  `sub_kind = "add"`.

- **Squash / merge commits.** Treat them like any other commit: one
  parent (the first parent for merges), full diff against that parent.
  This is conservative — a true merge commit's "changes" are the
  conflict-resolution edits, which is the right surface for retrieval
  ("how was this conflict resolved last time?").

- **Symlinks / submodules.** Skip. Same rationale as binary.

- **Files outside srcRoot's subtree.** Filtered by `remapToSrcRel`
  (`internal/buildpipe/temporal.go:100-122`) on the commit-list side
  *before* we open `git diff` for that file pair — we never invoke
  diff on files we wouldn't keep.

- **Non-UTF-8 patches.** Some legacy files (Korean CP949 in some
  go-wbft repos, for example) produce non-UTF-8 patch bytes. We do
  not transcode — the blob is stored verbatim. Downstream BM25
  tokenisation handles invalid UTF-8 gracefully (`pkg/bm25/tokenize.go`
  — checked).

### 3.7 Performance budget

For self-graph (178 commits, ~5 hunks/commit ≈ 700 hunks):

- `git diff` per commit: ~5–15 ms on warm-cache repo.
- Worker pool of 8: 178 commits / 8 ≈ 22 batches × 15 ms ≈ 0.3 s wall.
- Diff parser: ~0.5 ms per hunk × 700 hunks ≈ 0.4 s.
- Modifies-edge join: O(hunks × CodeNodes-in-same-file). For 700 hunks
  with typically 50 CodeNodes/file → ~35 K comparisons → trivial.
- Blob compression: gzip(patch) at ~50 MB/s on 700 KB total → 14 ms.

Total H1 added latency at HEAD-on-clean-cache: ~1 second. At a 10K-commit
repo: ~1 minute (dominated by `git diff` invocations). Acceptable.

---

## 4. AST overlap detection (H2)

> **Status (2026-05-10)**: Landed. `internal/buildpipe/temporal_hunks.go`
> `emitModifiesEdges` runs after both EXTRACTED and AMBIGUOUS hunks are
> in place; confidence on each `modifies` edge follows its source hunk.
> Self-graph eval (go-stablenet, 8967 EXTRACTED + 40 AMBIGUOUS hunks):
> 11,435 `modifies` edges (11,373 EXTRACTED + 62 AMBIGUOUS), avg 3.07
> per hunk, max 167 (a generated-code regeneration hit). Edge breakdown
> by destination type: Function 2981 · Method 2774 · Variable 2474 ·
> Field 2095 · Struct 575 · Constant 374 · TypeAlias 63 · Interface 60 ·
> Modifier 18 · Contract 17 — the §4.2 whitelist behaves as designed,
> with the FunctionLike + TypeLike kinds dominating and statement-level
> noise (CallSite/IfStmt/...) correctly skipped.

### 4.1 The join

After H1 lands all Hunk nodes, H2 emits `modifies` edges. The algorithm:

```
for hunk in hunks:
    if hunk.sub_kind == "binary": continue
    for codeNode in nodesByPath[hunk.file_path]:
        if codeNode.type not in {Function, Method, Constructor, Modifier,
                                 Type, Struct, Interface, Class, TypeAlias,
                                 Enum, Contract, Field, Constant, Variable}:
            continue   # skip Package/File/CallSite/IfStmt/Goroutine etc.
        if overlap([hunk.start_line, hunk.end_line],
                   [codeNode.start_line, codeNode.end_line]):
            emit modifies(hunk -> codeNode)
```

The `overlap` predicate is the standard interval-overlap test:
`a.start <= b.end && b.start <= a.end` (inclusive). For a hunk that
straddles two function bodies (rare but legal — e.g. an edit that
removes the closing `}` of one function and the opening of the next),
both modifies edges are emitted. For a hunk in inter-symbol whitespace
(comments, package docs, blank lines), zero modifies edges are
emitted; the Hunk node still exists, still gets `has_hunk` from its
commit, still gets BM25-indexed.

### 4.2 Why "FunctionLike + TypeLike + Field-ish" only

The full Node type enumeration (`pkg/types/enums.go:8-55`, 33 types)
includes statement-level nodes (`IfStmt`, `LoopStmt`, `CallSite`,
`ReturnStmt`, `SwitchStmt`) and concurrency / DI fragments (`Goroutine`,
`Channel`, `Parameter`, `LocalVariable`). Emitting `modifies` to those
would explode the edge count by ~10× without adding retrieval signal —
the Agent does not query "show me hunks that modified an `IfStmt` at
line 42". The whitelist above (10 types) keeps `modifies` semantically
"changed a top-level declaration", which is what the few-shot use case
actually wants.

The whitelist is implemented as a `map[NodeType]bool` constant beside
the H2 emitter; future inclusions (e.g. `Endpoint` once handler
declarations get richer) are one-line additions.

### 4.3 Performance

Construction is O(hunks × CodeNodesInSameFile). Index lookup is by
file path (already populated by `indexNodesByPath` —
`internal/buildpipe/temporal.go:211-225`). Self-graph numbers:

- 700 hunks × ~50 candidate CodeNodes/file × O(1) interval test ≈
  35 K comparisons → < 50 ms.
- Resulting edges: ~3 K (hunks typically straddle 1–4 declarations).

We do **not** persist `modifies` to the manifest's per-file edge list
because hunks aren't owned by source files in the cache sense. Instead,
on incremental builds the entire `modifies` set is dropped and re-built
(see §3.5). The cost is small and the correctness is obvious.

### 4.4 Determinism

`emitTemporalEdges` already runs after a stable-sorted node list (the
language parsers sort by file path). The `modifies` emission iterates
hunks in `(sha, file, idx)` order and within each hunk iterates
candidate CodeNodes in declaration order. The resulting edge slice is
appended to `g.Edges` in deterministic order; the existing `validateAndSanitize`
gate runs unchanged.

---

## 5. EvidencePack assembler (H3)

> **Status (2026-05-10)**: Landed. `pkg/evidence/evidence.go` `BuildPack`
> implements the §5.2 ranking algorithm; `internal/mcp/evidence.go`
> registers `evidence_for_intent` as the 8th MCP tool;
> `internal/server/api.go` `handleEvidence` exposes the same assembler
> via `GET /api/evidence`. §11.3 retrieval boundary enforced two ways
> — the MCP wrapper filters the storeReader, and `indexCorpus` itself
> drops AMBIGUOUS Hunk/Commit rows + AMBIGUOUS edges as defense in
> depth. Performance: a per-process `pkg/evidence.Cache` (sync.RWMutex
> + manifest-keyed invalidation) amortises the BM25 corpus build
> across queries — go-stablenet (240K nodes / 9K hunks) goes from
> ~5.2s cold to ~0.18s warm (~28× speedup); both `ckg serve` and the
> MCP `Run` hold a single Cache for their lifetime, so the second
> query against the same `graph.db` already pays the warm cost. Self-graph eval (go-stablenet, /tmp/ckg-h2): query
> `intent=release merge dev` returns 3 EXTRACTED commits about
> secp256k1 / GovMinter / EIP-7951 even though the AMBIGUOUS
> "release: merge dev to master (#80)" exists in the same corpus —
> proving the boundary holds end-to-end. 8 unit tests cover ranking,
> §11.3 filtering, seed_qname expansion, budget cap, empty intent,
> empty corpus, qname parser, and gunzip helper.

### 5.1 New MCP capability

H3 adds one new MCP tool, registered alongside the existing seven in
`internal/mcp/server.go:21-27`:

```go
registerEvidenceForIntent(s, store)
```

Tool name (in the MCP namespace): `evidence_for_intent`. Schema:

```
inputs:
  intent      string  (required)  free-text task description
  seed_qname  string  (optional)  if set, restrict to hunks that modify
                                  this CodeNode or its callers/callees
                                  (one BFS hop)
  k           int     (optional)  top-K commits to return (default 5)
  budget_tokens int   (optional)  stop early once accumulated patch
                                  text exceeds this (default 6000)

outputs: EvidencePack JSON (see §1.5)
```

### 5.2 Ranking algorithm (H3 v0)

For the first cut, BM25 over a per-hunk virtual document:

```
hunk_doc(h) = commit.subject(h.commit)
              || h.patch_text
              || ' '.join(qname for c in h.modifies for c in c.qualified_name)
```

We piggy-back on the existing `pkg/bm25` Scorer (`pkg/bm25/scorer.go`).
The corpus is built once at server startup (or lazily on first call,
guarded by a sync.Once). For self-graph that is ~700 documents at
~1 KB each = ~700 KB — trivially fits in memory.

Scoring pass:

1. BM25-score every hunk against the intent → top 50.
2. If `seed_qname` set: filter to hunks whose `modifies` reaches
   `seed_qname` directly OR via one G3 hop (calls/invokes). Drop the
   rest, take the top-K survivors.
3. Group by commit SHA; for each commit, attach all its hunks (the
   adjacency edge means an Agent reading the patch sees the full
   change, not just the matching hunk).
4. Decorate each hunk with its `modifies` neighbours (resolved to
   CodeNode metadata: qname, type, file_path, start_line/end_line —
   no body bytes; the Agent fetches `/api/blob/{id}` if it wants the
   current source).
5. Order commits by `commit.timestamp` DESC (recency tie-break) and
   stop when the cumulative `patch_text` size exceeds `budget_tokens`.

### 5.3 Why BM25 first, embeddings later

Three reasons:

1. **No new infra dependency.** The S0 acceptance constraint
   (`docs/archive/STATUS-REPORT-2026-05-04.md` and similar) requires
   determinism: same DB, same query, same output. BM25 is deterministic;
   any embedding-based ranker introduces a model-version dependency.
2. **Quality is good enough at this scale.** The Agent's queries are
   short and topical ("panel visibility", "rate limiter"). BM25 over
   ~700 patch documents lands plausible top-K consistently; the
   bottleneck is patch dedup and recency, not semantic recall.
3. **The S1 follow-up (CKV) is already on the map.** When CKV's
   embedding store lands, `evidence_for_intent` can fall back to
   embed-and-rank with the same hunk-doc shape — the schema doesn't
   change, only the scorer.

### 5.4 The seed-qname expansion

When `seed_qname` is set, the tool widens the candidate set to the
seed plus its 1-hop neighbourhood in G3 (`calls`, `invokes`). The
rationale: the Agent often seeds with "the function I am editing",
but the most informative recent commits may be on its **callers**
(behavioural change) or **callees** (dependency drift). A 1-hop BFS
matches the existing `get_subgraph` MCP tool's depth=1 default
(`internal/mcp/tools.go` → `registerGetSubgraph`).

The expansion is bounded: depth=1, max-degree-cap = 50 per node (avoid
exploding around super-hub functions). Beyond depth=1 the recall noise
overwhelms the precision gain — we'll revisit if eval shows otherwise.

### 5.5 Citation enforcement

Every Hunk in the response carries `file_path` + `start_line` (from the
Node row). This satisfies the existing citation-warn contract documented
on `pkg/smartctx/smartctx.go:11-15`. No new code path is needed; the
shape just inherits the existing convention.

---

## 6. API extension

### 6.1 New / modified endpoints

The existing routes are registered in `internal/server/server.go:65-74`.
H3 adds three new ones (the schema bump alone allows them — they would
no-op against pre-1.8 DBs, returning `[]`):

| route | purpose |
|---|---|
| `GET /api/nodes/top?metric=…&limit=…&excludeTypes=Commit,Hunk` | new `excludeTypes` query param. Default behaviour unchanged when omitted. Used by the viewer's boot path so the initial canvas isn't dominated by Hunk / Commit nodes once they exist. |
| `GET /api/hunks?commit=<sha>&limit=<n>` | hunks belonging to a specific commit; ordered by `(file_path, idx)`. Used by the viewer's "expand commit" affordance. Internally one SQL: `SELECT … FROM nodes WHERE type='Hunk' AND qualified_name LIKE 'hunk:<sha>:%' ORDER BY qualified_name LIMIT ?`. |
| `GET /api/hunks/by-node?qname=<q>&limit=<n>` | reverse lookup: which hunks recently modified the CodeNode with this qname. Joins via `modifies`: `SELECT h.* FROM nodes h JOIN edges e ON e.src=h.id WHERE e.type='modifies' AND e.dst=(SELECT id FROM nodes WHERE qualified_name=?) ORDER BY <commit_time> DESC LIMIT ?`. The order-by needs a join to the Commit node for `signature` parsing — or, cheaper, a denormalised `commit_time` column on Hunk. We do **not** add a column; the join is fine at this scale. |
| `GET /api/evidence?intent=…&k=…&seed_qname=…&budget_tokens=…` | HTTP wrapper around the same engine `evidence_for_intent` MCP tool uses. Returns the EvidencePack JSON described in §1.5. |

### 6.2 Type filter on `/api/nodes/top`

The existing handler (`internal/server/api.go:84-103`) calls
`s.store.TopNodes(metric, limit)` — no type filter. We add a third
optional argument: `excludeTypes []string`. The SQL adds a
`WHERE type NOT IN (?, ?, …)` clause. The whitelist column logic in
`topMetricColumn` (`internal/persist/sqlite.go:381-390`) is unchanged
— still SQL-injection-safe. The value list is sanitised against
`AllNodeTypes()` (any unknown type literal is a 400 BadRequest).

This is needed because once Hunk / Commit nodes exist, `pagerank` and
`usage` rankings give them surprising scores (Hunks have ~0 inbound
edges from anything except their own Commit, so they sink to the
bottom — but Commit nodes have huge in-degree from `changed_in` and
can rank artificially high).

### 6.3 Pagination

None of the new endpoints stream — they all return a JSON array
bounded by `limit` (default 50, max 1000). The existing `handleNodes`
50000 cap is overkill for hunk lookups; the lower 1000 cap matches
`handleTopNodes` (`internal/server/api.go:90-92`).

---

## 7. Viewer integration

### 7.1 G6 pill counts

The viewer's right-rail filter (driven by `EdgeTypeFilters.tsx` and
`GRAPH_GROUPS` from `web/viewer-next/src/lib/edges.ts:164-198`) shows
per-graph edge counts. After H1+H2 lands and `GRAPH_GROUPS.G6.edges`
includes `has_hunk`, `modifies`, `adjacent`, the existing aggregation
naturally picks up the new edges. No code change needed beyond the
edges.ts entry.

### 7.2 NodeDetail "Modified in" panel

For Function / Method / Type / Field nodes, the right-rail node detail
gains a section:

```
Modified in (3 most recent)
  ─────────────────────────
  abc12345 fix panel re-mount jitter      2026-04-30
  +12 -3   web/viewer-next/Panel.tsx
  ─────────────────────────
  fed98765 add ARIA roles to panel        2026-04-12
  +24 -0   web/viewer-next/Panel.tsx
  ...
```

Backed by `GET /api/hunks/by-node?qname=<q>&limit=3`. Click a hunk row
to:
1. Highlight that Hunk's modified CodeNodes on the canvas (via the
   existing selection mechanism).
2. Open a side panel with the gzipped patch text, fetched from
   `GET /api/blob/{hunk_id}`.

### 7.3 Commit timeline view

A new top-level view (toggled from the left-rail navigator):

```
Commit timeline
  Apr 30, 2026
    abc12345 fix panel re-mount jitter
      ▾ Panel.tsx           +12 -3   modifies Panel.render
      ▸ panel.test.tsx      +8 -0    modifies test_panel_renders
  Apr 28, 2026
    ...
```

Each row is a Commit; expanding shows its hunks; expanding a hunk
shows its `modifies` neighbours. The list is paged (50 commits per
page, ordered by `commit.timestamp` DESC). Backed by:
- `/api/hierarchy?kind=commit` (new — returns a flat list shaped like
  the existing pkg/topic hierarchies)
- `/api/hunks?commit=<sha>` per row expansion.

Style note: the timeline shares the existing left-rail HierarchyTree
component; only the data source differs. No new Three.js work.

### 7.4 Default visibility

Hunk nodes are **not** rendered on the main 3D canvas by default. Only
their edges-to-CodeNodes (`modifies`) are visible if the user toggles
G6 on. This keeps the "code map" reading intact; users who want
historical context use the right-rail panel and the timeline view, not
the canvas.

This default mirrors `changed_in`'s existing default in
`web/viewer-next/src/lib/edges.ts:114-115`.

---

## 8. Storage and cost analysis

### 8.1 Per-hunk footprint

| component | bytes (typical) | bytes (worst case) |
|---|---|---|
| `nodes` row | ~200 | ~400 (long signature) |
| `blobs` row (gzip patch) | ~500 | ~5000 (large refactor hunk) |
| `has_hunk` edge | ~80 | ~80 |
| `modifies` edges | 1–4 × ~80 = 80–320 | up to 10 × 80 = 800 |
| `adjacent` edge | 0–1 × ~80 = 0–80 | up to 1 × 80 = 80 |
| **per-hunk total** | **~1 KB** | **~6 KB** |

### 8.2 Self-graph projection

178 commits × ~4 hunks/commit ≈ 700 hunks → ~700 KB nodes/blobs +
~240 KB edges = **~1 MB added** to the current ~23 MB graph.db. Within
noise of normal build variance.

### 8.3 Large monorepo projection

10 K-commit repo × ~10 hunks/commit ≈ 100 K hunks → ~100 MB blobs +
~24 MB edges = **~125 MB added**. Manageable, but worth a knob:
`Options.HunkDepth` (mirroring `Options.TemporalDepth` —
`internal/buildpipe/pipeline.go` Options block). Default = unlimited
for the first cut; a single env var (`CKG_HUNK_DEPTH=500`) caps to
the 500 most-recent commits when storage matters.

### 8.4 Index considerations

The existing `idx_nodes_type` (`internal/persist/schema.sql:30`) makes
"all hunks" / "all commits" queries fast. The reverse-lookup endpoint
(`/api/hunks/by-node`) joins edges by `type='modifies'` and `dst=<id>`;
both are covered by `idx_edges_type` (line 50) + `idx_edges_dst` (line
49). No new indexes required.

The "hunks of commit X" query uses `qualified_name LIKE 'hunk:<sha>:%'`,
which the existing `idx_nodes_qname` (line 28) covers as a prefix scan.
For a million-hunk DB this might warrant a dedicated `commit_id` column;
for now the prefix scan is fine.

### 8.5 Build-time impact (re-cap)

H1 adds ~1 second to self-graph build (§3.7). The cold-rebuild policy
on schema bump (§2.6) means the *first* 1.8 build is full cold —
existing time budget already absorbs that.

---

## 9. Migration and rollout

### 9.1 Stages

| stage | scope | client visibility |
|---|---|---|
| H1 | git collector + Hunk node + has_hunk + adjacent + blob | invisible (no edges to CodeNodes; Hunk nodes hidden on canvas by default) |
| H2 | modifies edges (Hunk → CodeNode) | viewer right-rail: "Modified in" panel works; G6 pill includes new counts |
| H3 | EvidencePack MCP tool + `/api/evidence` HTTP route + `/api/hunks*` routes | Coding Agent can call `evidence_for_intent`; commit timeline view ships |
| H4 | issue-id extraction (`Fixes #123`, `[INGEST-456]`, etc.) into hunk metadata | EvidencePack output gains `issue_ids: []`; viewer shows badges |

Each stage is a separate PR. H1 is a no-op for existing clients. H2
reuses the same DB; viewer changes are additive (new panel, no removals).
H3 is a new MCP capability — old MCP clients still see the seven existing
tools. H4 is a metadata enrichment — no schema change, just a parser
addition.

### 9.2 Feature flag

For H1 and H2, gate the entire emission behind a build-time flag in
`Options` (e.g. `Options.EmitHunks bool`, default `false` until H3 is
green in eval). When false, `LoadHunks` is not called, no Hunk rows
exist, and the schema bump still happens (because the schema *can*
hold them, even if empty). This gives operators a safety lever to
roll back if the H1 git-diff cost surprises them.

### 9.3 Backfill

Not applicable. The schema bump forces a cold rebuild on first run,
and the cold rebuild materialises all hunks from the full git history
in one shot. There is no "old DB to migrate"; old DBs are just rebuilt.

### 9.4 Reverting

If H1 causes a regression, the rollback is:
1. Revert the `Options.EmitHunks` default to `false`.
2. Bump SchemaVersion back to "1.6" (or "1.7-norevert" — operators with
   1.7 DBs would get an automatic cold rebuild).
3. Delete `Hunk` rows + `has_hunk` / `modifies` / `adjacent` edges via
   `DeleteEdgesByType` + a one-off `DELETE FROM nodes WHERE type='Hunk'`.

The blob rows fall away via the existing `ON DELETE CASCADE` on
`blobs.node_id` (`internal/persist/schema.sql:69`).

### 9.5 Documentation

- `docs/SCHEMA.md`: add "Node types (34)" entry for `Hunk`, "Edge types
  (35)" entries for `has_hunk` / `modifies` / `adjacent`, and bump the
  schema-version header from 1.7 to 1.8.
- `internal/persist/SCHEMA.md`: same bump.
- `docs/archive/G6-INCREMENTAL-REDESIGN.md` (archived 2026-05-11):
  no further follow-up needed — G6 v4 + C1 have landed; partial-cache
  routing is stable and the design history is preserved for reference.
- `docs/SESSION-HANDOFF-2026-05-10.md` (current hand-off): mentions the
  Hunk graph stages and MCP `evidence_for_intent` tool. Older
  `docs/archive/HANDOFF-2026-05-04.md` is preserved for context only.

---

## 10. Acceptance criteria per stage

### 10.1 H1 (Hunk nodes + has_hunk + adjacent + blob)

```
sqlite3 data/graph.db "
  SELECT COUNT(*) FROM nodes WHERE type='Hunk';
"
```
Expected: > 0 for any non-empty git history, 0 for non-git checkouts.

```
sqlite3 data/graph.db "
  SELECT COUNT(*) FROM edges WHERE type='has_hunk';
"
```
Expected: equal to the Hunk count (each hunk has exactly one `has_hunk`
incoming).

```
sqlite3 data/graph.db "
  SELECT n.id, length(b.source)
  FROM nodes n JOIN blobs b ON b.node_id = n.id
  WHERE n.type='Hunk' LIMIT 5;
"
```
Expected: 5 rows, each with positive blob size; manually decompress the
first row's blob and verify it parses as a valid unified-diff hunk
(starts with `@@ -…,+… @@`).

Test (`internal/temporal/hunks_test.go`): a small fixture repo (5 known
commits, 3 known files) → assert the produced HunkInfo slice matches a
golden snapshot.

### 10.2 H2 (modifies edges)

```
sqlite3 data/graph.db "
  SELECT COUNT(*) FROM edges WHERE type='modifies';
"
```
Expected: > 0 once H1 has populated Hunks.

Manual spot-check: pick a recent commit known to have edited
`internal/buildpipe/pipeline.go:Run`. Run:

```
sqlite3 data/graph.db "
  SELECT n.qualified_name
  FROM nodes h
  JOIN edges e ON e.src = h.id
  JOIN nodes n ON n.id = e.dst
  WHERE h.type='Hunk'
    AND h.qualified_name LIKE 'hunk:<sha-prefix>:%pipeline.go%'
    AND e.type='modifies';
"
```

Expected to include `…pipeline.go:Run` (or whatever symbol the commit
actually touched).

Unit test (`internal/buildpipe/temporal_test.go`): build a fixture
graph with 2 functions at lines [10,30] and [40,60], emit a hunk at
[25,35] → expect both `modifies` edges; emit a hunk at [32,38] → expect
zero `modifies` edges.

### 10.3 H3 (EvidencePack)

End-to-end test (`internal/mcp/evidence_test.go`): seed the test repo
with three commits whose subjects mention "panel visibility";
`evidence_for_intent(intent="panel visibility flicker", k=3)` returns
those three commits in recency order.

Determinism: invoke the tool twice with identical input; the JSON
output is byte-identical (S0 acceptance — same shape as the existing
`get_context_for_task` determinism contract in
`pkg/smartctx/smartctx.go`).

Token-budget honoured: when `budget_tokens=500` and the third commit's
hunk would push the cumulative size over 500, only the first two
commits are returned.

### 10.4 H4 (issue-id extraction)

> **Status (2026-05-10)**: Landed. `internal/temporal/issueid.go` carries
> the four regex patterns (`ExtractIssueIDs` + `EncodeIssueIDs` /
> `DecodeIssueIDs`); `internal/buildpipe/temporal_hunks.go` wires the
> extraction into `buildHunkNodes` so every Hunk's `doc_comment`
> column ends up with `issues:GH-123;ABC-456` whenever its parent
> commit's subject matches a pattern. `pkg/evidence/evidence.go`
> aggregates the per-Hunk encoding into `CommitInfo.IssueIDs` so the
> EvidencePack JSON surfaces tickets per-commit. The viewer's
> `NodeDetail` panel renders amber pills for each issue ID.
> Self-graph eval (go-stablenet, /tmp/ckg-h4): 8,666 of 8,967 hunks
> (97%) carry issue links, all `GH-N` form (the codebase uses GitHub
> PRs exclusively). Top tickets by hunk-count: GH-66 (501), GH-14
> (449), GH-7 (381), GH-28 (374). EvidencePack JSON for an
> arbitrary intent now includes `commit.issue_ids: ["GH-18"]` for
> the matching commit. 11 unit tests cover regex coverage, dedup,
> false-positive guards, encode/decode round-trips, and prefix
> distinction from plain doc_comment.

Patterns the parser recognises (regex in `internal/temporal/issueid.go`):

| pattern | example | issue_ids field |
|---|---|---|
| GitHub-style `#NNN` | `Fixes #123` | `["GH-123"]` |
| Linear-style bracket | `[ABC-456]` | `["ABC-456"]` |
| Jira-style bare | `INGEST-789: ...` | `["INGEST-789"]` |
| URL form | `Closes https://github.com/foo/bar/issues/42` | `["GH-foo/bar#42"]` |

Storage: a JSON column would be ideal, but to avoid a schema change we
encode `issue_ids` as a `;`-separated list inside the Hunk's
`doc_comment` column, prefixed with `issues:` so it's parseable but
doesn't conflict with normal text. Acceptance:

```
sqlite3 data/graph.db "
  SELECT id, doc_comment FROM nodes
  WHERE type='Hunk' AND doc_comment LIKE 'issues:%' LIMIT 5;
"
```

Expected: rows whose corresponding commit subject contained one of the
patterns. Spot-check one against `git show <sha> --format=%B`.

---

## 11. Open questions and decision points

These are flagged for explicit user decision before implementation
begins. The recommended default is in **bold**.

> **Decisions log (finalised 2026-05-09 — H1 implementation):**
>
> | § | Decision |
> |---|----------|
> | 11.1 | gzip (recommendation accepted) |
> | 11.2 | no dedup in H1 (recommendation accepted) |
> | 11.3 | hybrid: `confidence` enum encodes reachability — H1 only emits HEAD-reachable hunks (`EXTRACTED`); a follow-up PR will reflog/fsck-collect unreachable hunks marked `AMBIGUOUS`. **H3 EvidencePack assembler MUST filter to `confidence='EXTRACTED'`** so the LLM never sees force-pushed-away code paths; the AMBIGUOUS rows remain available for the recovery use-case (an agent's overwrite mistake) via the viewer / direct SQL. See §11.3 below for the expanded note. |
> | 11.4 | target file extension; non-{go,ts,sol} → 'git' (recommendation accepted) |
> | 11.5 | out of scope for H1–H3 (recommendation accepted) |
> | 11.6 | 64 KB cap, first 32 KB + truncation marker + last 32 KB (recommendation accepted) |
> | 11.7 | exclude both Commit and Hunk from PageRank + Leiden (recommendation accepted) |
> | 11.8 | do NOT record Hunk node IDs in per-file NodeIDs (recommendation accepted) |

### 11.1 Patch encoding: gzip vs zstd vs raw

- **gzip** is in the Go standard library (`compress/gzip`), already
  proven on this codebase, ~70 % size reduction on diff text, ~50 MB/s
  decompress on typical hardware.
- zstd is faster and compresses ~10 % better, but adds a dep
  (`github.com/klauspost/compress/zstd`). The marginal benefit at
  ~700 KB total scale is invisible.
- raw is simplest and lets `sqlite3 -line "SELECT source FROM blobs"` Just
  Work — but at 100 K-hunk scale that's 350 MB instead of 100 MB.

**Recommendation: gzip.** Revisit if a future ML-eval shows the
decompress cost dominating retrieval latency.

### 11.2 Hunk dedup across rebased commits

When a feature branch is rebased, the same logical hunk can appear
under multiple SHAs (the original + the rewritten version). Should we
deduplicate?

- Pro: cleaner BM25 corpus, fewer near-duplicate hits.
- Con: dedup-by-content-hash loses the chronology. The Agent often
  wants to see "the latest version of the fix"; dedup might surface
  the abandoned original instead.

**Recommendation: do not dedup in H1.** Add a `signature_sha`
content-hash column on Hunk in a follow-up if the duplicate count
exceeds ~10 % of hunks in real eval.

### 11.3 Soft-delete for unreachable commits (force-push / reset)

If a commit is no longer reachable from HEAD (its branch was
force-pushed away), its Hunk rows are stale. Options:

- A. Leave them. Storage cost is small; the patch is still valid as
  historical evidence.
- B. Mark them with a `confidence='AMBIGUOUS'` flag and a `reachable`
  bit somewhere — viewer / retrieval can de-rank.
- C. Delete them on the next build that detects unreachability.

**Recommendation: A for H1.** Add the reachability check in H4 along
with issue-id extraction (the same pass over `git log <head> --` gives
us the live SHA set).

**Decision 2026-05-09 (hybrid — supersedes the original A recommendation):**
We need three layers, partitioned across schema-1.8 H1 and a follow-up PR:

- *Storage layer* (H1): every hunk row uses the existing `confidence` enum
  to tag reachability. H1 only walks `git log HEAD --` so every emitted
  hunk gets `confidence='EXTRACTED'`. No new schema column.
- *Storage layer* (follow-up PR after H1, separate change): a reflog/fsck
  pass enumerates unreachable SHAs (force-pushed-away branches, hard
  resets) and inserts their hunks with `confidence='AMBIGUOUS'`. The
  follow-up PR is independently reviewable — H1 stays scoped.
  **Landed**: see `internal/temporal/unreachable.go` +
  `emitUnreachableHunkGraph` in `internal/buildpipe/temporal_hunks.go`.
  Self-graph eval (go-stablenet, 2026-05-09): 14 AMBIGUOUS Commit
  nodes + 40 AMBIGUOUS Hunk nodes captured from reflog ∪ fsck-
  unreachable, disjoint from the 6402-EXTRACTED HEAD-reachable set.
- *Retrieval layer* (H3): `evidence_for_intent` and `/api/evidence` MUST
  add `WHERE n.confidence = 'EXTRACTED'` to every Hunk projection. The
  Coding Agent never sees code paths that were rolled back, even if the
  unreachable hunks live in the same DB. The viewer can show them in a
  dedicated "Recovery" panel (out of scope for H1) so a human can
  manually consult them when an agent overwrites code.
  **Landed**: `internal/mcp/h3_filter.go` introduces an
  `llmSafeStoreReader` that wraps the persist.StoreReader passed into
  `mcp.Run` — every tool (find_symbol / find_callers / find_callees /
  get_subgraph / search_text / get_context_for_task / impact_of_change)
  receives the wrapped reader, so AMBIGUOUS Hunk/Commit rows are
  dropped from `FindSymbol`, `NodesByIDs`, `Subgraph/NeighborhoodByQname`,
  `Search`, `SearchFTS` results and `GetBlob` returns sql.ErrNoRows for
  AMBIGUOUS Hunk IDs. The HTTP `/api/*` surface (server/api.go)
  intentionally stays unfiltered — those endpoints power the human
  viewer where a future Recovery panel will surface the AMBIGUOUS
  track deliberately. The new endpoint `/api/evidence` (when added)
  will share the boundary by re-using `llmSafeStoreReader`.

The recovery use-case is the user-stated motivation: an autonomous agent
sometimes overwrites correct code, and a force-push that rolled the
unwanted change back must still be inspectable by humans. The hybrid
keeps that history without leaking it back into the LLM's input.

### 11.4 Multi-language hunks

A hunk that touches a `.go` file is unambiguously `language='go'`. But
what about a hunk in a `.yaml` manifest, a `.proto` file, or a
markdown doc? The existing language enum is `{go, ts, sol, git}`.

**Recommendation: hunks inherit their target file's language by
extension. Files outside `{go, ts, sol}` get `language='git'`** (the
sentinel already used for NodeCommit). The BM25 corpus indexes them
all; the language filter on the canvas naturally excludes them from
language-specific views.

### 11.5 Cross-commit hunk linking

Sometimes a "fix" spans multiple commits — a flag flip, a follow-up
test, a doc update. Should we cluster these into a logical change?

**Recommendation: out of scope for H1–H3.** The Coding Agent's
EvidencePack already returns multiple commits ranked by recency, so the
common case ("show me the last 3 panel-fix commits") naturally
includes the follow-ups. A formal `same_logical_change` edge would
need a heuristic (issue-id collision? same author within 24 h?) that
we don't have evidence for yet. Revisit at H4+ if eval shows demand.

### 11.6 Blob retrieval for large hunks

A pathological hunk (e.g. a generated-code regeneration) can be > 1 MB.
The existing `GetBlob` (`internal/persist/sqlite.go:451`) loads the full
blob into memory. For the Coding Agent this is fine — it asks for
specific hunks. For the viewer a 1 MB patch in a side panel will lag.

**Recommendation: cap patch_text size at 64 KB.** Patches larger than
that are stored truncated (last 32 KB + "[... truncated, N bytes ...]"
marker + first 32 KB). The acceptance test in §10.1 includes a fixture
with a > 64 KB hunk to exercise the truncation path.

### 11.7 PageRank / community computation on the new node type

`score.Compute(g)` (`internal/score/`) and the cluster builders
(`cluster.BuildPkgTree` / `cluster.BuildTopicTree` —
`internal/buildpipe/pipeline.go:58-59`) walk the full node set. After
H1, ~700 extra nodes pass through them. This is fine for self-graph
but:
- PageRank now sees Commit and Hunk as ranking participants. We
  probably want them excluded — Hunks have ~1 inbound edge each
  (`has_hunk`) and would rank near-zero, which is correct but noisy in
  metrics.

**Recommendation: exclude `Commit` and `Hunk` from PageRank/Leiden
inputs in `score.Compute` and `cluster.BuildTopicTree`.** Implement as
a type whitelist in those functions; no schema change.

### 11.8 Manifest size

`persist.Manifest` (`internal/persist/manifest.go`) currently records
per-file `NodeIDs` / `EdgeIDs`. Hunk node IDs are not file-owned (a
hunk's file_path matches a source file, but the hunk itself isn't
owned by that file in the cache sense — it's owned by its commit). If
we record them in `Files[file].NodeIDs` we'd inflate the manifest and
get spurious cache invalidations.

**Recommendation: do NOT record Hunk node IDs in per-file NodeIDs.**
Hunks live outside the file-level cache; they're regenerated wholesale
on each build that calls `emitTemporalEdges`. Same idiom as Commit
nodes today.

**Decision 2026-05-09: accepted.** Implemented as the shared
`isMetaNodeType` helper in `internal/buildpipe/temporal_hunks.go` —
both `computeColdFileEntries` (cold rebuild) and `buildFileEntries`
(incremental rebuild) call it to skip Commit + Hunk before populating
`FileEntry.NodeIDs`. `extractBlobs` uses the same helper to skip meta
nodes (Hunk patch bytes come from the side-channel `hunkBlobs` map
returned by `emitTemporalEdges`, not from a file slice).

---

## 12. Test plan

### 12.1 Unit tests

| package | what | sample |
|---|---|---|
| `internal/temporal` | `parseUnifiedDiff` handles standard hunks, renames, binary, mode-only changes, multi-hunk files | golden tests in `testdata/diffs/` |
| `internal/temporal` | `LoadHunks` against a small fixture repo (`testdata/repo/`) — 3 commits, 2 files, 5 hunks total → expected HunkInfo slice | snapshot test |
| `internal/buildpipe` | `buildHunkNodes` emits stable IDs (sort-by-id assertion) | unit test |
| `internal/buildpipe` | `buildModifiesEdges` interval-overlap correctness: 5 cases (fully inside, fully containing, left-overlap, right-overlap, disjoint) | table test |
| `pkg/types` | new `NodeHunk` literal at index 33 in `AllNodeTypes()`, new edge literals at correct indices in `AllEdgeTypes()` | extends `TestAllNodeTypes_Stable` (`pkg/types/types_test.go:65`) |
| `internal/mcp` | `evidence_for_intent` shape conformance and determinism | integration test using `testdata/repo` |

### 12.2 Integration tests

A new `internal/e2e/hunk_e2e_test.go` that:

1. Initialises a test git repo at `t.TempDir()`.
2. Creates 5 commits with known content + subjects ("add foo",
   "fix foo bug", "extend foo", "rename foo to bar", "delete bar").
3. Runs `buildpipe.Run` against it.
4. Opens the resulting `graph.db` read-only.
5. Asserts: 5 Commit nodes, ≥ 5 Hunk nodes, ≥ 5 has_hunk edges,
   ≥ 5 modifies edges (the renames + deletes contribute fewer).
6. Asserts blob roundtrip: decompress one Hunk's blob, parse with
   stdlib `bufio.Scanner` for the `@@` header.

### 12.3 Performance test

`internal/buildpipe/temporal_perf_test.go`:

1. Fabricate a synthetic repo with 1000 commits × 3 files × 1
   hunk/file = 3000 hunks (using `git fast-import` so it's quick).
2. Run `emitTemporalEdges` and assert wall time < 30 s on CI hardware.
3. Assert peak memory delta < 200 MB.

This test gates against accidental O(N²) regressions in the join and
catches the "git diff per-commit fork" overhead if a future patch
inadvertently sequentialises the worker pool.

### 12.4 Eval impact

Add one new task to `eval/tasks/` (the existing eval harness — see
`eval/` directory). Task name: `evidence_recall_panel`. Inputs: a known
commit history with 3 panel-related fixes; intents to query;
expected top-K SHAs. Acceptance: top-3 recall ≥ 0.66 (2/3 expected
SHAs in top-3) on the self-graph baseline.

---

## 13. Implementation order summary

For the engineer who lands this:

1. **PR 1 (H1, schema 1.7).** New `internal/temporal/hunks.go` collector;
   wire into `emitTemporalEdges`; `pkg/types/enums.go` `NodeHunk` +
   `EdgeHasHunk` + `EdgeAdjacent`; `web/viewer-next/src/lib/edges.ts`
   `EDGE_STYLE` + `GRAPH_GROUPS.G6` updates; `internal/buildpipe/cache.go`
   `SchemaVersion = "1.8"`; unit + golden tests. NOT yet emitting
   `modifies`.

2. **PR 2 (H2).** `EdgeModifies`; `buildModifiesEdges` in `temporal.go`;
   AST overlap whitelist constant; integration test for interval
   overlap. Viewer's NodeDetail "Modified in" panel ships in this PR.

3. **PR 3 (H3).** `internal/mcp/evidence.go` (new file) registering
   `evidence_for_intent`; `internal/server/api.go` `/api/evidence` +
   `/api/hunks` + `/api/hunks/by-node` handlers; `excludeTypes` query
   param on `/api/nodes/top`; commit timeline viewer view; eval task.

4. **PR 4 (H4).** `internal/temporal/issueid.go` parser; HunkInfo
   carries `issue_ids`; encode into `doc_comment` as `issues:…`;
   EvidencePack output includes the field; viewer badges.

Each PR is independently reviewable, ships a measurable improvement,
and is reversible by reverting only that PR.

---

## 14. Glossary

| term | meaning in this doc |
|---|---|
| **Hunk** | one contiguous block of changed lines in one file in one commit, as defined by unified-diff `@@` headers. The atomic unit of patch content. |
| **CodeNode** | any of the AST-derived nodes the language parsers emit: Function, Method, Type, Field, etc. As distinct from meta nodes (Commit, Hunk, File, Package). |
| **EvidencePack** | the JSON envelope returned by the new MCP tool `evidence_for_intent` and the HTTP route `/api/evidence`. Shape spelled in §1.5. |
| **Few-shot retrieval** | the Coding Agent pattern of "give me 3 past examples of solving a similar problem, so I can copy the pattern". Hunks are the units of those examples. |
| **G6 Temporal axis** | the 6th of CKS's six graph axes; covers git-history-derived facts. Existing edges: `changed_in` (file-level), `blame` (file→latest commit). New edges from this design: `has_hunk`, `modifies`, `adjacent`. |
| **`modifies` edge** | the line-overlap link from a Hunk to a CodeNode whose declaration range covers some of the hunk's changed lines. The pivot that gives the Agent symbol-precise filtering. |
