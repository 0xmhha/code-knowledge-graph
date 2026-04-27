package server

import (
	"hash/fnv"
	"sync"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// defaultTopicResolution chooses which Leiden resolution to surface to
// the viewer. The build pipeline persists multiple resolutions
// (gammas 0.5/1.0/2.0 in V0); index 1 is the mid one — coarse enough
// to read at a glance, fine enough to distinguish neighbouring concerns.
// Configurable later (e.g. via /api/manifest).
const defaultTopicResolution = 1

// communityInfo carries what the viewer needs to colour & label a node.
// ID is a stable hash of Label, so the colour stays the same across
// server restarts without us needing to assign integer IDs at insert time.
type communityInfo struct {
	ID    int
	Label string
}

// communityCache lazily loads the topic_tree and exposes nodeID → community.
// Built once per Server lifetime; topic_tree only changes when the graph is
// rebuilt, and a fresh `ckg serve` reloads it anyway.
type communityCache struct {
	once sync.Once
	m    map[string]communityInfo
}

func (c *communityCache) lookup(nodeID string) (communityInfo, bool) {
	if c.m == nil {
		return communityInfo{}, false
	}
	info, ok := c.m[nodeID]
	return info, ok
}

// hashLabel turns a topic label into the int the viewer's communityColor()
// hashes against the 137.5° golden angle. FNV-32a is plenty for visual
// distinctness — collisions only matter if two communities accidentally
// share a hue, which is acceptable degradation.
func hashLabel(s string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32())
}

// apiNode is the wire envelope for /api/nodes responses. It embeds the
// canonical types.Node and adds the two community fields the viewer reads.
// Keeping them on the envelope (not types.Node itself) means the validation
// layer and the persist layer stay untouched — community is a *projection*
// for the viewer, not a property of the node.
type apiNode struct {
	types.Node
	CommunityID *int   `json:"community_id,omitempty"`
	TopicLabel  string `json:"topic_label,omitempty"`
}

// decorateNodes returns each node wrapped with its community info if any.
// Nodes outside the topic_tree (or before the cache is populated) are
// returned with the optional fields omitted, which omitempty drops from
// the JSON entirely — frontend treats absence as "no community known".
func (s *Server) decorateNodes(nodes []types.Node) []apiNode {
	s.ensureCommunityCache()
	out := make([]apiNode, len(nodes))
	for i, n := range nodes {
		out[i] = apiNode{Node: n}
		if info, ok := s.community.lookup(n.ID); ok {
			id := info.ID
			out[i].CommunityID = &id
			out[i].TopicLabel = info.Label
		}
	}
	return out
}

// ensureCommunityCache fires the one-shot topic_tree load on the first
// request that needs it. Called from decorateNodes so the cost is paid
// lazily rather than at New(), keeping startup cheap when the viewer is
// unused (e.g. when the binary serves only /api/* to other tools).
//
// Errors are logged and swallowed: the cache stays empty, decorateNodes
// emits no community fields, and the viewer transparently falls back to
// language colouring. This matches pre-M2 behaviour.
func (s *Server) ensureCommunityCache() {
	s.community.once.Do(func() {
		rows, err := s.store.LoadHierarchy("topic")
		if err != nil {
			s.log.Warn("load topic hierarchy", "err", err)
			s.community.m = map[string]communityInfo{}
			return
		}
		m := make(map[string]communityInfo, len(rows))
		for _, r := range rows {
			if r.Level != defaultTopicResolution || r.TopicLabel == "" {
				continue
			}
			m[r.ChildID] = communityInfo{ID: hashLabel(r.TopicLabel), Label: r.TopicLabel}
		}
		s.community.m = m
	})
}
