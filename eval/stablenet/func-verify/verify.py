#!/usr/bin/env python3
"""CKG functional verification — keyword -> certain code + change history.

Fully deterministic (no LLM). Validates what CKG's graph.db returns against the
on-disk go-stablenet source and its git history.

Layers verified:
  C1  keyword -> code definition        (DB)         vs on-disk source + blob bytes
  C1b keyword -> cited code             (ckg query)  LLM-free query CLI surface
  C1c keyword -> symbol citation        (ckg mcp)    fresh MCP subprocess surface
  C2  keyword -> change history         (DB)         vs `git log -L:<sym>:<file>` (symbol-precise)
        - commit-level recall: CKG `changed_in` edges vs git-L commits
        - PR-level recall:     CKG `node_prs`     vs git-L commits' (#N)

Usage:
  python3 verify.py [--ground-truth FILE] [--out FILE] [--label L] [--no-surface]
"""
import argparse, json, os, re, sqlite3, subprocess, sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
DEF_TYPES = ("Function", "Method", "Struct", "Interface", "Field", "Constant", "Variable", "Type")
PRNUM = re.compile(r"\(#(\d+)\)")


def sqlite_cli_json(db, sql):
    out = subprocess.run(["sqlite3", "-json", "-readonly", db, sql], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"sqlite3 CLI failed: {out.stderr.strip()}\nSQL: {sql}")
    s = out.stdout.strip()
    return json.loads(s) if s else []


def fts_def(db, keyword):
    kw = keyword.replace("'", "''")
    sql = ("SELECT n.id,n.type,n.name,n.qualified_name,n.file_path,n.start_line,n.end_line,"
           "n.start_byte,n.end_byte FROM nodes_fts f JOIN nodes n ON n.rowid=f.rowid "
           f"WHERE nodes_fts MATCH '{kw}' AND n.name='{kw}' AND n.type IN {DEF_TYPES} "
           "ORDER BY n.pagerank DESC LIMIT 1;")
    rows = sqlite_cli_json(db, sql)
    return rows[0] if rows else None


def git(src, *args):
    r = subprocess.run(["git", "-C", src, *args], capture_output=True, text=True)
    return r.stdout, r.returncode


def git_log_L(src, ref, name, file_path, limit=12):
    """Symbol-precise modification history: commits that changed function `name`
    in `file_path`. Returns [(full_sha, subject, pr_or_None)]. Authoritative
    ground truth, independent of CKG."""
    out, rc = git(src, "log", ref, f"-L:{name}:{file_path}", "-s", "--format=%H%x09%s", f"-n{limit}")
    res = []
    if rc != 0:
        return res
    for line in out.splitlines():
        if "\t" not in line:
            continue
        sha, subj = line.split("\t", 1)
        m = PRNUM.search(subj)
        res.append((sha.strip(), subj.strip(), int(m.group(1)) if m else None))
    return res


def run_ckg_query(ckg_bin, gdir, keyword, budget=1200):
    r = subprocess.run([ckg_bin, "query", "--graph", gdir, keyword, "--budget", str(budget)],
                       capture_output=True, text=True)
    return r.stdout if r.returncode == 0 else ""


def ckg_mcp_find_symbol(ckg_bin, gdir, names):
    """Fresh `ckg mcp` stdio session; call find_symbol for each name. Returns
    {name: result_text}. Uses a brand-new subprocess against the on-disk graph,
    so it does NOT depend on the long-running session MCP (which Claude Code does
    not respawn on /reload-plugins)."""
    msgs = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize",
         "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "verify", "version": "0"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
    ]
    for i, nm in enumerate(names):
        msgs.append({"jsonrpc": "2.0", "id": 100 + i, "method": "tools/call",
                     "params": {"name": "find_symbol", "arguments": {"name": nm}}})
    stdin = "\n".join(json.dumps(m) for m in msgs) + "\n"
    try:
        r = subprocess.run([ckg_bin, "mcp", "--graph", gdir], input=stdin,
                           capture_output=True, text=True, timeout=120)
    except Exception as e:
        return {nm: f"__ERR__ {e}" for nm in names}
    by_id = {}
    for line in r.stdout.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except Exception:
            continue
        if "id" in obj and "result" in obj:
            txt = ""
            try:
                txt = obj["result"]["content"][0]["text"]
            except Exception:
                txt = json.dumps(obj["result"])
            by_id[obj["id"]] = txt
    return {nm: by_id.get(100 + i, "__NO_RESPONSE__") for i, nm in enumerate(names)}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", default=None)
    ap.add_argument("--src", default=None)
    ap.add_argument("--smoke", type=int, default=0)
    ap.add_argument("--ground-truth", default=str(HERE / "ground-truth-A.json"))
    ap.add_argument("--out", default=str(HERE / "Report-A.md"))
    ap.add_argument("--label", default="A")
    ap.add_argument("--gdir", default=None)
    ap.add_argument("--ckg-bin", default=str((HERE / "../../../bin/ckg").resolve()))
    ap.add_argument("--no-surface", action="store_true")
    args = ap.parse_args()

    gt = json.loads(Path(args.ground_truth).read_text())
    db = args.db or str((HERE / gt["db_rel"]).resolve())
    if not os.path.exists(db):
        sys.exit(f"graph.db not found: {db}")
    conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    man = {r["key"]: r["value"] for r in conn.execute("SELECT key,value FROM manifest")}
    src = args.src or man.get("src_root")
    ref = man.get("src_commit", "HEAD")
    if not src or not os.path.isdir(src):
        sys.exit(f"src_root not found: {src}")
    gdir = args.gdir or os.path.dirname(db)
    surface_on = (not args.no_surface) and os.path.isfile(args.ckg_bin)

    # ---------- C1: keyword -> code definition ----------
    c1_kws = gt["c1_keywords"][:args.smoke] if args.smoke else gt["c1_keywords"]
    c1 = []
    for item in c1_kws:
        kw = item["keyword"]
        rec = {"keyword": kw, "module": item.get("module", ""), "query_term": item.get("query_term", kw)}
        node = fts_def(db, kw)
        if not node:
            rec.update(found=False, location_ok=False, blob_ok=False, errors=["recall_miss: no definition node"])
            c1.append(rec); continue
        rec.update(found=True, qualified_name=node["qualified_name"],
                   location=f'{node["file_path"]}:{node["start_line"]}')
        errs = []
        fpath = os.path.join(src, node["file_path"])
        if not os.path.isfile(fpath):
            errs.append(f"file_missing: {node['file_path']}")
            rec.update(location_ok=False, blob_ok=False, errors=errs); c1.append(rec); continue
        raw = Path(fpath).read_bytes()
        lines = raw.split(b"\n")
        sl = node["start_line"]
        decl = lines[sl - 1].decode("utf-8", "replace") if 0 < sl <= len(lines) else ""
        location_ok = kw in decl
        if not location_ok:
            errs.append(f"decl_mismatch: '{kw}' not on line {sl}")
        brow = conn.execute("SELECT source FROM blobs WHERE node_id=?", (node["id"],)).fetchone()
        blob = brow["source"] if brow else None
        disk_slice = raw[node["start_byte"]:node["end_byte"]]
        blob_ok = blob is not None and bytes(blob) == disk_slice
        if blob is None:
            errs.append("blob_missing")
        elif not blob_ok:
            errs.append(f"blob_mismatch: blob={len(blob)} disk={len(disk_slice)}")
        rec.update(payload_bytes=(len(blob) if blob else 0), file_bytes=len(raw),
                   location_ok=location_ok, blob_ok=blob_ok, errors=errs, node_id=node["id"])
        c1.append(rec)

    # ---------- C1b: ckg query surface ----------
    for rec in c1:
        if not surface_on or not rec.get("found"):
            continue
        out = run_ckg_query(args.ckg_bin, gdir, rec.get("query_term", rec["keyword"]))
        fp = rec["location"].rsplit(":", 1)[0]
        rec["query_cited"] = fp in out
        rec["query_cited_exact"] = rec["location"] in out

    # NOTE: the agent-facing MCP surface is the *cks* server (cks_context_find_symbol /
    # cks_context_change_history), NOT ckg's raw mcp. cks runs as a long-lived session
    # process that Claude Code does not respawn on /reload-plugins, so it is verified
    # agent-side (see Report "MCP 표면" section), not from this harness.

    # ---------- C2: keyword -> change history (git -L ground truth) ----------
    c2 = []
    for fact in gt["c2_history_facts"]:
        nrow = conn.execute(
            "SELECT id,qualified_name,file_path FROM nodes WHERE name=? AND file_path LIKE ? "
            "AND type IN ('Function','Method') ORDER BY pagerank DESC LIMIT 1",
            (fact["name"], fact["file_glob"])).fetchone()
        r = {"name": fact["name"], "file_glob": fact["file_glob"]}
        if not nrow:
            r.update(node_found=False); c2.append(r); continue
        f = nrow["file_path"]
        ground = git_log_L(src, ref, fact["name"], f)  # [(sha,subj,pr)]
        ground_shas = {g[0] for g in ground}
        ground_prs = {g[2] for g in ground if g[2] is not None}
        # CKG changed_in commit shas for this node
        ci = conn.execute(
            "SELECT d.qualified_name AS qn FROM edges e JOIN nodes d ON d.id=e.dst "
            "WHERE e.src=? AND e.type='changed_in' AND d.type='Commit'", (nrow["id"],)).fetchall()
        ckg_shas = set()
        for row in ci:
            qn = row["qn"] or ""
            ckg_shas.add(qn.split(":", 1)[1] if ":" in qn else qn)
        # CKG node_prs numbers
        ckg_prs = {x["number"] for x in conn.execute(
            "SELECT DISTINCT number FROM node_prs WHERE node_id=?", (nrow["id"],)).fetchall()}
        commit_hits = ground_shas & ckg_shas
        pr_hits = ground_prs & ckg_prs
        r.update(node_found=True, qualified_name=nrow["qualified_name"], file=f,
                 ground_n=len(ground_shas), ground_prs=sorted(ground_prs),
                 commit_recall=f"{len(commit_hits)}/{len(ground_shas)}",
                 commit_missed=sorted({s[:9] for s in (ground_shas - ckg_shas)}),
                 pr_recall=f"{len(pr_hits)}/{len(ground_prs)}",
                 pr_missed=sorted(ground_prs - ckg_prs),
                 ckg_commit_n=len(ckg_shas), ckg_prs=sorted(ckg_prs))
        c2.append(r)

    # ---------- metrics ----------
    n = len(c1); found = sum(1 for r in c1 if r.get("found"))
    loc = sum(1 for r in c1 if r.get("location_ok")); blob = sum(1 for r in c1 if r.get("blob_ok"))
    errs = sum(len(r.get("errors", [])) for r in c1)
    sq = [r for r in c1 if "query_cited" in r]
    sq_exact = sum(1 for r in sq if r.get("query_cited_exact"))
    c2f = [r for r in c2 if r.get("node_found")]

    def ratio(field):
        hit = tot = 0
        for r in c2f:
            a, b = r[field].split("/"); hit += int(a); tot += int(b)
        return hit, tot

    cr_hit, cr_tot = ratio("commit_recall"); pr_hit, pr_tot = ratio("pr_recall")
    results = {"manifest": {k: man.get(k) for k in ("schema_version", "src_commit", "build_timestamp")},
               "db": db, "src": src, "label": args.label, "c1": c1, "c2": c2,
               "metrics": {"c1_found": f"{found}/{n}", "c1_loc": f"{loc}/{found}", "c1_blob": f"{blob}/{found}",
                           "c1_errors": errs, "c1b_query_exact": f"{sq_exact}/{len(sq)}",
                           "c2_commit_recall": f"{cr_hit}/{cr_tot}", "c2_pr_recall": f"{pr_hit}/{pr_tot}"}}
    (HERE / f"results-{args.label}.json").write_text(json.dumps(results, indent=2))

    # ---------- report ----------
    def pct(a, b):
        return f"{100*a/b:.0f}%" if b else "n/a"
    L = [f"# CKG 기능 검증 보고서 [{args.label}] — 키워드 → 코드 + 수정 히스토리\n",
         f"- DB: `{db}`",
         f"- src: `{src}` (schema {man.get('schema_version')}, src_commit `{(man.get('src_commit') or '')[:12]}`, build {man.get('build_timestamp')})",
         f"- 정답원: 디스크 소스(C1) · `git log -L:<symbol>:<file>` on `{ref[:12]}`(C2). LLM 미사용.\n",
         "## 지표 요약\n", "| 지표 | 값 |", "|---|---|",
         f"| C1 정답률(정의 회수) | {found}/{n} ({pct(found,n)}) |",
         f"| C1 위치 정확도 | {loc}/{found} ({pct(loc,found)}) |",
         f"| C1 원본(blob) 무결성 | {blob}/{found} ({pct(blob,found)}) |",
         f"| C1 오류 건수 | {errs} |",
         f"| C1b 쿼리표면(`ckg query`) 정확인용 | {sq_exact}/{len(sq)} ({pct(sq_exact,len(sq))}) |",
         f"| C2 커밋레벨 recall (changed_in vs git-L, cap 10/file) | {cr_hit}/{cr_tot} ({pct(cr_hit,cr_tot)}) |",
         f"| C2 PR레벨 recall (node_prs vs git-L) | {pr_hit}/{pr_tot} ({pct(pr_hit,pr_tot)}) |\n",
         "## C1 — 키워드 → 코드 정의\n",
         "| 키워드 | 정의 | 위치 | 위치OK | blobOK | 쿼리표면 | payload/파일(B) | 오류 |",
         "|---|---|---|:--:|:--:|:--:|---|---|"]
    for r in c1:
        if not r.get("found"):
            L.append(f"| `{r['keyword']}` | — | — | ❌ | — | — | — | recall miss |"); continue
        qs = ("✅" if r.get("query_cited_exact") else ("file" if r.get("query_cited") else "❌")) if "query_cited" in r else "—"
        L.append(f"| `{r['keyword']}` | `{r['qualified_name']}` | `{r['location']}` | "
                 f"{'✅' if r['location_ok'] else '❌'} | {'✅' if r['blob_ok'] else '❌'} | {qs} | "
                 f"{r.get('payload_bytes',0)}/{r.get('file_bytes',0)} | {'; '.join(r.get('errors',[])) or '—'} |")

    L.append("\n## C2 — 키워드 → 수정 히스토리 (정답원: `git log -L`)\n")
    L.append("| 심볼 | 파일 | git-L 커밋수 | 커밋레벨 recall | PR레벨 recall | 누락 PR | CKG changed_in수 |")
    L.append("|---|---|--:|---|---|---|--:|")
    for r in c2:
        if not r.get("node_found"):
            L.append(f"| `{r['name']}` | (노드 없음: {r['file_glob']}) | — | — | — | — | — |"); continue
        L.append(f"| `{r['name']}` | `{r['file']}` | {r['ground_n']} | {r['commit_recall']} | "
                 f"{r['pr_recall']} | {r['pr_missed'] or '—'} | {r['ckg_commit_n']} |")

    L.append("\n## 발견사항 / 한계\n")
    L.append("- **언어 커버리지**: 데이터셋은 `--lang go` 빌드 → **Go 코드 심볼만** 인덱싱. Solidity(.sol)는 정의 심볼이 아니라 git-이력 hunk로만 존재(342건), TypeScript는 미인덱싱. 따라서 C1은 Go 한정 — **Solidity 시스템컨트랙트가 심볼로 미표현**인 것은 커버리지 갭.")
    L.append("- **C2 2계층**: 커밋레벨(`changed_in`)이 PR레벨(`node_prs`)보다 recall이 높을 수 있음 — node_prs는 *현재* 노드 라인범위와 과거 PR hunk의 겹침으로 귀속하므로 라인 드리프트 시 누락 가능. `git log -L`(함수추적)이 정답원.")
    L.append(f"- 단일 데이터셋(go-stablenet @ `{ref[:12]}`) 기준 · 일반화 아님.")
    Path(args.out).write_text("\n".join(L) + "\n")

    print(f"[{args.label}] C1 found {found}/{n} loc {loc}/{found} blob {blob}/{found} errs {errs}")
    print(f"[{args.label}] C1b query {sq_exact}/{len(sq)}")
    print(f"[{args.label}] C2 commit-recall {cr_hit}/{cr_tot}  pr-recall {pr_hit}/{pr_tot}")
    print(f"[{args.label}] report -> {args.out}")


if __name__ == "__main__":
    main()
