package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestAdminPatchMemberUpstreamOverridesDryRunAndClear(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.SaveRegistry(t.Context(), &llmservice.Registry{
		Providers: []llmpool.ProviderConfig{{
			ID: "a", Name: "A", APIURL: "https://a.example/v1", APIKey: "secret-a",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	h := adminPatchLLMProviderMember(svc, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"extra_body":{"reasoning_effort":"none"},"drop_fields":["thinking"],"dry_run":true}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"dry_run":true`) || !strings.Contains(rec.Body.String(), `"reasoning_effort":"none"`) {
		t.Fatalf("dry-run status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err := svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].ExtraBody != nil || reg.Providers[0].DropFields != nil {
		t.Fatalf("dry-run wrote %#v", reg.Providers[0])
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"extra_body":{"reasoning_effort":"none"},"extra_body_priority":"downstream","drop_fields":["thinking"]}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err = svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].ExtraBody["reasoning_effort"] != "none" || reg.Providers[0].ExtraBodyPriority != "downstream" || len(reg.Providers[0].DropFields) != 1 {
		t.Fatalf("saved %#v priority %q drop %#v", reg.Providers[0].ExtraBody, reg.Providers[0].ExtraBodyPriority, reg.Providers[0].DropFields)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"extra_body":{},"drop_fields":[]}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	reg, err = svc.LoadRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].ExtraBody != nil || reg.Providers[0].DropFields != nil || reg.Providers[0].ExtraBodyPriority != "downstream" {
		t.Fatalf("clear result extra %#v drop %#v priority %q", reg.Providers[0].ExtraBody, reg.Providers[0].DropFields, reg.Providers[0].ExtraBodyPriority)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"extra_body":["nope"]}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-object status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/a", strings.NewReader(`{"extra_body_priority":"sideways"}`))
	req.SetPathValue("id", "a")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("priority status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInjectProviderTestToolsHonorsMemberDrop(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":16,"reasoning_effort":"none"}`)
	tools := []map[string]interface{}{{"name": "ping"}}
	choice := map[string]interface{}{"type": "auto"}
	dropped := injectProviderTestTools(body, tools, choice, true, []string{"tools"})
	if string(dropped) != string(body) {
		t.Fatalf("dropped tools changed the probe body: %s", dropped)
	}
	kept := injectProviderTestTools(body, tools, choice, true, nil)
	if !strings.Contains(string(kept), `"name":"ping"`) || !strings.Contains(string(kept), `"tool_choice"`) || !strings.Contains(string(kept), `"reasoning_effort":"none"`) {
		t.Fatalf("probe tools or member field missing: %s", kept)
	}
	choiceDropped := injectProviderTestTools(body, tools, choice, true, []string{"tool_choice"})
	if !strings.Contains(string(choiceDropped), `"name":"ping"`) || strings.Contains(string(choiceDropped), `"tool_choice"`) {
		t.Fatalf("tool_choice drop = %s", choiceDropped)
	}
}
