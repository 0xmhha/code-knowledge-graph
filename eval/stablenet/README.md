# eval/stablenet — go-stablenet 4-방식 평가 자료

> CKG MCP 서버를 go-stablenet 코드베이스에 대해 검증하고 α/β/γ/δ 4-방식
> 평가를 돌릴 때 사용한 일체의 자료. 검증 보고서는 `VERIFICATION_REPORT.md`.

## 상위 프로젝트와의 관계

CKG는 stablenet-ai-agent의 **Coding Agent System** 구축에서 *Stage S0의 산출물*. CKS의 **Layer 1 (Storage Backends)** 부품으로 import 사용된다.

| Repo | 경로 | 책임 | go.mod |
|---|---|---|---|
| **CKG** (본 repo) | `/Users/.../tools/code-knowledge-graph` | Layer 1 그래프 + 일부 light/medium capability | `github.com/0xmhha/code-knowledge-graph` |
| **CKV** | `/Users/.../tools/code-knowledge-vector` | Layer 1 vector store + semantic search | `github.com/0xmhha/code-knowledge-vector` (진행 중) |
| **CKS** | `/Users/.../tools/code-knowledge-system` | Layer 2~4 (Working Memory + Orchestrator + Query API) | (미생성, S1에서 init) |
| **Coding Agent** | `stablenet-ai-agent/` | Orchestrator, `/command`, PR 생성 | (S2에서 시작) |

**핵심 원칙 (2026-05-11 결정)**: CKS·CKV는 CKG 코드를 *옮기지 않고 go.mod import로 사용*. CKG는 stable public API(`pkg/*`)를 책임. S1 진입 차단 요소는 `pkg/mcphandlers/` 신설 (T-14).

## 디렉토리 구조

```
eval/stablenet/
├── VERIFICATION_REPORT.md          # 최종 검증 보고서 (8 sections, 278 lines)
├── build_filterlist.py             # BUILD_SOURCE_FILES.md → JSON 변환 파서
├── stablenet-files.json            # 781-entry --files-from 화이트리스트 (테스트 제외)
├── stablenet-files-with-tests.json # dir/*.go 글롭 --files-from (관련 테스트 포함)
├── tasks/                          # 평가 task YAML
│   ├── T01-newblockchain-callers.yaml      (symbol_set, γ stress)
│   ├── T02-wbft-prepare-validation.yaml    (rubric, δ stress)
│   └── T03-systemcontracts-v2-upgrade.yaml (rubric cross-package)
├── probes/                         # MCP 도구 동작 검증 스크립트
│   ├── mcp_probe.py                # 8개 도구 smoke (v1)
│   └── mcp_probe_v2.py             # B1·B2 버그 수정 회귀 probe
├── sim/                            # 채점기 직접 시뮬레이션 (LLM=현 세션)
│   ├── collect_inputs.py           # 4 baseline의 input context 수집
│   └── score_simulation.py         # V0 채점기 Python 포팅 + T01 8-case
└── results/                        # 측정 결과 (재현용 baseline)
    ├── smoke/                      # MCP smoke + bench 결과
    ├── eval-v1/                    # ckg eval cli backend 12 측정점
    └── sim/                        # 직접 시뮬레이션 결과
```

## --files-from 필터 (두 종류)

`ckg build --files-from`에 넘기는 화이트리스트. go-stablenet은 트리 전체에 테스트·
TS·proto·플랫폼 외 빌드태그 파일이 섞여 있어, **무엇을 인덱싱할지 명시**해야 결정적이고
의도에 맞는 그래프가 나온다. 용도에 따라 둘을 구분해 쓴다.

| 파일 | 스코프 | 테스트 | 용도 |
|---|---|---|---|
| `stablenet-files.json` | 781개 명시 `.go` 파일 (바이너리 컴파일 입력, `BUILD_SOURCE_FILES.md` → `build_filterlist.py`) | **제외** (`exclude: **/*_test.go`) | 검색 정밀도 평가 코퍼스 (테스트 노이즈 없는 깨끗한 측정) |
| `stablenet-files-with-tests.json` | 130개 `<pkg-dir>/*.go` 글롭 (+ `systemcontracts/**/*.sol`) | **포함** (글롭이 `_test.go`도 매칭) | 지식 데이터(cks/ckv) 코퍼스 — 테스트 코드의 few-shot 가치(사용법·구현 패턴)를 살림 |

둘 다 TS/proto와 바이너리 외 디렉터리(`tests/`, 일부 `cmd/`·`internal/`)는 제외한다 —
차이는 **바이너리 디렉터리 안의 `_test.go`를 넣느냐**다. 지식 데이터(예:
`knowledge-data/pr-77`)는 with-tests 쪽을 쓴다. (출처: `.claude/docs/build-source-files.md`.)

```
ckg build --src=<go-stablenet@commit> --out=<dir> --lang=auto \
    --files-from=eval/stablenet/stablenet-files-with-tests.json
```

## 실행 순서 (재현)

전제: go-stablenet-latest 소스, ckg 바이너리, claude CLI(+cliwrap-agent) 또는
ANTHROPIC_API_KEY 중 하나.

```bash
# 0. 경로 (스크립트는 현재 아래 절대 경로 하드코딩 — 다른 환경에서 돌릴 때 수정)
STABLENET_SRC=/Users/.../go-stablenet-latest
CKG=/Users/.../code-knowledge-graph/bin/ckg
GRAPH=/tmp/ckg-stablenet                   # 출력 위치

# 1. 빌드 화이트리스트 생성 (BUILD_SOURCE_FILES.md → 781 paths JSON)
python3 build_filterlist.py
# → /tmp/ckg-stablenet-prep/stablenet-files.json (또는 eval/stablenet/stablenet-files.json 복사본)

# 2. 그래프 빌드
$CKG build --src=$STABLENET_SRC \
           --files-from=eval/stablenet/stablenet-files.json \
           --out=$GRAPH --lang=go

# 3. MCP 8개 도구 smoke
python3 probes/mcp_probe.py
python3 probes/mcp_probe_v2.py             # 버그 수정 회귀

# 4. 4-방식 평가
CLIWRAP_AGENT=/path/to/cliwrap-agent \
$CKG eval --graph=$GRAPH \
          --tasks='eval/stablenet/tasks/*.yaml' \
          --baselines=alpha,beta,gamma,delta \
          --llm-backend=cli \
          --out=eval/stablenet/results/eval-vN/

# 5. (옵션) 채점기 직접 시뮬레이션 — cli backend 미가용 시
python3 sim/collect_inputs.py              # T01의 4 baseline input 수집
python3 sim/score_simulation.py            # V0 채점기로 8-case 채점
```

## 환경변수

스크립트는 다음 환경변수로 경로를 받는다. 미설정 시 `STABLENET_SRC` 외에는 합리적 기본값을 사용한다.

| 변수 | 의미 | 기본값 | 사용 파일 |
|---|---|---|---|
| `STABLENET_SRC` | go-stablenet-latest 체크아웃 절대 경로 | (필수, 미설정 시 fail-fast) | `build_filterlist.py`, `tasks/*.yaml` |
| `CKG` | `ckg` 바이너리 경로 | `bin/ckg` (레포 루트 기준 상대) | `probes/mcp_probe*.py`, `sim/collect_inputs.py` |
| `GRAPH` | 빌드한 그래프 경로 | `/tmp/ckg-stablenet` | `probes/mcp_probe*.py`, `sim/collect_inputs.py` |
| `OUT` / `OUT_DIR` | 결과 출력 위치 | `/tmp/ckg-stablenet-prep/...` | `build_filterlist.py`, `probes/*.py`, `sim/collect_inputs.py` |

`sim/score_simulation.py`는 입력 없음 (내장 답안만).

YAML(`tasks/*.yaml`)의 `corpus_path: ${STABLENET_SRC}/...`는 CKG eval runner가 env-expand 처리한다.

## 관련 문서

- **`EXECUTION_STRATEGY.md`** — *최우선*. 상위 프로젝트(stablenet-ai-agent CKS) 맥락에서 본 CKG 작업 전략. HANDOFF의 10 task를 S0/S1 경계에 따라 재분류 + 4-phase 진행 계획
- **`HANDOFF.md`** — 미해결 결함 10건의 task별 상세 (acceptance, 코드 위치, 권장 접근)
- `VERIFICATION_REPORT.md` — 본 검증의 전체 결과·발견·결론
- `RESPONSE_VALIDATION_REPORT.md` — MCP 응답 정확도 + LLM 코드 이해도 + 30-question 인프라 gap 분석
- `docs/EVAL.md` (레포 상위) — CKG eval 인프라 일반 문서
- `internal/eval/baseline.go` — α/β/γ/δ 시스템 프롬프트 + AllowedTools
