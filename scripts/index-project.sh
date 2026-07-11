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

[ -x "$CKG_BIN" ] || { echo "ERROR: ckg binary not found/executable at $CKG_BIN (run 'make build')" >&2; exit 1; }
[ -d "$SRC" ] || { echo "ERROR: src not found: $SRC" >&2; exit 1; }

OUT="$OUT_ROOT/.ckg-$NAME"

args=(build --src="$SRC" --out="$OUT" --lang="$LANG_SET")

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
