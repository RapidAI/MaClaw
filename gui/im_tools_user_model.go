package main

// manage_user_model tool handler: allows users to view, correct, and reset
// individual profile dimensions in the dialectic user model.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/user"
)

// getUserModel returns the user model, lazily initializing it if not already
// created. Returns nil if initialization fails.
func (h *IMMessageHandler) getUserModel() *user.Model {
	if h.app == nil {
		return nil
	}
	h.app.userModelMu.Do(func() {
		modelPath := h.app.userModelPath()
		model, err := user.NewModel(modelPath)
		if err != nil {
			fmt.Printf("[manage_user_model] failed to load user model: %v\n", err)
			return
		}
		h.app.userModel = model
	})
	return h.app.userModel
}

// toolManageUserModel handles the manage_user_model tool call.
// Parameters:
//   - action (string, required): "view", "correct", or "reset"
//   - dimension (string, optional): the profile dimension to correct/reset
//   - value (string, optional): the new value for the dimension (required for "correct")
func (h *IMMessageHandler) toolManageUserModel(args map[string]interface{}) string {
	actionText := stringVal(args, "action")
	action := normalizeUserModelAction(actionText)
	if strings.TrimSpace(actionText) == "" {
		return "缺少 action 参数（可选值: view, correct, reset）"
	}

	model := h.getUserModel()
	if model == nil {
		return "用户画像模块未初始化"
	}

	switch action {
	case userModelActionView:
		return h.userModelView(model)
	case userModelActionCorrect:
		return h.userModelCorrect(model, args)
	case userModelActionReset:
		return h.userModelReset(model, args)
	default:
		return fmt.Sprintf("未知 action: %s（可选值: view, correct, reset）", action)
	}
}

// userModelView formats the current user profile as readable text.
func (h *IMMessageHandler) userModelView(model *user.Model) string {
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

// userModelCorrect sets a dimension to a user-confirmed value.
func (h *IMMessageHandler) userModelCorrect(model *user.Model, args map[string]interface{}) string {
	dimension := stringVal(args, "dimension")
	if dimension == "" {
		return "缺少 dimension 参数（可选值: communication_style, technical_level, preferred_languages, domain_expertise, work_patterns, tool_preferences）"
	}

	value := stringVal(args, "value")
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

// userModelReset clears a dimension back to empty state.
func (h *IMMessageHandler) userModelReset(model *user.Model, args map[string]interface{}) string {
	dimension := stringVal(args, "dimension")
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
