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

---

## L5 — Discrete depth navigation (2026-04-26 round 3)

After L4 the renderer was fast on small visible sets, but the navigation
model still produced unbounded growth: every click added another shell
of children to `visibleIds`, and mouse-wheel zoom triggered
camera-distance LOD that fetched even more nodes implicitly. The user's
exact complaint: "확대, 축소에 따라 일관되지 않는 렌더링이 발생하며,
graph knowledge 를 나타내는 방식이 내가 원하는 방식은 아니며,
렌더링이 많을 수 밖에 없는 구조로 되어 있고".

The redesign replaces the implicit, accumulating model with an explicit
`(anchorId, depth)` pair:

- **anchor** — the node the user is currently centred on. `null`
  means root view (top-level packages).
- **depth** — integer 0…6. visible = anchor's BFS d-hop neighbourhood
  capped at MAX_VISIBLE (500). depth=0 shows just the anchor;
  depth=N expands one shell at a time.

**Server:** new `POST /api/nodes-by-ids` endpoint (persist already had
`Store.NodesByIDs`). The depth BFS needs a single round-trip to fetch
every neighbour's metadata after the edge index gives it the ids.

**Client:** `web/viewer/src/depth.js` owns `recomputeVisible(store, api)`,
called by every depth-in / depth-out / set-anchor / home action. Camera
zoom is now pure visual — no implicit fetches, ever.

**UI:**
- Bottom bar gets ⇱ depth-out, ⇲ depth-in, 🏠 home, plus a `depth N/6`
  indicator.
- Render-time meter `142 ms · 412 nodes / 891 edges` displays the
  measured cost of each navigation step. Captured as
  `t1 = perf.now()` inside `requestAnimationFrame` after the batch
  emits + `onEngineStop` settles.
- Font-size toggle S/M/L (persisted to localStorage), affects both 3D
  HTML tooltips and 2D Canvas label drawing.

**Behaviour change:** clicking a node = "set as anchor at depth 1"
(navigation), not "expand children" (accumulation). Sidebar list clicks
remain inspection-only (selectOnly). Search auto-focuses but doesn't
re-anchor unless the user clicks the canvas node.

**Why it matters for performance debugging:** every step now has a
visible cost number. If depth-in 4→5 jumps from 80 ms to 1.4 s, the
user sees it immediately and can adjust MAX_VISIBLE or report the
specific bottleneck. Camera zoom never triggers fetches, so visible-set
size is stable across pan/zoom.

## Test surfaces affected

- `internal/persist`: new `SearchSubstr` covered by `sqlite_extra_test.go`
- `internal/server/api.go`: handleSearch routing covered by `api_extra_test.go`
- `web/viewer/src/main.js`: toggle / cap behaviour exercised manually;
  unit tests for store mutations are V1 work.

---

*Last updated: 2026-04-26.*
