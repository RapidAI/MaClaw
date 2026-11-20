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
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "画一张这个系统的架构图"}
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
			IMUserMessage{UserID: tc.userID, Platform: "scheduler", Text: "画一张这个系统的架构图"},
			ExecutionProfile{}, nil, "")
		if strings.Contains(prompt, "讲解配图") {
			t.Fatalf("%s: externally delivered turn must not carry the diagram cue", tc.name)
		}
	}
}

// TestBuildLightIMSystemPrompt_OmitsDiagramCueUntilAsked keeps the drawing
// rule on demand. A desktop markdown turn that only asks for an explanation
// must not be told to emit a mermaid fence.
func TestBuildLightIMSystemPrompt_OmitsDiagramCueUntilAsked(t *testing.T) {
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "解释一下这个系统的架构"}
	prompt := buildLightIMSystemPrompt(msg, ExecutionProfile{}, nil, "")
	if strings.Contains(prompt, "讲解配图") || strings.Contains(prompt, "```mermaid") {
		t.Fatal("desktop conversation must not carry the drawing rule until the user asks for a diagram")
	}
}

// TestConversationWantsDiagram_PlainLookupStaysOff guards the plain-text
// boundary at the predicate: a lookup, even one that says 画图 as image
// generation, is not a flowchart request.
func TestConversationWantsDiagram_PlainLookupStaysOff(t *testing.T) {
	for _, text := range []string{"", "北京天气", "画一只猫", "生成一张图片"} {
		if conversationWantsDiagram(text) {
			t.Fatalf("plain text %q must not request a flowchart", text)
		}
	}
	if !conversationWantsDiagram("请画一张登录流程图") {
		t.Fatal("an explicit flowchart request must opt in")
	}
}

// TestBuildLightIMSystemPrompt_DefersToPhaseInstructions keeps the light cue
// honest about the same precedence rule as the full block: a more specific
// phase instruction (e.g. the patent figure workflow) wins.
func TestBuildLightIMSystemPrompt_DefersToPhaseInstructions(t *testing.T) {
	msg := IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "画一张这个流程的流程图"}
	prompt := buildLightIMSystemPrompt(msg, ExecutionProfile{}, nil, intent.IntentLabel(""))
	if !strings.Contains(prompt, "以该指令为准") {
		t.Fatal("GUI light visualization cue must defer to phase instructions")
	}
}
