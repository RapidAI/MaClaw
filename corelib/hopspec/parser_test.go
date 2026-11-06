package hopspec

import (
	"strings"
	"testing"
)

const validReviewSpec = `# Spec: 文档评审
Id: document-review
Goal: 读取材料并产出经过核验的摘要
Constraints:
- 摘要必须忠实于输入
Inputs:
- document % text  # 原文
Outputs:
- summary % text  # 摘要

## Steps
1. [reason] 生成摘要
  - ← document
  + → summary % text
  > 提取原文核心内容。
2. [check] 核验摘要
  - ← document, summary
  + → accepted % bool
  + → feedback % text
3. [exit] 交付摘要
`

func TestParseAndValidateSpec(t *testing.T) {
	spec, parseErrs := Parse(validReviewSpec)
	if len(parseErrs) != 0 {
		t.Fatalf("Parse() errors = %v", parseErrs)
	}
	if errs := Validate(spec); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v", errs)
	}
	if spec.Title != "文档评审" || spec.ID != "document-review" || spec.Goal == "" {
		t.Fatalf("unexpected header: %+v", spec)
	}
	if len(spec.Inputs) != 1 || spec.Inputs[0].Name != "document" || spec.Inputs[0].Description != "原文" {
		t.Fatalf("inputs = %+v", spec.Inputs)
	}
	steps := spec.AllSteps()
	if len(steps) != 3 || steps[0].Type != StepReason || steps[1].Type != StepCheck || steps[2].Type != StepExit {
		t.Fatalf("steps = %+v", steps)
	}
	if got := steps[0].Inputs; len(got) != 1 || got[0] != "document" {
		t.Fatalf("reason inputs = %v", got)
	}
	if got := steps[1].Inputs; len(got) != 2 || got[1] != "summary" {
		t.Fatalf("check inputs = %v", got)
	}
	if steps[0].Outputs[0].Name != "summary" || steps[0].Outputs[0].Type != "text" {
		t.Fatalf("outputs = %+v", steps[0].Outputs)
	}
	if steps[0].Source.LineStart == 0 || steps[0].Source.LineEnd < steps[0].Source.LineStart {
		t.Fatalf("source = %+v", steps[0].Source)
	}
}

func TestParseNestedStepsAndChineseAliases(t *testing.T) {
	spec, errs := Parse(`# Spec: 嵌套任务
目标: 测试嵌套步骤
输入:
- document % text
输出:
- report % text
## 步骤
1. [子任务 重试=2] 处理材料
  1.1. [推理] 生成报告
    - ← document
    + → report % text
2. [退出] 完成
`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	if len(spec.Steps) != 2 || len(spec.Steps[0].Children) != 1 {
		t.Fatalf("tree = %+v", spec.Steps)
	}
	parent := spec.Steps[0]
	if parent.Type != StepSubtask || parent.Attributes["重试"] != "2" {
		t.Fatalf("parent = %+v", parent)
	}
	child := parent.Children[0]
	if child.Type != StepReason || child.Outputs[0].Name != "report" {
		t.Fatalf("child = %+v", child)
	}
	if errs := Validate(spec); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v", errs)
	}
}

func TestValidationRejectsBrokenContracts(t *testing.T) {
	spec, parseErrs := Parse(`# Spec: 坏规约
Goal: 验证错误
Outputs:
- result % text
## Steps
1. [reason] 读取未知值
  - ← missing
1. [reason] 重复步骤号
2. [check] 缺少布尔判定
  + → feedback % text
`)
	if len(parseErrs) == 0 {
		t.Fatal("duplicate step id was accepted")
	}
	errs := Validate(spec)
	for _, code := range []string{"UNKNOWN_INPUT", "OUTPUT_WITHOUT_PRODUCER", "CHECK_BOOL_REQUIRED"} {
		if !diagnosticsContain(errs, code) {
			t.Fatalf("missing %s in %v", code, errs)
		}
	}
}

func TestParseAcceptsBoolVerdictForms(t *testing.T) {
	spec, errs := Parse(`# Spec: 判定
Goal: 核验
Outputs:
- report % text
## Steps
1. [act] 取数
  - 工具：web_search
  + -> report % text
2. [check] 判定
  -<-report
  + -> accepted % boolean
  +→ flag % Bool
3. [confirm] 确认
  － ← report
  ＋ → 通过 % 布尔
  + -> 结论 % 真假
4. [call weather(city: report)] 查询
  + -> report: result
5. [exit]
`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	if spec.Steps[0].Outputs[0].Name != "report" || spec.Steps[0].Outputs[0].Type != "text" || len(spec.Steps[0].Tools) != 1 || spec.Steps[0].Tools[0] != "web_search" {
		t.Fatalf("ascii arrow output = %+v tools = %+v", spec.Steps[0].Outputs, spec.Steps[0].Tools)
	}
	check := spec.Steps[1]
	if len(check.Inputs) != 1 || check.Inputs[0] != "report" {
		t.Fatalf("check inputs = %+v", check.Inputs)
	}
	if len(check.Outputs) != 2 || check.Outputs[0].Name != "accepted" || check.Outputs[0].Type != "bool" || check.Outputs[1].Name != "flag" || check.Outputs[1].Type != "bool" {
		t.Fatalf("check outputs = %+v", check.Outputs)
	}
	confirm := spec.Steps[2]
	if len(confirm.Inputs) != 1 || confirm.Inputs[0] != "report" {
		t.Fatalf("confirm inputs = %+v", confirm.Inputs)
	}
	if len(confirm.Outputs) != 2 || confirm.Outputs[0].Name != "通过" || confirm.Outputs[0].Type != "bool" || confirm.Outputs[1].Name != "结论" || confirm.Outputs[1].Type != "bool" {
		t.Fatalf("confirm outputs = %+v", confirm.Outputs)
	}
	if spec.Steps[3].Call == nil || spec.Steps[3].Call.Inputs["city"] != "report" || spec.Steps[3].Call.Outputs["report"] != "result" || spec.Steps[3].Outputs[0].Type != "any" {
		t.Fatalf("call = %+v outputs = %+v", spec.Steps[3].Call, spec.Steps[3].Outputs)
	}
	if verrs := Validate(spec); len(verrs) != 0 {
		t.Fatalf("Validate() errors = %v", verrs)
	}
	plain, plainErrs := Parse("# Spec: 类型\nGoal: 保持\n## Steps\n1. [act] 写\n  + → note % string\n  + → title % Text\n  + → raw % 布尔值\n2. [exit]\n")
	if len(plainErrs) != 0 || len(plain.Steps) != 2 || len(plain.Steps[0].Outputs) != 3 {
		t.Fatalf("plain = %+v errs = %v", plain.Steps, plainErrs)
	}
	got := []string{plain.Steps[0].Outputs[0].Type, plain.Steps[0].Outputs[1].Type, plain.Steps[0].Outputs[2].Type}
	if got[0] != "string" || got[1] != "Text" || got[2] != "布尔值" {
		t.Fatalf("types = %v", got)
	}
	punct, punctErrs := Parse("# Spec: 句号\nGoal: 核验\n## Steps\n1. [check] 核验\n  + → accepted % bool。\n  + → note % text。\n2. [exit]\n")
	if len(punctErrs) != 0 || len(punct.Steps) != 2 || len(punct.Steps[0].Outputs) != 2 {
		t.Fatalf("punct = %+v errs = %v", punct.Steps, punctErrs)
	}
	if punct.Steps[0].Outputs[0].Type != "bool" || punct.Steps[0].Outputs[1].Type != "text。" {
		t.Fatalf("punct types = %+v", punct.Steps[0].Outputs)
	}
	if verrs := Validate(punct); len(verrs) != 0 {
		t.Fatalf("punct Validate() errors = %v", verrs)
	}
}

func TestParseRejectsUnknownStepType(t *testing.T) {
	_, errs := Parse("# Spec: bad\nGoal: bad\n## Steps\n1. [unknown] bad\n")
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "UNKNOWN_STEP_TYPE") {
		t.Fatalf("errors = %v", errs)
	}
}

func TestParseStructuredStepModifiers(t *testing.T) {
	spec, errs := Parse(`# Spec: 结构参数
Goal: 解析结构参数
## Steps
1. [loop for-each item in items, collect result into results]
  1.1. [subtask retry=2 parallel]
    1.1.1. [reason] 处理
2. [branch]
  2.1. [case(mode == "fast")] 快速
3. [loop max=4]
`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	loop := spec.Steps[0]
	if loop.ForEach == nil || loop.ForEach.Item != "item" || loop.ForEach.List != "items" || loop.ForEach.CollectItem != "result" || loop.ForEach.CollectInto != "results" {
		t.Fatalf("for-each = %+v", loop.ForEach)
	}
	child := loop.Children[0]
	if child.Retry == nil || *child.Retry != 2 || !child.Parallel {
		t.Fatalf("child = %+v", child)
	}
	if spec.Steps[1].Children[0].Condition != `mode == "fast"` {
		t.Fatalf("condition = %q", spec.Steps[1].Children[0].Condition)
	}
	if spec.Steps[2].MaxIterations != 4 {
		t.Fatalf("max = %d", spec.Steps[2].MaxIterations)
	}
}

func TestParseStepToolWhitelist(t *testing.T) {
	spec, errs := Parse(`# Spec: 工具声明
Goal: 限制工具面
## Steps
1. [act free] 自由执行
  - 工具: web_search, web_fetch
`)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	tools := spec.Steps[0].Tools
	if len(tools) != 2 || tools[0] != "web_search" || tools[1] != "web_fetch" {
		t.Fatalf("tools=%v", tools)
	}
	wide, wideErrs := Parse("# Spec: 全角工具\nGoal: 限制工具面\n## Steps\n1. [act] 查询\n  － 工具: web_search\n2. [act] 抓取\n  - Tools：web_fetch\n3. [exit]\n")
	if len(wideErrs) != 0 || len(wide.Steps) != 3 || len(wide.Steps[0].Tools) != 1 || wide.Steps[0].Tools[0] != "web_search" || len(wide.Steps[1].Tools) != 1 || wide.Steps[1].Tools[0] != "web_fetch" {
		t.Fatalf("wide tools=%v %v errs=%v", wide.Steps[0].Tools, wide.Steps[1].Tools, wideErrs)
	}
	spaced, spacedErrs := Parse("# Spec: 空格工具\nGoal: 限制工具面\n## Steps\n1. [act] 查询\n  - 工具 : web_search\n2. [act] 抓取\n  -工具:web_fetch\n3. [act] 再查\n  - tools: web_search\n4. [exit]\n")
	if len(spacedErrs) != 0 || len(spaced.Steps) != 4 || len(spaced.Steps[0].Tools) != 1 || spaced.Steps[0].Tools[0] != "web_search" || len(spaced.Steps[1].Tools) != 1 || spaced.Steps[1].Tools[0] != "web_fetch" || len(spaced.Steps[2].Tools) != 1 || spaced.Steps[2].Tools[0] != "web_search" {
		t.Fatalf("spaced tools=%v %v %v errs=%v", spaced.Steps[0].Tools, spaced.Steps[1].Tools, spaced.Steps[2].Tools, spacedErrs)
	}
}

func TestValidationRejectsUnsupportedParallelStep(t *testing.T) {
	spec, parseErrs := Parse(`# Spec: 非法并行
Goal: 不得静默串行
## Steps
1. [act parallel] 不支持并行的原子步骤
`)
	if len(parseErrs) != 0 {
		t.Fatal(parseErrs)
	}
	if errs := Validate(spec); !diagnosticsContain(errs, "PARALLEL_STEP_TYPE") {
		t.Fatalf("errors=%v", errs)
	}
}

func TestValidationRejectsIgnoredStepStructureAndAttributes(t *testing.T) {
	spec, parseErrs := Parse(`# Spec: 无效结构
Goal: 不得静默忽略配置
Inputs:
- items % [text]
## Steps
1. [reason retry=1 adaptive max=2 for-each item in items] 原子步骤
  1.1. [reason] 不会执行的子步骤
`)
	if len(parseErrs) != 0 {
		t.Fatal(parseErrs)
	}
	errs := Validate(spec)
	for _, code := range []string{"NON_CONTAINER_CHILDREN", "RETRY_NOT_ALLOWED", "ADAPTIVE_NOT_ALLOWED", "LOOP_ATTRIBUTE_NOT_ALLOWED"} {
		if !diagnosticsContain(errs, code) {
			t.Fatalf("missing %s in %v", code, errs)
		}
	}
}

func TestParseAcceptsFullwidthSpecColon(t *testing.T) {
	spec, errs := Parse(`# Spec：北京天气
Id：beijing-weather
Goal：查询今天的天气
输出：
- report % text
## 步骤
1. [reason] 查询
  + → report % text
2. [check] 核验
  - ← report
  + → accepted % bool
  + → feedback % text
3. [exit]
`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	if spec.Title != "北京天气" || spec.ID != "beijing-weather" || spec.Goal != "查询今天的天气" {
		t.Fatalf("header = %+v", spec)
	}
	wideDecl, wideErrs := Parse("# Spec: 类型\nGoal: 全角类型\nOutputs:\n- report：text\n## Steps\n1. [act] 运行\n  - 工具：web_search, web_fetch\n  + → report：text\n2. [exit]\n")
	if len(wideErrs) != 0 {
		t.Fatalf("wide decl errors = %v", wideErrs)
	}
	if len(wideDecl.Outputs) != 1 || wideDecl.Outputs[0].Name != "report" || wideDecl.Outputs[0].Type != "text" {
		t.Fatalf("wide outputs = %+v", wideDecl.Outputs)
	}
	if len(wideDecl.Steps) != 2 || len(wideDecl.Steps[0].Tools) != 2 || wideDecl.Steps[0].Tools[0] != "web_search" {
		t.Fatalf("wide tools = %+v", wideDecl.Steps[0].Tools)
	}
	if wideDecl.Steps[0].Outputs[0].Name != "report" || wideDecl.Steps[0].Outputs[0].Type != "text" {
		t.Fatalf("wide step output = %+v", wideDecl.Steps[0].Outputs)
	}
	if len(spec.Outputs) != 1 || spec.Outputs[0].Name != "report" {
		t.Fatalf("outputs = %+v", spec.Outputs)
	}
	if errs := Validate(spec); len(errs) != 0 {
		t.Fatalf("Validate() errors = %v", errs)
	}
	headed, headedErrs := Parse(`# Spec：标题
Goal：查询
## 输出：
- report：text
## 步骤：
1. [call weather(city：query)] 查询
  + → report：text
2. [exit]
`)
	if len(headedErrs) != 0 {
		t.Fatalf("headed errors = %v", headedErrs)
	}
	if len(headed.Outputs) != 1 || headed.Outputs[0].Name != "report" || headed.Outputs[0].Type != "text" {
		t.Fatalf("headed outputs = %+v", headed.Outputs)
	}
	if headed.Steps[0].Call == nil || headed.Steps[0].Call.Callee != "weather" || headed.Steps[0].Call.Inputs["city"] != "query" {
		t.Fatalf("call = %+v", headed.Steps[0].Call)
	}
	if headed.Steps[0].Call.Outputs["report"] != "text" {
		t.Fatalf("call outputs = %+v", headed.Steps[0].Call.Outputs)
	}
	headedGoal, headedGoalErrs := Parse("# Spec: 天气\n## 目标：查询北京天气\n## 输出：\n- report：text\n## 步骤\n1. [reason] 查询\n  + → report：text\n2. [exit]\n")
	if len(headedGoalErrs) != 0 || headedGoal.Goal != "查询北京天气" || len(headedGoal.Outputs) != 1 {
		t.Fatalf("headed goal = %+v errs = %v", headedGoal, headedGoalErrs)
	}
	bold, boldErrs := Parse("# Spec: 天气\n**目标：** 查询北京天气\n## Steps\n1. [exit]\n")
	if len(boldErrs) != 0 || bold.Goal != "查询北京天气" {
		t.Fatalf("bold goal = %+v errs = %v", bold, boldErrs)
	}
}

func TestParseKeepsGoalSectionBody(t *testing.T) {
	headed, errs := Parse("# Spec: 天气\n## Goal\n查询北京今天的天气\n并给出温度\n## Steps\n1. [exit]\n")
	if len(errs) != 0 || headed.Goal != "查询北京今天的天气\n并给出温度" {
		t.Fatalf("headed body = %q errs = %v", headed.Goal, errs)
	}
	bold, boldErrs := Parse("# Spec: 天气\n**目标：**\n查询北京天气\n## Steps\n1. [exit]\n")
	if len(boldErrs) != 0 || bold.Goal != "查询北京天气" {
		t.Fatalf("bold body = %q errs = %v", bold.Goal, boldErrs)
	}
	bullets, bulletErrs := Parse("# Spec: 天气\n## 目标\n- 查询北京天气\n－ 给出温度\n* 说明风力\n> 来自预报\n## Steps\n1. [exit]\n")
	if len(bulletErrs) != 0 || bullets.Goal != "查询北京天气\n给出温度\n说明风力\n来自预报" {
		t.Fatalf("bullets = %q errs = %v", bullets.Goal, bulletErrs)
	}
	colon, colonErrs := Parse("# Spec: 天气\n## Goal\n查询北京天气：\n温度和风力\n## Outputs\n- report % text\n## Steps\n1. [exit]\n")
	if len(colonErrs) != 0 || colon.Goal != "查询北京天气：\n温度和风力" || len(colon.Outputs) != 1 {
		t.Fatalf("colon body = %q outputs = %+v errs = %v", colon.Goal, colon.Outputs, colonErrs)
	}
	inline, inlineErrs := Parse("# Spec: 天气\n## 目标：查询北京天气\n## Steps\n1. [exit]\n")
	if len(inlineErrs) != 0 || inline.Goal != "查询北京天气" {
		t.Fatalf("inline = %q errs = %v", inline.Goal, inlineErrs)
	}
	prose, proseErrs := Parse("# Spec: 天气\n## Goal\n查询北京天气\n输出：温度和风力\n## Outputs\n- report % text\n## Steps\n1. [exit]\n")
	if len(proseErrs) != 0 || prose.Goal != "查询北京天气\n输出：温度和风力" || len(prose.Outputs) != 1 {
		t.Fatalf("prose = %q outputs = %+v errs = %v", prose.Goal, prose.Outputs, proseErrs)
	}
	hash, hashErrs := Parse("# Spec: 天气\n# 目标\n查询北京天气\n# 注释\n先看预报\n## Steps\n1. [exit]\n")
	if len(hashErrs) != 0 || hash.Goal != "查询北京天气\n# 注释\n先看预报" {
		t.Fatalf("hash = %q errs = %v", hash.Goal, hashErrs)
	}
	plainBold, plainBoldErrs := Parse("# Spec: 天气\n**目标**\n查询北京天气\n## Steps\n1. [exit]\n")
	if len(plainBoldErrs) != 0 || plainBold.Goal != "查询北京天气" {
		t.Fatalf("plain bold = %q errs = %v", plainBold.Goal, plainBoldErrs)
	}
	keptStep, keptStepErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n输出：温度\n  + → report % text\n2. [exit]\n")
	if len(keptStepErrs) != 0 || len(keptStep.Steps) != 2 || len(keptStep.Steps[0].Outputs) != 1 || keptStep.Steps[0].Outputs[0].Name != "report" {
		t.Fatalf("step outputs = %+v errs = %v", keptStep.Steps, keptStepErrs)
	}
	sub, subErrs := Parse("# Spec: 天气\n## 目标\n查询北京天气\n## 说明\n给出温度和风力\n标识：必须写清城市\n## Steps\n1. [exit]\n")
	if len(subErrs) != 0 || sub.Goal != "查询北京天气\n## 说明\n给出温度和风力\n标识：必须写清城市" || sub.ID != "" {
		t.Fatalf("sub goal = %q id = %q errs = %v", sub.Goal, sub.ID, subErrs)
	}
	quoted, quotedErrs := Parse("# Spec: 天气\n> ## 目标\n> 查询北京天气\n> 输出：温度和风力\n> ## 说明\n> 给出风力\n## 步骤\n> 1. [act] 示例\n1. [exit]\n")
	if len(quotedErrs) != 0 || quoted.Goal != "查询北京天气\n输出：温度和风力\n## 说明\n给出风力" || len(quoted.Steps) != 1 {
		t.Fatalf("quoted = %q steps = %+v errs = %v", quoted.Goal, quoted.Steps, quotedErrs)
	}
	keptQuote, keptQuoteErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n> ## 目标\n> 查询别的\n  + → report % text\n2. [exit]\n")
	if len(keptQuoteErrs) != 0 || keptQuote.Goal != "查询" || len(keptQuote.Steps) != 2 || len(keptQuote.Steps[0].Outputs) != 1 || !strings.Contains(keptQuote.Steps[0].Instruction, "查询别的") {
		t.Fatalf("quoted inside step = %+v errs = %v", keptQuote, keptQuoteErrs)
	}
	alias, aliasErrs := Parse("# Spec: 天气\n## 目标/Goal\n查询北京天气\n输出：温度和风力\n## 步骤／Steps\n1. [exit]\n")
	if len(aliasErrs) != 0 || alias.Goal != "查询北京天气\n输出：温度和风力" || len(alias.Steps) != 1 {
		t.Fatalf("alias = %q steps = %+v errs = %v", alias.Goal, alias.Steps, aliasErrs)
	}
	punctHead, punctErrs := Parse("# Spec: 天气\n## 目标。\n查询北京天气\n## 输出。\n- report % text\n## 步骤！\n1. [exit]\n")
	if len(punctErrs) != 0 || punctHead.Goal != "查询北京天气" || len(punctHead.Outputs) != 1 || len(punctHead.Steps) != 1 {
		t.Fatalf("punct heading = %+v errs = %v", punctHead, punctErrs)
	}
	inlineAlias, inlineAliasErrs := Parse("# Spec: 天气\n## 目标/查询北京天气\n## 说明/补充\n给出风力\n## Steps\n1. [exit]\n")
	if len(inlineAliasErrs) != 0 || inlineAlias.Goal != "查询北京天气\n## 说明/补充\n给出风力" || len(inlineAlias.Steps) != 1 {
		t.Fatalf("inline alias = %q steps = %+v errs = %v", inlineAlias.Goal, inlineAlias.Steps, inlineAliasErrs)
	}
	inlinePunct, inlinePunctErrs := Parse("# Spec: 天气\n## 目标。查询北京天气\n## Steps\n1. [exit]\n")
	if len(inlinePunctErrs) != 0 || inlinePunct.Goal != "查询北京天气" || len(inlinePunct.Steps) != 1 {
		t.Fatalf("inline punct = %q steps = %+v errs = %v", inlinePunct.Goal, inlinePunct.Steps, inlinePunctErrs)
	}
	keptAlias, keptAliasErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n目标/Goal\n目标。\n  + → report % text\n2. [exit]\n")
	if len(keptAliasErrs) != 0 || keptAlias.Goal != "查询" || len(keptAlias.Steps) != 2 || len(keptAlias.Steps[0].Outputs) != 1 {
		t.Fatalf("alias inside step = %+v errs = %v", keptAlias, keptAliasErrs)
	}
	gloss, glossErrs := Parse("# Spec: 天气\n## 目标（必填）\n查询北京天气\n输出：温度和风力\n## 输出（文本）\n- report % text\n## 步骤（按顺序）\n1. [exit]\n")
	if len(glossErrs) != 0 || gloss.Goal != "查询北京天气\n输出：温度和风力" || len(gloss.Outputs) != 1 || len(gloss.Steps) != 1 {
		t.Fatalf("gloss = %+v errs = %v", gloss, glossErrs)
	}
	bareGloss, bareGlossErrs := Parse("# Spec: 天气\n目标（必填）\n查询北京天气\n输出（文本）\n- report % text\n1. 步骤（收尾）\n1. [exit]\n")
	if len(bareGlossErrs) != 0 || bareGloss.Goal != "查询北京天气" || len(bareGloss.Outputs) != 1 || len(bareGloss.Steps) != 1 {
		t.Fatalf("bare gloss = %+v errs = %v", bareGloss, bareGlossErrs)
	}
	inlineGloss, inlineGlossErrs := Parse("# Spec: 天气\n## 1. 目标（必填）查询北京天气\n## Goal (required)\n## 说明（补充）\n给出温度\n## Steps\n1. [exit]\n")
	if len(inlineGlossErrs) != 0 || inlineGloss.Goal != "查询北京天气\n## 说明（补充）\n给出温度" || len(inlineGloss.Steps) != 1 {
		t.Fatalf("inline gloss = %q steps = %+v errs = %v", inlineGloss.Goal, inlineGloss.Steps, inlineGlossErrs)
	}
	keptGloss, keptGlossErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n目标（必填）\n  + → report % text\n2. [exit]\n")
	if len(keptGlossErrs) != 0 || keptGloss.Goal != "查询" || len(keptGloss.Steps) != 2 || len(keptGloss.Steps[0].Outputs) != 1 {
		t.Fatalf("gloss inside step = %+v errs = %v", keptGloss, keptGlossErrs)
	}
	closed, closedErrs := Parse("# Spec: 天气\n## 目标 ##\n查询北京天气\n并给出温度\n## 【输出】\n- report % text\n## 步骤 ##\n1. [exit]\n")
	if len(closedErrs) != 0 || closed.Goal != "查询北京天气\n并给出温度" || len(closed.Outputs) != 1 || len(closed.Steps) != 1 {
		t.Fatalf("closed heading = %+v errs = %v", closed, closedErrs)
	}
	spaced, spacedErrs := Parse("# Spec: 天气\n## 目标 查询北京天气\n并给出温度\n## 【说明】\n给出风力\n＃＃ 步骤\n1. [exit]\n")
	if len(spacedErrs) != 0 || spaced.Goal != "查询北京天气\n并给出温度\n## 【说明】\n给出风力" || len(spaced.Steps) != 1 {
		t.Fatalf("spaced heading = %q steps = %+v errs = %v", spaced.Goal, spaced.Steps, spacedErrs)
	}
	bracket, bracketErrs := Parse("# Spec: 天气\n【目标】\n查询北京天气\n【输出】\n- report % text\n【步骤】\n1. [exit]\n")
	if len(bracketErrs) != 0 || bracket.Goal != "查询北京天气" || len(bracket.Outputs) != 1 || len(bracket.Steps) != 1 {
		t.Fatalf("bracket = %+v errs = %v", bracket, bracketErrs)
	}
	inlineBracket, inlineBracketErrs := Parse("# Spec: 天气\n## 【目标】查询北京天气\n## 1. 步骤 收尾\n1. [exit]\n")
	if len(inlineBracketErrs) != 0 || inlineBracket.Goal != "查询北京天气" || len(inlineBracket.Steps) != 1 {
		t.Fatalf("inline bracket = %q steps = %+v errs = %v", inlineBracket.Goal, inlineBracket.Steps, inlineBracketErrs)
	}
	stuck, stuckErrs := Parse("# Spec: 天气\n## 目标查询北京天气\n## 步骤\n1. [exit]\n")
	if len(stuckErrs) != 0 || strings.TrimSpace(stuck.Goal) != "" || len(stuck.Steps) != 1 {
		t.Fatalf("stuck heading = %q steps = %+v errs = %v", stuck.Goal, stuck.Steps, stuckErrs)
	}
	atxNum, atxNumErrs := Parse("# Spec: 天气\n## 1. 目标\n查询北京天气\n## 2. 输出\n- report % text\n## 3. 步骤\n1. [exit]\n")
	if len(atxNumErrs) != 0 || atxNum.Goal != "查询北京天气" || len(atxNum.Outputs) != 1 || len(atxNum.Steps) != 1 {
		t.Fatalf("atx numbered = %+v errs = %v", atxNum, atxNumErrs)
	}
	boldNum, boldNumErrs := Parse("# Spec: 天气\n**1. 目标**\n查询北京天气\n**（2）步骤**\n1. [exit]\n")
	if len(boldNumErrs) != 0 || boldNum.Goal != "查询北京天气" || len(boldNum.Steps) != 1 {
		t.Fatalf("bold numbered = %q steps = %+v errs = %v", boldNum.Goal, boldNum.Steps, boldNumErrs)
	}
	hashNum, hashNumErrs := Parse("# Spec: 天气\n# 1. 目标\n查询北京天气\n# 2. 注释\n先看预报\n# 3. 步骤\n1. [exit]\n")
	if len(hashNumErrs) != 0 || hashNum.Goal != "查询北京天气\n# 2. 注释\n先看预报" || len(hashNum.Steps) != 1 {
		t.Fatalf("hash numbered = %q steps = %+v errs = %v", hashNum.Goal, hashNum.Steps, hashNumErrs)
	}
	unknownNum, unknownNumErrs := Parse("# Spec: 天气\n## 1. 目标\n查询北京天气\n## 2. 说明\n给出温度和风力\n## 3. 步骤\n1. [exit]\n")
	if len(unknownNumErrs) != 0 || unknownNum.Goal != "查询北京天气\n## 2. 说明\n给出温度和风力" || len(unknownNum.Steps) != 1 {
		t.Fatalf("unknown numbered = %q steps = %+v errs = %v", unknownNum.Goal, unknownNum.Steps, unknownNumErrs)
	}
	inlineAtx, inlineAtxErrs := Parse("# Spec: 天气\n## （1）目标：查询北京天气\n## 2. 步骤\n1. [exit]\n")
	if len(inlineAtxErrs) != 0 || inlineAtx.Goal != "查询北京天气" || len(inlineAtx.Steps) != 1 {
		t.Fatalf("inline atx = %q steps = %+v errs = %v", inlineAtx.Goal, inlineAtx.Steps, inlineAtxErrs)
	}
	numbered, numberedErrs := Parse("# Spec: 天气\n1. 目标\n查询北京天气\n一、输出\n- report % text\n- 步骤\n1. [exit]\n")
	if len(numberedErrs) != 0 || numbered.Goal != "查询北京天气" || len(numbered.Outputs) != 1 || len(numbered.Steps) != 1 {
		t.Fatalf("numbered = %+v errs = %v", numbered, numberedErrs)
	}
	inlineNum, inlineNumErrs := Parse("# Spec: 天气\n（1）目标：查询北京天气\n2. 步骤\n1. [exit]\n")
	if len(inlineNumErrs) != 0 || inlineNum.Goal != "查询北京天气" || len(inlineNum.Steps) != 1 {
		t.Fatalf("inline numbered = %q steps = %+v errs = %v", inlineNum.Goal, inlineNum.Steps, inlineNumErrs)
	}
	proseNum, proseNumErrs := Parse("# Spec: 天气\n## Goal\n1. 查询天气\n- 查询北京天气\n## Steps\n1. [exit]\n")
	if len(proseNumErrs) != 0 || proseNum.Goal != "1. 查询天气\n查询北京天气" || len(proseNum.Steps) != 1 {
		t.Fatalf("numbered prose = %q steps = %+v errs = %v", proseNum.Goal, proseNum.Steps, proseNumErrs)
	}
	keptMark, keptMarkErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n1. 目标\n- 步骤\n  + → report % text\n2. [exit]\n")
	if len(keptMarkErrs) != 0 || keptMark.Goal != "查询" || len(keptMark.Steps) != 2 || len(keptMark.Steps[0].Outputs) != 1 {
		t.Fatalf("mark inside step = %+v errs = %v", keptMark, keptMarkErrs)
	}
	outline, outlineErrs := Parse("# Spec: 天气\n目标\n查询北京天气\n输出\n- report % text\n步骤\n1. [exit]\n")
	if len(outlineErrs) != 0 || outline.Goal != "查询北京天气" || len(outline.Outputs) != 1 || len(outline.Steps) != 1 {
		t.Fatalf("outline = %+v errs = %v", outline, outlineErrs)
	}
	note, noteErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n## 备注\n  + → report % text\n2. [exit]\n")
	if len(noteErrs) != 0 || len(note.Steps) != 2 || len(note.Steps[0].Outputs) != 0 {
		t.Fatalf("note outputs = %+v errs = %v", note.Steps, noteErrs)
	}
	noteNum, noteNumErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n## 1. 说明\n  + → report % text\n2. [exit]\n")
	if len(noteNumErrs) != 0 || len(noteNum.Steps) != 2 || len(noteNum.Steps[0].Outputs) != 0 {
		t.Fatalf("numbered note outputs = %+v errs = %v", noteNum.Steps, noteNumErrs)
	}
	wrapped, wrappedErrs := Parse("# Spec: 天气\nGoal: \"查询北京天气\"\n## Steps\n1. [exit]\n")
	if len(wrappedErrs) != 0 || wrapped.Goal != "查询北京天气" || len(wrapped.Steps) != 1 {
		t.Fatalf("wrapped goal = %q steps = %+v errs = %v", wrapped.Goal, wrapped.Steps, wrappedErrs)
	}
	corner, cornerErrs := Parse("# Spec: 天气\n## 目标：「查询北京天气」\n\"再看风力\"\n输出：温度和风力\n## Steps\n1. [exit]\n")
	if len(cornerErrs) != 0 || corner.Goal != "查询北京天气\n再看风力\n输出：温度和风力" || len(corner.Steps) != 1 {
		t.Fatalf("corner goal = %q steps = %+v errs = %v", corner.Goal, corner.Steps, cornerErrs)
	}
	scalar, scalarErrs := Parse("# Spec: 天气\nGoal: |\n查询北京天气\n输出：温度和风力\n## Steps\n1. [exit]\n")
	if len(scalarErrs) != 0 || scalar.Goal != "查询北京天气\n输出：温度和风力" || len(scalar.Steps) != 1 {
		t.Fatalf("scalar goal = %q steps = %+v errs = %v", scalar.Goal, scalar.Steps, scalarErrs)
	}
	folded, foldedErrs := Parse("# Spec: 天气\nGoal: > 查询北京天气\nGoal: |-\n给出风力\n## Steps\n1. [exit]\n")
	if len(foldedErrs) != 0 || folded.Goal != "查询北京天气\n给出风力" || len(folded.Steps) != 1 {
		t.Fatalf("folded goal = %q steps = %+v errs = %v", folded.Goal, folded.Steps, foldedErrs)
	}
	marker, markerErrs := Parse("# Spec: 天气\nGoal: \"|\"\n查询北京天气\n## Steps\n1. [exit]\n")
	if len(markerErrs) != 0 || marker.Goal != "查询北京天气" || len(marker.Steps) != 1 {
		t.Fatalf("quoted marker = %q steps = %+v errs = %v", marker.Goal, marker.Steps, markerErrs)
	}
	plainFold, plainFoldErrs := Parse("# Spec: 天气\nGoal: >\n查询北京天气\n## Steps\n1. [exit]\n")
	if len(plainFoldErrs) != 0 || plainFold.Goal != "查询北京天气" {
		t.Fatalf("plain fold = %q errs = %v", plainFold.Goal, plainFoldErrs)
	}
	apostrophe, apostropheErrs := Parse("# Spec: 天气\nGoal: it's rainy\n## Steps\n1. [exit]\n")
	if len(apostropheErrs) != 0 || apostrophe.Goal != "it's rainy" {
		t.Fatalf("apostrophe = %q errs = %v", apostrophe.Goal, apostropheErrs)
	}
	nestedQuote, nestedErrs := Parse("# Spec: 天气\nGoal: \"say \"hi\"\"\n## Steps\n1. [exit]\n")
	if len(nestedErrs) != 0 || nestedQuote.Goal != "\"say \"hi\"\"" {
		t.Fatalf("nested quote = %q errs = %v", nestedQuote.Goal, nestedErrs)
	}
	keptID, keptIDErrs := Parse("# Spec: 天气\nId: \"beijing\"\nGoal: 查询\n## Steps\n1. [exit]\n")
	if len(keptIDErrs) != 0 || keptID.ID != "\"beijing\"" || keptID.Goal != "查询" {
		t.Fatalf("id = %q goal = %q errs = %v", keptID.ID, keptID.Goal, keptIDErrs)
	}
	quotedTitle, quotedTitleErrs := Parse("> # Spec: 天气\n## 目标\n查询北京天气\n## 步骤\n> 1. [exit]\n1. [exit]\n")
	if len(quotedTitleErrs) != 0 || quotedTitle.Title != "天气" || quotedTitle.Goal != "查询北京天气" || len(quotedTitle.Steps) != 1 {
		t.Fatalf("quoted title = %+v errs = %v", quotedTitle, quotedTitleErrs)
	}
	coded, codedErrs := Parse("# Spec: 天气\n## `目标`\n查询北京天气\n输出：温度和风力\n## `说明`\n给出风力\n## `步骤`\n1. [exit]\n")
	if len(codedErrs) != 0 || coded.Goal != "查询北京天气\n输出：温度和风力\n## `说明`\n给出风力" || len(coded.Steps) != 1 {
		t.Fatalf("code heading = %q steps = %+v errs = %v", coded.Goal, coded.Steps, codedErrs)
	}
	bareCode, bareCodeErrs := Parse("# Spec: 天气\n`目标`\n查询北京天气\n使用 `web_search` 查询\n`步骤`\n1. [exit]\n")
	if len(bareCodeErrs) != 0 || bareCode.Goal != "查询北京天气\n使用 `web_search` 查询" || len(bareCode.Steps) != 1 {
		t.Fatalf("bare code = %q steps = %+v errs = %v", bareCode.Goal, bareCode.Steps, bareCodeErrs)
	}
	codeValue, codeValueErrs := Parse("# Spec: 天气\nGoal: `查询北京天气`\nGoal: \"`再看风力`\"\n## Steps\n1. [exit]\n")
	if len(codeValueErrs) != 0 || codeValue.Goal != "查询北京天气\n再看风力" || len(codeValue.Steps) != 1 {
		t.Fatalf("code value = %q steps = %+v errs = %v", codeValue.Goal, codeValue.Steps, codeValueErrs)
	}
	bracketColon, bracketColonErrs := Parse("# Spec: 天气\n【目标】：查询北京天气\n输出：温度和风力\n【输出】\n- report % text\n【步骤】\n1. [exit]\n")
	if len(bracketColonErrs) != 0 || bracketColon.Goal != "查询北京天气\n输出：温度和风力" || len(bracketColon.Outputs) != 1 || len(bracketColon.Steps) != 1 {
		t.Fatalf("bracket colon = %+v errs = %v", bracketColon, bracketColonErrs)
	}
	bracketGloss, bracketGlossErrs := Parse("# Spec: 天气\n【目标】（必填）\n查询北京天气\n【说明】（补充）\n给出风力\n【步骤】\n1. [exit]\n")
	if len(bracketGlossErrs) != 0 || bracketGloss.Goal != "查询北京天气\n【说明】（补充）\n给出风力" || len(bracketGloss.Steps) != 1 {
		t.Fatalf("bracket gloss = %q steps = %+v errs = %v", bracketGloss.Goal, bracketGloss.Steps, bracketGlossErrs)
	}
	keptCode, keptCodeErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n`目标`\n1. `目标`：别的\n  + → report % text\n2. [exit]\n")
	if len(keptCodeErrs) != 0 || keptCode.Goal != "查询" || len(keptCode.Steps) != 2 || len(keptCode.Steps[0].Outputs) != 1 {
		t.Fatalf("code inside step = %+v errs = %v", keptCode, keptCodeErrs)
	}
	keptBracket, keptBracketErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 查询\n【目标】：别的\n  + → report % text\n2. [exit]\n")
	if len(keptBracketErrs) != 0 || keptBracket.Goal != "别的" || len(keptBracket.Steps) != 2 || len(keptBracket.Steps[0].Outputs) != 1 {
		t.Fatalf("bracket inside step = %+v errs = %v", keptBracket, keptBracketErrs)
	}
	doubleTitle, doubleTitleErrs := Parse(">> # Spec: 天气\n## 目标\n查询北京天气\n## 步骤\n1. [exit]\n")
	if doubleTitle.Title != "" || doubleTitle.Goal != "查询北京天气" || len(doubleTitle.Steps) != 1 || !diagnosticsContain(doubleTitleErrs, "MISSING_SPEC_TITLE") {
		t.Fatalf("double title = %+v errs = %v", doubleTitle, doubleTitleErrs)
	}
	empty, emptyErrs := Parse("# Spec: 天气\n## Steps\n1. [exit]\n")
	if len(emptyErrs) != 0 || strings.TrimSpace(empty.Goal) != "" {
		t.Fatalf("empty = %q errs = %v", empty.Goal, emptyErrs)
	}
	if validation := Validate(empty); !diagnosticsContain(validation, "MISSING_GOAL") {
		t.Fatalf("Validate = %v", validation)
	}
}

func TestParseAcceptsFullwidthStepBrackets(t *testing.T) {
	spec, errs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. ［reason］ 查询\n2、[exit]\n")
	if len(errs) != 0 || len(spec.Steps) != 2 || spec.Steps[0].Type != StepReason || spec.Steps[0].Summary != "查询" || spec.Steps[1].Type != StepExit {
		t.Fatalf("steps = %+v errs = %v", spec.Steps, errs)
	}
	prose, proseErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. 查询天气\n1. exit\n")
	if len(prose.Steps) != 0 || len(proseErrs) != 0 {
		t.Fatalf("prose steps = %+v errs = %v", prose.Steps, proseErrs)
	}
	kept, keptErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 使用【北京】\n2. [exit]\n")
	if len(keptErrs) != 0 || len(kept.Steps) != 2 || kept.Steps[0].Summary != "使用【北京】" {
		t.Fatalf("summary = %+v errs = %v", kept.Steps, keptErrs)
	}
}

func TestParsePlainHopPythonLabel(t *testing.T) {
	spec, errs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act free] 搜索\n  - 工具: web_search\n  hop_python:\n  weather = web_search(\"北京天气\")\n  + → weather % str\n2. [check] 确认\n  + → accepted % bool\n3. [exit]\n")
	if len(errs) != 0 || len(spec.Steps) != 3 || spec.Steps[0].Body != `weather = web_search("北京天气")` || len(spec.Steps[0].Tools) != 1 || len(spec.Steps[0].Outputs) != 1 || spec.Steps[0].Outputs[0].Name != "weather" {
		t.Fatalf("plain body = %+v errs = %v", spec.Steps, errs)
	}
	indented, indentedErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 搜索\n  hop_python:\n    weather = web_search(query: \"北京天气\")\n2. [exit]\n")
	if len(indentedErrs) != 0 || len(indented.Steps) != 2 || indented.Steps[0].Body != `weather = web_search(query: "北京天气")` || indented.Steps[1].Type != StepExit {
		t.Fatalf("indented body = %+v errs = %v", indented.Steps, indentedErrs)
	}
	nested, nestedErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [act] 搜索\n  hop_python:\n    if True:\n      weather = web_search(query: \"北京天气\")\n2. [exit]\n")
	nestedBody := ""
	if len(nested.Steps) > 0 {
		nestedBody = nested.Steps[0].Body
	}
	if len(nestedErrs) != 0 || len(nested.Steps) != 2 || nestedBody != "if True:\n  weather = web_search(query: \"北京天气\")" || nested.Steps[1].Type != StepExit {
		t.Fatalf("nested body = %q errs = %v", nestedBody, nestedErrs)
	}
	reason, reasonErrs := Parse("# Spec: 天气\nGoal: 查询\n## Steps\n1. [reason] 想\n  hop_python:\n  weather = web_search(\"北京天气\")\n2. [exit]\n")
	if len(reasonErrs) != 0 || len(reason.Steps) != 2 || reason.Steps[0].Body != "" || reason.Steps[0].Type != StepReason {
		t.Fatalf("reason body = %+v errs = %v", reason.Steps, reasonErrs)
	}
}

func TestParseKeepsColonsInsideHopPython(t *testing.T) {
	spec, errs := Parse("# Spec: py\nGoal: keep\n## Steps\n1. [act] 运行\n  > ```hop_python\n  > report = \"a:b：c\"\n  > ```\n")
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	if len(spec.Steps) != 1 || spec.Steps[0].Body != `report = "a:b：c"` {
		t.Fatalf("body = %q", spec.Steps[0].Body)
	}
	rawFence, rawErrs := Parse("# Spec: py\nGoal: keep\n## Steps\n1. [act] 运行\n```hop_python\n## 标题\n1. [act] 假\n```\n2. [exit]\n")
	if len(rawErrs) != 0 || len(rawFence.Steps) != 2 || rawFence.Steps[0].Summary != "运行" || rawFence.Steps[0].Body != "## 标题\n1. [act] 假" || rawFence.Steps[1].Type != StepExit {
		t.Fatalf("raw fence steps = %+v errs = %v", rawFence.Steps, rawErrs)
	}
	assigned, assignedErrs := Parse("# Spec: py\nGoal: keep\n## Steps\n1. [act] 运行\n  ```hop_python\n  report = \"a:b：c\"\n  ```\n2. [exit]\n")
	if len(assignedErrs) != 0 || len(assigned.Steps) != 2 || assigned.Steps[0].Body != `report = "a:b：c"` || assigned.Steps[1].Type != StepExit {
		t.Fatalf("assigned body = %+v errs = %v", assigned.Steps, assignedErrs)
	}
	afterExit, afterErrs := Parse("# Spec: py\nGoal: keep\n## Steps\n1. [exit]\n```hop_python\n1. [act] 假\n```\n")
	if len(afterErrs) != 0 || len(afterExit.Steps) != 1 || afterExit.Steps[0].Type != StepExit || afterExit.Steps[0].Body != "" {
		t.Fatalf("fence after exit = %+v errs = %v", afterExit.Steps, afterErrs)
	}
}

func diagnosticsContain(errs []error, code string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}
