package main

// session_search and manage_user_model tool handlers for TUI.
// Mirrors gui/im_tools_session_search.go and gui/im_tools_user_model.go.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/session"
	"github.com/RapidAI/CodeClaw/corelib/user"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ===================== Session Search =====================

// getSessionStore returns the session search store, lazily initializing it
// if not already created. Returns nil if initialization fails.
func (h *TUIAgentHandler) getSessionStore() *session.Store {
	h.sessionStoreMu.Do(func() {
		dbPath := filepath.Join(commands.ResolveDataDir(), "session_search.db")
		store, err := session.NewStore(dbPath)
		if err != nil {
			fmt.Printf("[session_search] failed to open store: %v\n", err)
			return
		}
		h.sessionSearchStore = store
	})
	return h.sessionSearchStore
}

// toolSessionSearch handles the session_search tool call.
// Parameters:
//   - query (string, required): the search query
//   - max_results (int, optional, default 10): maximum number of results
func (h *TUIAgentHandler) toolSessionSearch(args map[string]interface{}) string {
	query := stringArg(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}

	maxResults := 10
	if raw, ok := args["max_results"]; ok {
		switch v := raw.(type) {
		case float64:
			if int(v) > 0 {
				maxResults = int(v)
			}
		case int:
			if v > 0 {
				maxResults = v
			}
		}
	}

	store := h.getSessionStore()
	if store == nil {
		return "session search store 未初始化"
	}

	results, err := store.Search(query, maxResults)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}

	// The store returns a single result with Snippet="no results found" for empty results.
	if len(results) == 1 && results[0].SessionID == "" && results[0].Snippet == "no results found" {
		return "no results found"
	}

	// Format results as a readable string.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条匹配结果:\n\n", len(results)))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("--- 结果 %d ---\n", i+1))
		b.WriteString(fmt.Sprintf("会话 ID: %s\n", r.SessionID))
		b.WriteString(fmt.Sprintf("时间: %s\n", r.Timestamp))
		b.WriteString(fmt.Sprintf("平台: %s\n", r.Platform))
		if r.Topic != "" {
			b.WriteString(fmt.Sprintf("主题: %s\n", r.Topic))
		}
		b.WriteString(fmt.Sprintf("片段: %s\n", r.Snippet))
		b.WriteByte('\n')
	}
	return b.String()
}

// ===================== User Model =====================

// getUserModel returns the user model, lazily initializing it if not already
// created. Returns nil if initialization fails.
func (h *TUIAgentHandler) getUserModel() *user.Model {
	h.userModelMu.Do(func() {
		modelPath := filepath.Join(commands.ResolveDataDir(), "user_model.json")
		model, err := user.NewModel(modelPath)
		if err != nil {
			fmt.Printf("[manage_user_model] failed to load user model: %v\n", err)
			return
		}
		h.userModel = model
	})
	return h.userModel
}

// toolManageUserModel handles the manage_user_model tool call.
// Parameters:
//   - action (string, required): "view", "correct", or "reset"
//   - dimension (string, optional): the profile dimension to correct/reset
//   - value (string, optional): the new value for the dimension (required for "correct")
func (h *TUIAgentHandler) toolManageUserModel(args map[string]interface{}) string {
	action := stringArg(args, "action")
	if action == "" {
		return "缺少 action 参数（可选值: view, correct, reset）"
	}

	model := h.getUserModel()
	if model == nil {
		return "用户画像模块未初始化"
	}

	switch action {
	case "view":
		return h.userModelViewTUI(model)
	case "correct":
		return h.userModelCorrectTUI(model, args)
	case "reset":
		return h.userModelResetTUI(model, args)
	default:
		return fmt.Sprintf("未知 action: %s（可选值: view, correct, reset）", action)
	}
}

// userModelViewTUI formats the current user profile as readable text.
func (h *TUIAgentHandler) userModelViewTUI(model *user.Model) string {
	profile := model.GetProfile()

	dimensions := []struct {
		name string
		dim  user.Dimension
	}{
		{"communication_style", profile.CommunicationStyle},
		{"technical_level", profile.TechnicalLevel},
		{"preferred_languages", profile.PreferredLanguages},
		{"domain_expertise", profile.DomainExpertise},
		{"work_patterns", profile.WorkPatterns},
		{"tool_preferences", profile.ToolPreferences},
	}

	var b strings.Builder
	b.WriteString("用户画像:\n\n")

	hasContent := false
	for _, d := range dimensions {
		if d.dim.Value == "" {
			b.WriteString(fmt.Sprintf("- %s: (未设置)\n", d.name))
			continue
		}
		hasContent = true
		confirmed := ""
		if d.dim.UserConfirmed {
			confirmed = " [用户确认]"
		}
		b.WriteString(fmt.Sprintf("- %s: %s (置信度: %.2f)%s\n", d.name, d.dim.Value, d.dim.Confidence, confirmed))
		if len(d.dim.Evidence) > 0 {
			// Show last 3 evidence entries
			start := 0
			if len(d.dim.Evidence) > 3 {
				start = len(d.dim.Evidence) - 3
			}
			for _, ev := range d.dim.Evidence[start:] {
				b.WriteString(fmt.Sprintf("    证据: %s (%s, %s)\n", ev.Observation, ev.Source, ev.Timestamp.Format("2006-01-02")))
			}
		}
	}

	if !hasContent {
		b.WriteString("\n所有维度均未设置。系统将在对话中逐步学习你的偏好。")
	}

	return b.String()
}

// userModelCorrectTUI sets a dimension to a user-confirmed value.
func (h *TUIAgentHandler) userModelCorrectTUI(model *user.Model, args map[string]interface{}) string {
	dimension := stringArg(args, "dimension")
	if dimension == "" {
		return "缺少 dimension 参数（可选值: communication_style, technical_level, preferred_languages, domain_expertise, work_patterns, tool_preferences）"
	}

	value := stringArg(args, "value")
	if value == "" {
		return "缺少 value 参数"
	}

	if err := model.CorrectDimension(dimension, value); err != nil {
		return fmt.Sprintf("修正失败: %v", err)
	}

	if err := model.Save(); err != nil {
		return fmt.Sprintf("保存失败: %v", err)
	}

	return fmt.Sprintf("已将 %s 设置为: %s (置信度: 1.00, 用户确认)", dimension, value)
}

// userModelResetTUI clears a dimension back to empty state.
func (h *TUIAgentHandler) userModelResetTUI(model *user.Model, args map[string]interface{}) string {
	dimension := stringArg(args, "dimension")
	if dimension == "" {
		return "缺少 dimension 参数（可选值: communication_style, technical_level, preferred_languages, domain_expertise, work_patterns, tool_preferences）"
	}

	if err := model.ResetDimension(dimension); err != nil {
		return fmt.Sprintf("重置失败: %v", err)
	}

	if err := model.Save(); err != nil {
		return fmt.Sprintf("保存失败: %v", err)
	}

	return fmt.Sprintf("已重置 %s", dimension)
}
