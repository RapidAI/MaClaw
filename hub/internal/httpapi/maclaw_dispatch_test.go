package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestMaClawModuleGlobalAccessors(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	SetMaClawModule(nil)
	if got := GetMaClawAccessControl(); got != nil {
		t.Fatalf("access control with nil module = %#v", got)
	}
	body, status, err := ForwardViaMaClaw(context.Background(), nil, "tenant_default")
	if err != nil || status != http.StatusServiceUnavailable {
		t.Fatalf("forward without module status=%d err=%v", status, err)
	}
	if !strings.Contains(string(body), "not configured") {
		t.Fatalf("forward without module body=%s", string(body))
	}
	streamResp, err := ForwardStreamViaMaClaw(context.Background(), nil, "tenant_default")
	if err != nil || streamResp == nil || streamResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stream forward without module resp=%#v err=%v", streamResp, err)
	}
	streamBody, _ := io.ReadAll(streamResp.Body)
	_ = streamResp.Body.Close()
	if !strings.Contains(string(streamBody), "not configured") {
		t.Fatalf("stream forward without module body=%s", string(streamBody))
	}

	module := &llmservice.MaClawModule{AccessCtrl: llmservice.NewTenantLLMAccessControl(nil)}
	SetMaClawModule(module)
	if got := GetMaClawModule(); got != module {
		t.Fatalf("module getter returned %#v", got)
	}
	if got := GetMaClawAccessControl(); got != module.AccessCtrl {
		t.Fatalf("access control getter returned %#v", got)
	}
}
