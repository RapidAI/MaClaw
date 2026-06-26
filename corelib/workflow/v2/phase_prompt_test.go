package v2

import (
	"strings"
	"testing"
)

func TestFormDataContainsFilePath(t *testing.T) {
	cases := []struct {
		name   string
		data   map[string]interface{}
		expect bool
	}{
		{"windows path in path field", map[string]interface{}{"material_path": `D:\申报材料\申请书.pdf`}, true},
		{"windows path in any field", map[string]interface{}{"notes": `C:\Users\test\doc.docx`}, true},
		{"forward slash windows path", map[string]interface{}{"file": "E:/docs/report.pdf"}, true},
		{"unix absolute path with depth", map[string]interface{}{"input": "/home/user/thesis.pdf"}, true},
		{"unix root only slash", map[string]interface{}{"notes": "/yes"}, false},
		{"short unix no depth", map[string]interface{}{"notes": "/tmp"}, false},
		{"extension .pdf suffix", map[string]interface{}{"doc": "report.pdf"}, true},
		{"extension .docx suffix", map[string]interface{}{"doc": "合同.docx"}, true},
		{"extension .doc suffix", map[string]interface{}{"doc": "交底书.doc"}, true},
		{"no path just text", map[string]interface{}{"focus_areas": "注意合规性"}, false},
		{"url is not a unix path", map[string]interface{}{"ref": "https://example.com/file.pdf"}, true}, // ends with .pdf → true
		{"url without doc extension", map[string]interface{}{"ref": "https://example.com/page"}, false},
		{"empty form", map[string]interface{}{}, false},
		{"nil form", nil, false},
		{"underscore field skipped", map[string]interface{}{"_agent_view_variant": "file_mode"}, false},
		{"field name path with short value", map[string]interface{}{"path": "ab"}, false},
		{"field name path with value", map[string]interface{}{"path": "abc"}, true},
		{"field name file with value", map[string]interface{}{"disclosure_file": "交底书"}, true},
		{"double slash not unix path", map[string]interface{}{"x": "//server/share"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formDataContainsFilePath(tc.data)
			if got != tc.expect {
				t.Errorf("formDataContainsFilePath(%v) = %v, want %v", tc.data, got, tc.expect)
			}
		})
	}
}

func TestBuildPhasePrompt_InjectsDocParsingGuidance(t *testing.T) {
	state := &WorkflowState{
		Type:    "contract_review",
		Summary: "审查采购合同",
		Phases: []Phase{
			{
				ID:   "parsing",
				Name: "合同解析",
				FormData: map[string]interface{}{
					"contract_path":  `D:\合同\采购合同.pdf`,
					"review_purpose": "签约前风险评估",
				},
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	if !strings.Contains(prompt, "用户提供了文件路径，请根据文件扩展名选择解析方式") {
		t.Error("expected documentParsingGuidance to be injected when form has file path")
	}
	if !strings.Contains(prompt, "pymupdf") {
		t.Error("expected pymupdf parsing method in guidance")
	}
}

func TestBuildPhasePrompt_SkipsGuidanceWhenNoFilePath(t *testing.T) {
	state := &WorkflowState{
		Type:    "due_diligence",
		Summary: "投资尽调",
		Phases: []Phase{
			{
				ID:   "company_profile",
				Name: "公司画像",
				FormData: map[string]interface{}{
					"target_company": "XX科技有限公司",
					"dd_type":        "投资尽调",
				},
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	if strings.Contains(prompt, "用户提供了文件路径，请根据文件扩展名选择解析方式") {
		t.Error("documentParsingGuidance should NOT be injected when no file path in form data")
	}
}

func TestBuildPhasePrompt_PPTGenerationRequiresPPTXArtifactSkill(t *testing.T) {
	state := &WorkflowState{
		Type:        string(WorkflowPresentationDesign),
		Summary:     "制作北京庆祝主题 PPT",
		ProjectPath: `D:\work\presentation`,
		Phases: []Phase{
			{ID: "audience_goal", Name: "受众与目标", Output: "受众：大众；页数：20"},
			{ID: "outline", Name: "内容大纲", Output: "封面、历史、文化、未来"},
			{ID: "slide_scripting", Name: "逐页脚本", Output: "第1页封面；第2页历史"},
			{ID: "ppt_generation", Name: "PPT 生成"},
		},
		CurrentPhase: 3,
	}

	prompt := BuildPhasePrompt(state)
	for _, want := range []string{
		`manage_skill(action="run", name="pptx-generator"`,
		"search_and_install_skill",
		"craft_tool",
		".pptx",
		"send_file",
		"禁止只调用 generate_pdf",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("ppt_generation prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPhasePrompt_GenericArtifactPhaseUsesSemanticGuidance(t *testing.T) {
	state := &WorkflowState{
		Type:        "custom_report_workflow",
		Summary:     "生成最终交付包",
		ProjectPath: `D:\work\artifact`,
		Phases: []Phase{
			{ID: "planning", Name: "规划", Output: "交付一个 zip 包"},
			{
				ID:            "final_package",
				Name:          "最终交付包",
				Kind:          PhaseKindArtifactGeneration,
				MutationScope: MutationScopeArtifact,
			},
		},
		CurrentPhase: 1,
	}

	prompt := BuildPhasePrompt(state)
	for _, want := range []string{
		"通用产物生成阶段指令",
		"manage_skill",
		"search_and_install_skill",
		"craft_tool",
		"send_file",
		"实际文件已生成并发送",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("generic artifact prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestIsArtifactGenerationPhaseDerivesLegacyPresentationSemantics(t *testing.T) {
	if !isArtifactGenerationPhase(string(WorkflowPresentationDesign), &Phase{ID: "ppt_generation"}) {
		t.Fatal("presentation ppt_generation should derive artifact semantics without explicit phase fields")
	}
}

func TestBuildPhasePrompt_SkipsGuidanceForPaDisclosureParsing(t *testing.T) {
	state := &WorkflowState{
		Type:    "patent_application",
		Summary: "撰写发明专利",
		Phases: []Phase{
			{
				ID:   "pa_disclosure_parsing",
				Name: "交底书解析与技术提炼",
				FormData: map[string]interface{}{
					"disclosure_path":     `D:\专利\交底书.docx`,
					"_agent_view_variant": "file_mode",
					"patent_type":         "invention",
					"tech_field":          "人工智能",
				},
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	// pa_disclosure_parsing has its own parsing guidance — shared one should be skipped.
	if strings.Contains(prompt, "## 文档解析方法") {
		t.Error("shared documentParsingGuidance should be SKIPPED for pa_disclosure_parsing (has own)")
	}
	// The phase instruction should still be present.
	if !strings.Contains(prompt, "交底书文件") {
		t.Error("expected pa_disclosure_parsing's own instruction to be present")
	}
}

func TestBuildPhasePrompt_InjectsGuidanceForTechParsing(t *testing.T) {
	state := &WorkflowState{
		Type:    "patent_analysis",
		Summary: "分析侵权风险",
		Phases: []Phase{
			{
				ID:   "tech_parsing",
				Name: "技术解析",
				FormData: map[string]interface{}{
					"material_path": `D:\专利\CN123456A.pdf`,
				},
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	if !strings.Contains(prompt, "用户提供了文件路径，请根据文件扩展名选择解析方式") {
		t.Error("expected documentParsingGuidance to be injected for tech_parsing with file path")
	}
	// Also check the phase-specific instruction is present.
	if !strings.Contains(prompt, "技术方案详解") {
		t.Error("expected tech_parsing phase instruction to be present")
	}
}

func TestPhaseInstructionHasOwnParsingGuidance(t *testing.T) {
	if !phaseInstructionHasOwnParsingGuidance("pa_disclosure_parsing") {
		t.Error("pa_disclosure_parsing should be marked as having own parsing guidance")
	}
	if phaseInstructionHasOwnParsingGuidance("tech_parsing") {
		t.Error("tech_parsing should NOT be marked (uses shared guidance)")
	}
	if phaseInstructionHasOwnParsingGuidance("tender_analysis") {
		t.Error("tender_analysis should NOT be marked")
	}
}

func TestTextContainsFilePath(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		expect bool
	}{
		{"windows path in sentence", "帮我分析 D:\\patents\\CN123.doc 的侵权风险", true},
		{"windows forward slash", "文件在 C:/Users/test/合同.pdf 里", true},
		{"pdf extension mentioned", "帮我审查这个合同.pdf", true},
		{"docx extension mentioned", "交底书.docx 内容需要提炼", true},
		{"no path just text", "帮我分析这个专利的侵权风险", false},
		{"short text", "hi", false},
		{"empty", "", false},
		{"CJK char before drive letter", "分析D:\\专利\\file.pdf", true},
		{"mid-word drive letter", "abcD:\\path should not match", false},
		{"path at end of text", "文件是D:\\x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := textContainsFilePath(tc.text)
			if got != tc.expect {
				t.Errorf("textContainsFilePath(%q) = %v, want %v", tc.text, got, tc.expect)
			}
		})
	}
}

func TestBuildPhasePrompt_InjectsGuidanceFromSummaryWhenNoFormData(t *testing.T) {
	state := &WorkflowState{
		Type:    "patent_analysis",
		Summary: "帮我分析 D:\\专利\\CN202312345A.pdf 的侵权风险",
		Phases: []Phase{
			{
				ID:   "tech_parsing",
				Name: "技术解析",
				// FormData is nil — user didn't use form, typed path in chat.
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	if !strings.Contains(prompt, "用户提供了文件路径，请根据文件扩展名选择解析方式") {
		t.Error("expected documentParsingGuidance when Summary contains a file path but FormData is nil")
	}
}

func TestBuildPhasePrompt_NoGuidanceWhenSummaryHasNoPath(t *testing.T) {
	state := &WorkflowState{
		Type:    "patent_analysis",
		Summary: "帮我分析这个专利的侵权风险",
		Phases: []Phase{
			{
				ID:   "tech_parsing",
				Name: "技术解析",
				// FormData is nil, Summary has no path.
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	// The shared guidance block starts with "## 文档解析方法\n\n用户提供了文件路径".
	// The phase instruction may reference "文档解析方法" but the full block should not be present.
	if strings.Contains(prompt, "用户提供了文件路径，请根据文件扩展名选择解析方式") {
		t.Error("documentParsingGuidance block should NOT be injected when neither FormData nor Summary has a file path")
	}
}
