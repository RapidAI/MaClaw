package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// mcpKnowledgeSearch is the read-only local-knowledge surface exposed through
// this computer's MCP service. It deliberately reuses the GUI knowledge tool so
// desktop bots and MCP callers receive the same scoped owner-library results as
// the user-facing assistant, without opening a second knowledge API.
func (a *App) mcpKnowledgeSearch(_ context.Context, query string, limit int) (map[string]any, error) {
	if a == nil {
		return nil, errors.New("这台电脑的知识库还没有准备好")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("missing query argument")
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	raw := a.toolKnowledgeSearch(map[string]interface{}{
		"query": query,
		"limit": limit,
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("知识库检索结果格式错误: %w", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		msg, _ := payload["error"].(string)
		if strings.TrimSpace(msg) == "" {
			msg = "知识库检索失败"
		}
		return payload, errors.New(strings.TrimSpace(msg))
	}
	return payload, nil
}
