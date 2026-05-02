package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestGetLLMServiceAccountHandlerReturnsStatusAndUsage(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "account@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		UserBindings:       []llmservice.UserBinding{{Email: "account@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []llmservice.Grant{{
			ID: "grant-1", Email: "account@example.com", ServiceGroupID: "coding-basic",
			Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
			CreditsTotal: 120, CreditsUsed: 25,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	rep := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	rep.addUsage(now, "account@example.com", nil, corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 20, TotalTokens: 30, TotalCostRMB: 0.42, Requests: 1}, 0.003)
	if err := saveLLMUsageReports(ctx, system, rep); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/llm/service/account", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	GetLLMServiceAccountHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp llmServiceAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Email != "account@example.com" || resp.Status == nil || resp.Status.CreditsAvailable != 95 {
		t.Fatalf("unexpected account response: %+v", resp)
	}
	if resp.Usage.TotalTokens != 30 || resp.Usage.TotalCostRMB != 0.42 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}
