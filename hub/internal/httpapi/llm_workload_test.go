package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestApplyHubWorkloadSelectionCopiesIncomingHints(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req.Header.Set(llmpool.WorkflowTypeHeader, "coding")
	req.Header.Set(llmpool.PhaseKindHeader, "execution")
	req.Header.Set(llmpool.TaskTypeHeader, "reasoning")
	out := applyHubWorkloadSelection(rec, req, map[string]any{"model": "auto"}, nil, nil, &llmpool.WorkloadDecision{
		Class:  llmpool.WorkloadFallbackBalanced,
		Source: llmpool.ClassSourceFallback,
	})
	meta := llmservice.OfficialForwardMetaFrom(out.Context())
	if meta.WorkflowType != "coding" || meta.PhaseKind != "execution" || meta.TaskType != "reasoning" {
		t.Fatalf("forward meta hints = %+v", meta)
	}
}

func TestRewriteOfficialForwardBodyDoesNotSendAuto(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name:                   llmpool.OfficialTierMid,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ProviderUpstreamModels: map[string]string{"maclaw_official": llmpool.OfficialTierMid},
	}
	got := rewriteOfficialForwardBody(map[string]any{"model": "auto", "messages": []any{}}, model, llmservice.MaClawOfficialProviderID)
	if got["model"] != llmpool.OfficialTierMid {
		t.Fatalf("model = %#v, want official-mid", got["model"])
	}
}

func TestOfficialForwardServiceGroupIDsUsesOfficialPool(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name:                   llmpool.OfficialTierHigh,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ProviderUpstreamModels: map[string]string{"maclaw_official": llmpool.OfficialTierHigh},
		ProviderServiceGroups:  map[string][]string{"maclaw_official": {"coding-auto"}},
	}
	got := officialForwardServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(got) != 1 || got[0] != llmpool.OfficialGroupID {
		t.Fatalf("forward groups = %#v", got)
	}
	charged := llmservice.ChargedServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(charged) != 1 || charged[0] != "coding-auto" {
		t.Fatalf("charged = %#v", charged)
	}
}

func TestOfficialForwardServiceGroupIDsKeepsHubOfficialEntry(t *testing.T) {
	model := &llmservice.AuthorizedModel{
		Name:                  "auto",
		ServiceGroupIDs:       []string{llmservice.MaClawOfficialServiceGroupID},
		ProviderServiceGroups: map[string][]string{"maclaw_official": {llmservice.MaClawOfficialServiceGroupID}},
	}
	got := officialForwardServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(got) != 1 || got[0] != llmservice.MaClawOfficialServiceGroupID {
		t.Fatalf("forward groups = %#v, want Hub official entry", got)
	}
	charged := llmservice.ChargedServiceGroupIDs(model, llmservice.MaClawOfficialProviderID)
	if len(charged) != 1 || charged[0] != llmservice.MaClawOfficialServiceGroupID {
		t.Fatalf("charged = %#v, want Hub official entry", charged)
	}
}
