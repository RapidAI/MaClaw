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

func TestHubCenterServiceGroupIDsTranslatesOnlyVEVirtualGroup(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "virtual employee group", in: []string{"ve-service"}, want: []string{"system-free"}},
		{name: "case and whitespace", in: []string{" VE-Service ", "redeem"}, want: []string{"system-free", "redeem"}},
		{name: "ordinary groups unchanged", in: []string{"redeem", "enterprise"}, want: []string{"redeem", "enterprise"}},
		{name: "nil remains nil", in: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hubCenterServiceGroupIDs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("groups = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("groups = %#v, want %#v", got, tt.want)
				}
			}
			if len(tt.in) > 0 && &got[0] == &tt.in[0] {
				t.Fatal("translation must not mutate the caller slice")
			}
		})
	}
}
