package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// TestTUIPromptOmitsDiagramCue pins the fail-closed half of the visual-output
// feature for this host: the TUI answers into a terminal that renders no
// diagrams, so it must never ask the model for a ```mermaid fence. Telling it
// to do so would make replies degrade into raw syntax nobody can read.
//
// This is intentionally a *negative* guard on the host side. The core default
// already excludes undeclared surfaces (see
// TestBuildPromptBundle_OmitsVisualizationRulesWithoutSurfaceSupport in
// corelib/agent); this test protects against someone later flipping the TUI's
// own SystemPromptConfig, which core cannot see.
func TestTUIPromptOmitsDiagramCue(t *testing.T) {
	deps := agent.SystemPromptDeps{
		Config: agent.SystemPromptConfig{
			RoleName:        "MaClaw",
			RoleDescription: "a terminal assistant",
			IsProMode:       true,
			PromptProfile:   agent.PromptProfileFull,
		},
	}
	prompt := agent.BuildSystemPrompt(deps, "解释一下这个系统的架构", true)
	if strings.Contains(prompt, "讲解配图") {
		t.Fatal("TUI prompt must not carry the diagram cue: the terminal cannot render mermaid")
	}
	if strings.Contains(prompt, "```mermaid") {
		t.Fatal("TUI prompt must not teach the mermaid fence")
	}
	// Sanity: the absence above must not be because the block is missing from
	// the build entirely — a renamed section would silently make this vacuous.
	if !strings.Contains(agent.PromptVisualizationRules, "讲解配图") {
		t.Fatal("sanity check failed: PromptVisualizationRules no longer contains the expected marker")
	}
}
