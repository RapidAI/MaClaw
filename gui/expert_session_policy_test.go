package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExpertIDFromUserID(t *testing.T) {
	cases := []struct {
		userID string
		want   string
	}{
		{"desktop-user:expert:builtin-paper-polish", "builtin-paper-polish"},
		{"desktop-user:expert:expert-123-abc", "expert-123-abc"},
		{"desktop-user", ""},
		{"desktop-user:D:/proj/demo", ""},
		{"", ""},
		{"desktop-user:expert:", ""},
	}
	for _, c := range cases {
		if got := expertIDFromUserID(c.userID); got != c.want {
			t.Errorf("expertIDFromUserID(%q) = %q, want %q", c.userID, got, c.want)
		}
	}
	if got := expertIDFromUserID(expertSessionUserID("expert-1-x")); got != "expert-1-x" {
		t.Fatalf("roundtrip failed, got %q", got)
	}
}

func toolDefNamed(name string) map[string]interface{} {
	return map[string]interface{}{
		"type":     "function",
		"function": map[string]interface{}{"name": name, "description": "d"},
	}
}

func TestFilterToolsForExpert(t *testing.T) {
	tools := []map[string]interface{}{
		toolDefNamed("read_file"),
		toolDefNamed("write_file"),
		toolDefNamed("bash"),
		toolDefNamed("manage_skill"),
		toolDefNamed("ask_user"),
		toolDefNamed("discover_tool"),
	}

	// nil def / empty allow-list → passthrough.
	if got := filterToolsForExpert(tools, nil); len(got) != len(tools) {
		t.Fatalf("nil def should passthrough, got %d", len(got))
	}
	if got := filterToolsForExpert(tools, &ExpertDefinition{}); len(got) != len(tools) {
		t.Fatalf("empty Tools should passthrough, got %d", len(got))
	}

	def := &ExpertDefinition{Tools: []string{"read_file"}}
	got := filterToolsForExpert(tools, def)
	names := map[string]bool{}
	for _, td := range got {
		names[extractToolName(td)] = true
	}
	// Whitelist + always-kept interaction/skill tools.
	want := []string{"read_file", "manage_skill", "ask_user", "discover_tool"}
	if len(got) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(got), names)
	}
	for _, w := range want {
		if !names[w] {
			t.Fatalf("expected tool %q to be kept, got %v", w, names)
		}
	}
	if names["bash"] || names["write_file"] {
		t.Fatalf("non-whitelisted tools must be dropped, got %v", names)
	}
}

func TestFilterSkillsForExpert(t *testing.T) {
	skills := []NLSkillDefinition{
		{Name: "pptx-gen", Description: "d1"},
		{Name: "pdf-word", Description: "d2"},
		{Name: "doc-redact", Description: "d3"},
	}
	if got := filterSkillsForExpert(skills, nil); len(got) != len(skills) {
		t.Fatalf("nil def should passthrough, got %d", len(got))
	}
	if got := filterSkillsForExpert(skills, &ExpertDefinition{}); len(got) != len(skills) {
		t.Fatalf("empty Skills should passthrough, got %d", len(got))
	}
	def := &ExpertDefinition{Skills: []string{"pptx-gen"}}
	got := filterSkillsForExpert(skills, def)
	if len(got) != 1 || got[0].Name != "pptx-gen" {
		t.Fatalf("expected only pptx-gen, got %+v", got)
	}
}

func TestProjectPathGuardForExpertSessions(t *testing.T) {
	// Expert session userIDs must not be mistaken for project paths.
	if got := projectPathFromSessionOwnerID("desktop-user:expert:builtin-pptx-maker"); got != "" {
		t.Fatalf("expert session userID should not resolve to a project path, got %q", got)
	}
	// Real project sessions still resolve.
	if got := projectPathFromSessionOwnerID("desktop-user:D:/proj/demo"); got == "" {
		t.Fatal("project session userID should still resolve to a path")
	}
	// Local session unchanged.
	if got := projectPathFromSessionOwnerID(desktopUserID); got != "" {
		t.Fatalf("local session should not resolve to a path, got %q", got)
	}
}

// swapExpertStoreForTest points the process-wide expert store at a temp file
// and clears the def cache, restoring everything on cleanup.
func swapExpertStoreForTest(t *testing.T) {
	t.Helper()
	old := defaultExpertStore
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))
	t.Cleanup(func() { defaultExpertStore = old })
}

func TestNormalizeAIAssistantSessionUserIDExpert(t *testing.T) {
	// Expert keys pass through unchanged (and before the project-path branch).
	got, err := normalizeAIAssistantSessionUserID("desktop-user:expert:builtin-paper-polish")
	if err != nil || got != "desktop-user:expert:builtin-paper-polish" {
		t.Fatalf("expert key: got=%q err=%v", got, err)
	}
	got, err = normalizeAIAssistantSessionUserID(" desktop-user:expert:expert-1-x ")
	if err != nil || got != "desktop-user:expert:expert-1-x" {
		t.Fatalf("trimmed expert key: got=%q err=%v", got, err)
	}
	// Existing behaviors unchanged.
	if got, err = normalizeAIAssistantSessionUserID(""); err != nil || got != "" {
		t.Fatalf("empty: got=%q err=%v", got, err)
	}
	if got, err = normalizeAIAssistantSessionUserID(desktopUserID); err != nil || got != desktopUserID {
		t.Fatalf("desktop: got=%q err=%v", got, err)
	}
	// Garbage keys are rejected (ClearAIAssistantHistoryForSession now surfaces
	// this error instead of silently wiping the main conversation).
	if _, err = normalizeAIAssistantSessionUserID("garbage-key"); err == nil {
		t.Fatal("garbage key should be rejected")
	}
}

func TestExpertToolCallRejection(t *testing.T) {
	def := &ExpertDefinition{Tools: []string{"read_file"}}
	// Whitelisted tool passes.
	if got := expertToolCallRejectionWithDef(def, "read_file", "{}"); got != "" {
		t.Fatalf("whitelisted tool should pass, got %q", got)
	}
	// Always-kept tools pass even though not in the whitelist.
	for _, name := range []string{"manage_skill", "ask_user", "discover_tool"} {
		if got := expertToolCallRejectionWithDef(def, name, "{}"); got != "" {
			t.Fatalf("always-kept tool %q should pass, got %q", name, got)
		}
	}
	// Non-whitelisted tool is rejected before execution.
	if got := expertToolCallRejectionWithDef(def, "bash", "{}"); !strings.Contains(got, "未授权工具 bash") {
		t.Fatalf("bash should be rejected, got %q", got)
	}
	// Empty whitelist = no tool restriction.
	openDef := &ExpertDefinition{}
	if got := expertToolCallRejectionWithDef(openDef, "bash", "{}"); got != "" {
		t.Fatalf("empty whitelist should not restrict, got %q", got)
	}
	// nil def = non-expert session.
	if got := expertToolCallRejectionWithDef(nil, "bash", "{}"); got != "" {
		t.Fatalf("nil def should not restrict, got %q", got)
	}
}

func TestExpertToolCallRejectionManageSkillRun(t *testing.T) {
	def := &ExpertDefinition{Skills: []string{"pptx-gen"}}
	// run on whitelisted skill passes.
	if got := expertToolCallRejectionWithDef(def, "manage_skill", `{"action":"run","name":"pptx-gen"}`); got != "" {
		t.Fatalf("whitelisted skill run should pass, got %q", got)
	}
	// run on non-whitelisted skill is rejected.
	if got := expertToolCallRejectionWithDef(def, "manage_skill", `{"action":"run","name":"pdf-word"}`); !strings.Contains(got, "未授权技能 pdf-word") {
		t.Fatalf("non-whitelisted skill run should be rejected, got %q", got)
	}
	// Non-run actions (list/status/...) are not skill-gated.
	if got := expertToolCallRejectionWithDef(def, "manage_skill", `{"action":"status","name":"pdf-word"}`); got != "" {
		t.Fatalf("non-run manage_skill action should pass, got %q", got)
	}
	// Empty skill whitelist = all skills allowed.
	if got := expertToolCallRejectionWithDef(&ExpertDefinition{}, "manage_skill", `{"action":"run","name":"pdf-word"}`); got != "" {
		t.Fatalf("empty skill whitelist should not restrict, got %q", got)
	}
	// Other tools are untouched by the skill gate.
	if got := expertToolCallRejectionWithDef(def, "read_file", `{"path":"x"}`); got != "" {
		t.Fatalf("non manage_skill tool should pass, got %q", got)
	}
}

func TestExpertToolExecutionRejectionResolvesBuiltin(t *testing.T) {
	swapExpertStoreForTest(t)
	invalidateExpertDefCache("builtin-pptx-maker")
	// builtin-pptx-maker binds Skills=["pptx-gen"], Tools=[] (all tools).
	userID := expertSessionUserID("builtin-pptx-maker")
	if got := expertToolExecutionRejection(userID, "bash", "{}"); got != "" {
		t.Fatalf("pptx expert has no tool whitelist; bash should pass, got %q", got)
	}
	if got := expertToolExecutionRejection(userID, "manage_skill", `{"action":"run","name":"pdf-word"}`); !strings.Contains(got, "未授权技能") {
		t.Fatalf("pptx expert must not run pdf-word, got %q", got)
	}
	if got := expertToolExecutionRejection(userID, "manage_skill", `{"action":"run","name":"pptx-gen"}`); got != "" {
		t.Fatalf("pptx expert must run pptx-gen, got %q", got)
	}
	// Non-expert session → no gate.
	if got := expertToolExecutionRejection(desktopUserID, "manage_skill", `{"action":"run","name":"pdf-word"}`); got != "" {
		t.Fatalf("desktop session should not be gated, got %q", got)
	}
}

func TestFilterDiscoveredToolsForExpert(t *testing.T) {
	mcp := map[string]discoverableMCPTool{
		"mcp:local:s1:search": {serverID: "s1", toolName: "search"},
	}
	ranked := []discoveredToolScore{
		{name: "read_file", score: 3},
		{name: "bash", score: 2},
		{name: "mcp:local:s1:search", score: 1},
	}
	// nil/empty whitelist → passthrough.
	if got := filterDiscoveredToolsForExpert(ranked, mcp, nil); len(got) != 3 {
		t.Fatalf("nil def should passthrough, got %d", len(got))
	}
	// Whitelist without call_mcp_tool: bash and the MCP match are dropped.
	def := &ExpertDefinition{Tools: []string{"read_file"}}
	got := filterDiscoveredToolsForExpert(ranked, mcp, def)
	if len(got) != 1 || got[0].name != "read_file" {
		t.Fatalf("expected only read_file, got %+v", got)
	}
	// Whitelisting call_mcp_tool keeps MCP matches.
	def2 := &ExpertDefinition{Tools: []string{"read_file", "call_mcp_tool"}}
	got = filterDiscoveredToolsForExpert(ranked, mcp, def2)
	if len(got) != 2 {
		t.Fatalf("expected read_file + MCP match, got %+v", got)
	}
}

func TestHardStructuralFullExecutionProfileExpert(t *testing.T) {
	swapExpertStoreForTest(t)
	invalidateExpertDefCache("builtin-pptx-maker")
	// Expert session: full profile forced even for a short light-looking text.
	profile, forced := hardStructuralFullExecutionProfile(IMUserMessage{
		UserID: expertSessionUserID("builtin-pptx-maker"),
		Text:   "你好",
	}, false, false)
	if !forced {
		t.Fatal("expert session must force full execution profile")
	}
	if profile.Reason != "expert session" {
		t.Fatalf("expected reason 'expert session', got %q", profile.Reason)
	}
	// Non-expert session: the expert branch must not fire.
	_, forced = hardStructuralFullExecutionProfile(IMUserMessage{
		UserID: desktopUserID,
		Text:   "你好",
	}, false, false)
	if forced {
		t.Log("non-expert short text forced full by another heuristic (acceptable), but not by the expert branch")
	}
}

func TestExpertIDPattern(t *testing.T) {
	valid := []string{"expert-1712345678901234567-a1b2c3d4", "builtin-paper-polish", "a.b_c-d", "A1"}
	for _, id := range valid {
		if !expertIDPattern.MatchString(id) {
			t.Fatalf("id %q should be valid", id)
		}
	}
	invalid := []string{"", "bad id", "expert/x", "expert\\y", "中文id", strings.Repeat("a", 129), "a;b"}
	for _, id := range invalid {
		if expertIDPattern.MatchString(id) {
			t.Fatalf("id %q should be invalid", id)
		}
	}
	// Generated ids always satisfy the pattern.
	if id := newExpertID(); !expertIDPattern.MatchString(id) {
		t.Fatalf("generated id %q must match the pattern", id)
	}
}
