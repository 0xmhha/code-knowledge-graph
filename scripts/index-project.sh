#!/usr/bin/env bash
#
# index-project.sh — deterministic, repeatable ckg graph build for a project.
#
# Why this exists: `ckg build` is LLM-free and deterministic, but the *choices*
# around it (which languages, whether tests are included, cache policy, output
# path) were re-derived by hand on every run, so different DBs came out with
# different coverage. This script pins those choices in one place. Same inputs
# in -> same graph out. No AI in the loop.
#
# What it builds (per docs/adr/0002-staged-graph-composition.md):
#   - Stage 1: production build set (the `go build` compile packages).
#   - Stage 2: test overlay — `_test.go` files of those packages add their own
#     symbols/edges but never override production resolution. Test code is thus
#     included, scoped to files that participate in the build.
#
# Usage:
#   scripts/index-project.sh <src> <name>
#
# Environment overrides (all optional):
#   LANG_SET       languages passed to --lang           (default: go,sol)
#   OUT_ROOT       directory the graph dir is created in (default: ckg repo root)
#   NO_CACHE       1 = clean full rebuild (--no-cache)   (default: 1)
#   FAIL_ON_PARSE  1 = abort if any file fails to parse  (default: 1)
#   CKG_BIN        path to the ckg binary                (default: <repo>/bin/ckg)
#   MAIN_PKG       Go main package(s) to scope the graph to what the *binary*
#                  actually compiles, e.g. "./cmd/gstable". When set, the parsed
#                  symbol set is restricted to `go list -deps <MAIN_PKG>`
#                  in-module packages (+ their related _test.go) via --files-from.
#                  Empty = index the whole `go build ./...` set. (default: empty)
#   SOL_INCLUDE    when MAIN_PKG is set, glob(s) (comma-separated) for the
#                  production Solidity to keep      (default: systemcontracts/**/*.sol)
#   EXTRA_EXCLUDE  when MAIN_PKG is set, extra exclude glob(s) (comma-separated)
#                  applied on top of the include set; exclude trumps include.
#                  e.g. "systemcontracts/solidity/test/**" to drop test mocks.
#                  (default: empty)
#
# Note: --files-from restricts the *parsers* only. The temporal (git-history)
# pass still records Hunk nodes for files in commit history, including files
# deleted from the current tree — that is change history, not build code.
#
# Output dir: $OUT_ROOT/.ckg-<name>, suffixed with the source's short commit
# SHA when <src> is a git repo (via ckg --out-tag=auto-commit-hash), so each
# commit gets its own reproducible graph.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CKG_BIN="${CKG_BIN:-$REPO_ROOT/bin/ckg}"

usage() {
	echo "usage: $0 <src> <name>" >&2
	echo "  <src>   source tree to index" >&2
	echo "  <name>  short label; graph dir becomes .ckg-<name>[-<sha>]" >&2
	exit 2
}
[ "$#" -ge 2 ] || usage
SRC="$1"
NAME="$2"

LANG_SET="${LANG_SET:-go,sol}"
OUT_ROOT="${OUT_ROOT:-$REPO_ROOT}"
NO_CACHE="${NO_CACHE:-1}"
FAIL_ON_PARSE="${FAIL_ON_PARSE:-1}"
MAIN_PKG="${MAIN_PKG:-}"
SOL_INCLUDE="${SOL_INCLUDE:-systemcontracts/**/*.sol}"
EXTRA_EXCLUDE="${EXTRA_EXCLUDE:-}"

[ -x "$CKG_BIN" ] || { echo "ERROR: ckg binary not found/executable at $CKG_BIN (run 'make build')" >&2; exit 1; }
[ -d "$SRC" ] || { echo "ERROR: src not found: $SRC" >&2; exit 1; }

OUT="$OUT_ROOT/.ckg-$NAME"

args=(build --src="$SRC" --out="$OUT" --lang="$LANG_SET")

# Optional: scope the parsed symbol set to what the given binary compiles.
# Generates a --files-from JSON from `go list -deps <MAIN_PKG>` (in-module
# packages only) plus the production Solidity globs. Deterministic per commit.
if [ -n "$MAIN_PKG" ]; then
	command -v go >/dev/null 2>&1 || { echo "ERROR: MAIN_PKG set but 'go' not on PATH" >&2; exit 1; }
	MODPATH="$(cd "$SRC" && go list -m 2>/dev/null | head -1)"
	[ -n "$MODPATH" ] || { echo "ERROR: MAIN_PKG set but $SRC is not a Go module" >&2; exit 1; }
	FILTER_FILE="$OUT_ROOT/.ckg-$NAME.files.json"
	# shellcheck disable=SC2086
	( cd "$SRC" && go list -deps $MAIN_PKG 2>/dev/null ) \
		| grep "^$MODPATH" | sed "s#^$MODPATH/\{0,1\}##" | sort -u \
		| MODPATH="$MODPATH" SOL_INCLUDE="$SOL_INCLUDE" EXTRA_EXCLUDE="$EXTRA_EXCLUDE" python3 -c '
import sys, os, json
dirs = [l.strip() for l in sys.stdin]
inc = sorted({("*.go" if d == "" else d + "/*.go") for d in dirs})
inc += [g.strip() for g in os.environ["SOL_INCLUDE"].split(",") if g.strip()]
exc = [g.strip() for g in os.environ.get("EXTRA_EXCLUDE", "").split(",") if g.strip()]
json.dump({"include": inc, "exclude": exc}, open(sys.argv[1], "w"), indent=1)
print(f"binary-scoped filter: {len(inc)} include, {len(exc)} exclude patterns", file=sys.stderr)
' "$FILTER_FILE"
	echo "== files-from: $FILTER_FILE ($(grep -c '"' "$FILTER_FILE" 2>/dev/null) lines) =="
	args+=(--files-from="$FILTER_FILE")
fi

FINAL_OUT="$OUT"
if git -C "$SRC" rev-parse --git-dir >/dev/null 2>&1; then
	args+=(--out-tag=auto-commit-hash)
	# ckg suffixes with the first 12 chars of the full HEAD SHA; mirror that so
	# the summary below can find the graph it just wrote.
	SHA12="$(git -C "$SRC" rev-parse HEAD | cut -c1-12)"
	FINAL_OUT="$OUT-$SHA12"
fi
[ "$NO_CACHE" = "1" ] && args+=(--no-cache)
[ "$FAIL_ON_PARSE" = "1" ] && args+=(--fail-on-parse-errors)

echo "== ckg $("$CKG_BIN" version 2>/dev/null || echo '?') =="
echo "+ $CKG_BIN ${args[*]}"
"$CKG_BIN" "${args[@]}"

# Post-build summary: prove language coverage and test inclusion from the
# manifest, so a build that silently dropped a language is visible immediately.
MANIFEST="$FINAL_OUT/manifest.json"
if [ -f "$MANIFEST" ]; then
	echo ""
	echo "== summary: $FINAL_OUT =="
	python3 - "$MANIFEST" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
langs = m.get("languages", {})
files = m.get("files", [])
test_go = sum(1 for f in files if f.get("path", "").endswith("_test.go"))
print(f"  ckg_version   : {m.get('ckg_version')}")
print(f"  schema_version: {m.get('schema_version')}")
print(f"  src_commit    : {m.get('src_commit')}")
print(f"  languages     : " + ", ".join(f"{k}={v}" for k, v in sorted(langs.items())))
print(f"  files indexed : {len(files)} (of which _test.go: {test_go})")
print(f"  nodes/edges   : {m.get('stats',{}).get('nodes')} / {m.get('stats',{}).get('edges')}")
print(f"  parse_errors  : {m.get('parse_errors_count')}")
PY
else
	echo "WARN: manifest not found at $MANIFEST" >&2
fi
