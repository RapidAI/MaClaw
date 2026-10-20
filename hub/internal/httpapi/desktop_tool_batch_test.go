package httpapi

import (
	"context"
	"strings"
	"testing"
)

func TestDesktopToolComputerBatchIsNotDropped(t *testing.T) {
	for _, action := range []string{"computer", "app_run"} {
		call := desktopToolCall{RunID: "r", ToolCallID: "c", Tenant: "t", User: "u", Action: action,
			Args: map[string]any{"steps": []any{map[string]any{"action": "type", "text": "hi"}}}}
		_, _, err := dispatchDesktopTool(context.Background(), nil, call)
		if err == nil || !strings.Contains(err.Error(), "steps run on the worker") {
			t.Fatalf("%s batch: err=%v, want explicit refusal", action, err)
		}
	}
	if desktopToolHasBatch(map[string]any{"args": []any{"key", "Return"}}) {
		t.Fatal("plain app args are not a batch")
	}
}
