package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

func TestRunHopTaskCompilesAndExecutesSpec(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			if jsonResponse {
				t.Fatal("compiler requested JSON instead of Markdown")
			}
			return `# Spec: 任务摘要
Goal: 生成并核验摘要
Outputs:
- report % text
## Steps
1. [reason] 生成摘要
  + → report % text
2. [check] 核验摘要
  - ← report
  + → accepted % bool
  + → feedback % text
3. [exit]
`, nil
		}
		if jsonResponse && calls == 2 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCompiledHopSpecRejectsCommit(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return `# Spec: 危险规约
Goal: 不应运行
Outputs:
- report % text
## Steps
1. [commit] 提交
  + → report % text
  > ` + "```hop_python" + `
  > report = "danger"
  > ` + "```" + `
`, nil
	}}
	_, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "执行危险任务", caller)
	if err == nil || !strings.Contains(err.Error(), "commit is not allowed") {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("commit rejection retried, calls=%d", calls)
	}
}

func TestCompiledHopSpecRejectsMalformedDocument(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return "不是 HopSpec", nil
	}}
	_, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "任务", caller)
	if err == nil {
		t.Fatal("malformed compiler output was accepted")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want one rewrite", calls)
	}
}

func TestCompiledHopSpecAcceptsFullwidthTitle(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			return "# Spec：任务摘要\nGoal：生成并核验摘要\nOutputs：\n- report % text\n## Steps\n1. [reason] 生成摘要\n  + → report % text\n2. [check] 核验摘要\n  - ← report\n  + → accepted % bool\n  + → feedback % text\n3. [exit]\n", nil
		}
		if jsonResponse && calls == 2 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCompiledHopSpecKeepsGoalSectionBody(t *testing.T) {
	const task = "北京天气"
	body := "# Spec: 天气\n## Goal\n查询北京今天的天气，并给出温度和风力\n## Steps\n1. [exit]\n"
	kept := prepareCompiledHopMarkdown(body, task)
	spec, errs := hopspec.Parse(kept)
	if len(errs) != 0 || spec.Goal != "查询北京今天的天气，并给出温度和风力" {
		t.Fatalf("goal = %q errs = %v spec = %s", spec.Goal, errs, kept)
	}
	if strings.Contains(kept, "Goal: "+task) {
		t.Fatalf("task replaced the section body: %s", kept)
	}
	if err := validateCompiledHopSpec(kept); err != nil {
		t.Fatal(err)
	}
	sentence := prepareCompiledHopMarkdown("# Spec: 天气\n# 目标\n查询北京天气\n输出：温度和风力\n## Steps\n1. [exit]\n", task)
	sentenceSpec, sentenceErrs := hopspec.Parse(sentence)
	if len(sentenceErrs) != 0 || sentenceSpec.Goal != "查询北京天气\n输出：温度和风力" || strings.Contains(sentence, "Goal: "+task) {
		t.Fatalf("sentence = %q spec = %s errs = %v", sentenceSpec.Goal, sentence, sentenceErrs)
	}

	outline := prepareCompiledHopMarkdown("目标\n查询北京天气\n## 说明\n给出温度和风力\n步骤\n1. [exit]\n", task)
	outlineSpec, outlineErrs := hopspec.Parse(outline)
	if len(outlineErrs) != 0 || outlineSpec.Goal != "查询北京天气\n## 说明\n给出温度和风力" || strings.Contains(outline, "Goal: "+task) {
		t.Fatalf("outline goal = %q spec = %s errs = %v", outlineSpec.Goal, outline, outlineErrs)
	}
	if !strings.HasPrefix(outline, "# Spec: "+task+"\n") {
		t.Fatalf("outline title = %q", outline)
	}
	quoted := prepareCompiledHopMarkdown("> ## 目标\n> 查询北京天气\n> ## 说明\n> 给出温度和风力\n## 步骤\n1. [exit]\n", task)
	quotedSpec, quotedErrs := hopspec.Parse(quoted)
	if len(quotedErrs) != 0 || quotedSpec.Goal != "查询北京天气\n## 说明\n给出温度和风力" || strings.Contains(quoted, "Goal: "+task) || !strings.HasPrefix(quoted, "# Spec: "+task+"\n") {
		t.Fatalf("quoted goal = %q spec = %s errs = %v", quotedSpec.Goal, quoted, quotedErrs)
	}
	aliased := prepareCompiledHopMarkdown("## 目标/Goal。\n查询北京天气\n## 说明/补充\n给出温度和风力\n## 步骤｜Steps\n1. [exit]\n", task)
	aliasSpec, aliasErrs := hopspec.Parse(aliased)
	if len(aliasErrs) != 0 || aliasSpec.Goal != "查询北京天气\n## 说明/补充\n给出温度和风力" || strings.Contains(aliased, "Goal: "+task) || !strings.HasPrefix(aliased, "# Spec: "+task+"\n") {
		t.Fatalf("alias goal = %q spec = %s errs = %v", aliasSpec.Goal, aliased, aliasErrs)
	}
	glossed := prepareCompiledHopMarkdown("## 目标（必填）\n查询北京天气\n## 说明（补充）\n给出温度和风力\n## 步骤（按顺序）\n1. [exit]\n", task)
	glossSpec, glossErrs := hopspec.Parse(glossed)
	if len(glossErrs) != 0 || glossSpec.Goal != "查询北京天气\n## 说明（补充）\n给出温度和风力" || strings.Contains(glossed, "Goal: "+task) || !strings.HasPrefix(glossed, "# Spec: "+task+"\n") {
		t.Fatalf("gloss goal = %q spec = %s errs = %v", glossSpec.Goal, glossed, glossErrs)
	}
	closed := prepareCompiledHopMarkdown("## 目标 查询北京天气\n## 【说明】\n给出温度和风力\n## 步骤 ##\n1. [exit]\n", task)
	closedSpec, closedErrs := hopspec.Parse(closed)
	if len(closedErrs) != 0 || closedSpec.Goal != "查询北京天气\n## 【说明】\n给出温度和风力" || strings.Contains(closed, "Goal: "+task) || !strings.HasPrefix(closed, "# Spec: "+task+"\n") {
		t.Fatalf("closed goal = %q spec = %s errs = %v", closedSpec.Goal, closed, closedErrs)
	}
	atxNumbered := prepareCompiledHopMarkdown("## 1. 目标\n查询北京天气\n## 2. 说明\n给出温度和风力\n## 3. 步骤\n1. [exit]\n", task)
	atxSpec, atxErrs := hopspec.Parse(atxNumbered)
	if len(atxErrs) != 0 || atxSpec.Goal != "查询北京天气\n## 2. 说明\n给出温度和风力" || strings.Contains(atxNumbered, "Goal: "+task) || !strings.HasPrefix(atxNumbered, "# Spec: "+task+"\n") {
		t.Fatalf("atx numbered goal = %q spec = %s errs = %v", atxSpec.Goal, atxNumbered, atxErrs)
	}
	scalar := prepareCompiledHopMarkdown("Goal: |\n查询北京天气\n## 说明\n给出温度和风力\n## 步骤\n1. [exit]\n", task)
	scalarSpec, scalarErrs := hopspec.Parse(scalar)
	if len(scalarErrs) != 0 || scalarSpec.Goal != "查询北京天气\n## 说明\n给出温度和风力" || strings.Contains(scalar, "Goal: "+task) || !strings.HasPrefix(scalar, "# Spec: "+task+"\n") {
		t.Fatalf("scalar goal = %q spec = %s errs = %v", scalarSpec.Goal, scalar, scalarErrs)
	}
	wrappedGoal := prepareCompiledHopMarkdown("Goal: \"查询北京天气\"\n## 步骤\n1. [exit]\n", task)
	wrappedSpec, wrappedErrs := hopspec.Parse(wrappedGoal)
	if len(wrappedErrs) != 0 || wrappedSpec.Goal != "查询北京天气" || strings.Contains(wrappedGoal, "Goal: "+task) || !strings.HasPrefix(wrappedGoal, "# Spec: "+task+"\n") {
		t.Fatalf("wrapped goal = %q spec = %s errs = %v", wrappedSpec.Goal, wrappedGoal, wrappedErrs)
	}
	quotedTitle := prepareCompiledHopMarkdown("> # Spec: 天气\nGoal: |\n查询北京天气\n## 步骤\n1. [exit]\n", task)
	quotedTitleSpec, quotedTitleErrs := hopspec.Parse(quotedTitle)
	if len(quotedTitleErrs) != 0 || quotedTitleSpec.Title != "天气" || quotedTitleSpec.Goal != "查询北京天气" || strings.Contains(quotedTitle, "Goal: "+task) || strings.Contains(quotedTitle, "# Spec: "+task) {
		t.Fatalf("quoted title = %+v spec = %s errs = %v", quotedTitleSpec, quotedTitle, quotedTitleErrs)
	}
	coded := prepareCompiledHopMarkdown("`目标`\n查询北京天气\n## `说明`\n给出温度和风力\n`步骤`\n1. [exit]\n", task)
	codedSpec, codedErrs := hopspec.Parse(coded)
	if len(codedErrs) != 0 || codedSpec.Goal != "查询北京天气\n## `说明`\n给出温度和风力" || strings.Contains(coded, "Goal: "+task) || !strings.HasPrefix(coded, "# Spec: "+task+"\n") {
		t.Fatalf("coded goal = %q spec = %s errs = %v", codedSpec.Goal, coded, codedErrs)
	}
	bracketed := prepareCompiledHopMarkdown("【目标】：查询北京天气\n【步骤】\n1. [exit]\n", task)
	bracketSpec, bracketErrs := hopspec.Parse(bracketed)
	if len(bracketErrs) != 0 || bracketSpec.Goal != "查询北京天气" || strings.Contains(bracketed, "Goal: "+task) || !strings.HasPrefix(bracketed, "# Spec: "+task+"\n") {
		t.Fatalf("bracket goal = %q spec = %s errs = %v", bracketSpec.Goal, bracketed, bracketErrs)
	}
	numbered := prepareCompiledHopMarkdown("1. 目标\n查询北京天气\n- 步骤\n1. [exit]\n", task)
	numberedSpec, numberedErrs := hopspec.Parse(numbered)
	if len(numberedErrs) != 0 || numberedSpec.Goal != "查询北京天气" || strings.Contains(numbered, "Goal: "+task) || !strings.HasPrefix(numbered, "# Spec: "+task+"\n") {
		t.Fatalf("numbered goal = %q spec = %s errs = %v", numberedSpec.Goal, numbered, numberedErrs)
	}

	longTask := strings.Repeat("天气", 30)
	want := longTask + "的正文还更长一些，不能被标题截断替换"
	longBody := "## 目标\n" + want + "\n## 步骤\n1. [exit]\n"
	normalized := prepareCompiledHopMarkdown(longBody, longTask)
	parsed, parseErrs := hopspec.Parse(normalized)
	if len(parseErrs) != 0 || parsed.Goal != want {
		t.Fatalf("goal = %q errs = %v spec = %s", parsed.Goal, parseErrs, normalized)
	}
	if err := validateCompiledHopSpec(normalized); err != nil {
		t.Fatal(err)
	}

	bare := prepareCompiledHopMarkdown("# Spec: 天气\n## Goal\n## Steps\n1. [exit]\n", longTask)
	if !strings.Contains(bare, "Goal: "+longTask) {
		t.Fatalf("bare = %q", bare)
	}
	if err := validateCompiledHopSpec(bare); err != nil {
		t.Fatal(err)
	}

	missing := prepareCompiledHopMarkdown("# Spec: 天气\n## Steps\n1. [exit]\n", "北京  天气\n怎么样")
	if !strings.Contains(missing, "# Spec: 天气\nGoal: 北京 天气 怎么样\n") {
		t.Fatalf("filled = %q", missing)
	}
	filled, fillErrs := hopspec.Parse(missing)
	if len(fillErrs) != 0 || filled.Goal != "北京 天气 怎么样" {
		t.Fatalf("filled goal = %q errs = %v", filled.Goal, fillErrs)
	}
	if err := validateCompiledHopSpec(missing); err != nil {
		t.Fatal(err)
	}

	none := prepareCompiledHopMarkdown("# Spec: 天气\n## Steps\n1. [exit]\n", " \n\t")
	if strings.Contains(none, "Goal:") {
		t.Fatalf("invented goal: %s", none)
	}
	if err := validateCompiledHopSpec(none); err == nil || !strings.Contains(err.Error(), "MISSING_GOAL") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompiledHopSpecRepairsMissingTitle(t *testing.T) {
	const task = "查询北京今天的天气并给出一句结论"
	spec := normalizeCompiledHopMarkdown("Goal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [exit]\n", task)
	if strings.Contains(spec, "MISSING_SPEC_TITLE") || !strings.Contains(spec, "Goal: 查询天气") {
		t.Fatalf("spec=%q", spec)
	}
	if !strings.HasPrefix(spec, "# Spec: 查询北京今天的天气并给出一句结论\n") {
		t.Fatalf("spec=%q", spec)
	}
	headed := normalizeCompiledHopMarkdown("## 目标：查询天气\n## 步骤\n1. [exit]\n", "查询天气")
	if !strings.HasPrefix(headed, "# Spec: 查询天气\n") || !strings.Contains(headed, "## 目标：查询天气") {
		t.Fatalf("headed=%q", headed)
	}
	bold := normalizeCompiledHopMarkdown("**目标：** 查询天气\n## 步骤\n1. [exit]\n", "查询天气")
	if !strings.HasPrefix(bold, "# Spec: 查询天气\n") || !strings.Contains(bold, "查询天气") {
		t.Fatalf("bold=%q", bold)
	}
	long := strings.Repeat("天气", 30)
	title := hopSpecTitleFromTask(long)
	if len([]rune(title)) != 40 {
		t.Fatalf("title runes=%d", len([]rune(title)))
	}
}

func TestNormalizeCompiledHopMarkdownExtractsFencedSpec(t *testing.T) {
	raw := "下面是规约：\n```markdown\n# Spec：北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [exit]\n```\n以上。"
	spec := normalizeCompiledHopMarkdown(raw, "北京天气")
	if !strings.HasPrefix(spec, "# Spec：北京天气\n") || strings.Contains(spec, "```") || strings.Contains(spec, "以上") || !strings.Contains(spec, "Goal: 查询天气") {
		t.Fatalf("spec=%q", spec)
	}
	withPython := "说明\n```markdown\n# Spec: 任务\nGoal: 做\nOutputs:\n- report % text\n## Steps\n1. [act] 运行\n```hop_python\nreport = \"a:b\"\n```\n2. [exit]\n```\n以上。\n"
	spec = normalizeCompiledHopMarkdown(withPython, "任务")
	if !strings.Contains(spec, "```hop_python") || !strings.Contains(spec, "report = \"a:b\"") || !strings.Contains(spec, "2. [exit]") || strings.Contains(spec, "以上") {
		t.Fatalf("python spec=%q", spec)
	}
	thought := "<think>\n# Spec: 藏在思考里\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [exit]\n</think>\n我来解释。"
	spec = normalizeCompiledHopMarkdown(thought, "任务")
	if !strings.HasPrefix(spec, "# Spec: 藏在思考里\n") || strings.Contains(spec, "我来解释") || strings.Contains(spec, "<think>") {
		t.Fatalf("thought spec=%q", spec)
	}
}

func TestHopSpecFromModelFieldsPrefersStepsOverEmptyHeading(t *testing.T) {
	got := hopSpecFromModelFields(
		"# Spec: 北京天气\nGoal: 查询\n",
		"# Spec: 北京天气\nGoal: 查询\n",
		"# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [exit]\n",
	)
	if !strings.Contains(got, "1. [exit]") {
		t.Fatalf("steps in reasoning hidden by heading stub: %q", got)
	}
	got = hopSpecFromModelFields(
		"# Spec: 正文\nGoal: 新\n## Steps\n1. [exit]\n",
		"raw",
		"# Spec: 旧\nGoal: 旧\n## Steps\n1. [reason] 旧\n",
	)
	if !strings.Contains(got, "# Spec: 正文") || strings.Contains(got, "旧") {
		t.Fatalf("content steps lost: %q", got)
	}
	fenced := normalizeCompiledHopMarkdown("# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n```\n1. [exit]\n```\n", "北京天气")
	if !strings.Contains(fenced, "1. [exit]") {
		t.Fatalf("fenced steps dropped: %q", fenced)
	}
	afterThink := normalizeCompiledHopMarkdown("<think>\n# Spec: 草稿\nGoal: 草稿\n</think>\n# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [exit]\n", "北京天气")
	if !strings.Contains(afterThink, "1. [exit]") || strings.Contains(afterThink, "草稿") {
		t.Fatalf("steps after think dropped: %q", afterThink)
	}
	proseFirst := hopSpecFromModelFields(
		"# Spec: 正文\nGoal: 查询\n## Steps\n1. 查询天气\n",
		"raw",
		"# Spec: 长推理\nGoal: 查询\n## Steps\n1. 先想\n2. 再想\n3. 还想\n",
	)
	if !strings.Contains(proseFirst, "1. 查询天气") || strings.Contains(proseFirst, "长推理") {
		t.Fatalf("long reasoning hid the content steps: %q", proseFirst)
	}
	revised := normalizeCompiledHopMarkdown("# Spec: 草稿\nGoal: 草稿\n## Steps\n1. [exit]\n# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n", "北京天气")
	if !strings.Contains(revised, "1. [reason] 查询") || strings.Contains(revised, "草稿") {
		t.Fatalf("one-step draft hid the revision: %q", revised)
	}
	tied := normalizeCompiledHopMarkdown("# Spec: 第一份\nGoal: 一\n## Steps\n1. [exit]\n# Spec: 第二份\nGoal: 二\n## Steps\n1. [exit]\n", "任务")
	if !strings.Contains(tied, "第一份") || strings.Contains(tied, "第二份") {
		t.Fatalf("tie=%q", tied)
	}
	work := normalizeCompiledHopMarkdown("# Spec: 草稿\nGoal: 草稿\n## Steps\n1. [exit]\n# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [act] 查询\n", "北京天气")
	if !strings.Contains(work, "[act]") || strings.Contains(work, "草稿") {
		t.Fatalf("exit draft hid the work step: %q", work)
	}
	afterCloser := normalizeCompiledHopMarkdown("<think>\n# Spec: 草稿\nGoal: 草稿\n## Steps\n1. [exit]\n</think>\nGoal: 查询天气\n## Steps\n1. [reason] 查询\n2. [exit]\n", "北京天气")
	if !strings.Contains(afterCloser, "1. [reason] 查询") || strings.Contains(afterCloser, "草稿") {
		t.Fatalf("think draft hid the later steps: %q", afterCloser)
	}
	longDraft := normalizeCompiledHopMarkdown("<think>\n# Spec: 草稿\n## Steps\n1. [act] 想\n2. [act] 再想\n3. [act] 还想\n</think>\n# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n", "北京天气")
	if !strings.Contains(longDraft, "1. [reason] 查询") || strings.Contains(longDraft, "想") {
		t.Fatalf("long think draft hid the spec: %q", longDraft)
	}
	fragment := "<think>\n# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n</think>\n步骤写成 1. [act] 摘要\n"
	if got := hopSpecFromModelFields(fragment, "", ""); !strings.Contains(got, "1. [reason] 查询") {
		t.Fatalf("visible fragment hid the spec: %q", got)
	}
	kept := normalizeCompiledHopMarkdown(fragment, "北京天气")
	if !strings.Contains(kept, "1. [reason] 查询") || strings.Contains(kept, "摘要") {
		t.Fatalf("fragment replaced the spec: %q", kept)
	}
	beforeHeading := normalizeCompiledHopMarkdown("# Spec: 北京天气\n1. [act] 格式\n## Steps\n```\n1. [reason] 查询\n2. [exit]\n```\n", "北京天气")
	if !strings.Contains(beforeHeading, "1. [reason] 查询") || !strings.Contains(beforeHeading, "2. [exit]") {
		t.Fatalf("step before the heading dropped the fenced steps: %q", beforeHeading)
	}
	proseRegions := normalizeCompiledHopMarkdown("# Spec: 正文\nGoal: 查询\n## Steps\n1. 查询天气\n# Spec: 长推理\nGoal: 查询\n## Steps\n1. 先想\n2. 再想\n3. 还想\n", "正文")
	if !strings.Contains(proseRegions, "1. 查询天气") || strings.Contains(proseRegions, "长推理") {
		t.Fatalf("long prose region hid the first steps: %q", proseRegions)
	}
	outsideFragment := normalizeCompiledHopMarkdown("<think>\n# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n</think>\n# Spec: 说明\n1. [act] 格式\n", "北京天气")
	if !strings.Contains(outsideFragment, "1. [reason] 查询") || strings.Contains(outsideFragment, "格式") {
		t.Fatalf("outside fragment hid the spec: %q", outsideFragment)
	}
	outsideGoal := normalizeCompiledHopMarkdown("<think>\n# Spec: 草稿\n## Steps\n1. [act] 想\n2. [act] 再想\n3. [exit]\n</think>\n# Spec: 北京天气\nGoal: 查询\n1. [reason] 查询\n2. [exit]\n", "北京天气")
	if !strings.Contains(outsideGoal, "1. [reason] 查询") || strings.Contains(outsideGoal, "想") {
		t.Fatalf("inside draft hid the outside spec: %q", outsideGoal)
	}
	pythonHeading := normalizeCompiledHopMarkdown("# Spec: 任务\nGoal: 做\n## Steps\n1. [act] 运行\n```hop_python\n# Spec: 注释\nreport = \"a\"\n```\n2. [exit]\n", "任务")
	if !strings.Contains(pythonHeading, "1. [act] 运行") || !strings.Contains(pythonHeading, "2. [exit]") || !strings.Contains(pythonHeading, "report = \"a\"") {
		t.Fatalf("hop_python heading split the spec: %q", pythonHeading)
	}
	codeFence := normalizeCompiledHopMarkdown("# Spec: 北京天气\nGoal: 查询\n## Steps\n```hop_python\n## 标题\n1. [act] 假\n```\n```\n1. [reason] 查询\n2. [exit]\n```\n", "北京天气")
	if !strings.Contains(codeFence, "1. [reason] 查询") || !strings.Contains(codeFence, "2. [exit]") {
		t.Fatalf("hop_python line dropped the fenced steps: %q", codeFence)
	}
	codeDraft := normalizeCompiledHopMarkdown("# Spec: 草稿\nGoal: 草稿\n## Steps\n```hop_python\n1. [act] 想\n2. [act] 再想\n3. [act] 还想\n```\n# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n", "北京天气")
	if !strings.Contains(codeDraft, "1. [reason] 查询") || strings.Contains(codeDraft, "想") {
		t.Fatalf("hop_python draft hid the spec: %q", codeDraft)
	}
	codeField := hopSpecFromModelFields("# Spec: 草稿\nGoal: 草稿\n## Steps\n```hop_python\n1. [act] 假\n```\n", "", "# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n")
	if !strings.Contains(codeField, "1. [reason] 查询") || strings.Contains(codeField, "假") {
		t.Fatalf("hop_python field hid the spec: %q", codeField)
	}
	continued := normalizeCompiledHopMarkdown("# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n<think>\n2. [act] 获取\n3. [exit]\n</think>\n", "北京天气")
	if !strings.Contains(continued, "2. [act] 获取") || !strings.Contains(continued, "3. [exit]") || strings.Contains(continued, "<think>") {
		t.Fatalf("think continuation dropped: %q", continued)
	}
	draftSteps := hopSpecFromModelFields("# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n<think>\n## Steps\n1. [act] 想\n</think>\n", "", "")
	if strings.Contains(draftSteps, "想") || strings.Contains(draftSteps, "<think>") || !strings.Contains(draftSteps, "2. [exit]") {
		t.Fatalf("think draft merged into the spec: %q", draftSteps)
	}
	example := normalizeCompiledHopMarkdown("# Spec: 北京天气\nGoal: 查询\n## Steps\n1. [reason] 查询\n2. [exit]\n```\n1. [act] 这是例子\n", "北京天气")
	if strings.Contains(example, "[act]") || !strings.Contains(example, "[reason]") {
		t.Fatalf("example step kept: %q", example)
	}
	prose := hopSpecFromModelFields("# Spec: 北京天气\nGoal: 查询\n", "# Spec: 北京天气\nGoal: 查询\n", "# Spec: 北京天气\nGoal: 查询\n## Steps\n1. 查询天气\n")
	if !strings.Contains(prose, "1. 查询天气") {
		t.Fatalf("unparsed steps hidden by heading stub: %q", prose)
	}
	err := validateCompiledHopSpec(prose)
	if err == nil || !strings.Contains(err.Error(), "1. 查询天气") || !strings.Contains(err.Error(), "1. [act]") {
		t.Fatalf("err=%v", err)
	}
}

func TestHopSpecFromModelFieldsPrefersHeading(t *testing.T) {
	got := hopSpecFromModelFields("可见回答", "可见回答", "<think>\n# Spec: 藏在思考里\n</think>")
	if !strings.Contains(got, "# Spec: 藏在思考里") {
		t.Fatalf("reasoning heading dropped: %q", got)
	}
	got = hopSpecFromModelFields("# Spec: 正文", "raw", "# Spec: 旧思考")
	if !strings.HasPrefix(strings.TrimSpace(got), "# Spec: 正文") {
		t.Fatalf("content heading lost: %q", got)
	}
	got = hopSpecFromModelFields("<think>only</think>答案", "<think>only</think>答案", "thought")
	if got != "答案" {
		t.Fatalf("visible=%q", got)
	}
	got = hopSpecFromModelFields("好的", "好的", "Goal: 查询天气\n## Steps\n1. [exit]\n")
	if !strings.Contains(got, "Goal: 查询天气") || !strings.Contains(got, "## Steps") {
		t.Fatalf("goal document lost behind prose: %q", got)
	}
	got = hopSpecFromModelFields("好的", "好的", "<think>\nGoal: 查询天气\n## Steps\n1. [exit]\n</think>")
	if !strings.Contains(got, "Goal: 查询天气") || !strings.Contains(got, "## Steps") {
		t.Fatalf("goal document inside think was dropped: %q", got)
	}
	spec := normalizeCompiledHopMarkdown(got, "查询天气")
	if !strings.HasPrefix(spec, "# Spec: 查询天气\n") || !strings.Contains(spec, "Goal: 查询天气") {
		t.Fatalf("spec=%q", spec)
	}
}

func TestCompiledHopSpecRewriteRetriesOnce(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			return "不是 HopSpec", nil
		}
		if !jsonResponse && calls == 2 {
			return `# Spec: 任务摘要
Goal: 生成并核验摘要
Outputs:
- report % text
## Steps
1. [reason] 生成摘要
  + → report % text
2. [check] 核验摘要
  - ← report
  + → accepted % bool
  + → feedback % text
3. [exit]
`, nil
		}
		if jsonResponse && calls == 3 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestRepairMissingCheckVerdicts(t *testing.T) {
	raw := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  + → report % text\n2. [act] 整理\n3. [check] 核验\n4. [exit]\n"
	spec := repairMissingCheckVerdicts(raw)
	if err := validateCompiledHopSpec(spec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spec, "3. [check] 核验\n  + → accepted % bool\n4. [exit]") {
		t.Fatalf("spec=%q", spec)
	}
	if again := repairMissingCheckVerdicts(spec); again != spec {
		t.Fatalf("duplicated verdict: %q", again)
	}
	synonym := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  + → report % text\n2. [check] 核验\n  + -> accepted % boolean\n3. [exit]\n"
	kept := repairMissingCheckVerdicts(synonym)
	if kept != synonym {
		t.Fatalf("synonym verdict was rewritten: %q", kept)
	}
	if err := validateCompiledHopSpec(kept); err != nil {
		t.Fatal(err)
	}
	wide := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [confirm] 确认\n  ＋ → 通过 % 布尔\n2. [exit]\n"
	if repairMissingCheckVerdicts(wide) != wide {
		t.Fatal("fullwidth verdict was rewritten")
	}
	fenced := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\nnote = \"ok\"\n```\n3. [exit]\n"
	if repairMissingCheckVerdicts(fenced) != fenced {
		t.Fatal("body without a bool literal gained a verdict")
	}
	if err := validateCompiledHopSpec(fenced); err == nil || !strings.Contains(err.Error(), "CHECK_BOOL_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
	assigned := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\nnote = \"ok\"\naccepted = True\n```\n3. [exit]\n"
	assigned = repairMissingCheckVerdicts(assigned)
	if !strings.Contains(assigned, "```\n  + → accepted % bool\n3. [exit]") {
		t.Fatalf("assigned=%q", assigned)
	}
	if err := validateCompiledHopSpec(assigned); err != nil {
		t.Fatal(err)
	}
	other := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\nok = false\n```\n3. [exit]\n"
	other = repairMissingCheckVerdicts(other)
	if !strings.Contains(other, "+ → ok % bool") || strings.Contains(other, "+ → accepted % bool") {
		t.Fatalf("other=%q", other)
	}
	blocked := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\nreport = true\n```\n3. [exit]\n"
	if repairMissingCheckVerdicts(blocked) != blocked {
		t.Fatal("bool literal reused a text name")
	}
	compared := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\naccepted = report != \"\"\n```\n3. [exit]\n"
	comparedOnce := repairMissingCheckVerdicts(compared)
	if !strings.Contains(comparedOnce, "```\n  - ← report\n  + → accepted % bool\n3. [exit]") {
		t.Fatalf("compared=%q", comparedOnce)
	}
	if again := repairMissingCheckVerdicts(comparedOnce); again != comparedOnce {
		t.Fatalf("comparison verdict duplicated: %q", again)
	}
	if err := validateCompiledHopSpec(comparedOnce); err != nil {
		t.Fatal(err)
	}
	quoted := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\nnote = \"a != b\"\n```\n3. [exit]\n"
	if repairMissingCheckVerdicts(quoted) != quoted {
		t.Fatal("comparison inside a string became a verdict")
	}
	commented := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\naccepted = report != \"\"  # 空则失败\n```\n3. [exit]\n"
	commentedOnce := repairMissingCheckVerdicts(commented)
	if !strings.Contains(commentedOnce, "```\n  - ← report\n  + → accepted % bool\n3. [exit]") || strings.Contains(commentedOnce, "- ← 空") {
		t.Fatalf("commented=%q", commentedOnce)
	}
	decoy := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\nnote = \"a # b\"  # real != fake\n```\n3. [exit]\n"
	if repairMissingCheckVerdicts(decoy) != decoy {
		t.Fatal("comparison inside a comment became a verdict")
	}
	passed := "# Spec: 判定\nGoal: 核验\n## Steps\n1. [check] 核验\n```hop_python\npassed = True  # 通过\n```\n2. [exit]\n"
	passed = repairMissingCheckVerdicts(passed)
	if !strings.Contains(passed, "+ → passed % bool") || strings.Contains(passed, "+ → accepted % bool") || strings.Contains(passed, "- ← 通过") {
		t.Fatalf("passed=%q", passed)
	}
	wired := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n```hop_python\naccepted = len(report) > 0\n```\n3. [exit]\n"
	wiredOnce := repairMissingCheckVerdicts(wired)
	if !strings.Contains(wiredOnce, "```\n  - ← report\n3. [exit]") || strings.Count(wiredOnce, "+ → accepted % bool") != 1 {
		t.Fatalf("wired=%q", wiredOnce)
	}
	if again := repairMissingCheckVerdicts(wiredOnce); again != wiredOnce {
		t.Fatalf("input duplicated: %q", again)
	}
	unknown := "# Spec: 判定\nGoal: 核验\n## Steps\n1. [check] 核验\n```hop_python\naccepted = missing != \"\"\n```\n2. [exit]\n"
	unknown = repairMissingCheckVerdicts(unknown)
	if strings.Contains(unknown, "- ← missing") || !strings.Contains(unknown, "+ → accepted % bool") {
		t.Fatalf("unknown=%q", unknown)
	}
	chained := "# Spec: 判定\nGoal: 核验\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 非空\n```hop_python\naccepted = report != \"\"\n```\n3. [check] 沿用\n```hop_python\nstill = accepted and report != \"\"\n```\n4. [exit]\n"
	chainedOnce := repairMissingCheckVerdicts(chained)
	if !strings.Contains(chainedOnce, "```\n  - ← report\n  + → accepted % bool\n3. [check]") {
		t.Fatalf("chained first=%q", chainedOnce)
	}
	if !strings.Contains(chainedOnce, "```\n  - ← accepted\n  - ← report\n  + → still % bool\n4. [exit]") {
		t.Fatalf("chained second=%q", chainedOnce)
	}
	if again := repairMissingCheckVerdicts(chainedOnce); again != chainedOnce {
		t.Fatalf("chained duplicated: %q", again)
	}
	if err := validateCompiledHopSpec(chainedOnce); err != nil {
		t.Fatal(err)
	}
	field := "# Spec: 判定\nGoal: 核验\nOutputs:\n- item % text\n- city % text\n## Steps\n1. [act] 取数\n  + → item % text\n  + → city % text\n2. [check] 核验\n```hop_python\naccepted = item.city != \"\"\n```\n3. [exit]\n"
	field = repairMissingCheckVerdicts(field)
	if strings.Contains(field, "- ← city") || !strings.Contains(field, "- ← item") || !strings.Contains(field, "+ → accepted % bool") {
		t.Fatalf("field=%q", field)
	}
	identity := "# Spec: 判定\nGoal: 核验\nOutputs:\n- report % text\n## Steps\n1. [act] 取数\n  + → report % text\n2. [check] 核验\n```hop_python\naccepted = report is None\n```\n3. [exit]\n"
	identityOnce := repairMissingCheckVerdicts(identity)
	if strings.Contains(identityOnce, "- ← is") || !strings.Contains(identityOnce, "- ← report") || !strings.Contains(identityOnce, "+ → accepted % bool") {
		t.Fatalf("identity=%q", identityOnce)
	}
	if err := validateCompiledHopSpec(identityOnce); err != nil {
		t.Fatal(err)
	}
	named := "# Spec: 名\nGoal: 核验\n## Steps\n1. [reason] 写\n  + → accepted % text\n  + → verdict % text\n2. [subtask] 组\n  2.1. [check] 核验\n3. [exit]\n"
	named = repairMissingCheckVerdicts(named)
	if !strings.Contains(named, "2.1. [check] 核验\n  + → accepted_2_1 % bool\n3. [exit]") {
		t.Fatalf("named=%q", named)
	}
	if err := validateCompiledHopSpec(named); err != nil {
		t.Fatal(err)
	}
	hand := "# Spec: 坏\nGoal: 验证\nOutputs:\n- feedback % text\n## Steps\n1. [check] 缺少布尔判定\n  + → feedback % text\n2. [exit]\n"
	if err := validateCompiledHopSpec(hand); err == nil || !strings.Contains(err.Error(), "CHECK_BOOL_REQUIRED") {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileHopSpecRepairsMissingCheckVerdict(t *testing.T) {
	calls := 0
	var systems []string
	caller := &guiHopJITCaller{invoke: func(_ context.Context, messages []interface{}, jsonResponse bool) (string, error) {
		calls++
		if jsonResponse {
			t.Fatal("compiler requested JSON")
		}
		if len(messages) > 0 {
			if msg, ok := messages[0].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					systems = append(systems, content)
				}
			}
		}
		if calls == 1 {
			return "不是 HopSpec", nil
		}
		return "# Spec: 整理笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [reason] 整理\n  + → report % text\n2. [act] 排版\n3. [check] 核验\n4. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "整理笔记", caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if !strings.Contains(spec, "+ → accepted % bool") || strings.Contains(spec, "web_search") || !strings.Contains(spec, "[reason] 整理") {
		t.Fatalf("spec=%q", spec)
	}
	if err := validateCompiledHopSpec(spec); err != nil {
		t.Fatal(err)
	}
	if len(systems) != 2 {
		t.Fatalf("prompts=%d", len(systems))
	}
	for _, system := range systems {
		if !strings.Contains(system, "+ → accepted % bool") || !strings.Contains(system, "- ← 名字") || !strings.Contains(system, "- 工具: web_search") {
			t.Fatalf("prompt=%q", system)
		}
	}
}

func TestCompiledHopSpecRejectsActFreeWithoutTools(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		if calls == 1 {
			return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act free] 查询\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n", nil
		}
		return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act free] 查询\n  - 工具: web_search\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "整理笔记", caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d spec=%q err=%v", calls, spec, err)
	}
	if !strings.Contains(spec, "工具: web_search") {
		t.Fatalf("spec=%q", spec)
	}
}

func TestCompileHopSpecRepairsWeatherActTools(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d spec=%q", calls, spec)
	}
	if again := repairLiveLookupTools(spec, "北京天气"); again != spec {
		t.Fatalf("tool duplicated: %q", again)
	}
	parsed, parseErrs := hopspec.Parse(spec)
	if len(parseErrs) != 0 || len(parsed.Steps) < 1 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" {
		t.Fatalf("tools=%v errs=%v", parsed.Steps[0].Tools, parseErrs)
	}
	kept := repairLiveLookupTools("# Spec: 摘要\nGoal: 整理\n## Steps\n1. [act] 整理\n  + → report % text\n2. [exit]\n", "整理笔记")
	if strings.Contains(kept, "web_search") {
		t.Fatalf("unrelated task gained a tool: %q", kept)
	}
	wide := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询\n  － 工具: web_fetch\n  + → report % text\n2. [exit]\n"
	if repairLiveLookupTools(wide, "北京天气") != wide {
		t.Fatal("existing whitelist was rewritten")
	}
}

func TestCompileHopSpecRewritesWeatherBodyWithoutTools(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		if calls == 1 {
			return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"晴\"\n  > " + "```" + "\n2. [exit]\n", nil
		}
		return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n2. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || strings.Count(spec, "工具: web_search") != 1 || strings.Contains(spec, "report = \"晴\"") {
		t.Fatalf("calls=%d spec=%q", calls, spec)
	}
	parsed, errs := hopspec.Parse(spec)
	if len(errs) != 0 || len(parsed.Steps) < 1 || strings.TrimSpace(parsed.Steps[0].Body) != "" || len(parsed.Steps[0].Tools) != 1 {
		t.Fatalf("body=%q tools=%v errs=%v", parsed.Steps[0].Body, parsed.Steps[0].Tools, errs)
	}
}

func TestCompileHopSpecAttachesSearchToTheLookupAct(t *testing.T) {
	lookup := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n2. [act] 整理\n  + → note % text\n3. [exit]\n"
	got := repairLiveLookupTools(lookup, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) != 3 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || len(parsed.Steps[1].Tools) != 0 {
		t.Fatalf("lookup tools=%v %v errs=%v spec=%q", parsed.Steps[0].Tools, parsed.Steps[1].Tools, errs, got)
	}
	if again := repairLiveLookupTools(got, "北京天气"); again != got {
		t.Fatalf("tool duplicated: %q", again)
	}

	saving := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询天气\n  + → report % text\n2. [act] 保存\n  - 工具: write_file\n  + → note % text\n3. [exit]\n"
	saved := repairLiveLookupTools(saving, "北京天气")
	parsed, errs = hopspec.Parse(saved)
	if len(errs) != 0 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || len(parsed.Steps[1].Tools) != 1 || parsed.Steps[1].Tools[0] != "write_file" {
		t.Fatalf("saving tools=%v %v spec=%q", parsed.Steps[0].Tools, parsed.Steps[1].Tools, saved)
	}

	onCheck := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询\n  + → report % text\n2. [check] 核验\n  - 工具: web_search\n  + → accepted % bool\n3. [exit]\n"
	checked := repairLiveLookupTools(onCheck, "北京天气")
	parsed, errs = hopspec.Parse(checked)
	if len(errs) != 0 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" {
		t.Fatalf("check whitelist did not reach the act: %v spec=%q", parsed.Steps[0].Tools, checked)
	}

	both := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询天气\n  + → report % text\n2. [act] 查询新闻\n  + → note % text\n3. [exit]\n"
	repairedBoth := repairLiveLookupTools(both, "北京天气")
	parsedBoth, bothErrs := hopspec.Parse(repairedBoth)
	if len(bothErrs) != 0 || len(parsedBoth.Steps) < 2 || len(parsedBoth.Steps[0].Tools) != 1 || parsedBoth.Steps[0].Tools[0] != "web_search" || len(parsedBoth.Steps[1].Tools) != 1 || parsedBoth.Steps[1].Tools[0] != "web_search" {
		t.Fatalf("two lookup acts tools=%v %v spec=%q", parsedBoth.Steps[0].Tools, parsedBoth.Steps[1].Tools, repairedBoth)
	}
	if err := liveLookupToolsRequired(repairedBoth, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(repairedBoth, "北京天气"); again != repairedBoth {
		t.Fatalf("tools duplicated: %q", again)
	}

	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return lookup, nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 1 || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}
}

func TestCompileHopSpecRewritesWhenLookupBodyDoesNotSearch(t *testing.T) {
	fabricated := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"晴\"\n  > " + "```" + "\n2. [act] 整理\n  + → note % text\n3. [exit]\n"
	if err := liveLookupToolsRequired(fabricated, "北京天气"); err == nil {
		t.Fatal("a fabricated body counted as a search")
	}
	gotFabricated := repairLiveLookupTools(fabricated, "北京天气")
	parsedFabricated, fabricatedErrs := hopspec.Parse(gotFabricated)
	if len(fabricatedErrs) != 0 || len(parsedFabricated.Steps) < 2 || strings.TrimSpace(parsedFabricated.Steps[0].Body) != "" || len(parsedFabricated.Steps[0].Tools) != 1 || parsedFabricated.Steps[0].Tools[0] != "web_search" || len(parsedFabricated.Steps[1].Tools) != 0 {
		t.Fatalf("fabricated tools=%v %v body=%q spec=%q", parsedFabricated.Steps[0].Tools, parsedFabricated.Steps[1].Tools, parsedFabricated.Steps[0].Body, gotFabricated)
	}
	if err := liveLookupToolsRequired(gotFabricated, "北京天气"); err != nil {
		t.Fatal(err)
	}
	listed := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"晴\"\n  > " + "```" + "\n2. [exit]\n"
	if err := liveLookupToolsRequired(listed, "北京天气"); err == nil {
		t.Fatal("unused whitelist on a body counted as a search")
	}
	gotListed := repairLiveLookupTools(listed, "北京天气")
	parsedListed, listedErrs := hopspec.Parse(gotListed)
	if len(listedErrs) != 0 || len(parsedListed.Steps) < 1 || strings.TrimSpace(parsedListed.Steps[0].Body) != "" || len(parsedListed.Steps[0].Tools) != 1 || parsedListed.Steps[0].Tools[0] != "web_search" || strings.Count(gotListed, "工具: web_search") != 1 {
		t.Fatalf("listed tools=%v body=%q spec=%q", parsedListed.Steps[0].Tools, parsedListed.Steps[0].Body, gotListed)
	}
	if err := liveLookupToolsRequired(gotListed, "北京天气"); err != nil {
		t.Fatal(err)
	}
	quoted := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"web_search(query: x)\"\n  > " + "```" + "\n2. [exit]\n"
	if err := liveLookupToolsRequired(quoted, "北京天气"); err == nil {
		t.Fatal("a quoted call counted as a search")
	}
	gotQuoted := repairLiveLookupTools(quoted, "北京天气")
	parsedQuoted, quotedErrs := hopspec.Parse(gotQuoted)
	if len(quotedErrs) != 0 || len(parsedQuoted.Steps) < 1 || strings.TrimSpace(parsedQuoted.Steps[0].Body) != "" || len(parsedQuoted.Steps[0].Tools) != 1 || parsedQuoted.Steps[0].Tools[0] != "web_search" {
		t.Fatalf("quoted tools=%v body=%q spec=%q", parsedQuoted.Steps[0].Tools, parsedQuoted.Steps[0].Body, gotQuoted)
	}

	calling := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\")\n  > " + "```" + "\n2. [act] 整理\n  + → note % text\n3. [exit]\n"
	declared := repairLiveLookupTools(calling, "北京天气")
	parsed, errs := hopspec.Parse(declared)
	if len(errs) != 0 || len(parsed.Steps) < 2 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || len(parsed.Steps[1].Tools) != 0 {
		t.Fatalf("declared tools=%v %v errs=%v spec=%q", parsed.Steps[0].Tools, parsed.Steps[1].Tools, errs, declared)
	}
	if strings.Contains(parsed.Steps[0].Body, "工具") {
		t.Fatalf("tool line entered the body: %q", parsed.Steps[0].Body)
	}
	if err := liveLookupToolsRequired(declared, "北京天气"); err != nil {
		t.Fatal(err)
	}

	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return listed, nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 1 || strings.Count(spec, "工具: web_search") != 1 || strings.Contains(spec, "report = \"晴\"") {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}
}

func TestCompileHopSpecBindsSearchToTheAnswerStep(t *testing.T) {
	side := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"晴\"\n  > " + "```" + "\n2. [act] 记录\n  - 工具: web_search\n  + → log % text\n3. [exit]\n"
	if err := liveLookupToolsRequired(side, "北京天气"); err == nil {
		t.Fatal("web_search on a side act covered a fabricated report")
	}
	gotSide := repairLiveLookupTools(side, "北京天气")
	parsedSide, sideErrs := hopspec.Parse(gotSide)
	if len(sideErrs) != 0 || len(parsedSide.Steps) < 2 || strings.TrimSpace(parsedSide.Steps[0].Body) != "" || len(parsedSide.Steps[0].Tools) != 1 || parsedSide.Steps[0].Tools[0] != "web_search" || len(parsedSide.Steps[1].Tools) != 1 || parsedSide.Steps[1].Tools[0] != "web_search" {
		t.Fatalf("side tools=%v %v body=%q spec=%q", parsedSide.Steps[0].Tools, parsedSide.Steps[1].Tools, parsedSide.Steps[0].Body, gotSide)
	}
	if err := liveLookupToolsRequired(gotSide, "北京天气"); err != nil {
		t.Fatal(err)
	}

	answer := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → raw % text\n2. [act] 整理\n  + → report % text\n3. [exit]\n"
	got := repairLiveLookupTools(answer, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) < 2 || len(parsed.Steps[0].Tools) != 0 || len(parsed.Steps[1].Tools) != 1 || parsed.Steps[1].Tools[0] != "web_search" {
		t.Fatalf("answer tools=%v %v errs=%v spec=%q", parsed.Steps[0].Tools, parsed.Steps[1].Tools, errs, got)
	}

	overwrite := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → report % text\n  > " + "```hop_python" + "\n  > raw = web_search(query: \"北京天气\")\n  > report = \"晴\"\n  > " + "```" + "\n2. [exit]\n"
	if err := liveLookupToolsRequired(overwrite, "北京天气"); err == nil {
		t.Fatal("a literal assignment after web_search counted as the answer")
	}
	gotOverwrite := repairLiveLookupTools(overwrite, "北京天气")
	parsedOverwrite, overwriteErrs := hopspec.Parse(gotOverwrite)
	if len(overwriteErrs) != 0 || len(parsedOverwrite.Steps) < 1 || strings.TrimSpace(parsedOverwrite.Steps[0].Body) != "" || len(parsedOverwrite.Steps[0].Tools) != 1 || parsedOverwrite.Steps[0].Tools[0] != "web_search" || strings.Count(gotOverwrite, "工具: web_search") != 1 {
		t.Fatalf("overwrite tools=%v body=%q spec=%q", parsedOverwrite.Steps[0].Tools, parsedOverwrite.Steps[0].Body, gotOverwrite)
	}
	if err := liveLookupToolsRequired(gotOverwrite, "北京天气"); err != nil {
		t.Fatal(err)
	}

	forward := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > raw = web_search(query: \"北京天气\")\n  > report = raw\n  > " + "```" + "\n2. [exit]\n"
	declared := repairLiveLookupTools(forward, "北京天气")
	parsed, errs = hopspec.Parse(declared)
	if len(errs) != 0 || len(parsed.Steps) < 1 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || strings.Contains(parsed.Steps[0].Body, "工具") {
		t.Fatalf("forward tools=%v body=%q errs=%v", parsed.Steps[0].Tools, parsed.Steps[0].Body, errs)
	}
	if err := liveLookupToolsRequired(declared, "北京天气"); err != nil {
		t.Fatal(err)
	}

	both := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n- detail % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n2. [act] 补充\n  + → detail % text\n3. [exit]\n"
	gotBoth := repairLiveLookupTools(both, "北京天气")
	parsedBoth, bothErrs := hopspec.Parse(gotBoth)
	if len(bothErrs) != 0 || len(parsedBoth.Steps) < 2 || len(parsedBoth.Steps[0].Tools) != 1 || parsedBoth.Steps[0].Tools[0] != "web_search" || len(parsedBoth.Steps[1].Tools) != 1 || parsedBoth.Steps[1].Tools[0] != "web_search" {
		t.Fatalf("two answers tools=%v %v spec=%q", parsedBoth.Steps[0].Tools, parsedBoth.Steps[1].Tools, gotBoth)
	}
	if err := liveLookupToolsRequired(gotBoth, "北京天气"); err != nil {
		t.Fatal(err)
	}

	withExit := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → report % text\n2. [exit]\n  + → report % text\n"
	if repairLiveLookupTools(withExit, "北京天气") != withExit {
		t.Fatal("exit output declaration hid the act that searches")
	}
	if err := liveLookupToolsRequired(withExit, "北京天气"); err != nil {
		t.Fatal(err)
	}

	copied := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → raw % text\n2. [act] 整理\n  - ← raw\n  + → report % text\n  > " + "```hop_python" + "\n  > report = raw\n  > " + "```" + "\n3. [exit]\n"
	if repairLiveLookupTools(copied, "北京天气") != copied {
		t.Fatal("copy of a retrieved input was given another search")
	}
	if err := liveLookupToolsRequired(copied, "北京天气"); err != nil {
		t.Fatal(err)
	}
	unwired := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → raw % text\n2. [act] 整理\n  + → report % text\n  > " + "```hop_python" + "\n  > report = raw\n  > " + "```" + "\n3. [exit]\n"
	if err := liveLookupToolsRequired(unwired, "北京天气"); err == nil {
		t.Fatal("copy without an input counted as retrieved")
	}
	gotUnwired := repairLiveLookupTools(unwired, "北京天气")
	parsedUnwired, unwiredErrs := hopspec.Parse(gotUnwired)
	if len(unwiredErrs) != 0 || len(parsedUnwired.Steps) < 2 || len(parsedUnwired.Steps[1].Tools) != 0 || len(parsedUnwired.Steps[1].Inputs) != 1 || parsedUnwired.Steps[1].Inputs[0] != "raw" || !strings.Contains(parsedUnwired.Steps[1].Body, "report = raw") {
		t.Fatalf("unwired inputs=%v tools=%v body=%q spec=%q", parsedUnwired.Steps[1].Inputs, parsedUnwired.Steps[1].Tools, parsedUnwired.Steps[1].Body, gotUnwired)
	}
	if err := liveLookupToolsRequired(gotUnwired, "北京天气"); err != nil {
		t.Fatal(err)
	}

	shortCircuit := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"晴\" or web_search(query: \"北京天气\")\n  > " + "```" + "\n2. [exit]\n"
	if err := liveLookupToolsRequired(shortCircuit, "北京天气"); err == nil {
		t.Fatal("a truthy literal or web_search counted as a search")
	}
	gotShort := repairLiveLookupTools(shortCircuit, "北京天气")
	parsedShort, shortErrs := hopspec.Parse(gotShort)
	if len(shortErrs) != 0 || len(parsedShort.Steps) < 1 || strings.TrimSpace(parsedShort.Steps[0].Body) != "" || len(parsedShort.Steps[0].Tools) != 1 {
		t.Fatalf("short circuit body=%q tools=%v spec=%q", parsedShort.Steps[0].Body, parsedShort.Steps[0].Tools, gotShort)
	}
	if err := liveLookupToolsRequired(gotShort, "北京天气"); err != nil {
		t.Fatal(err)
	}

	fallback := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\") or \"晴\"\n  > " + "```" + "\n2. [exit]\n"
	declared = repairLiveLookupTools(fallback, "北京天气")
	parsed, errs = hopspec.Parse(declared)
	if len(errs) != 0 || len(parsed.Steps) < 1 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || strings.Contains(parsed.Steps[0].Body, "工具") {
		t.Fatalf("or fallback tools=%v body=%q errs=%v spec=%q", parsed.Steps[0].Tools, parsed.Steps[0].Body, errs, declared)
	}
	if err := liveLookupToolsRequired(declared, "北京天气"); err != nil {
		t.Fatal(err)
	}

	suffix := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → report % text\n  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\") + \"晴\"\n  > " + "```" + "\n2. [exit]\n"
	if err := liveLookupToolsRequired(suffix, "北京天气"); err == nil {
		t.Fatal("a literal suffix counted as the search result")
	}
	gotSuffix := repairLiveLookupTools(suffix, "北京天气")
	parsedSuffix, suffixErrs := hopspec.Parse(gotSuffix)
	if len(suffixErrs) != 0 || len(parsedSuffix.Steps) < 1 || strings.TrimSpace(parsedSuffix.Steps[0].Body) != "" || len(parsedSuffix.Steps[0].Tools) != 1 {
		t.Fatalf("suffix body=%q tools=%v spec=%q", parsedSuffix.Steps[0].Body, parsedSuffix.Steps[0].Tools, gotSuffix)
	}
	if err := liveLookupToolsRequired(gotSuffix, "北京天气"); err != nil {
		t.Fatal(err)
	}

	branched := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → report % text\n  > " + "```hop_python" + "\n  > if True:\n  >     report = \"晴\"\n  > else:\n  >     report = web_search(query: \"北京天气\")\n  > " + "```" + "\n2. [exit]\n"
	if err := liveLookupToolsRequired(branched, "北京天气"); err == nil {
		t.Fatal("a literal branch counted as a search")
	}
	gotBranched := repairLiveLookupTools(branched, "北京天气")
	parsedBranched, branchedErrs := hopspec.Parse(gotBranched)
	if len(branchedErrs) != 0 || len(parsedBranched.Steps) < 1 || strings.TrimSpace(parsedBranched.Steps[0].Body) != "" || len(parsedBranched.Steps[0].Tools) != 1 {
		t.Fatalf("branch body=%q tools=%v spec=%q", parsedBranched.Steps[0].Body, parsedBranched.Steps[0].Tools, gotBranched)
	}
	if err := liveLookupToolsRequired(gotBranched, "北京天气"); err != nil {
		t.Fatal(err)
	}

	bothBranches := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  > " + "```hop_python" + "\n  > if True:\n  >     report = web_search(query: \"北京天气\")\n  > else:\n  >     report = web_search(query: \"北京\")\n  > " + "```" + "\n2. [exit]\n"
	declared = repairLiveLookupTools(bothBranches, "北京天气")
	parsed, errs = hopspec.Parse(declared)
	if len(errs) != 0 || len(parsed.Steps) < 1 || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || strings.Contains(parsed.Steps[0].Body, "工具") {
		t.Fatalf("branch tools=%v body=%q errs=%v spec=%q", parsed.Steps[0].Tools, parsed.Steps[0].Body, errs, declared)
	}
	if err := liveLookupToolsRequired(declared, "北京天气"); err != nil {
		t.Fatal(err)
	}
}

func TestCompileHopSpecDropsFabricatedActFreeBody(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act free] 查询\n  + → report % text\n```hop_python\nreport = \"晴\"\n```\n2. [check] 核验\n  + → accepted % bool\n3. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || strings.Contains(spec, "report = \"晴\"") || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d spec=%q", calls, spec)
	}
	parsed, errs := hopspec.Parse(spec)
	if len(errs) != 0 || len(parsed.Steps) < 1 || !parsed.Steps[0].Free() || strings.TrimSpace(parsed.Steps[0].Body) != "" || len(parsed.Steps[0].Tools) != 1 {
		t.Fatalf("free=%v body=%q tools=%v errs=%v spec=%q", parsed.Steps[0].Free(), parsed.Steps[0].Body, parsed.Steps[0].Tools, errs, spec)
	}
	if err := liveLookupToolsRequired(spec, "北京天气"); err != nil {
		t.Fatal(err)
	}
}

func TestCompileHopSpecRewritesWhenTheAnswerIsNotAnAct(t *testing.T) {
	check := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [check] 查询\n  + → report % text\n  + → accepted % bool\n2. [exit]\n"
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		if calls == 1 {
			return check, nil
		}
		return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n2. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 2 || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}
	kept := repairLiveLookupTools(check, "北京天气")
	parsed, errs := hopspec.Parse(kept)
	if strings.Contains(kept, "web_search") || len(errs) != 0 || len(parsed.Steps) < 1 || parsed.Steps[0].Type != hopspec.StepCheck {
		t.Fatalf("check step gained a tool: type=%s spec=%q errs=%v", parsed.Steps[0].Type, kept, errs)
	}
	if err := liveLookupToolsRequired(kept, "北京天气"); err == nil {
		t.Fatal("a check that writes the text output counted as retrieved")
	}
	searching := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [check] 查询\n  - 工具: web_search\n  + → report % text\n  + → accepted % bool\n  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\")\n  > accepted = True\n  > " + "```" + "\n2. [exit]\n"
	if repairLiveLookupTools(searching, "北京天气") != searching {
		t.Fatal("a check that searches was rewritten")
	}
	if err := liveLookupToolsRequired(searching, "北京天气"); err != nil {
		t.Fatal(err)
	}
	parsedSearch, searchErrs := hopspec.Parse(searching)
	if len(searchErrs) != 0 || len(parsedSearch.Steps) < 1 || parsedSearch.Steps[0].Type != hopspec.StepCheck {
		t.Fatalf("check type=%s errs=%v", parsedSearch.Steps[0].Type, searchErrs)
	}
}

func TestRepairLiveLookupPromotesReasonWriter(t *testing.T) {
	reason := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  + → report % text\n2. [exit]\n"
	if err := liveLookupToolsRequired(reason, "北京天气"); err == nil {
		t.Fatal("a reason output counted as retrieved")
	}
	got := repairLiveLookupTools(reason, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) < 1 || parsed.Steps[0].Type != hopspec.StepAct || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || strings.Count(got, "工具: web_search") != 1 || strings.Contains(got, "[reason]") {
		t.Fatalf("promoted type=%s tools=%v spec=%q errs=%v", parsed.Steps[0].Type, parsed.Steps[0].Tools, got, errs)
	}
	if err := liveLookupToolsRequired(got, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(got, "北京天气"); again != got {
		t.Fatalf("reason promotion repeated: %q", again)
	}

	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return reason, nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 1 || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}

	fence := "  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\")\n  > " + "```" + "\n"
	searching := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  - 工具: web_search\n  + → report % text\n" + fence + "2. [exit]\n"
	if err := liveLookupToolsRequired(searching, "北京天气"); err == nil {
		t.Fatal("a reason body counted as a search")
	}
	gotSearch := repairLiveLookupTools(searching, "北京天气")
	parsedSearch, searchErrs := hopspec.Parse(gotSearch)
	if len(searchErrs) != 0 || len(parsedSearch.Steps) < 1 || parsedSearch.Steps[0].Type != hopspec.StepAct || strings.TrimSpace(parsedSearch.Steps[0].Body) == "" || !strings.Contains(parsedSearch.Steps[0].Body, "web_search(") || len(parsedSearch.Steps[0].Tools) != 1 || parsedSearch.Steps[0].Tools[0] != "web_search" || strings.Contains(parsedSearch.Steps[0].Body, "工具") {
		t.Fatalf("kept body type=%s tools=%v body=%q spec=%q", parsedSearch.Steps[0].Type, parsedSearch.Steps[0].Tools, parsedSearch.Steps[0].Body, gotSearch)
	}
	if err := liveLookupToolsRequired(gotSearch, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(gotSearch, "北京天气"); again != gotSearch {
		t.Fatalf("search promotion repeated: %q", again)
	}

	cn := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. ［推理］ 查询\n  + → report % text\n2. [exit]\n"
	gotCN := repairLiveLookupTools(cn, "北京天气")
	parsedCN, cnErrs := hopspec.Parse(gotCN)
	if len(cnErrs) != 0 || len(parsedCN.Steps) < 1 || parsedCN.Steps[0].Type != hopspec.StepAct || len(parsedCN.Steps[0].Tools) != 1 || strings.Contains(gotCN, "推理") {
		t.Fatalf("cn type=%s tools=%v spec=%q errs=%v", parsedCN.Steps[0].Type, parsedCN.Steps[0].Tools, gotCN, cnErrs)
	}

	body := func(text string) string {
		return "  > " + "```hop_python" + "\n" + text + "  > " + "```" + "\n"
	}
	chain := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- raw % text\n- report % text\n## Steps\n1. [reason] 查询天气\n  + → raw % text\n" + body("  > raw = web_search(query: \"北京天气\")\n") + "2. [reason] 整理\n  - ← raw\n  + → report % text\n" + body("  > report = raw\n") + "3. [exit]\n"
	if err := liveLookupToolsRequired(chain, "北京天气"); err == nil {
		t.Fatal("a reason copy counted as retrieved")
	}
	gotChain := repairLiveLookupTools(chain, "北京天气")
	parsedChain, chainErrs := hopspec.Parse(gotChain)
	if len(chainErrs) != 0 || len(parsedChain.Steps) < 2 || parsedChain.Steps[0].Type != hopspec.StepAct || parsedChain.Steps[1].Type != hopspec.StepAct || len(parsedChain.Steps[0].Tools) != 1 || parsedChain.Steps[0].Tools[0] != "web_search" || strings.TrimSpace(parsedChain.Steps[0].Body) == "" || len(parsedChain.Steps[1].Tools) != 0 || len(parsedChain.Steps[1].Inputs) != 1 || parsedChain.Steps[1].Inputs[0] != "raw" || !strings.Contains(parsedChain.Steps[1].Body, "report = raw") || strings.Count(gotChain, "工具: web_search") != 1 || strings.Contains(gotChain, "[reason]") {
		t.Fatalf("chain types=%s %s tools=%v %v inputs=%v body=%q spec=%q", parsedChain.Steps[0].Type, parsedChain.Steps[1].Type, parsedChain.Steps[0].Tools, parsedChain.Steps[1].Tools, parsedChain.Steps[1].Inputs, parsedChain.Steps[1].Body, gotChain)
	}
	if err := liveLookupToolsRequired(gotChain, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(gotChain, "北京天气"); again != gotChain {
		t.Fatalf("chain promotion repeated: %q", again)
	}

	wired := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- raw % text\n- report % text\n## Steps\n1. [act] 查询天气\n  + → raw % text\n2. [act] 整理\n  - ← raw\n  + → report % text\n" + body("  > report = raw\n") + "3. [exit]\n"
	gotWired := repairLiveLookupTools(wired, "北京天气")
	parsedWired, wiredErrs := hopspec.Parse(gotWired)
	if len(wiredErrs) != 0 || len(parsedWired.Steps) < 2 || len(parsedWired.Steps[0].Tools) != 1 || parsedWired.Steps[0].Tools[0] != "web_search" || len(parsedWired.Steps[1].Tools) != 0 || len(parsedWired.Steps[1].Inputs) != 1 || !strings.Contains(parsedWired.Steps[1].Body, "report = raw") || strings.Count(gotWired, "工具: web_search") != 1 {
		t.Fatalf("wired tools=%v %v inputs=%v body=%q spec=%q", parsedWired.Steps[0].Tools, parsedWired.Steps[1].Tools, parsedWired.Steps[1].Inputs, parsedWired.Steps[1].Body, gotWired)
	}
	if err := liveLookupToolsRequired(gotWired, "北京天气"); err != nil {
		t.Fatal(err)
	}
}

func TestRepairLiveLookupKeepsCommentedRetrieval(t *testing.T) {
	fence := "  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\")  # 注释\n  > " + "```" + "\n"
	spec := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n" + fence + "2. [exit]\n"
	got := repairLiveLookupTools(spec, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) < 1 || strings.TrimSpace(parsed.Steps[0].Body) == "" || !strings.Contains(parsed.Steps[0].Body, "web_search(query: \"北京天气\")") || !strings.Contains(parsed.Steps[0].Body, "# 注释") || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" {
		t.Fatalf("body=%q tools=%v spec=%q errs=%v", parsed.Steps[0].Body, parsed.Steps[0].Tools, got, errs)
	}
	if err := liveLookupToolsRequired(got, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(got, "北京天气"); again != got {
		t.Fatalf("commented retrieval repeated: %q", again)
	}
}

func TestRepairLiveLookupDropsUnrunnableCall(t *testing.T) {
	fence := func(body string) string {
		return "  > " + "```hop_python" + "\n" + body + "  > " + "```" + "\n"
	}
	assertDropped := func(spec string) {
		t.Helper()
		if err := liveLookupToolsRequired(spec, "北京天气"); err == nil {
			t.Fatalf("unrunnable body counted as a search: %q", spec)
		}
		got := repairLiveLookupTools(spec, "北京天气")
		parsed, errs := hopspec.Parse(got)
		if len(errs) != 0 || len(parsed.Steps) < 1 || parsed.Steps[0].Type != hopspec.StepAct || strings.TrimSpace(parsed.Steps[0].Body) != "" || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || strings.Count(got, "工具: web_search") != 1 {
			t.Fatalf("type=%s body=%q tools=%v spec=%q errs=%v", parsed.Steps[0].Type, parsed.Steps[0].Body, parsed.Steps[0].Tools, got, errs)
		}
		if err := liveLookupToolsRequired(got, "北京天气"); err != nil {
			t.Fatal(err)
		}
		if again := repairLiveLookupTools(got, "北京天气"); again != got {
			t.Fatalf("repeated: %q", again)
		}
	}
	assertDropped("# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → report % text\n" + fence("  > report = web_search(query=\"北京天气\")\n") + "2. [exit]\n")
	assertDropped("# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  + → report % text\n" + fence("  > report = web_search(\"北京天气\")\n") + "2. [exit]\n")
	assertDropped("# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n" + fence("  > report = web_search(query: city)\n") + "2. [exit]\n")

	wired := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- city % text\n- report % text\n## Steps\n1. [act] 查询城市\n  + → city % text\n2. [act] 查询天气\n  + → report % text\n" + fence("  > report = web_search(query: city)\n") + "3. [exit]\n"
	if err := liveLookupToolsRequired(wired, "北京天气"); err == nil {
		t.Fatal("an unbound query name counted as a search")
	}
	gotWired := repairLiveLookupTools(wired, "北京天气")
	parsedWired, wiredErrs := hopspec.Parse(gotWired)
	if len(wiredErrs) != 0 || len(parsedWired.Steps) < 2 || strings.TrimSpace(parsedWired.Steps[0].Body) != "" || len(parsedWired.Steps[0].Tools) != 1 || parsedWired.Steps[0].Tools[0] != "web_search" || !strings.Contains(parsedWired.Steps[1].Body, "web_search(query: city)") || len(parsedWired.Steps[1].Inputs) != 1 || parsedWired.Steps[1].Inputs[0] != "city" || len(parsedWired.Steps[1].Tools) != 1 || parsedWired.Steps[1].Tools[0] != "web_search" {
		t.Fatalf("wired bodies=%q %q tools=%v %v inputs=%v spec=%q errs=%v", parsedWired.Steps[0].Body, parsedWired.Steps[1].Body, parsedWired.Steps[0].Tools, parsedWired.Steps[1].Tools, parsedWired.Steps[1].Inputs, gotWired, wiredErrs)
	}
	if err := liveLookupToolsRequired(gotWired, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(gotWired, "北京天气"); again != gotWired {
		t.Fatalf("wired repeated: %q", again)
	}
}

func TestRepairLiveLookupDropsUnrunnableRewrite(t *testing.T) {
	fence := func(body string) string {
		return "  > " + "```hop_python" + "\n" + body + "  > " + "```" + "\n"
	}
	spec := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → report % text\n2. [act] 整理\n  - ← report\n  + → report % text\n" + fence("  > report = web_search(query=\"北京天气\")\n") + "3. [exit]\n"
	if err := liveLookupToolsRequired(spec, "北京天气"); err == nil {
		t.Fatal("an unrunnable rewrite kept the earlier search")
	}
	got := repairLiveLookupTools(spec, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) < 2 || strings.TrimSpace(parsed.Steps[0].Body) != "" || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || strings.TrimSpace(parsed.Steps[1].Body) != "" || len(parsed.Steps[1].Tools) != 1 || parsed.Steps[1].Tools[0] != "web_search" || len(parsed.Steps[1].Inputs) != 1 || parsed.Steps[1].Inputs[0] != "report" {
		t.Fatalf("bodies=%q %q tools=%v %v inputs=%v spec=%q errs=%v", parsed.Steps[0].Body, parsed.Steps[1].Body, parsed.Steps[0].Tools, parsed.Steps[1].Tools, parsed.Steps[1].Inputs, got, errs)
	}
	if err := liveLookupToolsRequired(got, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(got, "北京天气"); again != got {
		t.Fatalf("rewrite repeated: %q", again)
	}

	copied := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → report % text\n2. [act] 整理\n  - ← report\n  + → report % text\n" + fence("  > report = report\n") + "3. [exit]\n"
	if err := liveLookupToolsRequired(copied, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(copied, "北京天气"); again != copied {
		t.Fatalf("retrieved copy was rewritten: %q", again)
	}
}

func TestRepairLiveLookupPublishesEveryLookupAct(t *testing.T) {
	fence := func(body string) string {
		return "  > " + "```hop_python" + "\n" + body + "  > " + "```" + "\n"
	}
	mixed := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询天气\n  + → report % text\n" + fence("  > raw = web_search(query: \"北京天气\")\n  > report = \"晴\"\n") + "2. [exit]\n"
	if err := liveLookupToolsRequired(mixed, "北京天气"); err == nil {
		t.Fatal("a literal beside web_search counted as the published report")
	}
	gotMixed := repairLiveLookupTools(mixed, "北京天气")
	parsedMixed, mixedErrs := hopspec.Parse(gotMixed)
	if len(mixedErrs) != 0 || len(parsedMixed.Steps) < 1 || strings.TrimSpace(parsedMixed.Steps[0].Body) != "" || len(parsedMixed.Steps[0].Tools) != 1 || parsedMixed.Steps[0].Tools[0] != "web_search" || strings.Count(gotMixed, "工具: web_search") != 1 {
		t.Fatalf("mixed body=%q tools=%v spec=%q", parsedMixed.Steps[0].Body, parsedMixed.Steps[0].Tools, gotMixed)
	}
	if err := liveLookupToolsRequired(gotMixed, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(gotMixed, "北京天气"); again != gotMixed {
		t.Fatalf("mixed repair repeated: %q", again)
	}

	sibling := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 记录\n  - 工具: web_search\n  + → log % text\n2. [act] 查询天气\n  + → report % text\n" + fence("  > report = \"晴\"\n") + "3. [exit]\n"
	if err := liveLookupToolsRequired(sibling, "北京天气"); err == nil {
		t.Fatal("a search on another act covered a fabricated lookup")
	}
	gotSibling := repairLiveLookupTools(sibling, "北京天气")
	parsedSibling, siblingErrs := hopspec.Parse(gotSibling)
	if len(siblingErrs) != 0 || len(parsedSibling.Steps) < 2 || len(parsedSibling.Steps[0].Tools) != 1 || parsedSibling.Steps[0].Tools[0] != "web_search" || strings.TrimSpace(parsedSibling.Steps[1].Body) != "" || len(parsedSibling.Steps[1].Tools) != 1 || parsedSibling.Steps[1].Tools[0] != "web_search" {
		t.Fatalf("sibling tools=%v %v body=%q spec=%q", parsedSibling.Steps[0].Tools, parsedSibling.Steps[1].Tools, parsedSibling.Steps[1].Body, gotSibling)
	}

	copied := "# Spec: 北京天气\nGoal: 查询天气\n## Steps\n1. [act] 查询天气\n  - 工具: web_search\n  + → raw % text\n2. [act] 整理天气\n  + → report % text\n" + fence("  > report = raw\n") + "3. [exit]\n"
	gotCopied := repairLiveLookupTools(copied, "北京天气")
	parsedCopied, copiedErrs := hopspec.Parse(gotCopied)
	if len(copiedErrs) != 0 || len(parsedCopied.Steps) < 2 || len(parsedCopied.Steps[1].Tools) != 0 || len(parsedCopied.Steps[1].Inputs) != 1 || parsedCopied.Steps[1].Inputs[0] != "raw" || !strings.Contains(parsedCopied.Steps[1].Body, "report = raw") || strings.Count(gotCopied, "工具: web_search") != 1 {
		t.Fatalf("copied inputs=%v tools=%v body=%q spec=%q", parsedCopied.Steps[1].Inputs, parsedCopied.Steps[1].Tools, parsedCopied.Steps[1].Body, gotCopied)
	}
	if err := liveLookupToolsRequired(gotCopied, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(gotCopied, "北京天气"); again != gotCopied {
		t.Fatalf("copy repair repeated: %q", again)
	}

	chain := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- raw % text\n- report % text\n## Steps\n1. [act] 查询天气\n  + → raw % text\n2. [act] 整理\n  + → report % text\n" + fence("  > report = raw\n") + "3. [exit]\n"
	gotChain := repairLiveLookupTools(chain, "北京天气")
	parsedChain, chainErrs := hopspec.Parse(gotChain)
	if len(chainErrs) != 0 || len(parsedChain.Steps) < 2 || len(parsedChain.Steps[0].Tools) != 1 || parsedChain.Steps[0].Tools[0] != "web_search" || len(parsedChain.Steps[1].Tools) != 0 || len(parsedChain.Steps[1].Inputs) != 1 || parsedChain.Steps[1].Inputs[0] != "raw" || !strings.Contains(parsedChain.Steps[1].Body, "report = raw") {
		t.Fatalf("chain tools=%v %v inputs=%v body=%q spec=%q", parsedChain.Steps[0].Tools, parsedChain.Steps[1].Tools, parsedChain.Steps[1].Inputs, parsedChain.Steps[1].Body, gotChain)
	}
	if err := liveLookupToolsRequired(gotChain, "北京天气"); err != nil {
		t.Fatal(err)
	}

	bothTools := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n- detail % text\n## Steps\n1. [act] 查询天气\n  + → report % text\n  + → detail % text\n" + fence("  > report = web_search(query: \"北京天气\")\n  > detail = web_fetch(url: \"https://example.com/weather\")\n") + "2. [exit]\n"
	gotBoth := repairLiveLookupTools(bothTools, "北京天气")
	parsedBoth, bothErrs := hopspec.Parse(gotBoth)
	if len(bothErrs) != 0 || len(parsedBoth.Steps) < 1 || strings.TrimSpace(parsedBoth.Steps[0].Body) == "" || len(parsedBoth.Steps[0].Tools) != 2 || parsedBoth.Steps[0].Tools[0] != "web_search" || parsedBoth.Steps[0].Tools[1] != "web_fetch" || strings.Contains(parsedBoth.Steps[0].Body, "工具") {
		t.Fatalf("both tools=%v body=%q spec=%q errs=%v", parsedBoth.Steps[0].Tools, parsedBoth.Steps[0].Body, gotBoth, bothErrs)
	}
	if err := liveLookupToolsRequired(gotBoth, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(gotBoth, "北京天气"); again != gotBoth {
		t.Fatalf("tool repair repeated: %q", again)
	}

	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return bothTools, nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 1 || strings.Count(spec, "工具: web_search") != 1 || strings.Count(spec, "工具: web_fetch") != 1 {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}
}

func TestRepairLiveLookupDropsRepeatedHostText(t *testing.T) {
	repeated := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → report % text\n2. [check] 核验\n  ＋ → report % text\n  + → accepted % bool\n3. [exit]\n"
	if err := liveLookupToolsRequired(repeated, "北京天气"); err == nil {
		t.Fatal("a check that repeats the text output hid the search")
	}
	got := repairLiveLookupTools(repeated, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) < 2 || parsed.Steps[0].Type != hopspec.StepAct || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || parsed.Steps[1].Type != hopspec.StepCheck || len(parsed.Steps[1].Outputs) != 1 || parsed.Steps[1].Outputs[0].Name != "accepted" || strings.Count(got, "工具: web_search") != 1 {
		t.Fatalf("repeated tools=%v outs=%v spec=%q errs=%v", parsed.Steps[0].Tools, parsed.Steps[1].Outputs, got, errs)
	}
	if err := liveLookupToolsRequired(got, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(got, "北京天气"); again != got {
		t.Fatalf("repeat repair duplicated: %q", again)
	}

	confirm := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → report % text\n2. [confirm] 确认\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	gotConfirm := repairLiveLookupTools(confirm, "北京天气")
	parsedConfirm, confirmErrs := hopspec.Parse(gotConfirm)
	if len(confirmErrs) != 0 || len(parsedConfirm.Steps) < 2 || parsedConfirm.Steps[1].Type != hopspec.StepConfirm || len(parsedConfirm.Steps[1].Outputs) != 1 || parsedConfirm.Steps[1].Outputs[0].Name != "accepted" || strings.Count(gotConfirm, "工具: web_search") != 1 {
		t.Fatalf("confirm outs=%v spec=%q errs=%v", parsedConfirm.Steps[1].Outputs, gotConfirm, confirmErrs)
	}
	if err := liveLookupToolsRequired(gotConfirm, "北京天气"); err != nil {
		t.Fatal(err)
	}

	reason := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n  + → report % text\n2. [check] 核验\n  + → report % text\n3. [exit]\n"
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return reason, nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 1 || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}
	parsedSpec, specErrs := hopspec.Parse(spec)
	if len(specErrs) != 0 || len(parsedSpec.Steps) < 2 || parsedSpec.Steps[0].Type != hopspec.StepAct || parsedSpec.Steps[1].Type != hopspec.StepCheck || !hopTestStepOutputs(parsedSpec.Steps[0], "report") || hopTestStepOutputs(parsedSpec.Steps[1], "report") || !hopTestStepOutputs(parsedSpec.Steps[1], "accepted") {
		t.Fatalf("compiled spec=%q errs=%v", spec, specErrs)
	}

	republish := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → report % text\n2. [check] 核验\n  - ← report\n  + → report % text\n  + → accepted % bool\n  > " + "```hop_python" + "\n  > accepted = report != \"\"\n  > " + "```" + "\n3. [exit]\n"
	if repairLiveLookupTools(republish, "北京天气") != republish {
		t.Fatal("a check that republishes the retrieved input was rewritten")
	}
	if err := liveLookupToolsRequired(republish, "北京天气"); err != nil {
		t.Fatal(err)
	}

	notes := "# Spec: 整理笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [act] 整理\n  + → report % text\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	if repairLiveLookupTools(notes, "整理笔记") != notes {
		t.Fatal("a note check lost its text output")
	}
}

func TestRepairLiveLookupMovesHostTextOntoEarlierStep(t *testing.T) {
	reason := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	if err := liveLookupToolsRequired(reason, "北京天气"); err == nil {
		t.Fatal("a check that writes the text output counted as retrieved")
	}
	got := repairLiveLookupTools(reason, "北京天气")
	parsed, errs := hopspec.Parse(got)
	if len(errs) != 0 || len(parsed.Steps) < 2 || parsed.Steps[0].Type != hopspec.StepAct || len(parsed.Steps[0].Tools) != 1 || parsed.Steps[0].Tools[0] != "web_search" || !hopTestStepOutputs(parsed.Steps[0], "report") || parsed.Steps[1].Type != hopspec.StepCheck || hopTestStepOutputs(parsed.Steps[1], "report") || !hopTestStepOutputs(parsed.Steps[1], "accepted") || strings.Contains(got, "[reason]") || strings.Count(got, "工具: web_search") != 1 {
		t.Fatalf("moved type=%s tools=%v spec=%q errs=%v", parsed.Steps[0].Type, parsed.Steps[0].Tools, got, errs)
	}
	if err := liveLookupToolsRequired(got, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if again := repairLiveLookupTools(got, "北京天气"); again != got {
		t.Fatalf("host move repeated: %q", again)
	}

	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [check] 核验\n  + → report % text\n3. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "北京天气", caller)
	if err != nil || calls != 1 || strings.Count(spec, "工具: web_search") != 1 {
		t.Fatalf("calls=%d err=%v spec=%q", calls, err, spec)
	}
	parsedSpec, specErrs := hopspec.Parse(spec)
	if len(specErrs) != 0 || len(parsedSpec.Steps) < 2 || parsedSpec.Steps[0].Type != hopspec.StepAct || !hopTestStepOutputs(parsedSpec.Steps[0], "report") || parsedSpec.Steps[1].Type != hopspec.StepCheck || hopTestStepOutputs(parsedSpec.Steps[1], "report") || !hopTestStepOutputs(parsedSpec.Steps[1], "accepted") {
		t.Fatalf("compiled spec=%q errs=%v", spec, specErrs)
	}

	confirm := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [confirm] 确认\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	gotConfirm := repairLiveLookupTools(confirm, "北京天气")
	parsedConfirm, confirmErrs := hopspec.Parse(gotConfirm)
	if len(confirmErrs) != 0 || len(parsedConfirm.Steps) < 2 || parsedConfirm.Steps[0].Type != hopspec.StepAct || len(parsedConfirm.Steps[0].Tools) != 1 || hopTestStepOutputs(parsedConfirm.Steps[1], "report") || !hopTestStepOutputs(parsedConfirm.Steps[1], "accepted") {
		t.Fatalf("confirm spec=%q errs=%v", gotConfirm, confirmErrs)
	}
	if err := liveLookupToolsRequired(gotConfirm, "北京天气"); err != nil {
		t.Fatal(err)
	}

	ask := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. ［推理］ 查询\n2. [ask] 确认\n  ＋ → report % text\n3. [exit]\n"
	gotAsk := repairLiveLookupTools(ask, "北京天气")
	parsedAsk, askErrs := hopspec.Parse(gotAsk)
	if len(askErrs) != 0 || len(parsedAsk.Steps) < 2 || parsedAsk.Steps[0].Type != hopspec.StepAct || len(parsedAsk.Steps[0].Tools) != 1 || !hopTestStepOutputs(parsedAsk.Steps[0], "report") || hopTestStepOutputs(parsedAsk.Steps[1], "report") || strings.Contains(gotAsk, "推理") || !strings.Contains(gotAsk, "＋ → report % text") {
		t.Fatalf("ask spec=%q errs=%v", gotAsk, askErrs)
	}
	if err := liveLookupToolsRequired(gotAsk, "北京天气"); err != nil {
		t.Fatal(err)
	}

	saving := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 记录\n  - 工具: write_file\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	if repairLiveLookupTools(saving, "北京天气") != saving {
		t.Fatal("a write_file act received the weather output")
	}
	if err := liveLookupToolsRequired(saving, "北京天气"); err == nil {
		t.Fatal("a check writer counted as retrieved")
	}

	between := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [act] 记录\n  - 工具: write_file\n3. [check] 核验\n  + → report % text\n  + → accepted % bool\n4. [exit]\n"
	gotBetween := repairLiveLookupTools(between, "北京天气")
	parsedBetween, betweenErrs := hopspec.Parse(gotBetween)
	if len(betweenErrs) != 0 || len(parsedBetween.Steps) < 3 || parsedBetween.Steps[0].Type != hopspec.StepAct || len(parsedBetween.Steps[0].Tools) != 1 || parsedBetween.Steps[0].Tools[0] != "web_search" || !hopTestStepOutputs(parsedBetween.Steps[0], "report") || parsedBetween.Steps[1].Type != hopspec.StepAct || len(parsedBetween.Steps[1].Tools) != 1 || parsedBetween.Steps[1].Tools[0] != "write_file" || hopTestStepOutputs(parsedBetween.Steps[1], "report") || hopTestStepOutputs(parsedBetween.Steps[2], "report") {
		t.Fatalf("between spec=%q errs=%v", gotBetween, betweenErrs)
	}
	if err := liveLookupToolsRequired(gotBetween, "北京天气"); err != nil {
		t.Fatal(err)
	}

	marked := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [act] 标记\n  + → ok % bool\n3. [check] 核验\n  + → report % text\n  + → accepted % bool\n4. [exit]\n"
	gotMarked := repairLiveLookupTools(marked, "北京天气")
	parsedMarked, markedErrs := hopspec.Parse(gotMarked)
	if len(markedErrs) != 0 || len(parsedMarked.Steps) < 3 || parsedMarked.Steps[0].Type != hopspec.StepAct || len(parsedMarked.Steps[0].Tools) != 1 || !hopTestStepOutputs(parsedMarked.Steps[0], "report") || hopTestStepOutputs(parsedMarked.Steps[1], "report") || !hopTestStepOutputs(parsedMarked.Steps[1], "ok") || len(parsedMarked.Steps[1].Tools) != 0 {
		t.Fatalf("marked spec=%q errs=%v", gotMarked, markedErrs)
	}
	if err := liveLookupToolsRequired(gotMarked, "北京天气"); err != nil {
		t.Fatal(err)
	}

	listed := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- log % text\n- report % text\n## Steps\n1. [act] 查询\n  - 工具: web_search\n  + → log % text\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	gotListed := repairLiveLookupTools(listed, "北京天气")
	parsedListed, listedErrs := hopspec.Parse(gotListed)
	if len(listedErrs) != 0 || len(parsedListed.Steps) < 2 || len(parsedListed.Steps[0].Tools) != 1 || parsedListed.Steps[0].Tools[0] != "web_search" || !hopTestStepOutputs(parsedListed.Steps[0], "log") || !hopTestStepOutputs(parsedListed.Steps[0], "report") || hopTestStepOutputs(parsedListed.Steps[1], "report") || strings.Count(gotListed, "工具: web_search") != 1 {
		t.Fatalf("listed spec=%q errs=%v", gotListed, listedErrs)
	}
	if err := liveLookupToolsRequired(gotListed, "北京天气"); err != nil {
		t.Fatal(err)
	}

	fence := "  > " + "```hop_python" + "\n  > report = web_search(query: \"北京天气\")\n  > accepted = True\n  > " + "```" + "\n"
	searching := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n" + fence + "3. [exit]\n"
	if repairLiveLookupTools(searching, "北京天气") != searching {
		t.Fatal("a check that calls web_search lost its output")
	}
	if err := liveLookupToolsRequired(searching, "北京天气"); err == nil {
		t.Fatal("a check call without a tool line counted as retrieved")
	}

	literal := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n  > " + "```hop_python" + "\n  > report = \"晴\"\n  > accepted = True\n  > " + "```" + "\n3. [exit]\n"
	gotLiteral := repairLiveLookupTools(literal, "北京天气")
	parsedLiteral, literalErrs := hopspec.Parse(gotLiteral)
	if len(literalErrs) != 0 || len(parsedLiteral.Steps) < 2 || parsedLiteral.Steps[0].Type != hopspec.StepAct || strings.TrimSpace(parsedLiteral.Steps[0].Body) != "" || len(parsedLiteral.Steps[0].Tools) != 1 || !hopTestStepOutputs(parsedLiteral.Steps[0], "report") || hopTestStepOutputs(parsedLiteral.Steps[1], "report") || !strings.Contains(parsedLiteral.Steps[1].Body, "accepted = True") {
		t.Fatalf("literal spec=%q errs=%v", gotLiteral, literalErrs)
	}
	if err := liveLookupToolsRequired(gotLiteral, "北京天气"); err != nil {
		t.Fatal(err)
	}

	both := "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [reason] 查询\n2. [check] 草稿\n  + → report % text\n  + → accepted % bool\n3. [check] 核验\n  + → report % text\n  + → verdict % bool\n4. [exit]\n"
	gotBoth := repairLiveLookupTools(both, "北京天气")
	parsedBoth, bothErrs := hopspec.Parse(gotBoth)
	if len(bothErrs) != 0 || len(parsedBoth.Steps) < 3 || !hopTestStepOutputs(parsedBoth.Steps[0], "report") || hopTestStepOutputs(parsedBoth.Steps[1], "report") || hopTestStepOutputs(parsedBoth.Steps[2], "report") || !hopTestStepOutputs(parsedBoth.Steps[1], "accepted") || !hopTestStepOutputs(parsedBoth.Steps[2], "verdict") || strings.Count(gotBoth, "工具: web_search") != 1 {
		t.Fatalf("both spec=%q errs=%v", gotBoth, bothErrs)
	}
	if err := liveLookupToolsRequired(gotBoth, "北京天气"); err != nil {
		t.Fatal(err)
	}

	notes := "# Spec: 整理笔记\nGoal: 整理笔记\nOutputs:\n- report % text\n## Steps\n1. [reason] 整理\n2. [check] 核验\n  + → report % text\n  + → accepted % bool\n3. [exit]\n"
	if repairLiveLookupTools(notes, "整理笔记") != notes {
		t.Fatal("a note check lost its text output")
	}
}

func hopTestStepOutputs(step *hopspec.Step, name string) bool {
	for _, out := range step.Outputs {
		if out.Name == name {
			return true
		}
	}
	return false
}

func TestCompiledHopSpecAllowsActFreeBodyWithoutTools(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return "# Spec: 计算\nGoal: 计算\nOutputs:\n- report % text\n## Steps\n1. [act free] 计算\n  + → report % text\n  > " + "```hop_python" + "\n  > report = \"晴\"\n  > " + "```" + "\n2. [exit]\n", nil
	}}
	spec, err := compileHopSpec(context.Background(), "计算", caller)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(spec, "report = \"晴\"") {
		t.Fatalf("calls=%d spec=%q", calls, spec)
	}
}

func TestRunHopTaskPlainActFillsJSON(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			if jsonResponse {
				t.Fatal("compiler requested JSON")
			}
			return "# Spec: 北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [act] 查询\n  + → report % text\n2. [exit]\n", nil
		}
		if !jsonResponse {
			t.Fatal("plain act requested a tool turn")
		}
		return `{"report":"晴"}`, nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "整理笔记", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "晴" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
