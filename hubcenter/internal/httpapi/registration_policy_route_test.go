package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestUpdateHubRegistrationPolicyBodyHubIDHandlesOpaqueHubID(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	now := time.Now().UTC()
	hubID := "https://hub.example.com/root"
	if err := svc.store.Hubs.Create(context.Background(), &store.HubInstance{
		ID:                     hubID,
		InstallationID:         "opaque-hub-id",
		OwnerEmail:             "owner@example.com",
		Name:                   "Opaque Hub",
		BaseURL:                "https://hub.example.com/root",
		Host:                   "hub.example.com",
		Port:                   443,
		Visibility:             "shared",
		EnrollmentMode:         "open",
		Status:                 "online",
		CapabilitiesJSON:       "{}",
		RegistrationPolicyJSON: "{}",
		HubSecretHash:          "hash",
		LastSeenAt:             &now,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatalf("create opaque hub: %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/registration-policy", map[string]any{
		"hub_id":               hubID,
		"hub_origin":           "official",
		"default_signup_scope": "public",
		"tenant": map[string]any{
			"tenant_id":          "tenant_default",
			"signup_scope":       "inherit",
			"is_public_fallback": false,
			"invite_enabled":     true,
		},
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("body hub id registration policy status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"tenant_default"`)) {
		t.Fatalf("default tenant should be externalized, body=%s", resp.Body.String())
	}

	legacyPathResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+url.PathEscape(hubID)+"/registration-policy", map[string]any{
		"hub_origin":           "official",
		"default_signup_scope": "public",
		"tenant": map[string]any{
			"tenant_id":          "tenant_default",
			"signup_scope":       "inherit",
			"is_public_fallback": false,
		},
	}, token)
	if legacyPathResp.Code != http.StatusOK {
		t.Fatalf("legacy escaped path hub id registration policy status=%d body=%s", legacyPathResp.Code, legacyPathResp.Body.String())
	}

	rawSlashPathResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+hubID+"/registration-policy", map[string]any{
		"hub_origin":           "official",
		"default_signup_scope": "public",
		"tenant": map[string]any{
			"tenant_id":    "tenant_default",
			"signup_scope": "inherit",
		},
	}, token)
	if rawSlashPathResp.Code != http.StatusOK {
		t.Fatalf("raw slash path hub id registration policy status=%d body=%s", rawSlashPathResp.Code, rawSlashPathResp.Body.String())
	}

	visibilityResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/visibility", map[string]any{
		"hub_id":     hubID,
		"visibility": "private",
	}, token)
	if visibilityResp.Code != http.StatusOK {
		t.Fatalf("body hub id visibility status=%d body=%s", visibilityResp.Code, visibilityResp.Body.String())
	}

	disableResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/disable", map[string]any{
		"hub_id": hubID,
		"reason": "maintenance",
	}, token)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("body hub id disable status=%d body=%s", disableResp.Code, disableResp.Body.String())
	}

	enableResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/enable", map[string]any{"hub_id": hubID}, token)
	if enableResp.Code != http.StatusOK {
		t.Fatalf("body hub id enable status=%d body=%s", enableResp.Code, enableResp.Body.String())
	}

	digitalEmployeeBodyResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/digital-employee-authorization", map[string]any{
		"hub_id":    hubID,
		"tenant_id": "tenant_default",
		"quota":     1,
		"years":     1,
		"enabled":   true,
	}, token)
	if digitalEmployeeBodyResp.Code != http.StatusOK {
		t.Fatalf("body hub id digital employee authorization status=%d body=%s", digitalEmployeeBodyResp.Code, digitalEmployeeBodyResp.Body.String())
	}

	digitalEmployeeResp := doJSONRequest(t, svc.handler, http.MethodPost, "/api/admin/hubs/"+url.PathEscape(hubID)+"/digital-employee-authorization", map[string]any{
		"tenant_id": "tenant_default",
		"quota":     2,
		"years":     1,
		"enabled":   true,
	}, token)
	if digitalEmployeeResp.Code != http.StatusOK {
		t.Fatalf("escaped path hub id digital employee authorization status=%d body=%s", digitalEmployeeResp.Code, digitalEmployeeResp.Body.String())
	}
	if !bytes.Contains(digitalEmployeeResp.Body.Bytes(), []byte(`"tenant_id":"tenant_default"`)) {
		t.Fatalf("default tenant authorization response should be externalized, body=%s", digitalEmployeeResp.Body.String())
	}
}

func TestAdminOpaqueHubIDCompatHandlesBodyActionRoutesBeforeMux(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)
	now := time.Now().UTC()
	createHub := func(hubID string, port int) {
		t.Helper()
		if err := svc.store.Hubs.Create(context.Background(), &store.HubInstance{
			ID:                     hubID,
			InstallationID:         "opaque-body-route-" + hubID,
			OwnerEmail:             "owner-body@example.com",
			Name:                   "Opaque Body Route Hub",
			BaseURL:                hubID,
			Host:                   "hub.example.com",
			Port:                   port,
			Visibility:             "shared",
			EnrollmentMode:         "open",
			Status:                 "online",
			CapabilitiesJSON:       "{}",
			RegistrationPolicyJSON: "{}",
			HubSecretHash:          "hash",
			LastSeenAt:             &now,
			CreatedAt:              now,
			UpdatedAt:              now,
		}); err != nil {
			t.Fatalf("create opaque hub: %v", err)
		}
	}
	hubID := "https://hub.example.com/body-route"
	createHub(hubID, 443)
	deleteHubID := "https://hub.example.com/body-route-delete"
	createHub(deleteHubID, 444)
	confirmHubID := "https://hub.example.com/body-route-confirm"
	if err := svc.store.Hubs.Create(context.Background(), &store.HubInstance{
		ID:                     confirmHubID,
		InstallationID:         "opaque-body-route-hub-id",
		OwnerEmail:             "owner-body@example.com",
		Name:                   "Opaque Body Route Hub",
		BaseURL:                confirmHubID,
		Host:                   "hub.example.com",
		Port:                   445,
		Visibility:             "shared",
		EnrollmentMode:         "open",
		Status:                 "pending_confirmation",
		CapabilitiesJSON:       "{}",
		RegistrationPolicyJSON: "{}",
		HubSecretHash:          "hash",
		LastSeenAt:             &now,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatalf("create opaque hub: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	handler := adminOpaqueHubIDCompat(inner, svc.admins, svc.hubs)
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/admin/hubs/registration-policy", map[string]any{
		"hub_id":               hubID,
		"hub_origin":           "official",
		"default_signup_scope": "public",
		"tenant": map[string]any{
			"tenant_id":    "tenant_default",
			"signup_scope": "inherit",
		},
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("compat body action route should not fall through to mux, status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte(`"tenant_default"`)) {
		t.Fatalf("default tenant should be externalized, body=%s", resp.Body.String())
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{name: "visibility", method: http.MethodPost, path: "/api/admin/hubs/visibility", body: map[string]any{"hub_id": hubID, "visibility": "private"}},
		{name: "digital_employee_authorization", method: http.MethodPost, path: "/api/admin/hubs/digital-employee-authorization", body: map[string]any{"hub_id": hubID, "tenant_id": "tenant_default", "quota": 1, "years": 1, "enabled": true}},
		{name: "disable", method: http.MethodPost, path: "/api/admin/hubs/disable", body: map[string]any{"hub_id": hubID}},
		{name: "enable", method: http.MethodPost, path: "/api/admin/hubs/enable", body: map[string]any{"hub_id": hubID}},
		{name: "confirm", method: http.MethodPost, path: "/api/admin/hubs/confirm", body: map[string]any{"hub_id": confirmHubID}},
		{name: "delete", method: http.MethodDelete, path: "/api/admin/hubs", body: map[string]any{"hub_id": deleteHubID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSONRequest(t, handler, tc.method, tc.path, tc.body, token)
			if resp.Code != http.StatusOK {
				t.Fatalf("compat body action route should not fall through to mux, status=%d body=%s", resp.Code, resp.Body.String())
			}
		})
	}
}
