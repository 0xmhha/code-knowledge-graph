# Jira Tickets — StableNet AI Agent 전체 로드맵

> **용도**: `stablenet-ai-agent/claudedocs/` HLD(00~10) 기반으로 전체 시스템을 9 단계(Epic)로 쪼개어 Jira 티켓 작성용 본문을 제공.
> **근거 HLD**: `/Users/wm-it-22-00661/Work/github/stable-net/study/projects/stablenet-ai-agent/claudedocs/`
> **작성일**: 2026-04-24

---

## 0. 읽는 법 / 운영 규칙

### 0.1 단계 구조

본 문서는 **9개 Epic**으로 구성된다. 각 Epic은:
- `Epic` = 대단계(step 1 ~ step 9)
- `Story` = Epic을 쪼갠 HLD Phase / Subsystem 단위
- `Task` = Story를 실제 PR 단위로 쪼갠 것 (플래너 단계에서 추가 분해)

### 0.2 전체 단계 대 HLD Phase 매핑

| 본 문서 Step | Implementation Roadmap Phase | 대응 서브시스템 |
|---|---|---|
| **Step 1** CKG 구현 (현재 진행) | Phase 1 CKS Layer 1 일부 (Graph + BM25 + AST + File) | CKS Layer 1 Storage Backends |
| **Step 2** CKG 검증 테스트 | Phase 1 Acceptance (Eval 데이터셋: 30개 known-answer 쿼리 baseline) | Known-answer 30 + Citation accuracy 측정 |
| **Step 3** CKV (code-knowledge-vector) 구현 | Phase 1 CKS Layer 1 Storage Backends (Vector) | Vector backend |
| **Step 4** CKG+CKV 하이브리드 통합 | Phase 1 CKS Layer 3: Retrieval Orchestrator (Pager) | Retrieval Orchestrator |
| **Step 5** 하이브리드 재검증 | Phase 1 Acceptance 재측정 | Eval batch runner (로컬 확장) |
| **Step 6** chainbench 연동 | Phase 2 CL2 Build/Test Runner (+ Phase 5 Eval 연동) | CL2 chainbench adapter + CS4 Eval batch |
| **Step 7** 자동 PR + Slack 알림 | Phase 2 CL3 + CL5 | CL3 VCS Operator + CL5 External Adapter (Slack) |
| **Step 8** Jira → PR 자동화 (WF1) | Phase 3 + Phase 4 | CS1 / CS2 / CS3 + WF1 YAML |
| **Step 9** 나머지 HLD 작업 | Phase 2 CL4 + Phase 5 + Phase 6+ (CKS 고도화는 로컬 확장) | CL4 Vault, Observability(CS4), WF2~WF6 |

### 0.3 공통 배경 (모든 Epic에 재사용)

- **목적**: Jira 티켓 1개 → 자동 분석 → 자동 설계 → 자동 구현 → PR → 리뷰 → 머지까지 이어지는 **개발 라이프사이클 자동화 플랫폼** 구축.
- **핵심 가설**: **토큰 효율성의 99%가 CKS Retrieval Orchestrator(Pager) 계층에서 결정되며**, 다른 모든 서브시스템 품질은 CKS 정확도에 의존한다 ([CKS §1](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS)). 따라서 **CKG → CKV → 하이브리드**를 먼저 쌓고 점진 확장한다.
- **선행 실험**: [Code-review: only code vs with skills vs with AST](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538) 에서 AST 기반 컨텍스트가 모든 실무 지표에서 우세함을 확인. 본 로드맵은 그 결과를 일반화하는 표준 인프라 구축 과정이다.
- **상위 참조**: `claudedocs/SUMMARY-HLD.md`, [Implementation Roadmap](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap).

---

## Step 1 — `[CKG]` Code Knowledge Graph 엔진 v0 prototype

### Summary
Go / TypeScript / Solidity 소스를 AST 기반으로 파싱하여 29 node × 22 edge 지식 그래프로 변환하는 단일 Go 바이너리 `ckg` 구축. CKS Layer 1의 Graph + BM25 + AST + File Store 부분을 담당. **4개 sub-ticket**으로 구성:
- **1.1 CKG Engine** — 그래프 빌드 파이프라인 (`ckg build` / `ckg export-static`)
- **1.2 CKG MCP** — MCP 서버로 graph.db를 LLM 클라이언트에 노출 (`ckg mcp`)
- **1.3 CKG Evaluation** — 4-baseline 평가 프레임워크 (`ckg eval`)
- **1.4 CKG Viewer** — 3D 시각화 (`ckg serve`)

### Background
- HLD [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 1 (CKS Standalone, MVP)의 필수 선결 조건.
- [Code-review 실험](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538) 결과 `code + AST` 조건이 할루시네이션 0건, 라인 정확도 ±1 이내로 모든 지표에서 우세.
- CKS(Code Knowledge System) 전체 중 **Vector / Working Memory / 4-layer Pager** 제외한 "정적 구조 인지" 부분이 본 Step의 범위.

### Goal / Hypotheses (Step 1 전체)

본 Step의 성공은 아래 3가지 가설의 정량 검증으로 정의 (측정 주체는 **1.3 Evaluation**):

| # | 가설 | 측정 |
|---|---|---|
| H1 | CKG MCP δ-mode(smart 1-shot)가 raw-file dump(α) 대비 LLM 입력 토큰 **≥50% 절감** | 4-baseline eval |
| H2 | δ-mode 정답률이 α 대비 **동등 이상** (regression 없음) | rubric / test pass |
| H3 | δ-mode가 graph-dump(β) · granular(γ) 양쪽을 dominate | token + accuracy |

### Sub-tickets

#### 1.1 `[CKG Engine]` 그래프 빌드 파이프라인

##### Summary
Go / TypeScript / Solidity 소스를 AST 기반으로 파싱하여 29 node × 22 edge 지식 그래프를 생성·클러스터링·스코어링·영속화한다. `ckg build` + `ckg export-static` 2개 subcommand.

##### Background
- **선행 실험**(참조: [Code-review: only code vs with skills vs with AST](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538))에서 AST 기반 컨텍스트가 프롬프트 부합도·파일/심볼/라인 정확도·할루시네이션 부재 등 모든 실무 지표에서 우세.
- `only code` 조건에서는 라인 드리프트(±10), `code + skills` 조건에서는 존재하지 않는 파일(`core/stablenet_genesis.go`) 및 도구명(`genesis-updater`) 할루시네이션 2건 발생.
- 결론: **파서가 코드를 선(先)분석하여 노드/엣지/시그니처/라인 범위가 확정된 그래프를 제공**해야 LLM이 코드를 "정확히" 이해.
- 본 sub-ticket은 그래프 생성·저장 파트. MCP 노출은 1.2, 평가는 1.3, viewer는 1.4.

##### Scope (In)
- `ckg build` — detect → parse → link → graph build → cluster(pkg-tree + Leiden) → score → persist (`graph.db` SQLite + `manifest.json`)
- Go 파서: `golang.org/x/tools/go/packages` + `go/types` (interface dispatch resolution 포함)
- TS / JS 파서: `smacker/go-tree-sitter` + `tree-sitter-typescript`
- Solidity 파서: `tree-sitter-solidity` + ABI 추출
- Cross-language linking: Sol Contract ABI ↔ TS class (`binds_to` edge, INFERRED)
- `ckg export-static` — chunked JSON export (viewer 없는 정적 배포용)
- Scoring: degree / PageRank / usage_score
- `confidence ∈ {EXTRACTED, INFERRED, AMBIGUOUS}` 라벨링 (할루시네이션 방지)
- Pass 4 cross-ref validation: 모든 edge.src / dst가 nodes 집합에 존재함을 보증하여 *조용한 누락* 차단

##### Scope (Out — V0 제외, V1+ 위임)
- LLM을 빌드 / 클러스터링 / 네이밍에 사용
- Vector DB / 임베딩 / hybrid retrieval (Step 3~4)
- 실시간 incremental update (V0는 full rebuild)
- Go ↔ TS RPC, Go ↔ Sol cross-lang linking
- Rust / Python / Java / C++ 파서

##### Graph Schema 요약 (아키텍처 정체성)

**Node Types (29개)** — 카테고리별:

| 카테고리 | 타입 |
|---|---|
| Structural (2) | `Package`, `File` |
| Type Definitions (7) | `Struct` (go/sol), `Interface` (go/ts), `Class` (ts), `TypeAlias` (go/ts), `Enum` (ts/sol), `Contract` (sol), `Mapping` (sol) |
| Callable (4) | `Function`, `Method`, `Modifier` (sol), `Constructor` (ts/sol) |
| Value / State (5) | `Constant`, `Variable`, `Field`, `Parameter`, `LocalVariable` |
| Module I/O (3) | `Import`, `Export` (ts), `Decorator` (ts) |
| Event (1) | `Event` (sol) |
| Concurrency (2) | `Goroutine` (go), `Channel` (go) |
| Control-flow Logic (5) | `IfStmt`, `LoopStmt` (`for`/`while`/`range`), `CallSite`, `ReturnStmt`, `SwitchStmt` |

**Edge Types (22개)** — 카테고리별:

| 카테고리 | 엣지 |
|---|---|
| Containment / Definition (2) | `contains`, `defines` |
| Call (2) | `calls` (static, EXTRACTED), `invokes` (dynamic dispatch, INFERRED) |
| Type & Value Use (5) | `uses_type`, `instantiates`, `references`, `reads_field`, `writes_field` |
| Module Graph (2) | `imports`, `exports` (ts) |
| Inheritance (2) | `implements` (Go 구조적, INFERRED), `extends` |
| Solidity 특화 (4) | `has_modifier`, `emits_event`, `reads_mapping`, `writes_mapping` |
| TypeScript 특화 (1) | `has_decorator` |
| Go 동시성 (3) | `spawns`, `sends_to`, `recvs_from` |
| Cross-lang (1) ★ | `binds_to` (Sol Contract ABI ↔ TS class, INFERRED) |

Confidence 라벨: `EXTRACTED` / `INFERRED` / `AMBIGUOUS` (할루시네이션 방지).

##### Deliverables

| Phase | 산출물 | Checkpoint |
|---|---|---|
| 1. Foundation | go.mod, core types (29 node / 22 edge), SQLite persist, manifest | — |
| 2. Detect + Go parser | Go AST → file-local nodes / edges + cross-file resolve | — |
| 3. Graph + cluster + score | in-memory Graph + pkg-tree + Leiden topic overlay + PageRank / usage_score | — |
| 4. First vertical slice | `ckg build` 동작 (Go corpus) | **CP-1** |
| 5. TS + Sol parsers + cross-lang | 3개 언어 빌드 + `binds_to` 발행 | **CP-2** |
| 8. Export-static | portable chunked JSON | **CP-5** |

##### Definition of Done (1.1)
- [ ] `ckg build --src=testdata/synthetic --out=/tmp/ckg-synth` 무에러 종료
- [ ] `ckg build --src=$STABLENET_PATH --out=/tmp/ckg-real` 무에러 종료, **< 10분**
- [ ] `ckg export-static`으로 chunked JSON 생성 (viewer 없이도 외부 도구 연동 가능)
- [ ] graph.db schema 확정 (1.2 / 1.3 / 1.4가 공용 소비)
- [ ] 29 node type / 22 edge type 모두 emit 확인 (합성 corpus에서)
- [ ] cross-ref validator가 모든 edge 참조 무결성 보증
- [ ] unit test **80%+ coverage** (engine 범위)
- [ ] `docs/SCHEMA.md`, `docs/ARCHITECTURE.md` 작성

##### Dependencies
- **Unblock**: 1.2 MCP (schema 필요), 1.3 Eval (graph.db 소비), 1.4 Viewer (schema 필요)

##### Risks / Mitigations

| Risk | Mitigation |
|---|---|
| TS 타입 추론 없이 cross-file resolution 정확도 저하 | `AMBIGUOUS` confidence로 라벨링, 1.3 eval에서 정확도 직접 측정 |
| Leiden 구현 품질 (Python `leidenalg` 대비) | unit test + synthetic corpus 정답 비교 |
| 대형 corpus(`go-stablenet`)에서 빌드 시간 폭발 | parse Pass 1 파일 단위 병렬화 (GOMAXPROCS), 측정 기반 튜닝 |
| SQLite FTS5 CGO 의존성 | `modernc.org/sqlite` 사용 (CGO-free), cross-compile 보장 |

#### 1.2 `[CKG MCP]` MCP 서버로 graph.db를 LLM 클라이언트에 노출

##### Summary
1.1이 생성한 `graph.db`를 **MCP stdio 서버**(`ckg mcp`)로 노출하여 Claude Code / Cursor 등 MCP 호환 클라이언트가 6개 tool을 자연어 대화 중 자동 호출할 수 있게 한다.

##### Background
- **MCP (Model Context Protocol)**: Anthropic이 발표한 LLM 클라이언트 ↔ 외부 도구 표준 프로토콜. 비유: *LLM을 위한 USB-C*.
- Step 1 가설(H1/H2/H3)은 "graph 컨텍스트가 raw-file 대비 LLM 토큰 효율+정확도 ↑". 측정하려면 동일 task를 4 baseline에 자동으로 흘려보내야 하고, 이때 **MCP가 graph → LLM으로 컨텍스트를 자동 주입하는 채널** 역할.
- 사용자는 한 번만 등록(`claude mcp add ckg --command "ckg" --args "mcp,--graph=..."`)하면 이후 Claude가 자동으로 tool 호출.

##### Scope (In)
- `ckg mcp` stdio 서버 (JSON-RPC over stdin/stdout)
- graph.db는 read-only open (MCP 서버는 쓰기 없음)
- 6개 tool 노출 (아래 표)
- System prompt + tool allowlist 메커니즘 — baseline별로 사용 가능 도구 제한 (1.3 Evaluation 연동)
- 모든 응답에 citation(`file:line`) 강제 부착

##### Scope (Out — V1+)
- HTTP + JWT transport
- Multi-tenant / auth
- Tool-level rate limiting / quota

##### MCP Tool Set & Baseline 매핑

| # | Tool | 역할 | 허용 Baseline |
|---|---|---|---|
| 1 | `get_context_for_task` ★ | δ-mode 핵심: 단일 호출로 Evidence Pack 반환 | δ |
| 2 | `find_symbol` | exact symbol lookup | δ, γ |
| 3 | `find_callers` | 역방향 call graph | γ |
| 4 | `find_callees` | 순방향 call graph | γ |
| 5 | `get_subgraph` | 명시적 subgraph fetch (β는 `depth=99`로 whole dump) | γ, β |
| 6 | `search_text` | FTS5 raw search | γ |

**Eval-driven 제약** (1.3에서 활용): 4 baseline 은 동일 SQLite를 read하되, system prompt + tool allowlist로 **사용 가능한 도구 수**를 다르게 제한해 가설을 측정.
- α: 도구 0개 (raw file dump)
- β: `get_subgraph` 1회 (whole-graph dump)
- γ: `find_*` + `search_text` (granular)
- δ: `get_context_for_task` 1개 (smart 1-shot)

**Transport**: V0 stdio only (Claude Code / Cursor가 spawn).

##### Deliverables

| Phase | 산출물 | Checkpoint |
|---|---|---|
| 7. MCP server | `ckg mcp` 6개 tool over stdio, JSON-RPC 준수 | **CP-4** |

##### Definition of Done (1.2)
- [ ] `ckg mcp --graph=/tmp/ckg-real`이 Claude Code에서 spawn 후 6개 도구 정상 응답
- [ ] 각 tool의 JSON schema가 MCP spec 준수 (input / output 정의 명확)
- [ ] `get_context_for_task`가 token budget 파라미터에 맞춰 응답 크기 조절
- [ ] 모든 응답에 citation(`file:line`) 포함
- [ ] graph.db는 read-only로 open (SQLite immutable flag)
- [ ] Tool allowlist 메커니즘 동작 (1.3 eval에서 baseline별 제한 적용 가능)
- [ ] unit test **80%+ coverage** (MCP 어댑터 범위)

##### Dependencies
- **Upstream**: 1.1 Phase 4 완료 (CP-1, `graph.db` schema 확정)
- **Unblock**: 1.3 Evaluation (MCP tool allowlist 소비), Step 4 Hybrid Retrieval (`get_context_for_task` API 확장)

##### Risks / Mitigations

| Risk | Mitigation |
|---|---|
| MCP protocol 진화로 client 호환 깨짐 | 공식 SDK / spec 버전 고정, release note 모니터 |
| Claude Code 외 client(Cursor 등)에서 tool 불일치 | 공통 JSON-RPC + stdio만 사용, client-specific extension 금지 |
| Tool 응답 크기 예측 실패 → client timeout | `get_context_for_task`에 token budget 파라미터 강제, tail trim 동작 |

#### 1.3 `[CKG Evaluation]` 4-baseline 평가 프레임워크

##### Summary
CKG가 LLM의 코드 이해·구현 정확도를 실제로 개선하는지 검증하는 자동 eval runner(`ckg eval`) 구축. 4 baseline(α / β / γ / δ)에 동일 task를 흘려보내고 **H1 / H2 / H3를 정량 판정**.

##### Background
- **가설 검증이 빠지면 투자 근거가 없다**: Step 2 이후(Benchmark, CKV, Hybrid 등)의 모든 투자는 "CKG가 실제로 효과 있다"는 정량 근거에서 출발. 본 sub-ticket이 그 근거의 발원지.
- 선행 Confluence 실험은 단일 prompt · 정성 비교 수준 → 본 sub-ticket에서 재현 가능한 **자동 회귀 평가**로 승격.
- 4-baseline 비교는 1.2 MCP tool allowlist로 실현 — 동일 graph에 대해 **도구 접근만 다르게 제한**.

##### Scope (In)
- `ckg eval --baselines=alpha,beta,gamma,delta` subcommand
- Baseline 정의:
  - α: raw file dump (MCP tool 0개, system prompt로 파일 원문만 제공)
  - β: whole graph dump (`get_subgraph(depth=99)` 1회만 허용)
  - γ: granular MCP (`find_*` + `search_text`)
  - δ: smart 1-shot (`get_context_for_task` 1개만)
- Eval task 세트: synthetic corpus (정답 라벨링) + real corpus (go-stablenet 등)
- Metric 산출: **Citation accuracy / Answer accuracy / Hallucination count / Token budget**
- Report.md 자동 생성 (H1 / H2 / H3 판정 + baseline별 ranking 표)
- `docs/EVAL.md` 방법론 문서 (재현 가능한 설정)

##### Scope (Out)
- CKV / Hybrid baseline (ε) — Step 2 / Step 5에서 추가
- 외부 LLM API 비용 최적화 (eval은 비용보다 정확도 우선)

##### Deliverables

| Phase | 산출물 | Checkpoint |
|---|---|---|
| 9. Eval framework | 4-baseline 측정 runner + H1 / H2 / H3 report | **CP-6** |
| 10. Synthetic corpus | 라벨링된 known-answer 쿼리 세트 (30개) | (Step 1 CI green에 포함) |

##### Definition of Done (1.3)
- [ ] `ckg eval --baselines=alpha,beta,gamma,delta` 합성 + 실 corpus 완주
- [ ] Eval report에 H1 / H2 / H3 결과 명시 (token 절감율, 정답률, dominance 여부)
- [ ] Citation accuracy / Answer accuracy / Hallucination count / Token budget 4 metric 자동 산출
- [ ] Baseline별 ranking 표 자동 생성
- [ ] `docs/EVAL.md` 방법론 문서 작성
- [ ] Dry-run 캐싱으로 동일 prompt 재실행 회피
- [ ] unit test **80%+ coverage** (eval runner + metric 산출)

##### Dependencies
- **Upstream**: 1.1 (graph.db), 1.2 (MCP tool allowlist)
- **Unblock**: Step 2 Benchmark (이 runner를 확장하여 회귀 CI 구축)

##### Risks / Mitigations

| Risk | Mitigation |
|---|---|
| Synthetic corpus 정답 라벨링 노동 비용 | v0은 30개만, 실 corpus 확장은 Step 2에서 |
| 외부 LLM API 비용 폭발 | dry-run 캐싱, 동일 prompt 재실행 방지 |
| Baseline 간 token 계산 불공정 (tokenizer 차이) | 동일 tokenizer로 통일 측정, metric에 tokenizer 명시 |

#### 1.4 `[CKG Viewer]` 3D 시각화 viewer + `ckg serve` 구축

##### Summary
CKG가 생성한 `graph.db`를 브라우저에서 탐색할 수 있는 임베디드 3D 뷰어와 HTTP 서버(`ckg serve`)를 구축한다. LOD(Level of Detail) 기반 자동 확장 / 축소, 언어 · 타입 · 신뢰도별 시각 인코딩, FTS5 검색을 제공한다.

##### Background
- 선행 실험([Code-review: only code vs with skills vs with AST](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538))에서 AST 컨텍스트가 LLM의 코드 이해에 결정적임을 확인했으나, **개발자 본인이 해당 그래프의 구조 · 규모 · 핵심 경로를 직관적으로 파악하는 수단은 부재**했다.
- graph가 담는 정보(29 node / 22 edge / confidence / usage_score / cluster)는 텍스트 dump로는 스케일에서 읽히지 않는다.
- Viewer의 목적은 두 가지:
  1. **CKG 자체의 QA** — LLM이 소비할 그래프가 실제로 그럴싸한지 개발자가 시각 검증
  2. **개발자의 선(先) 발견** — 코드베이스 토폴로지 / hotspot / cross-lang link / 할루시네이션 의심 영역(AMBIGUOUS)을 시각적으로 먼저 확인
- 본 sub-ticket은 1.1 Engine의 `graph.db` schema가 확정된 이후 시작 (의존 관계).

##### Goal
- `ckg serve --graph=<dir>`로 로컬 브라우저에서 graph 탐색
- L0 패키지 super-node(≈30개)부터 L4 함수 내부 logic(IfStmt / LoopStmt / CallSite / ReturnStmt / SwitchStmt)까지 줌 / 클릭으로 단계적 unfold
- 시각 인코딩(도형 8종 × 언어 색상 3종 × confidence alpha 3단계)으로 타입 / 언어 / 신뢰도 즉시 식별
- MCP가 LLM에게 컨텍스트를 주는 것과 대칭적으로, **viewer는 사람에게 같은 graph를 노출**

##### Scope (In)
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

##### Scope (Out — V1+)
- Persistent viewer state (사용자 view 저장 / 복원)
- 두 graph 비교 diff (commit A vs B)
- Custom edge weight / scoring formula tweaking
- HTTP auth (단일 사용자, 로컬 가정)

##### Deliverables (Phase 6, T22–T26)

| Task | 산출물 | Checkpoint |
|---|---|---|
| T22 | Viewer scaffold (esbuild config, entry, api client) | — |
| T23 | 3D rendering + LOD + visual encoding (29 types 전원) | — |
| T24 | Search(FTS5) + selection panel + filters | — |
| T25 | HTTP server + `embed.FS` | — |
| T26 | `ckg serve` subcommand + Playwright smoke | **CP-3** |

##### Definition of Done (1.2)
- [ ] `ckg serve --graph=/tmp/ckg-real`로 viewer 정상 로드
- [ ] L0 → L4 fold / unfold 전 단계 동작
- [ ] Playwright smoke 4종 테스트 green (3 OS)
- [ ] 가시 노드 5K+ 시 60fps 유지 (impostor 강등 확인)
- [ ] 29 node type 전원 고유 도형 / glyph로 렌더링
- [ ] Confidence AMBIGUOUS 노드가 시각적으로 구분됨 (alpha 0.4)
- [ ] Search 결과 클릭 → 카메라 이동 + 조상 auto-expand 동작
- [ ] `embed.FS` 통해 단일 바이너리로 viewer 자산 배포 (별도 static file 경로 불필요)

##### Dependencies
- **Upstream**: 1.1 Phase 4 완료 (CP-1, `graph.db` schema 확정) — viewer 개발을 unblock
- **Parallel OK**: 1.1 잔여 Phase(5 / 8), 1.2 MCP(Phase 7), 1.3 Eval(Phase 9)과 병행 가능 (schema stable 전제)

##### Risks / Mitigations

| Risk | Mitigation |
|---|---|
| `3d-force-graph` 대형 graph에서 layout 수렴 느림 | step cap 200 + drag 시에만 unfreeze (spec §7.5) |
| esbuild 번들을 `embed.FS`에 포함 시 바이너리 크기 폭발 | lit-html 선택(React 회피, ~150KB 절감), viewer dist gzip ≤ 500KB 목표 |
| Cross-OS 브라우저 smoke 불안정 | Playwright OS matrix CI (Linux / macOS / Windows) |

### Definition of Done (Step 1 전체)
- [ ] 1.1 Engine DoD 전 항목 통과 (`ckg build` / `ckg export-static` + graph schema 완성 + 80%+ coverage)
- [ ] 1.2 MCP DoD 전 항목 통과 (`ckg mcp` 6 tool + tool allowlist 동작 + 80%+ coverage)
- [ ] 1.3 Evaluation DoD 전 항목 통과 (`ckg eval` 4 baseline + H1/H2/H3 리포트 + 80%+ coverage)
- [ ] 1.4 Viewer DoD 전 항목 통과 (`ckg serve` + Playwright smoke + 29 node type 렌더)
- [ ] CI 통과: 3 OS (linux / macos / windows) × 2 arch (amd64 / arm64)
- [ ] 단일 바이너리에 엔진 + viewer 자산 모두 포함 (`embed.FS`)
- [ ] README Quick Start 5분 이내

### References
- `docs/STUDY-GUIDE.md` (MCP 학습 가이드)
- [CKS §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS) (Layer 1 Storage Backends — Graph / Vector / BM25 / AST / File)
- 선행 실험: [Confluence — Code-review: only code vs with skills vs with AST](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2642837538)

---

## Step 2 — `[CKG Benchmark]` CKG 기반 LLM 코드 이해 정량 검증

### Summary
Step 1에서 구축한 CKG가 LLM의 코드 이해 · 구현 작업 정확도를 실제로 개선하는지 정량 검증. `benchmark_v1.json` 스키마와 30 known-answer query 세트 작성, 4-baseline 비교 리포트 자동 생성.

### Background
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 1 Acceptance 항목의 "Eval 데이터셋: 30개 known-answer 쿼리 baseline 수립"에 대응.
- **CKG가 LLM 답변 정확도를 개선한다는 주장이 실제로 측정 가능해야** 이후 CKV(Step 3) 투자 근거가 생긴다.
- 선행 Confluence 실험은 단일 prompt · 정성 비교 수준 → 본 Step에서 재현 가능한 **자동 회귀 평가**로 승격.

### Scope
- `benchmark/benchmark_v1.json` — 30 known-answer query (go-stablenet에서 수작업 정답 라벨링)
- 4 baseline runner 완성: alpha(raw) / beta(graph-dump) / gamma(granular MCP) / delta(smart 1-shot)
- 4 metric 산출: **Citation accuracy / Answer accuracy / Hallucination count / Token budget**
- 자동 report.md (H1/H2/H3 판정)
- CI hook: 회귀 발생 시 PR block

### Definition of Done
- [ ] benchmark_v1.json 30 query 라벨링 완료 (Go 경로)
- [ ] Citation accuracy = **100%**
- [ ] Answer accuracy ≥ **80% baseline**
- [ ] Hallucination = **0**
- [ ] Token budget = 100% (delta가 alpha 대비 50%+ 절감)
- [ ] CI가 PR마다 회귀 감지 시 자동 block
- [ ] 리포트에 baseline별 ranking 표 자동 생성

### Dependencies
- **Upstream**: Step 1 Phase 9 (`ckg benchmark` runner), Phase 10 (synthetic corpus)
- **Unblock**: Step 3 투자 의사결정 (ROI 근거)

### References
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) (Phase 1 Acceptance: Citation accuracy / Eval 데이터셋 30 baseline)
- [CKS §11](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS) (KPI / 측정 지표 — Citation accuracy, Token budget 등)
- Confluence: *Code-review only code vs with skills vs with AST*

---

## Step 3 — `[CKV]` Code-Knowledge-Vector 백엔드 구현

### Summary
의미 기반 코드 검색을 위한 Vector DB 백엔드를 CKS Layer 1에 추가. 임베딩 모델 선정, 인덱싱 파이프라인, 검색 API 제공. CKG(구조)와 상보적인 "의미 유사도" 질의를 담당.

### Background
- [CKS §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS): 단일 DB는 코드 질의 패턴 전체를 커버 못함. 4개 backend (Graph / Vector / BM25 / AST) 조합이 필요.
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 1 "CKS Layer 1: Storage Backends (Graph + Vector + BM25 + AST + File)"의 Vector backend에 해당.
- CKG(구조 정확)로 해결 안 되는 질의(예: "이것과 비슷한 로직은 어디 있나?", "자연어 설명으로 함수 찾기")를 커버.

### Scope
- **임베딩 모델 결정** (code-specific vs general, 로컬 vs 외부 API) — `claudedocs/00-overview.md §8` 의도적 미결정 항목
- Vector DB 선택 (Qdrant / LanceDB / pgvector 중 PoC 후 결정)
- 인덱싱 단위 결정: symbol / function / chunk(슬라이딩 윈도우) 중 택 1 + 이유
- CKG가 소유한 `graph.db`의 node 메타데이터와 연동되는 외래키(node_id ↔ vector_id)
- 검색 API: `semantic_search(query, top_k, lang?)` → `[{node_id, score, snippet}]`
- 증분 인덱싱 (파일 변경 감지 + 변경된 symbol만 재임베딩)
- Freshness checker 연동 (git diff 기반)

### Definition of Done
- [ ] 임베딩 모델 / Vector DB 선정 ADR 작성
- [ ] go-stablenet 전체 인덱싱 시간 측정 + 리포트
- [ ] `semantic_search("race condition in consensus")` 같은 자연어 질의가 relevant node 반환
- [ ] 증분 인덱싱: 단일 파일 변경 시 ≤ 10초 재인덱싱
- [ ] Vector backend 단위 테스트 80%+ coverage
- [ ] CKG `graph.db` schema와 vector store의 node_id 1:1 매핑 보장 (cross-ref validator)

### Dependencies
- **Upstream**: Step 1 (CKG schema 확정, node_id 발행 규칙)
- **Unblock**: Step 4 (하이브리드)

### References
- [CKS §4.2](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS) (Vector DB — 의미적 질의)
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 1

---

## Step 4 — `[Hybrid Retrieval]` CKG + CKV 하이브리드 리트리버

### Summary
CKG(구조)와 CKV(의미) 결과를 **RRF(Reciprocal Rank Fusion)** 로 융합하여 단일 Evidence Pack을 반환하는 Retrieval Orchestrator 구현. CKS Layer 3의 Pager 역할을 담당하여 LLM 토큰 예산을 자동 관리.

### Background
- [CKS §6](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS) Layer 3 Retrieval Orchestrator(Pager)가 "토큰 효율성의 99%를 결정".
- Graph 단독 = 구조 정확하나 의미 누락 / Vector 단독 = 의미 가까우나 콜 체인 모름. **두 결과를 fusion해야 둘 다 커버**.
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 1 "CKS Layer 3: Retrieval Orchestrator (Pager)" 완결 (RRF fusion은 로컬 구현 선택지).

### Scope
- **Intent Classifier**: 질의 유형 분류 (bug_fix / feature_add / architecture_explain / concurrency_safety / cross_lang_lookup)
- **Query Planner**: intent → backend 조합 결정 (예: bug_fix → Graph(call chain) + Vector(유사 증상) + BM25(정확 심볼))
- **Fan-out / Fusion**: 각 backend 호출 병렬 → RRF(k=60) → top-K
- **Token Budget Manager**: max_tokens 입력 받아 top-K 조절 + density 조정
- **Citation Generator**: 모든 응답에 `file:line` 강제 부착
- **Working Memory Lookup**: session 내 중복 질의 회피 (SQLite Layer 2)
- **Writeback**: LLM이 session 중 발견한 사실을 working memory에 저장 → 다음 질의 재활용
- Retrieval Playbook v1: `bug_fix` / `feature_add` / `architecture_explain`

### Definition of Done
- [ ] `get_context_for_task(task_description, budget_tokens)` 가 단일 호출로 delta-mode Evidence Pack 반환
- [ ] 5개 intent에 대해 Playbook이 올바른 backend 조합 선택
- [ ] RRF fusion이 Graph-only / Vector-only 대비 top-10 recall 개선 측정
- [ ] Token budget 8K / 16K / 32K 각각 over-budget 없이 tail trim 동작
- [ ] Working Memory가 동일 session 재질의 시 cache hit
- [ ] 모든 응답에 citation(`file:line`) 포함

### Dependencies
- **Upstream**: Step 1 (CKG MCP), Step 3 (CKV)
- **Unblock**: Step 5(재검증), Step 8(CS2 Task Planner가 이 API 소비)

### References
- [CKS §6](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS) (Layer 3 Retrieval Orchestrator / Pager — Intent Classifier, RRF fusion, Token Budget Manager)
- Retrieval Playbook (`bug_fix` / `feature_add` / `architecture_explain`) — 로컬 확장 개념 (Confluence CKS 범위 외)

---

## Step 5 — `[Hybrid Benchmark]` 하이브리드 Retrieval 재검증

### Summary
Step 2에서 수립한 벤치마크(benchmark_v1.json)를 Step 4의 하이브리드 리트리버에 재적용하여, CKG 단독 대비 **정확도 · 토큰 효율 개선폭**을 정량 측정. 결과가 기대치 미달이면 Retrieval Playbook / RRF 파라미터 튜닝.

### Background
- Step 2에서 측정한 CKG-only 베이스라인을 기준점으로, 하이브리드가 얼마나 개선하는지 확인.
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 1 Acceptance는 **"Citation accuracy = 100%" + "Eval 데이터셋: 30개 known-answer 쿼리 baseline 수립"**. 본 Step은 이 baseline 위에서 하이브리드 개선폭을 로컬 지표(Precision/Recall/Hallucination/Token budget)로 추가 측정.
- 이 결과가 이후 Step 6+ 전체의 투자 근거가 됨.

### Scope
- Step 2의 4-baseline runner를 하이브리드 포함 **5-baseline**으로 확장:
  - alpha: raw file dump
  - beta: whole graph dump
  - gamma: granular MCP (find_*)
  - delta: smart 1-shot (CKG-only)
  - **epsilon: smart 1-shot (CKG+CKV hybrid)** ← NEW
- benchmark_v1.json에 multi-lang / cross-lang query 추가 (Solidity ↔ TS binding, Go → Sol ABI)
- Regression test: 하이브리드가 CKG-only 대비 **정확도 감소 시 PR block**
- 파라미터 sensitivity 분석: RRF k 값, Vector top-K, Graph hop depth 각각 sweep
- 결과를 `docs/BENCHMARK.md`에 기록 (상위 참조: [CKS §11 KPI](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS))

### Definition of Done
- [ ] 5-baseline runner 동작
- [ ] epsilon(hybrid)가 delta(CKG-only) 대비 Answer accuracy **+5% 이상** 개선 (또는 미개선 시 원인 분석 리포트)
- [ ] Hallucination 여전히 0
- [ ] Token budget 내에서 Recall ≥ 0.70 달성
- [ ] 파라미터 sweep 결과 표 + 최적값 선정 ADR
- [ ] CI에서 epsilon baseline 회귀 감지

### Dependencies
- **Upstream**: Step 2(benchmark), Step 4(hybrid)
- **Unblock**: Step 6(chainbench 통합 의사결정)

### References
- [Implementation Roadmap §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) (Phase 1 Acceptance)

---

## Step 6 — `[Chainbench Integration]` LLM 기반 chainbench 회귀 테스트 연동

### Summary
LLM이 생성한 코드 변경이 실제 블록체인 시나리오(합의 / 송금 / 재구성 등)에서 깨지지 않는지 자동 검증하기 위해 [0xmhha/chainbench](https://github.com/0xmhha/chainbench) 를 CL2 Build/Test Runner의 capability로 통합. dry-run 결과를 CKS에 feedback하여 risk_level 판정에 활용.

### Background
- [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 CL2 Acceptance: "Go/Solidity/JS/TS/Shell + Sanitizer"의 Go 경로 확장.
- [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 CL2 위에서, chainbench 결과를 CKS edge에 verified 마킹하는 **로컬 확장 feedback loop** 추가 (Confluence 원문에는 없음).
- LLM 생성 코드의 **블록체인 semantic 검증**은 unit test 로 커버 안 됨 — chainbench 같은 시나리오 기반 프로브가 필수.

### Scope
- **chainbench adapter** (CL2 내부): `run_chain_test(profile, config)` capability
- Output Schema: `{passed, failed_scenarios[], gas_usage, timing, logs_ref}` — typed (free-form 금지, [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 CL2 "sanitized output" 정책 적용)
- **Feedback Loop**: chainbench 실패 시 관련 함수 node에 `verified_failed_scenario` 엣지 부착 (로컬 확장 — Confluence Phase 2 CL2 범위 외)
- CS3 Workflow Engine 연동: WF1의 post-PR 단계에 chainbench gate (선택)
- Sanitizer: chainbench 출력에서 RPC endpoint / validator 주소 등 민감 토큰 마스킹
- Quota / Circuit breaker 연동 (`policies/quotas.yaml`) — chainbench가 리소스 많이 먹으므로 필수

### Definition of Done
- [ ] `run_chain_test(profile="hardfork-v2")` 실행 → 구조화된 결과 반환
- [ ] 실패 시나리오가 CKS `verified_failed_scenario` 엣지로 저장
- [ ] CL2 Output Schema validator 통과 (free-form 금지 정책)
- [ ] Quota 초과 시 PolicyError 반환
- [ ] 단위 테스트: mock chainbench + 실제 chainbench 양쪽 통과
- [ ] WF1(Phase 4)에서 post-PR gate로 호출되어 risk_level 판정에 반영

### Dependencies
- **Upstream**: Step 1(CKG), Step 4(hybrid retrieval → `impact_of_change`가 chainbench scope 선택에 활용)
- **Parallel**: Phase 2 CL2 / CL4 개발과 병렬 가능

### References
- https://github.com/0xmhha/chainbench
- [Tier A §2](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045788/Tier+A+Capability+Services) (CL2 Build/Test Runner — 다언어 빌드·테스트·린트·sanitizer)
- [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) (Phase 2 CL2)

---

## Step 7 — `[Auto PR & Slack]` GitHub PR 자동 생성 + Slack 알림

### Summary
LLM이 생성한 코드 변경(changeset)을 자동으로 branch / commit / push / PR로 변환하고, 결과를 지정 Slack 채널에 구조화된 알림으로 게시. CL3 VCS Operator + CL5 Slack Adapter의 Phase 2 산출물.

### Background
- [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 CL3 / CL5에 대응.
- 이 단계까지 오면 "CKG + Hybrid Retrieval + chainbench 검증"을 거친 LLM 산출물이 **사람 리뷰 단계까지 자동 전달** 됨 → UC-A1(Bug Fix) / UC-A2(Feature) 의 Human Gate 직전 단계까지 완성.

### Scope
- **CL3 VCS Operator** ([Tier A §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045788/Tier+A+Capability+Services)):
  - `git commit / push / branch_create`
  - `pr_create` — template-driven (LLM 자유 텍스트 금지, 정형 템플릿만)
  - PR body 템플릿: 변경 요약 + Jira 링크 + checklist + chainbench 결과
  - PR lock mechanism (동일 브랜치 동시 PR 방지)
- **CL5 External Adapter** (Slack 부분):
  - `slack_post(channel, template, vars)` — 템플릿 기반
  - 알림 유형: PR opened / PR merged / chainbench failed / human gate required
  - Rich 포맷: Risk Panel(capability + manifest_ref + signer + recent activity) 자동 첨부 (로컬 확장 — Confluence Implementation Roadmap 범위 외, `claudedocs/10` 로컬 문서의 Human Gate 개념에서 유래)
- **Dry-run**: 실제 Slack 메시지 / GitHub PR 생성 없이 payload만 출력
- **Sanitizer**: PR body / Slack 메시지에서 secret ref 마스킹, raw value 절대 노출 금지 ([Security Model §3 Capability-Based Security](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623013082/Security+Model+Capability-Based+Security+and+Sanitization+Overview))

### Definition of Done
- [ ] `pr_create` 호출 시 template 기반 PR body 생성
- [ ] 동일 branch 동시 호출 시 Lock error 반환
- [ ] Slack 5개 알림 유형 각각 dry-run 통과
- [ ] Capability Vault 미통과 secret은 PR / Slack 메시지에 절대 등장 안 함 (보안 테스트)
- [ ] Audit log에 모든 git/Slack action 기록
- [ ] mTLS client cert로 caller 검증 (Phase 3 전제지만 mock cert로 미리 test)

### Dependencies
- **Upstream**: Phase 0 Foundation (Envelope lib, Audit log)
- **Parallel**: Step 6(chainbench)과 병렬 가능 — 결과만 소비
- **Unblock**: Step 8 WF1 실행

### References
- [Tier A §3, §5](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045788/Tier+A+Capability+Services) (CL3 VCS Operator / CL5 External System Adapter)
- [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap)
- [Security Model §3, §6](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623013082/Security+Model+Capability-Based+Security+and+Sanitization+Overview) (Capability-Based Security + Secret Lifecycle)

---

## Step 8 — `[Jira → PR]` WF1 전체 자동화 워크플로우

### Summary
Jira 티켓 생성 / 수정 webhook → 요구사항 LLM 분석 → hybrid retrieval로 작업 플랜 생성 → 자동 구현 → chainbench 검증 → PR 자동 생성 → Slack 알림 → Jira 댓글로 진행 상황 업데이트까지 end-to-end. CTO 보고서의 "3-line 목표" 실현 지점.

### Background
- [Implementation Roadmap §5, §6](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 3 + Phase 4 전체에 대응.
- 시스템의 flagship reference workflow (WF1) — Tier A/B/C 모두 투입.
- [UseCases §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623864888/UseCases) Group A Implementation의 **UC-A1 Bug Fix / UC-A2 Feature / UC-A4 Direct-Prompt**를 한 워크플로우로 실현.

### Sub-stories

#### 8.1 `[CS1]` Intake & Sanitization Gateway
- Jira webhook 수신 → `WorkRequest` 정규화
- Sanitization pipeline (6개 baseline 패턴 `pi-imperative-001` ~ `pi-base64-006`)
- Policy engine 적용 → 차단된 입력은 fail-closed

#### 8.2 `[CS3]` Workflow Engine (YAML 기반)
- `jira-to-pr.yaml` DSL 매니페스트 (Workflow YAML v1)
- State store (run_id 영속화, replay 지원)
- Gate enforcer (must_pass / human_gate / budget_gate)
- Compile-time type checker (`cmd/workflow-validate`)

#### 8.3 `[CS2]` Task Planner
- Jira 티켓 → Intent extractor (LLM 기반)
- Hybrid retrieval(Step 4)로 Evidence Pack 확보 → 작업 플랜 생성
- Complexity classifier + Task graph builder
- Plan validator — invalid plan → 재생성 1회 → human gate

#### 8.4 `[CS5]` Execution Session Manager
- 각 task step → 외부 LLM 호출 → structured changeset 파싱
- Touched files deriver (CKG `impact_of_change`와 교차 검증)
- `allowed_tools` envelope 주입 (first-party MCP only)
- Dry-run cache store

#### 8.5 `[WF1]` Jira→PR YAML + E2E
- `workflows/jira-to-pr.yaml` 작성
- 5 known ticket 실제 외부 LLM 사용 E2E 테스트
- Jira 댓글로 각 step 진행 상황 자동 업데이트 (Jira adapter)
- 성공률 baseline 측정

### Definition of Done (전체)
- [ ] Jira webhook 수신 → 5분 내 PR 자동 생성 (성공 경로)
- [ ] 5개 known ticket 중 ≥ 3개가 머지 가능 품질 PR 생성
- [ ] Jira 티켓에 step별 댓글 자동 기록 (started / evidence collected / implementing / PR opened / chainbench result)
- [ ] Slack 알림이 각 마일스톤마다 발송
- [ ] Human gate 4-dim 강화 통과 (Risk Panel + Justification + Dual approval + Min deliberation)
- [ ] Run state 영속화, crash 후 resume 가능
- [ ] 모든 step이 Audit hash chain으로 기록됨

### Dependencies
- **Upstream**: Step 1~7 전부 + Phase 0 Foundation + CL4 Vault ([Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 — Capability Services)
- **Critical path**: 본 시스템에서 가장 복잡한 통합 지점. Phase 1~3 단위 검증이 충실해야 실패 위험 축소.

### References
- [Tier C YAML §3](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623537389/Tier+C+YAML) (WF1 Jira → PR Workflow — full YAML + Sequence Diagram)
- [Tier B §2~§5](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623504585/Tier+B+Coordination+Services) (CS1 Intake / CS2 Planner / CS3 Workflow / CS4 Observability 상세 — CS5 Execution Session Manager는 로컬 확장)
- [Implementation Roadmap §5, §6](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap)

---

## Step 9 — `[Rest of HLD]` 나머지 HLD 작업 (Phase 2 CL4 / Phase 5 / Phase 6+)

### Summary
Step 8까지의 flagship 구현 완료 이후 HLD 문서에 정의된 **보안 / 관측 / 품질 인프라 + 추가 워크플로우**를 점진 확장. 본 Step은 여러 독립 sub-epic의 컨테이너.

### Background
- [Implementation Roadmap §4, §7, §8](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 CL4 Vault / Phase 5 Observability / Phase 6+ 추가 워크플로우. (CKS 고도화·Concurrency Graph 등은 로컬 확장)
- 이 단계부터는 **"새 코드 거의 없음"** — 기존 서브시스템 조립으로 가치 확장 (YAML 1장으로 새 WF).

### Sub-epics

#### 9.1 `[CKS 고도화 (로컬 확장)]` Concurrency + Git + Feedback
- Concurrency Graph (go/ssa 기반 정밀 분석)
- Git History Backend (symbol ↔ commit, blame cache)
- Distributed Interaction Graph (consensus message handler mapping)
- CL2 → CKS Feedback Loop (go test -race → race edge)
- `verify_patch` v2 (v1 + `impact_of_change` 통합)
- Retrieval Playbook v2 (concurrency_safety 추가)
- Systemization Report v2

**DoD**: 동시성 질의 5개 시나리오에 정확 응답 + `go test -race` 결과 CKS 반영 + `SafetyCritical` label 자동 라우팅.

#### 9.2 `[CL4]` Capability Vault + 10-step Verification
- SOPS+age 또는 Vault PKI engine 중 선택
- 7-step → 10-step verification (manifest fetch + cert chain + policy triple lookup + quota/circuit/kill switch)
- `preflight(envelope)` advisory capability
- Revocation layer (`policies/revoked_refs.yaml`)
- Audit hash chain + AES-256-GCM raw params encryption
- Kill switch admin API

**DoD**: [Implementation Roadmap §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) Phase 2 CL4 Acceptance 통과 ("send_tx --from_ref test-alice → secret 미노출 + audit log"). Revocation/Cache hit/Audit chain 등 세부 지표는 로컬 확장 목표.

#### 9.3 `[PKI + Manifest Signing]` 보안 인프라
- 자체 CA (smallstep / cfssl / Vault PKI 중 선택)
- 24h short-lived client cert
- `cmd/manifest-sign` + `cmd/manifest-verify` (ECDSA P-256, cosign 호환)
- `policies/` 디렉토리 governance (CODEOWNERS + dual-review)

**DoD**: 모든 inter-subsystem 호출 mTLS로 보호 + 매니페스트 위조 100% 탐지.

#### 9.4 `[CS4]` Observability + Benchmark Batch
- Metric Collector (Prometheus-style)
- Trace Aggregator (OpenTelemetry)
- Audit Aggregator
- KPI Dashboard (Grafana)
- Benchmark Batch Runner (UC-D3, 일일 회귀)
- Alert Engine
- **Phase 1 시작 직후부터 병렬 수집**이므로 Step 1부터 trace_id/metric 심어두기 (Phase 5는 "완성"만 Step 9에서)

**DoD**: KPI dashboard에서 lead time / success rate / token usage 조회 가능 + alert 정상 발화.

#### 9.5 `[WF2~WF6]` 추가 Reference Workflows
- WF2 Code Explain (UC-B1 워크플로우 wrap)
- WF6 PR Review (UC-C1)
- WF4 Test Generation (UC-A5)
- WF3 Debug Assist (UC-B3)
- WF5 Hardfork (UC-A3)

**DoD**: 각 WF가 YAML 1장만으로 동작 + 기존 코드 0줄 변경 + 개별 acceptance criteria 통과.

### Definition of Done (전체)
- [ ] Phase 2 CL4 / Phase 5 / Phase 6+ 모두 Acceptance 통과 (CKS 고도화는 로컬 확장 목표)
- [ ] 9개 서브시스템 운영 가능 (CS4 dashboard에서 9개 health check 녹색)
- [ ] 새 WF 추가 시 YAML 1장 이내 변경만으로 deploy 가능 입증 (WF2~WF6 중 최소 2개)

### Dependencies
- **Upstream**: Step 1~8 완료 + Phase 0 Foundation
- **Parallelizable**: 9.1 / 9.2 / 9.3 / 9.4 는 팀 독립 분리 가능. 9.5는 9.1~9.4 이후.

### References
- [Implementation Roadmap §4, §7, §8](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) (Phase 2 / Phase 5 / Phase 6+)
- [CKS Deep Dive](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623045773/Code+Knowledge+System+CKS) (CKS 고도화 참고 — Concurrency Graph / Git History / Feedback Loop 등 세부는 로컬 확장)
- [Security Model](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623013082/Security+Model+Capability-Based+Security+and+Sanitization+Overview) (상위 참조 — PKI / mTLS / Manifest Signing은 Confluence Security Model 범위 외 로컬 확장)
- [Tier C YAML §4](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623537389/Tier+C+YAML) (WF2 Code Explain / WF3 Debug Assist / WF4 Test Gen / WF5 Hardfork / WF6 PR Review YAML 샘플)

---

## 부록 A — Step 간 의존성 그래프

```
                ┌─────────────────┐
                │ Step 1  CKG     │  (현재 진행)
                └────────┬────────┘
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
   ┌───────────┐  ┌───────────┐  ┌───────────┐
   │  Step 2   │  │  Step 3   │  │ Step 7    │
   │ CKG Bench │  │   CKV     │  │ Auto PR + │
   └─────┬─────┘  └─────┬─────┘  │  Slack    │
         │              │        └─────┬─────┘
         │              ▼              │
         │        ┌───────────┐        │
         │        │  Step 4   │        │
         │        │  Hybrid   │        │
         │        └─────┬─────┘        │
         │              │              │
         │              ▼              │
         │        ┌───────────┐        │
         └───────▶│  Step 5   │        │
                  │ Re-bench  │        │
                  └─────┬─────┘        │
                        │              │
                        ▼              │
                  ┌───────────┐        │
                  │  Step 6   │        │
                  │chainbench │        │
                  └─────┬─────┘        │
                        │              │
                        ▼              │
                  ┌───────────┐        │
                  │  Step 8   │◀───────┘
                  │ WF1 Jira  │
                  │   → PR    │
                  └─────┬─────┘
                        │
                        ▼
                  ┌───────────┐
                  │  Step 9   │  (Phase 2 CL4/5/6+)
                  │ Rest HLD  │
                  └───────────┘
```

**Critical Path**: Step 1 → Step 3 → Step 4 → Step 5 → Step 8 → Step 9.
**Parallelizable**: Step 2 (Step 1 중반부터), Step 6/7 (Step 4 이후 병렬), Step 9 sub-epic 간 병렬.

---

## 부록 B — Jira 라벨링 제안

| 라벨 | 용도 |
|---|---|
| `epic-ckg` | Step 1 전체 (Engine / MCP / Eval / Viewer 공통) |
| `epic-ckg-engine` | Step 1.1 하위 |
| `epic-ckg-mcp` | Step 1.2 하위 |
| `epic-ckg-eval` | Step 1.3 하위 |
| `epic-ckg-viewer` | Step 1.4 하위 |
| `epic-ckv` | Step 3 하위 |
| `epic-hybrid` | Step 4 하위 |
| `epic-benchmark` | Step 2 / Step 5 |
| `epic-chainbench` | Step 6 |
| `epic-workflow` | Step 7 / Step 8 / Step 9.5 |
| `epic-security` | Step 9.2 / 9.3 |
| `epic-observability` | Step 9.4 |
| `area-cks-layer1` / `layer2` / `layer3` / `layer4` | CKS 내부 레이어 구분 |
| `lang-go` / `lang-ts` / `lang-sol` | 언어별 작업 |
| `blocked-by-{step#}` | 의존성 명시 |
| `critical-path` | Critical path 상의 티켓 (우선 할당) |
| `phase-0` ~ `phase-6` | HLD Phase 원본 매핑 |

---

## 부록 C — 티켓 작성 체크리스트

각 Jira 티켓 생성 시 다음 필드 채우기:

- [ ] **Summary**: 한 줄, 동사로 시작
- [ ] **Background**: 왜 필요한가 (HLD 참조 + 선행 실험 링크 포함)
- [ ] **Scope (In / Out)**: 본 티켓이 포함 / 제외하는 것 명시
- [ ] **DoD**: 측정 가능한 체크리스트
- [ ] **Dependencies**: upstream/downstream 티켓 링크
- [ ] **References**: `claudedocs/` 문서 + spec/plan 경로
- [ ] **Risks**: 알려진 위험 + 완화책
- [ ] **Label**: 부록 B 참조
- [ ] **Epic Link**: Step 1~9 중 해당

---

## 부록 D — 변경 이력

| 날짜 | 변경 | 근거 |
|---|---|---|
| 2026-04-24 | 초안 작성 (Step 1~9) | [Implementation Roadmap](https://wemade.atlassian.net/wiki/spaces/platfomDev/pages/2623406284/Implementation+Roadmap) + 사용자 9 단계 지시 |
