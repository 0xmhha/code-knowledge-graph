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
  // topNodes returns top-N nodes ranked by metric, regardless of type.
  // Used by the viewer boot path so the initial seed contains hub
  // functions/methods/types and 1-hop expansion shows real call/import
  // structure rather than 37 disconnected Package nodes (which is what
  // /api/nodes returns when parent="" — see backend QueryNodes).
  // Returns [] on older backends that don't expose /api/nodes/top — callers
  // should fall back to nodes('') in that case.
  topNodes(metric: TopMetric, limit: number): Promise<GraphNode[]>;
  edges(nodeIds: NodeId[]): Promise<GraphEdge[]>;
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
  async topNodes(metric: TopMetric, limit: number): Promise<GraphNode[]> {
    const q = new URLSearchParams({ metric, limit: String(limit) });
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
  // backend ORDER BY <metric> DESC, id ASC for cross-mode parity.
  async topNodes(metric: TopMetric, limit: number): Promise<GraphNode[]> {
    const all = await this.allNodes();
    const key = metric === 'usage' ? 'usage_score' : 'pagerank';
    const score = (n: GraphNode) => {
      const v = (n as unknown as Record<string, unknown>)[key];
      return typeof v === 'number' ? v : 0;
    };
    return [...all].sort((a, b) => {
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
