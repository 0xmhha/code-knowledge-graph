# Self-Verification Manual

> **ARCHIVED 2026-07-18.** The command *flows* still work, but the expected
> values are stale (schema was 1.6, now 1.23; "7 MCP tools", now 10; fixed
> node/edge counts drift every commit). `docs/CONTINUITY.md` is the sole live
> status doc. Kept for provenance and as a self-verification recipe reference.

> **목적**: ckg 프로젝트가 자기 자신의 코드를 분석한 결과(self-graph)로 7가지 사용자 검토항목이 충족되는지 수동으로 확인하기 위한 명령 모음.
> **대상 corpus**: CKG repo 루트 (`$REPO_ROOT`) — CKG 자기 자신.
> **선행 문서**: `docs/archive/analysis/SELF-GRAPH-COMPARISON.md` (변경 이력) / `docs/archive/analysis/CKS-SPEC-COMPLIANCE.md` (spec 충실도).

각 절은 단계별로 실행하며, 기대 출력과 함께 정리되어 있습니다. 빠른 한 번 체크는 [§ 9 자동 검증 스크립트](#9-자동-검증-스크립트)로 끝납니다.

---

## 0. 사전 준비

```bash
cd "$REPO_ROOT"   # CKG 저장소 루트 (예: ~/Work/.../code-knowledge-graph)

# 최신 binary 빌드
go build -o bin/ckg ./cmd/ckg

# self-graph 생성 (Go 한정 + strict 모드로 무결성 보장)
rm -rf /tmp/ckg-self
./bin/ckg build --src=. --out=/tmp/ckg-self --no-cache --lang=go --strict-validate

# SQLite 쿼리 헬퍼
qdb() { sqlite3 /tmp/ckg-self/graph.db "$@"; }
```

**기대 출력 (예시)**:

```
ckg: built 13,292 nodes / 61,863 edges into /tmp/ckg-self
```

`--strict-validate`이라 dangling edge 1건이라도 발견되면 빌드가 실패합니다. 통과했다면 그래프 무결성은 보장됨.

---

## 1. 검토항목 #1 — AST parsing → graph DB

> **요구**: 프로젝트 경로 → AST parsing → node/edge graph DB 저장.

### 1.1 노드/엣지 분포 확인

```bash
# 노드 타입별 카운트
qdb "SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY 2 DESC"

# 엣지 타입별 카운트 (G1~G6 분포 확인)
qdb "SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY 2 DESC"
```

### 1.2 빈값 / 무결성 검증

```bash
qdb "SELECT
  (SELECT COUNT(*) FROM nodes WHERE id='' OR id IS NULL)             AS empty_id,
  (SELECT COUNT(*) FROM nodes WHERE name='' OR name IS NULL)         AS empty_name,
  (SELECT COUNT(*) FROM nodes WHERE qualified_name='' OR qualified_name IS NULL) AS empty_qname,
  (SELECT COUNT(*) FROM edges WHERE src='' OR src IS NULL)           AS empty_src,
  (SELECT COUNT(*) FROM edges WHERE dst='' OR dst IS NULL)           AS empty_dst,
  (SELECT COUNT(*) FROM blobs WHERE source IS NULL)                  AS null_blobs"
```

**기대**: 모든 값이 `0`. 어느 하나라도 0이 아니면 emitter 버그.

### 1.3 manifest fingerprint 확인

```bash
qdb "SELECT key, value FROM manifest WHERE key IN
  ('schema_version','ckg_version','build_timestamp','staleness_method','src_commit')"
```

**기대**: `schema_version=1.6` (P2 이후), `staleness_method=git`, `src_commit`이 현재 HEAD와 일치.

---

## 2. 검토항목 #2 — golang/solidity/ts/js 지원

> **요구**: 4 언어 지원. 현재 V0 simplification: Go는 deep, TS/Sol은 declarations + imports만.

### 2.1 Go 검증 (deep)

```bash
# audit으로 build set vs DB set parity 확인
./bin/ckg audit --src=. --graph=/tmp/ckg-self
echo "exit=$?"
```

**기대**: `exit=0` (PARITY). drift 있으면 `in_build_only` / `in_db_only` 파일 목록 출력.

### 2.2 4 언어 모두 빌드 (V0 상태 비교)

```bash
rm -rf /tmp/ckg-self-all
./bin/ckg build --src=. --out=/tmp/ckg-self-all --no-cache
qdb_all() { sqlite3 /tmp/ckg-self-all/graph.db "$@"; }

qdb_all "SELECT language, type, COUNT(*) FROM nodes
  WHERE language != ''
  GROUP BY language, type
  ORDER BY language, 3 DESC LIMIT 30"
```

**기대**:
- `go`: Function/Method/Struct/Interface/CallSite/IfStmt/LoopStmt 모두 다수.
- `ts`: Class/Interface/Function/Method/TypeAlias/Enum/Decorator/Import만. CallSite/IfStmt 등은 0 (V0 의도된 simplification).
- `sol`: Contract/Function/Modifier/Event/Struct/Enum/Field/Mapping. CallSite 0 (Sol body walk 미구현).

---

## 3. 검토항목 #3 — 빌드 기반 분석 (Go 한정)

> **요구**: 빌드에 실제 포함되는 코드만. Go는 `go/packages.Load` 자동 적용. TS/Sol은 advanced로 후순위.

### 3.1 build constraint 적용 확인

```bash
# 호스트 OS와 다른 build tag 파일이 그래프에 포함되지 않는지 확인 (예시: linux-only 파일)
qdb "SELECT file_path FROM nodes WHERE language='go' AND type='File'" | grep -i "linux\|windows\|plan9" || echo "없음 (정상)"

# vendor/ 또는 testdata/ 가 의도적으로 포함되었는지 확인
qdb "SELECT DISTINCT file_path FROM nodes WHERE file_path LIKE 'vendor/%' OR file_path LIKE '%/testdata/%'" \
  | head -5
# 기대: vendor/는 없어야 함. testdata/ 는 일부 fixture가 보일 수 있음 (의도된 경우)
```

### 3.2 audit drift 체크

```bash
./bin/ckg audit --src=. --graph=/tmp/ckg-self --format=json | python3 -m json.tool | head -30
```

**기대**: `in_build_only=0`, `in_db_only=0`, `in_both=147` (현 시점 Go 파일 수).

---

## 4. 검토항목 #4 — `--files-from` 옵션

> **요구**: 빌드 파일 리스트 옵션. 없으면 모든 코드 분석.

### 4.1 부분집합 빌드

```bash
cat > /tmp/files-only-bm25.json <<'EOF'
{
  "include": ["pkg/bm25/**", "pkg/smartctx/**"],
  "exclude": ["**/*_test.go"]
}
EOF

rm -rf /tmp/ckg-bm25
./bin/ckg build --src=. --out=/tmp/ckg-bm25 --no-cache --lang=go \
  --files-from=/tmp/files-only-bm25.json 2>&1 | grep "files-from applied"
```

**기대 로그**: `level=INFO msg="files-from applied" go=147 go_after=4 ts=14049 ts_after=0 sol=5 sol_after=0`.

### 4.2 결과 그래프가 부분집합인지 확인

```bash
sqlite3 /tmp/ckg-bm25/graph.db \
  "SELECT DISTINCT file_path FROM nodes WHERE language='go' AND type='File' ORDER BY file_path"
```

**기대**:
```
pkg/bm25/okapi.go
pkg/bm25/scorer.go
pkg/bm25/tokenize.go
pkg/smartctx/smartctx.go
```

### 4.3 잘못된 JSON 처리

```bash
echo '{"include":' > /tmp/bad.json
./bin/ckg build --src=. --out=/tmp/ckg-bad --no-cache --files-from=/tmp/bad.json
echo "exit=$?"
```

**기대**: 비-0 exit + `filterlist: parse <path>: ...` 류 명확한 에러.

---

## 5. 검토항목 #5 — 6 graph 종류

> **요구**: G1 Structural / G2 Semantic / G3 Execution / G4 Concurrency / G5 Distributed / G6 Temporal 모두 spec대로.

### 5.1 G1 Structural

```bash
qdb "SELECT type, COUNT(*) FROM edges
  WHERE type IN ('contains','defines','imports','exports')
  GROUP BY type ORDER BY 2 DESC"
```

**기대**: contains 9K+, defines 1.5K+, imports 600+. exports는 0 (TS dead-key, V0 미구현).

### 5.2 G2 Semantic

```bash
qdb "SELECT type, COUNT(*) FROM edges
  WHERE type IN ('references','implements','extends','uses_type','instantiates',
                 'reads_field','writes_field','reads_mapping','writes_mapping',
                 'emits_event','has_modifier','has_decorator')
  GROUP BY type ORDER BY 2 DESC"
```

**기대**: implements 20+, extends 2+ (P0 결과). reads_field/writes_field는 production 사용 패턴에 따라 다름.

```bash
# implements 페어 샘플
qdb "SELECT n_src.qualified_name AS impl, n_dst.qualified_name AS iface
  FROM edges e
  JOIN nodes n_src ON e.src=n_src.id
  JOIN nodes n_dst ON e.dst=n_dst.id
  WHERE e.type='implements'
  ORDER BY iface, impl LIMIT 10"
```

**기대**: `persist.sqliteStore → persist.Store`, `bm25.Okapi → bm25.Scorer` 등.

### 5.3 G3 Execution

```bash
qdb "SELECT type, COUNT(*) FROM edges
  WHERE type IN ('calls','invokes','timeout_path','cancellation_path')
  GROUP BY type ORDER BY 2 DESC"
```

**기대**: calls 1.7K+, timeout_path 7 (P2 결과), cancellation_path 0 (코드에 WithCancel 없음).

```bash
# context.WithTimeout 호출 함수 확인
qdb "SELECT n.qualified_name FROM edges e JOIN nodes n ON e.src=n.id
  WHERE e.type='timeout_path' ORDER BY n.qualified_name"
```

**기대**: `server.Server.ListenAndServe`, `eval.queryTokenMonitor` 등.

### 5.4 G4 Concurrency

```bash
qdb "SELECT type, COUNT(*) FROM edges
  WHERE type IN ('spawns','sends_to','recvs_from',
                 'acquires_lock','releases_lock','accessed_under_lock')
  GROUP BY type ORDER BY 2 DESC"
```

**기대**: spawns 5, recvs_from 4, sends_to 3 (모두 `parseConcurrent` 등 production 동시성 코드에서 emit).

```bash
# Mutex 노드 + 그 위치 확인
qdb "SELECT qualified_name, file_path FROM nodes WHERE type='Mutex' ORDER BY file_path"
```

**기대**: `buildpipe.parseConcurrent.errMu`, `solidity.Parser.abiMu#mutex`.

### 5.5 G5 Distributed

```bash
qdb "SELECT type, COUNT(*) FROM edges
  WHERE type IN ('listens_on','handles_message','rpc_calls','binds_to')
  GROUP BY type ORDER BY 2 DESC"
```

**기대**: listens_on 7 (server.go의 7개 API endpoint, listens_on dangling 버그 fix 반영).

```bash
# listens_on 핸들러 → 엔드포인트 페어
qdb "SELECT n.qualified_name AS handler, dst.qualified_name AS endpoint
  FROM edges e
  JOIN nodes n ON e.src=n.id
  JOIN nodes dst ON e.dst=dst.id
  WHERE e.type='listens_on' ORDER BY endpoint"
```

**기대**: 7개 모두 `server.Server.handle*` → `http:GET/POST /api/...` 정상 연결.

### 5.6 G6 Temporal

```bash
qdb "SELECT type, COUNT(*) FROM edges WHERE type IN ('changed_in','blame') GROUP BY type"

# 가장 자주 수정된 파일 top 10 (commit 횟수 기준)
qdb "SELECT n.file_path, COUNT(DISTINCT e.dst) AS commits
  FROM edges e JOIN nodes n ON e.src=n.id
  WHERE e.type='changed_in'
  GROUP BY n.file_path
  ORDER BY commits DESC LIMIT 10"
```

**기대**: changed_in 다수 (commit × 파일 cardinality), blame은 파일 수와 동일.

---

## 6. 검토항목 #6 — Query 기능 (JSON 포맷)

> **요구**: graph DB 데이터를 LLM 활용 가능한 JSON으로 제공.

### 6.1 MCP 서버 기본 동작

```bash
# initialize + tools/list 한 번에 호출
cat <<'EOF' | ./bin/ckg mcp --graph=/tmp/ckg-self 2>/dev/null \
  | grep -o '"name":"[a-z_]*"' | sort -u
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"manual","version":"1"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
EOF
```

**기대 (7개 tool 출력)**:
```
"name":"find_callees"
"name":"find_callers"
"name":"find_symbol"
"name":"get_context_for_task"
"name":"get_subgraph"
"name":"impact_of_change"
"name":"search_text"
```

### 6.2 도구별 호출 헬퍼

```bash
mcp_call() {
  local tool="$1" args="$2"
  cat <<EOF | ./bin/ckg mcp --graph=/tmp/ckg-self 2>/dev/null \
    | python3 -c "
import json, sys
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    try:
        d = json.loads(line)
        if d.get('id') == 3 and 'result' in d:
            content = d['result']['content'][0]['text']
            print(json.dumps(json.loads(content), indent=2))
    except: pass
"
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"manual","version":"1"}}}
{"jsonrpc":"2.0","id":2,"method":"initialized"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"$tool","arguments":$args}}
EOF
}
```

### 6.3 7개 도구 검증

```bash
# 1) find_symbol — 식별자/qname 검색
mcp_call find_symbol '{"name":"sqliteStore","exact":false}' | head -20

# 2) find_callers — 어떤 함수가 호출하는가
mcp_call find_callers '{"qname":"persist.sqliteStore.AllNodes","depth":2}' | head -30

# 3) find_callees — 어떤 함수를 호출하는가
mcp_call find_callees '{"qname":"buildpipe.parseConcurrent","depth":1}' | head -30

# 4) get_subgraph — 양방향 1-hop 서브그래프
mcp_call get_subgraph '{"seed_qname":"validate.SchemaValidator","depth":2}' | head -40

# 5) search_text — FTS5 검색
mcp_call search_text '{"query":"BM25","top_k":5}' | head -30

# 6) get_context_for_task — smart 도구 (real BM25 + Citation Enforcement)
mcp_call get_context_for_task '{"task_description":"how does ckg detect goroutine spawns","budget_tokens":4000,"max_bodies":3}' \
  | python3 -c "
import json, sys
data = json.load(sys.stdin)
print('bodies:', len(data.get('bodies', [])))
print('summaries:', len(data.get('summaries', [])))
print('warnings:', len(data.get('metadata', {}).get('warnings', [])))
for b in data.get('bodies', [])[:3]:
    print(f\"  body @ {b.get('citation','?')}: {b.get('qname','?')}\")
"

# 7) impact_of_change — reverse dependency closure (P1a)
mcp_call impact_of_change '{"seed_qname":"persist.Store","depth":2}' \
  | python3 -c "
import json, sys
data = json.load(sys.stdin)
print('totals:', json.dumps(data.get('totals', {}), indent=2))
for group, items in data.get('impact', {}).items():
    if items:
        print(f'-- {group} ({len(items)}) --')
        for n in items[:5]: print(f\"  {n.get('citation','?')}: {n.get('qname','?')}\")
"
```

### 6.4 Citation Enforcement 검증 (warn mode)

`get_context_for_task` 응답의 모든 body/summary/subgraph node가 `citation` 필드를 갖거나 `metadata.warnings` 에 기록되는지 확인.

```bash
mcp_call get_context_for_task '{"task_description":"BM25 implementation","max_bodies":2}' \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
nodes = d.get('subgraph', {}).get('nodes', [])
warnings = {w['node_id']: w for w in d.get('metadata', {}).get('warnings', [])}
ok = 0; missing = 0
for n in nodes:
    if n.get('citation'): ok += 1
    elif n['id'] in warnings: ok += 1  # documented as warning
    else: missing += 1; print('LEAK:', n)
print(f'cited or warned: {ok} / leaked: {missing}')
"
```

**기대**: leaked 0건 (모든 노드가 citation 있거나 warning에 기록됨).

---

## 7. 검토항목 #7 — Validation tool

> **요구**: graph DB 누락/오류 검증 tool. 현재 SchemaValidator (deterministic) + LLMValidator (dry-run, V1+ 실제 호출).

### 7.1 SchemaValidator 단독

```bash
./bin/ckg validate --graph=/tmp/ckg-self --format=text
```

**기대**: `── schema ──  errors=0  warnings=0  info=0`. exit=0.

### 7.2 SchemaValidator JSON 포맷 (CI 통합용)

```bash
./bin/ckg validate --graph=/tmp/ckg-self --format=json | python3 -m json.tool
echo "exit=$?"
```

**기대**: `[]` 또는 `[{"Validator":"schema","Issues":[]}]`. exit=0.

### 7.3 LLMValidator dry-run

```bash
./bin/ckg validate --graph=/tmp/ckg-self --llm --format=text | head -30
```

**기대**: SchemaValidator 통과 + `── llm ── ... info=N` (N = INFERRED edge 수). 각 prompt가 `Question` + `EdgeKey` + `FilePath` 포함.

```bash
# JSON으로 prompt 텍스트만 추출
./bin/ckg validate --graph=/tmp/ckg-self --llm --format=json \
  | python3 -c "
import json, sys
data = json.load(sys.stdin)
for r in data:
    if r['Validator'] == 'llm':
        for issue in r['Issues']:
            print(f\"[{issue['Code']}]\")
            print(f'  Q: {issue[\"Message\"]}')
            print(f'  ref: {issue.get(\"EdgeKey\") or issue.get(\"NodeID\")}')
            print(f'  file: {issue.get(\"FilePath\",\"\")}')
            print()
"
```

이 출력을 ChatGPT/Claude에 그대로 붙여넣고 verdict를 받을 수 있음 (V0 manual flow).

### 7.4 LLMValidator real 모드 (V1+ 미구현 확인)

```bash
./bin/ckg validate --graph=/tmp/ckg-self --llm --llm-dry-run=false
echo "exit=$?"
```

**기대**: `errors=1` + `llm-not-yet-wired` Error issue. exit=1.

### 7.5 audit + validate 조합 (CI 시나리오)

```bash
./bin/ckg build --src=. --out=/tmp/ckg-ci --no-cache --lang=go --strict-validate \
  && ./bin/ckg audit --src=. --graph=/tmp/ckg-ci \
  && ./bin/ckg validate --graph=/tmp/ckg-ci --format=text \
  && echo "✅ ALL CHECKS PASSED"
```

---

## 8. 시나리오별 spot-check

사용자 의도("LLM이 정확히 어디를 수정할지 찾도록")를 직접 검증.

### 8.1 "BM25 정확도가 낮아 보임. 어디부터 봐야 하나?"

```bash
mcp_call get_context_for_task '{"task_description":"BM25 scoring is producing wrong ranks","budget_tokens":4000,"max_bodies":3}' \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
print('--- Top 3 bodies ---')
for b in d.get('bodies', [])[:3]: print(f\"  {b.get('citation','?')}: {b.get('qname','?')}\")
print('--- Top 5 summaries ---')
for s in d.get('summaries', [])[:5]: print(f\"  {s.get('citation','?')}: {s.get('qname','?')}\")
"
```

**기대**: `pkg/bm25/okapi.go`의 `Score` / `idf` 등이 상위에 노출. citation이 정확한 line number 제공.

### 8.2 "X 함수를 변경하면 어디가 영향?"

```bash
# bm25.NewOkapi 수정 시 파급 범위
mcp_call impact_of_change '{"seed_qname":"bm25.NewOkapi","depth":3}' \
  | python3 -c "
import json, sys
d = json.load(sys.stdin)
print('totals:', json.dumps(d.get('totals', {})))
for g, items in d.get('impact', {}).items():
    if items:
        print(f'--- {g} ({len(items)}) ---')
        for n in items[:5]: print(f\"  {n.get('citation','?')}: {n.get('qname','?')}\")
"
```

**기대**: `callers` 그룹에 `smartctx.scoreWithBM25` 가시적.

### 8.3 "동시성 관련 버그 의심. 어디 봐야?"

```bash
# G4 edges에서 시작
mcp_call get_context_for_task '{"task_description":"goroutine leak or data race","budget_tokens":4000,"max_bodies":3}' \
  | head -40
```

**기대**: `parseConcurrent`, `Parser.abiMu` 등 동시성 진입점이 상위.

### 8.4 "interface가 불완전하게 구현되었나?"

```bash
# Interface별 구현체 수 카운트
qdb "SELECT n_dst.qualified_name AS iface, COUNT(*) AS impl_count
  FROM edges e
  JOIN nodes n_src ON e.src = n_src.id
  JOIN nodes n_dst ON e.dst = n_dst.id
  WHERE e.type='implements'
  GROUP BY n_dst.qualified_name
  ORDER BY impl_count"

# 어떤 Interface가 implementer 0건인지
qdb "SELECT qualified_name FROM nodes WHERE type='Interface'
  EXCEPT
  SELECT n_dst.qualified_name FROM edges e
  JOIN nodes n_dst ON e.dst = n_dst.id
  WHERE e.type='implements'"
```

**기대**: 0건 implementer Interface는 LLMValidator의 sparse-implements sampler가 prompt로 재현해야 함.

---

## 9. 자동 검증 스크립트

한 번에 모든 검증을 돌리는 자동 스크립트:

```bash
cat > /tmp/verify-all.sh <<'EOF'
#!/bin/bash
set -e
cd "${REPO_ROOT:?REPO_ROOT must point at the CKG repo root}"

OUT=/tmp/ckg-verify
echo "=== 1. Build (--strict-validate, lang=go) ==="
rm -rf "$OUT"
./bin/ckg build --src=. --out="$OUT" --no-cache --lang=go --strict-validate

echo "=== 2. Audit (parity check) ==="
./bin/ckg audit --src=. --graph="$OUT"

echo "=== 3. Validate (schema + LLM dry-run) ==="
./bin/ckg validate --graph="$OUT" --llm --format=text | head -30

echo "=== 4. Empty value check ==="
sqlite3 "$OUT/graph.db" "SELECT
  (SELECT COUNT(*) FROM nodes WHERE id='' OR id IS NULL) +
  (SELECT COUNT(*) FROM nodes WHERE name='' OR name IS NULL) +
  (SELECT COUNT(*) FROM nodes WHERE qualified_name='' OR qualified_name IS NULL) +
  (SELECT COUNT(*) FROM edges WHERE src='' OR src IS NULL) +
  (SELECT COUNT(*) FROM edges WHERE dst='' OR dst IS NULL) AS total_empty"

echo "=== 5. 6-graph emit summary ==="
sqlite3 "$OUT/graph.db" "SELECT type, COUNT(*) FROM edges GROUP BY type ORDER BY 2 DESC LIMIT 20"

echo "=== 6. MCP smoke (7 tools listed) ==="
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"verify","version":"1"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | ./bin/ckg mcp --graph="$OUT" 2>/dev/null \
  | grep -o '"name":"[a-z_]*"' | sort -u

echo "=== ✅ All verification steps complete ==="
EOF
chmod +x /tmp/verify-all.sh
/tmp/verify-all.sh
```

---

## 10. SQL Query 치트 시트

자주 쓰는 쿼리 모음:

```bash
# 노드 ID로 노드 정보 보기
qdb "SELECT type, name, qualified_name, file_path, start_line FROM nodes WHERE id = 'XXXX'"

# qname으로 노드 찾기
qdb "SELECT id, type, file_path, start_line FROM nodes WHERE qualified_name LIKE '%Parser%'"

# 특정 노드의 모든 edge
qdb "SELECT type, src, dst FROM edges WHERE src = 'XXXX' OR dst = 'XXXX'"

# 특정 file의 모든 노드
qdb "SELECT type, name, qualified_name, start_line FROM nodes
  WHERE file_path = 'internal/parse/golang/declarations.go'
  ORDER BY start_line"

# 가장 호출 많이 받는 함수 (callee 인기도)
qdb "SELECT n.qualified_name, COUNT(*) AS callers
  FROM edges e JOIN nodes n ON e.dst = n.id
  WHERE e.type IN ('calls','invokes')
  GROUP BY n.qualified_name
  ORDER BY callers DESC LIMIT 20"

# PageRank top N
qdb "SELECT qualified_name, page_rank FROM nodes
  WHERE page_rank IS NOT NULL
  ORDER BY page_rank DESC LIMIT 20"

# 함수별 in-degree (얼마나 많은 다른 곳에서 참조)
qdb "SELECT n.qualified_name, COUNT(DISTINCT e.src) AS in_degree
  FROM edges e JOIN nodes n ON e.dst = n.id
  WHERE n.type IN ('Function','Method')
  GROUP BY n.qualified_name
  ORDER BY in_degree DESC LIMIT 20"
```

---

## 11. 문제 발생 시

### `--strict-validate` 빌드 실패 (dangling edge)

원인: parser가 src/dst가 nodes 집합에 없는 edge를 emit. 디버그:

```bash
# lenient 모드로 빌드 (drop된 dangling edge 정보가 stderr에 출력)
./bin/ckg build --src=. --out=/tmp/ckg-lenient --no-cache --lang=go --verbose 2>&1 \
  | grep -E "dangling|drop"
```

각 dangling edge를 추적해 emitter 버그 식별. 과거 사례: `listens_on` dangling은 `idForFunc`의 fset offset 버그였음 (commit `5eb4062`).

### audit drift

원인: detect.GoFiles의 build set과 DB의 file 집합 불일치.

```bash
./bin/ckg audit --src=. --graph=/tmp/ckg-self --format=json | python3 -m json.tool
# in_build_only / in_db_only 목록을 보고 어떤 파일이 누락/과다 포함됐는지 확인
```

### MCP 응답이 비어있음 (`not_found: true`)

원인: 검색어가 FTS index에 hit 안 됨. 우회:

```bash
# qname 일부로 직접 검색
qdb "SELECT type, qualified_name FROM nodes WHERE qualified_name LIKE '%검색어%' LIMIT 10"
```

### Validate 결과가 다른 환경과 다름

원인: 다른 환경의 graph.db를 같은 binary로 검증. SchemaValidator는 deterministic하므로 같은 graph.db에 같은 결과.

```bash
# graph.db의 fingerprint 비교
qdb "SELECT key, value FROM manifest WHERE key='src_commit'"
```

---

## 12. 참고 문서

- 7가지 검토항목 spec과 현 구현의 정합성 매트릭스: `docs/archive/analysis/CKS-SPEC-COMPLIANCE.md`
- 현 baseline + 변경 이력: `docs/archive/analysis/SELF-GRAPH-BASELINE.md`, `docs/archive/analysis/SELF-GRAPH-COMPARISON.md`
- 빌드 파이프라인 상세: `docs/archive/analysis/GO-PROJECT-BUILD-FLOW.md`, `docs/archive/analysis/TS-SOL-BUILD-FLOW.md`
- MCP query flow + 알고리즘: `docs/archive/analysis/MCP-QUERY-FLOW.md`
- Eval framework: `docs/archive/analysis/EVAL-FLOW.md`
- 전체 schema (33 NodeType × 32 EdgeType, schema 1.6): `docs/SCHEMA.md`

**End of self-verification manual.**
