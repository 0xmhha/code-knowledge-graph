# Eval framework 11-cycle trajectory

> **ARCHIVED 2026-07-18.** Frozen historical record of the C18–C37 eval series
> (2026-05-19 ~ 05-23). The cycle-9 baseline metrics are mirrored in
> `docs/CONTINUITY.md §3`. Kept for provenance; not a live status doc.

> 2026-05-19 ~ 2026-05-23. *Single-source-of-truth* metrics + cycle catalogue
> for the T-04 hallucination-validator series and the 4-axis evaluation
> roadmap. Read this alongside `eval/stablenet/HANDOFF.md` (T-04/T-05
> closing) and `eval/stablenet/CKS-INTEGRATION-2026-05-23.md` (cross-repo
> handoff).
>
> **Why this exists**: commit messages flatten 11 cycles' progression into
> 21+ separate entries. This file shows the *aggregate trajectory* so a
> reader new to the work understands *where the metrics came from* and
> *which cycle moved which needle*.

## 0. Trigger and scope

cks dogfood (2026-05-11) surfaced 14 P0 tasks in `HANDOFF.md`. T-04
(hallucination validator) was the consumer-facing correctness metric the
project needed before any other measurement made sense. The series below
took T-04 from "missing" through *measurement-framework convergence* to
*production-ready measurement*.

## 1. Cycle catalogue

| # | Cycle | Commit | Type | Δ |
|---|---|---|---|---|
| 1 | C18 V0 | `2f58ce7` | Capability | hallucination validator API + 6 tests |
| 2 | C19 V1 wiring | `bc6fe9c` | Integration | runner + CSV/Report metric |
| 3 | C20 infra | `7955b35` | Bug fix | CLIWRAP_AGENT diagnostic + nil-safe Close |
| 4 | C21 paren+prose | `e794504` | Tokeniser noise | `h.vault.Deposit(req`, `e.g` |
| 5 | C22 V2 suffix | `e9ffced` | Heuristic | short-qname match (`Vault.deposit` ↔ `service.Vault.Deposit`) |
| 6 | C23 brace | `dc87a96` | Tokeniser noise | `Vault{...}` struct-literal split + first real LLM noise |
| 7 | C24-C25 Axis 3+1+2+4 | `1db6d2d`+`c368d9c`+`1754b6c`+`63e1d44` | Roadmap | BASELINES + multi-shot + prompt + filter |
| 8 | C27 A+B+D | `22539fc` | Integration fix | β baseline + total-token + multi-lang graph |
| 9 | C29 cycle 6 | `524685a` | Tokeniser noise | line-ref + Hangul separator |
| 10 | C31 cycle 7 | `0d1b2f2` | Tokeniser noise | numeric + #/@/→ separator |
| 11 | C32 V3 | `49a6d26` | Heuristic | receiver-style prefix strip |
| 12 | C34 B audit | `46693a6` | Metric redesign | UserPromptBytes for H1 |
| 13 | C36-37 FTS bugfix | `2a4db90`+`8e8bf9b` | **ckg core bug** | dotted-id syntax + power-user gate |

Total: 21+ commits, ~7 working days, ~6-8 hours active time.

## 2. Hallucination-rate trajectory (α baseline)

```
C20 (cycle 1) — crash, no measurement
C21 (cycle 2) — 28.6% (all false positives from tokeniser)
C22 (cycle 3) — 0.0% (lucky single-shot)
C23 (cycle 4) — 36.4% (first real LLM noise visible)
C26 (cycle 5) — 18.7% (multi-shot baseline)
C28 (cycle 6) — 6.2% (A+B+D + multi-lang graph)
C30 (cycle 7) — 4.2% (line-ref + Hangul cleanup)
C35 (cycle 8) — 20.6% (single-shot variance; mean still trending)
C37 (cycle 9) — 8.3% (V3 + UserPromptBytes + FTS gate)
```

Asymptotic floor: ~4-8% (real LLM noise — Go std-lib reaching).

## 3. Final baseline (post-cycle 9, 2026-05-23)

Source: `make eval-llm-smoke BASELINES=alpha,beta,gamma,delta N_RUNS=3`
against `eval/.synthetic-data` (synthetic T01: "find callers of Vault.deposit").

| Baseline | N | User prompt bytes | Score (mean±std) | Halu rate | Avg mentions |
|---|---|---|---|---|---|
| α (raw file dump) | 3 | 2,245 | 0.396±0.119 | 0.083 | 7.3 |
| β (whole-graph dump) | 3 | 69,422 | 0.746±0.046 | **0.000** | 6.0 |
| γ (5 tools no pre-call) | 3 | 157 | 0.688±0.037 | 0.122 | 7.7 |
| δ (smartContext) | 3 | **12,612** | **0.825±0.035** | **0.000** | 4.7 |

H1 (user-prompt-bytes savings) — δ vs α: **-461.8%**
- δ supplies 5.6× more context than α
- *cost-benefit trade-off measured*; the original "δ uses less context" premise is false

H2 (score delta) — δ - α: **+0.429** ✅ (target ≥ 0)
- δ's task-tuned context substantially improves answer quality

## 4. Real LLM noise catalogue (cycle 9 surviving)

After 11 cycles of tokeniser cleanup, the hallucinations that survive are
genuine LLM inventions, not measurement artifacts:

| Pattern | Source | Disposition |
|---|---|---|
| `http.HandleFunc`, `mux.HandleFunc`, `http.HandlerFunc` | Go std-lib reaching | C — prompt engineering V2 |
| `package.Type.Method`, `pkg.Type.Method` | LLM placeholder usage | C — prompt engineering V2 |
| `expected.symbols` | YAML fixture key leak | Defer (V1+ YAML key heuristic) |
| `Caller.forward`, `SafeMath.add` | Sol convention reach | C — prompt engineering V2 |
| `msg.sender` | Sol global reach | C — prompt engineering V2 |

## 5. ckg core bugs surfaced (eval → ckg flow)

Two ckg core bugs surfaced by eval-driven discovery, both in
`internal/persist/sqlite.go::rewriteFTSQuery`:

| Bug | Discovery | Fix commit |
|---|---|---|
| FTS5 syntax error on dotted identifiers (`Vault.deposit*`) | δ smartContext silent failure (cycle 8 smoke) | `2a4db90` — split on `.` |
| Power-user gate triggers on natural-language quotes | δ smartContext silent failure persisted post-fix #1 (cycle 9 smoke) | `8e8bf9b` — narrow gate to `*`-only or fully phrase-quoted |

Both affect `store.Search` → `SearchFTS` path. Consumers: smartContext
(δ baseline), MCP `find_symbol`/`search_text`, cks `BM25Search`.

**Cks impact (2026-05-23 B-Phase 1 audit)**: cks's `extractKeywords` uses
`identifierRE = /[A-Za-z][A-Za-z0-9_]{2,}/` (no dot), so cks-side queries
never contain dotted identifiers and the bugs do not affect cks at runtime.
cks's outdated ckg version (`v0.0.0-20260513121714-85391f87b404`, 2026-05-13)
also predates the bugs — cks build is clean.

## 6. Lock categories established

The series established five distinct categories of measurement-quality
work. Future cycles can self-classify:

1. **Capability** (C18 V0): introduce missing API
2. **Integration** (C19 V1, C27): wire capability into the runtime path
3. **Tokeniser noise** (C21/C23/C29/C31): drop dot-bearing tokens that
   aren't graph symbols (paren / prose / brace / line-ref / Hangul /
   numeric / special chars)
4. **Heuristic** (C22 V2 suffix, C32 V3 receiver-style): match symbols
   the LLM names in non-graph form
5. **Metric redesign** (C34 B UserPromptBytes): switch what gets measured
   when the current proxy reads CLI-side cache instead of prompt size

Two more categories surfaced during the work but live outside the eval
boundary:

6. **ckg core bug** (C36-37 FTS): bug discovered via eval but fixed in
   the persist layer
7. **Cross-repo audit** (B-Phase 1 cks): determine whether an eval-side
   finding affects a consumer

## 7. Next horizon (post-cycle 9)

T-04/T-05 closed. The framework is production-grade for single-task
T01 measurement. The remaining work axes (priority queue):

| ID | Axis | Estimated | Direct user value |
|---|---|---|---|
| B-Phase 2 | cks go.mod ckg version bump | 5 min | cks receives ckg core fixes |
| A | This series of docs (CONTINUITY + trajectory + HANDOFF close) | 30-60 min | future-session continuity |
| C | Prompt engineering V2 (Go std-lib reaching) | 1 h | 5-10% halu rate reduction |
| D | smartContext budget audit (cost-benefit curve) | 1 h | H1 meaningful number |
| E | Task fixture expansion (T02-T30) | 1-2 h | coverage breadth |

Cross-project items (separate sessions):
- cks evaluation methodology transfer (ckv/ckg/cks integration session)
- HANDOFF.md T-12/T-13 (find_callers depth, impact_of_change regression tests)
- W-C lockdown series resumption (W7.5, W9 V19, W8 V28+)
- CKG-3 cross-snapshot policy (reactivated per CKS-INTEGRATION ckg-NEW-7)

## 8. Lessons (one-line each)

- *Measurement-framework convergence is iterative*. Bug-fix throughput per
  cycle drops (2→2→1→0+heuristic→1) as the noise floor approaches the
  real LLM signal.
- *Cli-backend prompt cache inflates `cached_tokens` 170K–587K* on the
  same prompt across runs. UserPromptBytes is the application-level
  proxy for prompt size.
- *Smoke-run stderr is the bug-discovery driver*. Iteration 1 of the FTS
  fix didn't actually run (gate trapped it). Iteration 2 caught the gate
  itself. Without the stderr trail the bug was undetectable.
- *Cross-repo audit before code change is cheap and often invalidates
  assumptions*. B-Phase 1 showed cks is unaffected by the FTS bug — the
  half-day of cks fix work that would have followed isn't needed.
