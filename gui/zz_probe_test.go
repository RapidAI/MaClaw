package main

import (
	"testing"
)

func TestZZProbeInitialFace(t *testing.T) {
	cb, _ := weatherPDFDesktopBeforeSearch(t)
	s := cb.semanticSurface
	t.Logf("coordinator=%v", s.coordinator != nil)
	for name, grant := range s.grants {
		t.Logf("grant name=%s adapter=%s selection=%s", name, grant.AdapterName, grant.SelectionID)
	}
	defs := cb.BuildToolsForModelRequest("南京天气，生成pdf报告", 0)
	for _, d := range defs {
		t.Logf("def=%s", extractToolName(d))
	}
	t.Logf("total defs=%d", len(defs))
	for _, sel := range s.plan.Selections {
		t.Logf("selection=%s cap=%s requires=%v", sel.ID, sel.FitProof.MatchedCapability, sel.Requires)
	}
}
