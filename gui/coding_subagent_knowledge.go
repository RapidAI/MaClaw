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

const knowledgeImageDisplayPromptRule = `
## Knowledge image display
- Use knowledge_image_search when the user asks to find, view, show, select, or compare an imported knowledge-base image.
- Its result can contain a [KB_IMAGE:asset_id|data_url] marker. When the user asks to see an image, copy the exact marker unchanged onto its own line in the final response so the chat can render it.
- Never construct a marker yourself and never expose local image paths.
`

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
			MaxTokens:   750,
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
		b.WriteString(knowledgeImageDisplayPromptRule)
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

// knowledgeImageSearchToolDef is the coding subagent variant of the dedicated
// text-to-image route. It returns image evidence only; display remains the
// responsibility of the outer client that owns the image asset store.
func knowledgeImageSearchToolDef() map[string]interface{} {
	return buildToolDef(
		"knowledge_image_search",
		"搜索当前项目知识库中已导入的图片，基于 OCR 文本、视觉描述、文件名和文档上下文召回。用户要求查找、查看或比较已保存图片时使用。结果可包含安全展示标记；需要展示时原样复制该标记，绝不构造标记或暴露本地路径。",
		map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "图片 OCR、描述或上下文的搜索关键词",
			},
			"topic_hint": map[string]interface{}{
				"type":        "string",
				"description": "可选的当前任务主题提示，用于本地重排。",
			},
			"context_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "可选的当前任务或对话术语，用于本地重排。",
			},
			"source_kinds": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "可选的来源类型过滤；结果始终只包含图片节点。",
			},
			"source_ids": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "可选的精确来源 ID 过滤。",
			},
			"ids": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "source_ids 的别名。",
			},
			"labels": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "可选的来源标签/集合过滤。",
			},
			"domain": map[string]interface{}{
				"type":        "string",
				"description": "可选的 URL 来源域名过滤。",
			},
			"include_disabled": map[string]interface{}{
				"type":        "boolean",
				"description": "是否显式包含已禁用的当前项目来源；默认 false。",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "最大图片结果数，默认 5，最大 10。",
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
			// Knowledge is advisory. A transient DB/index failure must not turn
			// an otherwise executable coding task into a failed tool turn.
			Text:    fmt.Sprintf("编程知识库当前不可用；请继续通过项目文件和验证命令完成任务。(%v)", err),
			Outcome: codingToolOutcomeSuccess,
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
			b.WriteString("   失败尝试（不要重复）:\n")
			for _, fa := range exp.FailedAttempts {
				b.WriteString(fmt.Sprintf("   - %s\n", truncateRunesForSubAgent(fa, 100)))
			}
		}
		if len(exp.Contraindications) > 0 {
			b.WriteString("   不适用场景:\n")
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
			// Do not make optional project-document recall a task blocker. The
			// agent still has its normal read/search tools for primary evidence.
			Text:    fmt.Sprintf("项目知识库当前不可用；请使用 read_file、Glob 或 ripgrep 获取一手项目证据。(%v)", err),
			Outcome: codingToolOutcomeSuccess,
		}
	}
	results = knowledge.ProjectImageSearchResultsForTool(results)
	if len(results) == 0 {
		return codingToolExecutionResult{
			Text:    fmt.Sprintf("未找到与 %q 相关的项目资料。", query),
			Outcome: codingToolOutcomeSuccess,
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条相关项目资料：\n\n", len(results)))
	for i, r := range results {
		source := knowledge.FormatSourceLabel(r)
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

func (c *codingSubAgentCallbacks) executeKnowledgeImageSearch(argsJSON string) codingToolExecutionResult {
	if c.subagent.generalKB == nil {
		return codingToolExecutionResult{Text: "项目知识库未配置。", Outcome: codingToolOutcomeSuccess}
	}
	args := parseCodingSubAgentToolArgs(argsJSON)
	query, _ := args["query"].(string)
	if query == "" {
		return codingToolExecutionResult{Text: "Error: query parameter is required", Outcome: codingToolOutcomeFailed}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	results, err := c.subagent.generalKB.SearchImages(ctx, knowledge.ImageSearchOptions{SearchOptions: codingKnowledgeImageSearchOptions(args, c.subagent.projectPath)})
	if err != nil {
		return codingToolExecutionResult{Text: fmt.Sprintf("图片知识库当前不可用；请继续使用文本项目证据完成任务。(%v)", err), Outcome: codingToolOutcomeSuccess}
	}
	if len(results) == 0 {
		return codingToolExecutionResult{Text: fmt.Sprintf("未找到与 %q 相关的已导入图片。", query), Outcome: codingToolOutcomeSuccess}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 张相关图片：\n\n", len(results)))
	for i, r := range results {
		source := knowledge.FormatImageSourceLabel(r)
		title := knowledge.SafeImageDisplayText(r.NodeTitle)
		if title == "" {
			title = "image evidence"
		}
		b.WriteString(fmt.Sprintf("%d. [%.1f] **%s**\n   %s\n   证据：%s\n", i+1, r.Score, source, title, knowledgeSearchSnippet(r)))
		// The marker contains only an opaque asset ID and an in-memory thumbnail.
		// It is intentionally safe to pass through the model/UI boundary and lets
		// the outer chat renderer show the matched evidence inline.
		if embed := knowledge.EmbedImageThumbForSearchResult(r, c.subagent.generalKB.ImageAssetBaseDir()); embed != nil {
			if marker := knowledge.FormatKBImageMarker(embed); marker != "" {
				b.WriteString("   " + marker + "\n")
			}
		}
		b.WriteString("\n")
	}
	c.trackSearchResult("knowledge_image_search", codingKnowledgeImageSearchAuditArgs(args), fmt.Sprintf("%d results", len(results)), true)
	return codingToolExecutionResult{Text: b.String(), Outcome: codingToolOutcomeSuccess}
}

// codingKnowledgeImageSearchOptions carries the same read-only filters as the
// GUI image search. A Coding Agent is additionally pinned to its active
// project; its tool definition deliberately omits project_path/search_scope so
// a model cannot widen recall into unrelated project knowledge.
func codingKnowledgeImageSearchOptions(args map[string]interface{}, projectPath string) knowledge.SearchOptions {
	limit := knowledgeToolIntArg(args, "limit", 5)
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	return knowledge.SearchOptions{
		Query:           knowledgeToolStringArg(args, "query"),
		ProjectPath:     projectPath,
		TopicHint:       knowledgeToolStringArg(args, "topic_hint"),
		ContextTerms:    knowledgeToolStringSlice(args["context_terms"]),
		SourceKinds:     knowledgeToolStringSlice(args["source_kinds"]),
		SourceIDs:       knowledgeToolSourceIDs(args),
		Labels:          knowledgeToolStringSlice(args["labels"]),
		Domain:          knowledgeToolStringArg(args, "domain"),
		IncludeDisabled: knowledgeToolBoolArg(args, "include_disabled", false),
		Limit:           limit,
	}
}

// codingKnowledgeImageSearchAuditArgs records only declared, read-only search
// filters. It intentionally omits thumbnail bytes and does not accept hidden
// scope overrides in the audit payload.
func codingKnowledgeImageSearchAuditArgs(args map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{"query": knowledgeToolStringArg(args, "query")}
	for _, key := range []string{"topic_hint", "context_terms", "source_kinds", "source_ids", "ids", "labels", "domain", "include_disabled", "limit"} {
		if value, ok := args[key]; ok {
			result[key] = value
		}
	}
	return result
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
