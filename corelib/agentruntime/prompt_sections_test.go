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

func TestBuildLightPromptLiveDataAnswersFromAStatedFact(t *testing.T) {
	prompt := BuildLightPrompt(LightPromptRequest{
		RoleName:      "MaClaw",
		UserText:      "北京天气",
		ExecutionTask: "live_data",
	})
	if !strings.Contains(prompt, "already states the current fact") || !strings.Contains(prompt, "call web_fetch on a result page") {
		t.Fatalf("live_data prompt = %s", prompt)
	}
	if strings.Contains(prompt, "Do not answer from the search snippet alone") || strings.Contains(prompt, "answer from that evidence") {
		t.Fatalf("live_data prompt treats any snippet as the answer: %s", prompt)
	}
	if strings.Contains(prompt, "一次成功的实时查询之后立即作答") {
		t.Fatalf("live_data prompt still stops after one lookup: %s", prompt)
	}
	if !strings.Contains(prompt, "列出的结果已经写明当前事实时直接作答") || !strings.Contains(prompt, "不要写文件、不要执行命令、不要生成文档") {
		t.Fatalf("live_data prompt dropped the fact clause or the file prohibition: %s", prompt)
	}
	fast := BuildLightPrompt(LightPromptRequest{RoleName: "MaClaw", UserText: "hi", ExecutionTask: "fast"})
	if strings.Contains(fast, "web_fetch") {
		t.Fatalf("generic light prompt names web_fetch: %s", fast)
	}
	if !strings.Contains(fast, "一次成功的实时查询之后立即作答") {
		t.Fatalf("generic light prompt lost the one-lookup stop: %s", fast)
	}
}
