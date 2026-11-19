package goal

import "testing"

func TestApplyMissingActionKeepsExplicitVerb(t *testing.T) {
	args := map[string]interface{}{
		"action":        "fail",
		"understanding": "已经写了理解",
		"steps":         []interface{}{map[string]interface{}{"title": "一步"}},
	}
	if got := ApplyMissingAction(args); got != "fail" {
		t.Fatalf("action = %q", got)
	}
}

func TestApplyMissingActionInfersPlanBody(t *testing.T) {
	args := map[string]interface{}{
		"acceptance_criteria": []interface{}{"窗口能打开"},
		"project_path":        `E:\bird-test2`,
		"steps":               []interface{}{map[string]interface{}{"detail": "确认仓库为空"}},
		"understanding":       "做一个图形版游戏",
	}
	if got := ApplyMissingAction(args); got != "plan" {
		t.Fatalf("action = %q", got)
	}
	if args["action"] != "plan" {
		t.Fatalf("args action = %#v", args["action"])
	}
}

func TestApplyMissingActionInfersOtherVerbs(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{name: "step", args: map[string]interface{}{"index": 1.0, "status": "done"}, want: "step"},
		{name: "create", args: map[string]interface{}{"objective": "做一个游戏"}, want: "create"},
		{name: "complete", args: map[string]interface{}{"summary": "窗口能打开"}, want: "complete"},
		{name: "fail", args: map[string]interface{}{"reason": "无法编译"}, want: "fail"},
		{name: "empty", args: map[string]interface{}{"project_path": `E:\bird-test2`}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyMissingAction(tc.args); got != tc.want {
				t.Fatalf("action = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIndexArgAcceptsNumberAndString(t *testing.T) {
	n, ok := IndexArg(map[string]interface{}{"index": "2"}, "index")
	if !ok || n != 2 {
		t.Fatalf("string index = %d ok=%v", n, ok)
	}
	n, ok = IndexArg(map[string]interface{}{"index": 1.0}, "index")
	if !ok || n != 1 {
		t.Fatalf("float index = %d ok=%v", n, ok)
	}
	if _, ok = IndexArg(map[string]interface{}{"index": "step"}, "index"); ok {
		t.Fatal("word index was accepted")
	}
	if _, ok = IndexArg(map[string]interface{}{}, "index"); ok {
		t.Fatal("missing index was present")
	}
}

func TestParseStepsExpandsJSONStringElement(t *testing.T) {
	// The model wrapped the whole steps document in a string inside the array:
	// steps=["[{\"title\":...}, ...]"]. The element must expand into real steps
	// instead of becoming one step whose title is the JSON text.
	steps, err := ParseSteps([]interface{}{
		`[{"title": "探测工具链并确定实现方案", "detail": "只读探查"}, {"title": "搭建工程骨架", "detail": "创建 src"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %#v", steps)
	}
	if steps[0].Title != "探测工具链并确定实现方案" || steps[0].Detail != "只读探查" {
		t.Fatalf("step 1 = %#v", steps[0])
	}
	if steps[1].Index != 2 || steps[1].Title != "搭建工程骨架" {
		t.Fatalf("step 2 = %#v", steps[1])
	}
}

func TestParseStepsAcceptsSingleObjectAndFencedDocument(t *testing.T) {
	steps, err := ParseSteps(map[string]interface{}{"title": "唯一步骤", "detail": "做完即可"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != "唯一步骤" {
		t.Fatalf("steps = %#v", steps)
	}

	steps, err = ParseSteps("```json\n[{\"title\": \"第一步\"}, {\"title\": \"第二步\"}]\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Title != "第一步" || steps[1].Title != "第二步" {
		t.Fatalf("steps = %#v", steps)
	}

	// Single-line fences — no newline after the info token or at all.
	steps, err = ParseSteps("```[{\"title\": \"带空格的标题\"}]```")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != "带空格的标题" {
		t.Fatalf("steps = %#v", steps)
	}
	steps, err = ParseSteps("\uFEFF[{\"title\": \"BOM 步骤\"}]")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != "BOM 步骤" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsKeepsNonJSONBracketTitle(t *testing.T) {
	// A title that only looks bracketed — HopSpec-style "[reason] 摘要" — is
	// not JSON and must stay a title, not fail or expand.
	steps, err := ParseSteps([]interface{}{"[reason] 摘要", "普通步骤"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Title != "[reason] 摘要" || steps[1].Title != "普通步骤" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsDoubleEncodedDocument(t *testing.T) {
	steps, err := ParseSteps(`"[{\"title\": \"内层步骤\"}]"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != "内层步骤" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsUnwrapsStepsMemberDocument(t *testing.T) {
	steps, err := ParseSteps(`{"steps": [{"title": "第一步"}, {"title": "第二步"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Title != "第一步" || steps[1].Title != "第二步" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsNonStepDocumentStaysTitle(t *testing.T) {
	// A JSON document that decodes to non-step elements was a title before:
	// the split-lines fallback keeps it instead of failing the plan call.
	steps, err := ParseSteps(`[1,2,3]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != `[1,2,3]` {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsRejectsEncodedElementWithNoUsableSteps(t *testing.T) {
	// An element that IS a steps document but yields no titled steps fails
	// the call so the corrective turn can fix it, instead of storing the
	// JSON text as a step title.
	if _, err := ParseSteps([]interface{}{"正常步骤", `[{"unexpected": 1}]`}); err == nil {
		t.Fatal("expected an error for an encoded element without usable steps")
	}
}

func TestParseStepsJSONWithNoUsableStepsStillErrors(t *testing.T) {
	// A JSON document that decodes but yields no titled steps must error the
	// way the object form always did, not fall back to storing it as a title.
	if _, err := ParseSteps(`[{"unexpected": 1}]`); err == nil {
		t.Fatal("expected an error for a JSON document without usable steps")
	}
}

func TestParseStepsRecoversTruncatedDocument(t *testing.T) {
	// The live corrupt row stored this shape: a JSON array string cut off at
	// the last complete object — the closing "]" never made it. Recover the
	// complete elements instead of keeping the blob as a title.
	steps, err := ParseSteps(`[{"title": "第一步", "detail": "d"}, {"title": "第二步"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Title != "第一步" || steps[0].Detail != "d" || steps[1].Title != "第二步" {
		t.Fatalf("steps = %#v", steps)
	}

	// A trailing partial element is dropped at the last complete object.
	steps, err = ParseSteps(`[{"title": "完成"}, {"title": "半截`)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != "完成" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsDecodesJSONLinesInText(t *testing.T) {
	// A multi-line string whose individual lines hold step documents gets
	// the same per-element decoding an array would — plain lines stay titles.
	steps, err := ParseSteps("准备环境\n[{\"title\": \"编码步骤\"}, {\"title\": \"验证步骤\"}]\n收尾交付")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 4 {
		t.Fatalf("steps = %#v", steps)
	}
	if steps[0].Title != "准备环境" || steps[1].Title != "编码步骤" || steps[2].Title != "验证步骤" || steps[3].Title != "收尾交付" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsBareOpenerStaysTitle(t *testing.T) {
	// "[" would decode to an empty doc under the truncation completion and
	// error the whole call; a lone opener is a title character instead. Same
	// inside a list element or a step object's title.
	steps, err := ParseSteps([]interface{}{"正常步骤", "["})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[1].Title != "[" {
		t.Fatalf("steps = %#v", steps)
	}
	steps, err = ParseSteps([]interface{}{map[string]interface{}{"title": "{"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Title != "{" {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestParseStepsUsesDetailWhenTitleMissing(t *testing.T) {
	steps, err := ParseSteps([]interface{}{
		map[string]interface{}{"detail": "确认仓库为空并锁定技术选型，只写决策文档。后面是很长的说明。"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %#v", steps)
	}
	if steps[0].Title != "确认仓库为空并锁定技术选型，只写决策文档" {
		t.Fatalf("title = %q", steps[0].Title)
	}
	if steps[0].Detail == "" {
		t.Fatal("detail was dropped")
	}
}
