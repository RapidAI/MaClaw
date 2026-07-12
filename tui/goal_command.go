package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// handleGoalSlashCommand processes /goal commands in the TUI.
// TUI is single-user, no continuation engine — goals serve as persistent
// state that the LLM can query via goal(action="get") across turns.
func (m *tuiModel) handleGoalSlashCommand(text string) tea.Cmd {
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "/goal"))

	store := m.getTUIGoalStore()
	if store == nil {
		m.root.Chat.AppendSystemMessage("Goal store not initialized")
		return nil
	}

	if body == "" {
		m.root.Chat.AppendSystemMessage(tuiGoalHelp())
		return nil
	}

	lower := strings.ToLower(body)
	switch {
	case lower == "status" || lower == "get":
		result := agent.ToolGoal(store, map[string]interface{}{"action": "get"})
		m.root.Chat.AppendSystemMessage(result)
		return nil

	case lower == "pause" || lower == "暂停":
		g := store.Get("default")
		if g == nil || g.Status != goal.StatusActive {
			m.root.Chat.AppendSystemMessage("当前没有活跃目标可暂停。")
			return nil
		}
		store.Pause("default", g.GoalID)
		m.root.Chat.AppendSystemMessage(fmt.Sprintf("目标已暂停: %s", g.Objective))
		return nil

	case lower == "resume" || lower == "继续" || lower == "恢复":
		g := store.Get("default")
		if g == nil || g.Status != goal.StatusPaused {
			m.root.Chat.AppendSystemMessage("当前没有已暂停的目标可恢复。")
			return nil
		}
		store.Resume("default", g.GoalID)
		m.root.Chat.AppendSystemMessage(fmt.Sprintf("目标已恢复: %s", g.Objective))
		return nil

	case lower == "cancel" || lower == "clear" || lower == "取消":
		if store.Clear("default") {
			m.root.Chat.AppendSystemMessage("目标已清除。")
		} else {
			m.root.Chat.AppendSystemMessage("当前没有目标。")
		}
		return nil

	default:
		// Treat as new goal creation
		existing := store.Get("default")
		if existing != nil && !existing.IsTerminal() {
			m.root.Chat.AppendSystemMessage(fmt.Sprintf("已有活跃目标：%s（%s）\n使用 /goal cancel 先取消。", existing.Objective, existing.Status))
			return nil
		}
		result := agent.ToolGoal(store, map[string]interface{}{
			"action":    "create",
			"objective": body,
		})
		m.root.Chat.AppendSystemMessage(result)
		return nil
	}
}

// getTUIGoalStore returns the goal store from the TUIApp.
func (m *tuiModel) getTUIGoalStore() *goal.Store {
	if m == nil || m.app == nil {
		return nil
	}
	return m.app.goalStore
}

func tuiGoalHelp() string {
	return `/goal — 持久化长时间运行目标

用法:
  /goal <目标描述>     创建新目标
  /goal status        查看当前目标状态
  /goal pause         暂停目标
  /goal resume        恢复暂停的目标
  /goal cancel        取消并清除目标

TUI 中目标作为持久化状态，LLM 可通过 goal(action="get") 查询。`
}
