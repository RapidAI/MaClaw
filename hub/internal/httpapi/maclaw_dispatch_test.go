package httpapi

import (
	"context"
	"net/http"
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
	if _, status, err := ForwardViaMaClaw(context.Background(), nil, "tenant_default"); err != nil || status != http.StatusServiceUnavailable {
		t.Fatalf("forward without module status=%d err=%v", status, err)
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
