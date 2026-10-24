package agent

import (
	"strings"
	"testing"
)

func TestWritingGuidanceForWritingTasks(t *testing.T) {
	task := "随机挑一天 HF Daily Papers，找点赞最多的论文，在知乎发论文解读"
	for _, prof := range []string{"", "light"} {
		deps := SystemPromptDeps{}
		deps.Config.PromptProfile = PromptProfile(prof)
		got := BuildPromptBundle(deps, task, true).StableSystemPrompt
		for _, want := range []string{"长文写作格式", "**加粗**", "图注", "来源", "总结"} {
			if !strings.Contains(got, want) {
				t.Fatalf("profile %q: guidance missing %q", prof, want)
			}
		}
	}
	deps := SystemPromptDeps{}
	deps.Config.PromptProfile = PromptProfile("light")
	if strings.Contains(BuildPromptBundle(deps, "北京今天几度", true).StableSystemPrompt, "长文写作格式") {
		t.Fatal("light lookup turn got the writing guidance")
	}
	if !promptIsWritingTask("Write a review of this paper") || promptIsWritingTask("what time is it") {
		t.Fatal("writing task detection")
	}
}
