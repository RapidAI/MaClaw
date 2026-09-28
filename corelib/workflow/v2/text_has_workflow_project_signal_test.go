package v2

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestCatalogCorroborationUsesTemplatesNotPhrases(t *testing.T) {
	cartoon := "生成一段5分钟长的 葫卢兄弟动画，需要传统动画片风格，故事要有趣。"
	if CatalogCorroboratesWorkflowProject(cartoon, "innovation") {
		t.Fatal("an invented workflow type on a cartoon is not a catalog project")
	}
	if CatalogCorroboratesWorkflowProject(cartoon, "") {
		t.Fatal("a cartoon is not a decisive catalog match on its own")
	}
	if CatalogCorroboratesWorkflowProject("开发一个贪吃蛇", "coding") {
		t.Fatal("a software workflow is not a panel project")
	}
	if !CatalogCorroboratesWorkflowProject("做一份完整的市场调研和商业计划", "coding") {
		t.Fatal("a software type must not hide a panel project the catalog matched on its own")
	}
	if CatalogCorroboratesWorkflowProject("查看审计日志", "") {
		t.Fatal("an audit log is only a weak overlap with a compliance template")
	}
	if CatalogCorroboratesWorkflowProject("生成一段动画的解决方案", "") {
		t.Fatal("a solution for an animation is not a panel project")
	}

	if !CatalogCorroboratesWorkflowProject("帮我写一份研究报告", "") {
		t.Fatal("a research report the catalog names outright must stay a panel project")
	}
	if !CatalogCorroboratesWorkflowProject("帮我写一份研究报告", "product_design") {
		t.Fatal("a mismatched type must not hide a panel project the catalog matched on its own")
	}
	if CatalogCorroboratesWorkflowProject(cartoon, "product_design") {
		t.Fatal("a mismatched type on a cartoon is not a panel project")
	}
	if !CatalogCorroboratesWorkflowProject("做一份完整的市场调研和商业计划", "business_plan") {
		t.Fatal("a business plan the classifier named must stay a panel project")
	}
	if !CatalogCorroboratesWorkflowProject("write a paper", "paper_writing") {
		t.Fatal("a named paper-writing template must be corroborated by the catalog")
	}
	if !CatalogCorroboratesWorkflowProject("这个位次能报哪些中外合办学校和专业", "gaokao_application") {
		t.Fatal("a gaokao application the catalog describes must be corroborated")
	}
	if CatalogCorroboratesWorkflowProject("生成一段志愿者动画，故事要能报告他们的日常", "gaokao_application") {
		t.Fatal("a volunteer cartoon does not corroborate the gaokao template")
	}
}

func TestACartoonDoesNotCorroborateAnyWorkflowTaskType(t *testing.T) {
	cartoon := "生成一段5分钟长的 葫卢兄弟动画，需要传统动画片风格，故事要有趣。"
	for _, def := range intent.DefaultDefinitions() {
		if def.Label != intent.LabelWorkflowTask {
			continue
		}
		if len(def.WorkflowTypes) == 0 {
			t.Fatal("workflow_task has no types to check")
		}
		for _, workflowType := range def.WorkflowTypes {
			if CatalogCorroboratesWorkflowProject(cartoon, workflowType) {
				t.Fatalf("workflow type %q corroborated a cartoon", workflowType)
			}
		}
	}
}
