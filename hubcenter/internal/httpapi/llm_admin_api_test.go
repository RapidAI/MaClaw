package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestLLMAdminAPIDocsArePublic(t *testing.T) {
	rec := httptest.NewRecorder()
	llmAdminAPIMarkdown(rec, httptest.NewRequest(http.MethodGet, "/api/llm/admin-api.md", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("markdown status=%d type=%s", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/api/admin/llm/provider-arrays/batch") || !strings.Contains(body, "dry_run") || !strings.Contains(body, "/api/admin/llm/access-nodes") || !strings.Contains(body, "member") || !strings.Contains(body, "/api/admin/llm/member-health") || !strings.Contains(body, "tools") || !strings.Contains(body, `"node"`) || !strings.Contains(body, "capability_tags") || !strings.Contains(body, `"enabled": false`) || !strings.Contains(body, `"enabled": true`) {
		t.Fatalf("markdown missing catalog, dry-run, nodes, or member status: %s", body)
	}
	// §6.3 Token Bank automation section: the paths, the three scopes, and the
	// fact that a key is never returned must all be documented.
	for _, want := range []string{
		"## Token Bank 分享管理",
		"/api/admin/llm/token-bank/shares",
		"/api/admin/llm/token-bank/shares/batch",
		"/api/admin/llm/token-bank/shares/{id}/paused",
		"/api/admin/llm/token-bank/shares/{id}",
		"/api/admin/llm/token-bank/shares/{id}/models/{model}/tier",
		"积分倍率固定为 1.0",
		"结算倍率",
		"has_key",
		"share_not_found",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("markdown missing the Token Bank automation section (%q): %s", want, body)
		}
	}

	rec = httptest.NewRecorder()
	llmAdminAPIOpenAPIDoc(rec, httptest.NewRequest(http.MethodGet, "/api/llm/admin-api.json", nil))
	openapi := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(openapi, "provider-arrays/batch") || !strings.Contains(openapi, "ProviderMemberPatch") || !strings.Contains(openapi, `"enabled"`) {
		t.Fatalf("openapi status=%d body=%s", rec.Code, openapi)
	}
	for _, want := range []string{
		"listTokenBankShares",
		"createTokenBankShare",
		"batchCreateTokenBankShares",
		"setTokenBankSharePaused",
		"setTokenBankModelTier",
		"takeOutTokenBankShare",
		"TokenBankPaused",
	} {
		if !strings.Contains(openapi, want) {
			t.Fatalf("openapi missing Token Bank operation %q", want)
		}
	}
	// The OpenAPI document must be valid JSON, not just contain the right words.
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("openapi document is not valid JSON: %v", err)
	}
}

func TestLLMAdminBatchRequiresCredential(t *testing.T) {
	h := RequireLLMAdmin(nil, llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}}), func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler ran without a credential")
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing credential status=%d body=%s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", nil)
	req.Header.Set("Authorization", "Bearer hck_not-a-real-key")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Invalid admin API key") {
		t.Fatalf("bad key status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminCreateLLMAPIKeyDoesNotCacheSecret(t *testing.T) {
	h := adminCreateLLMAdminAPIKey(llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/admin-keys", strings.NewReader(`{"name":"batch"}`))
	h(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"api_key":"hck_`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestAdminTestProviderMemberReportsSavedStatus(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{
			ID:       "pool-a-1",
			Name:     "Primary",
			APIKey:   "sk-live-secret-ABCD",
			Models:   []string{"free-llama-70b"},
			ModelMap: map[string]string{"free-llama-70b": "llama-3.3-70b"},
			Paused:   true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminTestLLMProviderMember(svc, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"model":"free-llama-70b"}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, "sk-live-secret-ABCD") {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	for _, want := range []string{`"success":false`, `"model":"llama-3.3-70b"`, `"api_key_last4":"ABCD"`, `"enabled":false`, `"id":"pool-a-1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"api_url":"https://evil.example"}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "saved provider configuration") {
		t.Fatalf("override status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/missing/test", nil)
	req.SetPathValue("id", "missing")
	h(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestProviderMemberNodeAndTools(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"ping","arguments":"{}"}}]}}]}`))
	}))
	defer upstream.Close()

	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{
			ID:             "pool-a-1",
			Name:           "Primary",
			APIURL:         upstream.URL + "/v1",
			APIKey:         "sk-live-secret-ABCD",
			Models:         []string{"llama"},
			AllowedNodeIDs: []string{"hc-1", "hc-2"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	proxyCfg := &llmservice.ProxyConfig{
		NodeID: "hc-1",
		ListAccessNodes: func() []llmservice.AccessNodeView {
			return []llmservice.AccessNodeView{{NodeID: "hc-1", Self: true}, {NodeID: "hc-2"}}
		},
	}
	h := adminTestLLMProviderMember(svc, proxyCfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"node":"hc-9"}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown node") {
		t.Fatalf("unknown node status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"node":"hc-3"}`))
	req.SetPathValue("id", "pool-a-1")
	proxyCfg.ListAccessNodes = func() []llmservice.AccessNodeView {
		return []llmservice.AccessNodeView{{NodeID: "hc-1", Self: true}, {NodeID: "hc-2"}, {NodeID: "hc-3"}}
	}
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "outside the member access scope") {
		t.Fatalf("scope status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"node":"hc-1","tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object","properties":{}}}}]}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"success":true`) || !strings.Contains(body, `"node":"hc-1"`) || !strings.Contains(body, `"name":"ping"`) || strings.Contains(body, "sk-live-secret-ABCD") {
		t.Fatalf("tool probe status=%d body=%s", rec.Code, body)
	}
	if !strings.Contains(gotBody, `"name":"ping"`) || !strings.Contains(gotBody, "Call one of the available tools now.") {
		t.Fatalf("upstream body = %s", gotBody)
	}
}

func TestAdminTestProviderMemberSendsNativeTools(t *testing.T) {
	var anthropicBody, responsesBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "messages") {
			anthropicBody = string(raw)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","name":"ping","input":{}}]}`))
			return
		}
		responsesBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"function_call","name":"ping","arguments":"{}"}]}`))
	}))
	defer upstream.Close()

	tools := `{"node":"hc-1","tool_choice":"required","tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object","properties":{}}}}]}`
	proxyCfg := &llmservice.ProxyConfig{
		NodeID: "hc-1",
		ListAccessNodes: func() []llmservice.AccessNodeView {
			return []llmservice.AccessNodeView{{NodeID: "hc-1", Self: true}}
		},
	}
	save := func(protocol, wire string) *llmservice.Service {
		svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
		if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
			Providers: []llmpool.ProviderConfig{{
				ID: "pool-a-1", APIURL: upstream.URL + "/v1", APIKey: "sk-live",
				Models: []string{"llama"}, Protocol: protocol, WireAPI: wire,
			}},
		}); err != nil {
			t.Fatal(err)
		}
		return svc
	}

	h := adminTestLLMProviderMember(save("anthropic", ""), proxyCfg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(tools))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) || !strings.Contains(rec.Body.String(), `"name":"ping"`) {
		t.Fatalf("anthropic status=%d body=%s upstream=%s", rec.Code, rec.Body.String(), anthropicBody)
	}
	if !strings.Contains(anthropicBody, `"input_schema"`) || !strings.Contains(anthropicBody, `"type":"any"`) {
		t.Fatalf("anthropic body = %s", anthropicBody)
	}

	h = adminTestLLMProviderMember(save("openai", "responses"), proxyCfg)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(tools))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) || !strings.Contains(rec.Body.String(), `"name":"ping"`) {
		t.Fatalf("responses status=%d body=%s upstream=%s", rec.Code, rec.Body.String(), responsesBody)
	}
	if !strings.Contains(responsesBody, `"max_output_tokens"`) || !strings.Contains(responsesBody, `"name":"ping"`) {
		t.Fatalf("responses body = %s", responsesBody)
	}
}

func TestAdminMemberHealthDayFilter(t *testing.T) {
	h := adminListMemberHealth(nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/member-health?day=yesterday", nil)
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminPatchTokenBankTierRejectsPlainProvider(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{ID: "plain", Name: "Plain", APIURL: "https://plain.example/v1", ArrayID: "plain"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminPatchLLMProviderMember(svc, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/plain", strings.NewReader(`{"token_bank_tier":"high"}`))
	req.SetPathValue("id", "plain")
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "token bank member") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].ArrayID != "plain" {
		t.Fatalf("plain provider moved to %s", reg.Providers[0].ArrayID)
	}
}

func TestAdminPatchRejectsBothNodeFields(t *testing.T) {
	h := adminPatchLLMProviderMember(llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}}), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"allowed_nodes":"hc-1","allowed_node_ids":["hc-2"]}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not both") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminPatchCapabilityTags(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{ID: "a", Name: "A", APIURL: "https://a.example/v1", ArrayID: "a"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminPatchLLMProviderMember(svc, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"capability_tags":"Tools, vision, reasoning"}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tools"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(reg.Providers[0].CapabilityTags, ",") != "tools,vision,reasoning" {
		t.Fatalf("tags = %#v", reg.Providers[0].CapabilityTags)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"capability_tags":["bad tag"]}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminBatchAcceptsCapabilityTagString(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	h := adminImportLLMProviderArrays(svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", strings.NewReader(`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","api_key":"secret","capability_tags":"tools, vision"}]}]}`))
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Providers) != 1 || strings.Join(reg.Providers[0].CapabilityTags, ",") != "tools,vision" {
		t.Fatalf("providers = %+v", reg.Providers)
	}
}

func TestAdminBatchRejectsPauseField(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	h := adminImportLLMProviderArrays(svc)
	for _, body := range []string{
		`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","paused":true}]}]}`,
		`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","enabled":false}]}]}`,
		`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","Paused":true}]}]}`,
		`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","Enabled":false}]}]}`,
		`{"Arrays":[{"id":"pool","name":"Pool","Providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","Paused":true}]}]}`,
		`{"paused":true,"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1"}]}]}`,
		`{"arrays":[{"id":"pool","name":"Pool","enabled":false,"providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1"}]}]}`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", strings.NewReader(body))
		h(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "PATCH enabled") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Providers) != 0 {
		t.Fatalf("providers = %+v", reg.Providers)
	}
}

func TestAdminBatchRejectsTrailingJSON(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	h := adminImportLLMProviderArrays(svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", strings.NewReader(`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","capability_tags":"tools"}]}]}]`))
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminBatchRejectsNonStringCapabilityTag(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	h := adminImportLLMProviderArrays(svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", strings.NewReader(`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","capability_tags":["tools",1]}]}]}`))
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "capability_tags") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminBatchKeepsFractionalWeightRejected(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	h := adminImportLLMProviderArrays(svc)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/provider-arrays/batch", strings.NewReader(`{"arrays":[{"id":"pool","name":"Pool","providers":[{"id":"pool-1","name":"One","api_url":"https://api.example/v1","dispatch_weight":3.0,"capability_tags":"tools"}]}]}`))
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminPatchAcceptsWholeNumberWeight(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{ID: "a", Name: "A", APIURL: "https://a.example/v1"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminPatchLLMProviderMember(svc, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"dispatch_weight":3.0}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].DispatchWeight != 3 {
		t.Fatalf("weight=%d", reg.Providers[0].DispatchWeight)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"dispatch_weight":3.5}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fraction status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSanitizeProviderTestErrorDropsSecret(t *testing.T) {
	got := sanitizeProviderTestError(`Get "https://user:sk-live-secret-ABCD@api.example/v1?api_key=sk-live-secret-ABCD": upstream said sk-live-secret-ABCD`, "sk-live-secret-ABCD")
	if strings.Contains(got, "sk-live") || strings.Contains(got, "user:") {
		t.Fatalf("secret leaked: %s", got)
	}
	reply := sanitizeProviderTestReply("pong sk-live-secret-ABCD", "sk-live-secret-ABCD")
	if strings.Contains(reply, "sk-live") || reply != "pong ****" {
		t.Fatalf("reply = %q", reply)
	}
}

func TestAdminCreateLLMAPIKeyEmptyBodyNeedsName(t *testing.T) {
	h := adminCreateLLMAdminAPIKey(llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}}))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/admin/llm/admin-keys", nil))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "name is required") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
