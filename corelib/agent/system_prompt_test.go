package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildSystemPromptCodingWorkflowUsesInternalPath(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:          "MaClaw",
			RoleDescription:   "test agent",
			IsProMode:         true,
			HasCodingSessions: true,
		},
	}, "fix a bug", true)

	if strings.Contains(prompt, "create_session") || strings.Contains(prompt, "send_and_observe") {
		t.Fatalf("core coding prompt should not route AI coding through external session tools: %s", prompt)
	}
	if !strings.Contains(prompt, "CodingSubAgent") {
		t.Fatalf("core coding prompt should mention the internal coding path")
	}
}

func TestBuildSystemPromptIgnoresLegacyCodingProviderInfo(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:        "MaClaw",
			RoleDescription: "test agent",
			IsProMode:       true,
		},
		CodingProviderInfo: func() string {
			return "create_session provider hint should not appear"
		},
	}, "fix a bug", true)

	if strings.Contains(prompt, "create_session provider hint") {
		t.Fatalf("legacy CodingProviderInfo should not be injected into AI prompt: %s", prompt)
	}
}

func TestPromptBundleStringSkipsEmptySegments(t *testing.T) {
	bundle := PromptBundle{
		StableSystemPrompt: "stable",
		SessionContext:     "  ",
		RetrievedContext:   "retrieved",
	}

	if got := bundle.String(); got != "stable\nretrieved" {
		t.Fatalf("PromptBundle.String() = %q", got)
	}
}

func TestPromptBundleTokenStatsAddsSegments(t *testing.T) {
	bundle := PromptBundle{
		StableSystemPrompt: "stable prompt",
		SessionContext:     "session context",
		RetrievedContext:   "retrieved context",
	}

	stats := bundle.TokenStats()
	if stats.StableSystemPromptTokens <= 0 || stats.SessionContextTokens <= 0 || stats.RetrievedContextTokens <= 0 {
		t.Fatalf("expected positive segment token stats, got %+v", stats)
	}
	if stats.TotalTokens != stats.StableSystemPromptTokens+stats.SessionContextTokens+stats.RetrievedContextTokens {
		t.Fatalf("total tokens should equal segment sum, got %+v", stats)
	}
}

func TestBuildPromptBundleSplitsDynamicSections(t *testing.T) {
	bundle := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", RoleDescription: "test agent"},
		SSHHostLister: func() []corelib.SSHHostEntry {
			return []corelib.SSHHostEntry{{Label: "prod", User: "root", Host: "example.com", Port: 2222}}
		},
		MCPServerLister: func() []MCPServerInfo {
			return []MCPServerInfo{{ID: "bundle-probe", Name: "BundleProbeMCP", Tools: []string{"bundle_probe_open"}}}
		},
		SkillLister: func() []SkillInfo {
			return []SkillInfo{{Name: "ppt", Description: "make slides"}}
		},
	}, "use ppt", true)

	for _, dynamic := range []string{"example.com", "ppt: make slides"} {
		if strings.Contains(bundle.StableSystemPrompt, dynamic) {
			t.Fatalf("stable segment should not contain dynamic section %q", dynamic)
		}
	}
	for _, want := range []string{"example.com", "BundleProbeMCP"} {
		if !strings.Contains(bundle.SessionContext, want) {
			t.Fatalf("session context missing %q: %s", want, bundle.SessionContext)
		}
	}
	if !strings.Contains(bundle.RetrievedContext, "ppt: make slides") {
		t.Fatalf("retrieved context missing skill index: %s", bundle.RetrievedContext)
	}
	if bundle.String() != BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", RoleDescription: "test agent"},
		SSHHostLister: func() []corelib.SSHHostEntry {
			return []corelib.SSHHostEntry{{Label: "prod", User: "root", Host: "example.com", Port: 2222}}
		},
		MCPServerLister: func() []MCPServerInfo {
			return []MCPServerInfo{{ID: "bundle-probe", Name: "BundleProbeMCP", Tools: []string{"bundle_probe_open"}}}
		},
		SkillLister: func() []SkillInfo {
			return []SkillInfo{{Name: "ppt", Description: "make slides"}}
		},
	}, "use ppt", true) {
		t.Fatal("BuildSystemPrompt should preserve PromptBundle string semantics")
	}
}

func TestBuildPromptBundlePostSSHRulesAreSessionContext(t *testing.T) {
	bundle := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", RoleDescription: "test agent"},
		PostSSHRules: func(b *strings.Builder) {
			b.WriteString("\nDYNAMIC_GUI_SKILL_AND_TOOL_STATE\n")
		},
	}, "hello", true)

	if strings.Contains(bundle.StableSystemPrompt, "DYNAMIC_GUI_SKILL_AND_TOOL_STATE") {
		t.Fatal("stable segment should not contain GUI dynamic SSH/tool state")
	}
	if !strings.Contains(bundle.SessionContext, "DYNAMIC_GUI_SKILL_AND_TOOL_STATE") {
		t.Fatalf("session context missing dynamic GUI state: %s", bundle.SessionContext)
	}
}

func TestBuildPromptBundlePostCodingWorkflowIsSessionContext(t *testing.T) {
	bundle := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", RoleDescription: "test agent", IsProMode: true},
		PostCodingWorkflow: func(b *strings.Builder) {
			b.WriteString("\nDYNAMIC_CODING_SESSION_STATE\n")
		},
	}, "fix bug", true)

	if strings.Contains(bundle.StableSystemPrompt, "DYNAMIC_CODING_SESSION_STATE") {
		t.Fatal("stable segment should not contain dynamic coding session state")
	}
	if !strings.Contains(bundle.SessionContext, "DYNAMIC_CODING_SESSION_STATE") {
		t.Fatalf("session context missing dynamic coding state: %s", bundle.SessionContext)
	}
}

func TestPromptBundleStableCacheKeyChangesOnlyWithStableSegment(t *testing.T) {
	base := PromptBundle{
		StableSystemPrompt: "stable prompt",
		SessionContext:     "session one",
		RetrievedContext:   "retrieved one",
	}
	changedDynamic := PromptBundle{
		StableSystemPrompt: "stable prompt",
		SessionContext:     "session two",
		RetrievedContext:   "retrieved two",
	}
	changedStable := PromptBundle{StableSystemPrompt: "different stable prompt"}

	if base.StableCacheKey() == "" {
		t.Fatal("expected stable cache key")
	}
	if base.StableCacheKey() != changedDynamic.StableCacheKey() {
		t.Fatalf("dynamic segment changes should not alter stable cache key: %q vs %q", base.StableCacheKey(), changedDynamic.StableCacheKey())
	}
	if base.StableCacheKey() == changedStable.StableCacheKey() {
		t.Fatalf("stable segment changes should alter stable cache key: %q", base.StableCacheKey())
	}
}
