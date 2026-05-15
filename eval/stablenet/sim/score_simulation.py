#!/usr/bin/env python3
"""
Replicate eval V0's scoring path in Python and run it against
hand-authored LLM answers for T01 under each baseline.

extractSymbols (port of internal/eval/score.go:142): split on " ,\n`\"",
keep tokens that contain `.` (not leading/trailing), trim `.:;()`.

PrecisionRecall: standard set IoU as P / R.
"""
import json
from pathlib import Path

T01_EXPECTED = ["eth.New", "tests.BlockTest.Run", "utils.MakeChain"]


def extract_symbols(s: str) -> list[str]:
    """Port of internal/eval/score.go::extractSymbols (V0)."""
    out = []
    for tok in (
        s.replace(",", " ").replace("\n", " ")
         .replace("`", " ").replace('"', " ").split()
    ):
        if "." in tok and not tok.startswith(".") and not tok.endswith("."):
            out.append(tok.strip(".:;()"))
    return out


def pr(got: list[str], want: list[str]) -> tuple[float, float, float]:
    g, w = set(got), set(want)
    tp = len(g & w)
    p = tp / len(g) if g else 0.0
    r = tp / len(w) if w else 0.0
    return p, r, (p + r) / 2


# Each entry: (baseline, style, raw output a typical LLM would emit given the
# baseline's input context — see comments)
CASES: list[tuple[str, str, str]] = [
    # α — only saw 5 files under core/asm/. NewBlockChain never mentioned.
    ("α", "minimal", "I cannot find any callers of core.NewBlockChain in the provided files."),
    ("α", "verbose",
     "Based on the supplied source files (core/asm/asm.go, "
     "core/asm/compiler.go, ...), there is no reference to "
     "core.NewBlockChain. The dump contains only core/asm package code."),

    # β — receives the full subgraph as JSON in user content. The relevant
    # callers are buried in 121K nodes, but the LLM is competent at scanning.
    # Typical helpful answer cites file paths for readability:
    ("β", "minimal", "eth.New\ntests.BlockTest.Run\nutils.MakeChain"),
    ("β", "verbose",
     "Direct callers of core.NewBlockChain found in the subgraph:\n"
     "- eth.New (eth/backend.go)\n"
     "- tests.BlockTest.Run (tests/block_test_util.go)\n"
     "- utils.MakeChain (cmd/utils/flags.go)"),

    # γ — find_callers result is given verbatim. seed (core.NewBlockChain)
    # appears in the nodes list too; the LLM must filter it out.
    ("γ", "minimal", "eth.New\ntests.BlockTest.Run\nutils.MakeChain"),
    ("γ", "verbose",
     "The function `core.NewBlockChain` is called directly by:\n"
     "- `eth.New` in eth/backend.go\n"
     "- `tests.BlockTest.Run` in tests/block_test_util.go\n"
     "- `utils.MakeChain` in cmd/utils/flags.go"),

    # δ — get_context_for_task returned `{}` for this query (BM25 miss on
    # the prose phrasing). LLM has no context to ground its answer.
    ("δ", "minimal", "I cannot answer without more context about the codebase."),
    ("δ", "verbose",
     "The smart-context tool returned no matches for this query, "
     "so I cannot reliably list the callers of core.NewBlockChain."),
]


def main() -> None:
    print(f"T01 expected: {T01_EXPECTED}\n")
    rows = []
    for baseline, style, answer in CASES:
        got = extract_symbols(answer)
        p, r, score = pr(got, T01_EXPECTED)
        rows.append({
            "baseline": baseline, "style": style,
            "extracted_tokens": got, "score": round(score, 4),
            "precision": round(p, 4), "recall": round(r, 4),
            "answer": answer,
        })
        print(f"[{baseline}/{style:7}] P={p:.2f} R={r:.2f} score={score:.4f}")
        print(f"  extracted: {got}")
        print()

    Path("/tmp/ckg-stablenet-prep/sim/score-simulation.json").write_text(
        json.dumps(rows, indent=2) + "\n"
    )
    # Pivot — score by (baseline, style)
    print("\n=== score matrix ===")
    print(f"{'baseline':<10} {'minimal':>10} {'verbose':>10}")
    print("-" * 36)
    by_b: dict[str, dict[str, float]] = {}
    for r in rows:
        by_b.setdefault(r["baseline"], {})[r["style"]] = r["score"]
    for b in ["α", "β", "γ", "δ"]:
        m = by_b.get(b, {})
        print(f"{b:<10} {m.get('minimal', 0):>10.4f} {m.get('verbose', 0):>10.4f}")


if __name__ == "__main__":
    main()
