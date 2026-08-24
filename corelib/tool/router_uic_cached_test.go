package tool

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	uicintent "github.com/RapidAI/CodeClaw/corelib/intent"
)

// When the embedding-only fast channel cannot activate anything (here: no
// embedder at all), routing must reuse an already-cached full-fusion
// classification for the same message instead of failing closed — and it must
// do so as a pure cache read, never triggering a new tree/LLM call. This
// reconciles two contracts: "routing never calls tree/LLM" and "conditional
// tools follow the current semantic intent".
func TestRouter_PreferEmbeddingOnlyConsumesCachedFullClassification(t *testing.T) {
	var llmCalls atomic.Int32
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			llmCalls.Add(1)
			return `{"top":[{"skill":"ssh","score":0.95}]}`, nil
		},
	})

	const msg = "将驱网服务器上的 19080 端口反代到 ve.mypapers.top"
	// Pre-warm: the main loop's full classification for this message.
	warm := uic.Classify(uicintent.MessageContext{Text: msg})
	if warm.Primary != uicintent.LabelSSH {
		t.Fatalf("pre-warm classification primary = %s, want ssh", warm.Primary)
	}
	if got := llmCalls.Load(); got != 1 {
		t.Fatalf("pre-warm should call the tree LLM exactly once, got %d", got)
	}

	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("ssh", "通过 SSH 连接服务器"))
	for i := 0; i < 40; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.RouteWithOptions(msg, tools, RouteOptions{PreferEmbeddingOnly: true})
	if !routedToolNames(result)["ssh"] {
		t.Fatalf("cached full classification should activate ssh via PreferEmbeddingOnly route, got: %v", routedToolNames(result))
	}
	if got := llmCalls.Load(); got != 1 {
		t.Fatalf("routing must not trigger a new tree/LLM call, got %d calls", got)
	}
}

// Without a cached full classification, the degraded fast channel stays
// fail-closed and no LLM call is made.
func TestRouter_PreferEmbeddingOnlyWithoutCacheStaysClosed(t *testing.T) {
	var llmCalls atomic.Int32
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			llmCalls.Add(1)
			return `{"top":[{"skill":"ssh","score":0.95}]}`, nil
		},
	})

	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("ssh", "通过 SSH 连接服务器"))
	for i := 0; i < 40; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	result := router.RouteWithOptions("将驱网服务器上的 19080 端口反代到 ve.mypapers.top", tools, RouteOptions{PreferEmbeddingOnly: true})
	if routedToolNames(result)["ssh"] {
		t.Fatalf("without a cached classification, ssh must stay fail-closed, got: %v", routedToolNames(result))
	}
	if got := llmCalls.Load(); got != 0 {
		t.Fatalf("routing must never call the tree/LLM channel, got %d calls", got)
	}
}
