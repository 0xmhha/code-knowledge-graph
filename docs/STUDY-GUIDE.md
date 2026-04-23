# CKG Study Guide — 외부 개념 학습 자료

| Field | Value |
|---|---|
| Document ID | study-guide |
| Date | 2026-04-23 |
| Status | Living document (확장 가능) |
| Audience | CKG 사용자/개발자 (특히 graph clustering / MCP / build cache 무효화 관련 사전 지식이 부족한 경우) |
| Companion | `docs/spec-ckg-v0-prototype.md` (V0 spec 본문) |

본 문서는 CKG spec 본문에 반복할 필요는 없지만, 구현·운영·확장 시 알아두면 도움이 되는 외부 개념을 정리한다. 매번 spec 옆에서 참조할 필요는 없고, 호기심이나 V1+ 진화 결정 시 펼쳐 보는 용도.

---

## Table of Contents

1. [Graph Clustering / Community Detection](#1-graph-clustering--community-detection)
2. [MCP (Model Context Protocol)](#2-mcp-model-context-protocol)
3. [Build Artifact Staleness / Cache Invalidation](#3-build-artifact-staleness--cache-invalidation)
4. [Bonus — Tree-sitter & AST 파싱](#4-bonus--tree-sitter--ast-파싱)
5. [Bonus — Force-directed 3D Graph Layout](#5-bonus--force-directed-3d-graph-layout)

---

## 1. Graph Clustering / Community Detection

### 1.1 What & Why

**Graph clustering (community detection)** 은 *밀접하게 연결된 노드 무리(community)*를 자동으로 찾아내는 알고리즘 군. 코드 그래프에 적용하면 *호출/사용 관계로 실제 함께 동작하는 코드 무리*를 발견 — 디렉토리 경계와 무관한 *기능 단위*가 드러남.

**왜 CKG 가 이걸 쓰나**: pkg_tree (디렉토리) 만으로는 *사람이 정한 분류*만 보임. Leiden topic_tree 가 *코드 자체가 말해주는 분류*를 추가로 보여줌. Viewer 토글로 두 시각을 비교.

### 1.2 핵심 개념

- **Modularity (Q)**: community 분할의 품질 지표. *내부 edge 밀도 - 무작위 그래프에서 기대되는 밀도*.
  - 공식: `Q = sum_c (e_c/m - (k_c/2m)^2)` — `m`=총 edge 수, `e_c`=community c 내부 edge 수, `k_c`=community c 의 total degree
  - Q ≥ 0.3 정도면 의미 있는 community 구조가 있다고 판단 (경험치)
- **Resolution parameter γ**: modularity 의 일반화 — γ↑ → 더 작고 많은 community / γ↓ → 더 크고 적은 community
- **Resolution limit**: γ=1 (기본값) modularity 의 한계 — 매우 작은 community 는 못 찾음. 이게 multi-resolution이 필요한 이유.

### 1.3 알고리즘 비교

| 알고리즘 | 발표 | 핵심 idea | 장점 | 단점 |
|---|---|---|---|---|
| **Louvain** | Blondel et al. 2008 | greedy node-swap → super-node aggregation 반복 | 빠름, 표준 | community가 disconnected 될 수 있는 결함 |
| **Leiden** ★ | Traag et al. 2019 | Louvain 의 결함 수정 + refinement step 추가 | well-connected 보장, 빠른 수렴 | 구현이 약간 더 복잡 |
| Label propagation | Raghavan et al. 2007 | 노드가 이웃의 다수 label 채택 반복 | 매우 빠름, 단순 | 비결정적 (random) |
| Spectral | Newman 2006 | 그래프 라플라시안 eigenvector 분해 | 수학적 우아함 | O(n³), 큰 그래프에 X |
| Infomap | Rosvall & Bergstrom 2008 | 정보이론 (random walk 압축) | community 외에 정보 흐름도 봄 | modularity와 다른 직관 |
| SBM (stochastic block model) | Holland 1983 / Peixoto | bayesian 모델 fitting | 통계적 검증 가능 | 무거움 |

### 1.4 학습 경로

#### 입문 (1~2 시간)

- **책 (무료 챕터)**: Mark Newman, *Networks: An Introduction* (Oxford 2010), Ch.7 "Communities" — 직관 잡기, 수학 가벼움
- **영상 (무료)**: Stanford CS224W (Jure Leskovec) 강의 시리즈 Lecture 13~14 "Community Detection" — YouTube 검색 "stanford cs224w community detection"

#### 중급 (4~6 시간)

- **핵심 paper**: V.A. Traag, L. Waltman, N.J. van Eck, "From Louvain to Leiden: guaranteeing well-connected communities", *Scientific Reports* 9 (2019). **9 페이지, 매우 읽기 쉬움**. arXiv: https://arxiv.org/abs/1810.08473
- **igraph 문서** (시각 예시 풍부): https://igraph.org/c/doc/igraph-Community.html
- **modularity 한계 paper**: Fortunato & Barthélemy, "Resolution limit in community detection", PNAS 2007 — 왜 multi-resolution이 필요한지

#### 실습 (반나절)

```python
# Python — 작은 그래프로 직접 돌려보기
pip install python-igraph leidenalg networkx
```

```python
import igraph as ig
import leidenalg

# Karate Club (community detection 의 hello-world)
g = ig.Graph.Famous("Zachary")
partition = leidenalg.find_partition(g, leidenalg.ModularityVertexPartition)
print(partition)  # 보통 2개 community
g.community_multilevel()  # Louvain 비교용
```

→ 자기 프로젝트 코드를 nodes/edges 로 변환해서 돌려보면 *내 코드의 숨은 community* 발견 경험.

#### 구현 reference (CKG Go 포팅용)

- **Java reference 구현**: https://github.com/CWTSLeiden/networkanalysis — 같은 저자 (Traag) 가 만든 read-friendly Java. `Leiden.java` + `CPMClusteringAlgorithm.java` + `Network.java` 가 핵심.
- **C++ 코어**: https://github.com/vtraag/leidenalg — Python wrapper 의 C++ 코어. 성능 최적화 참고.
- **검증용 Python**: 같은 input 으로 Python `leidenalg` 결과와 Go 구현 결과를 비교 (test fixture cross-check)

#### 심화 (관심 있을 때)

- **Map equation / Infomap**: Rosvall & Bergstrom, "Maps of random walks on complex networks reveal community structure", PNAS 2008 — 정보이론 기반 대안
- **Bayesian SBM**: Tiago Peixoto의 *graph-tool* 라이브러리 + 그의 lecture notes
- **Hierarchical community detection**: Lancichinetti et al. 의 OSLOM, 또는 hierarchical SBM
- **Dynamic community detection**: 시간에 따라 community 변화 추적 (CKG V2+ commit 시계열 분석에 잠재 적용)

### 1.5 CKG에서의 적용 요약

- 알고리즘: Leiden, modularity 함수
- Resolution levels: γ ∈ {0.5, 1.0, 2.0} → 3-level hierarchy
- Random seed: 42 (고정)
- Naming: `<dominant_pkg> — <common_substring>* + <top_pagerank_node>` 휴리스틱
- Spec 본문 §5.5 참조

---

## 2. MCP (Model Context Protocol)

### 2.1 What & Why

**MCP** 는 Anthropic 이 2024년 11월 발표한 *LLM 클라이언트 ↔ 외부 도구·데이터 소스 연결의 표준 프로토콜*. 비유: *LLM 을 위한 USB-C*. 한 번 만든 MCP 서버는 Claude Code, Claude Desktop, Cursor, Continue, Zed, Sourcegraph Cody 등 모든 호환 클라이언트에서 사용 가능.

**왜 표준이 필요한가**: MCP 이전에는 각 LLM 클라이언트마다 자체 plugin 시스템 (VS Code extension API, Cursor's MCP-like, Anthropic's Tool Use API 등) — 같은 통합을 N 번 작성해야 했음. MCP 는 N×M → N+M.

### 2.2 핵심 개념

- **MCP Server**: 외부 데이터/도구를 노출하는 독립 프로세스. Tools (호출 가능한 함수), Resources (읽기 가능한 데이터), Prompts (재사용 가능한 프롬프트 템플릿) 노출.
- **MCP Client**: LLM 호스트 (Claude Code 등). 서버 spawn + 통신.
- **Transport**:
  - **stdio**: 자식 프로세스 fork + stdin/stdout JSON-RPC. 가장 단순, V0 채택.
  - **HTTP**: 원격 서버. JWT 등 auth 필요. V1+.
  - **SSE (Server-Sent Events)**: streaming 응답 (긴 작업 진행 표시).
- **JSON-RPC 2.0**: 모든 transport 의 wire format. method + params → result/error.

### 2.3 메시지 흐름 (단순화)

```
Client                            Server
  │                                 │
  │── initialize ────────────────▶  │
  │◀─ capabilities ─────────────    │
  │                                 │
  │── tools/list ────────────────▶  │
  │◀─ [tool descriptions] ──────    │
  │                                 │
  │── tools/call ────────────────▶  │
  │   name=get_context_for_task     │
  │   args={...}                    │
  │◀─ tool result ─────────────     │
  │   content=[{type:text, ...}]    │
  │                                 │
```

LLM 은 `tools/list` 결과로 어떤 도구들이 있는지 알고, 자연어 prompt 처리 중 적절한 도구를 직접 결정해서 `tools/call`.

### 2.4 학습 경로

#### 입문 (1 시간)

- **공식 발표 글**: https://www.anthropic.com/news/model-context-protocol — *왜* 만들었는지 동기 + 비전
- **공식 문서 (5분)**: https://modelcontextprotocol.io — Quick intro + Quick start

#### 개념 정리 (2 시간)

- **Spec 문서**: https://spec.modelcontextprotocol.io — 정확한 wire format + capabilities
- 핵심 개념 정리:
  - Tool / Resource / Prompt 차이
  - Capabilities negotiation (초기화)
  - Lifecycle (initialize → operate → shutdown)
  - Error handling (JSON-RPC error codes)

#### 실습 (반나절)

- **공식 server 예시 (Python)**: https://github.com/modelcontextprotocol/servers — filesystem, github, postgres, slack 등. 코드 짧고 패턴 일관.
- **Claude Code 에 등록 실습**:
  ```bash
  # 가장 간단: filesystem 서버
  claude mcp add filesystem --command "uvx" --args "mcp-server-filesystem,/tmp"
  ```
  → Claude Code 에서 `/tmp` 의 파일을 자연어로 다룰 수 있게 됨.
- 자기 프로젝트의 작은 MCP 서버 작성해보기 (Python 또는 Go)

#### Go 구현 (1 일)

- **공식 Go SDK**: https://github.com/modelcontextprotocol/go-sdk
- **커뮤니티 SDK**: https://github.com/mark3labs/mcp-go (더 ergonomic, 예시 많음)
- 둘 다 stdio + HTTP + SSE 모두 지원. V0 (CKG) 는 stdio 만.
- 패턴:
  ```go
  server := mcp.NewServer("ckg")
  server.AddTool("get_context_for_task", getContextSchema, getContextHandler)
  server.Run(stdioTransport)
  ```

#### 심화

- **MCP vs Tool Use API 비교**: Anthropic Tool Use 는 *single-turn 도구*, MCP 는 *long-lived 서비스*. 같은 일을 두 방식으로 구현 → 차이 명확해짐
- **Security model**: Anthropic 의 trust tier 논의 (first-party / vetted third-party / community)
- **Discovery & registry**: 사용자가 MCP 서버를 어떻게 찾고 설치하나 — 미해결, 진화 중인 영역
- **Stateful resources**: MCP 가 단순 RPC 를 넘어 *resource subscription* (변경 알림) 까지 가는 구상

### 2.5 CKG에서의 적용 요약

- Transport: stdio (V0)
- 서버: Go 단일 바이너리 `ckg mcp --graph=DIR`
- 도구 6개: `get_context_for_task` (★) + `find_symbol` / `find_callers` / `find_callees` / `get_subgraph` / `search_text`
- Eval baseline 별 도구 allowlist 다르게 → 가설 검증 (§9.1)
- Spec 본문 §8 참조

---

## 3. Build Artifact Staleness / Cache Invalidation

### 3.1 What & Why

**Stale build artifact**: 빌드 시점의 input (소스 코드, 설정) 으로부터 만든 산출물 (artifact: graph, binary, cache 등) 이 *input 변경 후 outdated 상태*. CS 의 오래된 이중 hard problem 중 하나 (Phil Karlton: "There are only two hard things in Computer Science: cache invalidation and naming things").

**왜 중요**: stale artifact 를 *모르고 사용*하면:
- 잘못된 결과 (deleted code 추천)
- 시간 낭비 (없는 함수 디버깅)
- 신뢰 저하 (도구가 거짓말 한다는 인상)

### 3.2 일반 패턴

| 레벨 | 감지 방법 | 비용 | 정확도 |
|---|---|---|---|
| 1. Version/commit | `git rev-parse HEAD` 비교 | 미미 | git 단위 (commit 안 쳤으면 못 잡음) |
| 2. 샘플 mtime/hash | N 개 파일의 mtime/hash 합 | 작음 | 샘플에 없는 변경은 못 잡음 |
| 3. 전체 파일 hash | 모든 input 파일 hash | 큼 (I/O 많음) | 정확 |
| 4. Content-addressable | 모든 input 의 hash 가 artifact 의 이름 (e.g., Nix `/nix/store/<hash>-foo`) | 큼 | 완벽 |

### 3.3 실제 도구들의 접근

- **Make**: file mtime 비교 (단순, 가끔 false positive)
- **Bazel / Buck / Pants**: full file hash + action graph (정확, 빠름, 복잡)
- **Nix / Guix**: content-addressable storage — input hash 가 store path 자체
- **npm `package-lock.json`**: lockfile 의 `lockfileVersion` + 각 dep 의 integrity hash
- **Cargo**: `Cargo.lock` + 빌드 디렉토리의 `.fingerprint/`
- **Go build cache**: `~/.cache/go-build/` — input hash 기반 entry

### 3.4 학습 경로

이 영역은 학술 깊이보다 *실용 패턴 학습*이 가치 있음.

#### 입문

- Phil Karlton 의 인용 + 그 맥락 — 검색: "two hard things in computer science"
- "Cache invalidation patterns" — 일반 키워드 검색

#### 중급

- **Bazel 의 incremental build 모델**: https://bazel.build/run/build (action graph + content addressable)
- **Nix 의 derivation 개념**: https://nixos.org/manual/nix/stable/expressions/derivations.html

#### 심화

- "Build Systems à la Carte" — Mokhov, Mitchell, Peyton Jones (ICFP 2018). 빌드 시스템 분류학 paper.
- Andrey Mokhov 의 Hadrian (Haskell) 빌드 시스템 글들

### 3.5 CKG에서의 적용 요약

- 정책: Level 1 (git-based), 비-git 디렉토리 fallback Level 2 (5 random files mtime sum)
- 감지 시점: build / load (viewer/mcp/eval start)
- 표시: viewer banner + MCP metadata + eval log
- **자동 rebuild 안 함** — V0 측정 통제 우선
- Spec 본문 §6.5 참조

---

## 4. Bonus — Tree-sitter & AST 파싱

CKG TS/Sol 파서가 의존하는 핵심 라이브러리. 향후 언어 추가 시 알아야 함.

### 4.1 What

**Tree-sitter**: GitHub 에서 시작된 *incremental parser generator + runtime*. 여러 언어의 grammar 를 하나의 라이브러리로 처리. 핵심 강점:
- 빠름 (millisecond 단위 파싱)
- Incremental (편집 후 변경된 부분만 다시 파싱 — 에디터 통합)
- Error recovery (구문 오류 있어도 파싱 계속)
- Concrete syntax tree (AST 보다 더 raw — 모든 토큰 포함)

### 4.2 학습

- **공식 문서**: https://tree-sitter.github.io/tree-sitter/
- **Grammar 작성 가이드**: https://tree-sitter.github.io/tree-sitter/creating-parsers
- **Query syntax (CKG 가 사용)**: https://tree-sitter.github.io/tree-sitter/using-parsers#pattern-matching-with-queries
- **Go binding**: https://github.com/smacker/go-tree-sitter (CKG 채택)

### 4.3 실습

```bash
# Tree-sitter playground 으로 query 실험
npm install -g tree-sitter-cli
tree-sitter parse <file>
tree-sitter query <query.scm> <file>
```

또는 web playground: https://tree-sitter.github.io/tree-sitter/playground

---

## 5. Bonus — Force-directed 3D Graph Layout

CKG viewer (`3d-force-graph`) 가 사용하는 layout 알고리즘 군.

### 5.1 What

**Force-directed layout**: 노드를 입자로 보고 *charge* (반발력) + *spring* (edge 인장력) 등의 force 시뮬레이션으로 자연스러운 배치 계산. 1991년 Fruchterman-Reingold 알고리즘이 시초.

### 5.2 학습

- **Fruchterman-Reingold 알고리즘** (원조): Wikipedia 의 force-directed graph drawing 항목
- **D3 force module 문서**: https://github.com/d3/d3-force — 2D 표준이지만 같은 원리
- **3d-force-graph 문서**: https://github.com/vasturiano/3d-force-graph — CKG 채택, API 짧음
- **Three.js 기초** (선택): https://threejs.org/manual/ — 3D 렌더링 기초가 궁금할 때

### 5.3 핵심 파라미터

- `chargeStrength`: 노드 간 반발력. 음수 = 반발 / 양수 = 인력. 디폴트 -30.
- `linkDistance`: edge 의 자연 길이. 디폴트 30.
- `linkStrength`: edge 의 인장력. 디폴트 1/min(degree).
- `cooldownTicks`: 시뮬레이션 step 수 (CKG: 200)

### 5.4 LOD 와의 연계

CKG 의 fold/unfold 는 `nodeVisibility` accessor 로 구현. 핵심 트릭: *노드 set 자체를 변경하지 않고* 가시성만 토글 → relayout 발생 안 함, 즉각 반응.

---

**End of Study Guide.**
