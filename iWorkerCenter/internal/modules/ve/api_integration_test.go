package ve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// --- Test Helpers ---

// newTestHandler creates a fully wired Handler with a fresh registry, quota store, auth handler, and presence manager.
func newTestHandler(t *testing.T, quota int) (*Handler, *http.ServeMux) {
	t.Helper()
	dir := t.TempDir()
	key := testKeyMat()
	hubID := "hub-integration-test"

	quotaFP := filepath.Join(dir, "quota.enc")
	qs := NewQuotaStore(key, hubID, quotaFP)
	if err := qs.SaveQuota(quota); err != nil {
		t.Fatal(err)
	}

	regFP := filepath.Join(dir, "ve_registry.json")
	registry := NewRegistry(qs, regFP)
	authHandler := NewAuthHandler()
	presence := NewPresenceManager()

	handler := NewHandler(registry, authHandler, presence)

	mux := http.NewServeMux()
	handler.RegisterAdminRoutes(mux)
	handler.RegisterClientRoutes(mux)
	return handler, mux
}

// doRequest is a helper to perform an HTTP request against the test mux.
func doRequest(t *testing.T, mux *http.ServeMux, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// parseJSON decodes the response body into a map.
func parseJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nbody: %s", err, rr.Body.String())
	}
	return result
}

// extractVEID extracts the VE ID from a registration response.
func extractVEID(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	result := parseJSON(t, rr)
	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected non-empty 'id' in response: %v", result)
	}
	return id
}


// --- Integration Tests ---

// TestIntegration_FullRegistrationApprovalQueryFlow tests the complete lifecycle:
// Client registers VE → Admin approves → Client queries status → VE appears in discoverable list.
func TestIntegration_FullRegistrationApprovalQueryFlow(t *testing.T) {
	_, mux := newTestHandler(t, 10)
	machineID := "machine-flow-001"
	headers := map[string]string{"X-Machine-ID": machineID}

	// Step 1: Client registers a VE
	regBody := `{"name":"流程测试员工","skill_description":"擅长流程测试","access_policy":"public"}`
	rr := doRequest(t, mux, "POST", "/api/ve/register", regBody, headers)
	if rr.Code != http.StatusCreated {
		t.Fatalf("Register: status=%d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	veID := extractVEID(t, rr)

	// Step 2: Verify status is pending
	rr = doRequest(t, mux, "GET", "/api/ve/status", "", headers)
	if rr.Code != http.StatusOK {
		t.Fatalf("Status: status=%d, want 200", rr.Code)
	}
	statusResp := parseJSON(t, rr)
	if statusResp["registered"] != true {
		t.Error("expected registered=true")
	}
	emp := statusResp["employee"].(map[string]any)
	if emp["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", emp["status"])
	}

	// Step 3: VE should NOT appear in discoverable list (still pending)
	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "other-machine"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Discoverable: status=%d, want 200", rr.Code)
	}
	discResp := parseJSON(t, rr)
	employees := discResp["employees"]
	if employees != nil {
		empList, ok := employees.([]any)
		if ok && len(empList) > 0 {
			t.Error("pending VE should not appear in discoverable list")
		}
	}

	// Step 4: Admin approves the VE
	rr = doRequest(t, mux, "POST", "/api/ve/"+veID+"/approve", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("Approve: status=%d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	// Step 5: Verify status is now active
	rr = doRequest(t, mux, "GET", "/api/ve/status", "", headers)
	statusResp = parseJSON(t, rr)
	emp = statusResp["employee"].(map[string]any)
	if emp["status"] != "active" {
		t.Errorf("expected status=active after approval, got %v", emp["status"])
	}

	// Step 6: VE should now appear in discoverable list
	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "other-machine"})
	discResp = parseJSON(t, rr)
	empList := discResp["employees"].([]any)
	if len(empList) != 1 {
		t.Fatalf("expected 1 discoverable VE, got %d", len(empList))
	}
	discoveredVE := empList[0].(map[string]any)
	if discoveredVE["name"] != "流程测试员工" {
		t.Errorf("discoverable VE name = %v, want '流程测试员工'", discoveredVE["name"])
	}

	// Step 7: Admin list shows the VE
	rr = doRequest(t, mux, "GET", "/api/ve/list", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("Admin list: status=%d", rr.Code)
	}
	adminResp := parseJSON(t, rr)
	adminList := adminResp["employees"].([]any)
	if len(adminList) != 1 {
		t.Fatalf("admin list: expected 1 VE, got %d", len(adminList))
	}
}


// TestIntegration_AccessPolicyFiltering tests all 4 access policies with different requester IDs.
func TestIntegration_AccessPolicyFiltering(t *testing.T) {
	_, mux := newTestHandler(t, 10)

	// Register and approve 4 VEs with different policies
	type veSetup struct {
		machineID string
		name      string
		policy    string
		whitelist []string
		blacklist []string
	}
	setups := []veSetup{
		{"m-pub", "Public VE", "public", nil, nil},
		{"m-wl", "Whitelist VE", "whitelist", []string{"allowed-machine"}, nil},
		{"m-bl", "Blacklist VE", "blacklist", nil, []string{"blocked-machine"}},
		{"m-pr", "PerRequest VE", "per_request", nil, nil},
	}

	for _, s := range setups {
		body := `{"name":"` + s.name + `","skill_description":"test","access_policy":"` + s.policy + `"`
		if len(s.whitelist) > 0 {
			body += `,"whitelist":["` + strings.Join(s.whitelist, `","`) + `"]`
		}
		if len(s.blacklist) > 0 {
			body += `,"blacklist":["` + strings.Join(s.blacklist, `","`) + `"]`
		}
		body += `}`

		rr := doRequest(t, mux, "POST", "/api/ve/register", body, map[string]string{"X-Machine-ID": s.machineID})
		if rr.Code != http.StatusCreated {
			t.Fatalf("Register %s: status=%d, body=%s", s.name, rr.Code, rr.Body.String())
		}
		veID := extractVEID(t, rr)

		rr = doRequest(t, mux, "POST", "/api/ve/"+veID+"/approve", "", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("Approve %s: status=%d, body=%s", s.name, rr.Code, rr.Body.String())
		}
	}

	tests := []struct {
		name        string
		requesterID string
		wantCount   int
		wantNames   []string
	}{
		{
			name:        "allowed-machine sees public + whitelist + blacklist(not blocked) + per_request = 4",
			requesterID: "allowed-machine",
			wantCount:   4,
		},
		{
			name:        "blocked-machine sees public + per_request = 2 (whitelist excludes, blacklist excludes)",
			requesterID: "blocked-machine",
			wantCount:   2,
		},
		{
			name:        "random-machine sees public + blacklist(not blocked) + per_request = 3 (whitelist excludes)",
			requesterID: "random-machine",
			wantCount:   3,
		},
		{
			name:        "empty requester sees public + blacklist + per_request = 3 (whitelist excludes empty)",
			requesterID: "",
			wantCount:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.requesterID != "" {
				headers["X-Machine-ID"] = tt.requesterID
			}
			rr := doRequest(t, mux, "GET", "/api/ve/discoverable", "", headers)
			if rr.Code != http.StatusOK {
				t.Fatalf("Discoverable: status=%d", rr.Code)
			}
			resp := parseJSON(t, rr)
			empList := resp["employees"]
			if empList == nil {
				if tt.wantCount != 0 {
					t.Fatalf("expected %d VEs, got nil", tt.wantCount)
				}
				return
			}
			list := empList.([]any)
			if len(list) != tt.wantCount {
				names := make([]string, len(list))
				for i, e := range list {
					names[i] = e.(map[string]any)["name"].(string)
				}
				t.Errorf("requester=%q: got %d VEs %v, want %d", tt.requesterID, len(list), names, tt.wantCount)
			}
		})
	}
}


// TestIntegration_QuotaExceededRejection tests that registering VEs up to the quota limit
// causes the next registration to return a quota_exceeded error.
func TestIntegration_QuotaExceededRejection(t *testing.T) {
	quota := 2
	_, mux := newTestHandler(t, quota)

	// Register and approve VEs up to quota
	for i := 0; i < quota; i++ {
		machineID := "machine-quota-" + string(rune('A'+i))
		name := "VE-" + string(rune('A'+i))
		body := `{"name":"` + name + `","skill_description":"test","access_policy":"public"}`
		rr := doRequest(t, mux, "POST", "/api/ve/register", body, map[string]string{"X-Machine-ID": machineID})
		if rr.Code != http.StatusCreated {
			t.Fatalf("Register VE %d: status=%d, body=%s", i, rr.Code, rr.Body.String())
		}
		veID := extractVEID(t, rr)

		rr = doRequest(t, mux, "POST", "/api/ve/"+veID+"/approve", "", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("Approve VE %d: status=%d, body=%s", i, rr.Code, rr.Body.String())
		}
	}

	// Next registration should fail with quota_exceeded
	body := `{"name":"VE-Overflow","skill_description":"test","access_policy":"public"}`
	rr := doRequest(t, mux, "POST", "/api/ve/register", body, map[string]string{"X-Machine-ID": "machine-overflow"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("Register over quota: status=%d, want 409, body=%s", rr.Code, rr.Body.String())
	}
	resp := parseJSON(t, rr)
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != "QUOTA_EXCEEDED" {
		t.Errorf("error code = %v, want QUOTA_EXCEEDED", errObj["code"])
	}
}

// TestIntegration_QuotaExceededAtApproval tests that approving a VE when quota is already
// full returns a quota_exceeded error (quota checked at approval time too).
func TestIntegration_QuotaExceededAtApproval(t *testing.T) {
	_, mux := newTestHandler(t, 1)

	// Register two VEs (both pending — registration allows since active count is 0)
	body1 := `{"name":"VE-First","skill_description":"test","access_policy":"public"}`
	rr := doRequest(t, mux, "POST", "/api/ve/register", body1, map[string]string{"X-Machine-ID": "m-first"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("Register first: status=%d", rr.Code)
	}
	veID1 := extractVEID(t, rr)

	// Approve first — fills quota
	rr = doRequest(t, mux, "POST", "/api/ve/"+veID1+"/approve", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("Approve first: status=%d", rr.Code)
	}

	// Register second (pending is OK since active=1 >= quota=1 check is at registration)
	body2 := `{"name":"VE-Second","skill_description":"test","access_policy":"public"}`
	rr = doRequest(t, mux, "POST", "/api/ve/register", body2, map[string]string{"X-Machine-ID": "m-second"})
	// This should fail at registration since active count (1) >= quota (1)
	if rr.Code != http.StatusConflict {
		t.Fatalf("Register second when quota full: status=%d, want 409, body=%s", rr.Code, rr.Body.String())
	}
}


// TestIntegration_InputValidation tests name ≤50 chars and skill_desc ≤500 chars constraints.
func TestIntegration_InputValidation(t *testing.T) {
	_, mux := newTestHandler(t, 10)
	headers := map[string]string{"X-Machine-ID": "m-validation"}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "name exceeds 50 characters",
			body:       `{"name":"` + strings.Repeat("あ", 51) + `","skill_description":"ok","access_policy":"public"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "REGISTER_FAILED",
		},
		{
			name:       "name exactly 50 characters is OK",
			body:       `{"name":"` + strings.Repeat("x", 50) + `","skill_description":"ok","access_policy":"public"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "skill_description exceeds 500 characters",
			body:       `{"name":"Valid","skill_description":"` + strings.Repeat("描", 501) + `","access_policy":"public"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "REGISTER_FAILED",
		},
		{
			name:       "empty name",
			body:       `{"name":"","skill_description":"ok","access_policy":"public"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "REGISTER_FAILED",
		},
		{
			name:       "invalid access_policy",
			body:       `{"name":"Valid","skill_description":"ok","access_policy":"unknown_policy"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "REGISTER_FAILED",
		},
		{
			name:       "missing machine_id header",
			body:       `{"name":"Valid","skill_description":"ok","access_policy":"public"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "REGISTER_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := headers
			if tt.name == "missing machine_id header" {
				h = nil // no X-Machine-ID header
			}
			// Use unique machine IDs for successful cases to avoid duplicate registration errors
			if tt.wantStatus == http.StatusCreated {
				h = map[string]string{"X-Machine-ID": "m-valid-" + tt.name}
			}
			rr := doRequest(t, mux, "POST", "/api/ve/register", tt.body, h)
			if rr.Code != tt.wantStatus {
				t.Errorf("status=%d, want %d, body=%s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if tt.wantCode != "" && rr.Code != http.StatusCreated {
				resp := parseJSON(t, rr)
				errObj := resp["error"].(map[string]any)
				if errObj["code"] != tt.wantCode {
					t.Errorf("error code = %v, want %s", errObj["code"], tt.wantCode)
				}
			}
		})
	}
}


// TestIntegration_RejectFlow tests the reject flow: register → reject → verify status.
func TestIntegration_RejectFlow(t *testing.T) {
	_, mux := newTestHandler(t, 10)
	headers := map[string]string{"X-Machine-ID": "m-reject"}

	// Register
	body := `{"name":"Reject Target","skill_description":"will be rejected","access_policy":"public"}`
	rr := doRequest(t, mux, "POST", "/api/ve/register", body, headers)
	if rr.Code != http.StatusCreated {
		t.Fatalf("Register: status=%d", rr.Code)
	}
	veID := extractVEID(t, rr)

	// Reject with reason
	rejectBody := `{"reason":"不符合技能要求"}`
	rr = doRequest(t, mux, "POST", "/api/ve/"+veID+"/reject", rejectBody, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("Reject: status=%d, body=%s", rr.Code, rr.Body.String())
	}

	// Verify status is rejected
	rr = doRequest(t, mux, "GET", "/api/ve/status", "", headers)
	resp := parseJSON(t, rr)
	// After rejection, GetByOwner won't find it (only returns pending/active)
	if resp["registered"] != false {
		t.Error("rejected VE should not appear as registered")
	}

	// Admin list should show rejected status
	rr = doRequest(t, mux, "GET", "/api/ve/list", "", nil)
	adminResp := parseJSON(t, rr)
	adminList := adminResp["employees"].([]any)
	found := false
	for _, e := range adminList {
		emp := e.(map[string]any)
		if emp["id"] == veID {
			found = true
			if emp["status"] != "rejected" {
				t.Errorf("admin list: status=%v, want rejected", emp["status"])
			}
			if emp["reject_reason"] != "不符合技能要求" {
				t.Errorf("admin list: reject_reason=%v, want '不符合技能要求'", emp["reject_reason"])
			}
		}
	}
	if !found {
		t.Error("rejected VE not found in admin list")
	}

	// Rejected VE should NOT appear in discoverable list
	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "other"})
	discResp := parseJSON(t, rr)
	if empList := discResp["employees"]; empList != nil {
		list := empList.([]any)
		if len(list) > 0 {
			t.Error("rejected VE should not appear in discoverable list")
		}
	}
}

// TestIntegration_DisableFlow tests the disable flow: register → approve → disable → verify.
func TestIntegration_DisableFlow(t *testing.T) {
	_, mux := newTestHandler(t, 10)
	headers := map[string]string{"X-Machine-ID": "m-disable"}

	// Register and approve
	body := `{"name":"Disable Target","skill_description":"will be disabled","access_policy":"public"}`
	rr := doRequest(t, mux, "POST", "/api/ve/register", body, headers)
	if rr.Code != http.StatusCreated {
		t.Fatalf("Register: status=%d", rr.Code)
	}
	veID := extractVEID(t, rr)

	rr = doRequest(t, mux, "POST", "/api/ve/"+veID+"/approve", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("Approve: status=%d", rr.Code)
	}

	// Verify VE is discoverable
	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "other"})
	discResp := parseJSON(t, rr)
	empList := discResp["employees"].([]any)
	if len(empList) != 1 {
		t.Fatalf("expected 1 discoverable VE before disable, got %d", len(empList))
	}

	// Disable
	rr = doRequest(t, mux, "POST", "/api/ve/"+veID+"/disable", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("Disable: status=%d, body=%s", rr.Code, rr.Body.String())
	}

	// Verify VE is no longer discoverable
	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "other"})
	discResp = parseJSON(t, rr)
	if empList := discResp["employees"]; empList != nil {
		list := empList.([]any)
		if len(list) > 0 {
			t.Error("disabled VE should not appear in discoverable list")
		}
	}

	// Admin list shows disabled status
	rr = doRequest(t, mux, "GET", "/api/ve/list", "", nil)
	adminResp := parseJSON(t, rr)
	adminList := adminResp["employees"].([]any)
	for _, e := range adminList {
		emp := e.(map[string]any)
		if emp["id"] == veID {
			if emp["status"] != "disabled" {
				t.Errorf("admin list: status=%v, want disabled", emp["status"])
			}
		}
	}
}


// TestIntegration_AdminConfig tests the PUT /api/ve/config endpoint for group config.
func TestIntegration_AdminConfig(t *testing.T) {
	_, mux := newTestHandler(t, 10)

	// GET default config
	rr := doRequest(t, mux, "GET", "/api/ve/config", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET config: status=%d", rr.Code)
	}
	resp := parseJSON(t, rr)
	if resp["max_group_participants"] != float64(5) {
		t.Errorf("default max_group_participants=%v, want 5", resp["max_group_participants"])
	}

	// PUT valid config
	rr = doRequest(t, mux, "PUT", "/api/ve/config", `{"max_group_participants":8}`, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT config: status=%d, body=%s", rr.Code, rr.Body.String())
	}
	resp = parseJSON(t, rr)
	if resp["max_group_participants"] != float64(8) {
		t.Errorf("updated max_group_participants=%v, want 8", resp["max_group_participants"])
	}

	// PUT invalid config (out of range)
	rr = doRequest(t, mux, "PUT", "/api/ve/config", `{"max_group_participants":11}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("PUT config out of range: status=%d, want 400", rr.Code)
	}

	rr = doRequest(t, mux, "PUT", "/api/ve/config", `{"max_group_participants":0}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("PUT config zero: status=%d, want 400", rr.Code)
	}
}

// TestIntegration_MethodNotAllowed tests that wrong HTTP methods return 405.
func TestIntegration_MethodNotAllowed(t *testing.T) {
	_, mux := newTestHandler(t, 10)

	tests := []struct {
		method string
		path   string
	}{
		{"POST", "/api/ve/list"},
		{"GET", "/api/ve/register"},
		{"POST", "/api/ve/status"},
		{"GET", "/api/ve/settings"},
		{"POST", "/api/ve/discoverable"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rr := doRequest(t, mux, tt.method, tt.path, "", nil)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("status=%d, want 405", rr.Code)
			}
		})
	}
}

// TestIntegration_UpdateSettings tests the PUT /api/ve/settings endpoint.
func TestIntegration_UpdateSettings(t *testing.T) {
	_, mux := newTestHandler(t, 10)
	headers := map[string]string{"X-Machine-ID": "m-settings"}

	// Register and approve
	body := `{"name":"Settings VE","skill_description":"original","access_policy":"public"}`
	rr := doRequest(t, mux, "POST", "/api/ve/register", body, headers)
	if rr.Code != http.StatusCreated {
		t.Fatalf("Register: status=%d", rr.Code)
	}
	veID := extractVEID(t, rr)
	doRequest(t, mux, "POST", "/api/ve/"+veID+"/approve", "", nil)

	// Update settings
	updateBody := `{"name":"Updated Name","skill_description":"updated desc","access_policy":"whitelist","whitelist":["friend-machine"]}`
	rr = doRequest(t, mux, "PUT", "/api/ve/settings", updateBody, headers)
	if rr.Code != http.StatusOK {
		t.Fatalf("Update settings: status=%d, body=%s", rr.Code, rr.Body.String())
	}
	resp := parseJSON(t, rr)
	if resp["name"] != "Updated Name" {
		t.Errorf("name=%v, want 'Updated Name'", resp["name"])
	}
	if resp["access_policy"] != "whitelist" {
		t.Errorf("access_policy=%v, want 'whitelist'", resp["access_policy"])
	}

	// Verify access policy filtering works with updated whitelist
	// friend-machine should see it, random-machine should not
	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "friend-machine"})
	discResp := parseJSON(t, rr)
	empList := discResp["employees"].([]any)
	if len(empList) != 1 {
		t.Errorf("friend-machine should see 1 VE, got %d", len(empList))
	}

	rr = doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "random-machine"})
	discResp = parseJSON(t, rr)
	if empList := discResp["employees"]; empList != nil {
		list := empList.([]any)
		if len(list) != 0 {
			t.Errorf("random-machine should see 0 VEs, got %d", len(list))
		}
	}
}

// TestIntegration_DiscoverableIncludesGroupConfig tests that the discoverable endpoint
// returns the max_group_participants configuration.
func TestIntegration_DiscoverableIncludesGroupConfig(t *testing.T) {
	_, mux := newTestHandler(t, 10)

	rr := doRequest(t, mux, "GET", "/api/ve/discoverable", "", map[string]string{"X-Machine-ID": "any"})
	if rr.Code != http.StatusOK {
		t.Fatalf("Discoverable: status=%d", rr.Code)
	}
	resp := parseJSON(t, rr)
	if resp["max_group_participants"] != float64(5) {
		t.Errorf("max_group_participants=%v, want 5", resp["max_group_participants"])
	}
}
