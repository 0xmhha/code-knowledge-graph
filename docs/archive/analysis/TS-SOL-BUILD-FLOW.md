# TypeScript / Solidity 파싱 + 그래프 변환 Flow 검토

> **대상**: `ckg build`가 .ts/.tsx/.js/.jsx, .sol 파일을 그래프 노드/엣지로 변환하는 흐름
> **참조 파일**:
> - `internal/parse/typescript/{parser,queries,declarations,resolve}.go`
> - `internal/parse/solidity/{parser,queries,declarations,resolve,abi}.go`
> - `internal/link/xlang.go`
> - `internal/buildpipe/language_runners.go` (`runTSPipeline`, `runSolPipeline`)
> - `internal/parse/solidity/binding/` (벤더링된 grammar)
>
> **선행 문서**: `docs/analysis/GO-PROJECT-BUILD-FLOW.md`
> **마지막 갱신**: 2026-05-05

> ⚠️ **Honest assessment**: TS/Sol 파서는 Go 파서 대비 **현저히 단순한 추출 모델**을 사용합니다. spec과 schema는 33 NodeType × 30 EdgeType을 표방하지만, **TS/Sol에서 실제 emit되는 종류는 매우 제한적**이며, 함수 호출 추적·cross-file 해석·동시성/분산 그래프 생성이 모두 빠져 있습니다. 본 문서는 이 gap을 § 4와 § 5에서 명시적으로 정리합니다.

---

## 목차

1. [진입 및 공통 구조](#1-진입-및-공통-구조)
2. [TypeScript Flow](#2-typescript-flow)
3. [Solidity Flow](#3-solidity-flow)
4. [Cross-Language Link (G5 binds_to)](#4-cross-language-link-g5-binds_to)
5. [현재 한계 / Gap (Critical)](#5-현재-한계--gap-critical)
6. [검증된 동작 vs 미구현](#6-검증된-동작-vs-미구현)
7. [근본 원인 후보 (디버깅 시 우선 검사)](#7-근본-원인-후보-디버깅-시-우선-검사)
8. [핵심 한 줄 요약](#8-핵심-한-줄-요약)

---

## 1. 진입 및 공통 구조

`runCold` (pipeline.go) → 언어별 dispatcher 호출:

```go
runTSPipeline(srcRoot, files.TS, log)   // .ts/.tsx/.js/.jsx/.mjs/.cjs
runSolPipeline(srcRoot, files.Sol, log) // .sol
```

**Go와 다른 점**:

| 항목 | Go | TS/Sol |
|---|---|---|
| Detection | `go/packages.Load` (build constraints) | `detect.Walk` (extension-only) |
| Pre-discovery typing | `detect.GoPackages(ModeTypes)` → typed file index | (없음) |
| Body walk | `ast.Walk` (전체 AST 순회) | tree-sitter **query** match only |
| Concurrency emit | Mutex/Channel/Goroutine 추출 | **추출 없음** |
| Distributed emit | HTTP/RPC/Endpoint 추출 | **추출 없음** |
| Cross-file resolve | qname suffix match (Function/Method) | **단순 name 일치** (TS) / event/modifier/mapping 명만 (Sol) |

**Discovery 단계 (`detect.Walk`)** — `walker.go`:
- 확장자 기반 walker
- `.ckgignore` 파일 honored (gitignore-like semantics)
- vendor/, node_modules/, .git/, testdata/ skip
- 결과: `FileLists{TS, Sol}` (Go는 별도 `detect.GoFiles`)

이 단계에서 이미 **빌드 시스템 의미론을 모름**: tsconfig.json의 `include`/`exclude`, hardhat의 `paths.sources`, foundry remappings, `package.json`의 `main`/`exports` 등은 모두 무시. 단순 디스크 walk.

---

## 2. TypeScript Flow

### 2.1 ParseFile (per-file)

```
Parser.ParseFile(path, src):
   parser := sitter.NewParser()
   lang := languageForExt(ext)              // .ts → typescript / .tsx → tsx / 그 외 → javascript
   parser.SetLanguage(lang)
   tree := parser.Parse(src, nil)            // tree-sitter parse
   v := newDeclVisitor(rel, src, lang, tree.RootNode())
   v.visit()                                 // query-based visit (아래 §2.2)
   return ParseResult{Path, Nodes, Edges, Pending}
```

**File 노드만 부트스트랩**:
```go
fileQ := "file:" + rel
v.fileID = makeID(fileQ, "ts", 0)
v.nodes = [File 노드 1개]
```

**Package 노드는 emit하지 않음** (Go와 차이). `package.json`/`tsconfig` 트래버스 없음 → 모듈 그래프 없음.

### 2.2 visit() 동작 (`declarations.go`)

7개 query를 순차 실행:

| Query | NodeType | 추가 emit | Field/Param 추적 |
|---|---|---|---|
| `class_declaration name: (type_identifier) @name` | `Class` | File -defines-> Class | ❌ |
| `interface_declaration name: (type_identifier) @name` | `Interface` | File -defines-> Interface | ❌ |
| `function_declaration name: (identifier) @name` | `Function` | File -defines-> Function | ❌ |
| `method_definition name: (property_identifier) @name` | `Method` | File -defines-> Method | ❌ |
| `type_alias_declaration name: (type_identifier) @name` | `TypeAlias` | File -defines-> | ❌ |
| `enum_declaration name: (identifier) @name` | `Enum` | File -defines-> | ❌ |
| `decorator (call_expression function: (identifier) @name)` | `Decorator` | File -defines-> | ❌ |
| `import_statement source: (string) @path` | `Import` | File -imports-> Import | path string only |

**Method qname 처리**: `nearestClassName`이 부모 chain을 walk → `ClassName.methodName`. enclosing class가 없으면 simple name만.

### 2.3 Resolve (Pass 2)

```go
func (p *Parser) Resolve(results []*ParseResult) (*ResolvedGraph, error) {
    byName := map[string][]string{}  // Function/Method/Class 이름 → ID 목록
    for each result:
        out.Nodes = append(...)
        out.Edges = append(...)
        // index Function/Method/Class by Name (NOT QualifiedName)
    for each PendingRef:
        ids := byName[pr.TargetQName]
        if ids empty: continue
        emit Edge{Src: pr.SrcID, Dst: ids[0], Confidence: INFERRED}
}
```

**중요**: Pending refs는 visit() 안에서 **거의 push되지 않음** — runImports/runQuery 코드가 `v.pending`에 추가하는 곳이 사실상 없음. 따라서 Resolve의 두 번째 루프는 빈 입력을 받기 일쑤. 결과적으로 TS는 사실상 per-file 노드/엣지만 emit하고 cross-file edge는 거의 0건.

---

## 3. Solidity Flow

### 3.1 ParseFile + visit

`tree-sitter-solidity v1.2.11` (vendored, ABI 14) → 다음 query 실행:

| Query | NodeType | qname rule |
|---|---|---|
| `contract_declaration name: (identifier) @name` | `Contract` | bare identifier |
| `function_definition name: (identifier) @name` | `Function` | `Contract.func` (nearestContractName) |
| `modifier_definition name: (identifier) @name` | `Modifier` | bare |
| `event_definition name: (identifier) @name` | `Event` | bare |
| `struct_declaration name: (identifier) @name` | `Struct` | bare |
| `enum_declaration name: (identifier) @name` | `Enum` | bare |
| `state_variable_declaration` | `Field` 또는 `Mapping` | `name` 또는 `name:mapping` |
| `emit_statement name: (expression (identifier) @event)` | (pending) | `emits_event` 큐잉 |
| `modifier_invocation (identifier) @mod` | (pending) | `has_modifier` 큐잉 |
| `augmented_assignment_expression … array_access (identifier) @arr` | (pending) | `writes_mapping` 큐잉 (mapping 발견시에만) |

**ABI 수집** (`collectABI`):
- v.nodes를 순회하면서 가장 최근 본 Contract 이름을 currentContract로 저장
- Function 발견 시 `abi[currentContract] = append(..., ABISig{ContractName, FunctionName, ParamTypes: nil})`
- ⚠️ `ParamTypes: nil` — V0 placeholder로 항상 비어있음
- ⚠️ 단일-레벨 contract만 정확. **nested contract는 부정확**.

### 3.2 Resolve (Pass 2)

```go
byName := map[NodeType]map[string][]string{}
   - Event   → name → [IDs]
   - Modifier→ name → [IDs]
   - Mapping → qualifiedName ("foo:mapping") → [IDs]

for each pending:
   targetType := EdgeType → NodeType 매핑
      EdgeEmitsEvent      → NodeEvent
      EdgeHasModifier     → NodeModifier
      EdgeWritesMapping   → NodeMapping
      그 외               → continue (drop)
   ids := byName[targetType][pr.TargetQName]
   if empty: continue
   conf := EXTRACTED if same-file else INFERRED
   emit Edge
```

**누락된 edge 종류**:
- ❌ `calls` (함수 호출 추적 자체가 없음)
- ❌ `reads_mapping` (writes만 있음)
- ❌ `reads_field` / `writes_field` (state var 접근 추적 없음)
- ❌ `references` / `uses_type` / `instantiates`
- ❌ `extends` / `implements` (Solidity의 `is OtherContract` 미추적)

---

## 4. Cross-Language Link (G5 `binds_to`)

`emitDerivedPasses` → `link.SolToTS(g.Nodes, abi)`:

```go
SolToTS(nodes, abi):
   tsClassByName := group TS Class nodes by Name
   for each Sol Contract n:
      matches := tsClassByName[n.Name]
      if empty: skip
      _ = abi[n.Name]                         // ⚠️ ABI 시그니처는 "조회만" 하고 사용하지 않음
      best := pickBest(matches)               // path heuristic
         score = path contains "typechain"/"contracts"/"abi" 개수
      emit Edge{Src: Contract.ID, Dst: best.ID, Type: binds_to, Confidence: INFERRED}
```

**핵심 한계**:
- 매칭은 **이름만** (Solidity Contract `Vault` ↔ TS Class `Vault`)
- `ParamTypes`이 nil이라 ABI 시그니처 비교 자체가 불가능
- 동명이인의 다른 contract/class도 매칭됨 (ex: 다른 모듈의 `Token`)
- pickBest는 파일 경로에 `contracts/typechain/abi` 포함 횟수만 카운트
- TS Class 이름과 다르거나 (typechain-generated `__factory` suffix 등) interface로 표현된 경우 매칭 실패

---

## 5. 현재 한계 / Gap (Critical)

### 5.1 TS 파서의 핵심 누락

| 누락 | 영향 | spec/CKS 의도 대비 |
|---|---|---|
| **함수 body walk 없음** | `calls/invokes` 엣지 0건 (TS) | G3 Execution 사실상 비어있음 |
| **CallSite/IfStmt/LoopStmt 등 0** | 통제흐름 그래프 부재 | spec §4.6.2 Statement-level 노드 없음 |
| **Pending refs push 없음** | Resolve 단계 사실상 무동작 | Pass 2가 형태만 존재 |
| **Field/Property/Parameter 노드 없음** | 데이터 모델 빈약 | G2 Semantic 빈약 |
| **`extends`/`implements` 미추출** | 클래스 hierarchy 부재 | G1/G2 양쪽 미반영 |
| **Decorator 인자 무시** | `@Component({ selector: 'x' })` 정보 손실 | E5 G5 routing 정보 손실 |
| **Re-export(`export * from`) 미추적** | 모듈 그래프 단절 | 의도된 G1 imports 그래프 단절 |
| **JSX/TSX 컴포넌트 사용 (`<Foo />`) 미추출** | React 의존성 보이지 않음 | UI 그래프 거의 활용 불가 |
| **`async/await/Promise` 흐름 미반영** | 동시성 그래프 0 | G4 Concurrency = TS 0건 |
| **`fetch`/HTTP client 콜 미추출** | 분산 그래프 0 (TS) | G5 Distributed = TS 0건 |

### 5.2 Sol 파서의 핵심 누락

| 누락 | 영향 |
|---|---|
| **함수 호출 추적 자체 없음** | Solidity inter-function `calls` = 0건 |
| **`is X, Y` 상속 미추출** | OpenZeppelin 패턴 그래프 단절 |
| **`reads_mapping` 미구현** | 데이터 흐름 분석 반쪽 |
| **state var 접근 (`this.x`, `x = 1`) 미추적** | reads_field/writes_field 0건 |
| **`require`/`revert`/`assert` 미모델링** | 검증 로직 보이지 않음 |
| **interface inheritance 미추출** | `IERC20` 등 표준 인터페이스 연결 안 됨 |
| **library 사용 (`using SafeMath for uint;`) 미추출** | OZ/Solady 패턴 단절 |
| **multi-file import (`import "./Other.sol";`) 그래프 부재** | Sol 모듈 그래프 0 |
| **nested contract / abstract / library 구분 없이 ABI 1차원** | xlang 매칭 false positive |

### 5.3 xlang `binds_to`의 부정확성

- **이름만 보는 nominal 매칭**: 같은 이름 TS Class가 여러 개면 path heuristic으로 1개만 선택 → silent miss
- **ABI 시그니처 미사용**: `ParamTypes` nil이라 함수 단위 매칭 불가능, 오직 Contract↔Class 단위
- **TS interface ↔ Sol contract 매칭 불가**: ethers/viem 타입은 interface로 표현되지만 link.SolToTS는 `NodeClass`만 본다
- **typechain factory suffix 미대응**: `__Vault__factory` 같은 generated 이름 매칭 실패

### 5.4 의도된 G2~G6과 실제 emit의 정량 갭

| Graph axis | 의도 | TS 실제 emit | Sol 실제 emit |
|---|---|---|---|
| G1 Structural | contains/defines/imports/exports | defines/imports만 (exports TODO) | defines만 (imports 미구현) |
| G2 Semantic | references/extends/implements/uses_type/... | **거의 모두 0** | emits_event/has_modifier/writes_mapping만 |
| G3 Execution | calls/invokes/CallSite/... | **모두 0** | **모두 0** |
| G4 Concurrency | spawns/sends_to/.../Mutex | **모두 0** | (해당 없음) |
| G5 Distributed | listens_on/handles_message/rpc_calls/binds_to | **모두 0** | binds_to (xlang에서 emit) |
| G6 Temporal | changed_in/blame | (P6에서 file 단위로 일괄 적용) | (동일) |

→ **viewer의 G3/G4/G5 토글이 TS/Sol에선 거의 빈 상태**가 정상 동작.

---

## 6. 검증된 동작 vs 미구현

| 항목 | 상태 |
|---|---|
| .ts/.tsx/.js/.jsx/.mjs/.cjs 확장자 인식 | ✅ |
| tree-sitter v0.25 grammar 호환성 | ✅ (golden test 보유) |
| Solidity vendored grammar (ABI 14) | ✅ (golden test 보유) |
| TS Class/Interface/Function/Method/Decorator/TypeAlias/Enum/Import emit | ✅ |
| Sol Contract/Function/Modifier/Event/Struct/Enum/Field/Mapping emit | ✅ |
| Sol emits_event / has_modifier / writes_mapping 엣지 | ✅ |
| Sol→TS binds_to (이름 기반) | ✅ (이름 일치 시) |
| TS export statement | ❌ (`queryExport` 주석처리, TODO T18+) |
| TS function body / 호출 그래프 | ❌ |
| TS cross-file reference resolution | ❌ (Resolve 거의 무동작) |
| Sol inter-function calls | ❌ |
| Sol contract inheritance (`is`) | ❌ |
| Sol library usage (`using ... for ...`) | ❌ |
| Sol reads_mapping | ❌ |
| xlang ABI 시그니처 매칭 | ❌ (ParamTypes nil) |
| `tsconfig.json` paths/baseUrl 인식 | ❌ |
| `package.json` main/exports/dependencies 인식 | ❌ |
| hardhat/foundry remapping 인식 | ❌ |

---

## 7. 근본 원인 후보 (디버깅 시 우선 검사)

> 사용자가 "동작이 정상이지 않다"고 보고한 경우, TS/Sol 영역에서 가장 자주 발생할 가능성이 큰 root cause를 빈도순으로 정리.

### R1. **TS 파일이 그래프에 거의 비어있는 듯하다**
원인: 정상 동작입니다. TS 파서는 query-based 7종만 emit하며 함수 body/호출 추적이 없습니다. spec과 viewer UI는 풍부한 G2/G3을 약속하지만 실제 TS emit은 declarations + imports에 그칩니다.
검증: `sqlite3 graph.db "SELECT type, COUNT(*) FROM nodes WHERE language='ts' GROUP BY type"` → Function/Method/Class만 다수, CallSite/IfStmt 0.
대응: V1+ 영역. 단기적으론 expectation 조정.

### R2. **TS의 cross-file edge가 0이다**
원인: `Resolve` 함수의 두 번째 루프는 `r.Pending`을 순회하는데, 코드 어디에서도 TS pending이 push되지 않습니다 (declarations.go에 `v.pending = append(...)` 호출 없음).
검증: `grep -n "v.pending" internal/parse/typescript/*.go` → 0건.
대응: cross-file 호출 추적이 필요하면 TS 파서에 body-walk + identifier resolution 추가 필요.

### R3. **Sol 파일의 함수 호출 그래프가 비어있다**
원인: Solidity 파서는 `function_definition` 자체는 추출하지만 함수 body 안의 호출 (`other.foo()`, `someContract.bar()`)을 query로 잡지 않습니다.
검증: `sqlite3 graph.db "SELECT COUNT(*) FROM edges WHERE type='calls' AND ..."` (Sol 파일만) → 0.
대응: Solidity body 안의 `call_expression` query 추가 + nearestFunctionQnameAndStart로 src 매핑.

### R4. **xlang `binds_to`가 잘못 매칭되거나 누락된다**
원인: pickBest의 path heuristic이 corpus 디렉토리 구조와 다르면 엉뚱한 Class를 잡거나 모두 0점이라 처음 매칭만 선택.
검증: `sqlite3 graph.db "SELECT n_src.name, n_dst.file_path FROM edges JOIN nodes n_src ... WHERE type='binds_to'"`
대응: `--graph` 빌드 후 viewer에서 G5 binds_to 필터로 수동 검증. 잘못된 매칭은 V0 한계.

### R5. **`export-postgres` 시 schema/data 누락**
원인: TS/Sol 자체는 PG export와 무관. PG export는 SQLite의 노드/엣지를 단순 복제. 빠진 게 있다면 SQLite 자체에 없는 것.
검증: SQLite와 PG의 row count 비교. 다르면 export-postgres 코드 (B2) 버그, 같으면 build 단계 문제.

### R6. **`audit` exit 1 — TS/Sol 파일 누락 보고**
원인: audit는 Go만 검증. TS/Sol은 audit 대상 외.
검증: `audit.go` 코드 확인 → `go/packages.Load`만 비교.
대응: 현재 의도된 동작. Go 영역에서만 parity 보장.

### R7. **`testdata/`/`vendor/` 등 의도하지 않은 파일이 그래프에 들어왔다**
원인: `detect.Walk` skip 목록은 vendor/, node_modules/, .git/, testdata/. 그 외 디렉토리(`out/`, `dist/`, `.next/`, `build/`)는 walk됨.
검증: `sqlite3 graph.db "SELECT DISTINCT file_path FROM nodes WHERE file_path LIKE 'dist/%'"`
대응: `.ckgignore` 작성 (gitignore-style — `internal/detect/ckgignore.go`).

### R8. **TS 파서가 .vue/.svelte/.astro 등을 무시한다**
원인: Extensions 목록은 `.ts/.tsx/.js/.jsx/.mjs/.cjs`만. 다른 frontend 프레임워크는 미지원.
검증: `find <src> -name '*.vue' | wc -l` vs DB의 ts language count.
대응: V1+ — query 가능하다면 grammar 추가.

---

## 8. 핵심 한 줄 요약

> **TS 파서**: tree-sitter query 7종으로 declarations + imports만 emit, 함수 body 미추적, cross-file resolve 사실상 무동작 → G3/G4/G5 거의 비어있음.
>
> **Sol 파서**: declarations + emit/modifier/mapping 3종 pending만 emit, 함수 호출/상속/library 미추적, ABI 시그니처는 nil placeholder → calls/extends/implements 모두 0건.
>
> **xlang `binds_to`**: Contract.Name ↔ Class.Name 단일 매칭 + path heuristic, ABI 미사용 → false positive/negative 모두 가능.
>
> **결과적으로**, "33 NodeType × 30 EdgeType + G1~G6"이라는 schema 표면은 TS/Sol에서 **사용자 기대보다 빈약**하게 채워집니다. 사용자가 보고한 "누락"의 상당 부분이 이 V0 simplification의 결과일 가능성이 높습니다.

---

## Appendix: 의도된 spec과 실제 구현의 매핑

| spec §4.6.2/4.6.3 의도 | 실제 구현 | 결과 |
|---|---|---|
| TS: tree-sitter Queries로 7종 declaration | ✅ | OK |
| TS: import/export 그래프 | imports만 ✅, exports ❌ | 단방향 |
| TS: function calls (cross-file) | ❌ | G3 비어있음 |
| TS: class inheritance (`extends`/`implements`) | ❌ | G2 부분적 |
| Sol: contract/function/event 추출 | ✅ | OK |
| Sol: state var + mapping 구분 | ✅ | OK |
| Sol: emits_event/has_modifier/writes_mapping | ✅ | OK |
| Sol: contract inheritance | ❌ | OZ 패턴 단절 |
| Sol: function calls | ❌ | G3 비어있음 |
| Sol: ABI signature 추출 | ❌ (nil placeholder) | xlang 정밀도 저하 |
| xlang Sol → TS 시그니처 기반 매칭 | ❌ (이름만) | binds_to false matches |
| xlang Go → external (RPC client) | ❌ | spec V1+ |

**End of TS/Sol build flow analysis.** 본 문서는 TS/Sol에서 흔히 발생하는 "그래프가 비어있다" / "엣지가 부족하다" 류 문제의 상당수가 V0 의도된 simplification임을 분명히 합니다. 신규 추출이 필요하면 spec V1+ 작업으로 격상.
