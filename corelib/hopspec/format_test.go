package hopspec

import (
	"reflect"
	"testing"
)

func TestFormatRoundTripPreservesSpec(t *testing.T) {
	spec, parseErrs := Parse(`# Spec: 完整格式
Id: format-test
Goal: 验证格式化往返
Constraints:
- 保留结构
Inputs:
- document % text  # 原文
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect report into reports]
  + → reports % [text]
  1.1. [call child(document: item) parallel] 单项调用
    + → report: result
2. [subtask retry=2 adaptive]
  2.1. [act free require_human=true] 自由执行
    - ← document
    - Tools: read_file, web_search
    + → content % text
    > 第一行说明
    > 第二行说明
  2.2. [check final] 最终检查
    + → accepted % bool
  2.3. [on_fail] 降级
    2.3.1. [act] 确定性输出
      + → fallback % text
      > ` + "```hop_python" + `
      > fallback = "降级"
      > ` + "```" + `
3. [branch]
  3.1. [case(len(document) > 0)] 非空
    3.1.1. [reason] 处理
  3.2. [case(else)] 空输入
4. [loop max=2]
  4.1. [continue]
  4.2. [break]
5. [exit]
`)
	if len(parseErrs) != 0 {
		t.Fatal(parseErrs)
	}
	formatted := Format(spec)
	roundTrip, parseErrs := Parse(formatted)
	if len(parseErrs) != 0 {
		t.Fatalf("formatted spec is invalid: %v\n%s", parseErrs, formatted)
	}
	clearSpecSources(&spec)
	clearSpecSources(&roundTrip)
	if !reflect.DeepEqual(spec, roundTrip) {
		t.Fatalf("round trip differs\nformatted:\n%s\nwant=%#v\ngot=%#v", formatted, spec, roundTrip)
	}
}

func clearSpecSources(spec *SpecAST) {
	var clear func([]*Step)
	clear = func(steps []*Step) {
		for _, step := range steps {
			step.Source = SourceLocation{}
			clear(step.Children)
		}
	}
	clear(spec.Steps)
}
