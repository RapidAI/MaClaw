package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnroll_FullFlow(t *testing.T) {
	InvalidateCenterCache()

	// Fake Hub that accepts enrollment.
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["email"] != "test@example.com" {
			http.Error(w, "bad email", 400)
			return
		}
		if req["client_id"] == nil || req["client_id"] == "" {
			http.Error(w, "missing client_id", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"email":         "test@example.com",
			"user_id":       "user-123",
			"sn":            "SN-001",
			"machine_id":    "machine-abc",
			"machine_token": "token-xyz",
		})
	}))
	defer hub.Close()

	// Fake HubCenter that resolves to the fake hub.
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			json.NewEncoder(w).Encode(map[string]any{
				"quality_score":  10,
				"routable":       true,
				"service_status": "ok",
				"features":       map[string]any{"can_resolve": true},
			})
		case "/api/client/hubcenters":
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{}})
		case "/api/entry/resolve":
			json.NewEncoder(w).Encode(HubCenterResolveResult{
				Email:        "test@example.com",
				Mode:         "single",
				DefaultHubID: "hub-1",
				Hubs: []HubCenterResolveHub{
					{HubID: "hub-1", Name: "Test Hub", BaseURL: hub.URL, Status: "online"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer center.Close()

	// Override defaults so the test doesn't hit real HubCenter URLs.
	origDefaults := DefaultRemoteHubCenterURLs
	origDefault := DefaultRemoteHubCenterURL
	DefaultRemoteHubCenterURLs = []string{center.URL}
	DefaultRemoteHubCenterURL = center.URL
	defer func() {
		DefaultRemoteHubCenterURLs = origDefaults
		DefaultRemoteHubCenterURL = origDefault
	}()

	client := NewEnrollmentClient()
	result, err := client.Enroll(context.Background(), EnrollConfig{
		Email:        "test@example.com",
		HubCenterURL: center.URL,
		MachineName:  "test-machine",
		Platform:     "linux",
		Hostname:     "test-host",
		Arch:         "amd64",
		AppVersion:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("Enroll failed: %v", err)
	}

	if result.MachineID != "machine-abc" {
		t.Errorf("MachineID = %q, want %q", result.MachineID, "machine-abc")
	}
	if result.MachineToken != "token-xyz" {
		t.Errorf("MachineToken = %q, want %q", result.MachineToken, "token-xyz")
	}
	if result.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", result.Email, "test@example.com")
	}
	if result.HubURL != hub.URL {
		t.Errorf("HubURL = %q, want %q", result.HubURL, hub.URL)
	}
	if result.ClientID == "" {
		t.Error("ClientID should be auto-generated when not provided")
	}
	if result.HubCenterURL == "" {
		t.Error("HubCenterURL should be set to the center that resolved the hub")
	}
}

func TestEnroll_WithExistingHubURL(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"email":         "user@corp.com",
			"machine_id":    "m-direct",
			"machine_token": "t-direct",
		})
	}))
	defer hub.Close()

	client := &EnrollmentClient{HTTPClient: hub.Client()}
	result, err := client.Enroll(context.Background(), EnrollConfig{
		Email:      "user@corp.com",
		HubURL:     hub.URL,
		ClientID:   "existing-client-id",
		AppVersion: "2.0.0",
	})
	if err != nil {
		t.Fatalf("Enroll failed: %v", err)
	}
	if result.MachineID != "m-direct" {
		t.Errorf("MachineID = %q, want %q", result.MachineID, "m-direct")
	}
	if result.ClientID != "existing-client-id" {
		t.Errorf("ClientID = %q, want %q", result.ClientID, "existing-client-id")
	}
}

func TestEnroll_NormalizesHeartbeatIntervalInRequest(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "unset defaults to 30", in: 0, want: 30},
		{name: "small positive clamps to 5", in: 3, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotHeartbeat int
			hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/enroll/start" {
					http.NotFound(w, r)
					return
				}
				var req struct {
					HeartbeatIntervalSec int `json:"heartbeat_interval_sec"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				gotHeartbeat = req.HeartbeatIntervalSec
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":        "approved",
					"email":         "user@corp.com",
					"machine_id":    "m-direct",
					"machine_token": "t-direct",
				})
			}))
			defer hub.Close()

			client := &EnrollmentClient{HTTPClient: hub.Client()}
			_, err := client.Enroll(context.Background(), EnrollConfig{
				Email:        "user@corp.com",
				HubURL:       hub.URL,
				ClientID:     "existing-client-id",
				AppVersion:   "2.0.0",
				HeartbeatSec: tt.in,
			})
			if err != nil {
				t.Fatalf("Enroll failed: %v", err)
			}
			if gotHeartbeat != tt.want {
				t.Fatalf("heartbeat_interval_sec = %d, want %d", gotHeartbeat, tt.want)
			}
		})
	}
}

func TestEnroll_AcceptsBOMPrefixedJSON(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
		_, _ = w.Write([]byte(`{"status":"approved","email":"bom@example.com","machine_id":"m-bom","machine_token":"t-bom"}`))
	}))
	defer hub.Close()

	client := &EnrollmentClient{HTTPClient: hub.Client()}
	result, err := client.Enroll(context.Background(), EnrollConfig{
		Email:  "bom@example.com",
		HubURL: hub.URL,
	})
	if err != nil {
		t.Fatalf("Enroll failed: %v", err)
	}
	if result.MachineID != "m-bom" {
		t.Fatalf("MachineID = %q, want m-bom", result.MachineID)
	}
}

func TestEnroll_NonJSONErrorResponseIsUserFriendly(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<!doctype html><title>Bad Gateway</title><p>proxy failed</p>"))
	}))
	defer hub.Close()

	client := &EnrollmentClient{HTTPClient: hub.Client()}
	_, err := client.Enroll(context.Background(), EnrollConfig{
		Email:  "html@example.com",
		HubURL: hub.URL,
	})
	if err == nil {
		t.Fatal("expected non-json response error")
	}
	msg := err.Error()
	// The error should be user-friendly with the HTTP status code, not raw JSON parse error.
	if !strings.Contains(msg, "HTTP 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnroll_MissingEmail(t *testing.T) {
	client := NewEnrollmentClient()
	_, err := client.Enroll(context.Background(), EnrollConfig{})
	if err == nil || !strings.Contains(err.Error(), "email is required") {
		t.Fatalf("expected email required error, got: %v", err)
	}
}

func TestEnroll_HubReturnsError(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "EMAIL_BLOCKED",
			"message": "this email is blocked",
		})
	}))
	defer hub.Close()

	client := &EnrollmentClient{HTTPClient: hub.Client()}
	_, err := client.Enroll(context.Background(), EnrollConfig{
		Email:  "blocked@example.com",
		HubURL: hub.URL,
	})
	if err == nil {
		t.Fatal("expected error for blocked email")
	}
	if !strings.Contains(err.Error(), "EMAIL_BLOCKED") {
		t.Errorf("error should contain EMAIL_BLOCKED, got: %v", err)
	}
}

func TestResolveHubs_ReturnsParsedResult(t *testing.T) {
	InvalidateCenterCache()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			json.NewEncoder(w).Encode(map[string]any{
				"quality_score": 10, "routable": true,
				"service_status": "ok", "features": map[string]any{"can_resolve": true},
			})
		case "/api/client/hubcenters":
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{}})
		case "/api/entry/resolve":
			json.NewEncoder(w).Encode(HubCenterResolveResult{
				Email: "u@example.com", Mode: "multiple",
				DefaultHubID: "h1",
				Hubs: []HubCenterResolveHub{
					{HubID: "h1", Name: "Hub A", BaseURL: "https://a.example.com", Status: "online"},
					{HubID: "h2", Name: "Hub B", BaseURL: "https://b.example.com", Status: "online"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer center.Close()

	origDefaults := DefaultRemoteHubCenterURLs
	origDefault := DefaultRemoteHubCenterURL
	DefaultRemoteHubCenterURLs = []string{center.URL}
	DefaultRemoteHubCenterURL = center.URL
	defer func() {
		DefaultRemoteHubCenterURLs = origDefaults
		DefaultRemoteHubCenterURL = origDefault
	}()

	client := NewEnrollmentClient()
	result, usedCenter, _, err := client.ResolveHubs(context.Background(), "u@example.com", "", center.URL, nil)
	if err != nil {
		t.Fatalf("ResolveHubs failed: %v", err)
	}
	if len(result.Hubs) != 2 {
		t.Fatalf("expected 2 hubs, got %d", len(result.Hubs))
	}
	if result.DefaultHubID != "h1" {
		t.Errorf("DefaultHubID = %q, want %q", result.DefaultHubID, "h1")
	}
	if usedCenter != center.URL {
		t.Errorf("usedCenter = %q, want %q", usedCenter, center.URL)
	}
}

func TestResolveHubsSendsPhoneNumberForNumericIdentity(t *testing.T) {
	InvalidateCenterCache()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"quality_score":  10,
				"routable":       true,
				"service_status": "ok",
				"features":       map[string]any{"can_resolve": true},
			})
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{}})
		case "/api/entry/resolve":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode resolve payload: %v", err)
			}
			if payload["email"] != "199 0000 1111" || payload["phone_number"] != "19900001111" {
				t.Fatalf("resolve payload = %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(HubCenterResolveResult{
				Email:        "phone:19900001111",
				Mode:         "single",
				DefaultHubID: "hub-phone",
				Hubs: []HubCenterResolveHub{
					{HubID: "hub-phone", Name: "Phone Hub", BaseURL: "https://phonehub.example.com", Status: "online", TenantID: "tenant-phone"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer center.Close()

	origDefaults := DefaultRemoteHubCenterURLs
	origDefault := DefaultRemoteHubCenterURL
	DefaultRemoteHubCenterURLs = []string{center.URL}
	DefaultRemoteHubCenterURL = center.URL
	defer func() {
		DefaultRemoteHubCenterURLs = origDefaults
		DefaultRemoteHubCenterURL = origDefault
	}()

	client := NewEnrollmentClient()
	result, _, _, err := client.ResolveHubs(context.Background(), "199 0000 1111", "", center.URL, nil)
	if err != nil {
		t.Fatalf("ResolveHubs() error = %v", err)
	}
	if result.Email != "phone:19900001111" || len(result.Hubs) != 1 || result.Hubs[0].TenantID != "tenant-phone" {
		t.Fatalf("resolve result = %#v", result)
	}
}

func TestResolveHubsDoesNotSendPhoneNumberForAlphanumericUserID(t *testing.T) {
	InvalidateCenterCache()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"quality_score":  10,
				"routable":       true,
				"service_status": "ok",
				"features":       map[string]any{"can_resolve": true},
			})
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{}})
		case "/api/entry/resolve":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode resolve payload: %v", err)
			}
			if payload["email"] != "abc123456" {
				t.Fatalf("email payload = %q, want abc123456", payload["email"])
			}
			if _, ok := payload["phone_number"]; ok {
				t.Fatalf("resolve payload unexpectedly included phone_number: %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(HubCenterResolveResult{
				Email:        "abc123456",
				Mode:         "single",
				DefaultHubID: "hub-userid",
				Hubs: []HubCenterResolveHub{
					{HubID: "hub-userid", Name: "User ID Hub", BaseURL: "https://userid.example.com", Status: "online", TenantID: "tenant-userid"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer center.Close()

	origDefaults := DefaultRemoteHubCenterURLs
	origDefault := DefaultRemoteHubCenterURL
	DefaultRemoteHubCenterURLs = []string{center.URL}
	DefaultRemoteHubCenterURL = center.URL
	defer func() {
		DefaultRemoteHubCenterURLs = origDefaults
		DefaultRemoteHubCenterURL = origDefault
	}()

	client := NewEnrollmentClient()
	result, _, _, err := client.ResolveHubs(context.Background(), "abc123456", "", center.URL, nil)
	if err != nil {
		t.Fatalf("ResolveHubs() error = %v", err)
	}
	if result.Email != "abc123456" || len(result.Hubs) != 1 || result.Hubs[0].TenantID != "tenant-userid" {
		t.Fatalf("resolve result = %#v", result)
	}
}

func TestPickBestHubWithTenantAndIDPrefersTenantSpecificDefaultHub(t *testing.T) {
	result := HubCenterResolveResult{
		Email:        "znsoft@163.com",
		Mode:         "multiple",
		DefaultHubID: "hub-official",
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-official", Name: "Official", BaseURL: "https://hub.example.com", Status: "online"},
			{HubID: "hub-official", TenantID: "vantagics", Name: "Official", BaseURL: "https://hub.example.com", Status: "online"},
		},
	}

	hubURL, hubID, tenantID, err := PickBestHubWithTenantAndID(result)
	if err != nil {
		t.Fatalf("PickBestHubWithTenantAndID() error = %v", err)
	}
	if hubURL != "https://hub.example.com" || hubID != "hub-official" || tenantID != "vantagics" {
		t.Fatalf("picked hubURL=%q hubID=%q tenantID=%q, want tenant-specific default hub", hubURL, hubID, tenantID)
	}
}

func TestNormalizeResolvePhoneNumberRejectsAlphanumericUserID(t *testing.T) {
	if got := normalizeResolvePhoneNumber("abc123456"); got != "" {
		t.Fatalf("normalizeResolvePhoneNumber(alphanumeric) = %q, want empty", got)
	}
	if got := normalizeResolvePhoneNumber("199 0000-1111"); got != "19900001111" {
		t.Fatalf("normalizeResolvePhoneNumber(phone) = %q, want 19900001111", got)
	}
}

func TestBuildMachineProfile(t *testing.T) {
	profile := BuildMachineProfile("3.0.0")
	if profile.AppVersion != "3.0.0" {
		t.Errorf("AppVersion = %q, want %q", profile.AppVersion, "3.0.0")
	}
	if profile.Platform == "" {
		t.Error("Platform should not be empty")
	}
	if profile.Arch == "" {
		t.Error("Arch should not be empty")
	}
	if profile.HeartbeatSec != 10 {
		t.Errorf("HeartbeatSec = %d, want 10", profile.HeartbeatSec)
	}
}

func TestBuildCenterURLList_Dedup(t *testing.T) {
	urls := BuildCenterURLList("https://a.com", []string{"https://b.com", "https://a.com"})
	seen := map[string]int{}
	for _, u := range urls {
		seen[u]++
		if seen[u] > 1 {
			t.Errorf("duplicate URL: %s", u)
		}
	}
	if urls[0] != "https://a.com" {
		t.Errorf("first URL should be explicit, got %s", urls[0])
	}
}

func TestPickBestHub_DefaultHub(t *testing.T) {
	result := HubCenterResolveResult{
		DefaultHubID: "hub-2",
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-1", BaseURL: "https://hub1.example.com", Status: "online"},
			{HubID: "hub-2", BaseURL: "https://hub2.example.com", Status: "online"},
		},
	}
	url, err := PickBestHub(result)
	if err != nil {
		t.Fatalf("PickBestHub failed: %v", err)
	}
	if url != "https://hub2.example.com" {
		t.Errorf("should pick default hub, got %s", url)
	}
}

func TestPickBestHubWithTenantAndID_ReturnsHubContext(t *testing.T) {
	result := HubCenterResolveResult{
		DefaultHubID: "hub-2",
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-1", TenantID: "tenant-a", BaseURL: "https://hub1.example.com", Status: "online"},
			{HubID: "hub-2", TenantID: "tenant-b", BaseURL: "https://hub2.example.com", Status: "online"},
		},
	}
	url, hubID, tenantID, err := PickBestHubWithTenantAndID(result)
	if err != nil {
		t.Fatalf("PickBestHubWithTenantAndID failed: %v", err)
	}
	if url != "https://hub2.example.com" || hubID != "hub-2" || tenantID != "tenant-b" {
		t.Fatalf("context = (%q, %q, %q), want default hub context", url, hubID, tenantID)
	}
}

func TestPickBestHub_FallbackToFirst(t *testing.T) {
	result := HubCenterResolveResult{
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-1", BaseURL: "https://hub1.example.com", Status: "online"},
			{HubID: "hub-2", BaseURL: "https://hub2.example.com", Status: "online"},
		},
	}
	url, err := PickBestHub(result)
	if err != nil {
		t.Fatalf("PickBestHub failed: %v", err)
	}
	if url != "https://hub1.example.com" {
		t.Errorf("should pick first hub, got %s", url)
	}
}

func TestPickBestHub_NoHubs(t *testing.T) {
	result := HubCenterResolveResult{Message: "no hubs available"}
	_, err := PickBestHub(result)
	if err == nil {
		t.Fatal("expected error for empty hubs")
	}
	if !strings.Contains(err.Error(), "no hubs available") {
		t.Errorf("error should contain message, got: %v", err)
	}
}

func TestPickBestHub_SkipsNonOnlineDefault(t *testing.T) {
	result := HubCenterResolveResult{
		DefaultHubID: "hub-1",
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-1", BaseURL: "https://hub1.example.com", Status: "pending_confirmation"},
			{HubID: "hub-2", BaseURL: "https://hub2.example.com", Status: "online"},
		},
	}
	url, err := PickBestHub(result)
	if err != nil {
		t.Fatalf("PickBestHub failed: %v", err)
	}
	if url != "https://hub2.example.com" {
		t.Errorf("should skip non-online default, got %s", url)
	}
}

func TestPickBestHub_EmptyStatusTreatedAsOnline(t *testing.T) {
	result := HubCenterResolveResult{
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-1", BaseURL: "https://hub1.example.com", Status: ""},
		},
	}
	url, err := PickBestHub(result)
	if err != nil {
		t.Fatalf("PickBestHub failed: %v", err)
	}
	if url != "https://hub1.example.com" {
		t.Errorf("empty status should be treated as online, got %s", url)
	}
}

func TestPickBestHub_AllNonOnlineFallsBackToAny(t *testing.T) {
	result := HubCenterResolveResult{
		Hubs: []HubCenterResolveHub{
			{HubID: "hub-1", BaseURL: "https://hub1.example.com", Status: "pending_confirmation"},
		},
	}
	url, err := PickBestHub(result)
	if err != nil {
		t.Fatalf("PickBestHub failed: %v", err)
	}
	if url != "https://hub1.example.com" {
		t.Errorf("should fall back to any hub, got %s", url)
	}
}

func TestIsHubOnline(t *testing.T) {
	cases := map[string]bool{
		"":                     true,
		"online":               true,
		"Online":               true,
		"ONLINE":               true,
		"  online  ":           true,
		"pending_confirmation": false,
		"disabled":             false,
		"offline":              false,
	}
	for status, want := range cases {
		if got := isHubOnline(status); got != want {
			t.Errorf("isHubOnline(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestNormalizedPlatform(t *testing.T) {
	p := NormalizedPlatform()
	if p == "" {
		t.Error("NormalizedPlatform should not be empty")
	}
	valid := map[string]bool{"windows": true, "mac": true, "linux": true}
	if !valid[p] {
		t.Errorf("NormalizedPlatform = %q, want one of windows/mac/linux", p)
	}
}
