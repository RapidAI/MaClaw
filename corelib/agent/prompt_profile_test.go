package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestPromptProfileFromTask(t *testing.T) {
	if PromptProfileFromTask(llm.TaskFast) != PromptProfileLight {
		t.Fatal("fast → light")
	}
	if PromptProfileFromTask(llm.TaskReasoning) != PromptProfileFull {
		t.Fatal("reasoning → full")
	}
}

func TestBuildPromptBundle_LightSkipsCodingAndSSH(t *testing.T) {
	full := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:      "MaClaw",
			IsProMode:     true,
			PromptProfile: PromptProfileFull,
		},
	}, "hello", true)
	light := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:      "MaClaw",
			IsProMode:     true,
			PromptProfile: PromptProfileLight,
		},
	}, "hello", true)

	if !strings.Contains(full, "CodingSubAgent") {
		t.Fatal("full should include coding workflow")
	}
	if strings.Contains(light, "CodingSubAgent") {
		t.Fatal("light should omit coding workflow")
	}
	if !strings.Contains(full, "SSH") {
		t.Fatal("full should include SSH rules")
	}
	if strings.Contains(light, "SSH 远程服务器") {
		t.Fatal("light should omit SSH rules block")
	}
	if !strings.Contains(light, "轻量 turn") && !strings.Contains(light, "low-complexity") {
		t.Fatalf("light should use light principles, got len=%d", len(light))
	}
	if strings.Contains(light, "web_search / web_fetch") {
		t.Fatal("light principles must not advertise web_fetch")
	}
	if !strings.Contains(light, "web_search") || !strings.Contains(light, "一次性授权") {
		t.Fatal("light principles must treat listed tool names as the current lookup")
	}
	if !strings.Contains(light, "不要请用户“重新授权工具”") && !strings.Contains(light, "重新授权") {
		t.Fatal("light principles must tell the model not to ask for tool re-authorization")
	}
	if len(light) >= len(full) {
		t.Fatalf("light prompt should be shorter: light=%d full=%d", len(light), len(full))
	}
	if !strings.Contains(full, "台上名称和约束只认") {
		t.Fatal("full should include the working-state contract")
	}
	if strings.Contains(full, WorkingStateMarker) {
		t.Fatal("stable full prompt must not include the spliced working-state marker")
	}
	if strings.Contains(light, "台上名称和约束只认") {
		t.Fatal("light should omit the working-state contract")
	}
}

func TestBuildPromptBundleManagedOmitsSoupSignatures(t *testing.T) {
	managed := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:        "MaClaw",
			IsProMode:       true,
			PromptProfile:   PromptProfileFull,
			ManagedSemantic: true,
		},
		SkillLister: func() []SkillInfo {
			return []SkillInfo{{Name: "local-pdf", Description: "convert PDFs"}}
		},
	}, "南京天气，生成pdf报告", true)
	if !strings.Contains(managed, "不要传 path") || !strings.Contains(managed, "send_to_im(path=") {
		t.Fatalf("managed principles missing bound-delivery warning: %s", managed)
	}
	if strings.Contains(managed, "manage_skill(action=\"run\"") || strings.Contains(managed, "CodingSubAgent") {
		t.Fatalf("managed prompt still teaches soup gateways: %s", managed)
	}
	if strings.Contains(managed, "直接 send_file 该 MP3") {
		t.Fatalf("managed prompt still teaches soup send_file delivery: %s", managed)
	}
	if strings.Contains(managed, "write_file, edit_file, ripgrep") {
		t.Fatalf("managed prompt still names soup file tools as the path-resolution set: %s", managed)
	}
}

func TestBuildPromptBundle_LightManagedOmitsSkillDiscoveryCue(t *testing.T) {
	bundle := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{
			RoleName:        "MaClaw",
			PromptProfile:   PromptProfileLight,
			ManagedSemantic: true,
		},
		SkillLister: func() []SkillInfo {
			return []SkillInfo{{Name: "local-pdf", Description: "convert PDFs"}}
		},
	}, "杭州天气", true)
	if strings.Contains(bundle.RetrievedContext, "manage_skill") || strings.Contains(bundle.RetrievedContext, "Installed Skills") {
		t.Fatalf("light+managed prompt still teaches manage_skill: %s", bundle.RetrievedContext)
	}
}

func TestBuildPromptBundle_LightKeepsSkillDiscoveryCue(t *testing.T) {
	bundle := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileLight},
		SkillLister: func() []SkillInfo {
			return []SkillInfo{{Name: "local-pdf", Description: "convert PDFs"}}
		},
	}, "run local-pdf", true)
	if !strings.Contains(bundle.RetrievedContext, "Installed Skills are available") {
		t.Fatalf("light prompt lost skill discovery cue: %s", bundle.RetrievedContext)
	}
	if strings.Contains(bundle.RetrievedContext, "- local-pdf:") {
		t.Fatalf("light prompt should not include the full skill catalog: %s", bundle.RetrievedContext)
	}
}

func TestPromptProfileFromUserText(t *testing.T) {
	t.Setenv(PromptProfileEnvKey, "")
	p, c := PromptProfileFromUserText("你好", llm.ClassifyHints{})
	if p != PromptProfileLight {
		t.Fatalf("greeting profile=%s classify=%+v", p, c)
	}
	p2, c2 := PromptProfileFromUserText("debug this golang stack trace please", llm.ClassifyHints{})
	if p2 != PromptProfileFull {
		t.Fatalf("coding profile=%s classify=%+v", p2, c2)
	}
}

func TestResolvePromptProfile_EnvOverride(t *testing.T) {
	t.Setenv(PromptProfileEnvKey, "full")
	p, c := ResolvePromptProfile("你好", llm.ClassifyHints{})
	if p != PromptProfileFull {
		t.Fatalf("env full override failed: %s %+v", p, c)
	}
	if !strings.Contains(c.Reason, PromptProfileEnvKey) {
		t.Fatalf("reason=%q", c.Reason)
	}
	t.Setenv(PromptProfileEnvKey, "light")
	p2, _ := ResolvePromptProfile("debug this golang stack trace please", llm.ClassifyHints{})
	if p2 != PromptProfileLight {
		t.Fatalf("env light override failed: %s", p2)
	}
	t.Setenv(PromptProfileEnvKey, "")
	p3, _ := ResolvePromptProfile("你好", llm.ClassifyHints{})
	if p3 != PromptProfileLight {
		t.Fatalf("auto classify after clear: %s", p3)
	}
}

func TestResolvePromptProfile_SoftFullAgentUpgrade(t *testing.T) {
	t.Setenv(PromptProfileEnvKey, "")
	t.Setenv(PromptABPercentEnvKey, "")
	ResetPromptProfileStatsForTest()
	// Bare shell command would otherwise be short/fast → light.
	p, c := ResolvePromptProfile("pwd", llm.ClassifyHints{})
	if p != PromptProfileFull {
		t.Fatalf("pwd should upgrade to full, got %s reason=%s", p, c.Reason)
	}
	if !strings.Contains(c.Reason, "soft full-agent") {
		t.Fatalf("reason=%q", c.Reason)
	}
	st := GetPromptProfileStats()
	if st.LightUpgrades < 1 {
		t.Fatalf("expected light_upgrade count: %+v", st)
	}
}

func TestResolvePromptProfile_QualityABSample(t *testing.T) {
	t.Setenv(PromptProfileEnvKey, "")
	t.Setenv(PromptABPercentEnvKey, "100")
	ResetPromptProfileStatsForTest()
	p, c := ResolvePromptProfile("你好", llm.ClassifyHints{})
	if p != PromptProfileFull {
		t.Fatalf("100%% A/B should force full, got %s", p)
	}
	if !strings.Contains(c.Reason, "quality A/B") {
		t.Fatalf("reason=%q", c.Reason)
	}
	st := GetPromptProfileStats()
	if st.AbEligibleLight < 1 || st.AbSampleFull < 1 {
		t.Fatalf("ab stats=%+v", st)
	}
	// Greeting without A/B stays light.
	t.Setenv(PromptABPercentEnvKey, "0")
	ResetPromptProfileStatsForTest()
	p2, _ := ResolvePromptProfile("你好", llm.ClassifyHints{})
	if p2 != PromptProfileLight {
		t.Fatalf("ab off should stay light, got %s", p2)
	}
}

func TestShouldABSampleFull_Sticky(t *testing.T) {
	t.Setenv(PromptABPercentEnvKey, "50")
	a := ShouldABSampleFull("same-question-xyz")
	b := ShouldABSampleFull("same-question-xyz")
	if a != b {
		t.Fatal("sticky sample should be stable for same text")
	}
}

func TestPromptCorePrinciplesLookupDoesNotMandateWarehouseFirst(t *testing.T) {
	if strings.Contains(PromptCorePrinciples, "必须先调用当前工具列表中的记忆检索") {
		t.Fatal("full principles must not mandate warehouse-first on lookup turns")
	}
	if !strings.Contains(PromptCorePrinciples, "仅当本轮工具列表里有记忆检索或知识库检索") {
		t.Fatal("full principles should gate warehouse pull on the current tool list")
	}
	if !strings.Contains(PromptCorePrinciples, "查找类问题") || !strings.Contains(PromptCorePrinciples, "不要先搜知识库") {
		t.Fatal("full principles should keep lookup evidence order")
	}
}
