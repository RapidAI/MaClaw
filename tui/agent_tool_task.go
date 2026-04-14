package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/task"
)

// toolTask handles the unified task management tool for TUI.
func (h *TUIAgentHandler) toolTask(args map[string]interface{}) string {
	if h.taskStore == nil {
		h.taskStore = task.NewStore()
	}

	action := stringArg(args, "action")
	switch action {
	case "create":
		title := stringArg(args, "title")
		if title == "" {
			return "错误: 缺少 title 参数"
		}
		desc := stringArg(args, "description")
		var deps []string
		if raw, ok := args["depends_on"]; ok {
			if arr, ok := raw.([]interface{}); ok {
				for _, d := range arr {
					if s, ok := d.(string); ok {
						deps = append(deps, s)
					}
				}
			} else if s, ok := raw.(string); ok {
				var parsed []string
				if json.Unmarshal([]byte(s), &parsed) == nil {
					deps = parsed
				}
			}
		}
		id := h.taskStore.Create(title, desc, deps)
		t, _ := h.taskStore.Get(id)
		return fmt.Sprintf("任务已创建: %s [%s] %s", id, t.Status, title)

	case "update", "complete", "fail":
		id := stringArg(args, "task_id")
		if id == "" {
			return "错误: 缺少 task_id 参数"
		}
		statusStr := stringArg(args, "status")
		if action == "complete" {
			statusStr = "completed"
		} else if action == "fail" {
			statusStr = "failed"
		}
		note := stringArg(args, "status_note")
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
			// note-only update
		default:
			return fmt.Sprintf("未知状态: %s", statusStr)
		}
		if err := h.taskStore.Update(id, status, note); err != nil {
			return fmt.Sprintf("更新失败: %v", err)
		}
		t, _ := h.taskStore.Get(id)
		return fmt.Sprintf("任务已更新: %s [%s] %s", id, t.Status, t.Title)

	case "list":
		tasks := h.taskStore.List()
		if len(tasks) == 0 {
			return "当前没有任务。"
		}
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		})
		var b strings.Builder
		b.WriteString(fmt.Sprintf("共 %d 个任务:\n", len(tasks)))
		for _, t := range tasks {
			b.WriteString(fmt.Sprintf("\n%s [%s] %s", t.ID, t.Status, t.Title))
			if t.DelegatedTo != "" {
				b.WriteString(fmt.Sprintf(" → %s", t.DelegatedTo))
			}
			if len(t.DependsOn) > 0 {
				b.WriteString(fmt.Sprintf(" (依赖: %s)", strings.Join(t.DependsOn, ", ")))
			}
		}
		return b.String()

	case "delegate":
		id := stringArg(args, "task_id")
		delegateTo := stringArg(args, "delegate_to")
		if id == "" || delegateTo == "" {
			return "错误: 缺少 task_id 或 delegate_to 参数"
		}
		if err := h.taskStore.Delegate(id, delegateTo); err != nil {
			return fmt.Sprintf("委派失败: %v", err)
		}
		return fmt.Sprintf("任务已委派: %s → %s", id, delegateTo)

	case "delete":
		id := stringArg(args, "task_id")
		if id == "" {
			return "错误: 缺少 task_id 参数"
		}
		if err := h.taskStore.Delete(id); err != nil {
			return fmt.Sprintf("删除失败: %v", err)
		}
		return fmt.Sprintf("任务已删除: %s", id)

	default:
		return fmt.Sprintf("未知 task action: %s（支持: create/update/complete/fail/list/delegate/delete）", action)
	}
}
