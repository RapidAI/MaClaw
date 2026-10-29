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
