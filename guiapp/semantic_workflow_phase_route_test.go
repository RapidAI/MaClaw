package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func workflowPhaseClassification(labels ...intent.IntentLabel) intent.ClassificationResult {
	result := intent.ClassificationResult{Confidence: .98}
	if len(labels) > 0 {
		result.Primary = labels[0]
		result.Secondary = labels[1:]
	}
	return result
}

// The defect this exists for: a workflow phase carries the phase text, the phase
// text describes the project, and the classifier reads that back as
// workflow_task. Before the trim, the phase was refused as an unserved
// capability — a workflow aborting itself, mid-run, for still sounding like
// itself.
func TestAWorkflowPhaseIsNotRefusedForSoundingLikeAWorkflow(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	ctx := withSemanticWorkflowLoop(context.Background(), true)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "做一份完整的市场调研和商业计划", "desktop", "root-phase", "turn-phase",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err != nil {
		t.Fatalf("a workflow phase was refused by the semantic gate: %v", err)
	}
	if handled {
		t.Fatal("a workflow-task-only phase must fall through to the pipeline that already serves phases, not be claimed by the managed surface")
	}
}

// Only the redundant label is dropped. A phase that is also a coding turn is
// still a coding turn, and must keep the managed surface it earns.
func TestAWorkflowPhaseKeepsTheCapabilityItAlsoClaims(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	trimmed := semanticClassificationForWorkflowLoop(true,
		workflowPhaseClassification(intent.LabelWorkflowTask, intent.LabelCoding))
	if trimmed.Primary != intent.LabelCoding || len(trimmed.Secondary) != 0 {
		t.Fatalf("trim left %v/%v, want coding alone", trimmed.Primary, trimmed.Secondary)
	}
	ctx := withSemanticWorkflowLoop(context.Background(), true)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "把这个函数改一下", "desktop", "root-mixed", "turn-mixed",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask, intent.LabelCoding)), nil,
	)
	if err != nil || !handled {
		t.Fatalf("a coding phase lost its managed surface: handled=%v err=%v", handled, err)
	}
}

// The exemption is scoped to phases. An ordinary chat turn must still be
// refused, or the whole point of not auto-starting workflows from chat is lost.
func TestNamedSkillInvocationFallsThroughLikeTheMainAssistant(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "使用book pdf skill生成书籍", "desktop", "root-skill", "turn-skill",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err != nil {
		t.Fatalf("named skill was refused as a workflow panel start: %v", err)
	}
	if handled {
		t.Fatal("named skill must fall through to the shared agent, not a managed surface")
	}
}

func TestNamedSkillInvocationDoesNotLockOntoGeneratePDF(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "使用book pdf skill生成书籍", "desktop", "root-skill-pdf", "turn-skill-pdf",
		ptrClassification(workflowPhaseClassification(intent.LabelDocumentGenerate)), nil,
	)
	if err != nil {
		t.Fatalf("named skill was intercepted as document_generate: %v", err)
	}
	if handled {
		t.Fatal("named skill must not become a generate_pdf grant")
	}
}

func TestHyphenatedEnglishCompoundStillRefusesWorkflowTask(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "使用 open-source 方法写一份商业计划书", "desktop", "root-compound", "turn-compound",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err == nil {
		t.Fatal("an open-source compound must not be guessed as a named skill")
	}
	if !handled {
		t.Fatal("the refusal fell through to the legacy router")
	}
}

func TestSpacedSkillNameWithoutInstalledSkillDoesNotStartAWorkflow(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "使用book pdf生成书籍", "desktop", "root-spaced", "turn-spaced",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err != nil {
		t.Fatalf("an uncatalogued utterance was refused as a workflow panel start: %v", err)
	}
	if handled {
		t.Fatal("a guessed skill name must not become a managed surface when no skill is installed")
	}
}

func TestNegatedSkillMentionDoesNotStartAWorkflow(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "不要使用 book-pdf skill", "desktop", "root-negated", "turn-negated",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err != nil {
		t.Fatalf("a negated skill mention was refused as a workflow panel start: %v", err)
	}
	if handled {
		t.Fatal("a negated skill mention must not become a managed surface")
	}
}

// Production 2026-09-27: the tree labeled a cartoon the user wanted made in
// this chat as workflow_task (0.75, with a workflow type set) because the
// prompt treated style and a multi-step story as a workflow. The catalog does
// not own that object, and ordinary chat does not auto-start a workflow, so
// the refusal left no path. The invented workflow type must not keep the refusal.
func TestReleaseKeepsASurvivingPrimaryWorkflowType(t *testing.T) {
	in := intent.ClassificationResult{
		Primary:      intent.LabelOffice,
		Secondary:    []intent.IntentLabel{intent.LabelWorkflowTask},
		WorkflowType: "presentation_design",
		Reason:       "tree",
		RunnerUp:     intent.LabelWorkflowTask,
	}
	out := semanticReleaseUncataloguedWorkflowTask("生成一段5分钟长的 葫卢兄弟动画，需要传统动画片风格，故事要有趣。", in)
	if out.Primary != intent.LabelOffice || out.HasLabel(intent.LabelWorkflowTask) {
		t.Fatalf("release left %+v", out)
	}
	if out.WorkflowType != "presentation_design" {
		t.Fatalf("surviving workflow type = %q", out.WorkflowType)
	}
	if out.RunnerUp == intent.LabelWorkflowTask {
		t.Fatal("runner-up workflow_task survived the release")
	}
}

func TestReleaseDoesNotBorrowAnotherLabelsWorkflowType(t *testing.T) {
	in := intent.ClassificationResult{
		Primary:      intent.LabelOffice,
		Secondary:    []intent.IntentLabel{intent.LabelWorkflowTask},
		WorkflowType: "paper_reproduction",
		Reason:       "tree",
	}
	out := semanticReleaseUncataloguedWorkflowTask("生成一段5分钟长的 葫卢兄弟动画，需要传统动画片风格，故事要有趣。", in)
	if out.Primary != intent.LabelOffice || out.HasLabel(intent.LabelWorkflowTask) {
		t.Fatalf("another label's template kept workflow_task: %+v", out)
	}
	if out.WorkflowType != "paper_reproduction" {
		t.Fatalf("surviving workflow type = %q", out.WorkflowType)
	}
}

func TestReleaseClearsWorkflowTypeThatBelongedToTheDroppedLabel(t *testing.T) {
	in := intent.ClassificationResult{
		Primary:      intent.LabelWorkflowTask,
		WorkflowType: "innovation",
		Confidence:   0.75,
		Layer:        3,
	}
	out := semanticReleaseUncataloguedWorkflowTask("生成一段动画", in)
	if out.Primary != "" || out.WorkflowType != "" || out.HasLabel(intent.LabelWorkflowTask) {
		t.Fatalf("release left %+v", out)
	}
}

func TestMediaProductionIsNotRefusedAsAWorkflow(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	classified := workflowPhaseClassification(intent.LabelWorkflowTask)
	classified.WorkflowType = "innovation"
	classified.Confidence = 0.75
	classified.Layer = 3
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1",
		"生成一段5分钟长的 葫卢兄弟动画，需要传统动画片风格，故事要有趣。",
		"desktop", "root-cartoon", "turn-cartoon",
		ptrClassification(classified), nil,
	)
	if err != nil {
		t.Fatalf("a cartoon the catalog does not own was refused as a workflow: %v", err)
	}
	if handled {
		t.Fatal("media production must fall through to the ordinary agent, not a managed workflow surface")
	}
}

func TestACatalogWorkflowProjectIsStillRefusedInChat(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	classified := workflowPhaseClassification(intent.LabelWorkflowTask)
	classified.WorkflowType = "research_report"
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "帮我写一份研究报告", "desktop", "root-report", "turn-report",
		ptrClassification(classified), nil,
	)
	if err == nil {
		t.Fatal("a research report planned in ordinary chat instead of being sent to the workflow entry")
	}
	if !handled {
		t.Fatal("the catalog project refusal fell through to the legacy router")
	}
	resp := semanticHostRejectResponseForPlanError(err)
	if resp == nil || !strings.Contains(resp.Text, "/workflow") {
		t.Fatalf("catalog project refusal = %+v", resp)
	}
}

func TestAnOrdinaryTurnIsStillRefusedForWorkflowTask(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), "user-1", "做一份完整的市场调研和商业计划", "desktop", "root-chat", "turn-chat",
		ptrClassification(workflowPhaseClassification(intent.LabelWorkflowTask)), nil,
	)
	if err == nil {
		t.Fatal("an ordinary workflow_task turn planned instead of being refused")
	}
	if !handled {
		t.Fatal("the refusal fell through to the legacy router")
	}
}

// 2026-09-29: starting a blank paper from the LaTeX template library sends
// this opening line into the LaTeX expert. The tree labeled it workflow_task
// (0.92) with the paper_writing template, the catalog corroborated that type,
// and the turn was refused before the .tex was read.
const latexBlankOpening = "我已经建好一份空白的 LaTeX 论文，源文件是当前工作目录里的 main.tex。先给我一份提纲，然后按章节逐节写，每写完一节就编译确认。"

func latexPaperWritingClassification() intent.ClassificationResult {
	classified := workflowPhaseClassification(intent.LabelWorkflowTask)
	classified.WorkflowType = "paper_writing"
	classified.Confidence = 0.92
	classified.Layer = 3
	return classified
}

func TestLatexExpertDoesNotAbsorbAnotherConfirmedPanel(t *testing.T) {
	owner := expertSessionUserID(builtinLatexExpertID)
	cases := []struct {
		text string
		typ  string
	}{
		{"用 LaTeX 在远程服务器上复现这篇论文的实验，源文件是 main.tex", "paper_reproduction"},
		{"这个位次能报哪些中外合办学校和专业，结果写进 main.tex", "gaokao_application"},
		{"帮我做个合同审查，看有没有合规问题", "contract_review"},
	}
	for _, tc := range cases {
		in := workflowPhaseClassification(intent.LabelWorkflowTask)
		in.WorkflowType = tc.typ
		kept := semanticReleaseLatexExpertWorkflowTask(owner, tc.text, in)
		if !kept.HasLabel(intent.LabelWorkflowTask) || kept.WorkflowType != tc.typ || kept.Primary == intent.LabelFileWrite {
			t.Fatalf("%s was absorbed by the latex expert: %+v", tc.typ, kept)
		}
	}
}

func TestLatexExpertReleaseDropsAPaperFollowUpThatDoesNotNameTheFile(t *testing.T) {
	in := latexPaperWritingClassification()
	in.ToolNames = []string{"generate_pdf"}
	out := semanticReleaseLatexExpertWorkflowTask(expertSessionUserID(builtinLatexExpertID), "继续写下一节", in)
	same := semanticReleaseLatexExpertWorkflowTask(expertSessionUserID(builtinLatexExpertID), latexBlankOpening, in)
	if same.Primary != out.Primary || same.WorkflowType != out.WorkflowType || len(same.ToolNames) != len(out.ToolNames) {
		t.Fatalf("the utterance changed the expert release: %+v vs %+v", out, same)
	}
	if out.HasLabel(intent.LabelWorkflowTask) || out.WorkflowType != "" || out.Primary != intent.LabelFileWrite {
		t.Fatalf("a paper follow-up was not planned as a document edit: %+v", out)
	}
	if len(out.ToolNames) != 0 {
		t.Fatalf("the workflow tool list survived on the document edit: %v", out.ToolNames)
	}
	kept := semanticReleaseLatexExpertWorkflowTask("user-1", "继续写下一节", latexPaperWritingClassification())
	if !kept.HasLabel(intent.LabelWorkflowTask) || kept.WorkflowType != "paper_writing" {
		t.Fatalf("ordinary chat lost the paper workflow: %+v", kept)
	}
}

func TestLatexExpertOpeningDoesNotBecomeAPDF(t *testing.T) {
	in := latexPaperWritingClassification()
	in.Secondary = []intent.IntentLabel{intent.LabelDocumentGenerate}
	in.ToolNames = []string{"generate_pdf"}
	out := semanticReleaseLatexExpertWorkflowTask(expertSessionUserID(builtinLatexExpertID), latexBlankOpening, in)
	if out.Primary != intent.LabelFileWrite || out.HasLabel(intent.LabelDocumentGenerate) || out.HasLabel(intent.LabelWorkflowTask) {
		t.Fatalf("a PDF label survived the blank opening: %+v", out)
	}
	if len(out.ToolNames) != 0 || out.WorkflowType != "" {
		t.Fatalf("PDF tool list survived: %+v", out)
	}
	// The PDF label can also be the one the tree ranked first.
	pdfFirst := workflowPhaseClassification(intent.LabelDocumentGenerate, intent.LabelWorkflowTask)
	pdfFirst.WorkflowType = "paper_writing"
	pdfFirst.ToolNames = []string{"generate_pdf"}
	flipped := semanticReleaseLatexExpertWorkflowTask(expertSessionUserID(builtinLatexExpertID), latexBlankOpening, pdfFirst)
	if flipped.Primary != intent.LabelFileWrite || flipped.HasLabel(intent.LabelDocumentGenerate) || len(flipped.ToolNames) != 0 {
		t.Fatalf("a PDF-first opening stayed a PDF: %+v", flipped)
	}
	// A PDF request that is also a search stays a PDF request.
	mixed := latexPaperWritingClassification()
	mixed.Secondary = []intent.IntentLabel{intent.LabelDocumentGenerate, intent.LabelSearch}
	kept := semanticReleaseLatexExpertWorkflowTask(expertSessionUserID(builtinLatexExpertID), latexBlankOpening, mixed)
	if kept.Primary != intent.LabelDocumentGenerate || !kept.HasLabel(intent.LabelSearch) || kept.HasLabel(intent.LabelWorkflowTask) {
		t.Fatalf("a mixed PDF turn was rewritten: %+v", kept)
	}
}

func TestLatexExpertBlankStartIsNotRefusedAsAWorkflow(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), expertSessionUserID(builtinLatexExpertID), latexBlankOpening,
		"desktop", "root-latex", "turn-latex",
		ptrClassification(latexPaperWritingClassification()), nil,
	)
	if err != nil {
		t.Fatalf("the latex expert blank start was refused: %v", err)
	}
	if !handled || prepared == nil {
		t.Fatal("the latex expert blank start must plan the document edit")
	}
	if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityFSWriteLocal); !ok {
		t.Fatal("the document edit plan has no local file write")
	}
	if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityFSReadLocal); !ok {
		t.Fatal("the document edit plan cannot read the .tex it is supposed to change")
	}
	if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityShellExecuteLocal); !ok {
		t.Fatal("the document edit plan cannot compile")
	}
}

// Openings the template library actually sends, in the three product
// languages, plus a follow-up that shares none of those words. The expert
// plans every one as the document that session already owns. Ordinary chat
// stays open: the blank openings sit in the noise band, and the long English
// template opening only reaches 8.856 by leading its runner-up 1.74×.
func latexPaperOpenings() []string {
	return []string{
		latexBlankOpening,
		"我已經建好一份空白的 LaTeX 論文，原始檔是目前工作目錄裡的 main.tex。先給我一份提綱，然後按章節逐節寫，每寫完一節就編譯確認。",
		"I started a blank LaTeX paper. The document is main.tex in the current working directory. Ask me for an outline first, then write section by section and compile as you go.",
		"我已经用「IEEE 会议模板」模板建好一份 LaTeX 论文。模板包已经解压到当前工作目录，入口文件是 elsarticle-template-num.tex，导言区保留了模板原有的设置。请先读这个相对路径，再把你打算遵循的提纲告诉我，然后再开始写。",
		"我已經用「IEEE」模板建好一份 LaTeX 論文。模板包已經解壓到目前工作目錄，入口檔是 main.tex，前言區保留了模板原有的設定。請先讀這個相對路徑，再把你打算遵循的提綱告訴我，然後再開始寫。",
		"I started a LaTeX paper from the \"IEEE\" template. The package is already unpacked in the current working directory. The entry file is main.tex and it carries the template's preamble. Read that relative path first, then tell me the outline you plan to follow before writing.",
		"LaTeX 文档是 main.tex，已经在当前工作目录里。请先读一遍现有内容，再给修改建议。",
		"LaTeX 文件是 main.tex，已經在目前工作目錄裡。請先讀一遍現有內容，再給修改建議。",
		"The LaTeX document is main.tex, already in the current working directory. Read what is already there before suggesting changes.",
		"继续写下一节",
	}
}

func TestLatexExpertPlansPaperWritingWithoutReadingTheUtterance(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	classified := latexPaperWritingClassification()
	classified.ToolNames = []string{"generate_pdf"}
	for _, text := range latexPaperOpenings() {
		t.Run(text, func(t *testing.T) {
			prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
				context.Background(), expertSessionUserID(builtinLatexExpertID), text, "desktop",
				"root-src", "turn-src", ptrClassification(classified), nil,
			)
			if err != nil || !handled || prepared == nil {
				t.Fatalf("paper writing in the latex expert was refused: handled=%v err=%v", handled, err)
			}
			if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityFSWriteLocal); !ok {
				t.Fatal("the document edit plan has no local file write")
			}
			if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityFSReadLocal); !ok {
				t.Fatal("the document edit plan cannot read the document")
			}
			if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityShellExecuteLocal); !ok {
				t.Fatal("the document edit plan cannot compile")
			}
		})
	}
}

func TestNoiseBandPaperWritingDoesNotCloseChat(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	texts := append(latexPaperOpenings(),
		"write a paper",
		"帮我写一篇论文",
		"幫我寫一篇論文",
		"LaTeX 文档是 main.tex，已经在当前工作目录里。请先读一遍现有内容，再给修改建议。",
	)
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
				context.Background(), "user-1", text, "desktop", "root-noise", "turn-noise",
				ptrClassification(latexPaperWritingClassification()), nil,
			)
			if err != nil || handled {
				t.Fatalf("a noise-band paper_writing closed chat: handled=%v err=%v", handled, err)
			}
		})
	}
}

func TestADecisivePanelProjectStaysAPanelEvenIfItNamesAFile(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	// Measured 2026-09-29: adding main.tex leaves these scores unchanged
	// (gaokao 11.136, paper_reproduction 27.947). A filename scan would have
	// released them.
	cases := []struct {
		text string
		typ  string
	}{
		{"这个位次能报哪些中外合办学校和专业", "gaokao_application"},
		{"这个位次能报哪些中外合办学校和专业，结果写进 main.tex", "gaokao_application"},
		{"用 LaTeX 在远程服务器上复现这篇论文的实验", "paper_reproduction"},
		{"用 LaTeX 在远程服务器上复现这篇论文的实验，源文件是 main.tex", "paper_reproduction"},
		{"帮我做个合同审查，看有没有合规问题", "contract_review"},
	}
	users := []string{"user-1", expertSessionUserID(builtinLatexExpertID)}
	for _, tc := range cases {
		for _, userID := range users {
			t.Run(tc.typ+"/"+userID, func(t *testing.T) {
				classified := workflowPhaseClassification(intent.LabelWorkflowTask)
				classified.WorkflowType = tc.typ
				classified.Confidence = 0.92
				classified.Layer = 3
				_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
					context.Background(), userID, tc.text, "desktop", "root-panel", "turn-panel",
					ptrClassification(classified), nil,
				)
				if err == nil || !handled {
					t.Fatalf("decisive project was not refused: handled=%v err=%v", handled, err)
				}
			})
		}
	}
}

func TestLatexExpertReusedDocumentWithoutATypeIsAFileEdit(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	// The file was already on disk, so the library asks for a read and a
	// suggestion. A phase chain is not searchable, so neither an empty type
	// nor an explicit contract_review label closes the expert.
	texts := []string{
		latexBlankOpening,
		"LaTeX 文档是 main.tex，已经在当前工作目录里。请先读一遍现有内容，再给修改建议。",
		"LaTeX 文件是 main.tex，已經在目前工作目錄裡。請先讀一遍現有內容，再給修改建議。",
	}
	labeled := workflowPhaseClassification(intent.LabelWorkflowTask)
	labeled.WorkflowType = "contract_review"
	labeled.Confidence = 0.92
	labeled.Layer = 3
	classified := workflowPhaseClassification(intent.LabelWorkflowTask)
	classified.Confidence = 0.92
	classified.Layer = 3
	for _, text := range texts {
		for _, in := range []intent.ClassificationResult{classified, labeled} {
			in := in
			t.Run(in.WorkflowType+"/"+text, func(t *testing.T) {
				prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
					context.Background(), expertSessionUserID(builtinLatexExpertID), text, "desktop",
					"root-reuse", "turn-reuse", ptrClassification(in), nil,
				)
				if err != nil || !handled || prepared == nil {
					t.Fatalf("reused document was refused: handled=%v err=%v", handled, err)
				}
				if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityFSWriteLocal); !ok {
					t.Fatal("the reused document has no file write")
				}
				if _, ok := semanticSelectionForCapability(prepared.plan, tool.CapabilityFSReadLocal); !ok {
					t.Fatal("the reused document cannot be read")
				}
			})
		}
	}
}

func TestLatexExpertUnnamedDecisiveProjectStaysAPanel(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	// No workflow type. The catalog winner is still that project, so the
	// paper expert must not turn it into a document edit.
	texts := []string{
		"这个位次能报哪些中外合办学校和专业",
		"用 LaTeX 在远程服务器上复现这篇论文的实验",
	}
	classified := workflowPhaseClassification(intent.LabelWorkflowTask)
	classified.Confidence = 0.92
	classified.Layer = 3
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
				context.Background(), expertSessionUserID(builtinLatexExpertID), text, "desktop",
				"root-unnamed", "turn-unnamed", ptrClassification(classified), nil,
			)
			if err == nil || !handled {
				t.Fatalf("an unnamed decisive project was absorbed: handled=%v err=%v", handled, err)
			}
		})
	}
}

func TestLatexExpertStillRefusesADifferentPanelProject(t *testing.T) {
	h := semanticCodingHandler(t, intent.LabelCoding)
	classified := workflowPhaseClassification(intent.LabelWorkflowTask)
	classified.WorkflowType = "gaokao_application"
	_, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		context.Background(), expertSessionUserID(builtinLatexExpertID), "这个位次能报哪些中外合办学校和专业",
		"desktop", "root-latex-gaokao", "turn-latex-gaokao",
		ptrClassification(classified), nil,
	)
	if err == nil {
		t.Fatal("a gaokao application in the latex expert planned instead of being sent to the workflow entry")
	}
	if !handled {
		t.Fatal("the gaokao refusal fell through to the legacy router")
	}
}

// What the user is told when that refusal happens. "The capability catalog does
// not cover this request" describes an internal migration state and hides a
// route that exists, so the refusal must name the way in.
func TestTheWorkflowRefusalPointsAtTheWayIn(t *testing.T) {
	resp := semanticHostRejectResponseForPlanError(
		semanticUnmappedCapabilityError{Label: intent.LabelWorkflowTask})
	if resp == nil {
		t.Fatal("no response")
	}
	if !strings.Contains(resp.Text, "工作流") {
		t.Fatalf("refusal text %q never mentions the route it is redirecting to", resp.Text)
	}
	if resp.Error == "semantic_capability_unmet" {
		t.Fatal("a routed refusal must be separable from a genuine catalog gap in telemetry")
	}
}

// The refusal names a slash command, so it is only honest while that command
// is still routed. Mentioning "工作流" would survive the command being renamed
// or dropped, and the user would be sent to type something the dispatcher no
// longer recognizes — a more precise version of the dead end this refusal was
// written to remove.
func TestTheCommandTheWorkflowRefusalNamesIsStillRouted(t *testing.T) {
	resp := semanticHostRejectResponseForPlanError(
		semanticUnmappedCapabilityError{Label: intent.LabelWorkflowTask})
	if resp == nil {
		t.Fatal("no response")
	}
	commands := 0
	for _, field := range strings.Fields(resp.Text) {
		if !strings.HasPrefix(field, "/") {
			continue
		}
		commands++
		if kind := classifyImmediateIMCommand(field); kind == imCommandUnknown {
			t.Fatalf("refusal sends the user to %q, which no command routes", field)
		}
	}
	if commands == 0 {
		t.Fatalf("refusal text %q names no command, so this guard checks nothing", resp.Text)
	}
}

// Every other unmapped family keeps the generic refusal: there is no better
// answer to give, and inventing one per family is how a refusal starts lying.
func TestOtherUnmappedFamiliesKeepTheGenericRefusal(t *testing.T) {
	generic := semanticHostRejectResponse()
	routed := semanticHostRejectResponseForPlanError(
		semanticUnmappedCapabilityError{Label: semanticSyntheticUnmappedLabel})
	if routed.Text != generic.Text || routed.Error != generic.Error {
		t.Fatalf("an unrelated unmapped label got the workflow message: %+v", routed)
	}
	// A non-planning failure (empty surface, missing surface) is not a
	// statement about any family at all.
	if plain := semanticHostRejectResponseForPlanError(nil); plain.Text != generic.Text {
		t.Fatalf("a nil plan error was routed as if it named a family: %+v", plain)
	}
}

func ptrClassification(result intent.ClassificationResult) *intent.ClassificationResult {
	return &result
}
