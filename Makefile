.PHONY: all build viewer test test-race lint clean

GO ?= go

all: build

viewer:
	cd web/viewer && npm install && node esbuild.config.js
	mkdir -p internal/server/web_assets/assets
	cp web/viewer/index.html internal/server/web_assets/index.html
	cp web/viewer/dist/viewer.js internal/server/web_assets/assets/viewer.js
	if [ -f web/viewer/dist/viewer.js.map ]; then cp web/viewer/dist/viewer.js.map internal/server/web_assets/assets/viewer.js.map; fi

build: viewer
	$(GO) build -o bin/ckg ./cmd/ckg

build-no-viewer:
	$(GO) build -o bin/ckg ./cmd/ckg

test:
	$(GO) test ./...

test-race:
	$(GO) test -race -coverprofile=coverage.out ./...

lint:
	$(GO) vet ./...

clean:
	rm -rf bin/ /tmp/ckg-* coverage.out web/viewer/dist/
