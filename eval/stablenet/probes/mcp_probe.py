#!/usr/bin/env python3
"""
End-to-end MCP smoke probe.

Spawns `ckg mcp --graph=<path>` as a subprocess and drives it via NDJSON
JSON-RPC (one message per line on stdin/stdout). Covers all 8 registered
tools — the 6 spec tools plus the 2 extras (impact_of_change,
evidence_for_intent) — so we get evidence each one returns a structured
result against the real go-stablenet graph.
"""

import json
import os
import subprocess
import sys
import time
from pathlib import Path

CKG = os.environ.get("CKG", "bin/ckg")
GRAPH = os.environ.get("GRAPH", "/tmp/ckg-stablenet")
OUT = Path(os.environ.get("OUT", "/tmp/ckg-stablenet-prep/mcp-probe-results.json"))


def main() -> int:
    proc = subprocess.Popen(
        [CKG, "mcp", f"--graph={GRAPH}"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        bufsize=0,
    )
    assert proc.stdin and proc.stdout
    next_id = [0]

    def call(method: str, params: dict | None = None) -> dict:
        next_id[0] += 1
        msg = {"jsonrpc": "2.0", "id": next_id[0], "method": method,
               "params": params or {}}
        proc.stdin.write((json.dumps(msg) + "\n").encode())
        proc.stdin.flush()
        line = proc.stdout.readline()
        if not line:
            raise RuntimeError("mcp server closed stdout unexpectedly")
        return json.loads(line)

    def call_tool(name: str, args: dict) -> dict:
        return call("tools/call", {"name": name, "arguments": args})

    findings: list[dict] = []

    def record(label: str, resp: dict, hint: str = "") -> dict:
        is_err = "error" in resp
        # Tool errors are surfaced as result.isError=true in the MCP spec.
        result = resp.get("result", {})
        tool_err = bool(result.get("isError"))
        ok = not is_err and not tool_err
        # First node/edge as a compact correctness signal.
        sample = None
        structured = result.get("structuredContent") or {}
        if isinstance(structured, dict):
            nodes = structured.get("nodes") or []
            if nodes:
                n = nodes[0]
                sample = {
                    "name": n.get("name"),
                    "qname": n.get("qname"),
                    "type": n.get("type"),
                    "file": n.get("file"),
                    "line": n.get("line"),
                }
        entry = {
            "label": label,
            "ok": ok,
            "hint": hint,
            "rpc_error": resp.get("error"),
            "tool_isError": tool_err,
            "result_summary": {
                "node_count": len((structured or {}).get("nodes", []) or []),
                "edge_count": len((structured or {}).get("edges", []) or []),
                "first_node": sample,
                "content_text_preview": (
                    (result.get("content", [{}])[0].get("text") or "")[:140]
                    if result.get("content") else None
                ),
            },
        }
        findings.append(entry)
        flag = "OK " if ok else "ERR"
        print(f"  [{flag}] {label}: nodes={entry['result_summary']['node_count']} "
              f"edges={entry['result_summary']['edge_count']}")
        return entry

    try:
        # 1) MCP handshake
        init = call("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "smoke-probe", "version": "0.0.1"},
        })
        record("initialize", init, "MCP handshake")
        # initialized notification (optional in some impls, harmless if ignored)
        proc.stdin.write(json.dumps({
            "jsonrpc": "2.0", "method": "notifications/initialized",
        }).encode() + b"\n")
        proc.stdin.flush()

        # 2) tools/list — confirm 8 tools registered
        listed = call("tools/list", {})
        tools = (listed.get("result") or {}).get("tools") or []
        names = sorted(t.get("name") for t in tools)
        print(f"  tools/list -> {len(names)} tools: {names}")
        record("tools/list", listed,
               hint=f"discovered={len(names)}; names={names}")

        # 3) find_symbol — FindSymbol matches qualified_name. Use suffix
        # mode (exact=False) so plain symbol names resolve too.
        fs = call_tool("find_symbol", {
            "name": "NewBlockChain", "language": "go", "exact": False,
        })
        record("find_symbol(NewBlockChain, suffix)", fs)
        nodes = ((fs.get("result") or {}).get("structuredContent") or {}).get("nodes", []) or []
        # Prefer the core.NewBlockChain function over any same-named method.
        seed_qname = None
        for n in nodes:
            if n.get("type") == "Function" and n.get("qname", "").endswith(".NewBlockChain"):
                seed_qname = n["qname"]
                break
        if not seed_qname and nodes:
            seed_qname = nodes[0].get("qname")
        print(f"  seed_qname = {seed_qname}")

        # 4) Tools needing a seed
        for tool_name in ("find_callers", "find_callees", "get_subgraph",
                          "impact_of_change"):
            args: dict
            if tool_name == "get_subgraph":
                args = {"seed_qname": seed_qname, "depth": 1}
            elif tool_name == "impact_of_change":
                args = {"seed_qname": seed_qname, "depth": 2}
            else:  # find_callers / find_callees
                args = {"qname": seed_qname, "depth": 1}
            r = call_tool(tool_name, args)
            record(f"{tool_name}({seed_qname})", r)

        # 5) search_text
        r = call_tool("search_text", {"query": "WBFT consensus", "top_k": 5})
        record("search_text('WBFT consensus')", r)

        # 6) get_context_for_task — schema param is `task_description`.
        # NOTE: avoid trailing punctuation; smartctx tokeniser concatenates
        # tokens into an FTS5 query and a literal "." raises a syntax error
        # (observed bug; reproduce by adding a period to task_description).
        r = call_tool("get_context_for_task", {
            "task_description":
                "Investigate how WBFT prepare messages are validated",
            "budget_tokens": 4000,
            "max_bodies": 3,
        })
        record("get_context_for_task(WBFT prepare)", r)

        # 7) evidence_for_intent — extra tool
        r = call_tool("evidence_for_intent", {
            "intent": "Add a new system contract address for governance staking.",
        })
        record("evidence_for_intent(...)", r)

    finally:
        try:
            proc.stdin.close()
        except Exception:
            pass
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()

    OUT.write_text(json.dumps(findings, indent=2) + "\n")
    ok_count = sum(1 for f in findings if f["ok"])
    print(f"\nsummary: {ok_count}/{len(findings)} probes OK -> {OUT}")
    return 0 if ok_count == len(findings) else 1


if __name__ == "__main__":
    sys.exit(main())
