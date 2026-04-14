package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/task"
)

// toolTask handles the unified task management tool.
// Actions: create, update, complete, fail, list, delegate, delete
func (h *IMMessageHandler) toolTask(args map[string]interface{}) string {
	if h.taskStore == nil {
		h.taskStore = task.NewStore()
	}

	action, _ := args["action"].(string)
	switch action {
	case "create":
		return h.taskCreate(args)
	case "update":
		return h.taskUpdate(args)
	case "complete":
		args["status"] = "completed"
		return h.taskUpdate(args)
	case "fail":
		args["status"] = "failed"
		return h.taskUpdate(args)
	case "list":
		return h.taskList()
	case "delegate":
		return h.taskDelegate(args)
	case "delete":
		return h.taskDelete(args)
	default:
		return fmt.Sprintf("未知 task action: %s（支持: create/update/complete/fail/list/delegate/delete）", action)
	}
}

func (h *IMMessageHandler) taskCreate(args map[string]interface{}) string {
	title, _ := args["title"].(string)
	if title == "" {
		return "错误: 缺少 title 参数"
	}
	desc, _ := args["description"].(string)

	var deps []string
	if raw, ok := args["depends_on"]; ok {
		switch v := raw.(type) {
		case []interface{}:
			for _, d := range v {
				if s, ok := d.(string); ok {
					deps = append(deps, s)
				}
			}
		case string:
			var parsed []string
			if json.Unmarshal([]byte(v), &parsed) == nil {
				deps = parsed
			}
		}
	}

	id := h.taskStore.Create(title, desc, deps)
	t, _ := h.taskStore.Get(id)
	return fmt.Sprintf("任务已创建: %s [%s] %s", id, t.Status, title)
}

func (h *IMMessageHandler) taskUpdate(args map[string]interface{}) string {
	id, _ := args["task_id"].(string)
	if id == "" {
		return "错误: 缺少 task_id 参数"
	}
	statusStr, _ := args["status"].(string)
	note, _ := args["status_note"].(string)

	var status task.Status
	switch statusStr {
	case "pending":
		status = task.StatusPending
	case "in_progress":
		status = task.StatusInProgress
	case "completed":
		status = task.StatusCompleted
	case "failed":
		status = task.StatusFailed
	case "blocked":
		status = task.StatusBlocked
	case "":
		// no status change, just note update
	default:
		return fmt.Sprintf("未知状态: %s（支持: pending/in_progress/completed/failed/blocked）", statusStr)
	}

	if err := h.taskStore.Update(id, status, note); err != nil {
		return fmt.Sprintf("更新失败: %v", err)
	}
	t, _ := h.taskStore.Get(id)
	result := fmt.Sprintf("任务已更新: %s [%s] %s", id, t.Status, t.Title)
	if note != "" {
		result += fmt.Sprintf("\n备注: %s", note)
	}
	return result
}

func (h *IMMessageHandler) taskDelegate(args map[string]interface{}) string {
	id, _ := args["task_id"].(string)
	if id == "" {
		return "错误: 缺少 task_id 参数"
	}
	delegateTo, _ := args["delegate_to"].(string)
	if delegateTo == "" {
		return "错误: 缺少 delegate_to 参数"
	}
	if err := h.taskStore.Delegate(id, delegateTo); err != nil {
		return fmt.Sprintf("委派失败: %v", err)
	}
	t, _ := h.taskStore.Get(id)
	return fmt.Sprintf("任务已委派: %s → %s [%s]", id, delegateTo, t.Status)
}

func (h *IMMessageHandler) taskDelete(args map[string]interface{}) string {
	id, _ := args["task_id"].(string)
	if id == "" {
		return "错误: 缺少 task_id 参数"
	}
	if err := h.taskStore.Delete(id); err != nil {
		return fmt.Sprintf("删除失败: %v", err)
	}
	return fmt.Sprintf("任务已删除: %s", id)
}

func (h *IMMessageHandler) taskList() string {
	tasks := h.taskStore.List()
	if len(tasks) == 0 {
		return "当前没有任务。"
	}
	// Sort by creation time
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个任务:\n", len(tasks)))
	for _, t := range tasks {
		icon := statusIcon(t.Status)
		b.WriteString(fmt.Sprintf("\n%s %s [%s] %s", icon, t.ID, t.Status, t.Title))
		if t.DelegatedTo != "" {
			b.WriteString(fmt.Sprintf(" → %s", t.DelegatedTo))
		}
		if len(t.DependsOn) > 0 {
			b.WriteString(fmt.Sprintf(" (依赖: %s)", strings.Join(t.DependsOn, ", ")))
		}
		if t.StatusNote != "" {
			b.WriteString(fmt.Sprintf("\n  📝 %s", t.StatusNote))
		}
	}
	return b.String()
}

func statusIcon(s task.Status) string {
	switch s {
	case task.StatusPending:
		return "⏳"
	case task.StatusInProgress:
		return "🔄"
	case task.StatusCompleted:
		return "✅"
	case task.StatusFailed:
		return "❌"
	case task.StatusBlocked:
		return "🚫"
	default:
		return "❓"
	}
}
