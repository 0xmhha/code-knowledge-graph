# 검증 체크리스트

**목적**: 새 feature commit 전에 거쳐야 할 검증 surface를 표준화. 2026-05-10 세션의 5종 누락 패턴 ([§5 누락 카탈로그](#5-자주-누락된-패턴-카탈로그)) 재발 방지.

**적용 범위**: 한 feature가 두 개 이상의 surface(Options struct / HTTP / MCP / CLI / viewer)에 wired되거나, 결합 가능한 옵션 axis 두 개 이상 가질 때.

---

## 1. 4-축 surface fan-out 체크

새 옵션이 추가될 때 다음 4 surface 모두 확인:

| 축 | 위치 | 체크 |
|----|------|------|
| **Options struct** | `pkg/evidence/evidence.go::Options` (또는 동등) | 필드 추가 + godoc 한 줄 |
| **HTTP** | `internal/server/api.go::handle*` | query param 파싱 + 입력 검증 + 400 negative |
| **MCP** | `pkg/mcphandlers/<tool>.go::Register*` (public surface; `internal/mcp/server.go` calls `RegisterAll`) | `mcp.WithString/Number(...)` schema + handler 매핑 |
| **CLI** | `cmd/ckg/<cmd>.go` | `cmd.Flags().*Var()` + allow-list guard + `--help` 텍스트 |

**실패 사례**: `2982864` (mode=and) — Options/HTTP/MCP/CLI 4축 모두 wired했지만 라이브는 CLI 1축만 검증. 후속 `85af082` 보강.

---

## 2. 조합 매트릭스 체크

두 개 이상 axis가 결합 가능하면 조합 시나리오 단위 테스트가 있어야 한다.

**evidence.Options 현재 axis 매트릭스** (참조용):

| Axis | 값 | filter 순서 |
|------|------|------|
| `Intent` | "" / 토큰 | (필요) |
| `IssueID` | "" / "GH-N" / "JIRA-N" | BM25 후 1차 필터 |
| `Mode` | "" / "or" / "and" | BM25 후 2차 필터 |
| `SeedQname` | "" / qname | 위 둘 후 3차 필터 |
| `Offset` | 0 / N | recency 정렬 후 page slice |
| `K` / `BudgetTokens` | 양수 | emit 단계 cap |

조합 시나리오 라이브/단위 검증 의무:
- `IssueID + Mode=and` → 두 필터 모두 통과해야 함 (`TestBuildPack_AndMode_WithIssueID`)
- `IssueID + Offset` → ticket 내 페이지네이션 (`TestBuildPack_OffsetPaging`은 현재 issue-only)
- `SeedQname + Mode=and` → 미검증 (mode=and 후속 항목)
- `SeedQname + IssueID` → 미검증 (남음)

새 axis 추가 시: 기존 N axis와의 N개 페어 매트릭스 모두 체크.

---

## 3. Negative path 체크

매 endpoint/CLI는 다음 negative case에 명시적으로 응답해야 한다.

| Case | HTTP 응답 | CLI 응답 |
|------|-----------|----------|
| 필수 input 누락 | 400 + 메시지 | non-zero exit + stderr |
| input allow-list 위반 (`mode=xor`) | 400 + 메시지 | non-zero exit + stderr |
| 존재하지 않는 graph / 파일 | 500 또는 404 (의미 명확) | non-zero exit + stderr |
| 빈 결과 (입력은 valid but 매치 0) | 200 + `hits: []` | exit 0 + `(no hits)` 또는 빈 JSON |

**실패 사례**: 직전 세션 `--graph nonexistent`로 exit 0 의심 → pipeline 마지막 head 의 exit 이라 false alarm. 라이브 검증은 `2>/dev/null && echo "exit: $?"` 패턴 필수.

---

## 4. 라이브 vs 단위 분리

| 검증 대상 | 단위 (`*_test.go`) | 라이브 (`bin/ckg` 실행) |
|----------|--------------------|-----------------------|
| 알고리즘 정확성 | ✅ 필수 | optional |
| 파라미터 wiring | ✅ 필수 | ✅ 1건 이상 |
| HTTP/MCP endpoint 등록 | ✅ static scan (`TestRunRegistersAllEightTools` 같은) | ✅ 1건 이상 (curl 또는 spawn) |
| CLI flag 파싱 | ✅ cobra `Execute()` 직접 호출 | ✅ 1건 이상 (실제 graph) |
| 성능 / 결정성 | bench tests | bash loop ≥3회 |

**실패 사례**: `5f2cf21` (`ckg evidence`) — `--seed-qname` / `--offset` flag 라이브 미실시 (1번 항목 후속에서 보강).

---

## 5. 자주 누락된 패턴 카탈로그

이번 세션(2026-05-10) 에서 사후 발견된 5종:

1. **surface fan-out 1축만 검증** — 4축 wired됐는데 CLI/HTTP 중 하나만 라이브 (`2982864` mode=and)
2. **조합 시나리오 미고려** — single-axis만 단위 테스트 (`693c643` offset+issue 미검증)
3. **HTTP allow-list guard 라이브 누락** — code path는 trivially correct 가정 (`mode=xor` HTTP 400)
4. **MCP tool 라이브 cover 부족** — unit test로만 검증, e2e는 build tag 뒤에 격리되어 일상 실행 안 됨
5. **negative path 검증 빠짐** — exit code / 에러 메시지 quality 미확인

새 commit 전 이 카탈로그를 한 번 훑어 모든 항목이 클리어되는지 확인.

---

## 6. PR-ready 체크리스트 (요약)

```
[ ] go test ./... → all packages ok (match CI: `go test -race ./...`)
[ ] 추가/수정한 surface 4축 (Options/HTTP/MCP/CLI) 모두 cover
[ ] 새 axis 가 기존 N axis와 만들 수 있는 N개 조합 중 의미 있는 것 단위 테스트
[ ] negative path: 필수-input-누락, allow-list-violation, 존재하지-않는-리소스
[ ] 라이브: 새 flag/param 별로 최소 1건 (positive) + 1건 (negative)
[ ] 라이브 검증 시 `2>/dev/null && echo $?` 로 exit code 단정
[ ] commit message 에 실패 사례/회귀 가능성 1줄 포함
[ ] handoff doc 업데이트 (commits 표 + §6 우선순위)
```

---

## 7. 참조

- 이번 세션 사례별 회고: `docs/SESSION-HANDOFF-2026-05-10.md` §1
- 평가 fixture / 회귀 안전망: `internal/buildpipe/h3h4_integration_test.go`, `internal/temporal/issueid_test.go::TestExtractIssueIDs_CorpusPrecisionRecall`, `pkg/mcphandlers/safety_test.go` (`llmSafeStoreReader` AMBIGUOUS-drop coverage)
