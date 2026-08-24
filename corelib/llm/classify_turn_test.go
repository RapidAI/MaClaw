package llm

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestClassifyTurn_ShortFast(t *testing.T) {
	got := ClassifyTurn("你好", ClassifyHints{})
	if got.Task != TaskFast {
		t.Fatalf("got %s (%s)", got.Task, got.Reason)
	}
}

func TestClassifyTurn_Summary(t *testing.T) {
	got := ClassifyTurn("请总结一下这段会议纪要", ClassifyHints{})
	if got.Task != TaskSummary {
		t.Fatalf("got %s (%s)", got.Task, got.Reason)
	}
}

func TestClassifyTurn_ReasoningCode(t *testing.T) {
	got := ClassifyTurn("帮我调试这个 compile error in main.go", ClassifyHints{})
	if got.Task != TaskReasoning {
		t.Fatalf("got %s (%s)", got.Task, got.Reason)
	}
}

func TestClassifyTurn_ShortOpsCues(t *testing.T) {
	for _, text := range []string{"帮我修一下", "run npm install", "打开终端"} {
		got := ClassifyTurn(text, ClassifyHints{})
		if got.Task != TaskReasoning {
			t.Fatalf("%q got %s (%s)", text, got.Task, got.Reason)
		}
	}
}

func TestClassifyTurn_ImageWeatherLooksLikeVision(t *testing.T) {
	for _, text := range []string{"这张图里的天气如何", "图中有什么", "看看图里写了什么"} {
		got := ClassifyTurn(text, ClassifyHints{})
		if got.Task != TaskVision {
			t.Fatalf("%q got %s (%s), want vision", text, got.Task, got.Reason)
		}
	}
	got := ClassifyTurn("北京天气", ClassifyHints{})
	if got.Task == TaskVision {
		t.Fatalf("plain weather must not become vision: %s (%s)", got.Task, got.Reason)
	}
	got = ClassifyTurn("地图中的杭州天气", ClassifyHints{})
	if got.Task == TaskVision {
		t.Fatalf("地图中 must not count as a photo cue: %s (%s)", got.Task, got.Reason)
	}
}

func TestDecideTurn_UsesAuxForFast(t *testing.T) {
	primary := corelib.MaclawLLMConfig{URL: "http://p", Key: "pk", Model: "big"}
	aux := corelib.AuxiliaryLLMConfig{URL: "http://a", Key: "ak", Model: "flash"}
	r := NewModelRouter(nil)
	cfg, task, source, _ := DecideTurn(r, primary, aux, "hello", ClassifyHints{})
	if task != TaskFast {
		t.Fatalf("task=%s", task)
	}
	if source != "aux" || cfg.Model != "flash" {
		t.Fatalf("source=%s model=%s", source, cfg.Model)
	}
}

func TestDecideTurn_ExplicitRouteWins(t *testing.T) {
	primary := corelib.MaclawLLMConfig{URL: "http://p", Key: "pk", Model: "big"}
	aux := corelib.AuxiliaryLLMConfig{URL: "http://a", Key: "ak", Model: "flash"}
	r := NewModelRouter(map[string]ModelRoute{
		"fast": {Model: "route-fast"},
	})
	cfg, task, source, _ := DecideTurn(r, primary, aux, "hi", ClassifyHints{})
	if task != TaskFast || source != "route" || cfg.Model != "route-fast" {
		t.Fatalf("task=%s source=%s model=%s", task, source, cfg.Model)
	}
}

func TestDecideTurn_ReasoningStaysPrimaryWithoutRoute(t *testing.T) {
	primary := corelib.MaclawLLMConfig{URL: "http://p", Key: "pk", Model: "big"}
	aux := corelib.AuxiliaryLLMConfig{URL: "http://a", Key: "ak", Model: "flash"}
	r := NewModelRouter(nil)
	cfg, task, source, _ := DecideTurn(r, primary, aux, "fix the bug in this stack trace", ClassifyHints{})
	if task != TaskReasoning || source != "primary" || cfg.Model != "big" {
		t.Fatalf("task=%s source=%s model=%s", task, source, cfg.Model)
	}
}
