.PHONY: all build viewer viewer-old test test-race lint clean

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

# Legacy esbuild viewer kept during the migration window. Use
# `make viewer-old build` to fall back to web/viewer/. Slated for
# removal once the Next.js viewer is verified end-to-end
# (see docs/VIEWER-PERF-CLUSTERING.md).
viewer-old:
	cd web/viewer && npm install && node esbuild.config.js
	rm -rf internal/server/web_assets
	mkdir -p internal/server/web_assets/assets
	cp web/viewer/index.html internal/server/web_assets/index.html
	cp web/viewer/dist/viewer.js internal/server/web_assets/assets/viewer.js
	if [ -f web/viewer/dist/viewer.js.map ]; then cp web/viewer/dist/viewer.js.map internal/server/web_assets/assets/viewer.js.map; fi

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

lint:
	$(GO) vet ./...

clean:
	rm -rf bin/ /tmp/ckg-* coverage.out \
	       web/viewer/dist/ \
	       web/viewer-next/.next/ web/viewer-next/out/
