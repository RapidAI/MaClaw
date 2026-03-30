package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	defaultChecklistMaxTokens = 300
	checklistKeepCompleted    = 3 // 超出 token 限制时保留的最近已完成项数
)

// ChecklistItem 表示 checklist 中的一项。
type ChecklistItem struct {
	Description string
	Completed   bool
	CompletedAt time.Time
}

// HarnessProgressTracker 维护 Agent Loop 的结构化任务 checklist。
// 与 corelib/remote/session_progress_tracker.go 中的 ProgressTracker 不同，
// 此组件追踪的是 Agent Loop 自身的任务 checklist，而非编程工具的 tool_use 步骤。
type HarnessProgressTracker struct {
	items     []ChecklistItem
	maxTokens int // checklist token 上限 (默认 300)
}

// NewHarnessProgressTracker 从 checklist 项列表创建进度追踪器。
// 如果 items 为空，返回 nil 表示不需要 checklist。
func NewHarnessProgressTracker(items []ChecklistItem, maxTokens int) *HarnessProgressTracker {
	if len(items) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		maxTokens = defaultChecklistMaxTokens
	}
	// 复制 items 避免外部修改
	copied := make([]ChecklistItem, len(items))
	copy(copied, items)
	return &HarnessProgressTracker{
		items:     copied,
		maxTokens: maxTokens,
	}
}

// MarkComplete 标记指定项为已完成。
// index 越界时静默忽略。
func (t *HarnessProgressTracker) MarkComplete(index int) {
	if index < 0 || index >= len(t.items) {
		return
	}
	if !t.items[index].Completed {
		t.items[index].Completed = true
		t.items[index].CompletedAt = time.Now()
	}
}

// BuildChecklistContent 生成 Markdown checkbox 格式的 checklist。
// 超出 token 限制时仅保留最近 3 个已完成项和全部未完成项。
func (t *HarnessProgressTracker) BuildChecklistContent() string {
	if len(t.items) == 0 {
		return ""
	}

	// 先尝试完整输出
	content := t.formatItems(t.items)
	if estimateContentTokens(content) <= t.maxTokens {
		return content
	}

	// 超出限制：保留最近 checklistKeepCompleted 个已完成项 + 全部未完成项
	filtered := t.filterForTokenLimit()
	return t.formatItems(filtered)
}

// AllComplete 判断是否全部完成。
func (t *HarnessProgressTracker) AllComplete() bool {
	for i := range t.items {
		if !t.items[i].Completed {
			return false
		}
	}
	return true
}

// Summary 返回进度摘要（已完成数/总数 + 当前步骤描述 + 剩余项数）。
// 格式: "已完成 3/7 步 | 当前: <first incomplete item> | 剩余: 4 项"
func (t *HarnessProgressTracker) Summary() string {
	total := len(t.items)
	completed := 0
	firstIncomplete := ""
	for i := range t.items {
		if t.items[i].Completed {
			completed++
		} else if firstIncomplete == "" {
			firstIncomplete = t.items[i].Description
		}
	}

	remaining := total - completed
	if firstIncomplete == "" {
		return fmt.Sprintf("已完成 %d/%d 步 | 全部完成", completed, total)
	}
	return fmt.Sprintf("已完成 %d/%d 步 | 当前: %s | 剩余: %d 项", completed, total, firstIncomplete, remaining)
}

// formatItems 将 checklist 项列表格式化为 Markdown checkbox 格式。
func (t *HarnessProgressTracker) formatItems(items []ChecklistItem) string {
	var sb strings.Builder
	for i, item := range items {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if item.Completed {
			sb.WriteString("- [x] ")
		} else {
			sb.WriteString("- [ ] ")
		}
		sb.WriteString(item.Description)
	}
	return sb.String()
}

// filterForTokenLimit 返回最近 checklistKeepCompleted 个已完成项 + 全部未完成项。
func (t *HarnessProgressTracker) filterForTokenLimit() []ChecklistItem {
	var completedItems []ChecklistItem
	var incompleteItems []ChecklistItem

	for i := range t.items {
		if t.items[i].Completed {
			completedItems = append(completedItems, t.items[i])
		} else {
			incompleteItems = append(incompleteItems, t.items[i])
		}
	}

	// 按 CompletedAt 降序排序，取最新的 checklistKeepCompleted 个
	sort.SliceStable(completedItems, func(i, j int) bool {
		return completedItems[i].CompletedAt.After(completedItems[j].CompletedAt)
	})
	kept := completedItems
	if len(kept) > checklistKeepCompleted {
		kept = kept[:checklistKeepCompleted]
	}

	// 合并：已完成项在前，未完成项在后
	result := make([]ChecklistItem, 0, len(kept)+len(incompleteItems))
	result = append(result, kept...)
	result = append(result, incompleteItems...)
	return result
}

// estimateContentTokens 使用 len(content)/2 作为混合中英文内容的粗略 token 估算。
func estimateContentTokens(content string) int {
	return len(content) / 2
}
