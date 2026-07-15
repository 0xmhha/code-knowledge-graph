# eval/stablenet-keyword — keyword retrieval over the real go-stablenet corpus (Stage 2)

Stage 2 of ckg-NEW-5. Where `eval/ckv-mirror` mirrors CKV's fixtures over a tiny
vendored sample, this measures ckg **keyword retrieval on the real go-stablenet
corpus** (~183k nodes) with the same LLM-free `eval-retrieval` harness.

## Why it is env-gated (not self-contained)

The go-stablenet corpus is external and large, so it cannot be vendored into this
repo (unlike the mirror). The fixtures here are committed; **running them needs a
pre-built go-stablenet graph**. Expected qnames are **pinned to commit
`0bf2f4d1b`** built with the tests-included filter
(`eval/stablenet/stablenet-files-with-tests.json`, or `scripts/index-project.sh`)
— a different commit/filter can shift qnames and the fixtures will (correctly)
flag the drift.

## Run

```
make eval-stablenet-keyword GRAPH=/path/to/knowledge-data/pr-77-2
```

`GRAPH` is a graph dir containing `graph.db`. Current baseline against the pinned
canonical graph: **8/8 pass, R=P=F1=1.00**.

## Fixtures (8)

| id | probe | intent | note |
|---|---|---|---|
| SNK01 | find_symbol `validator.stickyProposer` (exact) | sticky proposer policy | exact-symbol |
| SNK02 | find_symbol `backend.API.GetValidators` (exact) | WBFT validator-set accessor | exact-symbol |
| SNK03 | find_symbol `GetMaxMinterAllowance` (suffix) | gov max-allowance getter | suffix name |
| SNK04 | search_text `proposer sticky` (AND) | sticky-proposer cluster | AND, 3 hits |
| SNK05 | search_text `future preprepare` (AND) | future-view timer pair | AND, 2 hits |
| SNK06 | search_text `mint allowance` (AND) | gov minter allowance cluster | AND, 6 hits |
| SNK07 | search_text `quorum deduplication` (AND) | quorum-after-dedup test | AND precision, 1 hit on 183k nodes |
| SNK08 | search_text `effective gasprice` (AND) | every EffectiveGasPrice bearer | AND, coherent broad cluster (12) |

The AND-mode probes exercise the `search_text mode="and"` precision surface
(landed with the AND/OR work) on a real corpus — a discriminative token pair
narrows 183k nodes to a small, exact cluster.
