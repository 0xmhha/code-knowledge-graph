package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W8 V1 — HasLowLevelCall and HasValueTransfer presence markers
// on Solidity callables.
//
// Complement to W7.1 V0 EdgeInvokes which only fires when the
// receiver is a state-var / parameter typed as Contract or Interface.
// Address-typed receivers (the common proxy pattern), address(x)
// cast wrappers, and chained receivers all produce no edge in V0
// but the markers light up so security audits can ask
// "which functions perform any low-level call shape".

func TestLowLevelCallMarker_Flags(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/low_level_call_marker", "markers.sol")

	want := map[string]struct {
		lowLevel bool
		value    bool
	}{
		"Markers.callBare":         {lowLevel: true, value: false},
		"Markers.callCast":         {lowLevel: true, value: false},
		"Markers.transferSend":     {lowLevel: false, value: true},
		"Markers.transferTransfer": {lowLevel: false, value: true},
		"Markers.plain":            {lowLevel: false, value: false},
	}

	got := map[string]struct {
		lowLevel bool
		value    bool
	}{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		seen[n.QualifiedName] = true
		got[n.QualifiedName] = struct {
			lowLevel bool
			value    bool
		}{lowLevel: n.HasLowLevelCall, value: n.HasValueTransfer}
	}

	for qn, w := range want {
		if !seen[qn] {
			t.Errorf("W8 V1 missing function %q", qn)
			continue
		}
		g := got[qn]
		if g.lowLevel != w.lowLevel {
			t.Errorf("W8 V1 %q HasLowLevelCall: got %v, want %v", qn, g.lowLevel, w.lowLevel)
		}
		if g.value != w.value {
			t.Errorf("W8 V1 %q HasValueTransfer: got %v, want %v", qn, g.value, w.value)
		}
	}
}
