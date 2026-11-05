package guiapp

import (
	"strings"
	"testing"
)

func TestStartAgentGroupRunAggregates(t *testing.T) {
	a := &App{}
	agentGroupMu.Lock()
	agentGroupOrch = nil
	agentGroupMu.Unlock()

	run, err := a.StartAgentGroupRun("搜索资料；写摘要", "router-1", []AgentGroupBot{
		{ID: "router-1", Name: "Router", Role: "router", Capabilities: []string{"search"}},
		{ID: "s1", Name: "Scout", Role: "worker", Capabilities: []string{"search"}},
		{ID: "w1", Name: "Scribe", Role: "worker", Capabilities: []string{"writing"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" {
		t.Fatalf("status=%s", run.Status)
	}
	if !strings.Contains(run.Summary, "Scout") || !strings.Contains(run.Summary, "Scribe") {
		t.Fatalf("summary=%q", run.Summary)
	}
	if len(run.Progress) != 2 {
		t.Fatalf("progress=%v", run.Progress)
	}
	got, err := a.GetAgentGroupRun(run.ID)
	if err != nil || got.ID != run.ID {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	list, err := a.ListAgentGroupRuns()
	if err != nil || len(list) == 0 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestStartAgentGroupRunRequiresGoal(t *testing.T) {
	a := &App{}
	if _, err := a.StartAgentGroupRun("  ", "", nil); err == nil {
		t.Fatal("expected error")
	}
}
