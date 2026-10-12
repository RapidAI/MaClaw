package agent

import (
	"strings"
	"testing"
)

// TestPromptVisualizationRulesRoutesEachDiagramType pins that every taught
// diagram type is presented as an AVAILABLE CHOICE, not merely mentioned.
//
// An earlier version asserted `strings.Contains(rules, "pie")` for each type,
// which was a false guard: replacing the routing line
// 「- 占比分布 → pie」 with 「- pie 这个图型暂不支持」 kept the substring present
// while inverting the meaning, and the test still passed. Substring checks can
// only prove the word survived, never that the model may still use the form.
//
// So each type must appear on a line that maps a use case to it (the arrow) and
// must not be negated on that line.
func TestPromptVisualizationRulesRoutesEachDiagramType(t *testing.T) {
	types := []string{"flowchart", "sequenceDiagram", "erDiagram", "stateDiagram-v2", "gantt", "mindmap", "pie"}
	for _, diagram := range types {
		routed := false
		for _, line := range strings.Split(PromptVisualizationRules, "\n") {
			if !strings.Contains(line, diagram) {
				continue
			}
			// A routing line is "use case → type": the arrow precedes the type.
			// (The erDiagram line is both: it routes the type and warns about
			// the alias form, so the type must also precede that warning text.)
			if arrow := strings.Index(line, "→"); arrow >= 0 {
				if idx := strings.Index(line, diagram); idx > arrow {
					routed = true
				}
			}
			// An explicit "type 暂不支持" line disables the type outright.
			if strings.Contains(line, diagram+" ") && strings.Contains(line, "暂不支持") {
				routed = false
			}
		}
		if !routed {
			t.Fatalf("visualization rules must present %s as an available choice (a use-case → type line)", diagram)
		}
	}
	// The block must still be able to route C4 somewhere.
	if !strings.Contains(PromptVisualizationRules, "C4Context") {
		t.Fatal("architecture blueprints should route to C4Context")
	}
}

// TestPromptVisualizationRulesCoversSyntaxHazards pins the per-diagram-type
// escape hatches recorded in guiapp/frontend/src/components/ai/__tests__/
// mermaidTaughtForms.test.ts. Every claim here was verified with mermaid.parse
// on mermaid 11.14 — two earlier drafts asserted forms that do NOT parse
// (erDiagram entity aliases) or wrongly forbade supported forms (mindmap
// quoted labels). A wrong hint makes the model emit a fence that degrades to
// raw source, so these are behavioural constraints, not just wording.
func TestPromptVisualizationRulesCoversSyntaxHazards(t *testing.T) {
	if !strings.Contains(PromptVisualizationRules, "不含空格的 ASCII 标识符") {
		t.Fatal("erDiagram guidance must state the entity-name constraint")
	}
	if !strings.Contains(PromptVisualizationRules, "中文等表头写在属性块内逐行声明") {
		t.Fatal("erDiagram guidance must tell the model where localized column names go")
	}
	if !strings.Contains(PromptVisualizationRules, "关系标签可本地化") {
		t.Fatal("erDiagram guidance must allow localized labels on relationships")
	}
	if !strings.Contains(PromptVisualizationRules, "别名写法无法解析") {
		t.Fatal("erDiagram guidance must reject the entity-alias form, which does not parse")
	}
	if !strings.Contains(PromptVisualizationRules, "与语言无关") {
		t.Fatal("visualization rules must say the syntax constraints are language-neutral")
	}
}

// TestPromptVisualizationRulesHasNoUnverifiedSyntaxClaims guards against the
// mistake this block shipped with twice: asserting a parser behaviour nobody
// measured. Every specific syntax claim here must have a matching case in
// guiapp/frontend/src/components/ai/__tests__/mermaidTaughtForms.test.ts.
//
// Removed example: the block used to warn that round brackets break mindmap
// node text. Probing the real parser showed `分支(A)` parses fine, so the
// warning was false and taught the model a non-existent restriction.
func TestPromptVisualizationRulesHasNoUnverifiedSyntaxClaims(t *testing.T) {
	if strings.Contains(PromptVisualizationRules, "圆括号") {
		t.Fatal("no round-bracket restriction may be asserted: it was disproved against the real parser")
	}
}

// TestPromptVisualizationRulesDefersToPhaseInstructions protects the patent
// figure workflow (corelib/workflow/v2/phase_prompt.go): that phase prompt
// mandates on-disk black-and-white line art and forbids inline mermaid/SVG
// source. A global "default to a mermaid fence" rule must yield to it.
func TestPromptVisualizationRulesDefersToPhaseInstructions(t *testing.T) {
	if !strings.Contains(PromptVisualizationRules, "以那条更具体的指令为准") {
		t.Fatal("full visualization rules must defer to more specific phase instructions")
	}
	if !strings.Contains(PromptVisualizationRulesLight, "以该指令为准") {
		t.Fatal("light visualization rules must defer to phase instructions too")
	}
}

func TestBuildPromptBundle_ProfilesCarryVisualizationRules(t *testing.T) {
	full := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", IsProMode: true, PromptProfile: PromptProfileFull, DiagramRenderingSupported: true},
	}, "解释一下这个系统的架构", true)
	if !strings.Contains(full, "讲解配图（默认执行）") {
		t.Fatal("full prompt should include the visualization rules")
	}
	if !strings.Contains(full, "```mermaid") {
		t.Fatal("full prompt should teach the mermaid fence")
	}

	light := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", IsProMode: true, PromptProfile: PromptProfileLight, DiagramRenderingSupported: true},
	}, "hello", true)
	if !strings.Contains(light, "讲解配图") {
		t.Fatal("light prompt should keep a compact visualization cue")
	}
	if strings.Contains(light, "图型选择（按讲解对象选") {
		t.Fatal("light prompt must not carry the full diagram-type routing table")
	}

	managed := BuildSystemPrompt(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileFull, ManagedSemantic: true, DiagramRenderingSupported: true},
	}, "南京天气，生成pdf报告", true)
	if !strings.Contains(managed, "讲解配图") {
		t.Fatal("managed prompt should keep a compact visualization cue")
	}
	if strings.Contains(managed, "图型选择（按讲解对象选") {
		t.Fatal("managed prompt must not carry the full diagram-type routing table")
	}
}

// TestBuildPromptBundle_OmitsVisualizationRulesWithoutSurfaceSupport is the
// fail-closed half of the contract: a host that never declared diagram
// rendering (terminal TUI, hub bots answering into IM platforms, code-fence-only
// surfaces) must not be told to emit mermaid, or replies degrade into raw
// source nobody can draw. The zero value of the flag means "no diagrams" —
// never "maybe".
func TestBuildPromptBundle_OmitsVisualizationRulesWithoutSurfaceSupport(t *testing.T) {
	profiles := []struct {
		name string
		cfg  SystemPromptConfig
	}{
		{"full", SystemPromptConfig{RoleName: "MaClaw", IsProMode: true, PromptProfile: PromptProfileFull}},
		{"light", SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileLight}},
		{"managed", SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileFull, ManagedSemantic: true}},
	}
	for _, profile := range profiles {
		prompt := BuildSystemPrompt(SystemPromptDeps{Config: profile.cfg}, "解释一下这个系统的架构", true)
		if strings.Contains(prompt, "讲解配图") {
			t.Fatalf("%s prompt must omit the visualization cue when the surface cannot render diagrams", profile.name)
		}
		if strings.Contains(prompt, "```mermaid") {
			t.Fatalf("%s prompt must not teach the mermaid fence when the surface cannot render diagrams", profile.name)
		}
	}
}

// TestBuildPromptBundle_VisualizationKeepsLightCheap guards the cost-sensitive
// light profile: the compact cue must stay a small fraction of the full block.
func TestBuildPromptBundle_VisualizationKeepsLightCheap(t *testing.T) {
	if len(PromptVisualizationRulesLight) > len(PromptVisualizationRules)/3 {
		t.Fatalf("light visualization cue too large: light=%d full=%d", len(PromptVisualizationRulesLight), len(PromptVisualizationRules))
	}
}
