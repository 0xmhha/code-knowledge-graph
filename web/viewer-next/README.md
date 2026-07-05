# CKG Viewer (web/viewer-next)

Next.js 15 + TypeScript + `react-force-graph-{2d,3d}` 기반 코드 지식 그래프 viewer.
`output: 'export'`로 만들어진 정적 자산이 `ckg` Go 바이너리에 `go:embed`로 박혀,
`ckg serve`가 `:8080`에서 백엔드(`/api/*`)와 함께 한 번에 serve.

> **첫 방문 시 이 문서부터 읽어라.** dev/prod 경로가 헷갈리기 쉽고,
> `next.config.mjs`의 `trailingSlash`가 dev에서 backend와 충돌하므로
> 무작정 `npm run dev`로 띄우면 막힌다.

---

## 1. 정상 경로 (Prod) — 가장 흔한 사용

`make build-full`은 `make viewer`를 의존성으로 가져, viewer-next를 정적 export한 결과를
`internal/server/web_assets/`로 복사한 뒤 Go 바이너리에 embed한다.
즉 **`make build-full` 1회면 backend + viewer가 한 바이너리**에 들어간다.
(`make build`는 Go 바이너리만 빌드하므로 embed된 viewer는 stub이다.)

```bash
# 0. 한 번만: ckg + viewer 한꺼번에 빌드
make build-full
# → ./bin/ckg

# 1. 분석 대상 프로젝트의 그래프 생성
./bin/ckg build --src=/path/to/project --out=/path/to/graph-out
# → /path/to/graph-out/graph.db + manifest.json

# 2. viewer 열기 (backend + viewer 동시 serve)
./bin/ckg serve --graph=/path/to/graph-out --open
# → http://localhost:8080 자동 오픈
```

한 줄로 통합:

```bash
./bin/ckg quickstart --src=/path/to/project --out=/tmp/graph --port=8080
```

> **viewer를 수정했다면 반드시 `make build-full` 다시 실행.** 바이너리에 embed된
> viewer는 컴파일 시점에 고정된다. ckg 코드는 그대로고 viewer만 수정한 경우엔
> `make viewer && make build` 조합도 같은 결과를 더 빠르게 만든다.

---

## 2. Dev — viewer만 빠르게 반복 (권장)

`CKG_DEV_VIEWER_DIR` 환경변수가 set되면, ckg는 embed된 viewer 대신
그 디렉토리의 정적 파일을 serve. 즉 **`ckg` 바이너리 재컴파일 없이도**
viewer 변경분을 확인할 수 있다.

```bash
# 한 번만
cd web/viewer-next && npm install

# 매 수정 사이클
cd web/viewer-next && npm run build       # 약 3~5s
CKG_DEV_VIEWER_DIR=$(pwd)/web/viewer-next/out \
  ./bin/ckg serve --graph=/path/to/graph-out --open
```

`trailingSlash` 충돌 없음 (Next dev 서버를 거치지 않으므로). 풀 hot reload는
아니지만 `npm run build`가 충분히 빨라 실용적.

---

## 3. Dev — Next hot reload (실험적, 비권장)

Next dev 서버의 hot reload를 쓰고 싶을 때. `npm run dev`로 :3001을 띄우고,
`/api/*`는 `:8080`의 `ckg serve --no-viewer`로 proxy.

```bash
# Term 1
./bin/ckg serve --graph=/path/to/graph-out --no-viewer --port=8080

# Term 2
cd web/viewer-next && npm run dev   # :3001
```

**알려진 함정**:

- `next.config.mjs`의 `trailingSlash: true`는 정적 export 요구 사항(디렉토리
  구조 `/foo/index.html`을 만들기 위함)이지만, dev 서버에서 `/api/*` rewrite와
  충돌해 `:8080`의 Go backend가 404를 반환(`/api/manifest/` → 404,
  `/api/manifest` → 200). NODE_ENV 분기로 dev에서 끄는 시도도 redirect loop가
  남아 미해결.
- 위 문제 풀기 전엔 §2의 `CKG_DEV_VIEWER_DIR` 경로를 권장.

---

## 4. 데이터 모드 자동 감지

`src/lib/api.ts`의 `detectMode()`가 `./manifest.json` fetch로 판별:

| `./manifest.json` 응답 | 모드 | 구현 |
|---|---|---|
| 200 OK | `static` | `StaticAPI` — `chunks/*.json`을 fetch |
| 그 외 | `serve` | `API` — `/api/*` REST 호출 |

- **`ckg serve`** → manifest는 `/api/manifest`로만 제공 → `./manifest.json` 404 → **serve mode**
- **`ckg export-static`** → 정적 dir 루트에 `manifest.json` 둠 → **static mode**

즉 viewer 코드는 두 모드를 모두 지원하지만, 각 모드는 데이터를 다르게 fetch한다.

---

## 5. 디렉토리 구조

| 경로 | 역할 |
|---|---|
| `app/{page,layout}.tsx`, `app/globals.css` | Next App Router 엔트리, 전역 CSS |
| `src/components/App.tsx` | 부팅·네비게이션·키보드 단축키·패널 레이아웃 |
| `src/components/GraphCanvas.tsx` | 2D/3D 그래프 본체. react-force-graph 래핑 |
| `src/components/{NodeDetail,NodeList,TopBar,…}.tsx` | UI 패널들 |
| `src/store/store.ts` | zustand 전역 상태 (visibleIds, focusDistance, anchor, history…) |
| `src/lib/api.ts` | `IAPI`, `API`, `StaticAPI`, `detectMode` |
| `src/lib/{edges,encoding,depth,trace}.ts` | 색·엣지 그룹·BFS·trace 유틸 |
| `out/` (`.gitignore`) | `npm run build` 결과. `make viewer`가 `internal/server/web_assets/`로 복사 |

---

## 6. 빌드 / 검증 스크립트

```bash
cd web/viewer-next
npm run typecheck     # tsc --noEmit
npm run lint          # eslint .
npm run build         # 정적 export → out/
npm run test:smoke    # Playwright 스모크
```

production 통합 검증:

```bash
make build-full
./bin/ckg serve --graph=/some/graph-out --open
```

---

## 7. 흔한 함정 체크리스트

| 증상 | 원인 | 해결 |
|---|---|---|
| viewer를 고쳤는데 화면이 그대로 | embed된 viewer는 컴파일 시점에 고정 | `make build-full` 다시 / 또는 §2 `CKG_DEV_VIEWER_DIR` |
| `npm run dev`에서 `/api/* → 500` | backend(ckg serve) 안 떠 있음 또는 trailingSlash 충돌 | §2 경로로 전환 권장 |
| `ckg serve` 후 빈 캔버스 | `--graph` 디렉토리에 `graph.db` 없음 | `ckg build`부터 |
| "1 Issue" 좌하단 빨간 배지 | `/api/*` 호출 실패 (manifest/edges/nodes 중 하나) | 콘솔 / 서버 로그 확인 |
| `ERR_TOO_MANY_REDIRECTS` on `/api/*` | Next dev + trailingSlash 처리 충돌 | §2로 전환 |
| `THREE.WARNING: Multiple instances` | `react-force-graph-3d`가 자체 three.js 번들. 무시 가능 | — |

---

## 8. 시각 효과 (2026-05-21 시점)

이 viewer가 지금 가진 4가지 시각 효과의 정의 위치:

| 효과 | 위치 | 키 변수 |
|---|---|---|
| 배경 보라/네이비 radial gradient | `app/globals.css .canvas-host` | `radial-gradient(...)` + `backgroundColor="rgba(0,0,0,0)"` |
| Dolly-in (거리 ×0.6, 400ms) | `GraphCanvas.tsx centerOnNode` 3D 분기 | `DOLLY_FACTOR`, `CENTER_DURATION_MS` |
| Focus-locked orbit (선택 노드 중심 회전) | `GraphCanvas.tsx centerOnNode` 3D + `zoomReset` | `controls().target.set(...)` |
| Edge gradient (src 불투명 → dst 투명) | `GraphCanvas.tsx linkCanvasObject` 2D | `linkCanvasObjectMode='after'`, alpha 0.85→0 |

대기 중: BFS-ripple (선택 시 1회 0.5초 펄스) — react-force-graph cooldown 이후
redraw 강제 필요라 별도 라운드.

---

## 9. 다음에 헷갈리지 않으려면

- "viewer 안 켜져요" → §1 또는 §2.
- "viewer 수정했는데 안 보여요" → §1 또는 §2의 `CKG_DEV_VIEWER_DIR`. 절대 §3 먼저 시도하지 말 것.
- "데이터가 안 보여요" → `ckg build`로 `graph.db`가 진짜 생겼는지 + `--graph` 경로가 맞는지부터.
- "정적 export로 배포" → `ckg export-static --graph=X --out=Y` 후 Y를 정적 호스팅.
