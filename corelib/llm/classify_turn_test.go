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

func TestClassifyTurn_ForceAndAttachments(t *testing.T) {
	if got := ClassifyTurn("hi", ClassifyHints{ForceReasoning: true}); got.Task != TaskReasoning {
		t.Fatalf("force: %s", got.Task)
	}
	if got := ClassifyTurn("hi", ClassifyHints{HasAttachments: true}); got.Task != TaskVision {
		t.Fatalf("attach: %s", got.Task)
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
