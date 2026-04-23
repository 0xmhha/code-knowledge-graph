# CKG (Code Knowledge Graph) — V0 Prototype Design Spec

| Field | Value |
|---|---|
| Spec ID | spec-ckg-v0-prototype |
| Version | 1.0 |
| Status | Draft (awaiting user review) |
| Date | 2026-04-23 |
| Owner | mhha |
| Related | stablenet-ai-agent CKS plan (Phase 1-α/1-β), graphify reference project |

---

## Table of Contents

1. [Goal & Non-Goals](#1-goal--non-goals)
2. [Hypotheses (V0 measures)](#2-hypotheses-v0-measures)
3. [High-Level Architecture](#3-high-level-architecture)
4. [Build Pipeline](#4-build-pipeline)
5. [Graph Schema (29 nodes / 22 edges)](#5-graph-schema)
6. [Storage Format (SQLite + chunked JSON)](#6-storage-format)
7. [Viewer (3D + LOD + visual encoding)](#7-viewer)
8. [MCP Server (smart 1-shot)](#8-mcp-server)
9. [Eval Framework (4 baselines × 2-stage corpus)](#9-eval-framework)
10. [Repo / Module Layout](#10-repo--module-layout)
11. [Error Handling](#11-error-handling)
12. [Testing Strategy](#12-testing-strategy)
13. [Out-of-Scope (V0 → V1)](#13-out-of-scope)
14. [V0 Definition of Done](#14-v0-definition-of-done)
15. [References](#15-references)

---

## 1. Goal & Non-Goals

### 1.1 Goal

단일 Go 바이너리(`ckg`) 1개로 다음 4가지를 제공하여, **graph 기반 컨텍스트가 raw-file 컨텍스트 대비 token 효율 + 코드 작업 정확도에서 우월함**을 정량 검증한다.

1. Go / TypeScript-JavaScript / Solidity 코드를 정교하게 graph로 추출 (29 node types / 22 edge types, deterministic + Leiden topic overlay)
2. SQLite 단일 파일로 저장 + 정적 chunked JSON 으로 export 가능
3. Embedded 3D viewer로 LOD 기반 시각화 (도형/색/크기/glyph 으로 type/언어/usage 식별)
4. MCP server로 LLM에게 "smart 1-shot" 컨텍스트 제공 (granular ping-pong을 의도적으로 회피)

### 1.2 Non-Goals (V0 한정)

다음은 V0 범위가 **아님** — V1 (CKS) 또는 그 이후로 위임:

- LLM을 빌드/클러스터링/네이밍에 사용 (전부 휴리스틱으로 처리)
- Vector DB / 임베딩 / hybrid retrieval (V1 CKS Layer 1)
- 실시간 incremental update (V0는 full rebuild 만)
- Auth / 멀티테넌트 / 분산 (단일 머신, 단일 사용자 가정)
- 사용자 정의 schema 확장 (V0 schema_version 1.0 고정)

---

## 2. Hypotheses (V0 measures)

V0의 가치는 다음 가설들을 정량적으로 검증하는 것이다.

| # | 가설 | 측정 |
|---|---|---|
| H1 | CKG MCP δ-mode (smart 1-shot) 가 동일 task 에서 raw-file dump (α) 대비 LLM 입력 토큰을 **≥ 50% 절감** | 4-baseline eval, token count |
| H2 | CKG MCP δ-mode 가 동일 task 에서 raw-file (α) 대비 LLM 정답률 **동등 이상** (regression 없음) | 4-baseline eval, rubric/test pass |
| H3 | δ-mode 가 graph-dump (β) 와 granular ping-pong (γ) 둘 다 dominate (token + accuracy 양면) | 4-baseline eval 종합 |

**Eval baselines** (§9 상세):
- **α** raw file dump (도구 0개)
- **β** whole-graph dump via `get_subgraph(root, depth=∞)`
- **γ** granular MCP — `find_*` + `search_text` 5개 도구 (ping-pong)
- **δ** smart 1-shot MCP — `get_context_for_task` 1개 도구 ★

---

## 3. High-Level Architecture

### 3.1 컴포넌트 다이어그램

```
┌──────────────────────────────────────────────────────────────┐
│  Single Go Binary (ckg) — multi-OS (darwin/linux/windows)   │
│                                                              │
│   ┌──────┐  ┌──────┐  ┌──────┐  ┌────────┐  ┌──────┐        │
│   │build │  │serve │  │ mcp  │  │ export │  │ eval │        │
│   │      │  │HTTP+ │  │stdio │  │-static │  │      │        │
│   └──┬───┘  │ UI   │  │      │  └────┬───┘  └──┬───┘        │
│      │      └──┬───┘  └──┬───┘       │          │           │
│      ▼         ▼         ▼           ▼          ▼           │
│   ┌────────────────────────────────────────────────────┐    │
│   │  graph.db  (SQLite — single file storage)           │    │
│   │  - nodes, edges, hierarchy, blobs (source slices)  │    │
│   │  - cluster trees (package-tree + Leiden topic)     │    │
│   │  - FTS5 (name + qname + signature + doc)           │    │
│   └────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
              │
              │ external consumers
              ▼
   ┌──────────┐   ┌──────────┐   ┌──────────┐
   │ Browser  │   │ Claude   │   │ S3 / GH  │
   │ (viewer) │   │ Code/etc │   │ (static) │
   │          │   │ via MCP  │   │ chunks   │
   └──────────┘   └──────────┘   └──────────┘
```

### 3.2 5개 Subcommand

| Command | 입력 | 출력 |
|---|---|---|
| `ckg build --src=PATH --out=DIR [--lang=auto\|go,ts,sol]` | 소스 디렉토리 | `DIR/graph.db` + `DIR/manifest.json` |
| `ckg serve --graph=DIR [--port=N] [--open]` | graph DIR | HTTP 서버 + 임베디드 viewer (브라우저 자동 오픈 옵션) |
| `ckg mcp --graph=DIR` | graph DIR | stdio MCP 서버 (Claude Code/Cursor가 spawn) |
| `ckg export-static --graph=DIR --out=STATIC_DIR` | graph DIR | chunked JSON + 정적 viewer 자산 |
| `ckg eval --tasks=GLOB --graph=DIR --baselines=LIST --out=DIR` | task YAMLs + graph + LLM endpoint | 결과 CSV + 자동 생성 report.md |

### 3.3 핵심 설계 원칙

- **단일 바이너리, 단일 데이터 layer**: 4개 consumer (build/serve/mcp/eval) 모두 같은 SQLite를 공유. 데이터 일관성 자동 보장.
- **Multi-OS first-class**: CGO-free SQLite (`modernc.org/sqlite`) → cross-compile 무손실.
- **Boundary 외부에서 모든 입력 검증**: 내부 함수는 검증된 타입 신뢰 (coding-style.md 준수).
- **graphify의 강점 흡수, 약점 회피**: 단순 파이프라인 + 명확 schema는 차용. 142KB 단일 `extract.py` 패턴은 거부 — `internal/parse/<lang>/` per-language 모듈로 분리.

---

## 4. Build Pipeline

```
detect → parse → link → graph build → cluster → score → persist
  (1)     (2)    (3)     (4)          (5)       (6)      (7)
```

### 4.1 단계별 책임

| 단계 | 모듈 | 책임 | 입력 → 출력 |
|---|---|---|---|
| (1) detect | `internal/detect` | dir walk + `.ckgignore` 적용 + 언어별 분류 | dir → `[]FilePath` per language |
| (2) parse | `internal/parse/<lang>` | 언어별 AST → 노드/엣지 추출 | files → per-file `{nodes, edges}` |
| (3) link | `internal/link` | cross-file resolve + cross-language ABI binder | per-file → resolved global symbol table |
| (4) graph build | `internal/graph` | dedup, edge 통합, attr 부여 | symbol table → in-memory `Graph` |
| (5) cluster | `internal/cluster` | (a) package-tree backbone + (b) Leiden topic overlay | Graph → Graph + 2 hierarchies |
| (6) score | `internal/score` | in/out degree + PageRank + usage_score | Graph → Graph + scores |
| (7) persist | `internal/persist` | SQLite 저장 + manifest.json | Graph → `graph.db` + `manifest.json` |

### 4.2 파서 결정 (V0)

| 언어 | 파서 | 이유 |
|---|---|---|
| Go | **`golang.org/x/tools/go/packages`** (내부적으로 `go/parser` + `go/types`) | 모듈-인지 (go.mod) + 의존성 자동 로드 + type info 자동 채움. interface dispatch resolution 위해 `go/types` 필수. |
| TS / JS | `tree-sitter-typescript` + `tree-sitter-javascript` (Go-native binding via `github.com/smacker/go-tree-sitter`) | Node.js 런타임 의존성 0 (단일 바이너리 약속 유지). 타입 추론은 *없이* 이름 휴리스틱. tsc/tsserver 호출은 V1+. |
| Solidity | `tree-sitter-solidity` | 성숙도 충분. solc 호출은 V0 회피. ABI는 AST에서 휴리스틱 추출 (Pass 3 cross-lang 용). |

> 각 언어의 AST → 노드/엣지 매핑 규칙은 **§4.6** 참조. Pass 모델(structural/resolution/cross-lang/validation)은 **§4.7** 참조. Confidence 부여 규칙은 **§4.8** 참조.

### 4.3 Cross-Language Linking (V0 범위)

| 범위 | 항목 |
|---|---|
| 포함 | Solidity contract ABI ↔ TS contract binding (typechain-style 자동 생성 binding 매칭) — `Contract.functionName` 일치 휴리스틱으로 연결 (`binds_to` edge 생성) |
| 포함 | TS `import('./generated/abi')` 같은 ABI artifact import 추적 |
| **제외 (V1)** | Go ↔ TS RPC 매칭, Go ↔ Solidity 매칭 |

대상 코드베이스가 web3 stack 인 경우 Sol ↔ TS 한 쌍이 가장 가치 있음.

### 4.4 `.ckgignore` 디폴트

```
vendor/
node_modules/
dist/
build/
*.generated.*
tests/fixtures/
# *_test.go  (옵션 토글)
```

### 4.5 Parser Interface & Dispatch

모든 언어 파서는 공통 `Parser` 인터페이스를 만족한다. Build pipeline의 `parse` 단계는 파일 확장자 기반으로 dispatch.

```go
// internal/parse/parser.go
type Parser interface {
    // Pass 1: Structural — AST 방문 후 file-local nodes/edges 발행.
    // Cross-file refs는 PendingRef placeholder로 표시.
    ParseFile(path string, src []byte) (*ParseResult, error)

    // Pass 2: Resolution — 같은 언어의 모든 ParseResult를 받아 PendingRef를 resolve.
    Resolve(results []*ParseResult) (*ResolvedGraph, error)

    // 지원 확장자 목록
    Extensions() []string
}

type ParseResult struct {
    Path    string
    Nodes   []graph.Node
    Edges   []graph.Edge
    Pending []PendingRef     // unresolved cross-file refs
}

type PendingRef struct {
    SrcID       string           // 이미 발행된 노드 ID
    EdgeType    graph.EdgeType
    TargetQName string           // resolve 대상 (cross-file qname 후보)
    HintFile    string           // import 추적 hint
}
```

**확장자 → 파서 dispatch**:

| 확장자 | Parser 모듈 |
|---|---|
| `.go` | `internal/parse/golang` |
| `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs` | `internal/parse/typescript` |
| `.sol` | `internal/parse/solidity` |

**병렬성 모델**:
- Pass 1 (Structural) — 파일 단위 **병렬 안전** (worker pool, GOMAXPROCS 까지)
- Pass 2 (Resolution) — 언어별 **sequential** (symbol table 빌드 때문)
- Pass 3 (Cross-language linking) — Pass 2 완료 후 **sequential**
- Pass 4 (Validation) — graph 전체 sequential

### 4.6 Per-Language AST → Graph 매핑 룰

#### 4.6.1 Go — `golang.org/x/tools/go/packages`

**Loader 설정**:

```go
cfg := &packages.Config{
    Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
          packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
          packages.NeedImports | packages.NeedModule,
    Dir:   srcRoot,
    Tests: false,  // V0 디폴트 — `--include-tests` 플래그로 토글
}
pkgs, err := packages.Load(cfg, "./...")
```

**AST → 노드 매핑**:

| AST | 노드 type | 비고 |
|---|---|---|
| `*ast.GenDecl{Tok:IMPORT}` | `Import` | + `imports` edge file→pkg |
| `*ast.FuncDecl{Recv:nil}` | `Function` | + `defines` edge file→func |
| `*ast.FuncDecl{Recv:!=nil}` | `Method` | + `defines` edge type→method |
| `*ast.GenDecl{Tok:TYPE}` Spec=`*ast.StructType` | `Struct` | + `Field` per field |
| `*ast.GenDecl{Tok:TYPE}` Spec=`*ast.InterfaceType` | `Interface` | + `Method` per signature |
| `*ast.GenDecl{Tok:TYPE}` Spec=other | `TypeAlias` | |
| `*ast.GenDecl{Tok:CONST}` | `Constant` | per ValueSpec.Names |
| `*ast.GenDecl{Tok:VAR}` | `Variable` | per ValueSpec.Names |
| `*ast.Field` (FuncType.Params) | `Parameter` | 함수/메서드 시그니처 |
| `*ast.AssignStmt{Tok:DEFINE}` LHS | `LocalVariable` | `:=` |
| `*ast.IfStmt` | `IfStmt` | |
| `*ast.ForStmt` | `LoopStmt{sub_kind:"for"}` | |
| `*ast.RangeStmt` | `LoopStmt{sub_kind:"range"}` | |
| `*ast.SwitchStmt` / `*ast.TypeSwitchStmt` | `SwitchStmt` | |
| `*ast.ReturnStmt` | `ReturnStmt` | |
| `*ast.GoStmt` | `Goroutine` | + `spawns` edge |
| `chan T` 타입의 state-level 선언 | `Channel` | |
| `*ast.SendStmt` | (CallSite + `sends_to` edge) | |
| `*ast.UnaryExpr{Op:ARROW}` | (CallSite + `recvs_from` edge) | |

**AST → 엣지 매핑** (Pass 1 placeholder, Pass 2 resolve):

| 트리거 | Edge | Resolution |
|---|---|---|
| `*ast.CallExpr` | `calls` 또는 `invokes` | `pkg.TypesInfo.Uses[ident]` → `*types.Func`. interface receiver 면 `invokes` (INFERRED) |
| `*ast.SelectorExpr` field 읽기 | `reads_field` | `pkg.TypesInfo.Selections[sel]` |
| `*ast.AssignStmt` LHS=field | `writes_field` | 동일 |
| `*ast.CompositeLit` | `instantiates` | type literal |
| `pkg.TypesInfo.Implements(T, I)` 매트릭스 | `implements` (INFERRED) | post-pass per package |
| `*ast.StructType.Fields` 의 embedded type | `extends` (Go embedding) | |
| `*ast.Ident` resolve → `*types.Const`/`*types.Var` | `references` | |

**도구**: `pkg.TypesInfo.{ObjectOf, Uses, Defs, Selections, Implements}` 활용. 실패 시 `PendingRef` + AMBIGUOUS.

#### 4.6.2 TypeScript / JavaScript — tree-sitter

**Parser 초기화**:

```go
parser := sitter.NewParser()
switch ext {
case ".ts", ".tsx":
    parser.SetLanguage(typescript.GetLanguage())
case ".js", ".jsx", ".mjs", ".cjs":
    parser.SetLanguage(javascript.GetLanguage())
}
tree := parser.Parse(nil, src)
```

**Tree-sitter query → 노드 매핑**:

| Query 패턴 | 노드 type |
|---|---|
| `(class_declaration name: (type_identifier) @name)` | `Class` |
| `(interface_declaration name: (type_identifier) @name)` | `Interface` |
| `(type_alias_declaration name: (type_identifier) @name)` | `TypeAlias` |
| `(enum_declaration name: (identifier) @name)` | `Enum` |
| `(function_declaration name: (identifier) @name)` | `Function` |
| `(method_definition name: (property_identifier) @name)` | `Method` |
| `(public_field_definition name: (property_identifier) @name)` | `Field` |
| `(formal_parameters (required_parameter pattern: (identifier) @name))` | `Parameter` |
| `(lexical_declaration (variable_declarator name: (identifier) @name))` | `LocalVariable` (function 내부) 또는 `Variable` (module-level) |
| `(import_statement source: (string) @path)` | `Import` |
| `(export_statement)` | `Export` |
| `(decorator (call_expression function: (identifier) @name))` | `Decorator` |
| `(if_statement)` | `IfStmt` |
| `(for_statement)` / `(for_in_statement)` / `(for_of_statement)` | `LoopStmt{sub_kind: for/for_in/for_of}` |
| `(while_statement)` | `LoopStmt{sub_kind: "while"}` |
| `(switch_statement)` | `SwitchStmt` |
| `(return_statement)` | `ReturnStmt` |

**엣지 매핑**:

| 트리거 | Edge | Resolution |
|---|---|---|
| `(call_expression function: ...)` | `calls` (cross-file: INFERRED) | symbol table 이름 매치 |
| Imported symbol 사용 | `references` | Import 노드 경유 |
| `(member_expression object: ... property: ...)` 읽기 | `reads_field` | name heuristic |
| Member assignment | `writes_field` | |
| `(new_expression constructor: ...)` | `instantiates` | |
| `(class_heritage (extends_clause ...))` | `extends` | |
| `(class_heritage (implements_clause ...))` | `implements` (declared, EXTRACTED) | |
| `(decorator ...)` 적용 | `has_decorator` | |
| `(export_statement ...)` | `exports` | |

**Cross-file resolution 휴리스틱** (V0):
- Import statement에서 module path → 동일 build root 안의 매칭 파일 찾기 (`./vault` → `<dir>/vault.ts`)
- `tsconfig.json` 의 `paths` map만 read-only로 파싱 (compilation은 안 함)
- Symbol name 매치는 *이름 동일성* 만 (signature 매칭은 V1+)
- 못 찾으면 `PendingRef` → AMBIGUOUS

**ABI artifact 추적** (Pass 3 cross-lang 준비):
- `import { Foo } from './contracts/Foo'` 또는 `from 'typechain'`/`from 'typechain-types'` 패턴 감지 → 임시 마킹
- Pass 3에서 Sol Contract 이름 ↔ TS Class 이름 매칭 후보로 사용

#### 4.6.3 Solidity — tree-sitter

**Parser 초기화**:

```go
parser := sitter.NewParser()
parser.SetLanguage(solidity.GetLanguage())
tree := parser.Parse(nil, src)
```

**Tree-sitter query → 노드 매핑**:

| Query 패턴 | 노드 type |
|---|---|
| `(contract_declaration name: (identifier) @name)` | `Contract` |
| `(function_definition name: (identifier) @name)` | `Function` (file-level) 또는 `Method` (Contract 안) |
| `(modifier_definition name: (identifier) @name)` | `Modifier` |
| `(constructor_definition)` | `Constructor` |
| `(event_definition name: (identifier) @name)` | `Event` |
| `(struct_declaration name: (identifier) @name)` | `Struct` |
| `(enum_declaration name: (identifier) @name)` | `Enum` |
| `(state_variable_declaration name: (identifier) @name)` | `Field` (Contract 멤버) |
| `(state_variable_declaration type: (mapping_type ...))` | + `Mapping` 노드 |
| `(parameter name: (identifier) @name)` | `Parameter` |
| `(if_statement)` / `(for_statement)` / `(while_statement)` / `(return_statement)` / etc | LogicBlock 5종 (Go/TS와 동일) |

**엣지 매핑**:

| 트리거 | Edge |
|---|---|
| `(call_expression function: ...)` | `calls` |
| `(emit_statement name: (identifier) @event)` | `emits_event` |
| `(modifier_invocation name: (identifier) @mod)` (function 시그니처 부착) | `has_modifier` |
| `(member_expression object: <mapping_var> ...)` 읽기 | `reads_mapping` |
| `(assignment_expression left: (member_expression object: <mapping_var>))` | `writes_mapping` |
| `(state_variable_declaration ...)` 사용 (read/write) | `reads_field` / `writes_field` |
| Inheritance (`is`) | `extends` (Sol 다중 상속 지원) |

**ABI 추출 부산물** (cross-lang link 용):
- 각 `Contract` 노드 빌드 시 함수 시그니처 list (`function name(types) returns(types)`) 를 별도 메모리 테이블 `solABI[ContractName] = []FunctionSig` 에 저장
- Pass 3에서 TS Class 매칭 시 사용

### 4.7 Extraction Pass 모델

총 4-pass.

| # | Pass | 단위 | 병렬성 | 책임 |
|---|---|---|---|---|
| 1 | **Structural** | per-file | 병렬 (worker pool) | AST 방문 → file-local Nodes/Edges 발행. cross-file ref은 `PendingRef` placeholder |
| 2 | **Resolution** | per-language | sequential (언어별) | symbol table 빌드 → `PendingRef` 를 실제 node ID로 resolve. 실패 시 AMBIGUOUS edge 발행 |
| 3 | **Cross-language linking** | global | sequential | V0 = Sol Contract ABI 테이블 ↔ TS Class 이름/메서드 매칭 → `binds_to` (INFERRED) |
| 4 | **Validation** | global | sequential | cross-ref 검사 (모든 edge.src/dst가 nodes에 존재) + schema validator |

Pass 1 결과는 메모리에 누적, Pass 2 진입 시 언어별 grouping. Pass 3는 결과 graph를 받아 양 언어 노드 간 추가 edge만 발행. **Pass 4 실패는 build 전체 fail-fast** — graphify의 *조용한 누락* 패턴을 정확히 차단 (§11.2 참조).

### 4.8 Confidence 부여 룰

모든 노드와 엣지는 `confidence ∈ {EXTRACTED, INFERRED, AMBIGUOUS}` 를 갖는다.

| 패턴 | Confidence | 이유 |
|---|---|---|
| 직접 AST 선언 (Function, Struct, Class, Contract, Variable, ...) | EXTRACTED | source에 명시적으로 존재 |
| Go `go/types` 로 resolved call/use_type/instantiates | EXTRACTED | type checker 검증 |
| Go interface dispatch (`go/types.Implements(T, I)`) | INFERRED | 정적 추론 |
| Go embedding 으로 인한 implicit method promotion | INFERRED | |
| TS `implements` (declared in source) | EXTRACTED | 명시적 |
| TS cross-file 이름 휴리스틱 매칭 | INFERRED | tsc 없이 추론 |
| TS dynamic dispatch (`obj.method()`) | INFERRED | |
| Sol → TS ABI 매칭 (name + arity heuristic) | INFERRED | 휴리스틱 |
| Unresolved import 타겟 | AMBIGUOUS | 외부 모듈 또는 누락 |
| Symbol 참조됐지만 def 못 찾음 | AMBIGUOUS | 외부 또는 dead code |
| Cluster Leiden 결과 (`topic_label` 자체) | INFERRED | 통계적 |

Viewer는 confidence 별 alpha 채도로 표시 (EXTRACTED=1.0 / INFERRED=0.7 / AMBIGUOUS=0.4 — §7.3). MCP 응답은 confidence를 그대로 노출 (LLM이 신뢰도 판단).

---

## 5. Graph Schema

V0 schema_version: **1.0**.

### 5.1 Node Types (29개)

| # | Type | Lang | Viewer 도형 | sub_kind |
|---|---|---|---|---|
| 1 | `Package` | all | sphere (대) | — |
| 2 | `File` | all | hex prism | — |
| 3 | `Struct` | go, sol | cube | — |
| 4 | `Interface` | go, ts | torus | — |
| 5 | `Class` | ts | cylinder | — |
| 6 | `TypeAlias` | go, ts | diamond | — |
| 7 | `Enum` | ts, sol | pyramid | — |
| 8 | `Contract` | sol | star (5각) | — |
| 9 | `Mapping` | sol | donut | — |
| 10 | `Event` | sol | starburst | — |
| 11 | `Function` | all | cone (대) | — |
| 12 | `Method` | go, ts, sol | cone (소) | — |
| 13 | `Modifier` | sol | tetrahedron | — |
| 14 | `Constructor` | ts, sol | cone (특수) | — |
| 15 | `Constant` | all | sphere (소, 단단) | — |
| 16 | `Variable` | all | cube (소) | — |
| 17 | `Field` | go, ts, sol | flat cube | — |
| 18 | `Parameter` | all | small flat cube | — |
| 19 | `LocalVariable` | all | tiny cube | — |
| 20 | `Import` | all | flat ring | — |
| 21 | `Export` | ts | flat ring (역 화살표) | — |
| 22 | `Decorator` | ts | small ring (스파이크) | — |
| 23 | `Goroutine` | go | branched cone | — |
| 24 | `Channel` | go | pipe (cyl + 화살표) | — |
| 25 | `IfStmt` | all | plane | (sub_kind 없음) |
| 26 | `LoopStmt` | all | plane | `for` / `while` / `range` |
| 27 | `CallSite` | all | plane | (sub_kind 없음) |
| 28 | `ReturnStmt` | all | plane | — |
| 29 | `SwitchStmt` | all | plane | — |

> Note: 함수-내부 logic 은 5개 별도 type 으로 분리됨 (IfStmt / LoopStmt / CallSite / ReturnStmt / SwitchStmt). `LoopStmt` 만 sub_kind로 `for`/`while`/`range` 구분.

### 5.2 Edge Types (22개)

| # | Type | 의미 | Lang | 신뢰도 기본 |
|---|---|---|---|---|
| 1 | `contains` | pkg_tree 부모→자식 | all | EXTRACTED |
| 2 | `defines` | file→type/func, type→method | all | EXTRACTED |
| 3 | `calls` | static call (resolved) | all | EXTRACTED |
| 4 | `invokes` | dynamic dispatch (interface) | go, ts | INFERRED |
| 5 | `uses_type` | param/return/local의 타입 | all | EXTRACTED |
| 6 | `instantiates` | `&Foo{}` / `new Foo()` | all | EXTRACTED |
| 7 | `references` | const/var 참조 | all | EXTRACTED |
| 8 | `reads_field` | field 읽기 | all | EXTRACTED |
| 9 | `writes_field` | field 쓰기 | all | EXTRACTED |
| 10 | `imports` | file → package | all | EXTRACTED |
| 11 | `exports` | file → symbol | ts | EXTRACTED |
| 12 | `implements` | struct/class → interface | go, ts | INFERRED (Go 구조적) |
| 13 | `extends` | class extends, struct embedding, Sol 상속 (`is`) | go, ts, sol | EXTRACTED |
| 14 | `has_modifier` | function → modifier | sol | EXTRACTED |
| 15 | `emits_event` | function → event | sol | EXTRACTED |
| 16 | `reads_mapping` | function → mapping | sol | EXTRACTED |
| 17 | `writes_mapping` | function → mapping | sol | EXTRACTED |
| 18 | `has_decorator` | symbol → decorator | ts | EXTRACTED |
| 19 | `spawns` | function → goroutine | go | EXTRACTED |
| 20 | `sends_to` | function → channel (`ch <- v`) | go | EXTRACTED |
| 21 | `recvs_from` | function → channel (`<-ch`) | go | EXTRACTED |
| 22 | `binds_to` ★ | Sol contract ↔ TS class (ABI 매칭) | cross-lang | INFERRED |

### 5.3 Node Attributes

```go
type Node struct {
    ID            string  // sha256(qname+lang+start_byte)[:16]
    Type          NodeType
    Name          string  // 짧은 이름 (e.g., "Hash")
    QualifiedName string  // FQN (e.g., "core/types.Block.Hash")
    FilePath      string  // build root 기준 상대 경로
    StartLine     int
    EndLine       int
    StartByte     int     // source slice 용
    EndByte       int
    Language      string  // "go" | "ts" | "sol"
    Visibility    string  // "exported" | "private"
    Signature     string  // callable의 params + return
    DocComment    string  // godoc / JSDoc
    Complexity    int     // cyclomatic (callable만)
    InDegree      int     // 빌드 후 계산
    OutDegree     int
    PageRank      float64
    UsageScore    float64 // 누적 외부 호출 = sum(incoming_calls.count)
    Confidence    string  // "EXTRACTED" | "INFERRED" | "AMBIGUOUS"
    SubKind       string  // LoopStmt의 "for"/"while"/"range" 등
}
```

### 5.4 Hierarchy

두 별도 hierarchy 동시 저장:

- **`pkg_tree`**: deterministic, package-tree backbone, 5 levels (L0 pkg → L1 subpkg → L2 file → L3 type/func/var → L4 LogicBlock)
- **`topic_tree`**: Leiden community, multi-resolution (3 levels), label = "공통 substring + top-PageRank + dominant pkg"

Viewer는 토글로 둘 중 하나의 hierarchy 만 활성화. MCP는 둘 다 쿼리 가능. Leiden 알고리즘 상세는 **§5.5** 참조.

### 5.5 Clustering Algorithm — Leiden (자체 구현)

`topic_tree` 생성에 사용하는 community detection 알고리즘.

#### 5.5.1 알고리즘 선택 — Leiden

**Leiden** (Traag, Waltman, van Eck, *Scientific Reports* 9, 2019): Louvain (2008) 의 개선판. **well-connected community 보장** + 더 빠른 수렴. 현재 community detection 의 de-facto 표준.

**선택 이유**:
- Modularity 기반 (직관적 + 잘 검증됨)
- 결정성: random seed 고정 시 동일 input → 동일 output
- 멀티-resolution 지원 (γ 파라미터로 community 단위 조절)
- 비수렴 매우 드묾 + 발생해도 fallback 명확 (§11.2 참조)

**대안 (V0에서 채택 안 함)**:
- Louvain: 일부 community가 disconnected 되는 알고리즘 결함 — Leiden이 정확히 이걸 해결
- Label propagation: 매번 다른 결과 (불안정) — eval 재현성 X
- Spectral / SBM: 큰 그래프에서 느림

#### 5.5.2 구현 전략 — Python `leidenalg` 분석 → Go 포팅

**Reference 구현**:
1. **Python `leidenalg`** (https://github.com/vtraag/leidenalg) — 논문 저자(Traag) 본인 작. C++ 코어 + Python wrapper. **결과 검증용 reference (test fixture cross-check 에 사용)**.
2. **Java `CWTSLeiden/networkanalysis`** (https://github.com/CWTSLeiden/networkanalysis) — 같은 저자가 만든 read-friendly Java 구현. **알고리즘 포팅 reference (가독성 좋아 Go 포팅 베이스로 활용)**.

**Go 포팅 가이드라인**:
- Java 구현의 `Leiden.java` + `CPMClusteringAlgorithm.java` + `Network.java` 를 1:1 매핑 → Go struct + method
- Modularity / CPM (Constant Potts Model) quality 함수 둘 중 modularity 사용 (단순 + 일반적)
- Worker pool로 노드별 community move 평가 병렬화 (Go의 goroutine 활용 — Java 대비 ergonomic 우위)
- 예상 분량: ~400~600 LOC (`internal/cluster/leiden.go` + tests)

#### 5.5.3 파라미터

| 항목 | 값 | 비고 |
|---|---|---|
| Quality function | Modularity | `Q = sum_c (e_c/m - (k_c/2m)^2)` |
| Resolution levels | 3개: γ ∈ {0.5, 1.0, 2.0} | 3-level multi-resolution hierarchy |
| Random seed | `42` (고정) | 결정성 보장 |
| Max iterations | 100 | 거의 모든 경우 < 30 회 수렴 |
| Convergence threshold | ΔQ < 1e-6 | |
| Fallback | 100 iter 내 비수렴 시 pkg_tree only | manifest에 `clustering_status='pkg_only'` |

#### 5.5.4 Topic Naming 휴리스틱

각 community 에 사람이 읽을 수 있는 label 부여 (LLM-free):

```
algorithm name_topic(community):
  pkg_paths   = [node.qualified_name.package_part for node in community]
  dominant_pkg = mode(pkg_paths)                       # 가장 많이 등장하는 pkg

  names       = [node.name for node in community]
  prefix      = longest_common_prefix(names) or longest_common_substring(names, min_count=3)
                                                       # 예: "validate", "tx_pool"
  if prefix and len(prefix) >= 3:
    pattern = prefix + "*"
  else:
    pattern = ""

  top3_pr = top_k(community, key=node.pagerank, k=3)
  top1_name = top3_pr[0].name

  return f"{dominant_pkg} — {pattern} + {top1_name}".strip(" —+")
```

**예시**:
- `consensus/clique — validate* + AuthorizeSigner`
- `core/types — block* + Header`
- `txpool — addLocal + addRemote`  (prefix 짧으면 top-1 만 + 다른 강한 노드)

빈약한 라벨 (예: 모든 노드 이름이 너무 다양) 의 경우 fallback `<dominant_pkg> [N nodes]`.

#### 5.5.5 학습

알고리즘 자체에 대한 깊이 있는 학습은 별도 문서 `docs/STUDY-GUIDE.md` §1 참조 (Leiden paper 링크 + Stanford 강의 + 실습 가이드 포함).

---

## 6. Storage Format

### 6.1 SQLite 결정

**Driver: `modernc.org/sqlite` (CGO-free)** ★

이유:
- 멀티 OS 단일 바이너리 요구사항 충족 (`GOOS=windows GOARCH=amd64 go build` 무손실)
- 약 3배 느리지만 V0 prototype 빌드 시간 < 10분 목표면 무관
- V1에서 빌드 시간이 안 맞으면 `mattn/go-sqlite3` (CGO) 옵션 build tag 추가 가능

### 6.2 Source Blob 저장 결정 ★

**노드별 source slice를 SQLite `blobs` 테이블에 저장** (offset만 X).

이유:
- `ckg export-static` 후 다른 머신/CI 에 복사해도 viewer/MCP가 작동 (eval 재현성)
- 사용자가 src를 이동/수정해도 graph가 stale-but-consistent 상태 유지 (manifest의 `src_commit`으로 staleness 표시)
- 추정 크기: 371K LOC ≈ 50~150MB DB로 SSD에서는 무관

### 6.3 SQLite Schema (V0)

```sql
CREATE TABLE nodes (
  id            TEXT PRIMARY KEY,
  type          TEXT NOT NULL,
  name          TEXT NOT NULL,
  qualified_name TEXT NOT NULL,
  file_path     TEXT NOT NULL,
  start_line    INTEGER NOT NULL,
  end_line      INTEGER NOT NULL,
  start_byte    INTEGER NOT NULL,
  end_byte      INTEGER NOT NULL,
  language      TEXT NOT NULL,
  visibility    TEXT,
  signature     TEXT,
  doc_comment   TEXT,
  complexity    INTEGER,
  in_degree     INTEGER DEFAULT 0,
  out_degree    INTEGER DEFAULT 0,
  pagerank      REAL DEFAULT 0,
  usage_score   REAL DEFAULT 0,
  confidence    TEXT NOT NULL DEFAULT 'EXTRACTED',
  sub_kind      TEXT
);
CREATE INDEX idx_nodes_qname ON nodes(qualified_name);
CREATE INDEX idx_nodes_file  ON nodes(file_path);
CREATE INDEX idx_nodes_type  ON nodes(type);

CREATE TABLE edges (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  src         TEXT NOT NULL REFERENCES nodes(id),
  dst         TEXT NOT NULL REFERENCES nodes(id),
  type        TEXT NOT NULL,
  file_path   TEXT,
  line        INTEGER,
  count       INTEGER NOT NULL DEFAULT 1,
  confidence  TEXT NOT NULL DEFAULT 'EXTRACTED'
);
CREATE INDEX idx_edges_src  ON edges(src);
CREATE INDEX idx_edges_dst  ON edges(dst);
CREATE INDEX idx_edges_type ON edges(type);

CREATE TABLE pkg_tree (
  parent_id  TEXT NOT NULL REFERENCES nodes(id),
  child_id   TEXT NOT NULL REFERENCES nodes(id),
  level      INTEGER NOT NULL,  -- 0..4
  PRIMARY KEY (parent_id, child_id)
);
CREATE INDEX idx_pkg_parent ON pkg_tree(parent_id);

CREATE TABLE topic_tree (
  parent_id    TEXT,                -- NULL = root
  child_id     TEXT NOT NULL REFERENCES nodes(id),
  resolution   INTEGER NOT NULL,    -- 0..2
  topic_label  TEXT,
  PRIMARY KEY (parent_id, child_id, resolution)
);

CREATE TABLE blobs (
  node_id  TEXT PRIMARY KEY REFERENCES nodes(id),
  source   BLOB NOT NULL  -- 원본 source slice (start_byte..end_byte)
);

CREATE VIRTUAL TABLE nodes_fts USING fts5(
  name, qualified_name, signature, doc_comment,
  content='nodes', content_rowid='rowid'
);

CREATE TABLE manifest (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- {schema_version, ckg_version, build_ts, src_root, src_commit,
--  lang_breakdown_json, stats_json, ckgignore_json,
--  parse_errors_count, unresolved_refs_count, clustering_status}
```

### 6.4 manifest.json (sibling, human-readable)

```json
{
  "schema_version": "1.0",
  "ckg_version": "0.1.0",
  "build_timestamp": "2026-04-23T12:00:00Z",
  "src_root": "/Users/.../go-stablenet-latest",
  "src_commit": "abc123",
  "languages": { "go": 1300, "ts": 0, "sol": 0 },
  "stats": {
    "nodes": 23456,
    "edges": 87654,
    "pkg_tree_depth": 5,
    "topic_clusters": 47,
    "parse_errors_count": 2,
    "unresolved_refs_count": 134,
    "clustering_status": "ok"
  },
  "ckgignore": ["vendor/", "node_modules/", "..."]
}
```

### 6.5 Staleness Detection (graph 가 src 변경 후 outdated 일 때)

#### 6.5.1 문제 정의

`ckg build` 시점 (T1) 의 source 상태를 graph가 capture. 이후 사용자가 src 를 수정 (T2) 하면 graph 는 *과거 상태*. Viewer / MCP / eval이 outdated 정보를 응답하면:

| 상황 | 영향 |
|---|---|
| Eval 측정 중 src 변경 | LLM이 *삭제된 함수* 본문 받음 → 측정 무효 |
| 일상 개발 중 | 잘못된 코드 위치 안내 → 혼란 |
| Demo/공유 | viewer가 없어진 함수 표시 → 신뢰도 ↓ |
| 장기 세션 | build 후 며칠/주 경과 → 거의 모든 응답 outdated |

#### 6.5.2 V0 정책 — Level 1 (git-based) + 경고 only

**Build 시**:
```go
if isGitRepo(srcRoot) {
    commit, _ := gitRevParseHEAD(srcRoot)
    manifest.Set("src_commit", commit)
    manifest.Set("staleness_method", "git")
} else {
    sample := randomFiles(srcRoot, 5)
    sum := sumMTimes(sample)
    manifest.Set("src_mtime_sum", sum)
    manifest.Set("staleness_files", sample)
    manifest.Set("staleness_method", "mtime")
}
```

**Load 시 (viewer/mcp/eval 모두)**:
```go
switch manifest.staleness_method {
case "git":
    current := gitRevParseHEAD(manifest.src_root)
    stale = (current != manifest.src_commit)
case "mtime":
    current := sumMTimes(manifest.staleness_files)
    stale = (current != manifest.src_mtime_sum)
}
```

#### 6.5.3 Stale 시 표시 — 자동 rebuild 안 함, 사용자 결정

| Channel | 표시 |
|---|---|
| Viewer | 상단 노란 banner: "⚠️ Graph built from `abc123` but src is now at `def456`. Run `ckg build` to refresh." |
| MCP 응답 metadata | `{"graph_stale": true, "build_commit": "abc123", "current_commit": "def456"}` 필드 추가 — LLM이 인지 가능 |
| Eval 결과 row | `stale` 컬럼에 `true` 기록 — 분석 시 stale row 제외 옵션 |
| CLI (`ckg serve` startup) | stderr에 warning 1줄 |

> **Auto-rebuild 안 하는 이유**: V0 prototype 본질이 *통제된 측정*. 같은 graph 에서 반복 측정해야 가설 검증 신호가 깨끗. 자동 rebuild 는 *사용자가 인지하지 못한 상태에서 측정 변인이 바뀌는* 위험을 만듦. V1+ 에서 `--auto-rebuild` 옵션 도입 검토.

### 6.6 Chunked JSON Export (`ckg export-static`)

```
DIR/
├── index.html              (임베디드 viewer entry)
├── assets/
│   ├── viewer.js
│   └── viewer.css
├── manifest.json           (DB manifest와 동일)
├── nodes/
│   ├── chunk_0000.json     (5K 노드/chunk, attribute 전체 포함)
│   └── chunk_NNNN.json
├── edges/
│   └── chunk_NNNN.json     (10K 엣지/chunk)
├── hierarchy/
│   ├── pkg_tree.json
│   └── topic_tree.json
└── blobs/
    └── <node_id>.txt       (per-node source, lazy fetch)
```

Viewer는 `fetch('./manifest.json')` → `fetch('./hierarchy/pkg_tree.json')`로 L0 시작. Zoom/expand 시 `./nodes/chunk_NNNN.json` 또는 `./blobs/<id>.txt` lazy fetch. `serve` 모드에서는 `/api/*`로 동일 의미의 SQLite-backed JSON endpoint를 제공해 viewer 코드는 동일.

---

## 7. Viewer

### 7.1 기술 스택

| Layer | 선택 | 이유 |
|---|---|---|
| 3D 렌더 | `3d-force-graph` (Vasco Asturiano, MIT) | Three.js 기반, **fold/unfold/visibility accessor 내장** — LOD 1급 시민 |
| UI framework | Vanilla JS + lit-html | React 번들(~150KB) 회피, 임베디드 viewer 가벼움 우선 |
| 빌드 | esbuild (Go에서 invoke) | `embed.FS`에 포함, Node.js 런타임 의존성 0 |
| 통신 | `serve` 모드: HTTP `/api/*` (SQLite-backed) / `export-static` 모드: chunked JSON `fetch` | 같은 viewer 코드, transport만 swap |

### 7.2 LOD 인터랙션 모델

| 동작 | 트리거 | 결과 |
|---|---|---|
| 초기 view | 페이지 로드 | L0: 패키지 super-node ~30개 force-directed layout |
| Auto-expand | 줌 ≥1.5× | L1 (선택 패키지의 서브패키지) lazy fetch + 추가 |
| Auto-expand | 줌 ≥3× | L2 (선택 서브패키지의 파일) |
| Auto-expand | 줌 ≥6× | L3 (파일 내 type/func/var) |
| Manual-only | 함수 노드 클릭 → "Inspect logic" | L4 (IfStmt/LoopStmt/CallSite/ReturnStmt/SwitchStmt) |
| Click expand | 단일 super-node 클릭 | 자식만 펼침 (형제 super-node 유지) |
| Drill | 더블클릭 | 그 subtree만 보이고 바깥 hide ("focus mode") |
| Collapse | 우클릭 | 자식 hide, super-node로 복귀 |
| Hierarchy 토글 | 상단 토글 | `pkg_tree ⇄ topic_tree` (Leiden overlay) |

LOD 구현 핵심: `3d-force-graph`의 `nodeVisibility(node => node.depth <= currentLOD)` accessor. 데이터 set은 그대로, 가시성만 토글 → relayout 없이 즉각 반응.

### 7.3 Visual Encoding (29 types)

**8개 기본 primitive** × **3개 언어 색상** × **glyph overlay** 조합:

| Primitive | 적용 type |
|---|---|
| Sphere (대) | Package |
| Sphere (소, 단단) | Constant |
| Hex prism | File |
| Cube | Struct, Variable, Field, Parameter, LocalVariable (크기로 구분) |
| Cone | Function, Method, Constructor (크기·뾰족함 정도로 구분) |
| Cylinder | Class, Channel (Channel은 화살표 cap) |
| Torus / Ring | Interface, Import, Export, Decorator |
| Special (pyramid/diamond/star/donut/branched/plane) | Enum, TypeAlias, Contract, Mapping, Goroutine, Modifier, Event, 5×LogicBlock (plane + sub-kind glyph) |

- **Color**: 언어 (Go=#00ADD8 / TS=#3178C6 / Sol=#3C3C3D / cross-lang link 노드=#FFD700 금색)
- **Size**: `node_size = base × log10(usage_score + 1)`, clip 0.5×~3.5×
- **Alpha**: confidence (EXTRACTED=1.0 / INFERRED=0.7 / AMBIGUOUS=0.4)

**Edge style**:

| Edge type group | Style |
|---|---|
| `calls`, `invokes` | solid line, arrow |
| `uses_type`, `instantiates`, `references` | dashed, arrow |
| `imports`, `exports` | dotted, arrow |
| `implements`, `extends` | double line |
| Sol-specific (`emits_event`, `reads/writes_mapping`, `has_modifier`) | colored Sol purple |
| Go-specific (`spawns`, `sends_to`, `recvs_from`) | colored Go cyan |
| `binds_to` (cross-lang) | gold thick |
| `contains` (pkg_tree) | **숨김** (구조이지 의미가 아님) |

### 7.4 UI 구성

```
┌─────────────────────────────────────────────────────┐
│ ckg viewer | go-stablenet-latest | search [_____]  │ ← top bar
├──────────────────────────┬──────────────────────────┤
│                          │  Selection Panel         │
│                          │  ─────────────────       │
│                          │  Type: Function          │
│      [3D canvas]         │  Name: ProcessTx         │
│                          │  Qualified: core/...     │
│   force-directed layout  │  Signature: ...          │
│                          │  Usage score: 432        │
│                          │  Confidence: EXTRACTED   │
│                          │                          │
│                          │  Source preview:         │
│                          │  ...                     │
│                          │                          │
│                          │  In edges: 12 calls      │
│                          │  Out edges: 5 calls      │
├──────────────────────────┴──────────────────────────┤
│ [pkg-tree] [topic] | LOD: L2 | filter: [✓ Go ...]  │ ← bottom bar
└─────────────────────────────────────────────────────┘
```

- **Search**: top-bar input → SQLite FTS5 (`serve` 모드) 또는 chunked JSON에서 in-memory FTS 미니 인덱스 (`export-static` 모드) → 결과 list 클릭 → 해당 노드로 카메라 이동 + 조상 자동 expand
- **Filter**: 언어 토글 (Go/TS/Sol) / Confidence (EXTRACTED only / 모두) / 노드 타입 그룹

### 7.5 성능 가드

- 단일 view에 가시 노드 > 5K → 도형을 simple sphere impostor로 강등 + edge 수 cap 10K
- L4 노드는 "최대 1개 함수의 logic만" 동시 표시
- Layout iteration cap: 60fps 유지 위해 시뮬레이션 step 200회 후 freeze (drag 시 unfreeze)

---

## 8. MCP Server

### 8.1 What is MCP & Why CKG uses it

**MCP (Model Context Protocol)**: Anthropic 이 2024년 발표한 LLM 클라이언트와 외부 도구/데이터 소스를 잇는 표준 프로토콜. 비유하자면 *LLM 을 위한 USB-C* — Claude Code/Desktop, Cursor, Continue 등 모든 호환 클라이언트가 같은 방식으로 어떤 MCP 서버든 연결한다.

**서버가 노출하는 것**: tools (호출 가능한 함수) + resources (읽기 가능한 데이터). 클라이언트의 LLM 이 자연어 대화 중 자동으로 적절한 tool을 호출 → 결과를 받아 응답에 활용.

**아키텍처**:

```
┌─────────────────┐                    ┌────────────────────┐
│  LLM Client     │                    │  MCP Server        │
│  (Claude Code)  │                    │  (ckg mcp)         │
│                 │ ◀── stdin/stdout ─▶│                    │
│                 │      JSON-RPC      │  6개 도구 노출        │
│                 │                    │  graph.db read-only│
└─────────────────┘                    └─────────┬──────────┘
                                                  ▼
                                            ┌──────────┐
                                            │ graph.db │
                                            └──────────┘
```

**왜 CKG 가 MCP 를 쓰는가** — *가설 검증의 인프라*:

CKG 본 가설은 "graph 컨텍스트가 raw-file 대비 LLM 토큰 효율 + 정확도가 더 높다" 임 (§2). 측정하려면 동일 task 를 4 baseline (raw / graph-dump / granular / smart 1-shot) 에 자동으로 흘려보내야 한다. MCP 가 *graph → LLM 으로 컨텍스트를 자동 주입하는 채널* 역할이다. 사람이 매번 복붙하면 측정 자체가 불가능.

**일상 사용 시나리오**:

1. 사용자가 Claude Code 에서 한 번만 등록: `claude mcp add ckg --command "ckg" --args "mcp,--graph=/tmp/ckg-real"`
2. 사용자: "consensus/clique 에 새 validator 함수 추가해줘"
3. Claude (내부): `ckg.get_context_for_task(...)` 자동 호출
4. ckg 가 graph 분석 → 관련 30 노드 + 상위 5 source body → JSON 반환
5. Claude: 받은 컨텍스트만으로 코드 작성 (raw 파일 다 안 읽음 → 토큰 절감 + 정확도 ↑)

MCP 자체에 대한 깊이 있는 학습은 별도 문서 `docs/STUDY-GUIDE.md` §3 참조 (공식 spec, Go SDK, 실습 가이드 포함).

### 8.2 Transport

V0: **stdio only** (Claude Code/Cursor가 spawn). HTTP+JWT는 V1 (CKS plan FD-004와 정합).

### 8.3 도구 6개

| # | Tool | 사용처 | 비고 |
|---|---|---|---|
| 1 | `get_context_for_task` ★ | δ-mode (smart 1-shot) | V0 핵심 도구 |
| 2 | `find_symbol` | δ + γ | exact lookup |
| 3 | `find_callers` | γ | 역방향 call graph |
| 4 | `find_callees` | γ | 순방향 call graph |
| 5 | `get_subgraph` | γ + β (whole-dump) | 명시적 subgraph fetch |
| 6 | `search_text` | γ | FTS5 raw |

> **Eval-driven 설계**: 6개 도구는 *모두 같은 SQLite만 read*. 가설 검증은 LLM에게 **어느 도구만 쓸 수 있는지**를 baseline마다 다르게 제한해서 측정 (system prompt + tool allowlist).
> - δ: `get_context_for_task` 1개만
> - γ: `find_*` + `search_text` (5개)
> - β: `get_subgraph(root, depth=99)` 1회
> - α: 도구 0개 (raw file dump)

### 8.4 핵심 도구 — `get_context_for_task`

**Input**:
```json
{
  "task_description": "Add transaction validation in the consensus layer to reject txs with invalid signatures",
  "budget_tokens": 8000,
  "language": "go",
  "include_blobs": true,
  "max_bodies": 5
}
```

**Algorithm**:

1. **Retrieve**: FTS5 BM25 → top 30 candidate nodes (name + qname + signature + doc_comment)
2. **Expand**: 각 candidate에서 1-hop traversal (`calls`, `uses_type`, `references`, `implements`) → ~150 nodes set
3. **Score**: `score = 0.5 * BM25_normalized + 0.3 * pagerank_normalized + 0.2 * usage_score_normalized`
4. **Diversify**: top-30 from set, max 3 per cluster (topic_tree resolution=1) → avoid one cluster dominance
5. **Pack**:
   - top-`max_bodies` (=5): full source blob 포함
   - 다음 ~15: signature + doc_comment + qname only
   - cluster overview: top-3 cluster의 topic_label + 노드 개수 요약
   - subgraph: 최종 set의 edges만 compact adjacency list로 포함
6. **Estimate tokens** (rough: chars/4) → over budget 이면 tail trim → return

**Output**:
```json
{
  "task_description": "...",
  "subgraph": {
    "nodes": [
      {"id": "abc", "name": "ValidateSig", "type": "Function",
       "qname": "consensus/clique.ValidateSig", "score": 0.82},
      "..."
    ],
    "edges": [["abc", "def", "calls"], "..."]
  },
  "bodies": [
    {"id": "abc", "qname": "consensus/clique.ValidateSig",
     "source": "func ValidateSig(...) error {...}"}
  ],
  "summaries": [
    {"id": "def", "qname": "core/types.Transaction.HashWithSig",
     "signature": "func ...", "doc": "..."}
  ],
  "cluster_overview": [
    {"topic": "consensus/clique — validator rotation [pkg]",
     "nodes": 47, "key_symbols": ["ValidateSig", "AuthorizeSigner"]}
  ],
  "tokens_estimated": 7234,
  "trimmed": false
}
```

### 8.5 보조 도구 시그니처

```
find_symbol(name: str, language?: str, exact?: bool=true) → {nodes: [...]}
find_callers(qname: str, depth: int=1) → {nodes: [...], edges: [...]}
find_callees(qname: str, depth: int=1) → {nodes: [...], edges: [...]}
get_subgraph(seed_qname: str, depth: int=2, edge_types?: [str]) → {nodes: [...], edges: [...]}
search_text(query: str, top_k: int=10, language?: str) → {nodes: [...], scores: [...]}
```

모두 `include_blobs` 옵션으로 source 본문 포함 여부 제어. 디폴트 false (γ-mode가 핑퐁 시 LLM이 명시 `include_blobs=true`를 지정해야 본문 받음 → 진짜 핑퐁 비용 측정).

### 8.6 구현 노트

- Go MCP SDK: `github.com/modelcontextprotocol/go-sdk` (또는 `mark3labs/mcp-go`)
- 단일 `*sql.DB` 인스턴스 read-only 모드 (`?mode=ro`)로 lock 없이 다중 도구 호출 동시 처리
- Logging: stderr 만 (stdout은 MCP 메시지 전용)

---

## 9. Eval Framework

### 9.1 Baselines (재정의)

| Baseline | LLM 컨텍스트 | MCP 도구 allowlist | 라운드트립 |
|---|---|---|---|
| α (raw) | task description + 지정된 raw source files | 없음 | 0턴 |
| β (graph dump) | task description + `get_subgraph(root, depth=99)` 1회 출력 | `get_subgraph` 1개 | 1턴 |
| γ (granular) | task description, 직접 도구 호출 | 5개 (find_*, get_subgraph, search_text) | 보통 3~6턴 |
| δ (smart 1-shot) ★ | task description + `get_context_for_task` 1회 출력 | `get_context_for_task` 1개 | 1턴 |

### 9.2 2-단계 corpus

**Phase 1 — 합성 corpus** (`testdata/synthetic/`)
- Go backend (~500 LOC, 3 packages: `api/`, `service/`, `domain/`)
- TS frontend (~300 LOC, 3 modules: `pages/`, `services/`, `contracts/`)
- Solidity contract (~150 LOC, 2 contracts: `Token.sol`, `Vault.sol`)
- 의도적 cross-lang link: `Vault.deposit()` ↔ TS `services/vault.ts` ↔ Go `service/vault_handler.go`
- **사람이 정답을 100% 알고 있음** → 정확도 자동 채점 가능

**Phase 2 — 실 corpus**
- `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` (1,300 files / 371K LOC, Go-only)
- 사람이 5~10 task 수동 작성 + 정답 set/rubric 도 수동 정의

### 9.3 Eval Task Spec (YAML)

```yaml
# eval/tasks/synthetic-T01-find-callers.yaml
id: T01
corpus: synthetic
description: "What functions in this codebase call Vault.deposit?"
expected_kind: symbol_set
expected:
  - "ts:services/vault.ts:depositFn"
  - "go:service/vault_handler.go:HandleDeposit"
scoring:
  type: precision_recall
  threshold:
    precision: 0.8
    recall: 0.8
```

```yaml
# eval/tasks/real-T07-impl-clique-validator.yaml
id: T07
corpus: real
corpus_path: /Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest
description: |
  Add a new function `AuthorizeSignerWithExpiry(addr common.Address, expireBlock uint64) error`
  to consensus/clique that follows the existing AuthorizeSigner pattern but adds expiration.
expected_kind: code_patch
expected:
  must_use_symbols:
    - "consensus/clique.Snapshot.signers"
    - "consensus/clique.Snapshot.lock"
  must_call:
    - "consensus/clique.Snapshot.apply"
  must_not_break_signature: true
scoring:
  type: rubric
  rubric:
    - "uses Snapshot mutex correctly (1pt)"
    - "validates input addr (1pt)"
    - "matches existing error wrapping pattern (1pt)"
    - "compiles with go vet clean (1pt, automated)"
```

### 9.4 Eval Runner (`ckg eval` subcommand)

```bash
ckg eval \
  --tasks=eval/tasks/*.yaml \
  --baselines=alpha,beta,gamma,delta \
  --graph=graph_out/ \
  --llm=claude-sonnet-4-6 \
  --out=eval/results/2026-04-23.csv
```

**흐름**:
1. Task load → corpus 결정
2. Baseline 별:
   - 시스템 프롬프트 + tool allowlist 구성
   - LLM 호출 (Anthropic API, prompt cache 활성)
   - 응답에서 token count 추출 (input + output, cache hits 분리)
   - 채점 (precision/recall 또는 rubric — 자동 가능한 항목만; 수동 항목은 reviewer 표시)
3. Result 행 append: `task_id, baseline, input_tokens, output_tokens, cached_tokens, score, latency_ms, num_tool_calls, raw_output`
4. 종합 분석 — task별 4 baseline 비교 + 평균 비교 chart

### 9.5 Result 분석 (자동 생성)

`eval/results/<date>/report.md` 자동 생성:
- 가설 H1 결과: δ 평균 input tokens vs α 평균 — 절감률 %
- 가설 H2 결과: δ score vs α score — regression 여부
- 가설 H3 결과: δ vs β/γ dominance 매트릭스
- Per-task 체크리스트 (실패 원인 hint 포함)

---

## 10. Repo / Module Layout

### 10.1 Repo 위치

`/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph` — 별도 standalone repo, `tools/` 산하. 이미 git init + LICENSE + initial commit 상태.

**모듈 경로**: `github.com/0xmhha/code-knowledge-graph` (FD-003 기존 GitHub user `0xmhha` 와 정합 — 실제 git remote 설정 시 변경 가능).

### 10.2 디렉토리 구조

```
code-knowledge-graph/
├── go.mod                      # module github.com/0xmhha/code-knowledge-graph
├── go.sum
├── README.md
├── LICENSE                     (existing)
├── Makefile
├── cmd/
│   └── ckg/
│       └── main.go             # 5 subcommands: build/serve/mcp/export-static/eval
├── internal/
│   ├── detect/                 # file walk + .ckgignore + 언어 분류
│   ├── parse/
│   │   ├── parser.go           # 디스패치 인터페이스
│   │   ├── go/                 # Go: go/parser + go/types
│   │   ├── ts/                 # TS: tree-sitter-typescript
│   │   └── sol/                # Sol: tree-sitter-solidity
│   ├── link/                   # cross-file resolver + cross-lang ABI binder
│   ├── graph/
│   │   ├── types.go            # Node, Edge, NodeType, EdgeType (29/22)
│   │   ├── builder.go          # in-memory graph build + dedup
│   │   └── validate.go         # cross-ref + schema validator
│   ├── cluster/
│   │   ├── pkg_tree.go         # deterministic package hierarchy
│   │   └── leiden.go           # Leiden community detection
│   ├── score/
│   │   ├── degree.go
│   │   ├── pagerank.go
│   │   └── usage.go            # 누적 외부 호출
│   ├── persist/
│   │   ├── schema.sql          # 임베디드 DDL
│   │   ├── sqlite.go           # modernc.org/sqlite
│   │   ├── manifest.go
│   │   └── chunked_export.go
│   ├── server/                 # ckg serve
│   │   ├── server.go
│   │   ├── api.go
│   │   └── viewer.go           # embed.FS for web/dist
│   ├── mcp/                    # ckg mcp
│   │   ├── server.go
│   │   └── tools/
│   │       ├── get_context_for_task.go ★
│   │       ├── find_symbol.go
│   │       ├── find_callers.go
│   │       ├── find_callees.go
│   │       ├── get_subgraph.go
│   │       └── search_text.go
│   └── eval/                   # ckg eval
│       ├── runner.go
│       ├── baseline.go
│       ├── llm.go              # Anthropic client wrapper
│       ├── score.go
│       └── report.go
├── pkg/                        # 외부 import 가능한 공개 API (V1 CKS용)
│   └── types/
│       ├── node.go
│       └── edge.go
├── web/
│   └── viewer/
│       ├── package.json
│       ├── esbuild.config.js
│       ├── src/
│       │   ├── main.js
│       │   ├── store.js
│       │   ├── layout.js       # 3d-force-graph 설정
│       │   ├── encoding.js     # 도형/색/glyph 매핑
│       │   ├── panel.js        # selection panel
│       │   ├── search.js
│       │   └── api.js
│       └── dist/               # esbuild 결과 (build 시 embed.FS로 흡수)
├── testdata/
│   └── synthetic/
│       ├── go-backend/
│       ├── ts-frontend/
│       └── sol-contract/
├── eval/
│   ├── tasks/                  # YAML task 정의
│   └── results/                # 실행 결과 CSV + report
├── docs/
│   ├── spec-ckg-v0-prototype.md  # 본 문서
│   ├── SCHEMA.md               # node/edge 29/22 reference
│   ├── ARCHITECTURE.md
│   └── EVAL.md                 # baseline + 채점 방법
└── scripts/
    └── build-viewer.sh
```

**파일 크기 가드**: 모든 `.go` 파일 ≤ 400 lines (`coding-style.md` SSoT 준수). 파서가 커질 위험이 큰 `internal/parse/<lang>/`는 type 단위로 split (예: `func.go`, `struct.go`, `block.go`).

### 10.3 Build 시스템

```makefile
.PHONY: all build viewer test eval-synthetic eval-real

all: build

viewer:
	cd web/viewer && npm install && node esbuild.config.js

build: viewer
	go build -o bin/ckg ./cmd/ckg

test:
	go test -race -coverprofile=coverage ./...

eval-synthetic:
	./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
	./bin/ckg eval --tasks=eval/tasks/synthetic-*.yaml --graph=/tmp/ckg-synth \
	  --baselines=alpha,beta,gamma,delta --out=eval/results/

eval-real:
	./bin/ckg build --src=$$STABLENET_PATH --out=/tmp/ckg-real
	./bin/ckg eval --tasks=eval/tasks/real-*.yaml --graph=/tmp/ckg-real \
	  --baselines=alpha,beta,gamma,delta --out=eval/results/
```

CGO-free 결과: `GOOS=darwin GOARCH=arm64 go build` / `GOOS=linux GOARCH=amd64` / `GOOS=windows GOARCH=amd64` 모두 cross-compile 가능 → 단일 release pipeline.

---

## 11. Error Handling

### 11.1 원칙

`coding-style.md` 준수 — 경계(boundary)에서 catch + 변환, 내부에서는 전파. 조용한 silent swallow 금지.

### 11.2 영역별

#### Build pipeline

| 실패 유형 | 동작 |
|---|---|
| 단일 파일 parse 실패 | stderr 로그 + skip + `parse_errors.json` 누적, build 자체는 계속 |
| 단일 파일 link 실패 (unresolved ref) | 해당 edge `confidence=AMBIGUOUS` 표시, build 계속 |
| Cluster Leiden 비수렴 | 패키지-tree backbone으로만 fallback, topic_tree 비활성, manifest에 `clustering_status='pkg_only'` |
| SQLite write 실패 | 즉시 fail-fast |
| Schema validation 실패 (V0 schema_version 불일치) | 즉시 fail-fast + 명확한 rebuild 안내 |
| Cross-ref 검증 실패 (edge가 nodes에 없는 id 참조) | 즉시 fail-fast — graphify가 *조용히 무시*해서 누락 생긴 케이스 정확히 차단 |

#### MCP 도구

| 실패 유형 | 동작 |
|---|---|
| Input JSON Schema 위반 | MCP error response (code=`InvalidParams`) |
| Token budget exceeded | `trimmed: true` marker + 가능한 만큼 응답 반환 |
| Unknown qname / symbol | 빈 결과 + `not_found: true` (MCP error 아님) |
| SQLite read 실패 | MCP error (`InternalError`) + stderr 로그 |
| Concurrent tool call 충돌 | SQLite read-only mode이므로 충돌 없음 |

#### HTTP API (`ckg serve`)

- 모든 핸들러는 boundary에서 panic recover + 5xx + structured log
- 4xx는 명시 에러 (`{"error": "...", "code": "..."}`)
- 5xx는 sanitize (스택 노출 금지)
- CORS: localhost only (V0 single-machine 사용)

#### Viewer

- API endpoint 실패 → toast notification + 부분 view 유지
- Malformed graph data → L0-only fallback (전체 crash 안 함)
- Web Console에 구조화 에러

### 11.3 Logging 표준

- 라이브러리: `log/slog` (Go 1.21+ stdlib) — 외부 의존성 0
- 기본 레벨: `INFO`, `--verbose=debug`로 토글
- 형식: text (사람) / `--log-format=json`으로 JSON 전환 (CI/eval)
- stderr 전용 (stdout은 MCP stdio와 conflict 방지)

---

## 12. Testing Strategy

| Layer | 도구 | 위치 | 커버리지 목표 | V0 필수 |
|---|---|---|---|---|
| Unit (pure logic) | `go test`, table-driven | `internal/<pkg>/*_test.go` | 80% | ✅ |
| Parser 정확도 | fixture + golden | `internal/parse/<lang>/testdata/` + `*_golden.json` | 90% | ✅ |
| Graph builder | dedup/cross-ref/validate | `internal/graph/*_test.go` | 90% | ✅ |
| Cluster 결정성 | seed 고정 후 동일 input → 동일 output 확인 | `internal/cluster/*_test.go` | 70% | ✅ |
| MCP 도구 | mock SQLite + per-tool table-driven | `internal/mcp/tools/*_test.go` | 모든 6 tool | ✅ |
| Eval runner | baseline switching + scoring | `internal/eval/*_test.go` | 70% | ✅ |
| Integration (E2E) | testdata/synthetic 전체 build → graph → MCP 호출 → 응답 검증 | `internal/e2e/synthetic_test.go` | 1 happy + 3 sad paths | ✅ |
| Viewer smoke | Playwright headless | `web/viewer/tests/smoke.spec.js` | 페이지 로드 + L0 노드 표시 | ✅ |
| Cross-OS build | CI matrix | `.github/workflows/build.yml` | darwin/linux/windows × amd64/arm64 | ✅ |
| Performance bench | `go test -bench` | `internal/bench/*_test.go` | 1회 run 후 baseline 기록 | ⚠️ V0 후반 |

### 12.1 Test Fixture 구조

```
testdata/
└── synthetic/                # 합성 corpus (eval에도 사용)
    ├── go-backend/
    ├── ts-frontend/
    └── sol-contract/

internal/parse/go/testdata/
├── struct.go                 # 입력 fixture
├── struct_golden.json        # 기대 출력 (graph 일부)
├── interface_embedded.go
├── interface_embedded_golden.json
└── ...
```

Golden file 패턴: 입력 .go 파일 옆에 `_golden.json`, 테스트는 `parse(file) == expected_json`. 변경 시 `-update` 플래그로 재생성.

### 12.2 CI

GitHub Actions matrix:
```yaml
strategy:
  matrix:
    os: [ubuntu-latest, macos-latest, windows-latest]
    arch: [amd64, arm64]
steps:
  - go vet ./...
  - staticcheck ./...
  - go test -race -coverprofile=coverage ./...
  - cd web/viewer && npm install && npm run build && npm run test:smoke
  - go build -o bin/ckg ./cmd/ckg
```

---

## 13. Out-of-Scope

V0가 의도적으로 *하지 않는 것* — scope creep 방어.

| # | 항목 | 이전 단계 |
|---|---|---|
| 1 | LLM 활용 빌드/네이밍/클러스터링 | V1 옵션 (Leiden topic_label 보정) |
| 2 | Vector DB / 임베딩 / hybrid retrieval | V1 CKS Layer 1 |
| 3 | 실시간 incremental update (file watcher + delta) | V1 |
| 4 | Auth / 멀티유저 | V1+ HTTP MCP 승격 시 (FD-004) |
| 5 | Cross-language linking 확장 (Go ↔ TS RPC, Go ↔ Sol) | V1 |
| 6 | More languages (Rust, Python, Java, C/C++) | V1+ (parser 추가만으로 OK) |
| 7 | HTTP MCP transport (JWT/Unix socket) | V1 (CKS Phase 1-β Group F 정합) |
| 8 | mTLS / Vault / signing / audit chain | V1+ CKS Phase 2~3 (FD-010/011/015) |
| 9 | Advanced edge analysis (data-flow, taint, slicing) | V1+ |
| 10 | Customizable schema (사용자 노드 타입 추가) | 검토 후 V1+ |
| 11 | Persistent viewer state (사용자 view 저장/복원) | V1+ |
| 12 | Comparison/diff between two graphs (commit A vs B) | V1+ |
| 13 | Monorepo 다중 root 동시 build | V1+ |
| 14 | Custom edge weight / scoring formula tweaking | V1+ |

---

## 14. V0 Definition of Done

- [ ] `ckg build --src=testdata/synthetic --out=/tmp/ckg-synth` 무에러 종료
- [ ] `ckg build --src=$STABLENET_PATH --out=/tmp/ckg-real` 무에러 종료, < 10분
- [ ] `ckg serve --graph=/tmp/ckg-real`로 viewer 정상 로드, L0~L4 fold/unfold 동작
- [ ] `ckg mcp --graph=/tmp/ckg-real`이 Claude Code에서 spawn 후 6개 도구 정상 응답
- [ ] `ckg eval --baselines=alpha,beta,gamma,delta` 합성 + 실 corpus 모두 완주
- [ ] Eval 보고서 자동 생성: H1/H2/H3 결과 명시
- [ ] CI 통과: 3 OS × 2 arch × test/build/smoke
- [ ] 모든 unit test 80%+ coverage
- [ ] `docs/SCHEMA.md`, `docs/ARCHITECTURE.md`, `docs/EVAL.md` 작성
- [ ] README.md 에 Quick Start 5분 안 됨

---

## 15. References

### 15.1 외부 프로젝트

- **graphify** (Python): `/Users/wm-it-22-00661/Work/github/study/ai/agent/graphify` — V0 설계의 출발점. 단순 파이프라인 + confidence label 패턴 차용. 142KB 단일 `extract.py` 구조는 회피 (per-language 모듈로 분리).

### 15.2 내부 프로젝트 — 미래 통합 대상

- **stablenet-ai-agent CKS plan**: `/Users/wm-it-22-00661/Work/github/stable-net/study/projects/stablenet-ai-agent/claudedocs/`
  - `plan-phase0-phase1-cks-mvp.md` (Phase 1-α plan, Go-only BM25+Graph+AST)
  - `plan-phase1-beta-mcp-vector-multilang.md` (Phase 1-β plan, MCP+Vector+Multi-lang)
  - `04-cks-deep-dive.md` (4-Layer CKS 설계)
  - `fixed/FD-003`, `FD-004`, `FD-007`, `FD-013` (모듈 경로, HTTP/MCP 결정, eval, MCP trust tier)

### 15.3 핵심 외부 라이브러리

| 용도 | 라이브러리 |
|---|---|
| SQLite | `modernc.org/sqlite` (CGO-free) |
| Tree-sitter (TS, Sol) | `github.com/smacker/go-tree-sitter` + `tree-sitter-typescript` / `tree-sitter-solidity` |
| MCP server | `github.com/modelcontextprotocol/go-sdk` (또는 `mark3labs/mcp-go`) |
| Logging | `log/slog` (stdlib, Go 1.21+) |
| Validation | `github.com/go-playground/validator/v10` |
| 3D 렌더 | `3d-force-graph` (Vasco Asturiano, MIT, npm) |
| HTML templating | `lit-html` (npm) |
| JS 빌드 | `esbuild` (npm + Go invoke) |
| Anthropic API | `github.com/anthropics/anthropic-sdk-go` |
| Test (Playwright) | `@playwright/test` (npm) |
| Bench plotting | `gonum.org/v1/plot` |

### 15.4 학습 자료 (별도 문서)

CKG 의 비자명한 외부 개념 (graph clustering / MCP / build-cache staleness 등) 에 대한 깊이 있는 학습 자료는 별도 문서로 분리:

- **`docs/STUDY-GUIDE.md`**: 본 spec 안에 반복되지 않는 *밖의* 지식 정리. 입문 → 중급 → 실습 → 심화 순. 스펙 작성·구현 시 매번 참조할 필요 X. 호기심 또는 V1+ 진화 결정 시 펼쳐 봄.

### 15.5 가중치 / 임계값 출처

| 값 | 출처 / 근거 |
|---|---|
| `get_context_for_task` scoring 0.5 BM25 + 0.3 PageRank + 0.2 usage | 직관 + V0 후반 ablation으로 조정 가능 |
| LOD 줌 임계값 1.5× / 3× / 6× | 사용자 인터랙션 직관 — 30 → 200 → 2K 노드 단계적 |
| Visible 노드 cap 5K | `3d-force-graph` 60fps 경험치 |
| Layout step cap 200 | `3d-force-graph` default + 사용자 drag 시 unfreeze |
| Chunked JSON 5K nodes / 10K edges per chunk | 1MB 미만 chunk 목표 (HTTP fetch 1회 ~수십ms) |

---

**End of Spec.**
