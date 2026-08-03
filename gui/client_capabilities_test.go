package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestBuildClientCapabilityPromptConstrainsTextOnlyHardware(t *testing.T) {
	prompt := buildClientCapabilityPrompt(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{MaxChars: 240, Locale: "zh-CN"},
	}})
	for _, want := range []string{"Output modalities: text", "max 240 Unicode characters", "Do not create or attach image"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}
