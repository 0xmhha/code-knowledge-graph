import type { GraphNode, GraphEdge, Manifest, HierarchyRow, NodeId } from '@/types';

const asArray = <T,>(v: unknown): T[] => (Array.isArray(v) ? (v as T[]) : []);

export type TopMetric = 'pagerank' | 'usage';

// ImpactNode is one row in any impact bucket. Mirrors the shape returned
// by pkg/impact.Compute → nodeToImpactEntry on the backend (see
// internal/server/handleImpact and pkg/impact/impact.go). Optional fields
// reflect that some node kinds (Package, Endpoint) lack file scope.
export interface ImpactNode {
  id: NodeId;
  type?: string;
  name?: string;
  qname?: string;
  file?: string;
  line?: number;
  confidence?: string;
  signature?: string;
  usage_score?: number;
  citation?: string;
  source?: string;  // present only when include_blobs=1
}

// ImpactBuckets keys mirror pkg/impact bucket order so consumers can
// iterate in a stable shape. Always all six keys (possibly empty arrays).
export interface ImpactBuckets {
  callers: ImpactNode[];
  interface_impact: ImpactNode[];
  type_users: ImpactNode[];
  distributed: ImpactNode[];
  concurrent: ImpactNode[];
  other_refs: ImpactNode[];
}

export interface ImpactSeedSummary {
  id: NodeId;
  type?: string;
  name?: string;
  qname?: string;
  file_path?: string;
  start_line?: number;
  citation?: string;
}

export interface ImpactWarning {
  code?: string;
  node_id?: NodeId;
  qname?: string;
}

export interface ImpactResult {
  depth?: number;
  not_found?: boolean;
  seed?: ImpactSeedSummary;
  seeds?: ImpactSeedSummary[];
  seed_qname?: string;
  seed_file?: string;
  impact?: ImpactBuckets;
  edges?: Array<[NodeId, NodeId, string, number]>;
  totals?: { nodes: number; edges: number; by_group: Record<string, number> };
  metadata?: { warnings: ImpactWarning[] };
}

export interface IAPI {
  manifest(): Promise<Manifest>;
  hierarchy(kind?: string): Promise<HierarchyRow[]>;
  nodes(parentId?: string, limit?: number): Promise<GraphNode[]>;
  // topNodes returns top-N nodes ranked by metric, descending. Used by
  // the viewer boot path so the initial seed contains hub functions/
  // methods/types and 1-hop expansion shows real call/import structure
  // rather than 37 disconnected Package nodes.
  // excludeTypes filters out node kinds at the SQL layer — the viewer
  // passes ['Commit', 'Hunk'] so the boot seed isn't dominated by meta
  // nodes (Commit/Hunk are excluded from PageRank in score.Compute per
  // schema 1.8 §11.7, but the SQL filter is kept as a defensive hedge
  // for older graph.db files and future PageRank-rule shifts).
  // Returns [] on older backends that don't expose /api/nodes/top —
  // callers should fall back to nodes('') in that case.
  topNodes(metric: TopMetric, limit: number, excludeTypes?: string[]): Promise<GraphNode[]>;
  edges(nodeIds: NodeId[]): Promise<GraphEdge[]>;
  // edgeCounts returns total edge count per edge type across the entire
  // graph (no node filter). Powers the viewer's per-group axis weight
  // badges (G1..G6 next to each pill). Returns {} on backends that
  // don't expose /api/edges/counts (older serve) so callers can hide
  // the badges gracefully.
  edgeCounts(): Promise<Record<string, number>>;
  nodesByIds(ids: NodeId[]): Promise<GraphNode[]>;
  blob(nodeId: NodeId): Promise<string>;
  search(q: string): Promise<GraphNode[]>;
  // impact returns the reverse-dependency closure for a seed qname,
  // grouped by impact category (callers, interface_impact, type_users,
  // distributed, concurrent, other_refs). Backed by pkg/impact.Compute on
  // serve mode; static mode rejects with a clear error since the static
  // export bundle does not include the SQLite store needed for arbitrary
  // qname lookups.
  impact(seedQname: string, depth: number): Promise<ImpactResult>;
  // ambiguousNodes returns Hunk + Commit rows whose confidence is
  // AMBIGUOUS — the §11.3 unreachable-history track populated by
  // reflog/fsck. Powers the viewer's Recovery panel; deliberately
  // unfiltered at the HTTP layer so a human operator can browse
  // force-pushed-away commits when an agent overwrites code.
  // Returns [] on backends that don't expose /api/nodes/ambiguous
  // (pre-§11.3 builds) so callers can hide the panel gracefully.
  ambiguousNodes(): Promise<GraphNode[]>;
  // tickets returns the H4 issue-id index aggregated by the server's
  // evidence cache: rows of {issue_id, hunk_count, commit_count,
  // sample_commits}. Powers the viewer's TicketIndex panel; rows
  // are pre-sorted by hunk_count desc so the caller can render
  // top-N directly.
  tickets(limit?: number): Promise<TicketRow[]>;
  // evidence returns the full EvidencePack assembled by H3 — top-K
  // commits with patch text + modifies neighbours. The viewer's
  // ticket-row "patches" button calls this with {issueID} only,
  // which the server interprets as "show the ticket's full footprint
  // sorted by recency" (no BM25 ranking needed). Combine with intent
  // to BM25-rank inside the ticket subset.
  evidence(opts: EvidenceQuery): Promise<EvidencePack>;
}

export interface EvidenceQuery {
  intent?: string;
  issueID?: string;
  seedQname?: string;
  k?: number;
  budgetTokens?: number;
  // offset skips the first N commits in the recency-sorted result.
  // Used by the TicketIndex "Load more" button to walk back through
  // a large ticket without inflating budgetTokens (which would also
  // make every server-side response heavier).
  offset?: number;
}

export interface EvidenceModifies {
  qname: string;
  type: string;
  file_path?: string;
  start_line?: number;
  end_line?: number;
}

export interface EvidenceHunk {
  id: string;
  file_path: string;
  start_line: number;
  end_line: number;
  patch_text: string;
  modifies?: EvidenceModifies[];
}

export interface EvidenceHit {
  commit: { sha: string; subject: string; author_time: number; issue_ids?: string[] };
  hunks: EvidenceHunk[];
}

export interface EvidencePack {
  intent: string;
  hits: EvidenceHit[];
}

export interface TicketRow {
  issue_id: string;
  hunk_count: number;
  commit_count: number;
  sample_commits?: Array<{
    sha: string;
    subject: string;
    author_time: number;
    // top_files surfaces the top-3 most-touched directories of the
    // commit's hunks (e.g. ["crypto/secp256k1", "consensus"]). Lets
    // the viewer hint a ticket's reach before the user fetches the
    // full EvidencePack via the "patches" button. Optional because
    // older backends (pre-9-top-files commit) don't emit it; the UI
    // skips the pill row when the array is empty/undefined.
    top_files?: string[];
  }>;
}

export class API implements IAPI {
  constructor(private base: string = '') {}

  async manifest(): Promise<Manifest> {
    return fetch(`${this.base}/api/manifest`).then(r => r.json());
  }

  async hierarchy(kind: string = 'pkg'): Promise<HierarchyRow[]> {
    return fetch(`${this.base}/api/hierarchy?kind=${kind}`).then(r => r.json()).then(asArray<HierarchyRow>);
  }

  async nodes(parentId: string = '', limit: number = 5000): Promise<GraphNode[]> {
    const q = new URLSearchParams({ limit: String(limit) });
    if (parentId) q.set('parent', parentId);
    return fetch(`${this.base}/api/nodes?${q}`).then(r => r.json()).then(asArray<GraphNode>);
  }

  // topNodes hits the /api/nodes/top endpoint added in schema 1.6 wiring.
  // Returns [] ONLY on 404 so callers can transparently fall back to
  // nodes('') against older backends that don't expose this route. Any
  // other non-2xx (500, 502, …) is a real backend error and MUST surface
  // — silently mapping it to [] hid actual failures behind the "older
  // backend" fallback path and made debugging impossible.
  async topNodes(metric: TopMetric, limit: number, excludeTypes?: string[]): Promise<GraphNode[]> {
    const q = new URLSearchParams({ metric, limit: String(limit) });
    if (excludeTypes && excludeTypes.length > 0) {
      q.set('excludeTypes', excludeTypes.join(','));
    }
    const r = await fetch(`${this.base}/api/nodes/top?${q}`);
    if (r.status === 404) return [];
    if (!r.ok) throw new Error(`/api/nodes/top ${r.status}`);
    return asArray<GraphNode>(await r.json());
  }

  async edges(nodeIds: NodeId[]): Promise<GraphEdge[]> {
    return fetch(`${this.base}/api/edges`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ids: nodeIds }),
    }).then(r => r.json()).then(asArray<GraphEdge>);
  }

  async edgeCounts(): Promise<Record<string, number>> {
    const r = await fetch(`${this.base}/api/edges/counts`);
    if (r.status === 404) return {};
    if (!r.ok) throw new Error(`/api/edges/counts ${r.status}`);
    const v = await r.json();
    return (v && typeof v === 'object') ? v as Record<string, number> : {};
  }

  async nodesByIds(ids: NodeId[]): Promise<GraphNode[]> {
    if (!Array.isArray(ids) || ids.length === 0) return [];
    return fetch(`${this.base}/api/nodes-by-ids`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ids }),
    }).then(r => r.json()).then(asArray<GraphNode>);
  }

  async blob(nodeId: NodeId): Promise<string> {
    const r = await fetch(`${this.base}/api/blob/${nodeId}`);
    if (!r.ok) return '';
    return r.text();
  }

  async search(q: string): Promise<GraphNode[]> {
    return fetch(`${this.base}/api/search?q=${encodeURIComponent(q)}`)
      .then(r => r.json()).then(asArray<GraphNode>);
  }

  async impact(seedQname: string, depth: number): Promise<ImpactResult> {
    const q = new URLSearchParams({ seed_qname: seedQname, depth: String(depth) });
    const r = await fetch(`${this.base}/api/impact?${q}`);
    if (!r.ok) throw new Error(`/api/impact ${r.status}`);
    return await r.json() as ImpactResult;
  }

  async ambiguousNodes(): Promise<GraphNode[]> {
    const r = await fetch(`${this.base}/api/nodes/ambiguous`);
    if (r.status === 404) return [];
    if (!r.ok) throw new Error(`/api/nodes/ambiguous ${r.status}`);
    return asArray<GraphNode>(await r.json());
  }

  async tickets(limit: number = 100): Promise<TicketRow[]> {
    const r = await fetch(`${this.base}/api/tickets?limit=${limit}`);
    if (r.status === 404) return [];
    if (!r.ok) throw new Error(`/api/tickets ${r.status}`);
    return asArray<TicketRow>(await r.json());
  }

  async evidence(opts: EvidenceQuery): Promise<EvidencePack> {
    const q = new URLSearchParams();
    if (opts.intent) q.set('intent', opts.intent);
    if (opts.issueID) q.set('issue_id', opts.issueID);
    if (opts.seedQname) q.set('seed_qname', opts.seedQname);
    if (opts.k != null) q.set('k', String(opts.k));
    if (opts.budgetTokens != null) q.set('budget_tokens', String(opts.budgetTokens));
    if (opts.offset != null) q.set('offset', String(opts.offset));
    const r = await fetch(`${this.base}/api/evidence?${q}`);
    if (!r.ok) throw new Error(`/api/evidence ${r.status}`);
    const v = await r.json();
    return { intent: v?.intent ?? '', hits: asArray<EvidenceHit>(v?.hits) };
  }
}

export class StaticAPI implements IAPI {
  private nodesCache: GraphNode[] | null = null;
  private edgesCache: GraphEdge[] | null = null;
  private pkgTreeCache: HierarchyRow[] | null = null;

  async manifest(): Promise<Manifest> {
    return fetch('./manifest.json', { cache: 'no-store' }).then(r => r.json());
  }

  async hierarchy(kind: string = 'pkg'): Promise<HierarchyRow[]> {
    const file = kind === 'topic' ? 'topic_tree.json' : 'pkg_tree.json';
    return fetch(`./hierarchy/${file}`).then(r => r.json()).then(v => (v as HierarchyRow[]) || []);
  }

  private async allNodes(): Promise<GraphNode[]> {
    if (!this.nodesCache) this.nodesCache = await concatChunks<GraphNode>('nodes');
    return this.nodesCache;
  }

  private async allEdges(): Promise<GraphEdge[]> {
    if (!this.edgesCache) this.edgesCache = await concatChunks<GraphEdge>('edges');
    return this.edgesCache;
  }

  private async pkgTree(): Promise<HierarchyRow[]> {
    if (!this.pkgTreeCache) this.pkgTreeCache = await this.hierarchy('pkg');
    return this.pkgTreeCache;
  }

  async nodes(parentId: string = '', limit: number = 5000): Promise<GraphNode[]> {
    const all = await this.allNodes();
    if (!parentId) {
      return all.filter(n => n.type === 'Package').slice(0, limit);
    }
    const tree = await this.pkgTree();
    const childIds = new Set(tree.filter(r => r.parent_id === parentId).map(r => r.child_id));
    return all.filter(n => childIds.has(n.id)).slice(0, limit);
  }

  // topNodes sorts the static-export node set client-side. Mirrors the
  // backend ORDER BY <metric> DESC, id ASC for cross-mode parity, and
  // applies the same excludeTypes filter at the JS layer so static and
  // serve modes stay symmetrical.
  async topNodes(metric: TopMetric, limit: number, excludeTypes?: string[]): Promise<GraphNode[]> {
    const all = await this.allNodes();
    const key = metric === 'usage' ? 'usage_score' : 'pagerank';
    const score = (n: GraphNode) => {
      const v = (n as unknown as Record<string, unknown>)[key];
      return typeof v === 'number' ? v : 0;
    };
    let filtered: GraphNode[] = all;
    if (excludeTypes && excludeTypes.length > 0) {
      const exclSet = new Set<string>(excludeTypes);
      // GraphNode.type is optional in the static export shape; nodes
      // missing a type are kept (they can't match any exclude entry).
      filtered = all.filter(n => n.type === undefined || !exclSet.has(n.type));
    }
    return [...filtered].sort((a, b) => {
      const d = score(b) - score(a);
      if (d !== 0) return d;
      return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
    }).slice(0, limit);
  }

  async edges(nodeIds: NodeId[]): Promise<GraphEdge[]> {
    const ids = new Set(nodeIds);
    const all = await this.allEdges();
    return all.filter(e => ids.has(e.src) || ids.has(e.dst));
  }

  async edgeCounts(): Promise<Record<string, number>> {
    const all = await this.allEdges();
    const out: Record<string, number> = {};
    for (const e of all) out[e.type] = (out[e.type] ?? 0) + 1;
    return out;
  }

  async nodesByIds(ids: NodeId[]): Promise<GraphNode[]> {
    if (!ids.length) return [];
    const all = await this.allNodes();
    const want = new Set(ids);
    return all.filter(n => want.has(n.id));
  }

  async blob(nodeId: NodeId): Promise<string> {
    return fetch(`./blobs/${nodeId}.txt`).then(r => r.ok ? r.text() : '');
  }

  async search(_q: string): Promise<GraphNode[]> { return []; }

  async impact(_seedQname: string, _depth: number): Promise<ImpactResult> {
    // Static export mode ships pre-computed JSON chunks but no SQLite
    // store, so impact_of_change (which needs FindSymbol +
    // NeighborhoodByQname) cannot run client-side. Fail fast so
    // NodeDetail can surface a clear "use ckg serve" message instead of
    // silently returning empty buckets.
    throw new Error('impact_of_change unavailable in static export mode (use `ckg serve`)');
  }

  async ambiguousNodes(): Promise<GraphNode[]> {
    // Filter the cached node set client-side — there's no separate
    // ambiguous chunk in the static export bundle. Same shape as the
    // serve API so the Recovery panel doesn't need a mode-aware code
    // path. Returns [] on graphs with no AMBIGUOUS rows.
    const all = await this.allNodes();
    return all.filter(n =>
      (n.type === 'Hunk' || n.type === 'Commit') && n.confidence === 'AMBIGUOUS');
  }

  async tickets(_limit: number = 100): Promise<TicketRow[]> {
    // Static-export mode does not ship an aggregated ticket index;
    // the viewer hides the TicketIndex panel when the array is
    // empty so this graceful-degrade keeps the static viewer simple.
    return [];
  }

  async evidence(_opts: EvidenceQuery): Promise<EvidencePack> {
    // EvidencePack assembly needs the BM25 corpus + per-blob lookups
    // that only `ckg serve` can provide. Static-export mode returns an
    // empty pack so the TicketIndex "patches" button renders a clear
    // "use ckg serve" message instead of crashing the panel.
    return { intent: '', hits: [] };
  }
}

async function concatChunks<T>(prefix: string): Promise<T[]> {
  const out: T[] = [];
  for (let i = 0; ; i++) {
    const path = `./${prefix}/chunk_${String(i).padStart(4, '0')}.json`;
    let r: Response;
    try { r = await fetch(path); } catch { break; }
    if (!r.ok) break;
    const arr = await r.json();
    if (Array.isArray(arr)) out.push(...arr as T[]);
  }
  return out;
}

export async function detectMode(): Promise<'static' | 'serve'> {
  try {
    const r = await fetch('./manifest.json', { cache: 'no-store' });
    if (r.ok) return 'static';
  } catch { /* fall through */ }
  return 'serve';
}
