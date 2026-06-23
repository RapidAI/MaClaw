package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

func TestNormalizeWorkflowDraftDescription(t *testing.T) {
	description, ok := normalizeWorkflowDraftDescription("  Purchase approval  ")
	if !ok || description != "Purchase approval" {
		t.Fatalf("description = %q ok=%v", description, ok)
	}
	if description, ok := normalizeWorkflowDraftDescription("   "); ok || description != "" {
		t.Fatalf("blank description = %q ok=%v", description, ok)
	}
	long := make([]byte, workflowDraftDescriptionMaxBytes+10)
	for i := range long {
		long[i] = 'a'
	}
	description, ok = normalizeWorkflowDraftDescription(string(long))
	if !ok || len(description) != workflowDraftDescriptionMaxBytes {
		t.Fatalf("truncated length = %d ok=%v", len(description), ok)
	}
	longUnicode := string(long[:workflowDraftDescriptionMaxBytes-1]) + "\u5ba1\u6279"
	description, ok = normalizeWorkflowDraftDescription(longUnicode)
	if !ok || !utf8.ValidString(description) || len(description) > workflowDraftDescriptionMaxBytes {
		t.Fatalf("unicode description length = %d valid=%v ok=%v", len(description), utf8.ValidString(description), ok)
	}
}

func TestWorkflowDraftLLMHandlerRejectsOversizedBody(t *testing.T) {
	authenticator := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-1": {TenantID: "tenant_default", UserID: "user-1", MachineID: "machine-1"},
		},
	}
	body := `{"description":"` + strings.Repeat("a", workflowDraftDescriptionMaxBytes*2) + `","language":"en"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(body))
	req.Header.Set("X-Machine-ID", "machine-1")
	req.Header.Set("Authorization", "Bearer machine-token")
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(authenticator, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "REQUEST_TOO_LARGE") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestWorkflowDraftLLMHandlerRequiresMachineAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(`{"description":"Purchase approval"}`))
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MACHINE_UNAUTHORIZED") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestWorkflowDraftLLMHandlerUsesTenantLLMServiceGroup(t *testing.T) {
	var providerCalled bool
	var requested map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("provider path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requested); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		content := `{"name":"LLM leave workflow","description":"from LLM","graph":{"nodes":[{"id":"n1","type":"trigger","label":"Start","position":{"x":80,"y":80},"config":{"trigger_type":"manual"}},{"id":"n2","type":"condition_branch","label":"Check leave days","position":{"x":300,"y":80},"config":{"branches":[{"label":"More than 3 days","condition":"days > 3","target_node_id":"n3"}],"default_branch":"n4"}},{"id":"n3","type":"approval","label":"HR approval","position":{"x":520,"y":20},"config":{"approver_ids":[],"mode":"single","min_approvals":1}},{"id":"n4","type":"terminal","label":"Complete","position":{"x":740,"y":80},"config":{"result_executors":[],"notifiers":[]}}],"edges":[{"id":"e1","source_id":"n1","target_id":"n2"},{"id":"e2","source_id":"n2","target_id":"n3"},{"id":"e3","source_id":"n2","target_id":"n4"},{"id":"e4","source_id":"n3","target_id":"n4"}]},"notes":["Generated dynamically by LLM"]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconv.Quote(content) + `}}]}`))
	}))
	defer upstream.Close()

	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := im.SaveLLMProviderRegistry(t.Context(), tenantSystem, &im.LLMProviderRegistry{
		Enabled:           true,
		CurrentProviderID: "provider-a",
		Providers: []im.LLMProvider{{
			ID:      "provider-a",
			Name:    "Provider A",
			APIURL:  upstream.URL + "/v1",
			APIKey:  "test-key",
			Model:   "gpt-test",
			WireAPI: "chat",
		}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := llmservice.SaveRegistry(t.Context(), tenantSystem, &llmservice.Registry{
		SystemDefaultServiceGroupID: "draft-group",
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "draft-group",
			Name:         "Draft Group",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	authenticator := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-1": {TenantID: "tenant-a", UserID: "designer@example.com", MachineID: "machine-1"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(`{"description":"Employee leave longer than 3 days requires HR approval","language":"en"}`))
	req.Header.Set("X-Machine-ID", "machine-1")
	req.Header.Set("Authorization", "Bearer machine-token")
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(authenticator, settings, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !providerCalled {
		t.Fatal("expected workflow draft generation to call configured LLM provider")
	}
	if requested["model"] != "gpt-test" {
		t.Fatalf("requested model = %#v", requested["model"])
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["name"] != "LLM leave workflow" {
		t.Fatalf("name = %#v body=%s", out["name"], rec.Body.String())
	}
	if out["generated_by"] != "llm" {
		t.Fatalf("generated_by = %#v body=%s", out["generated_by"], rec.Body.String())
	}
	graph := out["graph"].(map[string]any)
	nodes := graph["nodes"].([]any)
	if len(nodes) != 4 || nodes[1].(map[string]any)["type"] != "condition_branch" {
		t.Fatalf("LLM graph not applied: %#v", graph["nodes"])
	}
}

func TestWorkflowDraftLLMHandlerRequiresSystemDefaultLLMServiceGroup(t *testing.T) {
	var providerCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := im.SaveLLMProviderRegistry(t.Context(), tenantSystem, &im.LLMProviderRegistry{
		Enabled:           true,
		CurrentProviderID: "provider-a",
		Providers: []im.LLMProvider{{
			ID:      "provider-a",
			Name:    "Provider A",
			APIURL:  upstream.URL + "/v1",
			APIKey:  "test-key",
			Model:   "gpt-test",
			WireAPI: "chat",
		}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := llmservice.SaveRegistry(t.Context(), tenantSystem, &llmservice.Registry{
		GlobalServiceGroupIDs: []string{"draft-group"},
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "draft-group",
			Name:         "Draft Group",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	authenticator := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-1": {TenantID: "tenant-a", UserID: "designer@example.com", MachineID: "machine-1"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(`{"description":"Employee leave approval","language":"en"}`))
	req.Header.Set("X-Machine-ID", "machine-1")
	req.Header.Set("Authorization", "Bearer machine-token")
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(authenticator, settings, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if providerCalled {
		t.Fatal("provider should not be called without system_default_service_group_id")
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["generated_by"] != "fallback" {
		t.Fatalf("generated_by = %#v body=%s", out["generated_by"], rec.Body.String())
	}
}

func TestWorkflowDraftLLMHandlerFallsBackWhenSystemDefaultServiceGroupHasNoModels(t *testing.T) {
	var providerCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := im.SaveLLMProviderRegistry(t.Context(), tenantSystem, &im.LLMProviderRegistry{
		Enabled:           true,
		CurrentProviderID: "provider-a",
		Providers: []im.LLMProvider{{
			ID:      "provider-a",
			Name:    "Provider A",
			APIURL:  upstream.URL + "/v1",
			APIKey:  "test-key",
			Model:   "gpt-test",
			WireAPI: "chat",
		}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := llmservice.SaveRegistry(t.Context(), tenantSystem, &llmservice.Registry{
		SystemDefaultServiceGroupID: "draft-group",
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "draft-group",
			Name:         "Draft Group",
			AccessPolicy: llmservice.AccessPolicyFree,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	authenticator := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-1": {TenantID: "tenant-a", UserID: "designer@example.com", MachineID: "machine-1"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(`{"description":"Employee leave approval","language":"en"}`))
	req.Header.Set("X-Machine-ID", "machine-1")
	req.Header.Set("Authorization", "Bearer machine-token")
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(authenticator, settings, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if providerCalled {
		t.Fatal("provider should not be called when system default service group has no models")
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["generated_by"] != "fallback" {
		t.Fatalf("generated_by = %#v body=%s", out["generated_by"], rec.Body.String())
	}
}

func TestWorkflowDraftLLMHandlerFallsBackWhenSystemDefaultProviderIsMissing(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := im.SaveLLMProviderRegistry(t.Context(), tenantSystem, &im.LLMProviderRegistry{
		Enabled: true,
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := llmservice.SaveRegistry(t.Context(), tenantSystem, &llmservice.Registry{
		SystemDefaultServiceGroupID: "draft-group",
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "draft-group",
			Name:         "Draft Group",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"missing-provider"}}},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	authenticator := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-1": {TenantID: "tenant-a", UserID: "designer@example.com", MachineID: "machine-1"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(`{"description":"Employee leave approval","language":"en"}`))
	req.Header.Set("X-Machine-ID", "machine-1")
	req.Header.Set("Authorization", "Bearer machine-token")
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(authenticator, settings, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["generated_by"] != "fallback" {
		t.Fatalf("generated_by = %#v body=%s", out["generated_by"], rec.Body.String())
	}
}

func TestWorkflowDraftLLMResponseTextExtractsArrayContent(t *testing.T) {
	body := []byte(`{"content":[{"type":"output_text","text":"` + strings.ReplaceAll(workflowDraftMinimalLLMJSON("Array content draft"), `"`, `\"`) + `"}]}`)

	text, err := workflowDraftLLMResponseText(body)
	if err != nil {
		t.Fatalf("workflowDraftLLMResponseText: %v", err)
	}
	draft, err := parseWorkflowDraftLLMResponse(text)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	if draft["name"] != "Array content draft" {
		t.Fatalf("name = %#v", draft["name"])
	}
}

func TestWorkflowDraftLLMResponseTextExtractsResponsesAPIOutput(t *testing.T) {
	body := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"` + strings.ReplaceAll(workflowDraftMinimalLLMJSON("Responses output draft"), `"`, `\"`) + `"}]}]}`)

	text, err := workflowDraftLLMResponseText(body)
	if err != nil {
		t.Fatalf("workflowDraftLLMResponseText: %v", err)
	}
	draft, err := parseWorkflowDraftLLMResponse(text)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	if draft["name"] != "Responses output draft" {
		t.Fatalf("name = %#v", draft["name"])
	}
}

func TestParseWorkflowDraftLLMResponseNormalizesUnsafeGraph(t *testing.T) {
	content := `{
		"name": "Unsafe graph",
		"graph": {
			"nodes": [
				{"id": "start", "type": "trigger", "label": "", "position": {"x": -10, "y": 20}, "config": {}},
				{"id": "bad", "type": "unknown", "label": "Bad", "position": {"x": 1, "y": 1}, "config": {}},
				{"id": "approve", "type": "approval", "label": "Manager", "position": {"x": "300", "y": "80"}, "config": {}},
				{"id": "approve", "type": "terminal", "label": "Done", "position": {"x": 520, "y": 80}, "config": {}}
			],
			"edges": [
				{"id": "e1", "source_id": "start", "target_id": "approve"},
				{"id": "e1", "source_id": "start", "target_id": "missing"},
				{"id": "e2", "source_id": "approve", "target_id": "approve"}
			]
		}
	}`

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	if len(nodes) != 3 {
		t.Fatalf("nodes = %#v", nodes)
	}
	if nodes[0]["label"] != "Start" {
		t.Fatalf("default trigger label = %#v", nodes[0]["label"])
	}
	if nodes[0]["position"].(map[string]any)["x"] != 0 {
		t.Fatalf("negative x was not clamped: %#v", nodes[0]["position"])
	}
	approvalConfig := nodes[1]["config"].(map[string]any)
	if approvalConfig["mode"] != "single" || approvalConfig["timeout_hours"] != 24 {
		t.Fatalf("approval defaults = %#v", approvalConfig)
	}
	if nodes[2]["id"] != "approve_2" {
		t.Fatalf("duplicate node id not made unique: %#v", nodes[2]["id"])
	}
	edges := graph["edges"].([]map[string]any)
	if len(edges) != 2 || edges[0]["source_id"] != "start" || edges[0]["target_id"] != "approve" || edges[1]["target_id"] != "approve_2" {
		t.Fatalf("edges = %#v", edges)
	}
}

func TestParseWorkflowDraftLLMResponseSanitizesGeneratedIDs(t *testing.T) {
	content := `{
		"name": "Unsafe IDs",
		"graph": {
			"nodes": [
				{"id": "Start Node!", "type": "trigger", "label": "Start", "position": {"x": 80, "y": 80}, "config": {}},
				{"id": "Condition: leave", "type": "condition_branch", "label": "Check", "position": {"x": 300, "y": 80}, "config": {"branches": [{"target_node_id": "HR approval/1", "expression": {"field": "days", "operator": "greater_than", "value": 3}}], "default_branch": "Done#node"}},
				{"id": "HR approval/1", "type": "approval", "label": "HR", "position": {"x": 520, "y": 20}, "config": {}},
				{"id": "Done#node", "type": "terminal", "label": "Done", "position": {"x": 740, "y": 80}, "config": {}}
			],
			"edges": [
				{"id": "edge one", "source_id": "Start Node!", "target_id": "Condition: leave"},
				{"id": "edge/two", "source_id": "Condition: leave", "target_id": "HR approval/1"},
				{"id": "edge three", "source_id": "Condition: leave", "target_id": "Done#node"}
			]
		}
	}`

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	if nodes[0]["id"] != "Start_Node" || nodes[1]["id"] != "Condition_leave" || nodes[2]["id"] != "HR_approval_1" || nodes[3]["id"] != "Done_node" {
		t.Fatalf("sanitized node ids = %#v", nodes)
	}
	conditionConfig := nodes[1]["config"].(map[string]any)
	branches := conditionConfig["branches"].([]any)
	if branches[0].(map[string]any)["target_node_id"] != "HR_approval_1" || conditionConfig["default_branch"] != "Done_node" {
		t.Fatalf("condition targets were not remapped: %#v", conditionConfig)
	}
	edges := graph["edges"].([]map[string]any)
	if edges[0]["source_id"] != "Start_Node" || edges[0]["target_id"] != "Condition_leave" || edges[1]["target_id"] != "HR_approval_1" {
		t.Fatalf("edges were not remapped: %#v", edges)
	}
}

func TestParseWorkflowDraftLLMResponseExtractsFencedJSON(t *testing.T) {
	content := "```JSON\n" + workflowDraftMinimalLLMJSON("Fenced draft") + "\n```"

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	if draft["name"] != "Fenced draft" {
		t.Fatalf("name = %#v", draft["name"])
	}
}

func TestParseWorkflowDraftLLMResponseExtractsJSONWithExplanation(t *testing.T) {
	content := "Here is the generated draft:\n" + workflowDraftMinimalLLMJSON("Explained draft") + "\nReview the approvers before saving."

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	if draft["name"] != "Explained draft" {
		t.Fatalf("name = %#v", draft["name"])
	}
}

func TestParseWorkflowDraftLLMResponseProducesSaveableEntryGraph(t *testing.T) {
	content := `{
		"name": "Messy graph",
		"graph": {
			"nodes": [
				{"id": "a", "type": "approval", "label": "Manager", "position": {"x": 300, "y": 80}, "config": {}},
				{"id": "b", "type": "trigger", "label": "Start", "position": {"x": 80, "y": 80}, "config": {}},
				{"id": "c", "type": "trigger", "label": "Duplicate start", "position": {"x": 520, "y": 80}, "config": {}},
				{"id": "d", "type": "terminal", "label": "Done", "position": {"x": 740, "y": 80}, "config": {}}
			],
			"edges": [
				{"id": "e1", "source_id": "a", "target_id": "b"}
			]
		}
	}`

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	triggerCount := 0
	for _, node := range nodes {
		if node["type"] == "trigger" {
			triggerCount++
		}
	}
	if triggerCount != 1 {
		t.Fatalf("trigger count = %d nodes=%#v", triggerCount, nodes)
	}
	if nodes[2]["type"] == "trigger" {
		t.Fatalf("duplicate trigger was not normalized: %#v", nodes[2])
	}
	edges := graph["edges"].([]map[string]any)
	for _, edge := range edges {
		if edge["target_id"] == "b" {
			t.Fatalf("edge targets trigger: %#v", edges)
		}
	}
	reachable := workflowDraftReachableIDs("b", edges)
	for _, node := range nodes {
		if !reachable[node["id"].(string)] {
			t.Fatalf("node %s is not reachable via %#v", node["id"], edges)
		}
	}
	wfGraph := workflowDraftGraphForTest(t, graph)
	if err := workflow.ValidateGraphStructure(wfGraph); err != nil {
		t.Fatalf("normalized graph is not saveable: %v graph=%#v", err, wfGraph)
	}
}

func workflowDraftMinimalLLMJSON(name string) string {
	body, _ := json.Marshal(map[string]any{
		"name": name,
		"graph": map[string]any{
			"nodes": []map[string]any{
				{"id": "start", "type": "trigger", "label": "Start", "position": map[string]any{"x": 80, "y": 80}, "config": map[string]any{}},
				{"id": "done", "type": "terminal", "label": "Done", "position": map[string]any{"x": 300, "y": 80}, "config": map[string]any{}},
			},
			"edges": []map[string]any{
				{"id": "e1", "source_id": "start", "target_id": "done"},
			},
		},
	})
	return string(body)
}

func TestParseWorkflowDraftLLMResponseDropsTerminalOutgoingEdges(t *testing.T) {
	content := `{
		"name": "Terminal in the middle",
		"graph": {
			"nodes": [
				{"id": "start", "type": "trigger", "label": "Start", "position": {"x": 80, "y": 80}, "config": {}},
				{"id": "done", "type": "terminal", "label": "Done", "position": {"x": 300, "y": 80}, "config": {}},
				{"id": "approval", "type": "approval", "label": "Late approval", "position": {"x": 520, "y": 80}, "config": {}}
			],
			"edges": [
				{"id": "e1", "source_id": "start", "target_id": "done"},
				{"id": "e2", "source_id": "done", "target_id": "approval"}
			]
		}
	}`

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	graph := draft["graph"].(map[string]any)
	edges := graph["edges"].([]map[string]any)
	for _, edge := range edges {
		if edge["source_id"] == "done" {
			t.Fatalf("terminal outgoing edge was kept: %#v", edges)
		}
	}
	wfGraph := workflowDraftGraphForTest(t, graph)
	if err := workflow.ValidateGraphStructure(wfGraph); err != nil {
		t.Fatalf("normalized graph is not saveable: %v graph=%#v", err, wfGraph)
	}
}

func TestParseWorkflowDraftLLMResponseRepairsConditionRoutesFromEdges(t *testing.T) {
	content := `{
		"name": "Condition graph",
		"graph": {
			"nodes": [
				{"id": "start", "type": "trigger", "label": "Start", "position": {"x": 80, "y": 80}, "config": {}},
				{"id": "cond", "type": "condition_branch", "label": "Check amount", "position": {"x": 300, "y": 80}, "config": {"branches": []}},
				{"id": "finance", "type": "approval", "label": "Finance", "position": {"x": 520, "y": 20}, "config": {}},
				{"id": "done", "type": "terminal", "label": "Done", "position": {"x": 740, "y": 80}, "config": {}}
			],
			"edges": [
				{"id": "e1", "source_id": "start", "target_id": "cond"},
				{"id": "e2", "source_id": "cond", "target_id": "finance"},
				{"id": "e3", "source_id": "cond", "target_id": "done"},
				{"id": "e4", "source_id": "finance", "target_id": "done"}
			]
		}
	}`

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	config := nodes[1]["config"].(map[string]any)
	branches := config["branches"].([]any)
	if len(branches) != 1 || branches[0].(map[string]any)["target_node_id"] != "finance" || config["default_branch"] != "done" {
		t.Fatalf("condition routes = branches:%#v default:%#v", branches, config["default_branch"])
	}
	expression := branches[0].(map[string]any)["expression"].(map[string]any)
	if expression["operator"] != "equals" {
		t.Fatalf("expression operator = %#v", expression["operator"])
	}
	if err := validateNormalizedWorkflowDraftGraph(graph); err != nil {
		t.Fatalf("condition graph is not valid: %v graph=%#v", err, graph)
	}
}

func TestParseWorkflowDraftLLMResponseNormalizesConditionOperators(t *testing.T) {
	content := `{
		"name": "Operator graph",
		"graph": {
			"nodes": [
				{"id": "start", "type": "trigger", "label": "Start", "position": {"x": 80, "y": 80}, "config": {}},
				{"id": "cond", "type": "condition_branch", "label": "Check days", "position": {"x": 300, "y": 80}, "config": {"branches": [{"target_node_id": "hr", "expression": {"field": "days", "operator": ">", "value": 3}}], "default_branch": "done"}},
				{"id": "hr", "type": "approval", "label": "HR", "position": {"x": 520, "y": 20}, "config": {}},
				{"id": "done", "type": "terminal", "label": "Done", "position": {"x": 740, "y": 80}, "config": {}}
			],
			"edges": [
				{"id": "e1", "source_id": "start", "target_id": "cond"},
				{"id": "e2", "source_id": "cond", "target_id": "hr"},
				{"id": "e3", "source_id": "cond", "target_id": "done"},
				{"id": "e4", "source_id": "hr", "target_id": "done"}
			]
		}
	}`

	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		t.Fatalf("parseWorkflowDraftLLMResponse: %v", err)
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	config := nodes[1]["config"].(map[string]any)
	branch := config["branches"].([]any)[0].(map[string]any)
	expression := branch["expression"].(map[string]any)
	if expression["operator"] != "greater_than" {
		t.Fatalf("operator = %#v", expression["operator"])
	}
	if err := validateNormalizedWorkflowDraftGraph(graph); err != nil {
		t.Fatalf("operator graph is not valid: %v graph=%#v", err, graph)
	}
}

func TestValidateNormalizedWorkflowDraftGraphRejectsTerminalOutgoing(t *testing.T) {
	graph := map[string]any{
		"nodes": []map[string]any{
			{"id": "start", "type": "trigger", "label": "Start", "position": map[string]any{"x": 80, "y": 80}, "config": map[string]any{}},
			{"id": "done", "type": "terminal", "label": "Done", "position": map[string]any{"x": 300, "y": 80}, "config": map[string]any{}},
			{"id": "notify", "type": "notification", "label": "Notify", "position": map[string]any{"x": 520, "y": 80}, "config": map[string]any{}},
		},
		"edges": []map[string]any{
			{"id": "e1", "source_id": "start", "target_id": "done"},
			{"id": "e2", "source_id": "done", "target_id": "notify"},
		},
	}
	if err := validateNormalizedWorkflowDraftGraph(graph); err == nil || !strings.Contains(err.Error(), "terminal node") {
		t.Fatalf("expected terminal outgoing validation error, got %v", err)
	}
}

func TestBuildFallbackWorkflowDraftReturnsEditorGraph(t *testing.T) {
	draft := buildFallbackWorkflowDraft("Purchase approval", "en")
	if draft["name"] != "Approval workflow draft" {
		t.Fatalf("name = %q", draft["name"])
	}
	if draft["description"] != "Purchase approval" {
		t.Fatalf("description = %q", draft["description"])
	}
	graph, ok := draft["graph"].(map[string]any)
	if !ok {
		t.Fatalf("graph = %#v", draft["graph"])
	}
	nodes, ok := graph["nodes"].([]map[string]any)
	if !ok || len(nodes) != 4 {
		t.Fatalf("nodes = %#v", graph["nodes"])
	}
	edges, ok := graph["edges"].([]map[string]any)
	if !ok || len(edges) != 3 {
		t.Fatalf("edges = %#v", graph["edges"])
	}
	wantTypes := []string{"trigger", "form", "approval", "terminal"}
	for i, want := range wantTypes {
		if nodes[i]["type"] != want {
			t.Fatalf("node %d type = %q, want %q", i, nodes[i]["type"], want)
		}
	}
	triggerConfig, ok := nodes[0]["config"].(map[string]any)
	if !ok || triggerConfig["description"] != "Purchase approval" {
		t.Fatalf("trigger config = %#v", nodes[0]["config"])
	}
	if err := validateNormalizedWorkflowDraftGraph(graph); err != nil {
		t.Fatalf("fallback graph is not saveable: %v graph=%#v", err, graph)
	}
}

func workflowDraftGraphForTest(t *testing.T, graph map[string]any) workflow.WorkflowGraph {
	t.Helper()
	body, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("marshal graph: %v", err)
	}
	var wfGraph workflow.WorkflowGraph
	if err := json.Unmarshal(body, &wfGraph); err != nil {
		t.Fatalf("unmarshal workflow graph: %v body=%s", err, string(body))
	}
	return wfGraph
}

func TestBuildFallbackWorkflowDraftLocalizesChineseLabels(t *testing.T) {
	draft := buildFallbackWorkflowDraft("\u91c7\u8d2d\u5ba1\u6279", "zh-Hans")
	if draft["name"] != "\u5ba1\u6279\u6d41\u7a0b\u8349\u7a3f" {
		t.Fatalf("name = %q", draft["name"])
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	if nodes[0]["label"] != "\u5f00\u59cb" || nodes[2]["label"] != "\u5ba1\u6279" {
		t.Fatalf("localized labels = %#v %#v", nodes[0]["label"], nodes[2]["label"])
	}
}

func TestBuildFallbackWorkflowDraftCreatesConditionBranchForConditionalLanguage(t *testing.T) {
	draft := buildFallbackWorkflowDraft("\u5458\u5de5\u63d0\u4ea4\u8bf7\u5047\u7533\u8bf7\uff0c\u4e3b\u7ba1\u5148\u5ba1\u6279\u3002\u5982\u679c\u8bf7\u5047\u8d85\u8fc7 3 \u5929\uff0c\u518d\u7531 HR \u5ba1\u6279\u3002", "zh-Hans")
	graph := draft["graph"].(map[string]any)
	nodes, ok := graph["nodes"].([]map[string]any)
	if !ok || len(nodes) != 6 {
		t.Fatalf("nodes = %#v", graph["nodes"])
	}
	edges, ok := graph["edges"].([]map[string]any)
	if !ok || len(edges) != 6 {
		t.Fatalf("edges = %#v", graph["edges"])
	}
	if nodes[3]["type"] != "condition_branch" {
		t.Fatalf("condition node type = %q", nodes[3]["type"])
	}
	if nodes[4]["type"] != "approval" || nodes[4]["label"] != "HR \u5ba1\u6279" {
		t.Fatalf("conditional approval = %#v", nodes[4])
	}
	config, ok := nodes[3]["config"].(map[string]any)
	if !ok || config["default_branch"] != "node6" {
		t.Fatalf("condition config = %#v", nodes[3]["config"])
	}
	branches, ok := config["branches"].([]map[string]any)
	if !ok || len(branches) != 1 || branches[0]["target_node_id"] != "node5" {
		t.Fatalf("branches = %#v", config["branches"])
	}
	expression, ok := branches[0]["expression"].(map[string]any)
	if !ok || expression["field"] != "days" || expression["operator"] != "greater_than" || expression["value"] != 3 {
		t.Fatalf("expression = %#v", branches[0]["expression"])
	}
	if err := validateNormalizedWorkflowDraftGraph(graph); err != nil {
		t.Fatalf("conditional fallback graph is not saveable: %v graph=%#v", err, graph)
	}
}
