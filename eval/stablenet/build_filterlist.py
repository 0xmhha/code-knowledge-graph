#!/usr/bin/env python3
"""
Convert BUILD_SOURCE_FILES.md (md table of go-stablenet build inputs)
into a CKG --files-from JSON ({include, exclude}).

Why this exists:
  CKG's discovery walks the whole tree. We want the parser to see only
  the files that `make all` actually compiles into the 13 binaries on
  darwin/arm64 — the same set BUILD_SOURCE_FILES.md enumerates from
  `go list -deps ./cmd/...`. Anything else (_test.go, testdata,
  platform-foreign build-tagged files, generators) is filtered out.
"""

import json
import os
import re
import sys
from pathlib import Path

_STABLENET_SRC = os.environ.get("STABLENET_SRC")
if not _STABLENET_SRC:
    sys.exit(
        "error: STABLENET_SRC env var required "
        "(absolute path to go-stablenet-latest checkout)"
    )
MD = Path(_STABLENET_SRC) / ".claude/docs/BUILD_SOURCE_FILES.md"
OUT = Path(os.environ.get("OUT", "/tmp/ckg-stablenet-prep/stablenet-files.json"))

# Match a data row in section 2.x tables:
#   | <num> | `<pkg>` | `f1.go`, `f2.go`, ... | [optional flag col]
# Bold (**) wrappers are optional on both columns.
ROW = re.compile(
    r"^\|\s*\d+\s*\|\s*\*{0,2}`([^`]+)`\*{0,2}\s*\|\s*(.+?)\s*\|",
    re.MULTILINE,
)

# Inside the file cell, pull every `*.go` token regardless of bold.
FILE = re.compile(r"`([^`]+\.go)`")

text = MD.read_text()

includes: list[str] = []
seen: set[str] = set()
duplicates: list[str] = []
package_count = 0

for row in ROW.finditer(text):
    pkg, files_cell = row.group(1), row.group(2)
    # Root-of-module entry uses the import path itself; on disk it lives
    # at the repo root, so no directory prefix.
    if pkg == "github.com/ethereum/go-ethereum":
        prefix = ""
    else:
        prefix = pkg.rstrip("/") + "/"
    package_count += 1
    for f in FILE.finditer(files_cell):
        rel = prefix + f.group(1)
        if rel in seen:
            duplicates.append(rel)
            continue
        seen.add(rel)
        includes.append(rel)

# Deterministic order for reproducible builds.
includes.sort()

out_obj = {
    "include": includes,
    "exclude": ["**/*_test.go", "**/testdata/**"],
}

OUT.parent.mkdir(parents=True, exist_ok=True)
OUT.write_text(json.dumps(out_obj, indent=2) + "\n")

print(f"packages parsed     : {package_count}")
print(f"unique files        : {len(includes)}")
print(f"duplicate file rows : {len(duplicates)}", file=sys.stderr)
if duplicates:
    print("  (first 5):", duplicates[:5], file=sys.stderr)
print(f"wrote {OUT}")
