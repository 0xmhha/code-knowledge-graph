# Viewer & Search Limitations — Roadmap

This document tracks UX/perf gaps surfaced during go-stablenet
(212K nodes / 314K edges) viewer testing on 2026-04-26, and the planned
remediation. Companion to `ARCHITECTURE.md` and `EVAL.md`.

## Current state (post-`94d8de0`)

- 5 subcommand binary working end-to-end.
- 3D viewer renders 182 top-level Go packages from go-stablenet at L0.
- Click → expand: pulls children + edges, edges now wire correctly to
  3d-force-graph after `linkSource('src')/linkTarget('dst')` fix.
- Hover tooltip: type, qname, file:line, lang, conf, edges, usage, PR.
- Node list panel + selected-node detail + zoom controls (＋ / − / ⟲).
- Search (FTS5): English token-exact match works; Korean & prefix do not.

## Known limitations & options

### L1 — Korean (CJK) search returns 0 hits

**Root cause.** SQLite FTS5 default `unicode61` tokeniser treats CJK
text as a single un-segmentable token. A user searching `결제` against
text `결제시스템` gets no hits because the tokeniser stored
`결제시스템` as one token.

| Option | Where | Pros | Cons |
|---|---|---|---|
| **A. Substring fallback** | server (`Store.SearchSubstr`) — LIKE on name+qname | Immediate fix for any non-ASCII query, no schema change | O(n) scan, ~50–100 ms on 200K rows |
| **B. Trigram tokeniser** | server schema (FTS5 `tokenize='trigram'`) | Indexed CJK matching, exact precision | FTS index ~3× larger, full reindex required |
| **C. External index (Bleve)** | server: replace FTS5 | CJK + phonetic + analyzer rich | Adds dep, parallel index, maintenance |

**P0 chosen: A.** Activated only when query contains any non-ASCII byte;
ASCII queries continue through FTS5 for index-backed speed.

### L2 — Prefix search (`gene` → `gene*`) requires explicit wildcard

**Root cause.** FTS5 matches whole tokens by default. `gene` won't hit
`generator` unless typed as `gene*`.

| Option | Where | Pros | Cons |
|---|---|---|---|
| **A. Auto prefix** | server (`handleSearch`) — append `*` if last token has no special FTS chars and length ≥ 2 | One-line server fix, autocomplete-feel UX | Slight semantic drift: exact match no longer the default |
| **B. Multi-strategy** | server — try exact, then prefix, then substring; merge + dedupe | Most thorough recall | Slower, ranking complexity |
| **C. UI hint** | client placeholder text | Zero code | User must learn syntax |

**P0 chosen: A.** Activates only on benign queries; quoted phrases or
queries already containing `*`/`(`/`)`/`:` are passed through unchanged.

### L3 — Visible set explodes on deep expand (perf / cognitive load)

**Root cause.** `Package` → `File` (5–50) → `Function/Method` (5–500)
→ `CallSite/IfStmt/...` (5–50). One click can add 10K nodes; force-graph
fps degrades past 5–10K.

| Option | Where | Pros | Cons |
|---|---|---|---|
| **D1. Click-toggle collapse** | client (focusNode + Store) | Standard tree UX; user-driven cleanup | Doesn't help on first expand |
| **D2. Children cap** | client — limit children fetched per click to 100 | Bounded blast radius per click | User can't see all without override |
| **D3. Type filter toggles** | client (bottom bar) — toggle CallSite/IfStmt/etc. | Removes visual noise (statement-level nodes) | New UI surface |
| **D4. LOD-aware visible filter** | server + client — at low LOD, server returns top-N by pagerank | Automatic detail control | Complex; needs ranked queries |
| **D5. 2D fallback above N** | client — switch renderer mode at threshold | Maintains usability | Loses 3D affordance |

**P0 chosen: D1 + D2.** Toggle for re-clicks; cap of 100 for safety.

### Future (V1+)

- Trigram FTS5 tokeniser (L1-B)
- Type-filter UI (L3-D3)
- LOD-aware ranked queries (L3-D4)

## Priorities

| Priority | Item | LOC | Status |
|---|---|---|---|
| **P0** | L1-A: Korean substring fallback | ~25 server | 적용 |
| **P0** | L2-A: Auto prefix `*` | ~5 server | 적용 |
| **P0** | L3-D1: Click-toggle collapse | ~30 client | 적용 |
| **P0** | L3-D2: Children cap 100 | ~5 client | 적용 |
| **P0** | L4-A: Render-storm fix (batch + edge index) | ~80 client | 적용 |
| **P0** | L4-B: Focus + halo highlighting | ~80 client | 적용 |
| **P1** | L3-D3: Type filter | ~30 client | 보류 |
| **P1** | L4-C: Replace-mode visible (focus N-hop only) | ~60 client | 보류 |
| **P1** | L4-D: Path-trace mode (2 nodes → shortest) | ~80 client | 보류 |
| **V1** | L1-B: Trigram tokeniser | schema change | 보류 |
| **V1** | L3-D4: LOD-aware ranked queries | ~40 server + client | 보류 |

---

## L4 — Rendering algorithm overhaul (2026-04-26 round 2)

After P0/P1 search & UX fixes, rendering itself was still the bottleneck.
Profiling revealed two categorical problems:

**L4-A: render-storm.** `focusNode` fired 3–4 store emits per click
(`loadNodes` + `setVisible` + ad-hoc `emit()` + LOD `setLOD`). Every
emit re-ran `sync()`, which rebuilt the whole `graphData` and re-warmed
the force simulation (200 ticks). On top, `renderList` re-rendered all
200 sidebar rows on every emit — pure DOM thrash.

Resolved by:
- `Store.batch(fn)` — coalesces nested emits into one delivery.
- `Store.edgesBySrc` / `edgesByDst` indices — drop the
  `O(|edges|)` adjacency scan inside `sync()` and BFS to `O(|adj|)`.
- `Store.addEdges(arr)` — single dedup'd ingestion path.
- `cooldownTicks(80)` + `cooldownTime(2500)` — faster settle.
- `applyListSelection(el, id)` — selection-only DOM update; row rebuild
  only when the *items* change (visible set or search results).

**L4-B: focus + halo.** All visible nodes were rendered identically,
so a click on a hub package buried the relevant call path among 100+
sibling nodes. Resolved by:
- `Store.computeFocusDistance(id, depth=2)` — BFS over the edge index
  produces a `Map<nodeId, distance>` (focus=0, 1-hop=1, 2-hop=2).
- 3D layer: walks tracked meshes after each `sync()` and writes
  `material.opacity` from the distance — no scene rebuild needed.
- 2D layer: `nodeCanvasObject` reads distance per frame; focus node
  also gets a white outline ring. Out-of-focus edges fade to a dim grey
  via `linkColor` brightness scaling.
- Hover tooltip surfaces the focus distance ("FOCUS / direct / 2-hop")
  so the user can confirm what's been promoted.

Both layers preserve the existing colour-by-language and shape-by-type
encoding; the halo is multiplied on top, never replaces it.

**Future work** still parked under P1: replace-mode visible cap (only
keep the focus N-hop set on canvas) and path-trace (Ctrl+click two
nodes, highlight shortest path between them).

## Test surfaces affected

- `internal/persist`: new `SearchSubstr` covered by `sqlite_extra_test.go`
- `internal/server/api.go`: handleSearch routing covered by `api_extra_test.go`
- `web/viewer/src/main.js`: toggle / cap behaviour exercised manually;
  unit tests for store mutations are V1 work.

---

*Last updated: 2026-04-26.*
