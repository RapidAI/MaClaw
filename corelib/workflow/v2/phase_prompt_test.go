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
		{"https url ending .pdf is not a local path", map[string]interface{}{"ref": "https://example.com/file.pdf"}, false},
		{"url without doc extension", map[string]interface{}{"ref": "https://example.com/page"}, false},
		{"tender_standard_url only", map[string]interface{}{"tender_standard_url": "https://example.com/tender/xxx"}, false},
		{"url in path-named field still not local", map[string]interface{}{"tender_standard_path": "https://cdn.example.com/a.pdf"}, false},
		{"scheme-less host path not local", map[string]interface{}{"tender_standard_path": "example.com/tender/a.pdf"}, false},
		{"relative folder path still local", map[string]interface{}{"prepared_bid_path": "docs/bid.pdf"}, true},
		{"mixed url and local path", map[string]interface{}{"tender_standard_url": "https://example.com/tender", "prepared_bid_path": `D:\投标\标书.pdf`}, true},
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
	if !strings.Contains(prompt, "按下面**优先级阶梯**读取") {
		t.Error("expected documentParsingGuidance to be injected when form has file path")
	}
	if !strings.Contains(prompt, "read_document") {
		t.Error("expected office read_document guidance")
	}
	if !strings.Contains(prompt, "craft_tool") {
		t.Error("expected craft_tool fallback guidance for other formats")
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
	if strings.Contains(prompt, "按下面**优先级阶梯**读取") {
		t.Error("documentParsingGuidance should NOT be injected when no file path in form data")
	}
}

func TestRenderFormDataFields_MasksSensitiveSchemaFields(t *testing.T) {
	phase := &Phase{
		InputSchema: &PhaseInputSchema{
			Fields: []PhaseInputField{
				{Name: "ssh_user", Label: "用户名", Type: "text"},
				{Name: "ssh_password", Label: "密码", Type: "text", Sensitive: true},
				{Name: "project_description", Label: "项目描述", Type: "textarea"},
			},
		},
		FormData: map[string]interface{}{
			"ssh_user":            "root",
			"ssh_password":        "super-secret",
			"project_description": "修复登录接口",
		},
	}

	rendered := RenderFormDataFields(phase, true)
	if strings.Contains(rendered, "super-secret") {
		t.Fatalf("sensitive field value leaked into rendered form data:\n%s", rendered)
	}
	for _, want := range []string{
		"- **用户名**：root",
		"- **密码**：已填写（敏感信息已隐藏）",
		"- **项目描述**：修复登录接口",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered form data missing %q:\n%s", want, rendered)
		}
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

func TestBuildPhasePrompt_FullDependencyUsesAuthoritativeOutput(t *testing.T) {
	outline := "# Outline\n" + strings.Repeat("slide-detail ", 700)
	state := &WorkflowState{
		Type: string(WorkflowPresentationDesign),
		Phases: []Phase{
			{ID: "outline", Name: "Outline", Output: outline},
			{ID: "slide_scripting", Name: "Slide scripting", DependsOnFull: []string{"outline"}},
		},
		CurrentPhase: 1,
	}

	prompt := BuildPhasePrompt(state)
	if !strings.Contains(prompt, outline) {
		t.Fatal("full dependency was not injected into the next-phase prompt")
	}
	if strings.Contains(prompt, "read_file") || strings.Contains(prompt, ".maclaw/workflow/") {
		t.Fatalf("full dependency must not make the model discover a file:\n%s", prompt)
	}
}

func TestPresentationTemplate_SlideScriptingDependsOnOutline(t *testing.T) {
	tmpl := PresentationTemplate()
	for _, phase := range tmpl.Phases {
		if phase.ID != "slide_scripting" {
			continue
		}
		if len(phase.DependsOnFull) != 1 || phase.DependsOnFull[0] != "outline" {
			t.Fatalf("slide_scripting DependsOnFull = %#v, want [outline]", phase.DependsOnFull)
		}
		return
	}
	t.Fatal("slide_scripting phase missing")
}

func TestBuildPhasePrompt_FullDependenciesRespectAggregateBudget(t *testing.T) {
	first := strings.Repeat("a", fullDependencyRuneBudget+100)
	second := strings.Repeat("b", fullDependencyRuneBudget+100)
	state := &WorkflowState{
		Phases: []Phase{
			{ID: "first", Name: "First", Output: first},
			{ID: "second", Name: "Second", Output: second},
			{ID: "final", Name: "Final", DependsOnFull: []string{"first", "second"}},
		},
		CurrentPhase: 2,
	}

	prompt := BuildPhasePrompt(state)
	if strings.Count(prompt, "...(受上下文传输上限截断") != 2 {
		t.Fatalf("full dependencies should each declare a transport truncation:\n%s", prompt)
	}
	if strings.Count(prompt, "a") < fullDependencyRuneBudget || strings.Count(prompt, "b") < fullDependencyRuneBudget {
		t.Fatalf("each dependency should receive its per-dependency budget")
	}
}

func TestBuildPhasePrompt_MissingFullDependencyBlocksGeneration(t *testing.T) {
	state := &WorkflowState{
		Phases: []Phase{
			{ID: "outline", Name: "Outline"},
			{ID: "slide_scripting", Name: "Slide scripting", DependsOnFull: []string{"outline"}},
		},
		CurrentPhase: 1,
	}

	prompt := BuildPhasePrompt(state)
	if !strings.Contains(prompt, "outline") || !strings.Contains(prompt, "前序产出物不可用") {
		t.Fatalf("missing dependency should be explicit:\n%s", prompt)
	}
	if !strings.Contains(prompt, "不要搜索项目目录、PDF、记忆") {
		t.Fatalf("missing dependency prompt must prohibit fallback discovery:\n%s", prompt)
	}
}

func TestMissingFullDependencies(t *testing.T) {
	state := &WorkflowState{
		Phases: []Phase{
			{ID: "outline", Output: "ready"},
			{ID: "style"},
			{ID: "script", DependsOnFull: []string{"style", "outline", "absent"}},
		},
		CurrentPhase: 2,
	}
	if got := strings.Join(MissingFullDependencies(state), ","); got != "absent,style" {
		t.Fatalf("MissingFullDependencies = %q, want absent,style", got)
	}
}

func TestBackfillPhaseDependenciesFromTemplate(t *testing.T) {
	state := &WorkflowState{Phases: []Phase{
		{ID: "outline"},
		{ID: "script"},
		{ID: "final", DependsOnFull: []string{"explicit"}},
	}}
	tmpl := &WorkflowTemplate{Phases: []PhaseTemplate{
		{ID: "script", DependsOnFull: []string{"outline"}},
		{ID: "final", DependsOnFull: []string{"script"}},
	}}
	if !BackfillPhaseDependenciesFromTemplate(state, tmpl) {
		t.Fatal("expected template dependency backfill")
	}
	if got := strings.Join(state.Phases[1].DependsOnFull, ","); got != "outline" {
		t.Fatalf("backfilled dependencies = %q", got)
	}
	if got := strings.Join(state.Phases[2].DependsOnFull, ","); got != "explicit" {
		t.Fatalf("explicit dependencies overwritten: %q", got)
	}
}

func TestBuildPhasePrompt_InheritsPriorPhaseFormData(t *testing.T) {
	state := &WorkflowState{
		Type:    string(WorkflowPresentationDesign),
		Summary: "制作产品发布会 PPT",
		Phases: []Phase{
			{
				ID:   "audience_goal",
				Name: "受众与目标",
				InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{
					{Name: "topic", Label: "主题", Type: "text"},
					{Name: "audience", Label: "目标受众", Type: "text"},
					{Name: "page_count", Label: "期望页数", Type: "text"},
					{Name: "style", Label: "风格偏好", Type: "text"},
				}},
				FormData: map[string]interface{}{
					"topic":      "2026 产品发布会",
					"audience":   "公司高管",
					"page_count": "16 页",
					"style":      "科技商务",
				},
			},
			{ID: "outline", Name: "内容大纲"},
		},
		CurrentPhase: 1,
	}

	prompt := BuildPhasePrompt(state)
	for _, want := range []string{
		"工作流已收集的结构化信息",
		"主题**：2026 产品发布会",
		"目标受众**：公司高管",
		"期望页数**：16 页",
		"风格偏好**：科技商务",
		"禁止重复询问",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt should inherit prior form data; missing %q:\n%s", want, prompt)
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
	if !strings.Contains(prompt, "交底书/申请材料文件") {
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
	if !strings.Contains(prompt, "按下面**优先级阶梯**读取") {
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
	if !phaseInstructionHasOwnParsingGuidance("br_standards") {
		t.Error("br_standards should have own parsing guidance")
	}
	if !phaseInstructionHasOwnParsingGuidance("br_bid_content") {
		t.Error("br_bid_content should have own parsing guidance")
	}
	if phaseInstructionHasOwnParsingGuidance("br_conformity") {
		t.Error("br_conformity should use shared guidance when prior form has local paths")
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
		{"https url with pdf not local path", "请根据 https://example.com/tender/file.pdf 检查标书", false},
		{"scheme-less host path not local", "请根据 example.com/tender/file.pdf 检查标书", false},
		{"url plus local path", "标准 https://example.com/a.pdf 标书 D:\\投标\\标书.docx", true},
		{"bare local pdf name still matches", "帮我审查这个合同.pdf", true},
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
	if !strings.Contains(prompt, "按下面**优先级阶梯**读取") {
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
	// Shared guidance uses the ladder intro; phase instructions may mention
	// "文档解析方法" but must not include the full injected ladder block.
	if strings.Contains(prompt, "按下面**优先级阶梯**读取") {
		t.Error("documentParsingGuidance block should NOT be injected when neither FormData nor Summary has a file path")
	}
}

func TestBuildPhasePrompt_InheritsPriorPhaseFilePathForParsingGuidance(t *testing.T) {
	// File path collected only on intake phase; later phase has empty FormData.
	// Shared doc-parsing guidance must still inject so multi-phase doc workflows
	// (bid_review, contract_review, academic review, …) keep reading files.
	state := &WorkflowState{
		Type:    "bid_review",
		Summary: "检查标书是否符合招标要求",
		Phases: []Phase{
			{
				ID:   "br_standards",
				Name: "招标标准解析",
				FormData: map[string]interface{}{
					"tender_standard_path": `D:\投标\招标文件.pdf`,
					"prepared_bid_path":    `D:\投标\投标文件.pdf`,
				},
				Output: "标准清单已完成",
			},
			{
				ID:   "br_conformity",
				Name: "符合性检查",
				// No FormData on this phase — path lives on phase 0 only.
			},
		},
		CurrentPhase: 1,
	}
	// br_conformity does not declare own parsing guidance → shared ladder should inject.
	prompt := BuildPhasePrompt(state)
	if !strings.Contains(prompt, "按下面**优先级阶梯**读取") {
		t.Error("expected documentParsingGuidance from prior-phase form file paths")
	}
	// Prior form fields should still be visible as inherited structured context.
	if !strings.Contains(prompt, "prepared_bid_path") && !strings.Contains(prompt, `D:\投标\投标文件.pdf`) {
		t.Error("expected prior form file path to appear in inherited structured context")
	}
}

func TestBuildPhasePrompt_BidReviewIntakeSkipsSharedParsingGuidance(t *testing.T) {
	state := &WorkflowState{
		Type:    "bid_review",
		Summary: "标书检查",
		Phases: []Phase{
			{
				ID:   "br_standards",
				Name: "招标标准解析",
				FormData: map[string]interface{}{
					"tender_standard_path": `D:\投标\招标文件.pdf`,
					"prepared_bid_path":    `D:\投标\投标文件.pdf`,
				},
			},
		},
		CurrentPhase: 0,
	}
	prompt := BuildPhasePrompt(state)
	if strings.Contains(prompt, "按下面**优先级阶梯**读取") {
		t.Error("br_standards has own parsing guidance; shared ladder should be skipped")
	}
	if !strings.Contains(prompt, "tender_standard_path") && !strings.Contains(prompt, "web_fetch") {
		t.Error("expected bid-review-specific standards instruction content")
	}
}

func TestPhaseInstruction_BidReviewPhases(t *testing.T) {
	for _, id := range []string{"br_standards", "br_bid_content", "br_conformity", "br_fix_report"} {
		got := phaseInstruction(WorkflowBidReview, id)
		if strings.TrimSpace(got) == "" {
			t.Errorf("phaseInstruction(bid_review, %q) empty", id)
		}
	}
}
