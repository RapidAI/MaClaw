package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAdminProviderArrayRenameAndGuardedDelete(t *testing.T) {
	svc := newProviderDeleteTestService(t)

	renameReq := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-arrays/prov_a", strings.NewReader(`{"name":"  Nanjing  "}`))
	renameReq.SetPathValue("id", "prov_a")
	renameReq.Header.Set("Content-Type", "application/json")
	renameRR := httptest.NewRecorder()
	adminRenameLLMProviderArray(svc).ServeHTTP(renameRR, renameReq)
	if renameRR.Code != http.StatusOK {
		t.Fatalf("rename status = %d, body=%s", renameRR.Code, renameRR.Body.String())
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var renamed string
	for _, arr := range reg.ProviderArrays {
		if arr.ID == "prov_a" {
			renamed = arr.Name
		}
	}
	if renamed != "Nanjing" {
		t.Fatalf("arrays = %#v, want Nanjing", reg.ProviderArrays)
	}

	refReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/provider-arrays/prov_a/references", nil)
	refReq.SetPathValue("id", "prov_a")
	refRR := httptest.NewRecorder()
	adminListLLMProviderArrayReferences(svc).ServeHTTP(refRR, refReq)
	if refRR.Code != http.StatusOK {
		t.Fatalf("references status = %d, body=%s", refRR.Code, refRR.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/provider-arrays/prov_a?prune=1", nil)
	deleteReq.SetPathValue("id", "prov_a")
	deleteRR := httptest.NewRecorder()
	adminDeleteLLMProviderArray(svc).ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want 409; body=%s", deleteRR.Code, deleteRR.Body.String())
	}
	reg, err = svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reg.Providers) != 1 || reg.ServiceGroups[0].Models[0].ProviderConfigs[0].ProviderID != "prov_a" {
		t.Fatalf("referenced array changed: providers=%#v routes=%#v", reg.Providers, reg.ServiceGroups[0].Models[0])
	}
}

func TestAdminAddProviderArray(t *testing.T) {
	svc := newProviderDeleteTestService(t)
	body := `{"id":"nanjing","name":"Nanjing","billing":{"timezone":"Asia/Shanghai","credit_multiplier":2.1,"token_pricing":{"input_credits_per_10k":1,"output_credits_per_10k":4}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	adminAddLLMProviderArray(svc).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found bool
	for _, arr := range reg.ProviderArrays {
		if arr.ID == "nanjing" && arr.Name == "Nanjing" && len(arr.MemberIDs) == 0 && arr.CreditMultiplier == 2.1 && arr.TokenPricing.OutputCreditsPer10K == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("arrays = %#v", reg.ProviderArrays)
	}
	if reg.ServiceGroups[0].Models[0].ProviderConfigs[0].ProviderID != "prov_a" {
		t.Fatalf("existing route changed: %#v", reg.ServiceGroups[0].Models[0])
	}
}

func TestAdminUpdateProviderArrayBilling(t *testing.T) {
	svc := newProviderDeleteTestService(t)
	body := `{"name":"Nanjing","billing":{"timezone":"Asia/Shanghai","credit_multiplier":2.1,"credit_multiplier_schedule":[{"days":[1,2,3,4,5],"start":"00:30","end":"08:30","multiplier":0.5}],"token_pricing":{"input_credits_per_10k":1,"output_credits_per_10k":4,"minimum_request_credits":0.1,"timezone":"Asia/Shanghai","version":"2026-08-23-v1"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-arrays/prov_a", strings.NewReader(body))
	req.SetPathValue("id", "prov_a")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	adminRenameLLMProviderArray(svc).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	reg, err := svc.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	provider := reg.Providers[0]
	if provider.CreditMultiplier != 2.1 || provider.TokenPricing.InputCreditsPer10K != 1 || provider.TokenPricing.OutputCreditsPer10K != 4 || len(provider.CreditMultiplierSchedule) != 1 {
		t.Fatalf("provider billing = %#v schedule=%#v", provider.TokenPricing, provider.CreditMultiplierSchedule)
	}
	var arrName string
	var arrMultiplier float64
	for _, arr := range reg.ProviderArrays {
		if arr.ID == "prov_a" {
			arrName = arr.Name
			arrMultiplier = arr.CreditMultiplier
		}
	}
	if arrName != "Nanjing" || arrMultiplier != 2.1 {
		t.Fatalf("array name=%q multiplier=%v", arrName, arrMultiplier)
	}
	if reg.ServiceGroups[0].Models[0].ProviderConfigs[0].ProviderID != "prov_a" {
		t.Fatalf("route changed: %#v", reg.ServiceGroups[0].Models[0])
	}

	bad := httptest.NewRequest(http.MethodPut, "/api/admin/llm/provider-arrays/prov_a", strings.NewReader(`{"name":"Nanjing","billing":{"credit_multiplier":1,"token_pricing":{"input_credits_per_10k":-1,"output_credits_per_10k":1}}}`))
	bad.SetPathValue("id", "prov_a")
	bad.Header.Set("Content-Type", "application/json")
	badRR := httptest.NewRecorder()
	adminRenameLLMProviderArray(svc).ServeHTTP(badRR, bad)
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("negative price status = %d, body=%s", badRR.Code, badRR.Body.String())
	}
}
