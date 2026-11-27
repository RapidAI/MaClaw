package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatCodingRequestUnderstandingTextIsPlainProse(t *testing.T) {
	got := formatCodingRequestUnderstandingText("把现有的控制台贪吃蛇改成图形界面版本，保留原有玩法。")
	if got == "" || strings.Contains(got, "##") || strings.Contains(got, "需求理解") {
		t.Fatalf("streamed understanding must be plain prose, got %q", got)
	}
	var tokens []string
	emitCodingRequestUnderstanding(func(s string) { tokens = append(tokens, s) }, got)
	if len(tokens) != 1 || tokens[0] != got {
		t.Fatalf("emit tokens=%q", tokens)
	}
}

func TestFallbackCodingRequestRestatementDoesNotCopyUser(t *testing.T) {
	user := "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{
		SessionPlan: "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7, ANSI",
		LastSummary: "done snake.cpp console",
	})
	if got == "" {
		t.Fatal("expected restatement")
	}
	if codingRestatementCopiesUser(got, user) {
		t.Fatalf("restatement copied the user command: %q", got)
	}
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("restatement should name the GUI change: %q", got)
	}
	if !strings.Contains(got, "\u8d2a\u5403\u86c7") {
		t.Fatalf("restatement should use session context: %q", got)
	}
}

func TestParseCodingRequestRestatementRejectsCopy(t *testing.T) {
	got := parseCodingRequestRestatement("{\"restatement\":\"\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248\"}")
	if got != "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248" {
		t.Fatalf("parse=%q", got)
	}
	if !codingRestatementCopiesUser(got, "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248") {
		t.Fatal("copy detector missed verbatim restatement")
	}
}

func TestResolveCodingRequestUnderstandingUsesFallbackWithoutLLM(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.resolveCodingRequestUnderstanding("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		LastSummary: "snake.cpp console snake built",
	}, codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true})
	if codingRestatementCopiesUser(got, "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248") {
		t.Fatalf("copied user text: %q", got)
	}
	if got == "" {
		t.Fatal("empty restatement")
	}
}

func TestCodingRequestShouldNotRestateWorkspaceClear(t *testing.T) {
	if codingRequestShouldRestate(codingRequestDecision{Kind: codingRequestImplementation}, "\u6e05\u7a7a\u5f53\u524d\u76ee\u5f55") {
		t.Fatal("workspace clear must stay on the host wipe path")
	}
}

func TestResolveCodingWorkbenchTasksPlansGUIRewriteFollowUp(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	understood := "\u628a\u73b0\u6709\u63a7\u5236\u53f0\u8d2a\u5403\u86c7\u6539\u6210\u56fe\u5f62\u754c\u9762\u7248\u672c"
	h.setStickyCodingRequirementRestatement(userID, understood)
	mem := stickyCodingWorkbenchMemory{
		TurnCount:              1,
		SessionPlan:            "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7",
		LastSummary:            "snake.cpp console done",
		RequirementRestatement: understood,
	}
	tasks, md, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		"\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248",
		"D:/repo",
		mem,
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true},
		nil,
		nil,
	)
	if !planned || len(tasks) < 2 {
		t.Fatalf("GUI rewrite follow-up must plan, planned=%v steps=%d", planned, len(tasks))
	}
	if !strings.Contains(md, "\u63a7\u5236\u53f0") && !strings.Contains(md, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("plan goal should be a restatement, markdown=%q", md)
	}
}

func TestFallbackIgnoresGenericTaskTitle(t *testing.T) {
	user := "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{
		SessionPlan: "\u65b0\u5efa\u672c\u5730\u7f16\u7a0b\u4efb\u52a1-1787267059475988200",
	})
	if strings.Contains(got, "\u65b0\u5efa\u672c\u5730\u7f16\u7a0b\u4efb\u52a1") {
		t.Fatalf("generic task title leaked into restatement: %q", got)
	}
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("rewrite restatement lost the GUI target: %q", got)
	}
}

func TestPublishCodingRequestUnderstandingEmitsBeforeRefine(t *testing.T) {
	h := &IMMessageHandler{}
	var tokens []string
	got := h.publishCodingRequestUnderstanding("", "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		SessionPlan: "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7",
	}, codingRequestDecision{Kind: codingRequestImplementation}, func(s string) { tokens = append(tokens, s) })
	if got == "" || len(tokens) != 1 || tokens[0] != got {
		t.Fatalf("publish tokens=%q got=%q", tokens, got)
	}
	if strings.Contains(got, "##") || strings.Contains(got, "\u9700\u6c42\u7406\u89e3") {
		t.Fatalf("first prose must stay plain: %q", got)
	}
}

func TestJoinCodingUnderstandingAndBody(t *testing.T) {
	got := joinCodingUnderstandingAndBody("keep the snake playable", "## \u9700\u8981\u786e\u8ba4\u6267\u884c\u8ba1\u5212\n\n2 steps")
	if !strings.HasPrefix(got, "keep the snake playable\n\n") {
		t.Fatalf("joined=%q", got)
	}
	if joinCodingUnderstandingAndBody("same", "same\n\nbody") != "same\n\nbody" {
		t.Fatal("prefix body must not be duplicated")
	}
}

func assertOpeningTaskHostAnalysis(t *testing.T, got, ask string) {
	t.Helper()
	ask = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(ask), "。！？!? "))
	if !strings.Contains(got, "第一条指令") || !strings.Contains(got, "「"+ask+"」") || !strings.Contains(got, "开发计划") || !strings.Contains(got, "确认前不改文件") {
		t.Fatalf("opening analysis=%q ask=%q", got, ask)
	}
	if strings.Contains(got, "其它先不动") || strings.Contains(got, "回答「") || strings.Contains(got, "不改源代码") || strings.Contains(got, "只读不改文件") {
		t.Fatalf("opening analysis used another template: %q", got)
	}
}

func TestOpeningTaskFirstCommandIsAnalyzedAndKeptAboveThePlan(t *testing.T) {
	const ask = "开发c++版本的俄罗斯广场"
	impl := codingRequestDecision{Kind: codingRequestImplementation}
	if !codingOpeningTaskCommand(ask, stickyCodingWorkbenchMemory{}) {
		t.Fatal("empty session is the opening command")
	}
	if !codingOpeningTaskNeedsAnalysis(ask, stickyCodingWorkbenchMemory{}, impl) {
		t.Fatal("opening implementation needs an analysis")
	}
	if codingOpeningTaskNeedsAnalysis(ask, stickyCodingWorkbenchMemory{}, codingRequestDecision{Kind: codingRequestInquiry}) {
		t.Fatal("an inquiry keeps its own sentence")
	}
	launch := codingRequestDecision{Kind: codingRequestOperational, Acceptance: codingOperationalAcceptanceLaunch}
	if codingOpeningTaskNeedsAnalysis(ask, stickyCodingWorkbenchMemory{}, launch) {
		t.Fatal("a launch keeps its own sentence")
	}
	command := codingRequestDecision{Kind: codingRequestOperational, Acceptance: codingOperationalAcceptanceCommand}
	if !codingOpeningTaskNeedsAnalysis(ask, stickyCodingWorkbenchMemory{}, command) {
		t.Fatal("a host command on a new task still needs an analysis")
	}
	follow := stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"}
	if codingOpeningTaskCommand("再收紧阈值", follow) {
		t.Fatal("a named follow-up is not the opening command")
	}
	if codingOpeningTaskCommand(ask, stickyCodingWorkbenchMemory{TurnCount: 1}) {
		t.Fatal("a later turn is not the opening command")
	}
	if codingOpeningTaskCommand(ask, stickyCodingWorkbenchMemory{FilesCreated: []string{"main.cpp"}}) {
		t.Fatal("written files end the opening command")
	}
	got := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{}, impl)
	assertOpeningTaskHostAnalysis(t, got, ask)
	echo := fallbackCodingRequestRestatementFor("再收紧阈值", follow, impl)
	if !strings.Contains(echo, "这次再收紧阈值") || !strings.Contains(echo, "其它先不动") || !strings.Contains(echo, "「系统信息查看」") {
		t.Fatalf("follow-up should still echo: %q", echo)
	}
	if codingOpeningAnalysisUsable("这次开发c++版本的俄罗斯广场，其它先不动。", ask) {
		t.Fatal("the old echo must not pass as an analysis")
	}
	if codingOpeningAnalysisUsable("开发c++版本的俄罗斯广场，确认要做到的结果和边界。", ask) {
		t.Fatal("a near-copy of the command must not pass")
	}
	analysis := "要做一个用 C++ 编写的俄罗斯方块。完成后能在本地编译运行。只做这个游戏本身，不另加模式。方案确认前不改文件。"
	if !codingOpeningAnalysisUsable(analysis, ask) {
		t.Fatal("a real analysis should pass")
	}

	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	var tokens []string
	published := h.publishCodingRequestUnderstanding(userID, ask, stickyCodingWorkbenchMemory{}, impl, func(s string) {
		tokens = append(tokens, s)
	})
	assertOpeningTaskHostAnalysis(t, published, ask)
	if len(tokens) != 1 || tokens[0] != published {
		t.Fatalf("publish tokens=%q", tokens)
	}
	stored := h.getStickyCodingWorkbenchMemory(userID).RequirementRestatement
	if stored != published {
		t.Fatalf("stored=%q published=%q", stored, published)
	}
	refined := h.refineCodingRequestUnderstanding(userID, ask, h.getStickyCodingWorkbenchMemory(userID), impl)
	if refined != published {
		t.Fatalf("refine replaced the opening analysis: %q", refined)
	}
	plan := "## 需要确认执行计划\n\n确认后才会改文件。共 **3** 步。"
	joined := joinCodingUnderstandingAndBody(refined, plan)
	if !strings.HasPrefix(joined, published+"\n\n") || !strings.Contains(joined, "## 需要确认执行计划") {
		t.Fatalf("analysis must stay above the plan: %q", joined)
	}
}

func TestFallbackOpeningTitleClauseIsAnalyzed(t *testing.T) {
	const title = "使用本项目的代码"
	user := title + "，识别文件中的印章：\n与党中央保持高度一致，未发现过与中央精神相违背的言论。同意推荐该同志担任该职务。单位组织人事处。"
	mem := stickyCodingWorkbenchMemory{SessionPlan: title}
	if !codingOpeningTaskCommand(user, mem) {
		t.Fatal("a title that is only this command's leading clause is still the opening command")
	}
	if codingOpeningTaskCommand("再收紧阈值", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"}) {
		t.Fatal("a different stored title is not this command")
	}
	got := fallbackCodingRequestRestatementFor(user, mem, codingRequestDecision{Kind: codingRequestImplementation})
	if strings.Contains(got, "先处理「"+title+"」") {
		t.Fatalf("task title was repeated as the work: %q", got)
	}
	if !strings.Contains(got, "「识别文件中的印章」") {
		t.Fatalf("the requirement after the title was dropped: %q", got)
	}
	if !strings.Contains(got, "「"+title+"」") || !strings.Contains(got, "限定的是做法") {
		t.Fatalf("the leading clause should stay as a constraint: %q", got)
	}
	if !strings.Contains(got, "交给这个目标的材料") || !strings.Contains(got, "不复述原文") {
		t.Fatalf("the attached body should be named as material: %q", got)
	}
	if !strings.Contains(got, "当前项目里能完成这件事") || !strings.Contains(got, "确认前不改文件") {
		t.Fatalf("opening analysis missing: %q", got)
	}
	if strings.Contains(got, "与党中央") || strings.Contains(got, "组织人事处") {
		t.Fatalf("the attached body leaked into the reply: %q", got)
	}
	if !codingOpeningTaskNeedsAnalysis(user, mem, codingRequestDecision{Kind: codingRequestImplementation}) {
		t.Fatal("this first command should ask the model for an analysis")
	}

	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	published := h.publishCodingRequestUnderstanding(userID, user, mem, codingRequestDecision{Kind: codingRequestImplementation}, nil)
	if published != got {
		t.Fatalf("publish=%q fallback=%q", published, got)
	}
	pasted := "要识别文件中的印章。材料从与党中央保持高度一致，未发现过这段开始，确认前不改文件。"
	if codingOpeningAnalysisUsable(pasted, user) {
		t.Fatal("an analysis that quotes the supplied body must not replace the host sentence")
	}
	paraphrase := "用当前项目里已有的实现，识别用户给出的文书上的印章。正文只作为输入，不另做无关功能。确认前不改文件。"
	if !codingOpeningAnalysisUsable(paraphrase, user) {
		t.Fatal("a paraphrase that does not quote the body should stand")
	}
	later := "要识别文书上的印章。同意推荐该同志担任相关职务，正文只作输入。确认前不改文件。"
	if codingOpeningAnalysisUsable(later, user) {
		t.Fatal("quoting a later span of the body must not replace the host sentence")
	}
	prompt := codingOpeningAnalysisUserPrompt(user, mem)
	if strings.Contains(prompt, "与党中央") || strings.Contains(prompt, "组织人事处") {
		t.Fatalf("opening prompt pasted the body: %q", prompt)
	}
	if !strings.Contains(prompt, "识别文件中的印章") || !strings.Contains(prompt, "Constraint") || !strings.Contains(prompt, title) || !strings.Contains(prompt, "已附正文") {
		t.Fatalf("opening prompt dropped the task: %q", prompt)
	}
}

func TestFallbackOpeningKeepsRequirementBesideMaterial(t *testing.T) {
	const title = "使用本项目的代码"
	user := title + "，识别文件中的印章，并标出位置：\n" + strings.Repeat("与党中央保持高度一致。", 4)
	got := fallbackCodingRequestRestatementFor(user, stickyCodingWorkbenchMemory{SessionPlan: title}, codingRequestDecision{Kind: codingRequestImplementation})
	if !strings.Contains(got, "「识别文件中的印章」") || !strings.Contains(got, "「并标出位置」") {
		t.Fatalf("the requirement beside the document was dropped: %q", got)
	}
	if !strings.Contains(got, "材料") || strings.Contains(got, "与党中央") {
		t.Fatalf("document should stay material: %q", got)
	}
}

func TestFallbackOpeningColonBeforeGoalKeepsTheGoal(t *testing.T) {
	const title = "使用本项目的代码"
	user := title + "：识别文件中的印章：\n" + strings.Repeat("与党中央保持高度一致。", 4)
	got := fallbackCodingRequestRestatementFor(user, stickyCodingWorkbenchMemory{SessionPlan: title}, codingRequestDecision{Kind: codingRequestImplementation})
	if !strings.Contains(got, "「识别文件中的印章」") || strings.Contains(got, "与党中央") {
		t.Fatalf("the goal between two colons was lost: %q", got)
	}
	if !strings.Contains(got, "「"+title+"」") || !strings.Contains(got, "限定的是做法") {
		t.Fatalf("the title should stay a constraint: %q", got)
	}
}

func TestFallbackOpeningTitleColonDocumentIsNotTheGoal(t *testing.T) {
	const title = "使用本项目的代码"
	body := "与党中央保持高度一致：未发现过这段。\n" + strings.Repeat("单位组织人事处盖章。", 3)
	user := title + "：\n" + body
	got := fallbackCodingRequestRestatementFor(user, stickyCodingWorkbenchMemory{SessionPlan: title}, codingRequestDecision{Kind: codingRequestImplementation})
	if strings.Contains(got, "与党中央") {
		t.Fatalf("the document was quoted as the goal: %q", got)
	}
	if !strings.Contains(got, "「"+title+"」") || !strings.Contains(got, "材料") || !strings.Contains(got, "确认前不改文件") {
		t.Fatalf("the title should be the goal and the body material: %q", got)
	}
	if strings.Contains(got, "限定的是做法") {
		t.Fatalf("the title was repeated as both goal and constraint: %q", got)
	}
}

func TestOpeningTitleDoesNotSwallowTheNextWord(t *testing.T) {
	user := "数据库导出加水印记录并且支持一键回填到原始表格里面去，顺便把错误提示文案也统一成中文规范显示"
	mem := stickyCodingWorkbenchMemory{SessionPlan: "数据"}
	if codingHeadExtendsTitle("数据", "数据库导出") {
		t.Fatal("a title prefix must stop at a boundary")
	}
	if codingOpeningTaskCommand(user, mem) {
		t.Fatal("数据 must not count as the leading clause of 数据库")
	}
	got := fallbackCodingRequestRestatementFor(user, mem, codingRequestDecision{Kind: codingRequestImplementation})
	if strings.Contains(got, "限定的是做法") {
		t.Fatalf("a shorter title was peeled off the next word: %q", got)
	}
}

func TestFallbackOpeningDocumentIsMaterialNotAnotherTask(t *testing.T) {
	user := "识别文件中的印章：\n" + strings.Repeat("单位组织人事处盖章。", 6)
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "「识别文件中的印章」") || !strings.Contains(got, "材料") || !strings.Contains(got, "确认前不改文件") {
		t.Fatalf("document opening should name the goal and the material: %q", got)
	}
	if strings.Contains(got, "其余要求") || strings.Contains(got, "单位组织人事处") {
		t.Fatalf("the document was treated as another requirement: %q", got)
	}
	quoted := "识别文书印章时要看单位组织人事处盖章的位置。确认前不改文件，只做这一项。"
	if codingOpeningAnalysisUsable(quoted, user) {
		t.Fatal("quoting the seal line from the body must not replace the host sentence")
	}
}

func TestFallbackOpeningImageAttachmentIsTheMaterial(t *testing.T) {
	const title = "使用本项目的代码"
	const path = `C:\docs\stamp.png`
	block := filePathPromptPrefix + "\n" + path + "\nFor image files, the host sends them directly to a vision-capable model when available. Analyze attached images first; do not re-capture them or use read_file on image bytes."
	user := title + "，识别文件中的印章：\n\n" + block
	mem := stickyCodingWorkbenchMemory{SessionPlan: title}
	got := fallbackCodingRequestRestatementFor(user, mem, codingRequestDecision{Kind: codingRequestImplementation})
	if !strings.Contains(got, "「识别文件中的印章」") || !strings.Contains(got, "限定的是做法") {
		t.Fatalf("requirement dropped: %q", got)
	}
	if !strings.Contains(got, "附上的图片") || !strings.Contains(got, "材料") {
		t.Fatalf("the image was not named as material: %q", got)
	}
	if strings.Contains(got, "其余要求") || strings.Contains(got, "冒号后的正文") || strings.Contains(got, "先处理「"+title+"」") {
		t.Fatalf("the image was treated as more requirements or pasted prose: %q", got)
	}
	if strings.Contains(got, `C:\docs`) || strings.Contains(got, "stamp.png") || strings.Contains(got, "用户选择的本地文件路径") || strings.Contains(got, "For image files") {
		t.Fatalf("the path leaked into the reply: %q", got)
	}
	prompt := codingOpeningAnalysisUserPrompt(user, mem)
	if strings.Contains(prompt, `C:\docs`) || strings.Contains(prompt, "For image files") || strings.Contains(prompt, "已附正文") {
		t.Fatalf("opening prompt kept the path block: %q", prompt)
	}
	if !strings.Contains(prompt, "识别文件中的印章") || !strings.Contains(prompt, "Attached images: 1") || !strings.Contains(prompt, title) {
		t.Fatalf("opening prompt dropped the image: %q", prompt)
	}
	leaked := "要识别印章，材料在 C:\\docs\\stamp.png 这一张图里。确认前不改文件，只做这一项。"
	if codingOpeningAnalysisUsable(leaked, user) {
		t.Fatal("quoting the image path must not replace the host sentence")
	}
	ok := "用当前项目里已有的代码识别附上图片中的印章。图片是输入，不另做无关功能。确认前不改文件。"
	if !codingOpeningAnalysisUsable(ok, user) {
		t.Fatal("an analysis that names the image without its path should stand")
	}
	near := "使用本项目的代码，识别文件中的印章。确认前不改文件，只做这一项。"
	if codingOpeningAnalysisUsable(near, user) {
		t.Fatal("a copy of the typed sentence must not replace the host sentence")
	}

	ocrUser := user + "\n--- image_ocr: begin ---\n与党中央保持高度一致，未发现过这段。\n--- image_ocr: end ---"
	quoted := "要识别印章。图里写着与党中央保持高度一致，未发现过这段。确认前不改文件。"
	if codingOpeningAnalysisUsable(quoted, ocrUser) {
		t.Fatal("quoting the image transcript must not replace the host sentence")
	}
	if strings.Contains(fallbackCodingRequestRestatementFor(ocrUser, mem, codingRequestDecision{Kind: codingRequestImplementation}), "与党中央") {
		t.Fatal("the image transcript leaked into the host sentence")
	}
}

func TestFallbackOpeningImageWithoutColonIsNotAnotherRequirement(t *testing.T) {
	const title = "使用本项目的代码"
	user := title + "，识别文件中的印章\n\n" + filePathPromptPrefix + "\nC:\\docs\\stamp.png\nFor image files, the host sends them directly."
	got := fallbackCodingRequestRestatementFor(user, stickyCodingWorkbenchMemory{SessionPlan: title}, codingRequestDecision{Kind: codingRequestImplementation})
	if !strings.Contains(got, "「识别文件中的印章」") || !strings.Contains(got, "附上的图片") {
		t.Fatalf("image without a colon was dropped: %q", got)
	}
	if strings.Contains(got, "其余要求") || strings.Contains(got, `C:\docs`) || strings.Contains(got, "For image files") {
		t.Fatalf("the path block became another requirement: %q", got)
	}
}

func TestFallbackOpeningDocumentFileIsNamed(t *testing.T) {
	user := "识别文件中的印章\n\n" + filePathPromptPrefix + "\nC:\\docs\\paper.pdf\n"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "「识别文件中的印章」") || !strings.Contains(got, "附上的文件") || !strings.Contains(got, "第一条指令") {
		t.Fatalf("a document file should stay on the short opening: %q", got)
	}
	if strings.Contains(got, "附上的图片") || strings.Contains(got, "其余要求") || strings.Contains(got, `C:\docs`) {
		t.Fatalf("the document path leaked or was called an image: %q", got)
	}
}

func TestFallbackOpeningDocumentAndImageStayDistinct(t *testing.T) {
	const title = "使用本项目的代码"
	user := title + "，识别文件中的印章：\n" + strings.Repeat("与党中央保持高度一致。", 4) + "\n\n" + filePathPromptPrefix + "\nC:\\docs\\stamp.png\nFor image files, the host sends them directly."
	got := fallbackCodingRequestRestatementFor(user, stickyCodingWorkbenchMemory{SessionPlan: title}, codingRequestDecision{Kind: codingRequestImplementation})
	if !strings.Contains(got, "冒号后的正文") || !strings.Contains(got, "附上的图片") || !strings.Contains(got, "「识别文件中的印章」") {
		t.Fatalf("document and image should both be material: %q", got)
	}
	if strings.Contains(got, "与党中央") || strings.Contains(got, `C:\docs`) {
		t.Fatalf("document or path leaked: %q", got)
	}
}

func TestOpeningAnalysisKeepsRequirementAlsoWrittenInTheBody(t *testing.T) {
	user := "识别文件中的印章：\n识别文件中的印章就写在抬头。\n" + strings.Repeat("单位组织人事处盖章。", 4)
	analysis := "要做的是识别文件中的印章，用项目里已有的代码完成。正文只作输入，不另写新功能。确认前不改文件。"
	if !codingOpeningAnalysisUsable(analysis, user) {
		t.Fatal("naming the requirement must stay even when the body repeats it")
	}
	leaked := "识别时要看单位组织人事处盖章的位置。确认前不改文件，只做这一项。"
	if codingOpeningAnalysisUsable(leaked, user) {
		t.Fatal("a body span that is not the requirement must still be rejected")
	}
}

func TestFallbackOpeningTitleTrailingColonStillPeels(t *testing.T) {
	const title = "使用本项目的代码"
	user := title + "：识别文件中的印章：\n" + strings.Repeat("与党中央保持高度一致。", 4)
	mem := stickyCodingWorkbenchMemory{SessionPlan: title + "："}
	if !codingOpeningTaskCommand(user, mem) {
		t.Fatal("a trailing colon on the stored title is still this command")
	}
	got := fallbackCodingRequestRestatementFor(user, mem, codingRequestDecision{Kind: codingRequestImplementation})
	if !strings.Contains(got, "「识别文件中的印章」") || strings.Contains(got, "与党中央") {
		t.Fatalf("the goal after a trailing-colon title was lost: %q", got)
	}
	if !strings.Contains(got, "限定的是做法") {
		t.Fatalf("the title should stay a constraint: %q", got)
	}
}

func TestFallbackShortEditWithAttachmentStaysAnEcho(t *testing.T) {
	ask := "再收紧阈值\n\n" + filePathPromptPrefix + "\nC:\\Users\\ma139\\Documents\\notes\\threshold.pdf\n"
	if utf8.RuneCountInString(ask) <= 48 {
		t.Fatalf("the path block must push the raw text past the short gate: %d", utf8.RuneCountInString(ask))
	}
	mem := stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看", TurnCount: 1}
	got := fallbackCodingRequestRestatement(ask, mem)
	if !strings.Contains(got, "这次再收紧阈值") || !strings.Contains(got, "其它先不动") || !strings.Contains(got, "「系统信息查看」") {
		t.Fatalf("short edit lost its echo: %q", got)
	}
	if strings.Contains(got, "其余要求") || strings.Contains(got, `C:\Users`) || strings.Contains(got, "用户选择的本地文件路径") || strings.Contains(got, "第一条指令") {
		t.Fatalf("the file block changed the echo: %q", got)
	}
	if !codingRequestUnderstandingKeepsHostFallback(ask, mem, codingRequestDecision{Kind: codingRequestImplementation}) {
		t.Fatal("the attachment must not unlock the short echo")
	}
	same := fallbackCodingRequestRestatement(ask, stickyCodingWorkbenchMemory{SessionPlan: "再收紧阈值", TurnCount: 1})
	if strings.Contains(same, "在「再收紧阈值」里") {
		t.Fatalf("the path made the title look different from the ask: %q", same)
	}
}

func TestFallbackContinueWithAttachmentStaysAResume(t *testing.T) {
	user := "继续\n\n" + filePathPromptPrefix + "\nC:\\docs\\stamp.png\nFor image files, the host sends them directly to a vision-capable model when available."
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if !strings.Contains(got, "继续做「系统信息查看」") {
		t.Fatalf("continue plus a file should still resume: %q", got)
	}
	if strings.Contains(got, "先处理「继续」") || strings.Contains(got, `C:\docs`) || strings.Contains(got, "用户选择的本地文件路径") {
		t.Fatalf("the file block became the continue task: %q", got)
	}
}

func TestFallbackOpeningBareDocumentNameIsAFile(t *testing.T) {
	user := "识别文件中的印章\n\n" + filePathPromptPrefix + "\npaper.pdf\n"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "「识别文件中的印章」") || !strings.Contains(got, "附上的文件") {
		t.Fatalf("a bare document name was dropped: %q", got)
	}
	if strings.Contains(got, "paper.pdf") || strings.Contains(got, "其余要求") || strings.Contains(got, "附上的图片") {
		t.Fatalf("the bare document name leaked: %q", got)
	}
}

func TestFallbackQuestionDropsTheAttachment(t *testing.T) {
	user := "这个阈值现在是多少？\n\n" + filePathPromptPrefix + "\nC:\\docs\\note.pdf\n"
	got := fallbackCodingRequestRestatementFor(user, stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"}, codingRequestDecision{Kind: codingRequestInquiry})
	if !strings.Contains(got, "回答「这个阈值现在是多少」") || !strings.Contains(got, "只读不改文件") {
		t.Fatalf("question lost its sentence: %q", got)
	}
	if strings.Contains(got, `C:\docs`) || strings.Contains(got, "用户选择的本地文件路径") {
		t.Fatalf("the question quoted the file: %q", got)
	}
}

func TestFallbackAttachmentOnlyNamesTheTaskAndTheImage(t *testing.T) {
	const title = "使用本项目的代码"
	user := filePathPromptPrefix + "\nC:\\docs\\stamp.png\nFor image files, the host sends them directly."
	mem := stickyCodingWorkbenchMemory{SessionPlan: title}
	got := fallbackCodingRequestRestatement(user, mem)
	if !strings.Contains(got, "「"+title+"」") || !strings.Contains(got, "附上的图片") {
		t.Fatalf("attachment-only opening dropped the task or the image: %q", got)
	}
	if strings.Contains(got, `C:\docs`) || strings.Contains(got, "用户选择的本地文件路径") || strings.Contains(got, "其余要求") || strings.Contains(got, "先处理「") {
		t.Fatalf("attachment-only opening quoted the path: %q", got)
	}
	if !codingRequestUnderstandingKeepsHostFallback(user, mem, codingRequestDecision{Kind: codingRequestImplementation}) {
		t.Fatal("attachment-only opening must stay locked")
	}
	prompt := codingOpeningAnalysisUserPrompt(user, mem)
	if !strings.Contains(prompt, "Requirement: "+title) || !strings.Contains(prompt, "Attached images: 1") || strings.Contains(prompt, `C:\docs`) {
		t.Fatalf("attachment-only prompt dropped the task or kept the path: %q", prompt)
	}
}

func TestFallbackAttachmentOnlyWithoutTitleNamesTheFile(t *testing.T) {
	user := filePathPromptPrefix + "\npaper.pdf\n"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "「处理附上的文件」") || !strings.Contains(got, "第一条指令") || !strings.Contains(got, "附上的文件") {
		t.Fatalf("bare file drop was not an opening: %q", got)
	}
	if strings.Contains(got, "paper.pdf") || strings.Contains(got, "用户选择的本地文件路径") || strings.Contains(got, "其余要求") {
		t.Fatalf("bare file drop quoted the name: %q", got)
	}
}

func TestFallbackFollowUpAttachmentOnlyStaysAnEcho(t *testing.T) {
	user := filePathPromptPrefix + "\nC:\\docs\\note.pdf\n"
	mem := stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看", TurnCount: 1}
	got := fallbackCodingRequestRestatement(user, mem)
	if !strings.Contains(got, "这次处理附上的文件") || !strings.Contains(got, "其它先不动") || !strings.Contains(got, "「系统信息查看」") {
		t.Fatalf("follow-up file drop lost the echo: %q", got)
	}
	if strings.Contains(got, `C:\docs`) || strings.Contains(got, "用户选择的本地文件路径") || strings.Contains(got, "其余要求") || strings.Contains(got, "第一条指令") {
		t.Fatalf("follow-up file drop quoted the path: %q", got)
	}
	question := fallbackCodingRequestRestatementFor(user, mem, codingRequestDecision{Kind: codingRequestInquiry})
	if !strings.Contains(question, "回答「处理附上的文件」") || !strings.Contains(question, "只读不改文件") {
		t.Fatalf("attachment-only question quoted the wrong thing: %q", question)
	}
	if strings.Contains(question, `C:\docs`) || strings.Contains(question, "用户选择的本地文件路径") {
		t.Fatalf("attachment-only question quoted the path: %q", question)
	}
}

func TestFallbackOpeningShortColonKeepsTheClause(t *testing.T) {
	user := "请核对推理优化的完成度：六条线都要有证据，不要再写新的优化代码，结果写回现有报告。不要新做一套工具。"
	if utf8.RuneCountInString(user) <= 48 {
		t.Fatalf("fixture is not a long command: %d", utf8.RuneCountInString(user))
	}
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "六条线都要有证据") || strings.Contains(got, "材料") {
		t.Fatalf("a short colon clause should stay part of the requirement: %q", got)
	}
}

func TestFallbackLongFollowUpKeepsHeadline(t *testing.T) {
	got := fallbackCodingRequestRestatement(
		"帮我把导出功能加上水印记录并且支持一键回填到原始表格里面去，顺便把错误提示文案也统一成中文规范显示",
		stickyCodingWorkbenchMemory{SessionPlan: "数据导出工具", TurnCount: 1},
	)
	if strings.Contains(got, "第一条指令") {
		t.Fatalf("a later command became an opening analysis: %q", got)
	}
	if !strings.Contains(got, "先处理「") || !strings.Contains(got, "「数据导出工具」") || !strings.Contains(got, "水印记录") {
		t.Fatalf("follow-up headline dropped: %q", got)
	}
}

func TestCodingSessionContextLooksGeneric(t *testing.T) {
	if !codingSessionContextLooksGeneric("\u65b0\u5efa\u672c\u5730\u7f16\u7a0b\u4efb\u52a1-1") {
		t.Fatal("default local task title must be generic")
	}
	if codingSessionContextLooksGeneric("\u63a7\u5236\u53f0\u8d2a\u5403\u86c7") {
		t.Fatal("real session goal must stay usable")
	}
}

func TestFallbackDoesNotTreatGuidelinesAsGUI(t *testing.T) {
	got := fallbackCodingRequestRestatement("follow the guidelines", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "图形界面") || strings.Contains(got, "重新构建") || !strings.Contains(got, "follow the guidelines") {
		t.Fatalf("guidelines leaked into a GUI restatement: %q", got)
	}
}

func TestFallbackNewAskWithoutContextStillNamesWork(t *testing.T) {
	got := fallbackCodingRequestRestatement("分析前后端项目都有哪些功能", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "先看清当前项目") {
		t.Fatalf("context-free ask still got the process filler: %q", got)
	}
	if !strings.Contains(got, "分析前后端项目都有哪些功能") {
		t.Fatalf("restatement should name the ask: %q", got)
	}
}

func TestFallbackLongAskLeadsWithFirstRequirement(t *testing.T) {
	got := fallbackCodingRequestRestatement("帮我梳理一下这个项目里各个模块的职责和相互依赖关系，然后写一份完整的结构说明文档并且放到docs目录下面存档", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "先看清当前项目") {
		t.Fatalf("long ask still got the process filler: %q", got)
	}
	if !strings.Contains(got, "梳理一下这个项目里各个模块") {
		t.Fatalf("long ask should lead with its first requirement: %q", got)
	}
	if !strings.Contains(got, "其余要求") {
		t.Fatalf("remaining requirements should stay acknowledged: %q", got)
	}
}

func TestFallbackAskKeepsWorkNameContext(t *testing.T) {
	got := fallbackCodingRequestRestatement("帮我把导出功能加上水印记录，再补一组回归测试", stickyCodingWorkbenchMemory{SessionPlan: "数据导出工具"})
	if !strings.Contains(got, "「数据导出工具」") || !strings.Contains(got, "导出功能加上水印记录") {
		t.Fatalf("new ask should keep the work name and the ask itself: %q", got)
	}
}

func TestFallbackLongAskSkipsLeadingInterjection(t *testing.T) {
	got := fallbackCodingRequestRestatement("太好了！帮我把导出功能加上水印记录并且支持一键回填到原始表格里面去，顺便把错误提示文案也统一成中文规范显示", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "太好了") {
		t.Fatalf("interjection leaked into the headline: %q", got)
	}
	if !strings.Contains(got, "帮我把导出功能加上水印记录") {
		t.Fatalf("lead requirement dropped: %q", got)
	}
	got = fallbackCodingRequestRestatement("帮我，把导出功能加上水印记录并且支持一键回填到原始表格里面去，顺便把错误提示文案也统一成中文规范显示", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "「帮我，") {
		t.Fatalf("politeness opener leaked into the headline: %q", got)
	}
	if !strings.Contains(got, "把导出功能加上水印记录") {
		t.Fatalf("lead requirement dropped after opener: %q", got)
	}
}

func TestFallbackLongAskTruncationKeepsParensBalanced(t *testing.T) {
	got := fallbackCodingRequestRestatement("先把左侧导航栏的菜单项（icon1、icon2、icon3、icon4、icon5、icon6）重新整理为线性风格并更新样式", stickyCodingWorkbenchMemory{})
	if strings.Count(got, "（") != strings.Count(got, "）") || strings.Count(got, "(") != strings.Count(got, ")") {
		t.Fatalf("truncated head left unbalanced parens: %q", got)
	}
	if !strings.Contains(got, "菜单项") {
		t.Fatalf("head lost the lead requirement: %q", got)
	}
}

func TestFallbackLongAskNamesGoalBeforeList(t *testing.T) {
	got := fallbackCodingRequestRestatement("请完成下面这几件事：（1）梳理一下各个模块的职责划分和相互依赖关系（2）写一份完整的结构说明文档放到docs目录", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "（1）") || strings.Contains(got, "（2）") {
		t.Fatalf("list marker leaked into the headline: %q", got)
	}
	if !strings.Contains(got, "完成下面这几件事") {
		t.Fatalf("goal head dropped: %q", got)
	}
}

func TestFallbackLongAskBareListNamesFirstItem(t *testing.T) {
	got := fallbackCodingRequestRestatement("（1）把导出功能加上水印记录并支持一键回填到原始表格里面去、（2）把错误提示文案统一成中文规范显示样式", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "（1）") || strings.Contains(got, "（2）") {
		t.Fatalf("list marker leaked: %q", got)
	}
	if !strings.Contains(got, "把导出功能加上水印记录") {
		t.Fatalf("first list item dropped: %q", got)
	}
	if !strings.Contains(got, "其余要求") {
		t.Fatalf("later items should stay acknowledged: %q", got)
	}
}

func TestFallbackShortListAskNamesItems(t *testing.T) {
	got := fallbackCodingRequestRestatement("（1）改标题、（2）改图标", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "（1）") || strings.Contains(got, "（2）") {
		t.Fatalf("short list kept the markers: %q", got)
	}
	if !strings.Contains(got, "改标题") || !strings.Contains(got, "改图标") {
		t.Fatalf("short list dropped an item: %q", got)
	}
}

func TestFallbackListAskKeepsTrailingRequirement(t *testing.T) {
	got := fallbackCodingRequestRestatement("（1）改标题、（2）改图标，周三前交付", stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "改标题") || !strings.Contains(got, "改图标") {
		t.Fatalf("list items dropped: %q", got)
	}
	if !strings.Contains(got, "其余要求") {
		t.Fatalf("the requirement after the list must stay acknowledged: %q", got)
	}
}

func TestFallbackBareContinueWithoutContextKeepsProcessLine(t *testing.T) {
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "先看清当前项目") {
		t.Fatalf("context-free continue must not quote the continue token itself: %q", got)
	}
	if strings.Contains(got, "「继续」") {
		t.Fatalf("continue token leaked into the headline: %q", got)
	}
}

func TestFallbackUsesWorkspaceSourceIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		ProjectPath: dir,
	})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("workspace source identity missing: %q", got)
	}
}

func TestFallbackUsesReadmeIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Console Snake\n\nA demo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		ProjectPath: dir,
	})
	if !strings.Contains(got, "Console Snake") {
		t.Fatalf("readme identity missing: %q", got)
	}
}

func TestResolveCodingWorkbenchTasksSeedsRestatementNotShortCommand(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	understood := "\u628a\u5f53\u524d\u9879\u76ee\u6539\u6210\u56fe\u5f62\u754c\u9762\u7248\u672c\uff0c\u4fdd\u7559\u539f\u6709\u6838\u5fc3\u884c\u4e3a"
	_, _, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		"\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248",
		"D:/repo",
		stickyCodingWorkbenchMemory{RequirementRestatement: understood},
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true},
		nil,
		nil,
	)
	if !planned {
		t.Fatal("rewrite must still plan")
	}
	got := h.getStickyCodingWorkbenchMemory(userID).SessionPlan
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("session plan should seed from restatement, got %q", got)
	}
	if got == "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248" {
		t.Fatal("short follow-up must not become the session goal")
	}
}

func TestOperationalDecisionNamesALaunch(t *testing.T) {
	launch := codingRequestDecision{Kind: codingRequestOperational, Acceptance: codingOperationalAcceptanceLaunch}
	got := fallbackCodingRequestRestatementFor("运行一下", stickyCodingWorkbenchMemory{}, launch)
	if !strings.Contains(got, "不改源代码") {
		t.Fatalf("a launch decision should say the program will be run: %q", got)
	}
	// The same words are not a launch when the decision says otherwise.
	got = fallbackCodingRequestRestatementFor("运行一下", stickyCodingWorkbenchMemory{}, codingRequestDecision{Kind: codingRequestInquiry})
	if strings.Contains(got, "不改源代码") || !strings.Contains(got, "不改文件") {
		t.Fatalf("an inquiry that mentions 运行 is still an answer: %q", got)
	}
	got = fallbackCodingRequestRestatement("运行并加上日志", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "不改源代码") {
		t.Fatalf("wording alone must not become a launch: %q", got)
	}
	got = fallbackCodingRequestRestatementFor("运行一下", stickyCodingWorkbenchMemory{SessionPlan: "qmobjdet"}, launch)
	if !strings.Contains(got, "「qmobjdet」") || !strings.Contains(got, "不改源代码") {
		t.Fatalf("a launch should keep the work name: %q", got)
	}
}

func TestFallbackUsesCMakeProjectIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.16)\nproject(snake CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util.cpp"), []byte("int util(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "board.cpp"), []byte("int board(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: dir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("cmake project identity missing: %q", got)
	}

	commented := t.TempDir()
	if err := os.WriteFile(filepath.Join(commented, "CMakeLists.txt"), []byte("# project(draft)\nproject(${PROJECT_NAME})\nproject(qmobjdet CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commented, "det.cpp"), []byte("int d(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commented, "util.cpp"), []byte("int u(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: commented})
	if strings.Contains(got, "draft") || strings.Contains(got, "PROJECT_NAME") || !strings.Contains(got, "qmobjdet") {
		t.Fatalf("a commented or variable project() must not hide the literal name: %q", got)
	}

	variableOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(variableOnly, "CMakeLists.txt"), []byte("project(${PROJECT_NAME})\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(variableOnly, "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: variableOnly})
	if strings.Contains(got, "PROJECT_NAME") || !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("a variable project() must fall through to the source stem: %q", got)
	}
}

func TestFallbackUsesNestedSourceAndBinaryIdentity(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(srcDir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "src", "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: srcDir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("src/ identity missing: %q", got)
	}

	binDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(binDir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "build", "snake.exe"), []byte("mz"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: binDir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("build exe identity missing: %q", got)
	}
}

func TestFallbackFindsSourceAfterManyDirs(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		if err := os.Mkdir(filepath.Join(dir, "dir"+string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: dir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("source after dirs missing: %q", got)
	}
}

func TestFallbackFindsExeAfterManyBuildFiles(t *testing.T) {
	dir := t.TempDir()
	build := filepath.Join(dir, "build")
	if err := os.Mkdir(build, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(build, "obj"+string(rune('A'+i%26))+string(rune('0'+i/26))+".obj"), []byte("o"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(build, "snake.exe"), []byte("mz"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: dir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("exe after junk build files missing: %q", got)
	}
}

func TestInquiryDecisionAnswersWithoutReadingTheWording(t *testing.T) {
	inquiry := codingRequestDecision{Kind: codingRequestInquiry}
	for _, ask := range []string{"怎么运行", "how to run this", "指令集支持情况帮我讲讲", "本程序现在支持哪些指令集"} {
		got := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{}, inquiry)
		if strings.Contains(got, "不改源代码") || !strings.Contains(got, "不改文件") || !strings.Contains(got, "回答「") {
			t.Fatalf("inquiry %q should be answered from the current code: %q", ask, got)
		}
		plain := fallbackCodingRequestRestatement(ask, stickyCodingWorkbenchMemory{})
		assertOpeningTaskHostAnalysis(t, plain, ask)
	}
}

func TestAttachCodingWorkRootOverridesStalePath(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{ProjectPath: "D:/old-workspace"}
	attachCodingWorkRoot(&mem, "D:/live-workspace")
	if mem.ProjectPath != "D:/live-workspace" {
		t.Fatalf("work root=%q", mem.ProjectPath)
	}
}

func TestFallbackDoesNotTreatRunFailureAsRunOnly(t *testing.T) {
	got := fallbackCodingRequestRestatement("\u8fd0\u884c\u5931\u8d25\u4e86", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "\u4e0d\u6539\u6e90\u4ee3\u7801") {
		t.Fatalf("failed run restated as a no-edit launch: %q", got)
	}
}

func TestRestatementDoesNotPickARewriteFromWording(t *testing.T) {
	for _, text := range []string{
		"please review the rewriter comments",
		"rewrite the parser",
		"改为图形界面版",
		"介绍一下图形界面",
		"在图形界面里把按钮改成蓝色",
		"不要改成图形界面",
	} {
		got := fallbackCodingRequestRestatement(text, stickyCodingWorkbenchMemory{})
		if strings.Contains(got, "图形界面版本") || strings.Contains(got, "重新构建") || strings.Contains(got, "重写后的实现") {
			t.Fatalf("wording selected a rewrite template: %q -> %q", text, got)
		}
	}
}

func TestUnderstandingPromptCarriesWrittenSessionRecord(t *testing.T) {
	with := buildCodingUnderstandingUserPrompt("继续", stickyCodingWorkbenchMemory{
		FilesModified: []string{"/home/prj8/src/ui.cpp"},
		LastSummary:   "本环境为只读",
	})
	if !strings.Contains(with, "/home/prj8/src/ui.cpp") || !strings.Contains(with, "not read-only") {
		t.Fatalf("understanding prompt missing the written-session record: %s", with)
	}
	if strings.Index(with, "not read-only") < strings.Index(with, "本环境为只读") {
		t.Fatal("session record must come after the previous summary")
	}
	without := buildCodingUnderstandingUserPrompt("继续", stickyCodingWorkbenchMemory{})
	if strings.Contains(without, "not read-only") {
		t.Fatal("a session with no written files must not claim a write record")
	}
}

func TestRunOnlyRestatementIsNotReplaced(t *testing.T) {
	written := stickyCodingWorkbenchMemory{FilesModified: []string{"/home/prj8/src/ui.cpp"}}
	launch := codingRequestDecision{Kind: codingRequestOperational, Acceptance: codingOperationalAcceptanceLaunch}
	if !codingRequestUnderstandingKeepsHostFallback("编译并运行", written, launch) {
		t.Fatal("a launch decision must keep the host restatement")
	}
	inquiry := codingRequestDecision{Kind: codingRequestInquiry}
	if !codingRequestUnderstandingKeepsHostFallback("怎么运行", written, inquiry) {
		t.Fatal("an inquiry must keep the host answer")
	}
	if codingRequestUnderstandingKeepsHostFallback("编译并运行", stickyCodingWorkbenchMemory{}, codingRequestDecision{}) {
		t.Fatal("wording alone must not lock the host sentence")
	}
	writtenPlan := stickyCodingWorkbenchMemory{
		FilesModified: []string{"/home/prj8/src/ui.cpp"},
		SessionPlan:   "系统信息查看",
	}
	impl := codingRequestDecision{Kind: codingRequestImplementation}
	if !codingRequestUnderstandingKeepsHostFallback("继续", writtenPlan, impl) {
		t.Fatal("a short follow-up in a session that already wrote files must keep the host restatement")
	}
	long := strings.Repeat("继续完善当前进程列表的展示 ", 6)
	if utf8.RuneCountInString(long) <= 48 {
		t.Fatalf("fixture is not a long request: %d", utf8.RuneCountInString(long))
	}
	if codingRequestUnderstandingKeepsHostFallback(long, writtenPlan, impl) {
		t.Fatal("a longer change request must still be open to restatement")
	}
	if !codingRequestUnderstandingKeepsHostFallback(long, writtenPlan, inquiry) {
		t.Fatal("a long inquiry must keep the host answer")
	}
}

func TestWrittenSessionRestatementDoesNotAnnounceReadOnly(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{
		SessionPlan:   "系统信息查看",
		FilesModified: []string{"/home/prj8/src/ui.cpp"},
	}
	ask := "这段 CPU% 为什么乘了核心数？"
	plain := fallbackCodingRequestRestatement(ask, mem)
	if strings.Contains(plain, "不改文件") || strings.Contains(plain, "不改源代码") {
		t.Fatalf("wording alone must not announce a read-only turn: %q", plain)
	}
	got := fallbackCodingRequestRestatementFor(ask, mem, codingRequestDecision{Kind: codingRequestInquiry})
	if strings.Contains(got, "不改文件") || strings.Contains(got, "其它先不动") {
		t.Fatalf("a session that already wrote files must not be restated as read-only: %q", got)
	}
	if !strings.Contains(got, "为什么乘了核心数") || !strings.Contains(got, "对照现有实现") || !strings.Contains(got, "回答「") {
		t.Fatalf("written-session inquiry should still name the answer: %q", got)
	}
	if strings.Contains(got, "？。") || strings.Contains(got, "按你这次说的做") || strings.Contains(got, "再动手") {
		t.Fatalf("question restated as an edit: %q", got)
	}
}

func TestRestatementFollowsTheDecisionNotTheWording(t *testing.T) {
	inquiry := codingRequestDecision{Kind: codingRequestInquiry}
	impl := codingRequestDecision{Kind: codingRequestImplementation}
	for _, text := range []string{
		"怎么改成图形界面",
		"改为图形界面版？",
		"不要改成图形界面",
		"不小心改成图形界面",
		"port to GUI",
		"don't port to GUI",
		"图形界面支持哪些指令集",
		"介绍一下图形界面",
		"在图形界面里把按钮改成蓝色",
		"给图形界面加一个按钮",
	} {
		got := fallbackCodingRequestRestatement(text, stickyCodingWorkbenchMemory{})
		assertOpeningTaskHostAnalysis(t, got, text)
	}
	got := fallbackCodingRequestRestatementFor("怎么改成图形界面", stickyCodingWorkbenchMemory{SessionPlan: "控制台贪吃蛇"}, inquiry)
	if !strings.Contains(got, "不改文件") || !strings.Contains(got, "回答「") || strings.Contains(got, "图形界面版本") {
		t.Fatalf("inquiry should be an answer: %q", got)
	}
	got = fallbackCodingRequestRestatementFor("改为图形界面版", stickyCodingWorkbenchMemory{}, impl)
	assertOpeningTaskHostAnalysis(t, got, "改为图形界面版")
	got = fallbackCodingRequestRestatementFor("改为图形界面版", stickyCodingWorkbenchMemory{SessionPlan: "控制台贪吃蛇"}, impl)
	if !strings.Contains(got, "这次改为图形界面版") || !strings.Contains(got, "其它先不动") || !strings.Contains(got, "「控制台贪吃蛇」") || strings.Contains(got, "图形界面版本") || strings.Contains(got, "重新构建") {
		t.Fatalf("a later edit should still echo: %q", got)
	}
	got = fallbackCodingRequestRestatementFor("在图形界面里把按钮改成蓝色", stickyCodingWorkbenchMemory{}, impl)
	assertOpeningTaskHostAnalysis(t, got, "在图形界面里把按钮改成蓝色")
	got = fallbackCodingRequestRestatementFor("继续", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"}, inquiry)
	if !strings.Contains(got, "继续做「系统信息查看」") || strings.Contains(got, "不改文件") {
		t.Fatalf("a continue cue stays a resume: %q", got)
	}
	got = fallbackCodingRequestRestatementFor("安装 gcc", stickyCodingWorkbenchMemory{}, codingRequestDecision{
		Kind:       codingRequestOperational,
		Acceptance: codingOperationalAcceptanceCommand,
	})
	if strings.Contains(got, "不改源代码") || !strings.Contains(got, "安装 gcc") {
		t.Fatalf("a host command is not a project launch: %q", got)
	}
}

func TestFallbackShortFollowUpKeepsWorkName(t *testing.T) {
	got := fallbackCodingRequestRestatement("再收紧阈值", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if !strings.Contains(got, "「系统信息查看」") || !strings.Contains(got, "这次再收紧阈值") {
		t.Fatalf("short work name and the new ask should both stay: %q", got)
	}
	if strings.Contains(got, "上继续你这次提出的改动") || strings.Contains(got, "再动手") {
		t.Fatalf("new ask must not use the bare-continue template: %q", got)
	}
	if !codingRequestUnderstandingKeepsHostFallback("再收紧阈值", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"}, codingRequestDecision{Kind: codingRequestImplementation}) {
		t.Fatal("quoted short ask must stay on the checklist")
	}
}

func TestFallbackBareContinueKeepsShortWorkName(t *testing.T) {
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if !strings.Contains(got, "继续做「系统信息查看」") || !strings.Contains(got, "从上次停下的地方接着做") {
		t.Fatalf("bare continue should name the work: %q", got)
	}
	if strings.Contains(got, "上继续你这次提出的改动") || strings.Contains(got, "再动手") || strings.Contains(got, "只改这次提到的部分") {
		t.Fatalf("bare continue invented a change: %q", got)
	}
}

func TestFallbackLongRequirementEchoStaysReadable(t *testing.T) {
	plan := "你要的不是再写优化代码，而是一份可信的完成度核查：对6条推理优化线——（1）x86 AVX2、（2）x86 AVX-512、（3）ARM NEON，并核对现有实现。"
	plan += strings.Repeat("补充约束与回归范围。", 8)
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "上继续你这次提出的改动") || strings.Contains(got, "继续这项需求") || strings.Contains(got, "你要的不是") {
		t.Fatalf("requirement paragraph was wrapped instead of described: %q", got)
	}
	if !strings.Contains(got, "做一份可信的完成度核查") || !strings.Contains(got, "而不是再写优化代码") {
		t.Fatalf("echo should describe the check: %q", got)
	}
	if strings.Count(got, "（") != strings.Count(got, "）") {
		t.Fatalf("unbalanced parens: %q", got)
	}
	if !strings.Contains(got, "完成度核查") || !strings.Contains(got, "推理优化线") {
		t.Fatalf("echo dropped the requirement: %q", got)
	}
	if strings.Contains(got, "（1）") || strings.Contains(got, "（3）上") {
		t.Fatalf("list marker leaked into the echo: %q", got)
	}
	if !strings.Contains(got, "x86 AVX2") || !strings.Contains(got, "x86 AVX-512") || !strings.Contains(got, "ARM NEON") {
		t.Fatalf("echo dropped the optimization lines: %q", got)
	}
}

func TestFallbackRequirementEchoCutsLongPreambleAtList(t *testing.T) {
	plan := strings.Repeat("先核对现有实现与约束。", 12) + "（1）x86 AVX2、（2）x86 AVX-512"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "（1）") {
		t.Fatalf("long preamble fell through into a list marker: %q", got)
	}
	if !strings.Contains(got, "x86 AVX2") || !strings.Contains(got, "x86 AVX-512") {
		t.Fatalf("echo dropped the list titles: %q", got)
	}
	if !strings.Contains(got, "核对现有实现") {
		t.Fatalf("echo dropped the preamble: %q", got)
	}
	if strings.Count(got, "（") != strings.Count(got, "）") {
		t.Fatalf("unbalanced parens: %q", got)
	}
}

func TestFallbackContinueKeepsClauseAfterColon(t *testing.T) {
	plan := "请核对推理优化的完成度：六条线都要有证据，不要再写新的优化代码。"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if !strings.Contains(got, "六条线都要有证据") {
		t.Fatalf("colon cut dropped the constraint: %q", got)
	}
	if strings.Contains(got, "再动手") {
		t.Fatalf("check-only requirement must not grow an edit: %q", got)
	}
}

func TestFallbackRequirementEchoDropsUnbalancedList(t *testing.T) {
	plan := "核对完成度（范围先写到这里——（1）x86 AVX2、（2）x86 AVX-512"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "（1）") {
		t.Fatalf("unbalanced head fell through into a list marker: %q", got)
	}
	if !strings.Contains(got, "x86 AVX2") {
		t.Fatalf("echo dropped the list titles: %q", got)
	}
	if !strings.Contains(got, "核对完成度") {
		t.Fatalf("echo dropped the goal: %q", got)
	}
	if strings.Count(got, "（") != strings.Count(got, "）") {
		t.Fatalf("unbalanced parens: %q", got)
	}
}

func TestFallbackRequirementEchoStopsBeforeBareList(t *testing.T) {
	plan := "核对六条推理优化线的完成度——（1）x86 AVX2、（2）x86 AVX-512、（3）ARM NEON"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "（1）") {
		t.Fatalf("echo should not keep the list marker: %q", got)
	}
	if !strings.Contains(got, "x86 AVX2") || !strings.Contains(got, "ARM NEON") {
		t.Fatalf("echo dropped the optimization lines: %q", got)
	}
	if !strings.Contains(got, "完成度") {
		t.Fatalf("echo dropped the goal: %q", got)
	}
}

func TestFallbackShortNewAskIsNotReplacedByOldRequirement(t *testing.T) {
	plan := "你要的不是再写优化代码，而是一份可信的完成度核查：对6条推理优化线——（1）x86 AVX2、（2）x86 AVX-512。"
	got := fallbackCodingRequestRestatement("只查 AVX-512", stickyCodingWorkbenchMemory{
		SessionPlan: plan,
		LastSummary: "系统信息查看",
	})
	if strings.Contains(got, "完成度核查") || strings.Contains(got, "（1）") || strings.Contains(got, "在已有工作（") {
		t.Fatalf("new ask was replaced by the old requirement: %q", got)
	}
	if !strings.Contains(got, "这次只查 AVX-512") {
		t.Fatalf("new ask missing from the echo: %q", got)
	}
}

func TestFallbackColonAfterVerbStaysInOrder(t *testing.T) {
	plan := "请核对：对6条推理优化线给出证据，不要改代码。"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "做请核对") {
		t.Fatalf("verb was placed after 做: %q", got)
	}
	if !strings.Contains(got, "请核对") || !strings.Contains(got, "对6条推理优化线") {
		t.Fatalf("clause dropped: %q", got)
	}
}

func TestFallbackPoliteAskIsNotPrefixed(t *testing.T) {
	got := fallbackCodingRequestRestatement("请核对一下", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if strings.Contains(got, "这次请") {
		t.Fatalf("请 was glued to 这次: %q", got)
	}
	if !strings.Contains(got, "请核对一下") || !strings.Contains(got, "系统信息查看") {
		t.Fatalf("ask dropped: %q", got)
	}
}

func TestFallbackListAtStartDoesNotEchoMarker(t *testing.T) {
	plan := "（1）x86 AVX2、（2）x86 AVX-512"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "（1）") || strings.Contains(got, "（2）") {
		t.Fatalf("leading list marker leaked: %q", got)
	}
	if !strings.Contains(got, "x86 AVX2") || !strings.Contains(got, "x86 AVX-512") {
		t.Fatalf("leading list dropped the titles: %q", got)
	}
}

func TestFallbackShortPreambleDoesNotEchoListMarker(t *testing.T) {
	plan := "注：（1）x86 AVX2、（2）ARM NEON"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "（1）") {
		t.Fatalf("short preamble fell through into the marker: %q", got)
	}
	if !strings.Contains(got, "x86 AVX2") || !strings.Contains(got, "ARM NEON") {
		t.Fatalf("short preamble dropped the titles: %q", got)
	}
}

func TestFallbackColonNounRewriteStaysANoun(t *testing.T) {
	plan := "结果如下：对6条推理优化线给出证据。"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "做结果如下") {
		t.Fatalf("a clause was used as a noun: %q", got)
	}
	if !strings.Contains(got, "结果如下") || !strings.Contains(got, "对6条推理优化线") {
		t.Fatalf("clause dropped: %q", got)
	}
}

func TestFallbackListedItemAlreadyNamedIsNotDropped(t *testing.T) {
	plan := "核对 x86 AVX2 是否落地——（1）x86 AVX2、（2）ARM NEON"
	got := fallbackCodingRequestRestatement("继续", stickyCodingWorkbenchMemory{SessionPlan: plan})
	if strings.Contains(got, "（1）") {
		t.Fatalf("marker leaked: %q", got)
	}
	if !strings.Contains(got, "ARM NEON") {
		t.Fatalf("later list title was dropped because the first title was already named: %q", got)
	}
}

func TestFallbackWholeScopeAskDoesNotSayLeaveTheRest(t *testing.T) {
	got := fallbackCodingRequestRestatement("核对全部优化线", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if strings.Contains(got, "其它先不动") {
		t.Fatalf("a whole-scope ask was told to leave the rest: %q", got)
	}
	if !strings.Contains(got, "核对全部优化线") || !strings.Contains(got, "系统信息查看") {
		t.Fatalf("ask dropped: %q", got)
	}
}

func TestFallbackDoesNotUseRequirementClauseAsWorkName(t *testing.T) {
	summary := "请针对刚编译并刷入最新版本的 buddy 固件，确认设备是否又一次出现黑屏。"
	got := fallbackCodingRequestRestatement("继续核对", stickyCodingWorkbenchMemory{LastSummary: summary})
	if strings.Contains(got, "在已有工作（") {
		t.Fatalf("a sentence must not become the work name: %q", got)
	}
}

func TestFallbackQuestionNamesTheAnswerInsteadOfEchoing(t *testing.T) {
	const ask = "本程序现在支持哪些指令集"
	inquiry := codingRequestDecision{Kind: codingRequestInquiry}
	plain := fallbackCodingRequestRestatement(ask, stickyCodingWorkbenchMemory{SessionPlan: ask})
	assertOpeningTaskHostAnalysis(t, plain, ask)
	got := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{SessionPlan: ask}, inquiry)
	if strings.Contains(got, "其它先不动") || strings.Contains(got, "这次"+ask) {
		t.Fatalf("question was echoed as an edit: %q", got)
	}
	if strings.Contains(got, "在「"+ask+"」里") {
		t.Fatalf("task title repeated the question: %q", got)
	}
	if !strings.Contains(got, "回答「"+ask+"」") || !strings.Contains(got, "不改文件") || !strings.Contains(got, "对照当前实现") {
		t.Fatalf("question should say it will be answered from the current code: %q", got)
	}
	if !codingRequestUnderstandingKeepsHostFallback(ask, stickyCodingWorkbenchMemory{SessionPlan: ask}, inquiry) {
		t.Fatal("an inquiry must keep the host answer")
	}

	written := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{
		SessionPlan:   ask,
		FilesModified: []string{"src/simd.cpp"},
	}, inquiry)
	if strings.Contains(written, "不改文件") || strings.Contains(written, "其它先不动") {
		t.Fatalf("a session that already wrote files must not be restated as read-only: %q", written)
	}
	if !strings.Contains(written, "回答「"+ask+"」") || !strings.Contains(written, "对照现有实现") {
		t.Fatalf("written session should still answer the question: %q", written)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	named := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{
		SessionPlan: ask,
		ProjectPath: dir,
	}, inquiry)
	if strings.Contains(named, "在「"+ask+"」里") || !strings.Contains(strings.ToLower(named), "snake") || !strings.Contains(named, "回答「"+ask+"」") {
		t.Fatalf("a task titled with the question should still name the project: %q", named)
	}
}

func TestFallbackWorkNameFallsThroughATitleThatIsTheAsk(t *testing.T) {
	ask := "控制台贪吃蛇, ANSI"
	inquiry := codingRequestDecision{Kind: codingRequestInquiry}
	got := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{
		SessionPlan: ask,
		LastSummary: "qmobjdet",
	}, inquiry)
	if strings.Contains(got, "在「控制台贪吃蛇」里") || !strings.Contains(got, "「qmobjdet」") || !strings.Contains(got, "回答「"+ask+"」") {
		t.Fatalf("a comma note on the same sentence must not hide the next name: %q", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# 请针对刚编译并刷入最新版本的 buddy 固件，确认设备是否又一次出现黑屏。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte("project(qmobjdet CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "det.cpp"), []byte("int d(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util.cpp"), []byte("int u(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	named := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{
		SessionPlan: ask,
		ProjectPath: dir,
	}, inquiry)
	if strings.Contains(named, "在「控制台贪吃蛇」里") || strings.Contains(named, "buddy") || !strings.Contains(named, "「qmobjdet」") {
		t.Fatalf("a prose readme must not hide the project name: %q", named)
	}

	readmeDir := t.TempDir()
	const titled = "本程序现在支持哪些指令集"
	if err := os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# "+titled+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readmeDir, "CMakeLists.txt"), []byte("project(qmobjdet CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromReadme := fallbackCodingRequestRestatementFor(titled, stickyCodingWorkbenchMemory{
		SessionPlan: titled,
		ProjectPath: readmeDir,
	}, inquiry)
	if strings.Contains(fromReadme, "在「"+titled+"」里") || !strings.Contains(fromReadme, "「qmobjdet」") || !strings.Contains(fromReadme, "回答「"+titled+"」") {
		t.Fatalf("a readme titled with the question must not hide the project: %q", fromReadme)
	}

	sentenceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sentenceDir, "README.md"), []byte("# 编译已经完成。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sentenceDir, "CMakeLists.txt"), []byte("project(qmobjdet CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromSentence := fallbackCodingRequestRestatementFor(titled, stickyCodingWorkbenchMemory{
		SessionPlan: titled,
		LastSummary: "已完成编译。",
		ProjectPath: sentenceDir,
	}, inquiry)
	if strings.Contains(fromSentence, "已完成编译") || strings.Contains(fromSentence, "编译已经完成") || !strings.Contains(fromSentence, "「qmobjdet」") {
		t.Fatalf("a status sentence must not hide the project: %q", fromSentence)
	}

	dotted := fallbackCodingRequestRestatementFor("改为图形界面版", stickyCodingWorkbenchMemory{
		LastSummary: "done snake.cpp console",
	}, inquiry)
	if !strings.Contains(dotted, "done snake.cpp console") || !strings.Contains(dotted, "回答「改为图形界面版」") {
		t.Fatalf("a dotted file name is still a title: %q", dotted)
	}
	versioned := fallbackCodingRequestRestatementFor("改为图形界面版", stickyCodingWorkbenchMemory{
		LastSummary: "v1.2",
	}, inquiry)
	if !strings.Contains(versioned, "v1.2") || !strings.Contains(versioned, "回答「改为图形界面版」") {
		t.Fatalf("a version dot is still a title: %q", versioned)
	}

	mid := t.TempDir()
	if err := os.WriteFile(filepath.Join(mid, "README.md"), []byte("# 编译已经完成。详见 simd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mid, "CMakeLists.txt"), []byte("project(qmobjdet CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromMid := fallbackCodingRequestRestatementFor(titled, stickyCodingWorkbenchMemory{
		SessionPlan: titled,
		LastSummary: "Done. See simd",
		ProjectPath: mid,
	}, inquiry)
	if strings.Contains(fromMid, "编译已经完成") || strings.Contains(fromMid, "Done. See simd") || !strings.Contains(fromMid, "「qmobjdet」") {
		t.Fatalf("a sentence that continues on the same line must not hide the project: %q", fromMid)
	}

	laterTitle := t.TempDir()
	if err := os.WriteFile(filepath.Join(laterTitle, "README.md"), []byte("# 编译已经完成。详见 simd\n\nConsole Snake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(laterTitle, "CMakeLists.txt"), []byte("project(qmobjdet CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromLater := fallbackCodingRequestRestatementFor("改为图形界面版", stickyCodingWorkbenchMemory{
		ProjectPath: laterTitle,
	}, inquiry)
	if !strings.Contains(fromLater, "Console Snake") || strings.Contains(fromLater, "编译已经完成") || strings.Contains(fromLater, "qmobjdet") {
		t.Fatalf("a later readme title should still name the work: %q", fromLater)
	}
}

func TestFallbackQuestionKeepsADistinctWorkName(t *testing.T) {
	got := fallbackCodingRequestRestatementFor("现在支持哪些指令集", stickyCodingWorkbenchMemory{SessionPlan: "qmobjdet"}, codingRequestDecision{Kind: codingRequestInquiry})
	if !strings.Contains(got, "「qmobjdet」") || !strings.Contains(got, "回答「现在支持哪些指令集」") || !strings.Contains(got, "不改文件") {
		t.Fatalf("distinct work name should stay around the answer: %q", got)
	}
}

func TestFallbackEditThatMentionsWhichStaysAnEdit(t *testing.T) {
	got := fallbackCodingRequestRestatement("删掉哪些临时文件", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if strings.Contains(got, "不改文件") || strings.Contains(got, "回答「") {
		t.Fatalf("an edit that mentions 哪些 was restated as a question: %q", got)
	}
	if !strings.Contains(got, "删掉哪些临时文件") || !strings.Contains(got, "系统信息查看") {
		t.Fatalf("edit dropped: %q", got)
	}
	got = fallbackCodingRequestRestatement("改哪些按钮", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if strings.Contains(got, "不改文件") || !strings.Contains(got, "改哪些按钮") {
		t.Fatalf("改哪些 should stay an edit: %q", got)
	}
	got = fallbackCodingRequestRestatement("请帮我实现哪些接口", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "请帮我实现哪些接口")
	got = fallbackCodingRequestRestatement("修改哪些按钮", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "修改哪些按钮")
	got = fallbackCodingRequestRestatement("删除哪些临时文件", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "删除哪些临时文件")
	got = fallbackCodingRequestRestatement("写哪些配置", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "写哪些配置")
	got = fallbackCodingRequestRestatement("改好哪些按钮", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "改好哪些按钮")
	got = fallbackCodingRequestRestatement("运行几个测试", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "运行几个测试")
	got = fallbackCodingRequestRestatementFor("运行几个测试", stickyCodingWorkbenchMemory{}, codingRequestDecision{Kind: codingRequestOperational})
	if strings.Contains(got, "不改文件") || !strings.Contains(got, "不改源代码") {
		t.Fatalf("a launch decision should say the program will be run: %q", got)
	}
	got = fallbackCodingRequestRestatement("删掉临时文件？", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "删掉临时文件？")
	got = fallbackCodingRequestRestatement("继续？", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if strings.Contains(got, "不改文件") || !strings.Contains(got, "继续做") {
		t.Fatalf("继续？ should stay a continue: %q", got)
	}
	got = fallbackCodingRequestRestatement("支持哪些指令集？", stickyCodingWorkbenchMemory{})
	assertOpeningTaskHostAnalysis(t, got, "支持哪些指令集？")
	got = fallbackCodingRequestRestatementFor("支持哪些指令集？", stickyCodingWorkbenchMemory{}, codingRequestDecision{Kind: codingRequestInquiry})
	if !strings.Contains(got, "不改文件") || !strings.Contains(got, "回答「") {
		t.Fatalf("an inquiry decision should be answered: %q", got)
	}
}

func TestFallbackCompletedAspectQuestionIsNotAnEdit(t *testing.T) {
	inquiry := codingRequestDecision{Kind: codingRequestInquiry}
	for _, ask := range []string{
		"实现了哪些接口", "修复了哪些崩溃", "不要改成蓝色，现在支持哪些指令集",
		"删除了哪些文件", "修改了哪些接口", "改变了哪些行为", "修改过哪些接口",
		"参加哪些评审", "删减了哪些功能", "读写哪些文件", "有几个指令集", "支持几个指令集",
		"写完哪些文件", "实现完了吗", "改好了吗",
	} {
		plain := fallbackCodingRequestRestatement(ask, stickyCodingWorkbenchMemory{})
		assertOpeningTaskHostAnalysis(t, plain, ask)
		got := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{}, inquiry)
		if strings.Contains(got, "其它先不动") || !strings.Contains(got, "不改文件") || !strings.Contains(got, "回答「") {
			t.Fatalf("%q should be answered once the decision is inquiry: %q", ask, got)
		}
	}
}

func TestFallbackSoftenerNeeIsNotAQuestion(t *testing.T) {
	for _, ask := range []string{"再收紧阈值呢", "没什么要改的", "多少有点慢"} {
		got := fallbackCodingRequestRestatement(ask, stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
		if strings.Contains(got, "不改文件") || strings.Contains(got, "回答「") {
			t.Fatalf("wording became an answer: %q -> %q", ask, got)
		}
	}
	got := fallbackCodingRequestRestatement("再收紧阈值呢", stickyCodingWorkbenchMemory{SessionPlan: "系统信息查看"})
	if !strings.Contains(got, "再收紧阈值呢") || !strings.Contains(got, "系统信息查看") {
		t.Fatalf("soft follow-up dropped the ask: %q", got)
	}
}

func TestFallbackLongQuestionIsCutOnAClause(t *testing.T) {
	ask := "本程序现在支持哪些指令集，请按编译目标分别说明 x86 AVX2、AVX-512、ARM NEON、LoongArch LSX 和 LASX 在当前构建里有没有打开，以及运行时实际选了哪一档"
	if utf8.RuneCountInString(ask) <= 80 {
		t.Fatalf("fixture is not long: %d", utf8.RuneCountInString(ask))
	}
	got := fallbackCodingRequestRestatementFor(ask, stickyCodingWorkbenchMemory{SessionPlan: ask}, codingRequestDecision{Kind: codingRequestInquiry})
	if strings.Contains(got, "其它先不动") || strings.Contains(got, "在「"+ask+"」") {
		t.Fatalf("long question was echoed: %q", got)
	}
	if !strings.Contains(got, "…") || !strings.Contains(got, "不改文件") || !strings.Contains(got, "哪些指令集") {
		t.Fatalf("long question should be a cut answer: %q", got)
	}
	if strings.Count(got, "「") != strings.Count(got, "」") {
		t.Fatalf("quotes unbalanced: %q", got)
	}
	if strings.Contains(got, ask) {
		t.Fatalf("full question was pasted into the bubble: %q", got)
	}
}
