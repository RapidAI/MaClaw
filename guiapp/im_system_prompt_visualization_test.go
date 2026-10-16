package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// TestBuildLightIMSystemPrompt_CarriesDiagramCue pins the GUI light path
// end-to-end: buildLightIMSystemPrompt -> agentruntime.BuildLightPrompt must
// carry the compact diagram cue, because every GUI reply surface renders
// ```mermaid fences (aiAssistantMarkdown -> AssistantMermaidDiagram). Hosts
// whose replies land in plain text (TUI, hub bots) construct their own config
// without the flag and must not see this cue.
func TestBuildLightIMSystemPrompt_CarriesDiagramCue(t *testing.T) {
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "解释一下这个系统的架构"}
	prompt := buildLightIMSystemPrompt(msg, ExecutionProfile{}, nil, "")
	if !strings.Contains(prompt, "讲解配图") {
		t.Fatal("GUI light prompt must carry the compact visualization cue")
	}
	if !strings.Contains(prompt, "```mermaid") {
		t.Fatal("GUI light prompt must teach the mermaid fence")
	}
	if strings.Contains(prompt, "图型选择") {
		t.Fatal("GUI light prompt must stay compact: no full diagram-type routing table")
	}
}

// TestBuildLightIMSystemPrompt_OmitsDiagramCueForExternalDelivery closes the
// gap that "GUI builder => GUI surface" would otherwise hide: two turn shapes
// ride this same builder but are delivered OUT of the desktop markdown
// pipeline, where a ```mermaid fence is just raw syntax.
//
//   - scheduled tasks (owner "scheduled_task" / "lansenger-scheduled:...":)
//     deliver to the task's own channel.
//   - the Lansenger group bridge (owner "lansenger-group:...") replies over
//     the IM bus.
func TestBuildLightIMSystemPrompt_OmitsDiagramCueForExternalDelivery(t *testing.T) {
	cases := []struct {
		name   string
		userID string
	}{
		{"scheduled task", "scheduled_task"},
		{"profile scheduled task", "lansenger-scheduled:3:bot:4:task1"},
		{"lansenger group", "lansenger-group:5:grp42:6:user7"},
	}
	for _, tc := range cases {
		prompt := buildLightIMSystemPrompt(
			IMUserMessage{UserID: tc.userID, Platform: "scheduler", Text: "解释一下这个系统的架构"},
			ExecutionProfile{}, nil, "")
		if strings.Contains(prompt, "讲解配图") {
			t.Fatalf("%s: externally delivered turn must not carry the diagram cue", tc.name)
		}
	}
}

// TestBuildLightIMSystemPrompt_DefersToPhaseInstructions keeps the light cue
// honest about the same precedence rule as the full block: a more specific
// phase instruction (e.g. the patent figure workflow) wins.
func TestBuildLightIMSystemPrompt_DefersToPhaseInstructions(t *testing.T) {
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "梳理一下这个流程"}
	prompt := buildLightIMSystemPrompt(msg, ExecutionProfile{}, nil, intent.IntentLabel(""))
	if !strings.Contains(prompt, "以该指令为准") {
		t.Fatal("GUI light visualization cue must defer to phase instructions")
	}
}
