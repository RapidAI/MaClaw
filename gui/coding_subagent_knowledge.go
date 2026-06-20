package main

// coding_subagent_knowledge.go implements knowledge base integration for CodingSubAgent.
//
// Two knowledge stores are consulted:
// 1. Coding Knowledge Store (coding_knowledge.db) — accumulated coding experiences
// 2. General Knowledge Store (knowledge.db) — project documents, API specs, design docs
//
// Both are queried at task start (injected into system prompt) and available
// as read-only tools during execution.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ---------------------------------------------------------------------------
// Knowledge store references on CodingSubAgent
// ---------------------------------------------------------------------------

// SetKnowledgeStores configures the coding experience store and general knowledge store.
// Both are optional — nil stores are gracefully skipped.
func (s *CodingSubAgent) SetKnowledgeStores(codingKB *knowledge.CodingKnowledgeStore, generalKB *knowledge.SQLiteStore) {
	if s == nil {
		return
	}
	s.codingKB = codingKB
	s.generalKB = generalKB
}

// ---------------------------------------------------------------------------
// System prompt injection
// ---------------------------------------------------------------------------

// buildKnowledgePromptSections generates the knowledge-related system prompt
// sections for the SubAgent. Returns empty string if no relevant knowledge found.
func (c *codingSubAgentCallbacks) buildKnowledgePromptSections() string {
	if c == nil || c.subagent == nil {
		return ""
	}
	// Skip knowledge retrieval if already cancelled.
	if c.ShouldStop() {
		return ""
	}

	var b strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	taskQuery := ""
	taskLanguage := ""
	if c.task != nil {
		taskQuery = c.task.Title
		if c.task.Description != "" {
			taskQuery += " " + c.task.Description
		}
		taskLanguage = inferLanguageFromTaskFiles(c.task.Files)
	}
	if taskQuery == "" {
		return ""
	}

	projectPath := c.subagent.projectPath

	// 1. Coding knowledge (experiences)
	if c.subagent.codingKB != nil {
		pack, err := c.subagent.codingKB.ContextPackForTask(ctx, knowledge.CodingContextPackOptions{
			Query:       taskQuery,
			Language:    taskLanguage,
			ProjectPath: projectPath,
			MaxItems:    4,
			MaxChars:    1500,
		})
		if err == nil && len(pack.Items) > 0 {
			b.WriteString("\n## 相关编码经验（来自编程知识库）\n")
			b.WriteString("以下经验来自历史编码任务积累，供参考：\n")
			for _, item := range pack.Items {
				b.WriteString(fmt.Sprintf("- **%s**: %s\n", item.Title, truncateRunesForSubAgent(item.Text, 300)))
			}
		}
	}

	// 2. General knowledge (project docs)
	if c.subagent.generalKB != nil {
		searchOpts := knowledge.SearchOptions{
			Query:       taskQuery,
			ProjectPath: projectPath,
			Limit:       10,
		}
		pack, err := c.subagent.generalKB.ContextPack(ctx, knowledge.ContextPackOptions{
			SearchOptions: searchOpts,
			MaxItems:      3,
			MaxChars:      2000,
		})
		if err == nil && len(pack.Items) > 0 {
			b.WriteString("\n## 项目参考资料（来自通用知识库）\n")
			b.WriteString("以下是与当前任务相关的项目文档：\n")
			b.WriteString(knowledge.FormatContextPackForLLM(pack))
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Knowledge search tools (read-only)
// ---------------------------------------------------------------------------

// codingKnowledgeSearchToolDef returns the tool definition for coding_knowledge_search.
func codingKnowledgeSearchToolDef() map[string]interface{} {
	return buildToolDef(
		"coding_knowledge_search",
		"搜索编程经验知识库，获取算法选型、技术方案、常见陷阱等编码经验。当你不确定某个技术决策或遇到不熟悉的模式时使用。",
		map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索关键词，描述你想了解的技术问题或决策",
			},
		},
		[]string{"query"},
	)
}

// knowledgeSearchToolDef returns the tool definition for knowledge_search.
func knowledgeSearchToolDef() map[string]interface{} {
	return buildToolDef(
		"knowledge_search",
		"搜索项目知识库，获取 API 文档、数据库结构、接口规范、设计文档等项目资料。当你需要了解项目的具体约定或接口细节时使用。",
		map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "搜索关键词，描述你需要的项目信息",
			},
		},
		[]string{"query"},
	)
}

// executeCodingKnowledgeSearch handles the coding_knowledge_search tool call.
func (c *codingSubAgentCallbacks) executeCodingKnowledgeSearch(argsJSON string) codingToolExecutionResult {
	if c.subagent.codingKB == nil {
		return codingToolExecutionResult{
			Text:    "编程知识库未配置。暂无可用的编码经验。",
			Outcome: codingToolOutcomeSuccess,
		}
	}

	args := parseCodingSubAgentToolArgs(argsJSON)
	query, _ := args["query"].(string)
	if query == "" {
		return codingToolExecutionResult{
			Text:    "Error: query parameter is required",
			Outcome: codingToolOutcomeFailed,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	taskLanguage := ""
	if c.task != nil {
		taskLanguage = inferLanguageFromTaskFiles(c.task.Files)
	}

	experiences, err := c.subagent.codingKB.SearchExperiences(ctx, knowledge.CodingSearchOptions{
		Query:       query,
		Language:    taskLanguage,
		ProjectPath: c.subagent.projectPath,
		Status:      []string{knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
		Limit:       5,
	})
	if err != nil {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("编程知识库搜索失败: %v", err),
			Outcome: codingToolOutcomeFailed,
		}
	}
	if len(experiences) == 0 {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("未找到与 %q 相关的编码经验。", query),
			Outcome: codingToolOutcomeSuccess,
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关编码经验：\n\n", len(experiences)))
	for i, exp := range experiences {
		b.WriteString(fmt.Sprintf("%d. **%s** [%s/%s] (置信度: %.1f)\n", i+1, exp.Title, exp.Scope, exp.Category, exp.Confidence))
		if exp.TriggerCondition != "" {
			b.WriteString(fmt.Sprintf("   触发条件: %s\n", exp.TriggerCondition))
		}
		if exp.Content != "" {
			content := truncateRunesForSubAgent(exp.Content, 400)
			b.WriteString(fmt.Sprintf("   %s\n", content))
		}
		if exp.CodeSnippet != "" {
			snippet := truncateRunesForSubAgent(exp.CodeSnippet, 300)
			b.WriteString(fmt.Sprintf("   代码片段:\n   ```\n   %s\n   ```\n", snippet))
		}
		if len(exp.FailedAttempts) > 0 {
			b.WriteString("   ⚠️ 失败尝试（不要重复）:\n")
			for _, fa := range exp.FailedAttempts {
				b.WriteString(fmt.Sprintf("   - %s\n", truncateRunesForSubAgent(fa, 100)))
			}
		}
		if len(exp.Contraindications) > 0 {
			b.WriteString("   ❌ 不适用场景:\n")
			for _, ci := range exp.Contraindications {
				b.WriteString(fmt.Sprintf("   - %s\n", truncateRunesForSubAgent(ci, 100)))
			}
		}
		b.WriteString("\n")
	}

	// Track search for audit
	c.trackSearchResult("coding_knowledge_search", map[string]interface{}{"query": query},
		fmt.Sprintf("%d results", len(experiences)), true)

	return codingToolExecutionResult{
		Text:    b.String(),
		Outcome: codingToolOutcomeSuccess,
	}
}

// executeKnowledgeSearch handles the knowledge_search tool call (general knowledge).
func (c *codingSubAgentCallbacks) executeKnowledgeSearch(argsJSON string) codingToolExecutionResult {
	if c.subagent.generalKB == nil {
		return codingToolExecutionResult{
			Text:    "项目知识库未配置。请使用 read_file 直接查看项目文件获取信息。",
			Outcome: codingToolOutcomeSuccess,
		}
	}

	args := parseCodingSubAgentToolArgs(argsJSON)
	query, _ := args["query"].(string)
	if query == "" {
		return codingToolExecutionResult{
			Text:    "Error: query parameter is required",
			Outcome: codingToolOutcomeFailed,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	results, err := c.subagent.generalKB.Search(ctx, knowledge.SearchOptions{
		Query:       query,
		ProjectPath: c.subagent.projectPath,
		Limit:       5,
	})
	if err != nil {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("项目知识库搜索失败: %v", err),
			Outcome: codingToolOutcomeFailed,
		}
	}
	if len(results) == 0 {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("未找到与 %q 相关的项目资料。", query),
			Outcome: codingToolOutcomeSuccess,
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关项目资料：\n\n", len(results)))
	for i, r := range results {
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := knowledgeSearchSnippet(r)
		b.WriteString(fmt.Sprintf("%d. [%.1f] **%s**\n   %s\n\n", i+1, r.Score, source, text))
	}

	// Track search for audit
	c.trackSearchResult("knowledge_search", map[string]interface{}{"query": query},
		fmt.Sprintf("%d results", len(results)), true)

	return codingToolExecutionResult{
		Text:    b.String(),
		Outcome: codingToolOutcomeSuccess,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// inferLanguageFromTaskFiles guesses the primary language from file extensions.
func inferLanguageFromTaskFiles(files []string) string {
	langCounts := make(map[string]int)
	for _, f := range files {
		ext := strings.ToLower(strings.TrimSpace(f))
		if idx := strings.LastIndex(ext, "."); idx >= 0 {
			ext = ext[idx:]
		} else {
			continue
		}
		switch ext {
		case ".go":
			langCounts["go"]++
		case ".py":
			langCounts["python"]++
		case ".ts", ".tsx":
			langCounts["typescript"]++
		case ".js", ".jsx":
			langCounts["javascript"]++
		case ".cpp", ".cc", ".cxx", ".h", ".hpp":
			langCounts["cpp"]++
		case ".rs":
			langCounts["rust"]++
		case ".java":
			langCounts["java"]++
		case ".rb":
			langCounts["ruby"]++
		case ".cs":
			langCounts["csharp"]++
		case ".swift":
			langCounts["swift"]++
		case ".kt", ".kts":
			langCounts["kotlin"]++
		}
	}
	// Return the language with the highest count
	best := ""
	bestCount := 0
	for lang, count := range langCounts {
		if count > bestCount {
			best = lang
			bestCount = count
		}
	}
	return best
}

// knowledgeSearchSnippet extracts the best display text from a general search result.
// Delegates to the shared knowledge.BestContentText with truncation for SubAgent context budget.
func knowledgeSearchSnippet(r knowledge.SearchResult) string {
	text := knowledge.BestContentText(r)
	if text == "" {
		return "(no content)"
	}
	return truncateRunesForSubAgent(text, 300)
}

// parseCodingSubAgentToolArgs is a simple JSON parse helper for tool arguments.
func parseCodingSubAgentToolArgs(argsJSON string) map[string]interface{} {
	args := make(map[string]interface{})
	if argsJSON == "" {
		return args
	}
	normalized := normalizeCodingSubAgentToolArguments(argsJSON)
	if normalized == "" {
		return args
	}
	_ = json.Unmarshal([]byte(normalized), &args)
	return args
}
