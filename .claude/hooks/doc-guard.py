#!/usr/bin/env python3
"""Documentation-discipline guard hook for Claude Code.

Modes (argv[1]):
  pre  — PreToolUse on Write|Edit: if the target is docs/VISION.md (Tier 1),
         ask the user to confirm (it is append-mostly, never shrunk/deleted).
  post — PostToolUse on Write: if a new .md under docs/ is not yet listed in
         docs/DOC-MAP.md, remind to register it (or write an ADR instead).

Communicates via JSON on stdout (permission/additionalContext); never blocks
hard, so legitimate edits still proceed after confirmation.
"""
import sys
import json
import os


def emit(obj):
    print(json.dumps(obj))


def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "pre"
    try:
        data = json.load(sys.stdin)
    except Exception:
        return 0

    tool_input = data.get("tool_input") or {}
    fp = (tool_input.get("file_path") or "").replace("\\", "/")

    if mode == "pre":
        if fp.endswith("docs/VISION.md"):
            emit({
                "hookSpecificOutput": {
                    "hookEventName": "PreToolUse",
                    "permissionDecision": "ask",
                    "permissionDecisionReason": (
                        "docs/VISION.md is Tier 1 (project vision): append-mostly, "
                        "must not be shrunk or deleted. Confirm this edit preserves "
                        "the vision rather than pruning it."
                    ),
                }
            })
        return 0

    # post
    if data.get("tool_name") == "Write" and "/docs/" in fp and fp.endswith(".md"):
        base = os.path.basename(fp)
        if base != "DOC-MAP.md" and "/docs/archive/" not in fp:
            idx = fp.rfind("/docs/")
            docmap = fp[:idx] + "/docs/DOC-MAP.md"
            registered = True
            try:
                with open(docmap, "r", encoding="utf-8") as f:
                    registered = base in f.read()
            except Exception:
                registered = True  # cannot read index -> don't nag
            if not registered:
                emit({
                    "hookSpecificOutput": {
                        "hookEventName": "PostToolUse",
                        "additionalContext": (
                            f"Doc-discipline reminder: '{base}' is not yet listed in "
                            "docs/DOC-MAP.md. Per CLAUDE.md, register it (tier + one-line) "
                            "in the same change. If it records a new decision, prefer an "
                            "ADR under docs/adr/ instead of a free-standing doc."
                        ),
                    }
                })
    return 0


if __name__ == "__main__":
    sys.exit(main())
