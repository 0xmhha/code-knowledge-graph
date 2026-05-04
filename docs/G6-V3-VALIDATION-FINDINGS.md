# G6 v3 — Validation Findings (session boundary, 2026-05-04)

> **Audience**: 다음 세션 cold-read. 이 문서 + `docs/G6-INCREMENTAL-REDESIGN.md`
> + `docs/HANDOFF.md` 세 개를 순서대로 읽으면 G6 v3 implementation의 진척
> 상태와 § 7 validation gate 측정 결과, 그리고 다음 행동 분기를 모두 파악
> 가능. **이 문서는 working tree dirty 상태를 전제**로 한다 — 11 modified
> files (uncommitted) 위에 다음 세션이 진입.

| Field | Value |
|---|---|
| Snapshot date | 2026-05-04 |
| Working tree HEAD | `b9c15f0` (`docs: refresh handoff for next session`) |
| Working tree state | **dirty**, 11 modified files (583 ins / 176 del), uncommitted |
| Branch | `main` |
| Working machine | `wm-it-22-00661` (`/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph`) |
| Corpus | `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` (1259 .go + 320 .ts + 563 .sol = 2142 files; commit `0bf2f4d1b`) |
| Schema bump | 1.4 → **1.5** (pending_refs table) — `internal/buildpipe/cache.go:34` |
| Test gate | `go vet ./...` clean, `go test ./...` 17 packages PASS |
| § 7 result | § 7.1 ❌, § 7.2 ❌, § 7.3 ❌, § 7.4 ✅ |
| Synthetic smoke | ✅ PARITY (8 files, 1-file edit) |
| Routing change | `pipeline.go:127` partial-hit → `runIncremental` (was cold-fallback) |

---

## 0. THIS SESSION — Diagnostic results (2026-05-04, session 2)

H1-H4 diagnostics completed. Root cause confirmed:

| Hypothesis | Result | Evidence |
|---|---|---|
| H1 qnames drift | REFUTED | nodes dump diff = 0 (byte-identical) |
| H2 round-trip drift | REFUTED | 117,793 pending_refs, 0 trailing-space violations, hex clean |
| H3 qIndex winner non-determinism | **CONFIRMED (primary)** | partial `file_path=''` edge counts match § 7.2 bucket diff exactly: calls +1902, has_modifier +524, emits_event +224, writes_mapping +30 |
| H4 cross-file edge loss | CONFIRMED (secondary) | `reloadCachedEdges` drops cached_src→dirty_dst edges; -5 imports |

Root cause (H3 detail): `NodesByFilePath` returns nodes in DB rowid/ID-sorted order;
cold `Resolve()` processes nodes in AST declaration order. For ambiguous simple names with
multiple candidates, the qIndex winner differs between cold and partial paths. Same pending
refs resolve to different Dst nodes; both edges survive dedup because Dst differs.

Fix direction (future v4): Sort `NodesByFilePath` by `start_line ASC` to match declaration order.
D4 escape hatch executed; routing reverted to cold-fallback. Pending_refs infra preserved as dead code.

---

## 1. Quick start (cold-read, 5분)

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
git status                                  # 11 modified files (uncommitted)
git diff --stat
go vet ./...                                # clean
go test ./...                               # 17 packages PASS
make build                                  # full build incl. viewer
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth   # PARITY
```

상위 reference 문서 (priority 순):
- 본 문서 — § 7 측정 + 가설 4개 + 다음 분기
- `docs/G6-INCREMENTAL-REDESIGN.md` — v3 design spec (§ 4 architecture, § 7 gate, § 8 decisions)
- `docs/HANDOFF.md` — project-wide 세션 경계 snapshot

---

## 2. v3 implementation — 어디까지 들어갔나

### 2.1 변경된 파일 (11개)

| File | Δ | 주요 변경 |
|---|---|---|
| `internal/persist/schema.sql` | +19 | `pending_refs` 테이블 + `idx_pending_refs_file` (FK CASCADE on `src_id`) |
| `internal/persist/sqlite.go` | +147 / −9 | `PendingRefRow` struct, `InsertPendingRefs` (INSERT OR IGNORE), `PendingRefsByFilePath`, `QueryEdgesForNodes` 청크화 (400 ids/chunk, Q5 fix) |
| `internal/persist/store_interface.go` | +8 | StoreReader.PendingRefsByFilePath, StoreWriter.InsertPendingRefs |
| `internal/persist/sqlite_extra_test.go` | +86 | 3 신규 테스트: InsertAndReload, CascadeOnNodeDelete, PrimaryKeyDeduplicates |
| `internal/graph/builder.go` | +37 / −3 | edge dedup `(Type, Src, Dst, Line)` keep-first |
| `internal/graph/builder_test.go` | +50 | 2 신규 테스트: KeepFirst, DifferentLineKept |
| `internal/buildpipe/cache.go` | +9 / −4 | `SchemaVersion` 1.4 → 1.5 |
| `internal/buildpipe/cache_test.go` | +6 / −4 | 하드코딩 "1.4" → `buildpipe.SchemaVersion` |
| `internal/buildpipe/language_runners.go` | +35 / −6 | 모든 cold pipeline (Go/TS/Sol) → `[]PendingRefRow` 반환; `collectPendingRefs` helper |
| `internal/buildpipe/pipeline.go` | +75 / −52 | `emitDerivedPasses` 통합 helper (xlang/temporal/cluster/score), `runCold` 분리, partial-hit → `runIncremental` 라우팅 복원 |
| `internal/buildpipe/incremental.go` | +130 / −91 | runIncremental 재배선: `(1)` 항상 재계산되는 edge type drop → `(2)` dirty/removed file CASCADE → `(3)` Pass 1 + cached pending_refs reload → `(4)` graph.Build 입력 시 reloaded 먼저 prepend → `(5)` emitDerivedPasses → `(6)` persist + dirty pending_refs INSERT |

### 2.2 design 대비 빠진 것

`docs/G6-INCREMENTAL-REDESIGN.md` § 4 와 비교:

| 항목 | 상태 |
|---|---|
| § 4.2 pending_refs schema | ✅ 구현 (FK CASCADE + 복합 PK + idx_file) |
| § 4.3 graph.Build dedup | ✅ 구현 (keep-first; tie-breaker는 spec의 "Count 합산"이 아니라 "first wins"로 변경 — `builder.go:34-35` 주석 참조) |
| § 4.4 unified emitDerivedPasses | ✅ 구현 (`pipeline.go:24-58`) |
| § 4.1 routing 복원 | ✅ 구현 (`pipeline.go:127`) |
| Q1 incremental_test.go bucket-by-(edge_type, src_node_type, dst_node_type) | ❌ 미작성 |
| Q4 TestNodeIDStability_CrossFileRename | ❌ 미작성 |
| Q5 reverse-suffix index in Resolve | ❌ 미실시 (D2 — measure first, then optimise. measure 끝남, fail) |
| § 9 medium fixture (200 files) | ❌ 미생성 |

### 2.3 keep-first 결정의 근거 (spec 대비 이탈)

`builder.go:25-35` 주석 발췌:

> **Tie-breaker is keep-first, NOT count summation, because cold builds
> produce Edge.Count=1 for every edge** (verified empirically on go-stablenet:
> 317614 edges all have count=1) — summing would inflate counts under
> partial and break § 7.1 parity with cold.

이건 spec § 4.3 ("multiplicity preserved via Count summation")을 measure 후
의도적으로 reverse한 결정. spec 코드 예시는 `existing.Count += e.Count`를 함;
실제 코드는 `if seenEdge[k] { continue }`만 함. 다음 세션이 spec와 코드 사이의
이 불일치를 보면 의도된 변경임을 인지할 것.

---

## 3. § 7 validation gate — 측정 raw data

### 3.1 측정 환경

| | |
|---|---|
| Corpus | go-stablenet-latest @ `0bf2f4d1b` |
| Total files | 2142 (Go 1259 / TS 320 / Sol 563) |
| Touched file | `rpc/subscription.go` (Go file, append `// noop <unix-ts>` line) |
| Revert | `git checkout -- rpc/subscription.go` (corpus repo는 깨끗 상태로 복원됨) |

### 3.2 빌드 시간

```
[reference cold]   build --src=$STABLENET --out=/tmp/g6v3/cold       --no-cache    → 60.04 s
[partial base]     build --src=$STABLENET --out=/tmp/g6v3/partial    --no-cache    → 62.72 s
[partial rebuild]  echo // noop >> rpc/subscription.go && build --src=... --out=/tmp/g6v3/partial   → 115.44 s   ❌
[cold final]       git checkout -- rpc/subscription.go && build --src=... --out=/tmp/g6v3/coldfinal --no-cache  → 58.97 s
```

**§ 7.3 budget**: ≤ 3 s — **115 s 측정, 38× over**. v2 (30 min)보다는 16× 빠름.

### 3.3 § 7.1 parity diff

| | partial | coldfinal | Δ |
|---|---|---|---|
| nodes | 217233 | 217233 | 0 ✅ |
| edges (total) | 664287 | 661612 | **+2675 ❌** |

### 3.4 § 7.2 bucket diff (전체)

```
                       coldfinal   partial    Δ
accessed_under_lock         1247      1247    0
acquires_lock                781       781    0
blame                       1319      1319    0
calls                      89999     91901   +1902   ← worst
changed_in                344946    344946    0
contains                  165797    165797    0
defines                    44189     44189    0
emits_event                  294       518   +224
handles_message               57        57    0
has_modifier                 597      1121   +524
imports                     9097      9092    -5    ← partial loses some
recvs_from                  1331      1331    0
releases_lock                834       834    0
sends_to                     601       601    0
spawns                       483       483    0
writes_mapping                40        70    +30
```

순수 "partial 추가 emit" 6 buckets, 순수 "partial loss" 1 bucket (imports −5).

### 3.5 § 7.4 audit (PASS)

```
db files:    1259
in both:     1259
in build only (missing from DB): 0
in db only (over-included):       0
verdict: PARITY
```

사용자 4 완성도 조건 #1 (모든 파일이 DB화)는 partial path에서도 만족.

### 3.6 over-emit 구체 사례 (가장 informative)

partial DB에만 존재하고 coldfinal DB에는 부재한 calls edge:

```sql
SELECT src,dst,type,file_path,line,count FROM edges
 WHERE src='001425f4f746ef8b' AND dst='e10fb8b7c2a291dd';
-- partial:    001425f4f746ef8b | e10fb8b7c2a291dd | calls | (empty) | 209 | 1
-- coldfinal:  (no row)
```

```
src node:  Function  showNetwork                cmd/p2psim/main.go
dst node:  Method    simulations.Client.GetNetwork  p2p/simulations/http.go
pending_refs from src: (empty — pending_refs 테이블에 src_id=001425f4f746ef8b 없음)
```

핵심 특징:
- **cross-file, cross-package call** (cmd/p2psim/main → p2p/simulations)
- partial side에 file_path **빈 문자열** (cold-emitted edge는 `stampFilePath`가 채워야 함)
- `pending_refs.src_id` 매칭 행 없음 — 이 ref는 pending refs 경로로 들어온 게 아니라는 뜻 → Pass 2 Resolve가 직접 emit한 것 같음

전체 sample (partial only, cold absent) 5건:
```
001425f4f746ef8b -> e10fb8b7c2a291dd L209
007191b967a048ba -> f777c049ec1636c0 L27
00d4afc38b06e729 -> f777c049ec1636c0 L91
00e660a51fe2c649 -> f777c049ec1636c0 L444
01124760ad3de278 -> f777c049ec1636c0 L34
```

여러 src에서 동일 dst (`f777c049ec1636c0`)로 가는 edge들 — dst 노드는 hot symbol일 가능성.

### 3.7 unique src/dst 분포 (control)

partial과 coldfinal에서 src/dst 동일 pair의 multiplicity 분포는 일치 (top 5 같음):
```
2994abfd4aa2b7f4|a593c14977813300|116    (양쪽 동일)
439f4e91a6d33eb7|0112d9e7742fba40| 98    (양쪽 동일)
...
```
즉 같은 src→dst 쌍에서 line만 다른 edge는 dedup이 잘 되고 있고, 차이는 partial이 전혀 새로운 (src,dst) 쌍을 emit하는 데서 옴.

---

## 4. 가설 4개 (수렴 안 됨, 검증 방법 포함)

본 세션은 사용자 글로벌 룰 "원인 가설이 3개 이상으로 분기 → 보고 후 지시 대기"에
따라 여기서 멈춤. 다음 세션이 다음 4개 중 한 개씩 검증.

### H1 — qIndex 구성이 cold per-Pass와 partial merged Pass에서 다름

**가설**. cold path의 Go Resolve는 dirty Go 노드 + (없음)을 qIndex로 만들고 이 위에서
suffix-match. partial path의 Go Resolve는 cached Go 노드 (DB reload) + dirty Go 노드를
qIndex로 만들고 이 위에서 suffix-match. 만약 cached side reload가 cold-emit과
attribute-level로 미세하게 다르면 (e.g. `qualified_name` trailing space, `Name` case),
qIndex의 suffix 후보 집합이 달라져 Resolve가 다른 edge를 emit.

**검증 방법**.
```go
// internal/buildpipe/incremental_test.go (신규)
func TestQIndexParity_PartialVsCold(t *testing.T) {
    // 1. cold build on synthetic
    // 2. partial rebuild (1 dirty file)
    // 3. dump goPipeline's qIndex from both (need exposing test hook)
    // 4. assert key-set + value-set 일치
}
```
또는 **간이 진단**: cold/partial 양쪽에서 `SELECT id, qualified_name FROM nodes
ORDER BY id` 덤프를 sort/diff. 노드 attribute drift가 있으면 즉시 보임.

```bash
sqlite3 /tmp/g6v3/coldfinal/graph.db "SELECT id,qualified_name FROM nodes ORDER BY id" > /tmp/g6v3/cold.qnames
sqlite3 /tmp/g6v3/partial/graph.db    "SELECT id,qualified_name FROM nodes ORDER BY id" > /tmp/g6v3/partial.qnames
diff /tmp/g6v3/cold.qnames /tmp/g6v3/partial.qnames | head -20
```

만약 0-line diff면 H1 기각, 다른 가설로.

### H2 — pending_refs reload 시 target_qname text round-trip drift

**가설**. cold path가 `parse.PendingRef.TargetQName`을 그대로 메모리로 들고 Resolve에
전달. partial path는 동일 값을 SQLite에 INSERT → SELECT로 round-trip. SQLite
collation/encoding이 trailing/leading space, NFC vs NFD 등을 변환하면 cold와 다른
qname을 Resolve가 보게 됨. suffix-match는 정확한 substring을 쓰므로 1바이트 drift도
edge 수에 영향.

**검증 방법**.
```bash
# (Go REPL 또는 작은 test 작성)
# parse.PendingRef.TargetQName 메모리 문자열을 InsertPendingRefs → 
#   PendingRefsByFilePath로 round-trip 후 == 비교 (1000 rows 정도 sample).
```
또는 cold path에서 emit된 pending refs 1개를 골라 메모리 값과 DB 값을 hex dump로 비교:
```bash
sqlite3 /tmp/g6v3/coldfinal/graph.db "SELECT hex(target_qname) FROM pending_refs LIMIT 5"
```

### H3 — dedup 키에 file_path가 빠져 cross-file resolve 결과를 동일 edge로 인식 못함 (가장 유력)

**가설**. § 3.6의 over-emit 사례에서 `file_path` 빈 값. cold path는 Pass 2 Resolve 후
`stampFilePath`로 edge에 file_path를 찍음 (`language_runners.go:88`). 그런데 incremental
경로의 reloaded edges는 `EdgesByFilePath`로 가져오므로 file_path가 채워져 있음.
graph.Build dedup 키는 `(Type, Src, Dst, Line)` — file_path는 키에서 빠짐, 따라서 같은
src→dst→line의 두 edge가 다른 file_path여도 첫 것만 들어감 → 키 비교는 통과.

그런데 § 3.6 사례는 partial에 **file_path 빈값** edge가 들어감. 이건 fresh resolve 결과인데
stampFilePath를 못 거친 것. cold가 emit하는 같은 edge는 file_path로 stamp되어 DB에
들어가는데, partial은 reloaded path와 fresh path 양쪽에서 같은 src→dst를 만들고도
fresh side를 dedup이 못 잡았다는 뜻. **즉 dedup 키 자체가 너무 좁다는 게 아니라
fresh side의 edge가 cold가 emit하지 않는 추가 edge**.

이 가설을 정확히 말하면: **partial path의 Pass 2 Resolve가 cold path에서는 발화하지 않는
suffix-match를 발화시킨다**. H1과 사실상 동일 — H1을 더 좁힌 것.

**검증 방법**.
1. partial DB에서 fresh-side만 골라내기 (file_path = '' 인 edges):
   ```bash
   sqlite3 /tmp/g6v3/partial/graph.db "SELECT type, COUNT(*) FROM edges WHERE file_path='' GROUP BY type"
   ```
2. 이 숫자가 § 3.4 bucket diff의 partial-extra와 일치하는지 확인.
3. 일치하면 fresh-emit이 cold에 없는 edge들 — H1/H3 결합 (qIndex+stamp 둘 다 영향).

### H4 — incremental의 reloaded ResolvedGraph 입력 순서가 dedup 의도와 다름

**가설**. `incremental.go:147-153`:
```go
parts := make([]*parse.ResolvedGraph, 0, len(resolved)+1)
parts = append(parts, &parse.ResolvedGraph{Edges: reloadedFromDB})
parts = append(parts, resolved...)
g, err := graph.Build(parts)
```
prepend 의도는 "DB-resident edge를 keep-first로 우선". 그러나 `parse.ResolvedGraph`에는
`Nodes`도 같이 묶여야 dedup의 의미가 살아남. 위 코드는 reloadedFromDB를 Edges-only
ResolvedGraph로 만들어 prepend한다 — Nodes는 비어있음. 그 다음 resolved (per-language
ResolvedGraph: Nodes + Edges 다 들어있음)가 따라옴. 이때 graph.Build 내부의 노드 dedup
(`byID[n.ID] = n`)은 resolved의 Nodes를 마지막으로 본 것을 keep — 이건 cold와 동일하니
문제 없음. edge dedup은 첫 등장 keep — reloaded edges가 먼저 보이므로 keep-first 의도
대로 동작. **여기까지는 OK**.

문제는 reloadedFromDB가 cached files의 cross-file edges (특히 cached_src→cached_dst)
**전부**를 reload하지 않을 가능성. `reloadCachedEdges` (incremental.go:230-281) 구현을
다시 보고 cross-file edge 누락 여부 확인.

**검증 방법**.
```go
// reloadCachedEdges가 EdgesByFilePath만 쓰는가, QueryEdgesForNodes도 쓰는가?
// 만약 EdgesByFilePath만 쓰면 cached_src→dirty_dst edge는 dirty 쪽 file_path에 stamp되어
// dirty의 EdgesByFilePath는 dirty file path만 매칭 (cached file에서 stamp된 edge는 못 찾음)
```
실제 코드 트레이스가 5분 작업.

---

## 5. 다음 분기 (옵션 A / B / C)

### A — parity 고치기 (가설 검증 → 수정)

**작업 범위**: § 4 가설 1개씩 검증, dedup/qIndex/reload 중 진짜 원인 발견하고 fix.

**예상 비용**: 반나절~하루. H3가 정답이면 수 시간; H1/H4면 더 김.

**리스크**:
- parity 고쳐도 § 7.3 (3 s budget) 통과 못 할 가능성. 115 s에서 reverse-suffix index
  + chunked QueryEdgesForNodes 만으로 38× 단축은 빠듯 (Q5 영향 측정 필요).
- v1 + v2 + v3 모두 실패 시 § 8 D4 발동.

**판단 분기점**: parity 고친 후 § 7.3 측정 → < 3 s 면 ship; ≥ 3 s 면 D4.

### B — § 8 D4 escape hatch 발동 (partial-cache drop)

**작업 범위**:
1. `pipeline.go:127` 라우팅 cold-fallback로 되돌림 (현재 `runIncremental` 호출 → "Cache: partial hit; falling back to cold rebuild for correctness" 로그로).
2. `runIncremental` + `pending_refs` 관련 데이터 구조는 dead code로 보존 (B3 / C1 후속을 위한 자산).
3. `docs/INCREMENTAL.md` § "Phase 1 limitations"에 "partial-cache deferred until B3 (tree-sitter Tree.Edit) or C1 (reverse-reference index) prerequisite"로 기록.
4. `SchemaVersion` 1.5 → 1.4로 되돌리거나, 1.5는 유지하되 pending_refs는 v1의 dead column처럼 유지 (다음 시도 때 schema bump 비용 절약 — 권장).
5. `docs/G6-INCREMENTAL-REDESIGN.md` § 8 D4 항을 "EXECUTED 2026-05-XX"로 stamp.

**예상 비용**: 1-2시간 (코드 1 line 라우팅 변경 + 문서 3 군데 stamp + commit).

**리스크**: 작음. ship된 short-circuit (~1 s)는 그대로 살아있고 partial이 cold-fallback로
돌아가는 상태는 본 세션 시작 시점과 동일.

### C — WIP 보존 (다른 머신/세션 인계용)

**작업 범위**:
1. 11 modified files를 `wip/g6v3-attempt-1` branch로 commit (실패해도 명시).
2. main은 `b9c15f0` 그대로 유지.
3. 본 문서 + HANDOFF.md만 main에 commit.

**예상 비용**: 30분.

**리스크**: dead branch가 누적될 수 있으나 반-년 retention 정책으로 정리 가능.

---

## 6. 재현 명령어 (다른 머신 / 다음 세션)

```bash
# Prereq
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
STABLENET=/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest
mkdir -p /tmp/g6v3

# 1. Build & test
make build
go test ./...                                # 17 packages PASS

# 2. § 7.1+7.2 baseline (cold → cold)
rm -rf /tmp/g6v3/cold /tmp/g6v3/coldfinal /tmp/g6v3/partial
./bin/ckg build --src="$STABLENET" --out=/tmp/g6v3/cold --no-cache
./bin/ckg build --src="$STABLENET" --out=/tmp/g6v3/partial --no-cache    # partial base

# 3. Touch + § 7.3 partial rebuild (3s budget)
echo "// noop $(date +%s)" >> "$STABLENET/rpc/subscription.go"
START=$(date +%s%N)
./bin/ckg build --src="$STABLENET" --out=/tmp/g6v3/partial
END=$(date +%s%N)
echo "ELAPSED_MS=$(( (END-START) / 1000000 ))"

# 4. Revert + cold for parity
(cd "$STABLENET" && git checkout -- rpc/subscription.go)
./bin/ckg build --src="$STABLENET" --out=/tmp/g6v3/coldfinal --no-cache

# 5. § 7.4 audit
./bin/ckg audit --src="$STABLENET" --graph=/tmp/g6v3/partial

# 6. § 7.1 + § 7.2 diff
sqlite3 /tmp/g6v3/coldfinal/graph.db "SELECT type,COUNT(*) FROM edges GROUP BY type ORDER BY type" > /tmp/g6v3/coldfinal.buckets
sqlite3 /tmp/g6v3/partial/graph.db    "SELECT type,COUNT(*) FROM edges GROUP BY type ORDER BY type" > /tmp/g6v3/partial.buckets
diff /tmp/g6v3/coldfinal.buckets /tmp/g6v3/partial.buckets

# 7. 진단: partial-only edges
sqlite3 /tmp/g6v3/partial/graph.db    "SELECT src||' -> '||dst||' L'||COALESCE(line,0) FROM edges WHERE type='calls'" | sort > /tmp/g6v3/partial.calls
sqlite3 /tmp/g6v3/coldfinal/graph.db  "SELECT src||' -> '||dst||' L'||COALESCE(line,0) FROM edges WHERE type='calls'" | sort > /tmp/g6v3/coldfinal.calls
comm -23 /tmp/g6v3/partial.calls /tmp/g6v3/coldfinal.calls | wc -l    # 2099
comm -13 /tmp/g6v3/partial.calls /tmp/g6v3/coldfinal.calls | wc -l    #  197

# 8. Hypothesis H3 검증: file_path 빈값 edges
sqlite3 /tmp/g6v3/partial/graph.db "SELECT type, COUNT(*) FROM edges WHERE file_path='' GROUP BY type"
```

본 세션 raw measurements 모두 위 명령으로 재생산 가능.

---

## 7. 본 세션 진행 history

1. `git status` → 11 modified, HEAD `b9c15f0`. v3 implementation in progress on disk.
2. `go build ./...` clean, `go vet ./...` clean, `go test ./...` 17 packages PASS.
3. v3 코드 변경 분 review: schema 1.5 + pending_refs CRUD + edge dedup + emitDerivedPasses + routing.
4. 새 단위 테스트 PASS: TestPendingRefs_×3, TestBuildEdgeDedup_×2.
5. Synthetic smoke (testdata/synthetic + 1 file edit on `vault.go`) → audit PARITY.
6. § 7 gate on go-stablenet (1259 .go files):
   - 7.1 fail (+2675 edges)
   - 7.2 fail (6 buckets diverge)
   - 7.3 fail (115 s vs 3 s)
   - 7.4 pass (audit zero-diff)
7. 진단: showNetwork → Client.GetNetwork edge가 partial에만 존재 + file_path 빈값.
8. 가설 4개 도출 → 사용자 글로벌 룰 "3+ 가설 분기 시 stop" 발동, user에게 보고.
9. 본 문서 작성.

---

## 8. open questions (다음 세션이 결정)

1. 옵션 A/B/C 중 어느 것?
2. WIP 처리 방식 (commit on wip branch / git stash / 그대로 두기)?
3. § 7.3 budget 자체를 재검토 — 3 s는 § 7 spec의 명시 수치, 실제 dev 가치가 있는 임계는
   몇 초인가? (cold 60 s, short-circuit 1 s. 단일 파일 편집 → 5 s 또는 10 s도 acceptable
   할 수도. spec 갱신 시 § 8 D2 + D4 재해석 필요.)
4. Q5 reverse-suffix index를 별도 commit으로 먼저 land해 둘 가치 — partial이 안 가도
   cold rebuild에 영향 있을까? (Cold도 Pass 2 Resolve 돈다 → 이론상 영향 있으나 실제
   profile 안 함. 측정 후 결정.)

---

## 9. cross-references

- `docs/G6-INCREMENTAL-REDESIGN.md` — design spec, § 4 architecture / § 7 gate / § 8 decisions
- `docs/HANDOFF.md` — project-wide snapshot (본 세션 끝에 § 4.1 v3 진척 반영하여 갱신됨)
- `docs/WORK-PLAN.md` — group A-G 작업 tracker
- `docs/INCREMENTAL.md` — operator-facing partial-cache 동작 (D4 발동 시 갱신 대상)
- 본 세션 working tree diff: `git diff` 또는 § 2.1 표

---

**End of findings.** 다음 세션은 본 문서 + G6-INCREMENTAL-REDESIGN.md § 7 + HANDOFF.md
§ 4.1 세 곳을 cold-read 후 옵션 A/B/C 중 선택하여 진입.
