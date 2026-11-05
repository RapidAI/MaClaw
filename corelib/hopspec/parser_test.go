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

func diagnosticsContain(errs []error, code string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}
