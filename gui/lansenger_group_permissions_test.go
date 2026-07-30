package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestLansengerGroupShortcutCommandsAreBlocked(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy
	h := &IMMessageHandler{}

	for _, command := range []string{"/reset", "/goal test", "/skill list", "/btw summarize"} {
		resp, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{UserID: "group-user", Platform: "lansenger_local", Text: command}, command, ctx, nil, nil)
		if !handled || resp == nil || resp.Text != localizedLansengerGroupCommandRestrictedMessage("") {
			t.Fatalf("command %q = %#v handled=%v, want group rejection", command, resp, handled)
		}
	}

	if resp, handled := h.handleImmediateIMCommandWithLoop(IMUserMessage{UserID: "private-user", Platform: "lansenger_local", Text: "/help"}, "/help", nil, nil, nil); !handled || resp == nil {
		t.Fatal("private shortcut command should retain existing behavior")
	}
}

func TestLansengerGroupMoACommandIsBlockedBeforeSessionMutation(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy
	h := &IMMessageHandler{}

	resp := h.handleIMMessageWithLoop(IMUserMessage{
		UserID:   "group-user",
		Platform: "lansenger_local",
		Text:     "/moa summarize this",
	}, ctx, nil, nil, nil, nil)
	if resp == nil || resp.Text != localizedLansengerGroupCommandRestrictedMessage("") {
		t.Fatalf("response = %#v, want group shortcut rejection", resp)
	}
	if h.moaSessions != nil {
		t.Fatal("group /moa must not allocate or mutate the MoA session store")
	}
}

func TestLansengerGroupPermissionPolicyRestrictsKnowledgeSources(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}

	args := map[string]interface{}{"query": "status"}
	if err := policy.restrictKnowledgeArgs(args); err != nil {
		t.Fatalf("restrict unspecified sources: %v", err)
	}
	if got := knowledgeIDsForLansengerPermissionTest(args["source_ids"]); len(got) != 1 || got[0] != "approved" {
		t.Fatalf("source_ids = %#v, want [approved]", got)
	}

	if err := policy.restrictKnowledgeArgs(map[string]interface{}{"source_ids": []interface{}{"other"}}); err == nil {
		t.Fatal("unapproved source should be rejected")
	}
	if err := policy.restrictKnowledgeArgs(map[string]interface{}{"source_ids": []interface{}{}}); err == nil {
		t.Fatal("explicit empty source list should be rejected")
	}
	if err := (lansengerGroupPermissionPolicy{}).restrictKnowledgeArgs(map[string]interface{}{"query": "status"}); err == nil {
		t.Fatal("empty allowlist should deny knowledge access")
	}
}

func TestLansengerGroupPermissionPolicyRestrictsFiles(t *testing.T) {
	allowed := t.TempDir()
	inside := filepath.Join(allowed, "visible.txt")
	if err := os.WriteFile(inside, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	policy := lansengerGroupPermissionPolicy{AllowedDirectories: []string{allowed}}
	if err := policy.validateFileToolArgs("read_file", map[string]interface{}{"path": inside}); err != nil {
		t.Fatalf("allowed file rejected: %v", err)
	}
	if err := policy.validateFileToolArgs("read_file", map[string]interface{}{"path": outside}); err == nil {
		t.Fatal("outside file should be rejected")
	}
	if err := policy.validateFileToolArgs("list_directory", map[string]interface{}{"path": filepath.Join(allowed, "..", filepath.Base(allowed))}); err != nil {
		t.Fatalf("cleaned allowed directory rejected: %v", err)
	}
	if err := policy.validateFileToolArgs("search_files", map[string]interface{}{"project_path": outsideDir}); err == nil {
		t.Fatal("search_files outside project_path should be rejected")
	}

	policy.AllowAllDirectories = true
	if err := policy.validateFileToolArgs("read_file", map[string]interface{}{"path": outside}); err != nil {
		t.Fatalf("allow-all should permit file access: %v", err)
	}
}

func TestLansengerGroupPermissionPolicyValidatesResolvedFileToolPath(t *testing.T) {
	allowed := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	args := map[string]interface{}{"path": "relative.txt"}
	policy := lansengerGroupPermissionPolicy{AllowedDirectories: []string{allowed}}
	err := policy.resolveAndValidateFileToolArgs("read_file", args, func(string) (string, error) {
		return outside, nil
	})
	if err == nil {
		t.Fatal("path must be checked after resolving against the tool's execution base")
	}

	inside := filepath.Join(allowed, "visible.txt")
	if err := os.WriteFile(inside, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	args = map[string]interface{}{"project_path": "relative-project"}
	if err := policy.resolveAndValidateFileToolArgs("search_files", args, func(string) (string, error) {
		return allowed, nil
	}); err != nil {
		t.Fatalf("resolved directory rejected: %v", err)
	}
	if got, _ := args["project_path"].(string); got != allowed {
		t.Fatalf("project_path = %q, want resolved path %q", got, allowed)
	}
}

func TestLansengerGroupPermissionPolicyFiltersToolExposure(t *testing.T) {
	tools := []map[string]interface{}{
		agent.ToolDef("knowledge_search", "", nil, nil),
		agent.ToolDef("knowledge_delete_source", "", nil, nil),
		agent.ToolDef("read_file", "", nil, nil),
		agent.ToolDef("bash", "", nil, nil),
		agent.ToolDef("git_status", "", nil, nil),
		agent.ToolDef("web_fetch", "", nil, nil),
		agent.ToolDef("current_datetime", "", nil, nil),
	}

	filtered := filterToolsForLansengerGroupPermissions(tools, lansengerGroupPermissionPolicy{})
	if containsLansengerPermissionTestTool(filtered, "knowledge_search") || containsLansengerPermissionTestTool(filtered, "read_file") || containsLansengerPermissionTestTool(filtered, "bash") || containsLansengerPermissionTestTool(filtered, "git_status") {
		t.Fatalf("ungranted tools remain exposed: %#v", filtered)
	}
	if !containsLansengerPermissionTestTool(filtered, "current_datetime") || !containsLansengerPermissionTestTool(filtered, "web_fetch") {
		t.Fatal("explicitly safe tools should remain exposed")
	}

	filtered = filterToolsForLansengerGroupPermissions(tools, lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs:  []string{"approved"},
		AllowedDirectories:  []string{t.TempDir()},
		AllowAllDirectories: false,
	})
	if !containsLansengerPermissionTestTool(filtered, "knowledge_search") || !containsLansengerPermissionTestTool(filtered, "read_file") {
		t.Fatalf("granted retrieval tools missing: %#v", filtered)
	}
	if containsLansengerPermissionTestTool(filtered, "knowledge_delete_source") || containsLansengerPermissionTestTool(filtered, "bash") || containsLansengerPermissionTestTool(filtered, "git_status") {
		t.Fatalf("unsafe tools remain exposed: %#v", filtered)
	}
}

func TestLansengerGroupPermissionPolicyFailsClosedForUnlistedTools(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{
		KnowledgeSourceIDs: []string{"approved"},
		AllowedDirectories: []string{t.TempDir()},
	}
	for _, name := range []string{"git_status", "check_health", "manage_skill", "call_mcp_tool", "future_local_tool"} {
		if policy.allowsTool(name) {
			t.Fatalf("%q must be denied unless it has an explicit group-permission contract", name)
		}
	}
	for _, name := range []string{"knowledge_search", "read_file", "web_search", "web_fetch", "current_datetime"} {
		if !policy.allowsTool(name) {
			t.Fatalf("%q should be allowed by its explicit contract", name)
		}
	}
}

func TestLansengerGroupPermissionPolicyBlocksInjectedAndDiscoveredTools(t *testing.T) {
	policy := lansengerGroupPermissionPolicy{KnowledgeSourceIDs: []string{"approved"}}
	ctx := NewLoopContext("lansenger-group", 1, nil)
	ctx.LansengerGroupPermissions = &policy

	defs := []map[string]interface{}{
		agent.ToolDef("current_datetime", "", nil, nil),
		agent.ToolDef("git_status", "", nil, nil),
		agent.ToolDef("call_mcp_tool", "", nil, nil),
	}
	h := &IMMessageHandler{}
	filtered, _ := h.finalizeInjectionAugmentedTools(ctx, "group-user", defs)
	if containsLansengerPermissionTestTool(filtered, "git_status") || containsLansengerPermissionTestTool(filtered, "call_mcp_tool") {
		t.Fatalf("injected tools bypassed group policy: %#v", filtered)
	}
	if !containsLansengerPermissionTestTool(filtered, "current_datetime") {
		t.Fatalf("safe injected tool was removed: %#v", filtered)
	}

	ranked := []discoveredToolScore{{name: "current_datetime"}, {name: "git_status"}, {name: "remote_search"}}
	discovered := filterDiscoveredToolsForLansengerGroup(ranked, map[string]discoverableMCPTool{"remote_search": {}}, policy)
	if len(discovered) != 1 || discovered[0].name != "current_datetime" {
		t.Fatalf("group discovery = %#v, want only current_datetime", discovered)
	}
}

func knowledgeIDsForLansengerPermissionTest(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func containsLansengerPermissionTestTool(defs []map[string]interface{}, name string) bool {
	for _, def := range defs {
		if fn, ok := def["function"].(map[string]interface{}); ok && fn["name"] == name {
			return true
		}
	}
	return false
}
