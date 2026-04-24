# Jira Tickets — CKG v0 Prototype

> **용도**: Jira 티켓 생성 시 본문으로 사용할 초안. CKG(엔진)와 CKG Viewer(시각화) 2개 티켓으로 분할.
> **근거 스펙**: `docs/spec-ckg-v0-prototype.md` / `docs/plan-ckg-v0-prototype.md`
> **작성일**: 2026-04-24

---

## 티켓 1 — `[CKG]` Go 코드 Knowledge Graph 엔진 v0 prototype 구축

### Summary

Go / TypeScript / Solidity 소스를 AST 기반으로 파싱하여 29 node × 22 edge 지식 그래프로 변환하는 단일 Go 바이너리 `ckg`를 구축한다. `build` / `mcp` / `export-static` / `eval` 4개 subcommand 제공 (viewer `serve`는 별도 티켓).

### Background

- **선행 실험** (참조: [Code-review: only code vs with skills vs with AST](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538))에서 동일 prompt에 대해 3가지 컨텍스트 조건을 비교한 결과, **AST 기반 컨텍스트(Doc③)가 프롬프트 부합도 · 파일/심볼/라인 정확도 · 할루시네이션 부재 · side-effect 없는 구현 가능성 등 모든 실무 지표에서 우세**했다.
- `only code` 조건에서는 LLM이 라인 드리프트(±10)는 감수할 수 있으나 가이드 초안 수준에 머무르고, `code + skills` 조건에서는 존재하지 않는 파일(`core/stablenet_genesis.go`) 및 도구명(`genesis-updater`)을 생성하는 할루시네이션이 2건 발생했다.
- 결론: **LLM이 코드를 "정확히" 이해하려면 파서가 코드를 선(先)분석하여 노드/엣지/시그니처/라인 범위가 확정된 그래프를 제공해야 한다.** CKG는 이 실험에서 검증된 "AST 환경"을 재사용 가능한 표준 인프라로 일반화하는 프로젝트다.
- 본 티켓은 그래프 생성 · 저장 · MCP 제공 · 정량 평가까지의 엔진 파트를 담당 (viewer UI는 `[CKG Viewer]` 티켓).

### Goal / Hypotheses

스펙 `docs/spec-ckg-v0-prototype.md` §2 기준 정량 검증:

| # | 가설 | 측정 |
|---|---|---|
| H1 | CKG MCP δ-mode(smart 1-shot)가 raw-file dump(α) 대비 LLM 입력 토큰 **≥50% 절감** | 4-baseline eval |
| H2 | δ-mode 정답률이 α 대비 **동등 이상** (regression 없음) | rubric / test pass |
| H3 | δ-mode가 graph-dump(β) · granular(γ) 양쪽을 dominate | token + accuracy |

### Scope (In)

- `ckg build` — detect → parse → link → graph build → cluster(pkg-tree + Leiden) → score → persist (`graph.db` SQLite + `manifest.json`)
- Go 파서: `golang.org/x/tools/go/packages` + `go/types` (interface dispatch resolution 포함)
- TS / JS 파서: `smacker/go-tree-sitter` + `tree-sitter-typescript`
- Solidity 파서: `tree-sitter-solidity` + ABI 추출
- Cross-language linking: Sol Contract ABI ↔ TS class (`binds_to` edge, INFERRED)
- `ckg mcp` — stdio MCP 서버, 6개 tool (`get_context_for_task` ★ / `find_symbol` / `find_callers` / `find_callees` / `get_subgraph` / `search_text`)
- `ckg export-static` — chunked JSON export (viewer 없는 정적 배포용)
- `ckg eval` — 4-baseline runner (α / β / γ / δ) + report.md 자동 생성
- `confidence ∈ {EXTRACTED, INFERRED, AMBIGUOUS}` 라벨링 (할루시네이션 방지)
- Pass 4 cross-ref validation: 모든 edge.src / dst가 nodes 집합에 존재함을 보증하여 *조용한 누락* 차단

### Scope (Out — V0 제외, V1+ 위임)

- LLM을 빌드 / 클러스터링 / 네이밍에 사용
- Vector DB / 임베딩 / hybrid retrieval
- 실시간 incremental update (V0는 full rebuild)
- Auth / 멀티테넌트 / HTTP MCP transport
- Go ↔ TS RPC, Go ↔ Sol cross-lang linking
- Rust / Python / Java / C++ 파서

### Deliverables (Phase 기준 — `docs/plan-ckg-v0-prototype.md`)

| Phase | 산출물 | Checkpoint |
|---|---|---|
| 1. Foundation (T1–T4) | go.mod, core types (29 node / 22 edge), SQLite persist, manifest | — |
| 2. Detect + Go parser (T5–T9) | Go AST → file-local nodes / edges + cross-file resolve | — |
| 3. Graph + cluster + score (T10–T14) | in-memory Graph + pkg-tree + Leiden topic overlay + PageRank / usage_score | — |
| 4. First vertical slice (T15–T17) | `ckg build` 동작 (Go corpus) | **CP-1** |
| 5. TS + Sol parsers + cross-lang (T18–T21) | 3개 언어 빌드 + `binds_to` 발행 | **CP-2** |
| 7. MCP server (T27–T29) | `ckg mcp` 6개 tool over stdio | **CP-4** |
| 8. Export-static (T30–T31) | portable chunked JSON | **CP-5** |
| 9. Eval framework (T32–T35) | 4-baseline 측정 + H1 / H2 / H3 report | **CP-6** |
| 10. Corpus + CI + docs (T36–T38) | synthetic + CI green (3 OS × 2 arch) | **CP-7** |

### Definition of Done (v0)

- [ ] `ckg build --src=testdata/synthetic --out=/tmp/ckg-synth` 무에러 종료
- [ ] `ckg build --src=$STABLENET_PATH --out=/tmp/ckg-real` 무에러 종료, **< 10분**
- [ ] `ckg mcp --graph=/tmp/ckg-real`이 Claude Code에서 spawn 후 6개 도구 정상 응답
- [ ] `ckg eval --baselines=alpha,beta,gamma,delta` 합성 + 실 corpus 완주
- [ ] Eval report에 H1 / H2 / H3 결과 명시
- [ ] CI 통과: 3 OS (linux / macos / windows) × 2 arch (amd64 / arm64)
- [ ] unit test **80%+ coverage**
- [ ] `docs/SCHEMA.md`, `docs/ARCHITECTURE.md`, `docs/EVAL.md` 작성
- [ ] README Quick Start 5분 이내

### Risks / Mitigations

| Risk | Mitigation |
|---|---|
| TS 타입 추론 없이 cross-file resolution 정확도 저하 | `AMBIGUOUS` confidence로 라벨링, eval에서 정확도 직접 측정 |
| Leiden 구현 품질 (Python `leidenalg` 대비) | unit test + synthetic corpus 정답 비교 (spec §5.5) |
| 대형 corpus(`go-stablenet`)에서 빌드 시간 폭발 | parse Pass 1 파일 단위 병렬화 (GOMAXPROCS), 측정 기반 튜닝 |
| SQLite FTS5 CGO 의존성 | `modernc.org/sqlite` 사용 (CGO-free), cross-compile 보장 |

### References

- Spec: `docs/spec-ckg-v0-prototype.md` (§1–§15)
- Plan: `docs/plan-ckg-v0-prototype.md` (10 phases × 38 tasks)
- Study guide: `docs/STUDY-GUIDE.md`
- 선행 실험: Confluence *Code-review: only code vs with skills vs with AST*

---

## 티켓 2 — `[CKG Viewer]` 3D 시각화 viewer + `ckg serve` 구축

### Summary

CKG가 생성한 `graph.db`를 브라우저에서 탐색할 수 있는 임베디드 3D 뷰어와 HTTP 서버(`ckg serve`)를 구축한다. LOD(Level of Detail) 기반 자동 확장 / 축소, 언어 · 타입 · 신뢰도별 시각 인코딩, FTS5 검색을 제공한다.

### Background

- 선행 실험([Code-review: only code vs with skills vs with AST](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538))에서 AST 컨텍스트가 LLM의 코드 이해에 결정적임을 확인했으나, **개발자 본인이 해당 그래프의 구조 · 규모 · 핵심 경로를 직관적으로 파악하는 수단은 부재**했다.
- graph가 담는 정보(29 node / 22 edge / confidence / usage_score / cluster)는 텍스트 dump로는 스케일에서 읽히지 않는다.
- Viewer의 목적은 두 가지:
  1. **CKG 자체의 QA** — LLM이 소비할 그래프가 실제로 그럴싸한지 개발자가 시각 검증
  2. **개발자의 선(先) 발견** — 코드베이스 토폴로지 / hotspot / cross-lang link / 할루시네이션 의심 영역(AMBIGUOUS)을 시각적으로 먼저 확인
- 본 티켓은 `[CKG]` 엔진 티켓의 `graph.db` schema가 확정된 이후 시작 (의존 관계).

### Goal

- `ckg serve --graph=<dir>`로 로컬 브라우저에서 graph 탐색
- L0 패키지 super-node(≈30개)부터 L4 함수 내부 logic(IfStmt / LoopStmt / CallSite / ReturnStmt / SwitchStmt)까지 줌 / 클릭으로 단계적 unfold
- 시각 인코딩(도형 8종 × 언어 색상 3종 × confidence alpha 3단계)으로 타입 / 언어 / 신뢰도 즉시 식별
- MCP가 LLM에게 컨텍스트를 주는 것과 대칭적으로, **viewer는 사람에게 같은 graph를 노출**

### Scope (In)

- **기술 스택**: Vanilla JS + `lit-html` + `3d-force-graph` (Three.js), esbuild 빌드 → Go `embed.FS` 주입 (Node.js 런타임 의존성 0)
- **LOD 인터랙션** (spec §7.2):
  - 줌 ≥1.5× → L1 서브패키지 lazy fetch
  - 줌 ≥3× → L2 파일
  - 줌 ≥6× → L3 type / func / var
  - 함수 클릭 "Inspect logic" → L4 (5종 logic block)
  - 더블클릭 focus mode / 우클릭 collapse
  - 상단 토글 `pkg_tree ⇄ topic_tree` (Leiden overlay)
- **Visual Encoding** (spec §7.3):
  - Shape: 8 primitive (sphere / cube / cone / cylinder / torus / pyramid / diamond / plane) + sub-kind glyph
  - Color: Go = #00ADD8 / TS = #3178C6 / Sol = #3C3C3D / cross-lang = #FFD700
  - Size: `node_size = base × log10(usage_score + 1)`, clip 0.5× ~ 3.5×
  - Alpha: confidence (EXTRACTED = 1.0 / INFERRED = 0.7 / AMBIGUOUS = 0.4)
- **UI**: top-bar search (SQLite FTS5) / 우측 Selection Panel (type / name / qname / signature / usage / confidence / source preview / in-out edges) / 하단 pkg-tree ⇄ topic toggle + LOD + 언어 filter
- **`ckg serve`** subcommand: HTTP `/api/*` endpoint, `graph.db` read-only open, `--open` 옵션으로 브라우저 자동 launch
- **성능 가드**: 가시 노드 > 5K → sphere impostor 강등, edge cap 10K, layout step 200회 후 freeze
- **Playwright smoke test**: L0 로드 / 노드 클릭 / search / LOD 단계 전환 4종

### Scope (Out — V1+)

- Persistent viewer state (사용자 view 저장 / 복원)
- 두 graph 비교 diff (commit A vs B)
- Custom edge weight / scoring formula tweaking
- HTTP auth (단일 사용자, 로컬 가정)

### Deliverables (Phase 6 — `docs/plan-ckg-v0-prototype.md` T22–T26)

| Task | 산출물 | Checkpoint |
|---|---|---|
| T22 | Viewer scaffold (esbuild config, entry, api client) | — |
| T23 | 3D rendering + LOD + visual encoding (29 types 전원) | — |
| T24 | Search(FTS5) + selection panel + filters | — |
| T25 | HTTP server + `embed.FS` | — |
| T26 | `ckg serve` subcommand + Playwright smoke | **CP-3** |

### Definition of Done (v0)

- [ ] `ckg serve --graph=/tmp/ckg-real`로 viewer 정상 로드
- [ ] L0 → L4 fold / unfold 전 단계 동작
- [ ] Playwright smoke 4종 테스트 green (3 OS)
- [ ] 가시 노드 5K+ 시 60fps 유지 (impostor 강등 확인)
- [ ] 29 node type 전원 고유 도형 / glyph로 렌더링
- [ ] Confidence AMBIGUOUS 노드가 시각적으로 구분됨 (alpha 0.4)
- [ ] Search 결과 클릭 → 카메라 이동 + 조상 auto-expand 동작
- [ ] `embed.FS` 통해 단일 바이너리로 viewer 자산 배포 (별도 static file 경로 불필요)

### Dependencies

- **Upstream**: `[CKG]` 티켓 Phase 4 완료 (CP-1, `graph.db` schema 확정) — viewer 개발을 unblock
- **Parallel OK**: `[CKG]` Phase 5(TS / Sol parsers) / Phase 7–9와 병행 가능 (schema stable 전제)

### Risks / Mitigations

| Risk | Mitigation |
|---|---|
| `3d-force-graph` 대형 graph에서 layout 수렴 느림 | step cap 200 + drag 시에만 unfreeze (spec §7.5) |
| esbuild 번들을 `embed.FS`에 포함 시 바이너리 크기 폭발 | lit-html 선택(React 회피, ~150KB 절감), viewer dist gzip ≤ 500KB 목표 |
| Cross-OS 브라우저 smoke 불안정 | Playwright OS matrix CI (Linux / macOS / Windows) |

### References

- Spec: `docs/spec-ckg-v0-prototype.md` §7 (Viewer), §11 (Error handling — Viewer)
- Plan: `docs/plan-ckg-v0-prototype.md` Phase 6 (T22–T26)
- 의존 티켓: `[CKG]` (graph.db schema provider)
