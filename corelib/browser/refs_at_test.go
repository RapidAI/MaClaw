package browser

import "testing"

func TestResolveRefAcceptsBareName(t *testing.T) {
	s := &BrowserAgentSession{snapshots: map[string]*BrowserSnapshot{"s1": {SnapshotID: "s1", Refs: []BrowserElementRef{{Ref: "@e1", Name: "添加话题"}}}}, lastSnapshotID: "s1"}
	for _, r := range []string{"e1", "@e1", " E1 "} {
		if got, err := s.ResolveRef("", r); err != nil || got.Name != "添加话题" {
			t.Fatalf("%q: %v", r, err)
		}
	}
}
