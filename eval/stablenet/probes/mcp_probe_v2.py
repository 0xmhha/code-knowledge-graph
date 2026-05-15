#!/usr/bin/env python3
"""
Regression probe for VERIFICATION_REPORT §3.1 B1/B2 fixes.

B1 — get_context_for_task FTS5 punctuation: replays the original failing
input (task_description ending in `.`) that used to surface
`fts5: syntax error near "."`. The fix at
internal/persist/sqlite.go:trimFTSToken() now strips trailing punctuation
from prefix-tagged tokens.

B2 — find_symbol description ↔ behaviour mismatch: replays the call that
previously false-emptied (`{"name":"NewBlockChain","exact":true}`) and
the documented alternative (full qname with exact=true; bare name with
exact=false). Description rewrite at internal/mcp/tools.go now spells
out the contract so the false-empty path is at least discoverable.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

CKG = os.environ.get("CKG", "bin/ckg")
GRAPH = os.environ.get("GRAPH", "/tmp/ckg-stablenet")
OUT = Path(os.environ.get("OUT", "/tmp/ckg-stablenet-prep/mcp-probe-v2-results.json"))


def main() -> int:
    proc = subprocess.Popen(
        [CKG, "mcp", f"--graph={GRAPH}"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        bufsize=0,
    )
    assert proc.stdin and proc.stdout
    rid = [0]

    def call(method: str, params: dict | None = None) -> dict:
        rid[0] += 1
        msg = {"jsonrpc": "2.0", "id": rid[0], "method": method,
               "params": params or {}}
        proc.stdin.write((json.dumps(msg) + "\n").encode())
        proc.stdin.flush()
        line = proc.stdout.readline()
        if not line:
            raise RuntimeError("mcp server closed stdout")
        return json.loads(line)

    def call_tool(name: str, args: dict) -> dict:
        return call("tools/call", {"name": name, "arguments": args})

    findings: list[dict] = []

    def record(label: str, resp: dict, expectation: str) -> None:
        rpc_err = resp.get("error")
        result = resp.get("result") or {}
        tool_err = bool(result.get("isError"))
        sc = result.get("structuredContent") or {}
        nodes = sc.get("nodes") or []
        edges = sc.get("edges") or []
        ok = not rpc_err and not tool_err
        entry = {
            "label": label,
            "expectation": expectation,
            "ok": ok,
            "rpc_error": rpc_err,
            "tool_isError": tool_err,
            "nodes": len(nodes),
            "edges": len(edges),
            "first_node": ({
                "name": nodes[0].get("name"),
                "qname": nodes[0].get("qname"),
                "type": nodes[0].get("type"),
            } if nodes else None),
        }
        findings.append(entry)
        tag = "OK " if ok else "ERR"
        print(f"  [{tag}] {label}\n         → nodes={entry['nodes']} edges={entry['edges']} "
              f"err={rpc_err.get('message') if rpc_err else None}")

    try:
        # Handshake
        call("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "regression-probe", "version": "v2"},
        })
        proc.stdin.write(json.dumps({
            "jsonrpc": "2.0", "method": "notifications/initialized",
        }).encode() + b"\n")
        proc.stdin.flush()

        # ─── B1 regression: trailing period in task_description ────────
        print("[B1] get_context_for_task with trailing period (previously failed)")
        r = call_tool("get_context_for_task", {
            "task_description":
                "Investigate how WBFT prepare messages are validated.",
            "budget_tokens": 4000,
            "max_bodies": 3,
        })
        record(
            "B1: trailing-period task_description",
            r,
            "Was: fts5 syntax error near '.'. Expected after fix: 200 OK.",
        )

        # Bonus: even more punctuation chaos to confirm trimFTSToken is general.
        r = call_tool("get_context_for_task", {
            "task_description":
                "Where does (NewBlockChain) get called, and why? Show callers!",
            "budget_tokens": 4000,
            "max_bodies": 3,
        })
        record(
            "B1: heavy-punctuation task_description",
            r,
            "Parens / `?` / `!` mixed in. Expected: 200 OK (trimmed at token boundary).",
        )

        # ─── B2 regression: documented contract for find_symbol ────────
        print("\n[B2] find_symbol contract — exact vs suffix")
        # Documented usage 1: full qname + exact=true → must hit.
        r = call_tool("find_symbol", {
            "name": "core.NewBlockChain", "exact": True,
        })
        record(
            "B2a: full qname + exact=true",
            r,
            "Doc says exact=true matches qualified_name exactly. Expect ≥1 node.",
        )
        # Documented usage 2: bare name + exact=false → must hit (suffix).
        r = call_tool("find_symbol", {
            "name": "NewBlockChain", "exact": False,
        })
        record(
            "B2b: bare name + exact=false (suffix)",
            r,
            "Doc says exact=false treats input as suffix. Expect ≥1 node.",
        )
        # Anti-case (still false-empty by design, now expected per docs):
        r = call_tool("find_symbol", {
            "name": "NewBlockChain", "exact": True,
        })
        record(
            "B2c: bare name + exact=true (documented to false-empty)",
            r,
            "Doc warns this returns 0 — bare names need exact=false.",
        )

    finally:
        try: proc.stdin.close()
        except Exception: pass
        try: proc.wait(timeout=5)
        except subprocess.TimeoutExpired: proc.kill()

    OUT.write_text(json.dumps(findings, indent=2) + "\n")

    # ─── verdict ─────────────────────────────────────────────────────
    b1_a, b1_b, b2_a, b2_b, b2_c = findings
    print("\n=== verdict ===")
    b1_pass = b1_a["ok"] and b1_b["ok"]
    b2_pass = (b2_a["ok"] and b2_a["nodes"] >= 1
               and b2_b["ok"] and b2_b["nodes"] >= 1
               and b2_c["ok"] and b2_c["nodes"] == 0)
    print(f"  B1 FTS5 punctuation fix : {'PASS' if b1_pass else 'FAIL'}")
    print(f"  B2 find_symbol contract : {'PASS' if b2_pass else 'FAIL'}")
    print(f"  results -> {OUT}")
    return 0 if b1_pass and b2_pass else 1


if __name__ == "__main__":
    sys.exit(main())
