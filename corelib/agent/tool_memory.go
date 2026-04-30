package agent

// tool_memory.go implements the memory tool handler as a standalone function.
// Shared by GUI (via IMMessageHandler.toolMemory wrapper) and TUI.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ToolMemory handles memory operations (save/recall/delete/list).
func ToolMemory(store *memory.Store, args map[string]interface{}) string {
	if store == nil {
		return "长期记忆未初始化"
	}

	action := StringArg(args, "action")
	switch action {
	case "recall":
		query := StringArg(args, "query")
		if query == "" {
			return "缺少 query 参数"
		}
		category := memory.Category(StringArg(args, "category"))
		entries := store.RecallDynamic(query, category, "")
		if len(entries) == 0 {
			return "没有找到相关记忆。"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "召回 %d 条相关记忆:\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "- [%s] %s\n", string(e.Category), e.Content)
		}
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		store.TouchAccess(ids)
		return b.String()

	case "save":
		content := StringArg(args, "content")
		if content == "" {
			return "缺少 content 参数"
		}
		category := StringArg(args, "category")
		if category == "" {
			category = "user_fact"
		}
		var tags []string
		if rawTags, ok := args["tags"]; ok {
			if tagSlice, ok := rawTags.([]interface{}); ok {
				for _, t := range tagSlice {
					if s, ok := t.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
			}
		}
		if len(tags) == 0 {
			expanded := memory.ExpandQuery(content)
			tags = expanded.Entities
		}
		entry := memory.Entry{
			Content:  content,
			Category: memory.Category(category),
			Tags:     tags,
		}
		// Derive a title from the first meaningful line of content.
		// This provides a clean display name for the task list.
		for _, line := range strings.SplitN(content, "\n", 10) {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			line = strings.TrimPrefix(line, "# ")
			line = strings.TrimPrefix(line, "## ")
			if runes := []rune(line); len(runes) > 60 {
				entry.Title = string(runes[:60])
			} else {
				entry.Title = line
			}
			break
		}
		// Use SaveWithContext when conversation context is available,
		// enriching tags with entities from surrounding dialogue.
		contextHint := StringArg(args, "_context_hint")
		if err := store.SaveWithContext(entry, contextHint); err != nil {
			return fmt.Sprintf("保存记忆失败: %s", err.Error())
		}
		summary := content
		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}
		return fmt.Sprintf("已保存记忆: %s", summary)

	case "list":
		category := memory.Category(StringArg(args, "category"))
		keyword := StringArg(args, "keyword")
		entries := store.List(category, keyword)
		if len(entries) == 0 {
			return "没有找到匹配的记忆条目。"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "找到 %d 条记忆:\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "- [%s] (%s) %s\n", e.ID, e.Category, e.Content)
		}
		return b.String()

	case "delete":
		id := StringArg(args, "id")
		if id == "" {
			return "缺少 id 参数"
		}
		if err := store.Delete(id); err != nil {
			return fmt.Sprintf("删除记忆失败: %s", err.Error())
		}
		return fmt.Sprintf("已删除记忆: %s", id)

	default:
		return fmt.Sprintf("未知 memory action: %s（支持 save/recall/delete/list）", action)
	}
}
