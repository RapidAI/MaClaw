package modelrouting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func TestLLMCallerUsesExperienceExtractionPolicyAndAudits(t *testing.T) {
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer provider.Close()
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var seenModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seenModel = body.Model
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": `{"experiences":[]}`}}},
		})
	}))
	defer server.Close()

	now := time.Now().Format(time.RFC3339)
	_, err = provider.Write.Exec(`INSERT INTO model_endpoints (id, tenant_id, name, protocol, base_url, api_key, model, cost_tier, priority, features, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "endpoint-default", "tenant-a", "Default", "openai", server.URL, "key-default", "default-model", "low", 1, "[]", "active", now, now)
	if err != nil {
		t.Fatalf("seed default endpoint: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO model_endpoints (id, tenant_id, name, protocol, base_url, api_key, model, cost_tier, priority, features, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "endpoint-experience", "tenant-a", "Experience", "openai", server.URL, "key-exp", "experience-model", "low", 0, "[]", "active", now, now)
	if err != nil {
		t.Fatalf("seed experience endpoint: %v", err)
	}
	_, err = provider.Write.Exec(`INSERT INTO model_routing_policies (id, tenant_id, name, description, work_type, role_code, endpoint_id, fallback_mode, priority, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "policy-exp", "tenant-a", "Experience extraction", "", ExperienceExtractionWorkType, "*", "endpoint-experience", "next_priority", 100, "active", now, now)
	if err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	auditRepo := audit.NewRepo(provider.Write, provider.Read)
	caller := NewLLMCaller(provider.Read, auditRepo)
	content, err := caller.Chat(context.Background(), "tenant-a", ExperienceExtractionWorkType, "", "system", "user")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if content != `{"experiences":[]}` {
		t.Fatalf("content = %q", content)
	}
	if seenModel != "experience-model" {
		t.Fatalf("seen model = %q, want experience-model", seenModel)
	}
	logs, err := auditRepo.ListRecent("tenant-a", 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(logs) == 0 || logs[0].WorkType != ExperienceExtractionWorkType || logs[0].Status != "ok" {
		t.Fatalf("audit logs = %+v", logs)
	}
}

func TestOpenAICompatContentSupportsTextParts(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":[{"type":"text","text":"hello"},{"type":"text","text":"world"}]}}]}`)
	content, err := openAICompatContent(body)
	if err != nil {
		t.Fatalf("openAICompatContent: %v", err)
	}
	if content != "hello\nworld" {
		t.Fatalf("content = %q", content)
	}
}
