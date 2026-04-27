export type NodeId = string;

export type Confidence = 'EXTRACTED' | 'INFERRED' | 'AMBIGUOUS';

export interface GraphNode {
  id: NodeId;
  type?: string;
  name?: string;
  qualified_name?: string;
  file_path?: string;
  start_line?: number;
  language?: string;
  confidence?: Confidence;
  signature?: string;
  in_degree?: number;
  out_degree?: number;
  usage_score?: number;
  pagerank?: number;
  community_id?: number;
  topic_label?: string;
  // Mutated by force-graph at runtime; safe to ignore in our code paths.
  x?: number;
  y?: number;
}

export interface GraphEdge {
  src: NodeId;
  dst: NodeId;
  type: string;
}

export interface Manifest {
  src_root?: string;
  src_commit?: string;
  current_commit?: string;
  graph_stale?: boolean;
}

export interface HierarchyRow {
  parent_id: string;
  child_id: string;
  resolution: number;
  topic_label?: string;
}

export type CommitReason = 'navigate' | 'trace' | 'search-pick' | 'filter' | 'boot';

export interface CommitGraph {
  visibleIds: Set<NodeId>;
  focusDistance: Map<NodeId, number>;
  reason: CommitReason;
}

export type ViewMode = '2d' | '3d';
export type ColorMode = 'lang' | 'community';
export type TraceDirection = 'callers' | 'callees' | 'both';
