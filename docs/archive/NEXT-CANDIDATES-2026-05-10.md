# 다음 세션 작업 후보 — 2026-05-10

기준 commit: `a6210d1` (feat: issue_id filter for /api/evidence + ticket→patches viewer flow)

이 문서는 직전 commit 이후 가능한 다음 작업들을 사용자가 직접 선택할 수 있도록 항목별로 풀어 쓴 것입니다. 각 항목은 같은 형식을 따릅니다.

| 필드 | 의미 |
|------|------|
| **현재 상태** | 지금 코드/기능이 어디까지 와 있는지 |
| **문제 / 동기** | 왜 이 작업이 필요한가 |
| **제안 작업** | 무엇을 만들 것인가 (파일/접근 단위) |
| **대안 / Trade-offs** | 가능한 다른 길과 그 비용 |
| **검증** | 작업이 끝났는지 어떻게 확인하는가 |
| **공수** | Low (≤2h) / Mid (반나절) / High (1일+) |
| **의존성** | 다른 후보가 먼저 있어야 하는지 |

---

## 1. EvidencePack 페이지네이션 / cursor

**한 줄 요약**: 큰 ticket(GH-66 = 501 hunks)이 budget cap에 걸려 1 commit만 표시되는 문제 해결.

**현재 상태**
- `groupByCommit`은 cumulative `patch_text` 길이가 `budget_tokens`(viewer 기본 12000)를 넘으면 emit 중단 (`pkg/evidence/cache.go` `groupByCommit` 함수, 약 482-501 line).
- 첫 commit은 항상 emit하므로 GH-66의 첫 번째 commit이 budget을 단독 초과해도 표시.
- 하지만 두 번째 commit(`a857b5961c3b consensus metrics`)은 영원히 못 봄.
- 캐시는 manifest-keyed라 사용자가 viewer에서 다시 patches 버튼을 눌러도 같은 1 commit 반환.

**문제 / 동기**
- 사용자가 "GH-66 관련 변경 전부 보여줘"라는 의도를 표현했지만 일부만 받음.
- 큰 PR이 많은 monorepo(go-stablenet 같은)에서 가장 가치 있는 ticket일수록 가장 보이지 않음.
- BM25 ranking이 없는 IssueID-only 모드에서는 budget 한계만이 cap이라 명시적 page 가 자연스러움.

**제안 작업**
- `pkg/evidence/evidence.go` `Options` 에 `Offset int` 추가 (`Offset >= len(commits)`이면 빈 결과).
- `pkg/evidence/cache.go` `groupByCommit` 에서 정렬 후 `commitOrder[opt.Offset:]` 으로 시작.
- `internal/server/api.go` `handleEvidence` 에 `offset` query 파라미터 파싱.
- `internal/mcp/evidence.go` MCP tool schema에 `offset` 추가.
- `web/viewer-next/src/components/TicketIndex.tsx` "Load more" 버튼 — 클릭 시 `offset += loadedCommitCount` 로 다음 페이지 fetch + `pack.hits` append.

**대안 / Trade-offs**
- (A) Offset-based: 깔끔. 단 commit 정렬이 안정적이어야 함 (현재는 author_time DESC + tie-break으로 안정).
- (B) Cursor-based (last-sha): tie-break 시 더 안전. 단 cache가 cursor-aware 해야 해서 복잡.
- (C) `budget_tokens=0` 무제한 모드: 단순하지만 큰 ticket은 응답이 수 MB가 됨 — 브라우저 / LLM context 모두 부담.
- (A)를 권장합니다. (확신도 중)

**검증**
- 단위 테스트: offset이 commit count를 넘으면 빈 hits.
- Playwright: GH-66 expand → patches 클릭 → "Load more" → 두 번째 commit (`a857b5961c3b consensus metrics`) 등장.

**공수**: Mid (확신도 중)
**의존성**: 없음.

---

## 2. NodeDetail issue-id pill 클릭 → filtered EvidencePack

**한 줄 요약**: Function 노드에서 표시된 issue id pill(amber)을 클릭하면 그 ticket의 EvidencePack을 즉시 띄우는 loop 닫기.

**현재 상태**
- `web/viewer-next/src/components/NodeDetail.tsx` 에 `parseIssueIDs` 와 amber pill 렌더링 로직이 이미 있음.
- pill은 단순 표시 — 클릭 핸들러 없음.
- TicketIndex 안에서만 ticket→patches flow 동작.

**문제 / 동기**
- 사용자가 "이 함수가 어떤 변경에서 손댔지?"를 보다가 issue id를 발견해도, ticket의 다른 hunks를 보려면 직접 TicketIndex 패널에서 다시 찾아야 함.
- 코드 → 변경 이력 → 같은 ticket의 다른 변경의 traversal이 끊겨 있음. 이 loop가 닫히면 "이 함수의 origin → 같은 PR의 다른 변경" 흐름이 한 번에 가능.

**제안 작업**
- `web/viewer-next/src/store/store.ts` 또는 새로운 zustand slice에 `selectedIssueID: string | null` 상태 추가.
- `NodeDetail.tsx` issue pill에 `onClick={() => setSelectedIssueID(id)}` 핸들러.
- `TicketIndex.tsx` 가 `selectedIssueID` 를 watch — 값이 들어오면 자동으로 해당 ticket을 expand + patches 로드.
- 또는 더 격리된 UX: `EvidenceDrawer.tsx` 새 컴포넌트, App.tsx 가 selectedIssueID 시 drawer 띄움.

**대안 / Trade-offs**
- (A) TicketIndex 통합: 코드 적음, 단 사용자가 "ticket 패널이 갑자기 뛰어 expand"되는 surprise 가능.
- (B) 별도 Drawer: UX 명확, 단 EvidenceView 컴포넌트 분리 (#4) 가 선행되어야 깔끔.
- (B) 권장. (확신도 중)

**검증**
- Playwright: 함수 노드 선택 → modifies hunks 확인 → issue pill 클릭 → drawer 열림 + ticket의 patches 표시.

**공수**: Low (확신도 높) — store + click handler + 작은 component.
**의존성**: (B)를 택하면 #4 EvidenceView 분리 선행 권장.

---

## 3. eval/ harness에 H3 + H4 시나리오 추가

**한 줄 요약**: schema가 1.9 등으로 진화할 때 H3 (EvidencePack) / H4 (issue-id 추출) 회귀를 자동 검출.

**현재 상태**
- `internal/eval/` 패키지가 있고 `go test ./internal/eval/...` 약 30s 소요 — 기존 graph 평가 시나리오 보유.
- H3 (BuildPack) 와 H4 (ExtractIssueIDs) 는 unit test (`pkg/evidence/*_test.go`, `internal/temporal/issueid_test.go`) 만 존재.
- 통합 검증 — 실제 build pipeline → store → BuildPack 까지의 end-to-end — 없음.

**문제 / 동기**
- AMBIGUOUS leak (예: confidence filter 조건 변경 시), BM25 ranking 변화, issue_id 정규식 false positive 등은 unit 단계에서 잡히지 않음.
- 미래에 §11.3 retrieval boundary 가 변경되거나 추가 confidence enum (`SUSPICIOUS` 등)이 들어오면 회귀 가능.

**제안 작업**
- `internal/eval/h3_evidence_test.go` 신규
  - 작은 git fixture repo 생성 또는 기존 graph.db (예: `/tmp/ckg-h4`) 를 testdata로 묶어 둠.
  - "intent=consensus metrics" 같은 알려진 query 의 expected commit SHA 와 비교.
  - AMBIGUOUS leak: confidence='AMBIGUOUS' commit/hunk 가 hits 안에 절대 없는지 assert.
- `internal/eval/h4_issueid_test.go` 신규
  - 알려진 commit subject 100+ 개 → ExtractIssueIDs 결과를 ground truth 와 비교.
  - precision/recall 메트릭 출력 — 임계 미달 시 fail.

**대안 / Trade-offs**
- (A) Fixture repo: 경량, 재현 가능. 단 fixture 유지 비용 (commit 추가 시 expected 도 갱신).
- (B) Live graph.db 캐시: 빠르고 현실적. 단 fixture 가 거대 binary 가 되어 git LFS 검토 필요.
- (A) 가 일반적 권장. (확신도 중)

**검증**
- `go test ./internal/eval/... -run H3` 통과.
- 의도적으로 evidence.go 에서 AMBIGUOUS filter 조건을 깨뜨려 보고 테스트가 정말 fail 하는지 negative check.

**공수**: Mid (확신도 중) — fixture 만드는 비용이 큼.
**의존성**: 없음.

---

## 4. EvidenceView 컴포넌트 분리

**한 줄 요약**: TicketIndex.tsx 안에 정의된 EvidenceView 함수를 별도 파일로 추출 — 다른 surface(NodeDetail drawer 등)에서 재사용.

**현재 상태**
- 이번 commit `a6210d1` 에서 `web/viewer-next/src/components/TicketIndex.tsx` 가 200+ 줄로 커짐.
- 같은 파일 안에 helper component `EvidenceView({ pack })` 가 정의됨.
- 외부에서 import 불가.

**문제 / 동기**
- #2 (NodeDetail issue pill loop) 에서 같은 patch 렌더가 필요하면 그대로 복붙 — DRY 위반.
- TicketIndex 단일 파일이 5+ 종 책임 (collapse, fetch tickets, expand row, evidence cache, evidence render) 보유.

**제안 작업**
- `web/viewer-next/src/components/EvidenceView.tsx` 신규
  - props: `{ pack: EvidencePack; compact?: boolean }`
  - compact 모드는 patch_text 첫 N줄만 (drawer 미리보기용)
- TicketIndex.tsx 에서 inline 함수 제거 + import.
- 가능하면 패치 색칠 (diff `+` / `-` 라인 색) 을 이 컴포넌트에 추가 — 모든 surface 가 동일 색상 일관.

**대안 / Trade-offs**
- 분리하지 않음: 한 곳에서만 쓰는 동안은 OK, #2 시점에 미루면 큰 비용 아님.
- 지금 분리: cleanup 빠름, 단 #2 작업이 안 들어오면 즉각적 가치 없음.

**검증**
- TicketIndex Playwright 회귀: GH-66 patches 표시 동일.
- TS typecheck pass.

**공수**: Low (확신도 높)
**의존성**: 단독 가능. #2 의 선결조건으로 자주 쓰임.

---

## 5. MCP 8 tools end-to-end 통합 테스트

**한 줄 요약**: 모든 MCP tool 을 같은 graph.db 에 대해 순차 호출하는 fixture 테스트 — wrapper / cache / tool간 상호작용 회귀 검출.

**현재 상태**
- `internal/mcp/` 의 각 tool 별 unit test 존재 (`evidence_test.go`, `tickets_test.go`, ...).
- 하지만 Server 레벨의 통합 (8 tools 가 같은 store + cache 를 공유) 검증 없음.
- llmSafeStoreReader wrapper 가 새 tool 추가 시 누락되어도 unit test 로는 못 잡음.

**문제 / 동기**
- §11.3 retrieval boundary 의 spirit ("LLM 은 AMBIGUOUS 못 봄") 는 모든 read-path tool 에 적용되어야 함.
- 미래에 9번째 tool 추가 시 wrapper 적용을 잊는 게 가장 흔한 실수 패턴.
- Cache 가 tool A 호출 후 tool B 호출 시 stale 가능 — manifest-key 변경 시 invalidation 동작 검증.

**제안 작업**
- `internal/mcp/integration_test.go` 신규
  - 작은 fixture graph (testdata/) 으로 MCPServer 띄움.
  - 8 tools 모두 표준 query 로 호출.
  - 각 응답에서 AMBIGUOUS 마커 가 절대 안 나오는지 assert (read-path tools 전부).
  - bench 옵션: 각 tool 평균 latency 출력.

**대안 / Trade-offs**
- 통합 테스트 없이 review checklist 로 대체: 가볍지만 사람 의존.
- 통합 테스트: 설정 비용 크지만 회귀 자동 차단.

**검증**
- `go test ./internal/mcp/... -run Integration` 통과.
- 의도적으로 evidence wrapper 한 줄 깨뜨리고 테스트가 fail 하는지 negative check.

**공수**: Mid (확신도 중)
**의존성**: 없음.

---

## 6. `ckg evidence` CLI subcommand

**한 줄 요약**: `ckg serve` 띄우지 않고도 shell에서 EvidencePack을 직접 받는 CLI 명령 — CI / 평가 / 디버그 편의.

**현재 상태**
- `cmd/ckg/` 에 path / benchmark / query / report / export-json / quickstart 명령 존재 (이전 세션들에서 추가).
- evidence 받으려면 반드시 `ckg serve` 띄우고 curl 또는 MCP client 연결 필요.

**문제 / 동기**
- shell script / CI pipeline 에서 evidence 패치를 단발성으로 가져오고 싶을 때 거추장스러움.
- 디버깅 시 같은 query 를 latency 기록하며 반복 호출하기 어려움.
- eval harness (#3) 도 직접 evidence.BuildPack 을 부르는 게 더 간단하지만, CLI 가 있으면 사용자 워크플로 도 정리됨.

**제안 작업**
- `cmd/ckg/evidence.go` 신규
  - flags: `--graph PATH` `--intent STRING` `--issue STRING` `--seed-qname STRING` `-k INT` `--budget INT` `--format json|text`
  - 내부적으로 evidence.NewCache().BuildPack 직접 호출.
  - JSON 또는 사람이 읽기 좋은 text(commit subject + 첫 5줄 patch).

**대안 / Trade-offs**
- CLI 없이 `ckg query` 확장: query 명령이 이미 다른 책임 가짐 — 혼동 위험.
- 별도 evidence 명령: 깔끔, 단 명령 수 증가.

**검증**
- `bin/ckg evidence --graph /tmp/ckg-h4 --issue GH-66 -k 5 --format text` 실행 → 1+ commit 출력.
- `cmd/ckg/registry_test.go` 의 `want` slice 에 "evidence" 추가 (TestSubcommandsRegistered).

**공수**: Mid (확신도 높)
**의존성**: 없음.

---

## 7. go-stablenet (1.9M edges) 성능 baseline

**한 줄 요약**: 가장 큰 실제 graph 에서 모든 /api/* 와 MCP tool 의 latency / memory profile 을 기록 — 미래 회귀 비교 기준.

**현재 상태**
- 이전 세션들에서 build pipeline 시간 (243K nodes / 1.98M edges 만들기) 만 측정.
- evidence cache hit 후 4s → 0.18s (28x) 한 번 측정됨.
- 다른 endpoint (/api/nodes/top, /api/impact, /api/edges, /api/hierarchy 등) 는 미측정.
- MCP tool 별 latency 미측정.

**문제 / 동기**
- 사용자 graph 가 더 커지면(예: chromium / linux 규모) 어디가 bottleneck 인지 모름.
- 이번 세션에서 cache / wrapper / 새 endpoint 추가 — 하지만 baseline 없으면 다음 세션 변경이 regression 인지 개선인지 비교 불가.

**제안 작업**
- `cmd/ckg/bench-server.go` 신규 또는 기존 `benchmark` 명령 확장
  - serve 자동 띄움 (또는 외부 URL 받음).
  - 각 endpoint 100회 sequential + 10 동시 호출.
  - p50 / p95 / p99 출력, JSON 으로 저장.
- 결과를 `docs/PERF-BASELINE-2026-05-10.md` 로 commit.
- 향후 PR 에서 같은 명령 돌려 비교 표 자동 생성 가능.

**대안 / Trade-offs**
- 수동 측정 (`hey`, `wrk`): 외부 도구 의존, OS 별 차이.
- 내장 bench: 재현 가능, 단 작성 비용.

**검증**
- 결과 JSON 이 stable (재실행 시 ±10% 이내) 한지 3회 돌려 확인.

**공수**: Mid (확신도 중)
**의존성**: 없음.

---

## 8. SESSION-HANDOFF-2026-05-10.md 작성

**한 줄 요약**: 이번 세션의 commits 와 결정사항을 다음 세션이 콜드 스타트할 수 있도록 한 문서로 정리.

**현재 상태**
- 직전 핸드오프: `docs/SESSION-HANDOFF-2026-05-08.md` (이번 세션 시작점).
- 이번 세션에서 main 에 들어간 주요 commit (시간순):
  - H1 hunks (schema 1.8 §11 8개 결정 모두 resolve)
  - §11.3 hybrid: storage AMBIGUOUS / LLM-filter wrapper / Recovery panel
  - H2 modifies edge whitelist (13 NodeType)
  - H3 EvidencePack assembler + Cache (4s→0.18s)
  - H4 issue-id extraction (4 regex pattern) + ticket index
  - issue_id filter for /api/evidence + ticket→patches viewer flow (`a6210d1`)
  - TS body walk P3 + statement-level nodes
  - Tier 1 god-node filter / Tier 2 lean json
  - Adaptive community split (top 1916 → 499)

**문제 / 동기**
- 다음 세션은 이번 8+ commit 의 컨텍스트 없이 시작.
- §11 의 8개 결정이 어떻게 묶였는지, 어떤 항목이 미해결인지 자료 산재.

**제안 작업**
- `docs/SESSION-HANDOFF-2026-05-10.md` 신규
  - 섹션: §1 이번 세션 commits, §2 §11 결정 8개 결과, §3 viewer 패널 현황, §4 미해결 / 다음 후보 (이 문서 참조), §5 환경 (graph.db 위치 등).

**대안 / Trade-offs**
- 작성하지 않음: 다음 세션 onboarding 비용 증가.
- 작성: 1-2시간 비용으로 다음 세션 절약.

**검증**
- 문서를 새 세션처럼 읽었을 때 cold start 가능한지 self-review.

**공수**: Low (확신도 높)
**의존성**: 없음. 이 문서(`NEXT-CANDIDATES`) 와 cross-link 권장.

---

## 9. TicketIndex sample_commits 메타 확장

**한 줄 요약**: ticket expand 시 commit subject 만 보이는데, 변경 파일 / 패키지 hint 를 함께 표시해 ticket 의 영향 범위를 미리 파악.

**현재 상태**
- backend `TicketRow` (=`pkg/evidence/cache.go` `TicketIndex` 함수) 가 `{issue_id, hunk_count, commit_count, sample_commits[3]}` 반환.
- sample_commits 는 `{sha, subject, author_time}` 만 — 파일 정보 없음.
- frontend 도 SHA + subject 한 줄 렌더 (TicketIndex.tsx).

**문제 / 동기**
- "GH-66 이 어떤 영역을 건드렸는지" 미리 보이면 patches 클릭 전에 가치 판단 가능.
- 현재는 일단 patches 눌러 봐야 알 수 있음 — 큰 ticket 일수록 비용.

**제안 작업**
- `pkg/evidence/cache.go` `pickSampleCommits` 옆에 `topFilesForCommit(corpus, sha) []string` 추가.
  - 각 commit 의 hunks → file_path 의 dirname → 빈도 top-3.
- `TicketRow.SampleCommits[].TopFiles []string` 추가.
- frontend: commit row 안에 작은 monospace pill 로 표시.

**대안 / Trade-offs**
- 작은 화면에서 패널이 시끄러워질 수 있음 — collapse 가능.
- backend payload 약간 커짐 (commit 당 ~30 byte).

**검증**
- Playwright: GH-66 expand → top files 가 `crypto/secp256k1/...` 같은 dir 로 표시.

**공수**: Low (확신도 중)
**의존성**: 없음.

---

## 10. /api/evidence intent OR/AND mode 토글

**한 줄 요약**: BM25 자연어 ranking 외에 "모든 토큰이 들어간 hunks만"(AND) 모드 추가 — agent 정밀 검색 능력 향상.

**현재 상태**
- 모든 query 가 BM25 OR-fashion (term 빈도 기반 ranking).
- "X 와 Y 모두 포함"을 강제할 방법 없음.
- 작은 코드베이스 / 정밀한 LLM 의도 표현 시 너무 fuzzy.

**문제 / 동기**
- LLM agent 가 "RetryPolicy 와 Backoff 모두 등장하는 변경"을 원해도 OR ranking 으로 약하게 들어옴.
- 사용자가 정확한 search 를 원할 때 fallback 없음.

**제안 작업**
- `pkg/evidence/evidence.go` `Options.Mode string` ("or"|"and", default "or").
- `Cache.BuildPack`: AND 모드면 BM25 ranking 후, query token 모두 포함하지 않은 hunks 필터.
- `internal/server/api.go` `?mode=and` 받음.
- `internal/mcp/evidence.go` mode param 노출.

**대안 / Trade-offs**
- 별도 endpoint: 인터페이스 명확. 단 코드 중복.
- mode 파라미터: 단일 endpoint, 추후 다른 mode (phrase, regex) 도 같은 자리에서 확장 가능.

**검증**
- 단위 테스트: 같은 corpus 에 mode=or vs mode=and 결과 차이 검증.

**공수**: Low (확신도 중)
**의존성**: 없음.

---

## 추천 우선순위 (단순 의견 — 사용자가 재정렬 가능)

| 순위 | 항목 | 이유 |
|------|------|------|
| 1 | #2 NodeDetail pill 클릭 → EvidencePack | ROI 가장 높음. Loop 닫음. Low effort. |
| 2 | #1 EvidencePack 페이지네이션 | 큰 ticket 에서 즉시 사용성 문제. 시 즉시 발현. |
| 3 | #8 SESSION-HANDOFF | 다음 세션 cost 절약. Low effort. |
| 4 | #4 EvidenceView 분리 | #2 의 선결로 자연스러움. Low effort. |
| 5 | #3 eval harness H3+H4 | 회귀 방지 안전망. 본격적 schema 1.9 작업 전에. |
| 6 | #6 `ckg evidence` CLI | shell / CI 워크플로 개선. |
| 7 | #5 MCP 통합 테스트 | tool 추가 시점에 더 가치 있음. 지금은 8개로 안정. |
| 8 | #10 OR/AND mode | LLM agent 사용 패턴 잡힌 후. |
| 9 | #9 sample_commits 메타 확장 | nice-to-have. |
| 10 | #7 성능 baseline | graph 확장 / CI 도입 시점에 더 가치. |

### Fact-based Answer
- Fact:
  - 직전 commit `a6210d1` 까지 H1–H4 + §11.3 hybrid + Cache + Tier1+2 + TS body walk/stmt 모두 main 반영
  - 본 문서는 코드/회귀/관측성 관점의 후보 도출이며 사용자가 합의한 우선순위는 아님
- Your Opinion:
  - High prediction: 위 표의 1-3 위는 비용 대비 효과 분명. 4-7 위는 미래 작업의 안전망 / 인프라.
  - Mid prediction: #5 통합 테스트는 향후 다른 언어(Solidity / Rust) 추가 시 ROI 급증 — 지금은 비용이 더 큼.
  - Low prediction: #7 성능 baseline 은 graph 가 현재 1.9M edges 에서 안정적이라 즉각적 가치 낮음.
  - None.
