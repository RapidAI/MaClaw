package intent

import (
	"strings"
	"testing"
)

func TestExplicitSkillInvocation(t *testing.T) {
	yes := []string{
		"使用book pdf skill生成书籍",
		"使用book-pdf skill 编写人工智能数学入门教授，先列个大纲",
		"用 book_pdf skill 写一本教程",
		"请用这个技能写书",
		"use the book-pdf skill to generate a book",
		"using the book-pdf skill",
		"调用 book pdf skill",
		"使用 book pdf 技能生成书籍",
		"请帮我使用刚才安装好的那个 book-pdf skill 生成书籍",
		"run the book-pdf skill to generate a book",
		"/skill book-pdf",
	}
	for _, text := range yes {
		if !ExplicitSkillInvocation(text) {
			t.Fatalf("%q should be a named skill invocation", text)
		}
	}
	no := []string{
		"帮我写一份商业计划书",
		"做一份完整的市场调研和商业计划",
		"生成一份PDF文档并发给我",
		"facebook skillful tricks",
		"不用 skill 也能写",
		"不要用 skill，去工作流面板启动",
		"不要使用 book-pdf skill",
		"请勿使用 skill",
		"禁止使用 skill 写书",
		"别用 skill 写书",
		"don't use the book-pdf skill, start a workflow",
		"don’t use the book-pdf skill",
		"cannot use the book-pdf skill for this",
		"don't run the book-pdf skill",
		"shouldn't use the book-pdf skill",
		"run a workflow, don't use the skill",
		"使用沟通技能写一份商业计划书",
		"改进优化下这个技能？",
		"用户 skill 在哪",
		"使用者 skill 权限",
		"运行时 skill 目录",
		"执行力 skill 模型",
		"用于 skill 开发的目录",
		"facebook-pdf 怎么导出",
		"handbook-pdf 在哪",
		"使用 book_pdf 编写教程",
		"使用book-pdf生成书籍",
		"使用 open-source 方法写一份商业计划书",
		"",
	}
	for _, text := range no {
		if ExplicitSkillInvocation(text) {
			t.Fatalf("%q must not count as a named skill invocation", text)
		}
	}
}

func TestReleaseNamedSkillFromWorkflowIntercept(t *testing.T) {
	result := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.84, Layer: 2, Reason: "embedding"}
	ReleaseNamedSkillFromWorkflowIntercept("使用book pdf skill生成书籍", &result)
	if result.Primary != "" || len(result.Secondary) != 0 {
		t.Fatalf("skill invocation kept workflow_task: %+v", result)
	}
	if !strings.Contains(result.Reason, "named skill") {
		t.Fatalf("reason=%q, want the release recorded", result.Reason)
	}

	kept := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.9, Layer: 3}
	ReleaseNamedSkillFromWorkflowIntercept("写一份商业计划书", &kept)
	if kept.Primary != LabelWorkflowTask {
		t.Fatalf("panel-style workflow request lost workflow_task: %+v", kept)
	}

	compound := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.9}
	ReleaseNamedSkillFromWorkflowIntercept("使用 open-source 方法写一份商业计划书", &compound)
	if compound.Primary != LabelWorkflowTask {
		t.Fatalf("hyphenated English compound must not clear a panel start: %+v", compound)
	}

	mixed := ClassificationResult{Primary: LabelWorkflowTask, Secondary: []IntentLabel{LabelCoding}, Confidence: 0.8}
	ReleaseNamedSkillFromWorkflowIntercept("使用 coding skill 改这个函数", &mixed)
	if mixed.Primary != "" || len(mixed.Secondary) != 0 {
		t.Fatalf("named skill must not keep a governed leftover that HostRejects: %+v", mixed)
	}

	generate := ClassificationResult{Primary: LabelDocumentGenerate, Confidence: 0.88, Layer: 2, WorkflowType: "paper_writing", CreationOriented: true}
	ReleaseNamedSkillFromWorkflowIntercept("使用book pdf skill生成书籍", &generate)
	if generate.Primary != "" || generate.WorkflowType != "" || generate.CreationOriented {
		t.Fatalf("named skill must not keep a workflow type or generate grant: %+v", generate)
	}

	leftover := ClassificationResult{CreationOriented: true, ToolNames: []string{"generate_pdf"}}
	ReleaseNamedSkillFromWorkflowIntercept("使用book pdf skill生成书籍", &leftover)
	if leftover.CreationOriented || len(leftover.ToolNames) != 0 {
		t.Fatalf("named skill must clear leftover grant fields: %+v", leftover)
	}
}

func TestReleaseNamedSkillInterceptAgentGuidedInject(t *testing.T) {
	book := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.84, Layer: 2}
	ReleaseNamedSkillIntercept("按书籍级PDF手册全流程写一本入门教程", true, &book)
	if book.Primary != "" {
		t.Fatalf("agent-guided inject must fall through like the main assistant: %+v", book)
	}

	spaced := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.84}
	ReleaseNamedSkillIntercept("使用book pdf生成书籍", true, &spaced)
	if spaced.Primary != "" {
		t.Fatalf("inject must cover the skill name without the word skill: %+v", spaced)
	}

	hyphen := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.84}
	ReleaseNamedSkillIntercept("使用 book_pdf 编写教程", true, &hyphen)
	if hyphen.Primary != "" {
		t.Fatalf("inject must cover underscore skill names: %+v", hyphen)
	}

	negated := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.9}
	ReleaseNamedSkillIntercept("不要用 skill，去工作流面板启动", true, &negated)
	if negated.Primary != LabelWorkflowTask {
		t.Fatalf("negated skill must still HostReject as a panel start: %+v", negated)
	}

	ordinary := ClassificationResult{Primary: LabelWorkflowTask, Confidence: 0.9}
	ReleaseNamedSkillIntercept("写一份商业计划书", false, &ordinary)
	if ordinary.Primary != LabelWorkflowTask {
		t.Fatalf("ordinary workflow_task without inject must stay refused: %+v", ordinary)
	}

	generate := ClassificationResult{Primary: LabelDocumentGenerate, Confidence: 0.88}
	ReleaseNamedSkillIntercept("使用book pdf生成书籍", true, &generate)
	if generate.Primary != "" {
		t.Fatalf("inject must not leave document_generate to steal generate_pdf: %+v", generate)
	}
	if NamedSkillInterceptCandidate(ClassificationResult{Primary: LabelSearch, Confidence: 0.9, ToolNames: []string{"web_search", "web_fetch"}}) {
		t.Fatal("search tools must not trigger an inject scan")
	}
	if NamedSkillInterceptCandidate(ClassificationResult{Primary: LabelCoding, WorkflowType: "coding", CreationOriented: false}) {
		t.Fatal("a coding workflow_type must not trigger an inject scan")
	}
	if !NamedSkillInterceptCandidate(ClassificationResult{Primary: LabelDocumentGenerate, Confidence: 0.88}) {
		t.Fatal("document_generate must consult inject")
	}
	if !NamedSkillInterceptCandidate(ClassificationResult{Primary: LabelSearch, WorkflowType: "business_plan"}) {
		t.Fatal("a workflow_v2 type must consult inject")
	}
}
