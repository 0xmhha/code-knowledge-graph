#!/usr/bin/env python3
"""
Collect the β / γ / δ input contexts for T01 by hitting the live MCP
server. (α was reproduced by dumpFiles in the prior shell step.)

T01: "List every Go function or method that DIRECTLY calls
core.NewBlockChain. Return qualified names only."
"""
import json
import os
import subprocess
import sys
from pathlib import Path

CKG = os.environ.get("CKG", "bin/ckg")
GRAPH = os.environ.get("GRAPH", "/tmp/ckg-stablenet")
OUT_DIR = Path(os.environ.get("OUT_DIR", "/tmp/ckg-stablenet-prep/sim"))


def main():
    proc = subprocess.Popen(
        [CKG, "mcp", f"--graph={GRAPH}"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        bufsize=0,
    )
    rid = [0]

    def call(method, params=None):
        rid[0] += 1
        msg = {"jsonrpc": "2.0", "id": rid[0], "method": method,
               "params": params or {}}
        proc.stdin.write((json.dumps(msg) + "\n").encode())
        proc.stdin.flush()
        return json.loads(proc.stdout.readline())

    def call_tool(name, args):
        return call("tools/call", {"name": name, "arguments": args})

    try:
        call("initialize", {"protocolVersion": "2024-11-05", "capabilities": {},
                            "clientInfo": {"name": "sim", "version": "1"}})
        proc.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
        proc.stdin.flush()

        # β — full subgraph dump (eval β baseline does
        # store.SubgraphByQname("", 99); we approximate via tool call).
        beta = call_tool("get_subgraph",
                         {"seed_qname": "core.NewBlockChain", "depth": 99})
        (OUT_DIR / "beta-input.json").write_text(
            json.dumps(beta.get("result", {}).get("structuredContent", {}), indent=2))
        b_nodes = len((beta.get("result", {}).get("structuredContent", {}) or {}).get("nodes", []))
        b_edges = len((beta.get("result", {}).get("structuredContent", {}) or {}).get("edges", []))
        print(f"β: {b_nodes} nodes / {b_edges} edges saved")

        # γ — agent runs find_callers
        gamma = call_tool("find_callers",
                          {"qname": "core.NewBlockChain", "depth": 1})
        (OUT_DIR / "gamma-input.json").write_text(
            json.dumps(gamma.get("result", {}).get("structuredContent", {}), indent=2))
        g_nodes = len((gamma.get("result", {}).get("structuredContent", {}) or {}).get("nodes", []))
        print(f"γ: find_callers returned {g_nodes} nodes")

        # δ — smart context for the exact task phrasing
        delta = call_tool("get_context_for_task", {
            "task_description":
                "List every Go function or method that DIRECTLY calls "
                "core.NewBlockChain (depth = 1, no transitive callers). "
                "Return the fully qualified names only, one per line, no commentary",
            "budget_tokens": 4000,
            "max_bodies": 5,
        })
        (OUT_DIR / "delta-input.json").write_text(
            json.dumps(delta.get("result", {}).get("structuredContent", {}), indent=2))
        d_keys = list((delta.get("result", {}).get("structuredContent", {}) or {}).keys())
        print(f"δ: smartctx pack keys = {d_keys}")
    finally:
        try: proc.stdin.close()
        except Exception: pass
        try: proc.wait(timeout=5)
        except subprocess.TimeoutExpired: proc.kill()


if __name__ == "__main__":
    main()
