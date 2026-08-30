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

func searchRouteTools() []map[string]interface{} {
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools, makeToolDef("web_search", "Search the public web"))
	tools = append(tools, makeToolDef("web_fetch", "Fetch a URL"))
	tools = append(tools, makeToolDef("ssh", "SSH to a server"))
	for i := 0; i < 40; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}
	return tools
}

// Leftover SkipUnifiedClassifier must still consume a completed tree verdict.
// Fusion timeout writes a degraded unknown into LoopContext; the late tree
// then caches search. Skipping live fusion is required (no second LLM), but
// skipping ClassifyCached hid web_search on the in-flight turn.
func TestRouter_SkipUnifiedClassifierConsumesCachedSearch(t *testing.T) {
	var llmCalls atomic.Int32
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			llmCalls.Add(1)
			return `{"top":[{"skill":"search","score":0.88}]}`, nil
		},
	})
	const msg = "长江学者申请后，一般研究项目执行几年？"
	cacheMsg := uicintent.MessageContext{Text: msg, UserID: "desktop-user:cloud"}
	warm := uic.Classify(cacheMsg)
	if warm.Primary != uicintent.LabelSearch || warm.Degraded {
		t.Fatalf("pre-warm = %+v, want search", warm)
	}

	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	unknown := uicintent.ClassificationResult{
		Primary: uicintent.LabelUnknown, Confidence: 0.30, Degraded: true,
		Reason: "embedding ambiguous; tree classification unavailable (l2=workflow_task conf=0.73)",
	}
	result := router.RouteWithOptions(msg, searchRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreferEmbeddingOnly:   true,
		PreResolved:           &unknown,
		CacheMessage:          cacheMsg,
	})
	names := routedToolNames(result)
	if !names["web_search"] {
		t.Fatalf("leftover skip must activate cached search tools, got %v", names)
	}
	if names["ssh"] {
		t.Fatalf("sensitive tools must stay fail-closed on leftover, got %v", names)
	}
	if got := llmCalls.Load(); got != 1 {
		t.Fatalf("leftover routing must not call the tree again, got %d", got)
	}
}

func TestRouter_SkipUnifiedClassifierDoesNotActivateDegradedLookupHint(t *testing.T) {
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			t.Fatal("skip-tree short lookup must not call the tree")
			return "", nil
		},
	})
	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	degraded := uicintent.ClassificationResult{
		Primary: uicintent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true,
		ToolNames: []string{"web_search", "web_fetch"},
		Reason:    "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61)",
	}
	result := router.RouteWithOptions("北京天所", searchRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &degraded,
		CacheMessage:          uicintent.MessageContext{Text: "北京天所"},
	})
	if routedToolNames(result)["ssh"] {
		t.Fatalf("sensitive tools must stay fail-closed, got %v", routedToolNames(result))
	}
}

func TestRouter_SkipUnifiedClassifierDoesNotOpenCachedSSH(t *testing.T) {
	var llmCalls atomic.Int32
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			llmCalls.Add(1)
			return `{"top":[{"skill":"ssh","score":0.95}]}`, nil
		},
	})
	const msg = "把服务器端口转发出去"
	cacheMsg := uicintent.MessageContext{Text: msg, UserID: "u-ssh"}
	warm := uic.Classify(cacheMsg)
	if warm.Primary != uicintent.LabelSSH || warm.Degraded {
		t.Fatalf("pre-warm = %+v, want ssh", warm)
	}
	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	unknown := uicintent.ClassificationResult{
		Primary: uicintent.LabelUnknown, Confidence: 0.30, Degraded: true,
	}
	result := router.RouteWithOptions(msg, searchRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &unknown,
		CacheMessage:          cacheMsg,
	})
	if routedToolNames(result)["ssh"] {
		t.Fatalf("leftover skip must not force-open cached ssh, got %v", routedToolNames(result))
	}
	if got := llmCalls.Load(); got != 1 {
		t.Fatalf("leftover routing must not call the tree again, got %d", got)
	}
}

func TestRouter_SkipUnifiedClassifierCachedCodingDoesNotConstrainSkills(t *testing.T) {
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"coding","score":0.95,"workflow_type":"coding"}]}`, nil
		},
	})
	const msg = "长江学者申请后，一般研究项目执行几年？"
	cacheMsg := uicintent.MessageContext{Text: msg, UserID: "u-coding"}
	warm := uic.Classify(cacheMsg)
	if warm.Primary != uicintent.LabelCoding {
		t.Fatalf("pre-warm = %+v, want coding", warm)
	}
	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	unknown := uicintent.ClassificationResult{Primary: uicintent.LabelUnknown, Confidence: 0.30, Degraded: true}
	result := router.RouteWithOptions(msg, searchRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &unknown,
		CacheMessage:          cacheMsg,
	})
	names := routedToolNames(result)
	if names["ssh"] {
		t.Fatalf("cached coding on leftover must not open ssh, got %v", names)
	}
}

func TestRouter_SkipUnifiedClassifierIgnoresWeakCachedSearch(t *testing.T) {
	uic := uicintent.New(uicintent.Config{
		Embedder: embedding.NoopEmbedder{},
		LLMFunc: func(systemPrompt, userText string) (string, error) {
			return `{"top":[{"skill":"search","score":0.55}]}`, nil
		},
	})
	const msg = "随便问一句"
	cacheMsg := uicintent.MessageContext{Text: msg, UserID: "u-weak"}
	warm := uic.Classify(cacheMsg)
	if warm.Primary != uicintent.LabelSearch || warm.Degraded || warm.Confidence >= uicintent.EmbeddingLookupMinScore {
		t.Fatalf("pre-warm = %+v, want weak search", warm)
	}
	router := NewRouter(nil)
	router.SetUnifiedClassifier(uic)
	unknown := uicintent.ClassificationResult{Primary: uicintent.LabelUnknown, Confidence: 0.30, Degraded: true}
	got := router.lookupRouteClassification(msg, RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &unknown,
		CacheMessage:          cacheMsg,
	})
	if got.Primary == uicintent.LabelSearch {
		t.Fatalf("leftover skip must not activate sub-floor cached search: %+v", got)
	}
}

func TestRouter_SkipUnifiedClassifierDoesNotLiveClassify(t *testing.T) {
	ic := NewIntentClassifier(nil)
	ic.SetLLMFunc(func(prompt string) (string, error) {
		t.Fatalf("leftover skip must not live-classify: %q", prompt)
		return "", nil
	})
	router := NewRouter(nil)
	router.SetIntentClassifier(ic)
	unknown := uicintent.ClassificationResult{Primary: uicintent.LabelUnknown, Confidence: 0.30, Degraded: true}
	_ = router.RouteWithOptions("长江学者申请后，一般研究项目执行几年？", searchRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &unknown,
	})
}
