package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/task"
)

// ToolTask handles the unified task management tool.
// Actions: create, update, complete, fail, list, delegate, delete
func ToolTask(store *task.Store, args map[string]interface{}) string {
	if store == nil {
		return "任务管理器未初始化"
	}
	action, _ := args["action"].(string)
	switch action {
	case "create":
		return TaskCreate(store, args)
	case "update":
		return TaskUpdate(store, args)
	case "complete":
		args["status"] = "completed"
		return TaskUpdate(store, args)
	case "fail":
		args["status"] = "failed"
		return TaskUpdate(store, args)
	case "list":
		return TaskList(store)
	case "delegate":
		return TaskDelegate(store, args)
	case "delete":
		return TaskDelete(store, args)
	default:
		return fmt.Sprintf("未知 task action: %s（支持: create/update/complete/fail/list/delegate/delete）", action)
	}
}

// TaskCreate creates a new task in the store.
func TaskCreate(store *task.Store, args map[string]interface{}) string {
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

	id := store.Create(title, desc, deps)
	t, _ := store.Get(id)
	return fmt.Sprintf("任务已创建: %s [%s] %s", id, t.Status, title)
}

// TaskUpdate updates an existing task's status and/or note.
func TaskUpdate(store *task.Store, args map[string]interface{}) string {
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

	if err := store.Update(id, status, note); err != nil {
		return fmt.Sprintf("更新失败: %v", err)
	}
	t, _ := store.Get(id)
	result := fmt.Sprintf("任务已更新: %s [%s] %s", id, t.Status, t.Title)
	if note != "" {
		result += fmt.Sprintf("\n备注: %s", note)
	}
	return result
}

// TaskDelegate delegates a task to another agent/session.
func TaskDelegate(store *task.Store, args map[string]interface{}) string {
	id, _ := args["task_id"].(string)
	if id == "" {
		return "错误: 缺少 task_id 参数"
	}
	delegateTo, _ := args["delegate_to"].(string)
	if delegateTo == "" {
		return "错误: 缺少 delegate_to 参数"
	}
	if err := store.Delegate(id, delegateTo); err != nil {
		return fmt.Sprintf("委派失败: %v", err)
	}
	t, _ := store.Get(id)
	return fmt.Sprintf("任务已委派: %s → %s [%s]", id, delegateTo, t.Status)
}

// TaskDelete removes a task from the store.
func TaskDelete(store *task.Store, args map[string]interface{}) string {
	id, _ := args["task_id"].(string)
	if id == "" {
		return "错误: 缺少 task_id 参数"
	}
	if err := store.Delete(id); err != nil {
		return fmt.Sprintf("删除失败: %v", err)
	}
	return fmt.Sprintf("任务已删除: %s", id)
}

// TaskList returns a formatted list of all tasks.
func TaskList(store *task.Store) string {
	tasks := store.List()
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
		icon := StatusIcon(t.Status)
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

// StatusIcon returns the emoji icon for a task status.
func StatusIcon(s task.Status) string {
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
