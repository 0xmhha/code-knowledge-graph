package cluster

// PersistClusterEdge mirrors persist.ClusterEdge so the buildpipe orchestrator
// can hand pkg-tree edges to persist.Store without forcing cluster to import
// persist (which would, via persist→cluster for TopicTree types, create an
// import cycle). Both types have identical fields.
type PersistClusterEdge struct {
	ParentID, ChildID string
	Level             int
}

// PersistEdges converts pkg_tree edges to the persist-friendly slice expected
// by persist.Store.InsertPkgTreeFromCluster.
func (t *PkgTree) PersistEdges() []PersistClusterEdge {
	out := make([]PersistClusterEdge, len(t.Edges))
	for i, e := range t.Edges {
		out[i] = PersistClusterEdge{ParentID: e.ParentID, ChildID: e.ChildID, Level: e.Level}
	}
	return out
}

// ResolutionsCount/ResolutionGamma/ResolutionMembers satisfy the
// persist.TopicTreeInput interface (declared in persist) so persist can
// consume a *TopicTree without importing cluster types directly at the
// interface boundary.
func (t *TopicTree) ResolutionsCount() int         { return len(t.Resolutions) }
func (t *TopicTree) ResolutionGamma(i int) float64 { return t.Resolutions[i].Gamma }
func (t *TopicTree) ResolutionMembers(i int) map[string][]string {
	out := map[string][]string{}
	for _, c := range t.Resolutions[i].Communities {
		out[c.Label] = c.Members
	}
	return out
}
