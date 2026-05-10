# Session Hand-off — 2026-05-10

다음 세션이 cold start 가능하도록 정리한 문서. 직전 핸드오프(`docs/SESSION-HANDOFF-2026-05-08.md`) 이후 진행된 모든 작업과 결정의 요약.

기준점: branch `main`, HEAD `218008a` (feat: bench-mcp — in-process MCP tool latency baseline).

---

## §1. 이번 세션 commits (시간 역순)

| SHA | 제목 | 핵심 |
|------|------|------|
| `218008a` | feat(cmd,mcp): bench-mcp — in-process MCP tool latency baseline | `mcp.NewBenchHandlers` + `cmd/ckg/bench-mcp` 신규. 8 tools 측정. Live: find_symbol 0.07ms / search_text 0.75ms / get_context 7ms / evidence 172ms p50. HTTP vs in-process 40x discrepancy (evidence) flagged for trace |
| `1a79cbc` | perf(server): cache + pre-warm /api/edges/counts | EXPLAIN QUERY PLAN: 이미 covering index 사용 — 1.98M row scan 자체가 비용. cachedManifestStore에 lazy `EdgeCountsByType` cache + boot goroutine prewarm. edges.counts p50: 152→**0.10ms** (−99.9%) / p99: 755→**0.31ms** (−99.96%). 3 unit |
| `302b703` | perf(server): debounce computeStaleness git spawn | 5s TTL `stalenessCache` (sync.Mutex + key/expires). manifest p50: 235→64→**26ms** (baseline 대비 −89%). 3 unit (HitWithinTTL / RefreshAfterTTL / KeyInvalidation) |
| `473f839` | perf(server): manifest caching + ticket-index pre-warm | `cachedManifestStore` wrapper + boot goroutine. before/after: manifest p50 −73% / tickets p50 −90% & p99 −99.4% / evidence.intent p50 −97% / evidence.and p50 −98%. 2 unit |
| `1d175a8` | feat(cmd): bench-server — /api/* p50/p95/p99 baseline harness | `ckg bench-server` 신규 (in-process httptest, 12 probes). `docs/PERF-BASELINE-2026-05-10.md` 첫 측정 결과 (manifest 235ms hottest slow / search 0.6ms hottest fast / tickets p99 5.7s = cold start). 4 unit + 라이브 |
| `1dbe2e0` | chore(evidence,viewer): cleanup x3 — null hits / mode coverage / pill tooltip | (1) BuildPack nil → `[]Hit{}` (외부 client JSON 호환) (2) AND+SeedQname / issue-only ignores mode 단위 (3) MCP evidence_for_intent in-process mode propagation 검증 (4) pill `title` tooltip. 5 tests / 라이브 OK |
| `51cd1c7` | feat(evidence): top_files hint per sample commit in TicketIndex | `CommitInfo.TopFiles` (omitempty) + `topFilesForCommit` count-desc/name-asc top-3 directory rollup. viewer ticket panel pill 렌더 + (root) 폴백. HTTP + Playwright 라이브 검증, GH-66 → ["crypto/secp256k1/...", "consensus/qbft/core", ...] |
| `f1e2609` | docs: verification checklist + viewer hydration pattern guide | `docs/VERIFICATION-CHECKLIST.md` (4축 surface / 조합 매트릭스 / negative path / PR-ready checklist) + `docs/HYDRATION-PATTERN.md` (React #418 anti-pattern + `usePersistedState` 사용법 + 1-frame flash 트레이드오프) |
| `85af082` | test(evidence,server): close mode=and verification gaps | AND+IssueID 조합 단위 + HTTP `/api/evidence?mode=and\|invalid\|empty` 3종 wiring 테스트. 라이브 재검증 HTTP AND=0 / OR=5 / invalid=400 |
| `2982864` | feat(evidence): mode=and toggle for precise term-match queries | `Options.Mode` ("or" default / "and") + BM25 후 `containsAll` 후처리. /api/evidence + MCP + CLI `--mode` 모두 wired. 단위 테스트 2 + 라이브 OR/AND 차이 확인 |
| `5df3ed8` | test(mcp): lock §11.3 boundary across all 8 MCP tools | 12 wrapper 메서드 AMBIGUOUS leak guard (table-driven) + Run() 8 register* 정적 scan. fakeStore 10 method 확장. negative case 검증 |
| `5f2cf21` | feat(cmd): ckg evidence — H3 EvidencePack assembler from the CLI | `ckg evidence --graph DIR (--intent T \| --issue ID) [--seed-qname Q] [-k N] [--budget N] [--offset N] [--format text\|json]` — `ckg serve` 없이 shell/CI에서 EvidencePack 생성. text/json 출력, 5 unit tests + 9 시나리오 라이브 검증 |
| `72ea194` | test(buildpipe,temporal): H3+H4 regression nets | 실제 git fixture 위에 `BuildPack` 통합 테스트 (intent / issue_id / offset paging / §11.3 leak guard) + 30-subject H4 corpus precision/recall 100% lock-in |
| `fa59535` | fix: eliminate React #418 hydration mismatch | `usePersistedBool/Number/JSON` 신규 hook, 8 컴포넌트 + store `hydrateFromStorage()` 마이그레이션 — SSR-safe default + mount-effect로 stored value 적용 |
| `da4af1d` | refactor: extract EvidenceView component + diff line colouring | `EvidenceView.tsx` 신규 분리, `+`/`-`/`@` 첫 char 기반 라인 color (green/red/blue) — `<pre>` 안 `<span>` 으로 monospace+copy 유지 |
| `693c643` | pagination + NodeDetail pill → ticket EvidencePack loop | `Options.Offset` + viewer "load more" 버튼; NodeDetail amber pill → store 시그널 → TicketIndex 자동 expand+scroll+load |
| `a6210d1` | issue_id filter for /api/evidence + ticket→patches viewer flow | `Options.IssueID`; IssueID-only=recency / +Intent=BM25 교집합 / +SeedQname=modifies-reach 추가 필터 |
| `ff7ff9b` | ticket index — H4 issue-id rollup surface | `/api/tickets` + `Cache.TicketIndex(limit)` + viewer TicketIndex 패널 (purple/blue accent) |
| `a3fe80e` | cache BM25 corpus across BuildPack calls (~28x speedup) | manifest-keyed (`BuildTimestamp+SrcCommit`) `evidence.Cache` + sync.RWMutex 더블체크 락 |
| `d1be6b2` | H4 issue-id extraction — commit subjects → Hunk doc_comment | 4 정규식 (GH-#, [PROJ-N], JIRA prefix, GitHub URL) → `issues:GH-42;JIRA-7` encoding |
| `197d786` | H3 EvidencePack assembler — evidence_for_intent | BM25 over (subject \|\| patch \|\| modifies-qnames) 가상 문서, K-Top 후 budget cap |
| `7567c71` | H2 modifies edge — Hunk → CodeNode AST overlap | 13-NodeType 화이트리스트로 modifies 적용 (Function/Method/Type/etc.) |
| `2af1259` | Recovery panel for AMBIGUOUS unreachable history | viewer Recovery 패널 (amber accent), `/api/nodes/ambiguous` |
| `452552a` | H3 retrieval boundary — AMBIGUOUS Hunk/Commit hidden from LLM | `llmSafeStoreReader` wrapper로 모든 read-path tool에서 AMBIGUOUS 차단 |
| `6eec88d` | unreachable hunk collection — schema 1.8 §11.3 follow-up | `git reflog --all` + `git fsck --no-reflogs --unreachable` 합성, HEAD-reachable 차감 → AMBIGUOUS confidence |
| `84f4f2c` | TS statement-level nodes | IfStmt/LoopStmt/SwitchStmt/ReturnStmt/CallSite + PendingRef anchor |
| `6d312c5` | Tier 2 graphify-inspired ergonomics + accuracy | `--minimal` JSON, EXTRACTED-only filter |
| `1282e3e` | Tier 1 graphify-inspired ergonomics | god-node filter 강화, report 명령 |
| `12cfbc8` | TS function body walk P3 — calls PendingRef | tree-sitter body 순회, CallSite anchor (Go-parity) |
| `7d70f0a` | graphify-style CLI UX + viewer default filter relaxation | viewer 기본 노드 타입 확장 (Field/Variable/Constant/Goroutine 등) |
| `5a34126` | H1 Hunk graph — schema 1.8 | `Hunk` 노드 + `has_hunk`/`adjacent` 엣지, gzip 압축 patch blob, 64KB cap |

---

## §2. schema 1.8 §11 결정 8개 — 결과

§11 결정들은 H1을 시작하기 전 사용자와 합의한 8개 항목으로, 모두 main에 반영됨.

| §11.x | 항목 | 결정 / 구현 |
|-------|------|------|
| §11.1 | patch 저장 방식 | gzip 압축 (~70% 감소) |
| §11.2 | 동일 hunk 중복 dedup | 미적용 (단순성 우선) |
| §11.3 | unreachable 처리 | **3-layer hybrid**: storage AMBIGUOUS / LLM-filter wrapper / human Recovery 패널 |
| §11.4 | 대상 확장자 | 빌드 시 indexed 모든 파일 (제한 없음) |
| §11.5 | cross-repo dedup | out of scope |
| §11.6 | blob 최대 크기 | 64KB cap |
| §11.7 | PageRank 제외 | Hunk + Commit 모두 제외 |
| §11.8 | manifest 노드 추가 | 추가 안 함 (manifest는 별도 테이블) |

§11.3 hybrid의 검증 (직접 측정):
- 쿼리 "release merge dev"는 EXTRACTED 3 commits 반환
- AMBIGUOUS "release: merge dev to master (#80)" 는 노출 안 됨 (LLM-filter wrapper 동작)
- Recovery 패널에서는 정상 표시 (인간 surface)

---

## §3. viewer 패널 현황 (8 + 1 = 9 패널)

| 패널 | 상태 | 색상 | 데이터 소스 |
|------|------|------|------|
| Topbar (search, home, back) | ✅ | — | — |
| Canvas (3D/2D) | ✅ | per node type | `/api/nodes/top` boot, `/api/edges` 1-hop |
| Canvas legend (node shape + edge dash) | ✅ | — | static |
| NodeList (visible nodes) | ✅ | — | store derived |
| NodeDetail | ✅ | — | `/api/blob/{id}`, `/api/impact` |
| **NodeDetail issue pills** | ✅ amber 클릭 가능 | amber | Hunk.doc_comment `issues:` |
| EdgeFilters / NodeTypeFilters | ✅ | — | static |
| **Recovery panel** | ✅ | amber | `/api/nodes/ambiguous` |
| **TicketIndex panel** | ✅ | purple/blue | `/api/tickets` + `/api/evidence?issue_id=` |

H4/H3 cross-panel loop:
1. NodeDetail에서 Hunk 노드 선택 시 amber issue pill 렌더
2. pill 클릭 → store `selectedIssueID` 시그널
3. TicketIndex가 watch → 패널 강제 expand → 매칭 row 자동 open → `/api/evidence?issue_id=` 자동 fetch → 스크롤 인투뷰
4. EvidenceView에서 commits + hunks + patch_text 인라인 표시
5. "▾ load more" 버튼으로 다음 page (offset 기반) 추가 로드 가능
6. 빈 응답 시 버튼 자동 사라짐

---

## §4. 환경 / 검증된 graph.db

각 graph.db는 다른 commit/feature 검증용. 가장 큰 (go-stablenet) 그래프가 H4 평가 기준.

| 경로 | 소스 | 노드 | 엣지 | 비고 |
|------|------|------|------|------|
| `/tmp/ckg-h4` | go-stablenet | 243K | 1.98M | H4 issue-id 검증 graph (GH-66 = 501 hunks 등) |
| `/tmp/ckg-tsstmt` | (TS test) | small | small | TS statement-level 노드 검증 |
| `/tmp/ckg-h2` | go-stablenet 일부 | medium | medium | H2 modifies 검증 |
| `/tmp/ckg-self` | CKG 자체 | small | small | self-graph 평가 |
| `/tmp/ckg-tier2` | — | — | — | Tier 2 lean json 검증 |

**서버 띄우는 법** (--graph 는 directory, graph.db 가 아님):
```bash
./bin/ckg serve --graph /tmp/ckg-h4 --port 8765
```

**MCP stdio**:
```bash
./bin/ckg mcp --graph /tmp/ckg-h4
```

---

## §5. 주요 API + 데이터 형식

### `/api/evidence`
- 파라미터: `intent`, `issue_id`, `seed_qname`, `k`, `budget_tokens`, **`offset`**
- 적어도 `intent` 또는 `issue_id` 중 하나 필요 (둘 다 비어있으면 400)
- Combinations:
  - `intent` 만: BM25 ranking (기존)
  - `issue_id` 만: ticket 전체 footprint, recency desc
  - `intent + issue_id`: BM25 ranking을 ticket으로 교집합
  - `seed_qname` 추가: modifies-reach 1-hop 필터 (G3 calls/invokes 양방향)
  - `offset`: recency 정렬 후 N commit skip

### `/api/tickets`
- 파라미터: `limit` (default 100)
- 반환: `[{issue_id, hunk_count, commit_count, sample_commits[3]}]`
- hunk_count desc → commit_count desc → issue_id asc 정렬

### `/api/nodes/ambiguous`
- AMBIGUOUS confidence Hunk + Commit (unreachable history)

### MCP tool `evidence_for_intent`
- 파라미터: `intent`, `seed_qname`, `issue_id`, `k`, `budget_tokens`, **`offset`**
- 모든 read-path tool은 `llmSafeStoreReader` wrapper 통해 AMBIGUOUS 차단

---

## §5.1 schema 1.8 노드 / 엣지 카운트

- 노드 타입: 34개 (Hunk = #33, NodeCommit = #21 등)
- 엣지 타입: 35개 (modifies 추가, has_hunk/adjacent 추가)
- Confidence enum: `EXTRACTED` / `INFERRED` / `AMBIGUOUS`

---

## §6. 다음 세션 후보

상세 후보는 `docs/NEXT-CANDIDATES-2026-05-10.md` 참조 (10개 항목, 항목별 상세 분석).

이번 세션에서 #1 (페이지네이션) + #2 (NodeDetail pill loop) 가 완료됐으므로, 우선순위 재정렬 후 남은 후보:

| 순위 | 항목 | 비고 |
|------|------|------|
| ✅ | ~~#4 EvidenceView 컴포넌트 분리~~ | 완료 (`da4af1d`) — diff coloring 동봉 |
| ✅ | ~~#3 eval harness H3+H4 시나리오~~ | 완료 (`72ea194`) — 위치는 `internal/buildpipe/` + `internal/temporal/`, eval/은 LLM benchmark 전용이라 schema 불일치 |
| ✅ | ~~#6 `ckg evidence` CLI subcommand~~ | 완료 (`5f2cf21`) — 9 시나리오 라이브 검증 |
| ✅ | ~~#5 MCP 8 tools 통합 테스트~~ | 완료 (`5df3ed8`) — 12-method wrapper boundary + 8-tool register static scan |
| ✅ | ~~#10 OR/AND mode~~ | 완료 (`2982864` + `85af082`) — Options.Mode + BM25 post-filter, AND+IssueID 조합까지 lock-in |
| ✅ | ~~검증 체크리스트 + hydration 패턴 docs~~ | 완료 (`f1e2609`) — `docs/VERIFICATION-CHECKLIST.md` + `docs/HYDRATION-PATTERN.md` |
| ✅ | ~~#9 sample_commits top-files 메타~~ | 완료 (`51cd1c7`) — TopFiles + viewer pill, VERIFICATION-CHECKLIST §1 첫 적용 사례 |
| ✅ | ~~그룹 A cleanup × 3~~ | 완료 (`1dbe2e0`) — hits=null cleanup + mode 남은 3 검증 + pill tooltip 모두 한 commit |
| ✅ | ~~#7 성능 baseline~~ | 완료 (`1d175a8`) — `ckg bench-server` + `docs/PERF-BASELINE-2026-05-10.md` 첫 측정 |
| ✅ | ~~manifest 캐싱~~ | 완료 (`473f839`) — `cachedManifestStore` wrapper, p50 −73% (235ms→64ms) |
| ✅ | ~~tickets cache 사전 워밍~~ | 완료 (`473f839`) — boot goroutine, p50 −90% & p99 −99.4% (5775ms→34ms). evidence 모든 surface 부수 효과로 -97%까지 |
| ✅ | ~~computeStaleness 디바운스~~ | 완료 (`302b703`) — 5s TTL stalenessCache, manifest p50 64→26ms |
| ✅ | ~~edges.counts cache + pre-warm~~ | 완료 (`1a79cbc`) — cachedManifestStore.EdgeCountsByType + prewarmEdgeCounts. p50 152→0.1ms / p99 755→0.31ms |
| ✅ | ~~bench-mcp (in-process)~~ | 완료 (`218008a`) — 8 tools p50/p99 측정. evidence HTTP vs in-process 40x 격차 발견 (follow-up trace) |
| 1 | evidence_for_intent HTTP vs in-process 격차 trace | Low — `218008a` 가 발견. 동일 cache인데 HTTP 4ms / in-process 172ms 원인 미상 |
| 2 | bench-mcp-stdio (JSON-RPC framing) | Mid — stdio framing 비용 attribution. in-process 결과로 graph layer dominated 확인됐으니 framing 측정 가치 낮음 |
| 3 | schema 1.9 design / next-gen surfaces | High — 별도 세션 권장 |

---

## §7. 미해결 / 알려진 한계

- `/api/evidence` 에서 `offset >= len(commits)` 시 JSON `hits: null` 반환 — frontend `asArray()` 가 coerce 하므로 viewer는 정상 동작하나 외부 클라이언트(curl/python)에서 `null.length` 접근 시 에러. 후속 cleanup 후보.
- TS body walk P3가 `function-expression` / `arrow-function` 의 일부 nested 형태에서 enclosing 식별 누락 가능성 — 검증된 케이스는 모두 통과하나 edge case 가능.
- `/api/search` FTS가 일부 graph 에서 활성화 안 됨 (페이지네이션 검증 시 발견; FTS 활성화 단계 미수행 그래프). `ckg build` 의 FTS 인덱싱 옵션 검토 필요.
- viewer의 NodeDetail pill 클릭은 코드 리뷰 + setter+useEffect 표준 패턴으로 검증됨; live click 검증은 Hunk 노드를 캔버스에 노출하는 절차가 추가로 필요해서 이번 세션에선 unit + 페이지네이션 라이브 검증으로 대체.
- 모든 `localStorage`-backed UI 상태는 mount 후 1 frame 의 default-state flash 가 발생 — React #418 회피의 의도적 trade-off. 16-30ms 수준이라 사용자 인지 어려움. 다른 패널이 추가되면 동일 패턴 (`usePersistedBool/Number/JSON` 또는 `hydrateFromStorage`) 따라야 회귀 방지.

---

## §8. 빌드 / 테스트 명령어

```bash
# 전체 빌드 (TS + Go embed)
make build

# 전체 회귀
go test ./... -count 1

# 특정 패키지 빠른 회귀
go test ./pkg/evidence/... ./internal/server/... ./internal/mcp/... -count 1

# TS-only typecheck
cd web/viewer-next && npx tsc --noEmit
```

직전 회귀 (HEAD `218008a`): 23/23 패키지 PASS. `218008a` bench-mcp 신규 (in-process 측정). MCP tools live: find_symbol 0.07ms / search_text 0.75ms / get_context 7ms / evidence 172ms p50. PERF-BASELINE doc에 MCP tool latency 섹션 추가. 누적 perf 안정.

---

## §9. 참조

- 직전 핸드오프: `docs/SESSION-HANDOFF-2026-05-08.md` (직전 세션 시작점)
- 다음 후보 상세: `docs/NEXT-CANDIDATES-2026-05-10.md`
- 설계 문서: `docs/design/hunk-graph.md` (H1-H4, §11 결정 원본)
- 스키마: `docs/SCHEMA.md` (schema 1.8 정의)
- 새 hydration 패턴: `web/viewer-next/src/lib/usePersistedState.ts` (`usePersistedBool/Number/JSON`)
- H3+H4 회귀 안전망: `internal/buildpipe/h3h4_integration_test.go` + `internal/temporal/issueid_test.go::TestExtractIssueIDs_CorpusPrecisionRecall`
- evidence CLI: `cmd/ckg/evidence.go` (사용 예: `ckg evidence --graph /tmp/ckg-h4 --issue GH-66 -k 5 --budget 1000000 --format text`, `--mode and` 정밀 검색)
- MCP boundary 회귀 안전망: `internal/mcp/h3_filter_test.go::TestLLMSafeStoreReader_AllReadMethods_DropAmbiguousMeta` + `internal/mcp/server_test.go::TestRunRegistersAllEightTools`
- 검증 체크리스트: `docs/VERIFICATION-CHECKLIST.md` (PR-ready 워크플로 + 5종 누락 패턴 카탈로그)
- viewer hydration 패턴: `docs/HYDRATION-PATTERN.md` (React #418 anti-pattern + 8 마이그레이션 사례)
- 성능 baseline: `cmd/ckg/bench_server.go` + `cmd/ckg/bench_mcp.go` + `docs/PERF-BASELINE-2026-05-10.md` (사용 예: `ckg bench-server` 또는 `ckg bench-mcp --graph /tmp/ckg-h4 --iterations 50 --concurrency 4`)
- MCP in-process bench: `internal/mcp/bench.go::NewBenchHandlers` (8 tools handler map exposed for cmd/ckg)
