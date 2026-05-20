.PHONY: all build viewer test test-race lint lint-viewer fmt fmt-check install-hooks audit clean

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

build: viewer
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

build-no-viewer:
	$(GO) build -o bin/ckg ./cmd/ckg

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
