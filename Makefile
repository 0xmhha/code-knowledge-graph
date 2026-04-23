.PHONY: all build viewer test test-race lint clean

GO ?= go

all: build

viewer:
	cd web/viewer && npm install && node esbuild.config.js

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
