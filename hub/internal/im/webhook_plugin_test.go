package im

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookIMPluginOutboundUsesTenantScopedConfig(t *testing.T) {
	type capturedRequest struct {
		TenantID string
		Body     webhookOutPayload
	}
	var captured []capturedRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload webhookOutPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal outbound payload: %v", err)
		}
		captured = append(captured, capturedRequest{TenantID: r.Header.Get("X-Tenant-ID"), Body: payload})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var seen []string
	plugin := NewWebhookIMPlugin("openclaw", func(ctx context.Context) WebhookConfig {
		tenantID := webhookTenantIDFromContext(ctx)
		seen = append(seen, tenantID)
		return WebhookConfig{TenantID: tenantID, WebhookURL: server.URL, Secret: "secret"}
	})
	plugin.SetHTTPClient(server.Client())

	ctx := WithTenant(context.Background(), "tenant_a")
	if err := plugin.SendText(ctx, UserTarget{PlatformUID: "p1"}, "hello"); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	if len(seen) != 1 || seen[0] != "tenant_a" {
		t.Fatalf("expected config provider to see tenant_a, got %#v", seen)
	}
	if len(captured) != 1 {
		t.Fatalf("expected one outbound request, got %d", len(captured))
	}
	if captured[0].TenantID != "tenant_a" {
		t.Fatalf("expected X-Tenant-ID tenant_a, got %q", captured[0].TenantID)
	}
	if captured[0].Body.TenantID != "tenant_a" {
		t.Fatalf("expected payload tenant_a, got %q", captured[0].Body.TenantID)
	}
}

func TestWebhookIMPluginOutboundOmitsTenantForLegacyContext(t *testing.T) {
	var capturedHeader string
	var capturedPayload webhookOutPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Tenant-ID")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedPayload); err != nil {
			t.Fatalf("unmarshal outbound payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	plugin := NewWebhookIMPlugin("openclaw", func(ctx context.Context) WebhookConfig {
		tenantID := webhookTenantIDFromContext(ctx)
		return WebhookConfig{TenantID: tenantID, WebhookURL: server.URL, Secret: "secret"}
	})
	plugin.SetHTTPClient(server.Client())

	if err := plugin.SendText(context.Background(), UserTarget{PlatformUID: "legacy"}, "hello"); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	if capturedHeader != "" {
		t.Fatalf("expected legacy outbound request without tenant header, got %q", capturedHeader)
	}
	if capturedPayload.TenantID != "" {
		t.Fatalf("expected legacy outbound payload without tenant_id, got %q", capturedPayload.TenantID)
	}
}
