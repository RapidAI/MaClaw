package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type contextUsageRecordingCallbacks struct {
	*mockCallbacks
	used      int
	max       int
	estimated bool
	calls     int
}

func (c *contextUsageRecordingCallbacks) ReportContextWindowUsage(used, max int, estimated bool) {
	c.used = used
	c.max = max
	c.estimated = estimated
	c.calls++
}

func TestRunLoopReportsOutboundContextBeforeProviderResponse(t *testing.T) {
	cb := &contextUsageRecordingCallbacks{}
	tools := []map[string]interface{}{tooldef.BuildToolDef("read_file", "Read a file", map[string]interface{}{"type": "object"})}
	usageBeforeResponse := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usageBeforeResponse = cb.calls > 0 && cb.used > 0
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(textResponse("done"))
	}))
	defer server.Close()
	cb.mockCallbacks = &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key", ContextLength: 10000},
		maxIter:    1,
		sysPrompt:  "sys",
		toolResult: "ok",
		tools:      tools,
	}
	result := RunLoop(cb, "inspect the project", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !usageBeforeResponse {
		t.Fatal("provider response arrived before any outbound usage estimate")
	}
	if !cb.estimated {
		t.Fatal("outbound estimate was not marked as estimated")
	}
	if cb.max != 8000 {
		t.Fatalf("context max = %d, want 8000", cb.max)
	}
	if minimum := EstimateToolsTokens(tools); cb.used < minimum {
		t.Fatalf("usage %d excludes tool definitions (%d)", cb.used, minimum)
	}
}
