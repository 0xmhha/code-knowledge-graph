#!/usr/bin/env python3
"""sync_tasks.py — bidirectional sync between CKG task YAMLs and
the Coding Agent's known-issues.jsonl.

Usage:
    python3 sync_tasks.py --check           # detect drift (exit 1 if out of sync)
    python3 sync_tasks.py --apply           # write JSONL from YAML (YAML is source of truth)
    python3 sync_tasks.py --apply --reverse # write YAML from JSONL (JSONL is source of truth)

Environment:
    STABLENET_SRC   go-stablenet checkout path (for pre_fix_commit)
    JSONL_PATH      path to known-issues.jsonl (default: auto-detect)
"""

import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("ERROR: PyYAML required. Install: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

TASKS_DIR = Path(__file__).parent / "tasks"
DEFAULT_JSONL = Path(__file__).parent.parent.parent / "stablenet-ai-agent" / "benchmark" / "known-issues.jsonl"


def load_yamls() -> dict:
    """Load all task YAMLs keyed by id."""
    tasks = {}
    for f in sorted(TASKS_DIR.glob("T*.yaml")):
        with open(f) as fh:
            t = yaml.safe_load(fh)
        tasks[t["id"]] = t
    return tasks


def load_jsonl(path: Path) -> dict:
    """Load known-issues.jsonl keyed by issue_id."""
    issues = {}
    if not path.exists():
        return issues
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            obj = json.loads(line)
            issues[obj["issue_id"]] = obj
    return issues


def yaml_to_jsonl_entry(task: dict, commit: str) -> dict:
    """Convert a CKG task YAML dict to a known-issues JSONL entry."""
    expected = task.get("expected", {})
    symbols = expected.get("symbols", [])
    rubric = expected.get("rubric", [])

    fix_summary = task.get("description", "").strip()
    if len(fix_summary) > 200:
        fix_summary = fix_summary[:200] + "..."

    return {
        "issue_id": task["id"],
        "pre_fix_commit": commit,
        "request": task.get("description", ""),
        "ground_truth": {
            "files_changed": symbols if symbols else rubric,
            "fix_summary": fix_summary,
            "fix_diff_path": None,
        },
        "scoring": {
            "file_recall_target": 0.8,
            "behavior_match_target": "n/a",
        },
    }


def content_hash(task: dict) -> str:
    """Stable hash of task content for drift detection."""
    canonical = json.dumps(
        {"id": task.get("id") or task.get("issue_id"),
         "description": task.get("description") or task.get("request", ""),
         "expected": task.get("expected", task.get("ground_truth", {}))},
        sort_keys=True, ensure_ascii=True,
    )
    return hashlib.sha256(canonical.encode()).hexdigest()[:16]


def get_head_commit() -> str:
    src = os.environ.get("STABLENET_SRC", "")
    if not src:
        return ""
    try:
        out = subprocess.check_output(
            ["git", "-C", src, "rev-parse", "--short", "HEAD"],
            stderr=subprocess.DEVNULL,
        )
        return out.decode().strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def check(yamls: dict, issues: dict) -> bool:
    """Return True if in sync, False if drifted."""
    yaml_ids = set(yamls.keys())
    jsonl_ids = set(issues.keys())

    missing_in_jsonl = yaml_ids - jsonl_ids
    missing_in_yaml = jsonl_ids - yaml_ids
    common = yaml_ids & jsonl_ids

    drifted = []
    for tid in sorted(common):
        yh = content_hash(yamls[tid])
        jh = content_hash(issues[tid])
        if yh != jh:
            drifted.append(tid)

    ok = True
    if missing_in_jsonl:
        print(f"Missing in JSONL: {sorted(missing_in_jsonl)}")
        ok = False
    if missing_in_yaml:
        print(f"Missing in YAML:  {sorted(missing_in_yaml)}")
        ok = False
    if drifted:
        print(f"Content drifted:  {drifted}")
        ok = False
    if ok:
        print(f"In sync: {len(common)} tasks match.")
    return ok


def apply_yaml_to_jsonl(yamls: dict, jsonl_path: Path):
    """Write JSONL from YAML (YAML is source of truth)."""
    commit = get_head_commit()
    existing = load_jsonl(jsonl_path)

    for tid, task in sorted(yamls.items()):
        existing[tid] = yaml_to_jsonl_entry(task, commit)

    jsonl_path.parent.mkdir(parents=True, exist_ok=True)
    with open(jsonl_path, "w") as fh:
        for _, entry in sorted(existing.items()):
            fh.write(json.dumps(entry, ensure_ascii=False) + "\n")
    print(f"Wrote {len(existing)} entries to {jsonl_path}")


def main():
    parser = argparse.ArgumentParser(description="Sync CKG task YAMLs ↔ known-issues.jsonl")
    parser.add_argument("--check", action="store_true", help="detect drift (exit 1 if out of sync)")
    parser.add_argument("--apply", action="store_true", help="write JSONL from YAML")
    parser.add_argument("--reverse", action="store_true", help="with --apply, write YAML from JSONL instead")
    parser.add_argument("--jsonl", type=str, default="", help="path to known-issues.jsonl")
    args = parser.parse_args()

    jsonl_path = Path(args.jsonl) if args.jsonl else DEFAULT_JSONL

    if args.check:
        yamls = load_yamls()
        issues = load_jsonl(jsonl_path)
        if not check(yamls, issues):
            sys.exit(1)
        return

    if args.apply:
        if args.reverse:
            print("Reverse sync (JSONL → YAML) not yet implemented.")
            sys.exit(2)
        yamls = load_yamls()
        apply_yaml_to_jsonl(yamls, jsonl_path)
        return

    parser.print_help()


if __name__ == "__main__":
    main()
