package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRecallLightMemRoutesTaskArtifactsAndProjectMemory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	projectPath := `D:\work\snake`
	entries := []Entry{
		{Content: "Requirements document: build snake game with keyboard control and scoring", Category: CategoryTaskArtifact, Scope: ScopeProject, Tags: []string{projectPath}},
		{Content: "Project uses Vite and pnpm test for verification", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{projectPath}},
		{Content: "Unrelated deployment note for another project", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\other`}},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	results, plan := store.RecallLightMemDebug("继续实现 snake 需求和测试", "", projectPath)
	if len(results) == 0 {
		t.Fatalf("expected LightMem recall results, plan=%+v", plan)
	}
	if !hasRoute(plan, "task_artifact") || !hasRoute(plan, "project_memory") {
		t.Fatalf("expected task_artifact and project_memory routes, got %+v", plan.Routes)
	}
	joined := strings.ToLower(joinEntryContents(results))
	if !strings.Contains(joined, "requirements") || !strings.Contains(joined, "pnpm test") {
		t.Fatalf("expected task artifact and project fact in results, got %q", joined)
	}
	if strings.Contains(joined, "unrelated deployment") {
		t.Fatalf("strict project route leaked other project memory: %q", joined)
	}
}

func TestBuildLightMemRecallPlanSkipsSmallTalk(t *testing.T) {
	plan := BuildLightMemRecallPlan("谢谢", "", "")
	if plan.NeedMemory {
		t.Fatalf("expected small talk to skip memory, got %+v", plan)
	}
}

func hasRoute(plan LightMemRecallPlan, name string) bool {
	for _, route := range plan.Routes {
		if route.Name == name {
			return true
		}
	}
	return false
}

func joinEntryContents(entries []Entry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, entry.Content)
	}
	return strings.Join(parts, "\n")
}
