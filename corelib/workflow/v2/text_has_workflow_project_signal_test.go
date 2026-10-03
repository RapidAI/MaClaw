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
	if CatalogCorroboratesWorkflowProject("帮我写一份研究报告", "product_design") {
		t.Fatal("a named type that is not the winner does not borrow the catalog hit")
	}
	if CatalogCorroboratesWorkflowProject(cartoon, "product_design") {
		t.Fatal("a mismatched type on a cartoon is not a panel project")
	}
	if !CatalogCorroboratesWorkflowProject("做一份完整的市场调研和商业计划", "business_plan") {
		t.Fatal("a business plan the classifier named must stay a panel project")
	}
	if CatalogCorroboratesWorkflowProject("write a paper", "paper_writing") {
		t.Fatal("paper_writing in the noise band must not close chat; the choice panel keeps the lower floor")
	}
	if !CatalogCorroboratesWorkflowProject("这个位次能报哪些中外合办学校和专业", "gaokao_application") {
		t.Fatal("a gaokao application the catalog describes must be corroborated")
	}
	if CatalogCorroboratesWorkflowProject("生成一段志愿者动画，故事要能报告他们的日常", "gaokao_application") {
		t.Fatal("a volunteer cartoon does not corroborate the gaokao template")
	}
}

func TestANoiseBandNamedTypeDoesNotCloseChat(t *testing.T) {
	// Blank openings and "suggest edits" sit in the noise band. A description
	// that is only a phase chain is not searchable, so 修改建议 does not make
	// contract review the winner. The decisive projects below still close
	// chat, and a filename does not move them off that decision.
	noise := []struct {
		text string
		typ  string
	}{
		{"我已经建好一份空白的 LaTeX 论文，源文件是当前工作目录里的 main.tex。先给我一份提纲，然后按章节逐节写，每写完一节就编译确认。", "paper_writing"},
		{"我已經建好一份空白的 LaTeX 論文，原始檔是目前工作目錄裡的 main.tex。先給我一份提綱，然後按章節逐節寫，每寫完一節就編譯確認。", "paper_writing"},
		{"I started a blank LaTeX paper. The document is main.tex in the current working directory. Ask me for an outline first, then write section by section and compile as you go.", "paper_writing"},
		{"write a paper", "paper_writing"},
		{"I started a LaTeX paper from the \"IEEE\" template. The package is already unpacked in the current working directory. The entry file is main.tex and it carries the template's preamble. Read that relative path first, then tell me the outline you plan to follow before writing.", "paper_writing"},
		{"LaTeX 文档是 main.tex，已经在当前工作目录里。请先读一遍现有内容，再给修改建议。", "paper_writing"},
		{"LaTeX 文件是 main.tex，已經在目前工作目錄裡。請先讀一遍現有內容，再給修改建議。", "paper_writing"},
		// The type id contract_review is not the project name. These sentences
		// say "review" and "contract", which is the id, not 合同审查.
		{"please review this contract", ""},
		{"please review this contract", "contract_review"},
		{"review the contract and suggest changes", ""},
		{"review the contract and suggest changes", "contract_review"},
		{"介绍一下厦门大学马来西亚分校", ""},
		{"介绍一下厦门大学马来西亚分校", "gaokao_application"},
		{"这个基金项目能不能保底", ""},
		{"这个基金项目能不能保底", "nsfc_youth"},
		{"查看审计日志", ""},
		{"介紹一下廈門大學馬來西亞分校", ""},
		{"介紹一下廈門大學馬來西亞分校", "gaokao_application"},
		{"這個基金項目能不能保底", ""},
		{"把测试计划里的日期改一下", ""},
		{"把测试计划里的日期改一下", "testing"},
		{"把測試計劃裡的日期改一下", "testing"},
	}
	for _, tc := range noise {
		if CatalogCorroboratesWorkflowProject(tc.text, tc.typ) {
			t.Fatalf("noise-band %s closed chat: %s", tc.typ, tc.text)
		}
	}
	decisive := []struct {
		text string
		typ  string
	}{
		{"这个位次能报哪些中外合办学校和专业，结果写进 main.tex", "gaokao_application"},
		{"這個位次能報哪些中外合辦學校和專業", "gaokao_application"},
		{"用 LaTeX 在远程服务器上复现这篇论文的实验，源文件是 main.tex", "paper_reproduction"},
		{"用 LaTeX 在遠程服務器上復現這篇論文的實驗", "paper_reproduction"},
		{"帮我做个竞品分析", "competitive_analysis"},
		{"幫我做個競品分析", "competitive_analysis"},
		{"帮我写一份研究报告", "research_report"},
		{"幫我寫一份研究報告", "research_report"},
		{"請審查這份合同，指出條款風險並給出修改建議", "contract_review"},
	}
	for _, tc := range decisive {
		if !CatalogCorroboratesWorkflowProject(tc.text, tc.typ) {
			t.Fatalf("decisive %s was dropped: %s", tc.typ, tc.text)
		}
	}
	// 修改建议 is a contract-review phase step. The search document keeps the
	// template name and drops a description that is only the phase chain, so
	// "suggest edits" does not close chat in either Chinese script.
	for _, reused := range []string{
		"LaTeX 文档是 main.tex，已经在当前工作目录里。请先读一遍现有内容，再给修改建议。",
		"LaTeX 文件是 main.tex，已經在目前工作目錄裡。請先讀一遍現有內容，再給修改建議。",
	} {
		if CatalogCorroboratesWorkflowProject(reused, "") || CatalogCorroboratesWorkflowProject(reused, "contract_review") {
			t.Fatalf("the reused-document opening closed chat on a phase step: %s", reused)
		}
		if CatalogDecisiveWinner(reused) != "" {
			t.Fatalf("the reused-document opening still has a decisive winner: %s", reused)
		}
	}
	realReview := "帮我做个合同审查，看有没有合规问题"
	if !CatalogCorroboratesWorkflowProject(realReview, "contract_review") || CatalogDecisiveWinner(realReview) != "contract_review" {
		t.Fatal("a contract review that names the work was dropped")
	}
	if CatalogDecisiveWinner("这个位次能报哪些中外合办学校和专业") != "gaokao_application" {
		t.Fatal("an unnamed gaokao sentence must still name its winner")
	}
	blank := "我已经建好一份空白的 LaTeX 论文，源文件是当前工作目录里的 main.tex。先给我一份提纲，然后按章节逐节写，每写完一节就编译确认。"
	if CatalogDecisiveWinner(blank) != "" {
		t.Fatal("the blank opening has no decisive winner")
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
