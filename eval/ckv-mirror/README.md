# eval/ckv-mirror — CKV fixture mirror (ckg-NEW-5, Stage B)

LLM-free keyword-retrieval eval that **mirrors CKV's evaluation fixtures on the
ckg side**, so ckg (keyword / FTS) and ckv (semantic / embeddings) can be
cross-referenced on **one query set over one corpus**.

## Why

CKV scores its retrieval with natural-language *intents* against its own sample
corpus (`code-knowledge-vector/testdata/sample`: `server.go`, `cache.go`,
`token.sol`, `handler.ts`, `validator.go`, `client.js`, `docs/`). ckg answers
the same information need with **keyword** tools (`search_text`, `find_symbol`),
not embeddings. To compare the two on equal footing we index the *same code*
with ckg and probe it with keyword/exact queries derived from the CKV intents.

This is stage 1 (mirror) of the ckg-NEW-5 work. Stage 2 (broaden the same style
of keyword fixtures over the real go-stablenet corpus) is tracked separately.

## Layout

- `corpus/` — a **vendored copy** of CKV's `testdata/sample`, plus a minimal
  `go.mod` (CKV's tree-sitter parser indexes the Go files without a module; ckg's
  `go/packages`-based Go parser needs one, or it emits zero Go symbols).
- `fixtures/` — 12 `MCKV*` fixtures, each mirroring one CKV query. They span the
  CKV query *types* (exact-symbol paste, paraphrased intent, vague intent,
  error-message string, short keyword) across Go / TypeScript / Solidity /
  JavaScript, using `find_symbol` (exact) and `search_text` (keyword) probes.
- `.index/` — the regenerable ckg graph (git-ignored).

## Run

```
make eval-ckv-mirror
```

Builds `corpus/` into `.index/` and runs `ckg eval-retrieval`. Deterministic:
same corpus + fixtures → same result. Current baseline: **12/12 pass, R=P=F1=1.00**.

## Fixture ↔ CKV mapping

| ckg fixture | probe | CKV query | type |
|---|---|---|---|
| MCKV01 | find_symbol `sample.Server.Listen` | q11 | exact-symbol (Go) |
| MCKV02 | find_symbol `sample.Cache.Get` | q12 | exact-symbol (Go) |
| MCKV03 | find_symbol `Handler.dispatch` | q13 | exact-symbol (TS) |
| MCKV04 | find_symbol `Token.transfer` | q14 | exact-symbol (Sol) |
| MCKV05 | search_text `password` | q37 | intent→keyword (Go) |
| MCKV06 | search_text `evict` | q19 | vague→keyword (Go) |
| MCKV07 | search_text `positive` | q22 | error-message (Sol) |
| MCKV08 | search_text `validate` | q30 | intent→keyword (TS) |
| MCKV09 | search_text `transfer` | q24 | short keyword (Sol) |
| MCKV10 | search_text `balance` | q31 | intent→keyword (Sol) |
| MCKV11 | search_text `client` | q39 | intent→keyword (JS) |
| MCKV12 | find_symbol `ValidateEmail` | q35 | exact intent (Go) |
