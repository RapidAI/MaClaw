package agentruntime

import (
	"strings"
	"testing"
)

// TestBuildLightPrompt_DiagramCueFollowsSurfaceFlag pins the plumbing from
// LightPromptRequest.DiagramRenderingSupported into the shared light bundle.
// The GUI sets it (its reply surfaces render mermaid); headless hosts that
// answer into plain text leave it false and must not receive the cue.
func TestBuildLightPrompt_DiagramCueFollowsSurfaceFlag(t *testing.T) {
	on := BuildLightPrompt(LightPromptRequest{
		RoleName:                  "MaClaw",
		UserText:                  "解释一下这个系统的架构",
		DiagramRenderingSupported: true,
	})
	if !strings.Contains(on, "讲解配图") {
		t.Fatal("light prompt with diagram support must carry the compact visualization cue")
	}
	if !strings.Contains(on, "```mermaid") {
		t.Fatal("light prompt with diagram support must teach the mermaid fence")
	}

	off := BuildLightPrompt(LightPromptRequest{
		RoleName: "MaClaw",
		UserText: "解释一下这个系统的架构",
	})
	if strings.Contains(off, "讲解配图") {
		t.Fatal("light prompt without diagram support must omit the visualization cue")
	}
	if strings.Contains(off, "```mermaid") {
		t.Fatal("light prompt without diagram support must not teach the mermaid fence")
	}
}
