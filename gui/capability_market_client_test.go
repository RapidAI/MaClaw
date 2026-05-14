package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCapabilityMarketClientCreateInstallIntent(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody HubCapabilityInstallIntent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(HubCapabilityInstallIntentResult{Action: "create_purchase_request", RequestID: "req_1"})
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "viewer-token", http: server.Client()}
	resp, err := client.createInstallIntent(context.Background(), HubCapabilityInstallIntent{
		CapabilityID:   "paid/mcp",
		CapabilityType: "mcp",
		Source:         "hubcenter",
		Pricing:        "paid",
		Price:          map[string]any{"amount_cents": float64(9900)},
		License:        map[string]any{"seats": float64(5)},
	})
	if err != nil {
		t.Fatalf("createInstallIntent: %v", err)
	}
	if resp.Action != "create_purchase_request" || resp.RequestID != "req_1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if gotPath != "/api/capabilities/paid%2Fmcp/install-intent" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody.CapabilityID != "paid/mcp" || gotBody.Pricing != "paid" || gotBody.Price["amount_cents"] != float64(9900) {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
}

func TestCapabilityMarketClientSaveMCPHubSecret(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody HubMCPHubSecretInput
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(HubMCPHubSecret{ID: "hub_secret_1", MCPServerID: gotBody.MCPServerID, RequirementName: gotBody.RequirementName, SecretDigest: "digest"})
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "viewer-token", http: server.Client()}
	resp, err := client.saveMCPHubSecret(context.Background(), HubMCPHubSecretInput{MCPServerID: "billing", RequirementName: "api_token", SecretValue: "secret"})
	if err != nil {
		t.Fatalf("saveMCPHubSecret: %v", err)
	}
	if resp.SecretDigest != "digest" || gotPath != "/api/capabilities/mcp-hub-secrets" || gotAuth != "Bearer viewer-token" {
		t.Fatalf("resp=%+v path=%q auth=%q", resp, gotPath, gotAuth)
	}
	if gotBody.SecretValue != "secret" || gotBody.MCPServerID != "billing" || gotBody.RequirementName != "api_token" {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
}

func TestCapabilityMarketClientListCapabilitiesFiltersQuery(t *testing.T) {
	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []HubCapabilitySummary{
			{ID: "cap-1", CapabilityID: "billing-mcp", CapabilityType: "mcp", DisplayName: "Billing MCP"},
			{ID: "cap-2", CapabilityID: "crm-mcp", CapabilityType: "mcp", DisplayName: "CRM MCP"},
		}})
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "viewer-token", http: server.Client()}
	items, err := client.listCapabilities(context.Background(), "mcp", "billing")
	if err != nil {
		t.Fatalf("listCapabilities: %v", err)
	}
	if gotPath != "/api/capabilities?type=mcp" || gotAuth != "Bearer viewer-token" {
		t.Fatalf("path=%q auth=%q", gotPath, gotAuth)
	}
	if len(items) != 1 || items[0].CapabilityID != "billing-mcp" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestListHubCenterMCPCapabilitiesMapsExternalItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/capability-market/mcp?q=jira" {
			t.Fatalf("path = %q", r.URL.RequestURI())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{
			"id":           "jira-mcp",
			"display_name": "Jira MCP",
			"description":  "Issue tracker tools",
			"version_key":  "1.2.0",
			"pricing":      map[string]any{"mode": "paid", "credits": float64(10)},
			"license":      map[string]any{"seats": float64(5)},
		}}})
	}))
	defer server.Close()

	items, err := listHubCenterMCPCapabilities(context.Background(), server.Client(), server.URL, "jira")
	if err != nil {
		t.Fatalf("listHubCenterMCPCapabilities: %v", err)
	}
	if len(items) != 1 || !items[0].External || items[0].Source != "hubcenter" || items[0].Status != "external" || items[0].CurrentVersionKey != "1.2.0" {
		t.Fatalf("unexpected items: %+v", items)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(items[0].MetadataJSON), &meta); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	pricing, _ := meta["pricing"].(map[string]any)
	if pricing["mode"] != "paid" {
		t.Fatalf("metadata pricing = %#v", meta["pricing"])
	}
}
func TestCapabilityPricingModeFromMetadataReadsObjectMode(t *testing.T) {
	mode := capabilityPricingModeFromMetadata(map[string]any{"pricing": map[string]any{"mode": corelib.CapabilityPricingPaid}})
	if mode != corelib.CapabilityPricingPaid {
		t.Fatalf("mode = %q, want %q", mode, corelib.CapabilityPricingPaid)
	}
}

func TestCapabilityPricingModeFromMetadataDefaultsFree(t *testing.T) {
	mode := capabilityPricingModeFromMetadata(map[string]any{})
	if mode != corelib.CapabilityPricingFree {
		t.Fatalf("mode = %q, want %q", mode, corelib.CapabilityPricingFree)
	}
}
func TestCapabilityMarketClientListMCPSecretBindings(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []HubMCPSecretBinding{{MCPServerID: "billing", RequirementName: "api_token", Storage: "local", LocalSecretRef: "mcp:billing:api_token", Status: "configured"}}})
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "viewer-token", http: server.Client()}
	items, err := client.listMCPSecretBindings(context.Background(), "billing")
	if err != nil {
		t.Fatalf("listMCPSecretBindings: %v", err)
	}
	if gotPath != "/api/capabilities/mcp-secret-bindings?mcp_server_id=billing" {
		t.Fatalf("path=%q", gotPath)
	}
	if len(items) != 1 || items[0].Storage != "local" || items[0].Status != "configured" {
		t.Fatalf("unexpected bindings: %+v", items)
	}
}

func TestMCPSecretRequirementsNeedUserConfigHonorsExistingBindings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capabilities/mcp-secret-bindings":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []HubMCPSecretBinding{{MCPServerID: "billing", RequirementName: "api_token", Storage: "local", LocalSecretRef: "mcp:billing:api_token", Status: "configured"}}})
		case "/api/capabilities/mcp-hub-secrets":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []HubMCPHubSecret{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "viewer-token", http: server.Client()}
	needsConfig := mcpSecretRequirementsNeedUserConfig(context.Background(), client, corelib.MCPServerEntry{ID: "billing"}, []HubMCPSecretRequirement{{Name: "api_token", StoragePolicy: "local", Required: true}})
	if needsConfig {
		t.Fatal("expected configured local binding to satisfy required secret")
	}
}

func TestMCPSecretRequirementsNeedUserConfigRequiresMissingHubSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "viewer-token", http: server.Client()}
	needsConfig := mcpSecretRequirementsNeedUserConfig(context.Background(), client, corelib.MCPServerEntry{ID: "billing", AuthSecret: "local-token"}, []HubMCPSecretRequirement{{Name: "api_token", StoragePolicy: "hub", Required: true}})
	if !needsConfig {
		t.Fatal("expected hub-only required secret to need config when only local auth exists")
	}
}
