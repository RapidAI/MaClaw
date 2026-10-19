package desktopbot

import "testing"

func TestVisibleNoSendIsSilent(t *testing.T) {
	view := Visible(Turn{Prose: "无变化", Thinking: "\x01secret", Waiting: true})
	if view.Bubble || view.Failed || view.Sentence != "" || view.Arrange || view.Notify {
		t.Fatalf("%#v", view)
	}
	if view.Sentence == "无变化" || view.Activity != "" {
		t.Fatalf("%#v", view)
	}
}

func TestVisibleSentenceIsSendToUser(t *testing.T) {
	view := Visible(Turn{Prose: "无变化", Thinking: "\x01secret", SendToUser: "首页已打开。", Waiting: true})
	if !view.Bubble || view.Failed || view.Sentence != "首页已打开。" || view.Arrange || view.Notify {
		t.Fatalf("%#v", view)
	}
}

func TestVisibleThinkingIsNotASentenceOrAStep(t *testing.T) {
	view := Visible(Turn{Thinking: "\x01the plan", Prose: "I'll open it", Activity: "\x01the plan"})
	if view.Bubble || view.Sentence != "" || view.Activity != "" {
		t.Fatalf("%#v", view)
	}
}

func TestVisibleNamedFillersAreNotBubbles(t *testing.T) {
	fillers := []string{
		"后台没有返回结果。",
		"後台沒有返回結果。",
		"The backend returned no result.",
		"MaClawSrv 没有返回结果",
		"run ended without a result",
		"run run_1 ended as cancelled without a result",
	}
	for _, filler := range fillers {
		view := Visible(Turn{Prose: filler, SendToUser: filler, Error: filler})
		if view.Bubble || view.Sentence != "" || view.Failed {
			t.Fatalf("%q -> %#v", filler, view)
		}
	}
	named := Visible(Turn{Prose: "截个图"})
	if named.Bubble || named.Sentence != "" {
		t.Fatalf("task name became a sentence: %#v", named)
	}
	wrapped := Visible(Turn{Error: "certificate failed: run run_1 ended as cancelled without a result"})
	if !wrapped.Bubble || !wrapped.Failed || wrapped.Sentence == "" {
		t.Fatalf("a longer error was swallowed: %#v", wrapped)
	}
}

func TestRealChoiceSkipsPlaceholderLabels(t *testing.T) {
	for _, label := range []string{"", " ", "无变化", "(无输出)", "no change"} {
		if RealChoice(label) {
			t.Fatalf("placeholder label counted as a choice: %q", label)
		}
	}
	if !RealChoice(" 继续 ") {
		t.Fatal("real choice was dropped")
	}
}

func TestVisibleNotContentIsNotASentence(t *testing.T) {
	for _, text := range []string{"无变化", "(无输出)", "（无输出）", "no change", "No Change"} {
		view := Visible(Turn{SendToUser: text, Prose: "首页变了", Waiting: true})
		if view.Bubble || view.Sentence != "" || view.Failed {
			t.Fatalf("%q -> %#v", text, view)
		}
	}
}

func TestVisibleRealErrorStaysAFailedBubble(t *testing.T) {
	view := Visible(Turn{Error: "打不开页面", Prose: "无变化"})
	if !view.Bubble || !view.Failed || view.Sentence != "打不开页面" {
		t.Fatalf("%#v", view)
	}
}

func TestVisiblePlanningProseIsNotArrange(t *testing.T) {
	view := Visible(Turn{UserText: "开始吧", PlanReply: "先打开网站，再告诉你结果。", Prose: "先打开网站，再告诉你结果。"})
	if view.Arrange || view.Bubble || view.Sentence != "" {
		t.Fatalf("%#v", view)
	}
}

func TestActivityPhraseStaysOnTheClosedSet(t *testing.T) {
	if got := ActivityPhrase("正在点击"); got != "正在点击" {
		t.Fatalf("phrase = %q", got)
	}
	if got := ActivityPhrase("\x01thinking"); got != "" {
		t.Fatalf("thinking phrase = %q", got)
	}
	if got := ActivityPhrase("正在处理"); got != "" {
		t.Fatalf("blocked phrase = %q", got)
	}
}
