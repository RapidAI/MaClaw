package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	if !strings.Contains(body, "/api/admin/llm/provider-arrays/batch") || !strings.Contains(body, "dry_run") || !strings.Contains(body, "/api/admin/llm/access-nodes") || !strings.Contains(body, "member") || !strings.Contains(body, "/api/admin/llm/member-health") || !strings.Contains(body, "tools") || !strings.Contains(body, `"node"`) || !strings.Contains(body, "capability_tags") || !strings.Contains(body, "extra_body") || !strings.Contains(body, "drop_fields") || !strings.Contains(body, `"enabled": false`) || !strings.Contains(body, `"enabled": true`) {
		t.Fatalf("markdown missing catalog, dry-run, nodes, or member status: %s", body)
	}
	for _, want := range []string{
		"extra_body_priority 只有是 downstream",
		"OpenAI 兼容 chat、Anthropic 兼容和 Responses",
		"先合并 extra_body，再按 drop_fields 删除字段",
		`"extra_body":{"reasoning_effort":"none"}`,
		"max_output_tokens_per_request",
		"max_completion_tokens",
		"两个字段各自截断",
		"stream ended before the end marker",
		"stream_end",
		"回执不包含密钥",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("markdown missing upstream override doc %q", want)
		}
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
	if rec.Code != http.StatusOK || !strings.Contains(openapi, "provider-arrays/batch") || !strings.Contains(openapi, "ProviderMemberPatch") || !strings.Contains(openapi, `"enabled"`) || !strings.Contains(openapi, "extra_body") || !strings.Contains(openapi, "extra_body_priority") || !strings.Contains(openapi, "drop_fields") {
		t.Fatalf("openapi status=%d body=%s", rec.Code, openapi)
	}
	for _, want := range []string{
		"a missing priority means member",
		"dry_run true validates",
		"The test body cannot override the first three",
		"max_output_tokens_per_request",
		"each field is capped on its own",
		"stream_end",
		"The receipt does not include the API key or the request body",
		"At most 16 KiB",
		"OpenAI-compatible chat, Anthropic-compatible, and Responses",
	} {
		if !strings.Contains(openapi, want) {
			t.Fatalf("openapi missing upstream override doc %q", want)
		}
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
	// free-llama-70b is in the member's model list, so the probe dials it as
	// saved. The model map only rewrites names outside that list.
	for _, want := range []string{`"success":false`, `"model":"free-llama-70b"`, `"api_key_last4":"ABCD"`, `"enabled":false`, `"id":"pool-a-1"`} {
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

func TestAdminPatchMemberOutputCap(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{ID: "a", Name: "A", APIURL: "https://a.example/v1", MaxOutputTokensPerRequest: 4096}},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminPatchLLMProviderMember(svc, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"max_output_tokens_per_request":16384}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"max_output_tokens_per_request":16384`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].MaxOutputTokensPerRequest != 16384 {
		t.Fatalf("cap = %d", reg.Providers[0].MaxOutputTokensPerRequest)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"max_output_tokens_per_request":0}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err = svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].MaxOutputTokensPerRequest != 0 {
		t.Fatalf("cleared cap = %d", reg.Providers[0].MaxOutputTokensPerRequest)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"max_output_tokens_per_request":-1}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestProviderMemberOutputBudget(t *testing.T) {
	var mu sync.Mutex
	var saw []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		mu.Lock()
		saw = append(saw, payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong","tool_calls":[{"function":{"name":"ping","arguments":"{}"}}]}}]}`))
	}))
	defer upstream.Close()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-a-1", Name: "Typhoon", APIURL: upstream.URL + "/v1", APIKey: "sk-live",
			Models: []string{"typhoon"}, MaxOutputTokensPerRequest: 16384,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	proxyCfg := &llmservice.ProxyConfig{NodeID: "hc-1"}
	h := adminTestLLMProviderMember(svc, proxyCfg)
	post := func(body string) map[string]any {
		t.Helper()
		mu.Lock()
		before := len(saw)
		mu.Unlock()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(body))
		req.SetPathValue("id", "pool-a-1")
		h(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
			t.Fatalf("status=%d body=%s request=%s", rec.Code, rec.Body.String(), body)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(saw) != before+1 {
			t.Fatalf("captured %d, want one new for %s", len(saw)-before, body)
		}
		return saw[before]
	}
	number := func(body map[string]any, key string) int64 {
		t.Helper()
		n, ok := body[key].(float64)
		if !ok {
			t.Fatalf("%s = %#v", key, body[key])
		}
		return int64(n)
	}
	huge := post(`{"max_tokens":100000}`)
	if number(huge, "max_tokens") != 16384 {
		t.Fatalf("huge = %#v", huge["max_tokens"])
	}
	if _, ok := huge["max_completion_tokens"]; ok {
		t.Fatal("huge probe invented max_completion_tokens")
	}
	small := post(`{"max_tokens":128}`)
	if number(small, "max_tokens") != 128 {
		t.Fatalf("small = %#v", small["max_tokens"])
	}
	both := post(`{"max_tokens":100000,"max_completion_tokens":2000,"tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object","properties":{}}}}]}`)
	if number(both, "max_tokens") != 16384 || number(both, "max_completion_tokens") != 2000 {
		t.Fatalf("both = %#v / %#v", both["max_tokens"], both["max_completion_tokens"])
	}
	tools, _ := both["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", both["tools"])
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"max_tokens":-1}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestProviderMemberStreamAndOutputReceipt(t *testing.T) {
	const probeSSE = "data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ng\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"ping\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	var mu sync.Mutex
	var saw []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream body: %v", err)
			return
		}
		mu.Lock()
		saw = append(saw, payload)
		mu.Unlock()
		if payload["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			if payload["max_tokens"] == float64(1) {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n"))
				return
			}
			_, _ = w.Write([]byte(probeSSE))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "pool-a-1", Name: "Typhoon", APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"typhoon"}, MaxOutputTokensPerRequest: 16384},
			{ID: "pool-small", Name: "Small", APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"typhoon"}, MaxOutputTokensPerRequest: 32},
			{ID: "pool-empty", Name: "Empty", APIKey: "sk-live", Models: []string{"typhoon"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminTestLLMProviderMember(svc, &llmservice.ProxyConfig{NodeID: "hc-1"})
	type outField struct {
		Value   int64 `json:"value"`
		Clipped bool  `json:"clipped"`
	}
	post := func(id, body string) (int, map[string]json.RawMessage, map[string]any) {
		t.Helper()
		mu.Lock()
		before := len(saw)
		mu.Unlock()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/"+id+"/test", strings.NewReader(body))
		req.SetPathValue("id", id)
		h(rec, req)
		var top map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil && rec.Code == http.StatusOK {
			t.Fatalf("decode receipt: %v body=%s", err, rec.Body.String())
		}
		mu.Lock()
		defer mu.Unlock()
		var wire map[string]any
		if len(saw) == before+1 {
			wire = saw[before]
		}
		return rec.Code, top, wire
	}
	outputOf := func(top map[string]json.RawMessage) map[string]outField {
		t.Helper()
		raw, ok := top["output"]
		if !ok {
			t.Fatalf("receipt missing output: %s", top)
		}
		if !strings.Contains(string(raw), `"clipped"`) {
			t.Fatalf("clipped omitted: %s", raw)
		}
		var output map[string]outField
		if err := json.Unmarshal(raw, &output); err != nil {
			t.Fatal(err)
		}
		return output
	}
	tools := `{"max_tokens":128,"tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object","properties":{}}}}],"stream":true}`
	code, top, wire := post("pool-a-1", tools)
	if code != http.StatusOK {
		t.Fatalf("stream status=%d", code)
	}
	var success bool
	_ = json.Unmarshal(top["success"], &success)
	var reply string
	_ = json.Unmarshal(top["reply"], &reply)
	var streamEnd bool
	if err := json.Unmarshal(top["stream_end"], &streamEnd); err != nil || !streamEnd {
		t.Fatalf("stream_end=%s err=%v", top["stream_end"], err)
	}
	var calls []struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := json.Unmarshal(top["tool_calls"], &calls); err != nil || len(calls) != 1 || calls[0].Name != "ping" || calls[0].Arguments != "{}" {
		t.Fatalf("tool_calls=%s err=%v", top["tool_calls"], err)
	}
	if !success || reply != "pong" || string(top["stream"]) != "true" {
		t.Fatalf("success=%v reply=%q stream=%s body keys failed", success, reply, top["stream"])
	}
	got := outputOf(top)
	if got["max_tokens"].Value != 128 || got["max_tokens"].Clipped {
		t.Fatalf("stream output=%#v", got["max_tokens"])
	}
	if _, ok := got["max_completion_tokens"]; ok {
		t.Fatal("stream probe invented max_completion_tokens")
	}
	if wire["stream"] != true || wire["max_tokens"] != float64(128) {
		t.Fatalf("upstream stream body=%#v", wire)
	}
	receipt := string(mustJSON(top))
	for _, secret := range []string{"sk-live", `"messages"`, `"parameters"`, "Call one of the available tools"} {
		if strings.Contains(receipt, secret) {
			t.Fatalf("receipt contains %s: %s", secret, receipt)
		}
	}

	code, top, _ = post("pool-a-1", `{"stream":true,"max_tokens":1}`)
	if code != http.StatusOK {
		t.Fatalf("truncated stream status=%d", code)
	}
	_ = json.Unmarshal(top["success"], &success)
	var errMsg string
	_ = json.Unmarshal(top["error"], &errMsg)
	_ = json.Unmarshal(top["reply"], &reply)
	if err := json.Unmarshal(top["stream_end"], &streamEnd); err != nil || streamEnd {
		t.Fatalf("missing marker stream_end=%s", top["stream_end"])
	}
	if success || !strings.Contains(errMsg, "stream ended before the end marker") || reply != "po" {
		t.Fatalf("success=%v error=%q reply=%q", success, errMsg, reply)
	}
	if got := outputOf(top)["max_tokens"]; got.Value != 1 || got.Clipped {
		t.Fatalf("partial output=%#v", got)
	}

	code, top, wire = post("pool-a-1", `{"max_tokens":100000}`)
	_ = json.Unmarshal(top["success"], &success)
	if code != http.StatusOK || !success || wire["max_tokens"] != float64(16384) || wire["stream"] != false {
		t.Fatalf("huge status=%d success=%v wire=%#v", code, success, wire)
	}
	if _, ok := top["stream"]; ok || top["stream_end"] != nil {
		t.Fatal("non-stream receipt included stream fields")
	}
	if got := outputOf(top)["max_tokens"]; got.Value != 16384 || !got.Clipped {
		t.Fatalf("huge output=%#v", got)
	}
	if _, ok := outputOf(top)["max_completion_tokens"]; ok {
		t.Fatal("huge receipt invented max_completion_tokens")
	}

	_, top, wire = post("pool-a-1", `{"max_tokens":128}`)
	if got := outputOf(top)["max_tokens"]; got.Value != 128 || got.Clipped || wire["max_tokens"] != float64(128) {
		t.Fatalf("small output=%#v wire=%#v", got, wire["max_tokens"])
	}
	_, top, wire = post("pool-a-1", `{"max_tokens":100000,"max_completion_tokens":2000}`)
	both := outputOf(top)
	if both["max_tokens"].Value != 16384 || !both["max_tokens"].Clipped || both["max_completion_tokens"].Value != 2000 || both["max_completion_tokens"].Clipped {
		t.Fatalf("both=%#v", both)
	}
	if wire["max_tokens"] != float64(16384) || wire["max_completion_tokens"] != float64(2000) {
		t.Fatalf("both wire=%#v / %#v", wire["max_tokens"], wire["max_completion_tokens"])
	}
	_, top, wire = post("pool-a-1", `{}`)
	if got := outputOf(top)["max_tokens"]; got.Value != 64 || got.Clipped || wire["max_tokens"] != float64(64) {
		t.Fatalf("omitted cap above default output=%#v wire=%#v", got, wire["max_tokens"])
	}
	_, top, wire = post("pool-small", `{}`)
	if got := outputOf(top)["max_tokens"]; got.Value != 32 || !got.Clipped || wire["max_tokens"] != float64(32) {
		t.Fatalf("omitted cap below default output=%#v wire=%#v", got, wire["max_tokens"])
	}
	_, top, wire = post("pool-a-1", `{"max_tokens":0}`)
	if got := outputOf(top)["max_tokens"]; got.Value != 0 || got.Clipped || wire["max_tokens"] != float64(0) {
		t.Fatalf("explicit zero output=%#v wire=%#v", got, wire["max_tokens"])
	}
	_, top, wire = post("pool-a-1", `{"stream":false,"max_tokens":128}`)
	if _, ok := top["stream"]; ok || top["stream_end"] != nil || wire["stream"] != false {
		t.Fatalf("stream false leaked stream=%s end=%s wire=%#v", top["stream"], top["stream_end"], wire["stream"])
	}

	code, _, _ = post("pool-a-1", `{"stream":"yes"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("non-bool stream status=%d", code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"stream":"yes"}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if !strings.Contains(rec.Body.String(), "stream must be a boolean") {
		t.Fatalf("stream type body=%s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/pool-a-1/test", strings.NewReader(`{"api_key":"x"}`))
	req.SetPathValue("id", "pool-a-1")
	h(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unknown field api_key") {
		t.Fatalf("unknown field status=%d body=%s", rec.Code, rec.Body.String())
	}

	code, top, wire = post("pool-empty", `{"stream":true}`)
	_ = json.Unmarshal(top["success"], &success)
	_ = json.Unmarshal(top["error"], &errMsg)
	if err := json.Unmarshal(top["stream_end"], &streamEnd); err != nil || streamEnd {
		t.Fatalf("early stream_end=%s", top["stream_end"])
	}
	if code != http.StatusOK || success || !strings.Contains(errMsg, "api_url and model are required") || string(top["stream"]) != "true" || top["output"] != nil || wire != nil {
		t.Fatalf("early status=%d success=%v error=%q stream=%s output=%s wire=%v", code, success, errMsg, top["stream"], top["output"], wire != nil)
	}
	if strings.Contains(string(mustJSON(top)), "sk-live") {
		t.Fatal("early receipt contains the key")
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(err.Error())
	}
	return raw
}

// The /test receipt reports content_normalized when the upstream answered
// with a content-block array (Mistral's hosted shape) that the probe folded
// into the reply string. A string reply reports false.
func TestAdminTestProviderMemberContentNormalized(t *testing.T) {
	const arraySSE = "data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"why \"}]}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"po\"}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"ng\"}]},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode upstream body: %v", err)
			return
		}
		mistral := payload["model"] == "mistral-large-latest"
		if payload["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			if mistral {
				_, _ = w.Write([]byte(arraySSE))
				return
			}
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if mistral {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"why "}]},{"type":"text","text":"pong"}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "pool-mistral", Name: "Mistral", APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"mistral-large-latest"}},
			{ID: "pool-plain", Name: "Plain", APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"plain"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminTestLLMProviderMember(svc, &llmservice.ProxyConfig{NodeID: "hc-1"})
	post := func(id, body string) map[string]json.RawMessage {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/"+id+"/test", strings.NewReader(body))
		req.SetPathValue("id", id)
		h(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", id, rec.Code, rec.Body.String())
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &top); err != nil {
			t.Fatalf("decode receipt: %v body=%s", err, rec.Body.String())
		}
		return top
	}
	assertReceipt := func(top map[string]json.RawMessage, wantNormalized bool) {
		t.Helper()
		var success bool
		_ = json.Unmarshal(top["success"], &success)
		var reply string
		_ = json.Unmarshal(top["reply"], &reply)
		var normalized bool
		if err := json.Unmarshal(top["content_normalized"], &normalized); err != nil {
			t.Fatalf("content_normalized missing: %s", top["content_normalized"])
		}
		if !success || reply != "pong" || normalized != wantNormalized {
			t.Fatalf("success=%v reply=%q normalized=%v want %v", success, reply, normalized, wantNormalized)
		}
	}
	assertReceipt(post("pool-mistral", `{}`), true)
	assertReceipt(post("pool-mistral", `{"stream":true}`), true)
	assertReceipt(post("pool-plain", `{}`), false)
	assertReceipt(post("pool-plain", `{"stream":true}`), false)
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
