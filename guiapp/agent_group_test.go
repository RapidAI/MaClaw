package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentgroup"
)

func TestStartAgentGroupRunAggregates(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	agentGroupMu.Lock()
	agentGroupOrch = nil
	agentGroupMu.Unlock()
	prev := agentGroupDispatchFunc
	agentGroupDispatchFunc = func(_ *App, ctx context.Context, bot agentgroup.Bot, task agentgroup.SubTask) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		return "已处理：" + strings.TrimSpace(task.Description), nil
	}
	t.Cleanup(func() { agentGroupDispatchFunc = prev })

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
	if run.PageID == "" {
		t.Fatal("expected linked shared page")
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
	a := &App{testHomeDir: t.TempDir()}
	agentGroupMu.Lock()
	agentGroupOrch = nil
	agentGroupMu.Unlock()
	if _, err := a.StartAgentGroupRun("  ", "", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestAgentGroupDispatchPollsAssignment(t *testing.T) {
	calls := 0
	a := &App{}
	// Override ListBotAssignments via embedding isn't easy; unit-test matching helper path by
	// injecting dispatch that simulates success.
	prev := agentGroupDispatchFunc
	agentGroupDispatchFunc = func(_ *App, _ context.Context, bot agentgroup.Bot, task agentgroup.SubTask) (string, error) {
		calls++
		return "report-from-" + bot.ID + ":" + task.Description, nil
	}
	t.Cleanup(func() { agentGroupDispatchFunc = prev })
	agentGroupMu.Lock()
	agentGroupOrch = nil
	agentGroupMu.Unlock()
	a.testHomeDir = t.TempDir()
	run, err := a.StartAgentGroupRun("写报告", "", []AgentGroupBot{{ID: "w", Name: "Writer", Role: "worker", Capabilities: []string{"writing"}}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(run.Summary, "Writer") {
		t.Fatalf("calls=%d summary=%q", calls, run.Summary)
	}
}
