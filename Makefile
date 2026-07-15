.PHONY: all build build-full build-no-viewer viewer test test-race lint lint-viewer fmt fmt-check install-hooks audit clean eval eval-baseline-update eval-ckv-mirror eval-stablenet-keyword

GO ?= go

all: build

# Default viewer build: Next.js (web/viewer-next/). Static export
# (`output: 'export'`) writes to out/, which we mirror into
# internal/server/web_assets/ so the go:embed directive in viewer.go
# picks it up at compile time.
#
# We rm -rf the embed dir before copying to drop stale files from the
# previous viewer build (the old esbuild output's assets/viewer.js
# would otherwise hang around). Bounded to internal/server/web_assets/.
viewer:
	cd web/viewer-next && npm install && npm run build
	rm -rf internal/server/web_assets
	mkdir -p internal/server/web_assets
	cp -R web/viewer-next/out/. internal/server/web_assets/

# build: Go binary only. The viewer UI is embedded at compile time via
# go:embed (internal/server/viewer.go), so a binary from this target
# serves whatever is currently in internal/server/web_assets/ — the
# tracked stub unless `make viewer` (or `make build-full`) ran before.
# Use `make build-full` for a release-grade binary with the real viewer.
build:
	$(GO) build -o bin/ckg ./cmd/ckg

# build-full: Next.js viewer + Go binary (the old `make build`).
build-full: viewer
	$(GO) build -o bin/ckg ./cmd/ckg
	@# The binary already embedded the real Next.js index.html at compile
	@# time, so restore the tracked stub so `git status` stays clean. No-op
	@# outside a git repo (CI tarballs etc.) — the stub on disk doesn't
	@# affect the running binary.
	@# `git rev-parse --git-dir` instead of `[ -d .git ]` because .git is a
	@# *file* inside `git worktree add` checkouts; the dir test would silently
	@# skip there and bring back the churn we are eliminating.
	@if git rev-parse --git-dir >/dev/null 2>&1; then \
	    git checkout -- internal/server/web_assets/index.html 2>/dev/null \
	      || echo "warn: could not restore stub web_assets/index.html — run 'git checkout -- internal/server/web_assets/index.html' manually"; \
	fi

# Back-compat alias (older docs/scripts): identical to `build`.
build-no-viewer: build

test:
	$(GO) test ./...

test-race:
	$(GO) test -race -coverprofile=coverage.out ./...

lint: lint-viewer fmt-check
	$(GO) vet ./...

# fmt: rewrite every Go file in place with gofmt's canonical
# formatting. Safe to run any time — gofmt is deterministic and
# behaviour-preserving, so 'make fmt' followed by 'make test' is
# a no-op for everything except whitespace.
#
# Scope: every .go file under the repo except web/viewer-next/node_modules,
# which contains vendored .go files we must not touch.
fmt:
	@find . -name '*.go' -not -path './web/viewer-next/node_modules/*' -print0 | xargs -0 gofmt -w

# fmt-check: fail loudly when any tracked .go file diverges from
# gofmt's canonical form. Added as a `lint` dependency so the same
# 'make lint' that CI already runs (.github/workflows/ci.yml) blocks
# gofmt drift PRs without needing a new workflow step.
#
# Why this matters: the repo accumulated 79 unformatted files between
# 2026-05-19 and 2026-05-20 (cleaned up in commit df5709b). Without
# a gate, the same drift returns the moment someone forgets `gofmt -w`
# before commit. The check is fast (<1s on this tree) so it is fine
# to run on every PR.
fmt-check:
	@drift=$$(find . -name '*.go' -not -path './web/viewer-next/node_modules/*' -print0 | xargs -0 gofmt -l); \
	if [ -n "$$drift" ]; then \
	    echo "gofmt drift detected — run 'make fmt' before commit:"; \
	    echo "$$drift"; \
	    exit 1; \
	fi

# install-hooks: opt-in helper that points git at .githooks/ so the
# pre-commit script runs locally. Idempotent — re-running is safe.
# We don't auto-install on `make build` because hooks are a per-clone
# config (some operators use IDE-side commit flows that already cover
# formatting).
install-hooks:
	@git config core.hooksPath .githooks
	@echo "git hooks path set to .githooks (pre-commit will run fmt-check)"

# eval: LLM-free regression baseline measurement (CKG-EV1 Phase 1).
#
# Self-indexes this repo (Go only) and runs three deterministic probes
# against the resulting graph:
#
#   1. validate  — schema invariants, dangling edges. PASS = exit 0 +
#                  zero Issues across registered validators.
#   2. benchmark — token-reduction ratio vs grep-everything baseline.
#                  Watch for >20% drop in reduction_ratio between runs.
#   3. bench-mcp --depth-sweep — p50/p95/p99 latency for the eight
#                  MCP traversal probes at depth=1 and depth=2.
#
# Output: eval/results/latest/{validate,benchmark,bench-mcp}.json.
# Compare with eval/baseline/ (committed snapshot) to detect regressions.
#
# Why no LLM here: ckg eval (the LLM-driven baseline) costs API calls and
# runs in minutes, not seconds. The three probes above run in <2 minutes
# end-to-end with no external dependencies — cheap enough to gate every
# PR if/when CI integration lands.
eval: build-no-viewer
	@mkdir -p eval/results/latest
	@echo "=== ckg eval: step 1/5 — self-index (corpus baselining) ==="
	./bin/ckg build --src=. --out=eval/.ckg-data --lang=go
	@echo
	@echo "=== ckg eval: step 2/5 — validate (schema integrity) ==="
	./bin/ckg validate --graph=eval/.ckg-data --format=json > eval/results/latest/validate.json
	@echo "  → eval/results/latest/validate.json"
	@echo
	@echo "=== ckg eval: step 3/5 — benchmark (token reduction) ==="
	./bin/ckg benchmark --graph=eval/.ckg-data --format=json > eval/results/latest/benchmark.json
	@echo "  → eval/results/latest/benchmark.json"
	@echo
	@echo "=== ckg eval: step 4/5 — bench-mcp (latency, depth sweep) ==="
	./bin/ckg bench-mcp --graph=eval/.ckg-data --depth-sweep --iterations=50 \
	    --output=eval/results/latest/bench-mcp.json
	@echo
	@# Retrieval probes run against the synthetic corpus rather than the
	@# self-index because expected node IDs must stay stable across code
	@# changes. testdata/synthetic/ is the fixed ground truth; eval/.synthetic-data/
	@# is the regenerable index.
	@echo "=== ckg eval: step 5/5 — eval-retrieval (LLM-free recall/precision) ==="
	./bin/ckg build --src=testdata/synthetic --out=eval/.synthetic-data --lang=go,ts,sol
	./bin/ckg eval-retrieval --graph=eval/.synthetic-data --fixtures=eval/retrieval \
	    --output=eval/results/latest/retrieval.json
	@echo
	@echo "=== Summary ==="
	@if [ -d eval/baseline ]; then \
	    echo "Compare:        diff -ur eval/baseline eval/results/latest"; \
	    echo "Update baseline: make eval-baseline-update   (after reviewing the diff)"; \
	else \
	    echo "No baseline yet. Inspect eval/results/latest/*.json, then:"; \
	    echo "  make eval-baseline-update"; \
	fi

# eval-baseline-update: promote the latest run to the committed baseline.
# Intentionally a separate manual step — accidentally overwriting the
# baseline would silently mask regressions on the next run.
eval-baseline-update:
	@[ -d eval/results/latest ] || { echo "Run 'make eval' first"; exit 1; }
	@rm -rf eval/baseline
	@cp -R eval/results/latest eval/baseline
	@echo "eval/baseline/ refreshed from eval/results/latest/"
	@echo "Commit the change to lock the new baseline."

# eval-ckv-mirror: LLM-free keyword-retrieval eval that mirrors CKV's fixture
# set (ckg-NEW-5, Stage B). CKV measures semantic recall over its sample corpus
# with natural-language intents; this builds a ckg graph over the SAME vendored
# corpus (eval/ckv-mirror/corpus, with a go.mod so the Go files type-check) and
# probes ckg's keyword tools (find_symbol / search_text) with keyword/exact
# queries derived from those intents — so ckg and ckv can be cross-referenced on
# one query set. Deterministic: same corpus + fixtures -> same result.
eval-ckv-mirror: build-no-viewer
	@rm -rf eval/ckv-mirror/.index
	./bin/ckg build --src=eval/ckv-mirror/corpus --out=eval/ckv-mirror/.index --lang=auto
	./bin/ckg eval-retrieval --graph=eval/ckv-mirror/.index --fixtures=eval/ckv-mirror/fixtures \
	    --output=eval/ckv-mirror/.index/results.json

# eval-stablenet-keyword: Stage 2 — the same keyword-retrieval eval over the
# REAL go-stablenet corpus (not the tiny vendored sample). Unlike the mirror,
# the corpus is external and huge, so this cannot vendor it: it runs the
# committed fixtures against a pre-built go-stablenet graph. Point GRAPH at a
# graph dir built from analysis-test-3 @ 0bf2f4d1b with the tests-included
# filter (see docs + scripts/index-project.sh); the expected qnames are pinned
# to that commit. Example:
#   make eval-stablenet-keyword GRAPH=/path/to/knowledge-data/pr-77-2
eval-stablenet-keyword: build-no-viewer
	@[ -n "$(GRAPH)" ] || { echo "ERROR: set GRAPH=<go-stablenet graph dir> (pinned to 0bf2f4d1b, tests-included). See eval/stablenet-keyword/README.md"; exit 1; }
	@[ -f "$(GRAPH)/graph.db" ] || { echo "ERROR: $(GRAPH)/graph.db not found"; exit 1; }
	./bin/ckg eval-retrieval --graph=$(GRAPH) --fixtures=eval/stablenet-keyword/fixtures \
	    --output=eval/stablenet-keyword/results.json

# Machine-local paths — override via environment or .env.local:
#   STABLENET_SRC  path to go-stablenet checkout
#   EVAL_DB_ROOT   persistent directory for evaluation graph DBs
#
# No defaults — these MUST be set. .env.local is gitignored and
# sourced automatically if present:
#   echo 'export STABLENET_SRC=~/Work/github/stable-net/go-stablenet-latest' >> .env.local
#   echo 'export EVAL_DB_ROOT=~/Work/github/tools/go-stable-code'           >> .env.local
-include .env.local
STABLENET_SRC ?=
EVAL_DB_ROOT  ?=

# eval-build-dbs: pre-build evaluation graphs into EVAL_DB_ROOT.
# Skips if graph.db already exists (use --force to rebuild).
# Builds HEAD by default; pass AT_COMMITS for additional snapshots.
#
# Examples:
#   make eval-build-dbs                                 # HEAD only, Go corpus
#   make eval-build-dbs AT_COMMITS="319b84d 0bf2f4d"   # HEAD + two past commits
#   make eval-build-dbs FORCE=--force                   # rebuild all
#   make eval-build-dbs LANG=auto                        # include sol/proto too
#
# LANG defaults to `go` because the retrieval eval measures the Go consensus
# corpus; pass LANG=auto (or LANG=go,sol) to index Solidity/proto as well, e.g.
# to validate cross-language canonical_id.
FORCE ?=
AT_COMMITS ?=
LANG ?= go
eval-build-dbs: build-no-viewer
	@[ -n "$(STABLENET_SRC)" ] || { echo "ERROR: STABLENET_SRC not set. Add it to .env.local"; exit 1; }
	@[ -n "$(EVAL_DB_ROOT)" ]  || { echo "ERROR: EVAL_DB_ROOT not set. Add it to .env.local"; exit 1; }
	@[ -d "$(STABLENET_SRC)" ] || { echo "ERROR: STABLENET_SRC=$(STABLENET_SRC) not found"; exit 1; }
	@mkdir -p $(EVAL_DB_ROOT)
	@echo "=== Building HEAD graph ==="
	./bin/ckg build \
	    --src=$(STABLENET_SRC) \
	    --out=$(EVAL_DB_ROOT)/stablenet \
	    --out-tag=auto-commit-hash \
	    --lang=$(LANG) $(FORCE)
	@for sha in $(AT_COMMITS); do \
	    echo ""; \
	    echo "=== Building graph at $$sha ==="; \
	    ./bin/ckg build \
	        --src=$(STABLENET_SRC) \
	        --at-commit=$$sha \
	        --out=$(EVAL_DB_ROOT)/stablenet \
	        --out-tag=auto-commit-hash \
	        --lang=$(LANG) $(FORCE); \
	done
	@echo ""
	@echo "=== Evaluation DBs in $(EVAL_DB_ROOT) ==="
	@ls -d $(EVAL_DB_ROOT)/stablenet-* 2>/dev/null || echo "(none)"

# eval-stage-b: Stage B evaluation — runs 30 stable-net task YAMLs × 4
# baselines against a pre-built go-stablenet graph.
# Requires:
#   - Pre-built graph in EVAL_DB_ROOT (run `make eval-build-dbs` first)
#   - LLM backend configured (ANTHROPIC_API_KEY or CLIWRAP_AGENT)
#
# Output: eval/results/stage-b/{results.csv,report.md}
STAGE_B_BASELINES ?= alpha,beta,gamma,delta
STAGE_B_NRUNS ?= 1
eval-stage-b: build-no-viewer
	@[ -n "$(STABLENET_SRC)" ] || { echo "ERROR: STABLENET_SRC not set. Add it to .env.local"; exit 1; }
	@[ -n "$(EVAL_DB_ROOT)" ]  || { echo "ERROR: EVAL_DB_ROOT not set. Add it to .env.local"; exit 1; }
	@[ -d "$(STABLENET_SRC)" ] || { echo "ERROR: STABLENET_SRC=$(STABLENET_SRC) not found"; exit 1; }
	@mkdir -p eval/results/stage-b
	@# Resolve HEAD graph path; build if missing.
	@GRAPH_DIR=$$(./bin/ckg build \
	    --src=$(STABLENET_SRC) \
	    --out=$(EVAL_DB_ROOT)/stablenet \
	    --out-tag=auto-commit-hash \
	    --lang=go 2>&1 | grep -E "(already exists at|built.*into)" | sed 's/.*at //;s/.*into //;s/ .*//' ) && \
	echo "=== Stage B: graph at $$GRAPH_DIR ===" && \
	echo "" && \
	echo "=== Stage B: eval 30 tasks × $(STAGE_B_BASELINES) ===" && \
	STABLENET_SRC=$(STABLENET_SRC) ./bin/ckg eval \
	    --tasks='eval/stablenet/tasks/T*.yaml' \
	    --graph=$$GRAPH_DIR \
	    --baselines=$(STAGE_B_BASELINES) \
	    --llm-backend=$(LLM_BACKEND) \
	    --llm=$(LLM_MODEL) \
	    --n-runs=$(STAGE_B_NRUNS) \
	    --out=eval/results/stage-b
	@echo ""
	@echo "=== Stage B Report ==="
	@cat eval/results/stage-b/report.md

# eval-stage-b-baseline-update: promote Stage B results to committed baseline.
eval-stage-b-baseline-update:
	@[ -d eval/results/stage-b ] || { echo "Run 'make eval-stage-b' first"; exit 1; }
	@mkdir -p eval/baseline/stage-b
	@cp eval/results/stage-b/results.csv eval/baseline/stage-b/
	@cp eval/results/stage-b/report.md eval/baseline/stage-b/
	@echo "eval/baseline/stage-b/ refreshed from eval/results/stage-b/"
	@echo "Commit the change to lock the new Stage B baseline."

# eval-llm-smoke: one-shot LLM-driven eval against the synthetic corpus
# for the *alpha* baseline only. This is the fastest path to a real
# correctness signal — alpha appends raw files to the prompt with no
# tools so it exercises the LLM directly without the MCP loop. Used to
# (1) verify the LLM backend works end-to-end, (2) trigger T-04
# hallucination measurement on a real response, (3) eyeball the output
# before committing to the full 4-baseline run.
#
# Backend selection:
#   - With ANTHROPIC_API_KEY in env → uses Anthropic API directly
#     (LLM_BACKEND=api LLM_MODEL=claude-sonnet-4-6 by default)
#   - Without API key → falls back to the claude CLI binary
#     (LLM_BACKEND=cli, needs `claude` on PATH and CLIWRAP_AGENT set
#     to a cliwrap-agent binary path — see internal/eval/llm_cli.go
#     for setup; CKG does not install cliwrap-agent).
#
# Override via:
#   make eval-llm-smoke LLM_BACKEND=api
#   make eval-llm-smoke TASKS_GLOB='eval/tasks/synthetic-T*.yaml'
#   make eval-llm-smoke BASELINES=alpha,beta,gamma,delta
#   make eval-llm-smoke N_RUNS=5
#
# N_RUNS controls multi-shot averaging (Axis 1, 2026-05-22). Cost
# scales linearly: N_RUNS=5 × BASELINES=4 = 20 LLM invocations per
# task. Use N_RUNS≥3 to get a meaningful mean±std for the
# non-deterministic LLM response (single-shot has been observed to
# swing from 0% to 36% hallucination rate on the same fixture).
#
# Output: eval/results/latest/{results.csv,report.md}. Read report.md
# Hallucination detail (T-04) section first — that's the signal this
# target was added to surface.
#
# Baseline semantics (internal/eval/baseline.go):
#   alpha — raw files appended to prompt, no tools (cheapest, noisiest)
#   beta  — get_subgraph result pre-appended (graph context, no tools)
#   gamma — tool-named in prompt but NOT pre-called (multi-turn cost
#           emulation; HANDOFF.md T-01 P0 — currently fiction without
#           γ tool-loop emulation, scores near-zero by design)
#   delta — smartContext pre-appended (task-tuned context, no tools)
#
# H1/H2 hypothesis check in the report uses alpha vs delta token
# savings + score delta. Adding beta and gamma to BASELINES surfaces
# the intermediate signals but does NOT change the H1/H2 numbers.
LLM_BACKEND ?= $(if $(ANTHROPIC_API_KEY),api,cli)
LLM_MODEL ?= claude-sonnet-4-6
TASKS_GLOB ?= eval/tasks/synthetic-T01-find-callers.yaml
BASELINES ?= alpha
N_RUNS ?= 1
eval-llm-smoke: build-no-viewer
	@mkdir -p eval/results/latest
	@echo "=== ckg eval-llm-smoke ==="
	@echo "  backend   = $(LLM_BACKEND)"
	@echo "  tasks     = $(TASKS_GLOB)"
	@echo "  baselines = $(BASELINES)"
	@echo "  n_runs    = $(N_RUNS)"
	@if [ "$(LLM_BACKEND)" = "cli" ] && [ -z "$(CLIWRAP_AGENT)" ]; then \
	    echo ""; \
	    echo "ERROR: LLM_BACKEND=cli requires CLIWRAP_AGENT to point at the"; \
	    echo "  cliwrap-agent binary (https://github.com/0xmhha/cli-wrapper)."; \
	    echo "  Either:"; \
	    echo "    1. Set ANTHROPIC_API_KEY to use the API backend instead, OR"; \
	    echo "    2. Install cliwrap-agent and set CLIWRAP_AGENT=/path/to/agent"; \
	    exit 1; \
	fi
	@# Rebuild every time. The graph is small (synthetic fixture) and
	@# the language set / fixture content changes from time to time —
	@# a stale eval/.synthetic-data was the smoking gun behind the
	@# `VaultService.depositFn` hallucination chain in the first
	@# 4-axis smoke run (the TS symbol existed in source but the
	@# graph was Go-only). One rebuild ≈ one second; the cost is
	@# orders of magnitude smaller than the LLM call.
	@rm -rf eval/.synthetic-data
	@echo "Building synthetic graph..."
	./bin/ckg build --src=testdata/synthetic --out=eval/.synthetic-data --lang=go,ts,sol
	./bin/ckg eval \
	    --tasks='$(TASKS_GLOB)' \
	    --graph=eval/.synthetic-data \
	    --baselines=$(BASELINES) \
	    --llm-backend=$(LLM_BACKEND) \
	    --llm=$(LLM_MODEL) \
	    --n-runs=$(N_RUNS) \
	    --out=eval/results/latest
	@echo ""
	@echo "=== Report ==="
	@cat eval/results/latest/report.md
	@echo ""
	@echo "=== Done. Hallucination detail above (T-04 V1). ==="

# lint-viewer: ESLint over web/viewer-next/. The primary concern is
# react-hooks/rules-of-hooks — viewer once shipped a regression where a
# useCallback was placed below an early return, producing React error
# #310 ("Rendered fewer hooks than expected") on the first node click
# (commit 50ee9f7). Enforcing the hooks rule in lint catches the same
# class of bug at PR time instead of at runtime in the user's browser.
#
# Requires `npm install` to have been run in web/viewer-next once;
# the rule depends on eslint-plugin-react-hooks being present.
lint-viewer:
	cd web/viewer-next && npm run lint

# audit: dependency-tree vulnerability scan for both languages.
#
#   - govulncheck (Go side): hits the Go vulnerability DB via
#     golang.org/x/vuln/cmd/govulncheck. Reports only call-graph-reachable
#     vulns by default — false-positive rate is much lower than naive
#     fingerprint scans.
#   - npm audit (web/viewer-next): tree-fingerprint scan against npm's
#     advisory DB. Gated at --audit-level=high so transitive low/moderate
#     warnings (npm's stretch use of moderate) don't fail CI.
#
# Both must pass for the target to succeed. govulncheck is a soft
# dependency: if not installed, we surface the install hint and exit
# non-zero so operators know the gate is incomplete.
audit:
	@echo "=== Go module vulnerabilities (govulncheck) ==="
	@if command -v govulncheck >/dev/null 2>&1; then \
	    govulncheck ./...; \
	else \
	    echo "govulncheck not installed."; \
	    echo "  install: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	    exit 1; \
	fi
	@echo ""
	@echo "=== npm dependencies (web/viewer-next) ==="
	cd web/viewer-next && npm audit --audit-level=high

clean:
	rm -rf bin/ /tmp/ckg-* coverage.out \
	       web/viewer-next/.next/ web/viewer-next/out/
