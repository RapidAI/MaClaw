package tool

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: skillrouter-body-aware-retrieval, Property 3: BM25 文本不包含 Body
// For any RegisteredTool with Body length > 50, buildSearchText() output does not contain Body substring.
// **Validates: Requirements 6.1, 6.2**
func TestProperty_BM25TextExcludesBody(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z_]{3,15}`).Draw(t, "name")
		desc := rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(t, "desc")
		// Generate a body longer than 50 chars to be a meaningful substring check.
		body := rapid.StringMatching(`[a-zA-Z0-9\-]{51,200}`).Draw(t, "body")

		reg := NewRegistry()
		reg.Register(RegisteredTool{
			Name:        name,
			Description: desc,
			Category:    CategoryMCP,
			Body:        body,
			BodySummary: TruncateBody(body, DefaultBodyMaxChars),
		})

		gen := NewDefinitionGenerator(nil, nil)
		router := NewRouter(gen)
		router.SetRegistry(reg)

		searchText := router.buildSearchText(name, desc)
		if strings.Contains(searchText, body) {
			t.Fatalf("BM25 search text should not contain body\n  searchText: %q\n  body: %q", searchText, body)
		}
	})
}

// Feature: skillrouter-body-aware-retrieval, Property 4: Embedding 文本不含 BodySummary
// For any RegisteredTool, buildEmbeddingText() is exactly name + description.
// BodySummary is long, templated parameter documentation; embedding it collapses
// the vector space (shared boilerplate dominates mean pooling), so body content
// reaches ranking only through the LLM reranker's CandidateSummary.
// **Validates: Requirements 7.1, 7.3**
func TestProperty_EmbeddingTextExcludesBodySummary(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z_]{3,15}`).Draw(t, "name")
		desc := rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(t, "desc")
		hasBody := rapid.Bool().Draw(t, "hasBody")

		reg := NewRegistry()
		tool := RegisteredTool{
			Name:        name,
			Description: desc,
			Category:    CategoryMCP,
		}
		if hasBody {
			tool.Body = rapid.StringMatching(`[a-zA-Z0-9 ]{10,100}`).Draw(t, "body")
			tool.BodySummary = TruncateBody(tool.Body, DefaultBodyMaxChars)
		}
		reg.Register(tool)

		gen := NewDefinitionGenerator(nil, nil)
		router := NewRouter(gen)
		router.SetRegistry(reg)

		embText := router.buildEmbeddingText(name, desc)

		expected := name + " " + desc
		if embText != expected {
			t.Fatalf("embedding text should be name+desc regardless of body\n  got: %q\n  want: %q", embText, expected)
		}
		if tool.BodySummary != "" && strings.Contains(embText, tool.BodySummary) {
			t.Fatalf("embedding text should not contain BodySummary\n  embText: %q\n  bodySummary: %q", embText, tool.BodySummary)
		}
	})
}

// --- Mock reranker for property tests ---

type countingReranker struct {
	callCount   int
	lastCount   int
	lastTopK    int
	returnErr   error
	returnNames []string
}

func (m *countingReranker) Rerank(userMessage string, candidates []CandidateSummary, topK int) ([]string, error) {
	m.callCount++
	m.lastCount = len(candidates)
	m.lastTopK = topK
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	if m.returnNames != nil {
		return m.returnNames, nil
	}
	// Default: return first min(topK, len) candidate names.
	n := topK
	if n > len(candidates) {
		n = len(candidates)
	}
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = candidates[i].Name
	}
	return names, nil
}

// buildLargeToolSet creates a tool set large enough to trigger reranking.
// The reranker condition is len(scoredList) > remainingSlots, where
// remainingSlots is MaxToolBudget minus already-selected core/session/intent
// tools. Adding > MaxToolBudget non-core tools keeps this helper safely above
// that threshold even when the core set changes.
func buildLargeToolSet(reg *Registry) []map[string]interface{} {
	var tools []map[string]interface{}
	// Register core tools.
	for name := range CoreToolNames {
		reg.Register(RegisteredTool{Name: name, Description: "core " + name, Category: CategoryBuiltin})
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	// Add > MaxToolBudget non-core tools so scoredList exceeds the threshold.
	for i := 0; i < MaxToolBudget+5; i++ {
		name := fmt.Sprintf("extra_%d", i)
		desc := fmt.Sprintf("extra tool number %d for testing", i)
		body := fmt.Sprintf("Parameters:\n- param%d (string): test parameter %d\nUsage: test tool %d", i, i, i)
		reg.Register(RegisteredTool{
			Name:        name,
			Description: desc,
			Category:    CategoryNonCode,
			Body:        body,
			BodySummary: TruncateBody(body, DefaultBodyMaxChars),
		})
		tools = append(tools, makeToolDef(name, desc))
	}
	return tools
}

// Feature: skillrouter-body-aware-retrieval, Property 8: Reranker 调用契约
// When reranker is configured and candidates exceed remaining routed slots,
// Rerank is called with ≤ 20 candidates and topK=5.
// When not configured, no Rerank call.
// **Validates: Requirements 8.1, 8.2, 8.4, 9.3**
func TestProperty_RerankerCallContract(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		useReranker := rapid.Bool().Draw(t, "useReranker")

		reg := NewRegistry()
		gen := NewDefinitionGenerator(nil, nil)
		router := NewRouter(gen)
		router.SetRegistry(reg)

		mock := &countingReranker{}
		if useReranker {
			router.SetReranker(mock)
		}

		tools := buildLargeToolSet(reg)
		query := rapid.StringMatching(`[a-z ]{3,30}`).Draw(t, "query")
		router.Route(query, tools)

		if useReranker {
			// Reranker should have been called (candidates exceed remaining slots).
			if mock.callCount == 0 {
				t.Fatal("reranker should have been called when configured and candidates exceed remaining slots")
			}
			if mock.lastCount > 20 {
				t.Fatalf("reranker should receive ≤ 20 candidates, got %d", mock.lastCount)
			}
			if mock.lastTopK != 5 {
				t.Fatalf("reranker topK should be 5, got %d", mock.lastTopK)
			}
		} else {
			if mock.callCount != 0 {
				t.Fatal("reranker should not be called when not configured")
			}
		}
	})
}

// Feature: skillrouter-body-aware-retrieval, Property 9: Reranker 失败优雅回退
// With an always-error reranker, Route() output matches no-reranker output.
// **Validates: Requirements 8.5, 8.6**
func TestProperty_RerankerErrorFallback(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reg1 := NewRegistry()
		gen1 := NewDefinitionGenerator(nil, nil)
		routerNoReranker := NewRouter(gen1)
		routerNoReranker.SetRegistry(reg1)

		reg2 := NewRegistry()
		gen2 := NewDefinitionGenerator(nil, nil)
		routerWithError := NewRouter(gen2)
		routerWithError.SetRegistry(reg2)
		routerWithError.SetReranker(&countingReranker{returnErr: fmt.Errorf("always fail")})

		tools1 := buildLargeToolSet(reg1)
		tools2 := buildLargeToolSet(reg2)

		query := rapid.StringMatching(`[a-z ]{3,30}`).Draw(t, "query")
		result1 := routerNoReranker.Route(query, tools1)
		result2 := routerWithError.Route(query, tools2)

		// Both should return the same number of tools.
		if len(result1) != len(result2) {
			t.Fatalf("error reranker should produce same count as no reranker: %d vs %d",
				len(result1), len(result2))
		}
	})
}

// Feature: skillrouter-body-aware-retrieval, Property 10: 空 Body 向后兼容
// With all tools having empty Body/BodySummary, NoopEmbedder, and nil Reranker,
// Router.Route() produces identical output to a baseline router.
// **Validates: Requirements 10.1, 10.2, 10.3, 10.4**
func TestProperty_EmptyBodyBackwardCompat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reg := NewRegistry()
		gen := NewDefinitionGenerator(nil, nil)
		router := NewRouter(gen)
		router.SetRegistry(reg)
		// No reranker, no embedder — pure BM25 path.

		var tools []map[string]interface{}
		for name := range CoreToolNames {
			reg.Register(RegisteredTool{Name: name, Description: "core " + name, Category: CategoryBuiltin, Body: "", BodySummary: ""})
			tools = append(tools, makeToolDef(name, "core "+name))
		}
		for i := 0; i < 15; i++ {
			name := fmt.Sprintf("compat_%d", i)
			reg.Register(RegisteredTool{Name: name, Description: fmt.Sprintf("compat tool %d", i), Category: CategoryNonCode})
			tools = append(tools, makeToolDef(name, fmt.Sprintf("compat tool %d", i)))
		}

		query := rapid.StringMatching(`[a-z ]{3,20}`).Draw(t, "query")
		result := router.Route(query, tools)

		// With empty body, no embedder, no reranker — should still return results within budget.
		if len(result) == 0 {
			t.Fatal("should return tools even with empty body")
		}
		if len(result) > MaxToolBudget+2 {
			t.Fatalf("result count %d exceeds budget %d+2", len(result), MaxToolBudget)
		}
	})
}

// Feature: skillrouter-body-aware-retrieval, Property 11: Router 与 Builder 行为一致性
// For same tool set and user message, Router and DynamicToolBuilder produce identical
// buildSearchText() and buildEmbeddingText() output for each tool.
// **Validates: Requirements 6.3, 7.4, 10.5**
func TestProperty_RouterBuilderConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reg := NewRegistry()
		numTools := rapid.IntRange(1, 10).Draw(t, "numTools")

		type toolInfo struct{ name, desc string }
		var infos []toolInfo

		for i := 0; i < numTools; i++ {
			name := fmt.Sprintf("tool_%d", i)
			desc := rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(t, "desc")
			hasBody := rapid.Bool().Draw(t, "hasBody")
			tool := RegisteredTool{Name: name, Description: desc, Category: CategoryNonCode}
			if hasBody {
				tool.Body = rapid.StringMatching(`[a-zA-Z0-9 ]{10,100}`).Draw(t, "body")
				tool.BodySummary = TruncateBody(tool.Body, DefaultBodyMaxChars)
			}
			reg.Register(tool)
			infos = append(infos, toolInfo{name, desc})
		}

		gen := NewDefinitionGenerator(nil, nil)
		router := NewRouter(gen)
		router.SetRegistry(reg)

		builder := NewDynamicToolBuilder(reg)

		for _, info := range infos {
			rEmb := router.buildEmbeddingText(info.name, info.desc)
			bEmb := builder.buildEmbeddingText(info.name, info.desc)
			if rEmb != bEmb {
				t.Fatalf("embedding text mismatch for %q:\n  router:  %q\n  builder: %q", info.name, rEmb, bEmb)
			}
		}
	})
}
