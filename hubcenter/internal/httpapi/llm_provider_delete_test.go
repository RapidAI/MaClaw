package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func newProviderDeleteTestService(t *testing.T) *llmservice.Service {
	t.Helper()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(context.Background(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "prov_a", Name: "Provider A", APIURL: "https://a.example.com"},
		},
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:   "group_one",
			Name: "Group One",
			Models: []llmpool.ModelConfig{{
				Name:            "model-x",
				ProviderIDs:     []string{"prov_a"},
				ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "prov_a"}},
			}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return svc
}

func TestAdminDeleteLLMProviderConflictListsGroups(t *testing.T) {
	svc := newProviderDeleteTestService(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/providers/prov_a", nil)
	req.SetPathValue("id", "prov_a")
	rr := httptest.NewRecorder()
	adminDeleteLLMProvider(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusConflict, rr.Body.String())
	}
	var body struct {
		Error  string   `json:"error"`
		Groups []string `json:"groups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "provider_in_use" || len(body.Groups) != 1 || body.Groups[0] != "Group One" {
		t.Fatalf("body = %+v", body)
	}

	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.Providers) != 1 {
		t.Fatalf("provider must not be deleted on 409: %#v", reg.Providers)
	}
}

func TestAdminDeleteLLMProviderPrune(t *testing.T) {
	svc := newProviderDeleteTestService(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/providers/prov_a?prune=1", nil)
	req.SetPathValue("id", "prov_a")
	rr := httptest.NewRecorder()
	adminDeleteLLMProvider(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var body struct {
		Status       string   `json:"status"`
		PrunedGroups []string `json:"pruned_groups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || len(body.PrunedGroups) != 1 || body.PrunedGroups[0] != "Group One" {
		t.Fatalf("body = %+v", body)
	}

	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.Providers) != 0 {
		t.Fatalf("provider was not deleted: %#v", reg.Providers)
	}
	if len(reg.ServiceGroups) != 1 || len(reg.ServiceGroups[0].Models[0].ProviderIDs) != 0 || len(reg.ServiceGroups[0].Models[0].ProviderConfigs) != 0 {
		t.Fatalf("group references were not pruned: %#v", reg.ServiceGroups)
	}
}

func TestAdminDeleteLLMProviderNotFound(t *testing.T) {
	svc := newProviderDeleteTestService(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/providers/prov_missing", nil)
	req.SetPathValue("id", "prov_missing")
	rr := httptest.NewRecorder()
	adminDeleteLLMProvider(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestAdminListLLMProviderReferences(t *testing.T) {
	svc := newProviderDeleteTestService(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers/prov_a/references", nil)
	req.SetPathValue("id", "prov_a")
	rr := httptest.NewRecorder()
	adminListLLMProviderReferences(svc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var body struct {
		Groups []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Groups) != 1 || body.Groups[0].ID != "group_one" || body.Groups[0].Name != "Group One" {
		t.Fatalf("body = %+v", body)
	}
}
