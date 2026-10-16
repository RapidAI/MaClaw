package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestInactiveSysUserBlocksAPIKeys(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := identity.UsersRepo().Create(ctx, &store.User{
		ID:               llmservice.SystemLLMUserID(store.DefaultTenantID),
		TenantID:         store.DefaultTenantID,
		Email:            llmservice.SystemLLMUserEmail,
		SN:               "SN-SYSUSER-inactive",
		Status:           "inactive",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create inactive sys_user: %v", err)
	}
	if _, err := ensureHubSystemLLMUser(ctx, identity, store.DefaultTenantID); !errors.Is(err, errSystemLLMUserInactive) {
		t.Fatalf("ensureHubSystemLLMUser err = %v", err)
	}
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-pro",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/endpoint-api-keys", strings.NewReader(`{"service_group_id":"coding-pro"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateLLMEndpointAPIKeyHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReservedSysUserCannotLogin(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	if _, err := identity.RequestEmailLogin(ctx, llmservice.SystemLLMUserEmail); !errors.Is(err, auth.ErrReservedSystemAccount) {
		t.Fatalf("sys_user email login err = %v", err)
	}
	if _, err := identity.AdminConfirmLoginByEmail(ctx, llmservice.SystemLLMUserEmail); !errors.Is(err, auth.ErrReservedSystemAccount) {
		t.Fatalf("sys_user admin confirm err = %v", err)
	}
	if _, err := identity.StartEnrollment(ctx, llmservice.SystemLLMUserEmail, "pc", "windows", "", ""); !errors.Is(err, auth.ErrReservedSystemAccount) {
		t.Fatalf("sys_user enrollment err = %v", err)
	}
}

func TestLLMEndpointAPIKeyCRUDAndChatUsesSysUser(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointUserLimiter.reset()
	defer globalLLMEndpointUserLimiter.reset()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{
				ID:           "coding-pro",
				Name:         "Coding Pro",
				AccessPolicy: llmservice.AccessPolicyGrantRequired,
				Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
			},
			{
				ID:           "writing-basic",
				Name:         "Writing",
				AccessPolicy: llmservice.AccessPolicyGrantRequired,
				Models:       []llmservice.ModelServiceModel{{Name: "writer", ProviderIDs: []string{"provider-a"}}},
			},
		},
		DefaultNewUserBenefitMode: llmservice.NewUserBenefitModeLimitCard,
		DefaultNewUserLimitCard:   llmservice.NewUserLimitCard{ServiceGroupIDs: []string{"coding-pro"}, PeriodLimits: llmservice.CreditPeriodLimits{Daily: 5}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/llm/endpoint-api-keys", strings.NewReader(`{"service_group_id":"coding-pro","name":"ci"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	CreateLLMEndpointAPIKeyHandler(identity, system, nil).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		AccessToken    string `json:"access_token"`
		Email          string `json:"email"`
		ServiceGroupID string `json:"service_group_id"`
		Key            struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !strings.HasPrefix(created.AccessToken, "sk-llmg-") || created.Email != llmservice.SystemLLMUserEmail || created.ServiceGroupID != "coding-pro" || created.Key.ID == "" {
		t.Fatalf("unexpected create payload: %#v", created)
	}

	user, err := identity.UsersRepo().GetByTenantEmail(ctx, store.DefaultTenantID, llmservice.SystemLLMUserEmail)
	if err != nil || user == nil {
		t.Fatalf("sys_user missing: user=%#v err=%v", user, err)
	}
	reg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if llmservice.HasLiveNewUserLimitCard(reg, user.ID, user.Email) {
		t.Fatalf("sys_user received a new-user limit card: %#v", reg.Grants)
	}

	listRec := httptest.NewRecorder()
	ListLLMEndpointAPIKeysHandler(system).ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/admin/llm/endpoint-api-keys", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	if !bytes.Contains(listRec.Body.Bytes(), []byte(created.Key.ID)) || !bytes.Contains(listRec.Body.Bytes(), []byte(`"account":"sys_user"`)) {
		t.Fatalf("list missing created key: %s", listRec.Body.String())
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	modelsReq := httptest.NewRequest(http.MethodGet, "/api/llm/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Bearer "+created.AccessToken)
	modelsRec := httptest.NewRecorder()
	LLMV1ModelsHandler(identity, system, nil).ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("models status = %d body=%s", modelsRec.Code, modelsRec.Body.String())
	}
	if !bytes.Contains(modelsRec.Body.Bytes(), []byte(`"id":"auto"`)) || bytes.Contains(modelsRec.Body.Bytes(), []byte(`"id":"writer"`)) {
		t.Fatalf("models should only expose the bound service group, body=%s", modelsRec.Body.String())
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	chatReq := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	chatReq.Header.Set("Authorization", "Bearer "+created.AccessToken)
	chatReq.Header.Set("Content-Type", "application/json")
	chatRec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(chatRec, chatReq)
	if chatRec.Code != http.StatusOK {
		t.Fatalf("chat status = %d body=%s", chatRec.Code, chatRec.Body.String())
	}

	writerBody, err := json.Marshal(map[string]any{"model": "writer", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal writer body: %v", err)
	}
	writerReq := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(writerBody))
	writerReq.Header.Set("Authorization", "Bearer "+created.AccessToken)
	writerReq.Header.Set("Content-Type", "application/json")
	writerRec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(writerRec, writerReq)
	if writerRec.Code != http.StatusForbidden {
		t.Fatalf("unbound model status = %d body=%s", writerRec.Code, writerRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/endpoint-api-keys/"+created.Key.ID, nil)
	deleteReq.SetPathValue("id", created.Key.ID)
	deleteRec := httptest.NewRecorder()
	DeleteLLMEndpointAPIKeyHandler(system, nil).ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	chatReq2 := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	chatReq2.Header.Set("Authorization", "Bearer "+created.AccessToken)
	chatReq2.Header.Set("Content-Type", "application/json")
	chatRec2 := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(chatRec2, chatReq2)
	if chatRec2.Code != http.StatusUnauthorized {
		t.Fatalf("deleted key status = %d body=%s", chatRec2.Code, chatRec2.Body.String())
	}
}

func TestLLMEndpointAPIKeyWithoutServiceGroupIsRejected(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	system := newTestLLMServiceSystemSettings()
	raw, err := newLLMEndpointAPIKeyToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	key := llmEndpointAPIKey{
		ID:             "llmk-empty-group",
		TenantID:       store.DefaultTenantID,
		ServiceGroupID: "",
		TokenHash:      hashLLMEndpointAPIKey(raw),
		TokenPrefix:    llmEndpointAPIKeyDisplayPrefix(raw),
		CreatedAt:      time.Now().UTC(),
	}
	if err := saveCreatedLLMEndpointAPIKey(context.Background(), system, system, key); err != nil {
		t.Fatalf("save key: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	LLMV1ModelsHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty service group key status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateLLMEndpointAPIKeyRequiresServiceGroup(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	system := newTestLLMServiceSystemSettings()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/endpoint-api-keys", strings.NewReader(`{"name":"ci"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	CreateLLMEndpointAPIKeyHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMEndpointAPIKeysAreTenantScoped(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-pro",
			Name:         "Coding Pro",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", system), reg); err != nil {
		t.Fatalf("save tenant_a registry: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_b", system), reg); err != nil {
		t.Fatalf("save tenant_b registry: %v", err)
	}

	createFor := func(tenantID string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/endpoint-api-keys", strings.NewReader(`{"service_group_id":"coding-pro","name":"`+tenantID+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(WithRequestTenant(req.Context(), tenantID))
		rec := httptest.NewRecorder()
		CreateLLMEndpointAPIKeyHandler(identity, system, nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", tenantID, rec.Code, rec.Body.String())
		}
		var payload struct {
			Key struct {
				ID string `json:"id"`
			} `json:"key"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s: %v", tenantID, err)
		}
		return payload.Key.ID
	}
	idA := createFor("tenant_a")
	idB := createFor("tenant_b")

	listFor := func(tenantID string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/endpoint-api-keys", nil)
		req = req.WithContext(WithRequestTenant(req.Context(), tenantID))
		rec := httptest.NewRecorder()
		ListLLMEndpointAPIKeysHandler(system).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s status = %d body=%s", tenantID, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	listA := listFor("tenant_a")
	listB := listFor("tenant_b")
	if !strings.Contains(listA, idA) || strings.Contains(listA, idB) {
		t.Fatalf("tenant_a list leaked keys: %s", listA)
	}
	if !strings.Contains(listB, idB) || strings.Contains(listB, idA) {
		t.Fatalf("tenant_b list leaked keys: %s", listB)
	}
}

func TestLLMEndpointAPIKeysHaveIndependentRateLimits(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointUserLimiter.reset()
	defer globalLLMEndpointUserLimiter.reset()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-pro",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "upstream", "model": "auto",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		UserRateLimitPerMinute: 1,
		UserRateLimitBurst:     1,
		UserRateLimitMaxWaitMS: 20,
		Providers:              []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save providers: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	createKey := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/endpoint-api-keys", strings.NewReader(`{"service_group_id":"coding-pro"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		CreateLLMEndpointAPIKeyHandler(identity, system, nil).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
		}
		var payload struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return payload.AccessToken
	}
	keyA := createKey()
	keyB := createKey()
	body, _ := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	chat := func(token string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := chat(keyA); code != http.StatusOK {
		t.Fatalf("keyA first = %d", code)
	}
	if code := chat(keyA); code != http.StatusTooManyRequests {
		t.Fatalf("keyA second = %d, want 429", code)
	}
	if code := chat(keyB); code != http.StatusOK {
		t.Fatalf("keyB should have its own rate bucket, got %d", code)
	}
}

func TestLLMEndpointAPIKeyMissingGroupIsForbidden(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	invalidateLLMRuntimeCaches(system)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-pro",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/llm/endpoint-api-keys", strings.NewReader(`{"service_group_id":"coding-pro"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	CreateLLMEndpointAPIKeyHandler(identity, system, nil).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{}); err != nil {
		t.Fatalf("clear registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)
	body, _ := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+created.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing group status = %d body=%s", rec.Code, rec.Body.String())
	}
}
