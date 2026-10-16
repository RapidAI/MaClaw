package guiapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTokenBankShareInputBindsTheDialogPayload(t *testing.T) {
	raw := []byte(`{
		"DisplayName": "WorkBuddy 国际版",
		"APIURL": "https://www.workbuddy.ai/v2",
		"APIKey": "sk-test",
		"Protocol": "openai",
		"KeyFingerprint": "fp",
		"Models": [{"model": "fast-model", "available": true, "probe_error": "", "used_input_tokens": 0, "used_output_tokens": 0, "share_window": {"days": [1, 5], "start": "22:00", "end": "08:00"}}],
		"MaxInputTokens": 128,
		"MaxOutputTokens": 64,
		"ClientInstanceID": "maclaw-1",
		"Visibility": "public",
		"HubIDs": "",
		"TenantIDs": "",
		"SeparateAudiences": false
	}`)
	var input TokenBankShareInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	if input.DisplayName != "WorkBuddy 国际版" || input.APIURL != "https://www.workbuddy.ai/v2" || input.APIKey != "sk-test" || input.KeyFingerprint != "fp" {
		t.Fatalf("identity = %+v", input)
	}
	if input.MaxInputTokens != 128 || input.MaxOutputTokens != 64 {
		t.Fatalf("caps = %d / %d", input.MaxInputTokens, input.MaxOutputTokens)
	}
	if len(input.Models) != 1 || input.Models[0].Model != "fast-model" || !input.Models[0].Available {
		t.Fatalf("models = %+v", input.Models)
	}
	window := input.Models[0].ShareWindow
	if window == nil || window.Start != "22:00" || window.End != "08:00" || len(window.Days) != 2 || window.Days[0] != 1 || window.Days[1] != 5 {
		t.Fatalf("share window = %+v", window)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sk-test") || strings.Contains(string(encoded), "APIKey") {
		t.Fatalf("marshaled share input leaked the key: %s", encoded)
	}
}

func TestTokenBankAudienceRows(t *testing.T) {
	paired := tokenBankAudienceRows("hub-a, hub-b", "ten-1, ten-2")
	if len(paired) != 2 || paired[0]["hub_id"] != "hub-a" || paired[0]["tenant_id"] != "ten-1" || paired[1]["tenant_id"] != "ten-2" {
		t.Fatalf("equal counts should pair by index: %+v", paired)
	}
	one := tokenBankAudienceRows("hub-a", "ten-1")
	if len(one) != 1 || one[0]["hub_id"] != "hub-a" || one[0]["tenant_id"] != "ten-1" {
		t.Fatalf("one hub and one tenant is one row: %+v", one)
	}
	split := tokenBankAudienceRows("hub-a, hub-b", "ten-1")
	if len(split) != 3 || split[0]["tenant_id"] != "" || split[2]["hub_id"] != "" || split[2]["tenant_id"] != "ten-1" {
		t.Fatalf("unequal lists are hub-only and tenant-only rows: %+v", split)
	}
	if _, _, err := tokenBankAccessBody("private", "", "  "); err == nil {
		t.Fatal("a private share with no ids should be rejected")
	}
	visibility, rows, err := tokenBankAccessBody("", "hub-a", "")
	if err != nil || visibility != "public" || len(rows) != 1 {
		t.Fatalf("empty visibility is public, rows=%+v err=%v", rows, err)
	}
}

func TestTokenBankVisibilityRowsKeepPairs(t *testing.T) {
	raw := []byte(`[{"hub_id":"hub-a","tenant_id":"ten-1"},{"hub_id":"hub-b"},{"tenant_id":"ten-2"},{"hub_id":" ","tenant_id":" "}]`)
	var decoded []TokenBankVisibilityAudience
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	visibility, rows, err := tokenBankVisibilityRows("private", decoded)
	if err != nil || visibility != "private" || len(rows) != 3 {
		t.Fatalf("visibility=%s rows=%+v err=%v", visibility, rows, err)
	}
	if rows[0]["hub_id"] != "hub-a" || rows[0]["tenant_id"] != "ten-1" {
		t.Fatalf("a paired grant must stay one row: %+v", rows[0])
	}
	if rows[1]["hub_id"] != "hub-b" || rows[1]["tenant_id"] != "" || rows[2]["hub_id"] != "" || rows[2]["tenant_id"] != "ten-2" {
		t.Fatalf("single-id rows = %+v", rows[1:])
	}
	if _, _, err := tokenBankVisibilityRows("private", nil); err == nil {
		t.Fatal("a private share with no rows should be rejected")
	}
	again, narrowed, err := tokenBankVisibilityRows("private", []TokenBankVisibilityAudience{
		{HubID: "Hub-A", TenantID: "Ten-1"},
		{HubID: "hub-a", TenantID: "ten-1"},
	})
	if err != nil || again != "private" || len(narrowed) != 1 || narrowed[0]["hub_id"] != "Hub-A" {
		t.Fatalf("case-only duplicates = %+v err=%v", narrowed, err)
	}
}

func TestTokenBankSeparateAudiencesDoNotPair(t *testing.T) {
	rows := tokenBankSeparateAudienceRows("hub-a, hub-b", "ten-1, ten-2")
	if len(rows) != 4 || rows[0]["hub_id"] != "hub-a" || rows[0]["tenant_id"] != "" || rows[1]["hub_id"] != "hub-b" {
		t.Fatalf("equal counts stay independent hub rows: %+v", rows)
	}
	if rows[2]["hub_id"] != "" || rows[2]["tenant_id"] != "ten-1" || rows[3]["tenant_id"] != "ten-2" {
		t.Fatalf("equal counts stay independent tenant rows: %+v", rows)
	}
	visibility, got, err := tokenBankAccessBodyMode("private", "hub-a", "ten-1", true)
	if err != nil || visibility != "private" || len(got) != 2 || got[0]["tenant_id"] != "" || got[1]["hub_id"] != "" || got[1]["tenant_id"] != "ten-1" {
		t.Fatalf("one hub and one tenant stay two rows: visibility=%s rows=%+v err=%v", visibility, got, err)
	}
}

func TestMergeLocalTokenBankAudiences(t *testing.T) {
	remote := map[string]interface{}{
		"hubs": []interface{}{
			map[string]interface{}{"id": "Hub-B", "name": "Beta"},
		},
		"tenants": []interface{}{
			map[string]interface{}{"id": "ten-1", "name": "市场"},
		},
	}
	got := mergeLocalTokenBankAudiences(remote, "hub-b", "ten-local", "本机")
	hubs := got["hubs"].([]map[string]interface{})
	if len(hubs) != 1 || hubs[0]["id"] != "Hub-B" || hubs[0]["name"] != "Beta" {
		t.Fatalf("existing hub must not be duplicated: %+v", hubs)
	}
	tenants := got["tenants"].([]map[string]interface{})
	if len(tenants) != 2 || tenants[1]["id"] != "ten-local" || tenants[1]["name"] != "本机" || tenants[1]["hub_id"] != "hub-b" {
		t.Fatalf("local tenant = %+v", tenants)
	}
	if _, ok := remote["hubs"].([]interface{}); !ok || len(remote["hubs"].([]interface{})) != 1 {
		t.Fatalf("merge must not rewrite the caller map: %+v", remote)
	}

	localOnly := mergeLocalTokenBankAudiences(nil, " hub-local ", " ten-local ", "  ")
	localHubs := localOnly["hubs"].([]map[string]interface{})
	localTenants := localOnly["tenants"].([]map[string]interface{})
	if len(localHubs) != 1 || localHubs[0]["id"] != "hub-local" || localHubs[0]["name"] != "" {
		t.Fatalf("local hub = %+v", localHubs)
	}
	if len(localTenants) != 1 || localTenants[0]["id"] != "ten-local" || localTenants[0]["name"] != "" || localTenants[0]["hub_id"] != "hub-local" {
		t.Fatalf("local tenant without a name = %+v", localTenants)
	}
}

func TestTokenBankAudienceListFallsBackOnlyWhenTheRouteIsMissing(t *testing.T) {
	missing := &tokenBankHTTPError{Status: http.StatusNotFound, Detail: "404 page not found"}
	got, err := tokenBankAudienceListOrLocal(nil, missing, "hub-local", "ten-local", "本机")
	if err != nil {
		t.Fatalf("404 with a local hub should fall back: %v", err)
	}
	if len(got["hubs"].([]map[string]interface{})) != 1 || len(got["tenants"].([]map[string]interface{})) != 1 {
		t.Fatalf("local fallback = %+v", got)
	}
	if _, err := tokenBankAudienceListOrLocal(nil, missing, "", "", ""); err == nil {
		t.Fatal("404 without a local hub or tenant should stay an error")
	}
	unavailable := &tokenBankHTTPError{Status: http.StatusInternalServerError, Detail: "token bank unavailable"}
	if _, err := tokenBankAudienceListOrLocal(nil, unavailable, "hub-local", "ten-local", "本机"); err == nil || !strings.Contains(err.Error(), "token bank unavailable") {
		t.Fatalf("a server error must stay visible, err=%v", err)
	}
	if got := unavailable.Error(); got != "HubCenter rejected the request: token bank unavailable" {
		t.Fatalf("status error text = %q", got)
	}
}

func TestTokenBankAudienceLimit(t *testing.T) {
	var hubs strings.Builder
	for i := 0; i < tokenBankMaxAudiences+1; i++ {
		if i > 0 {
			hubs.WriteByte(',')
		}
		fmt.Fprintf(&hubs, "hub-%d", i)
	}
	if _, _, err := tokenBankAccessBodyMode("private", hubs.String(), "", true); err == nil {
		t.Fatal("33 hubs should be rejected before the request is sent")
	}
	hubs.Reset()
	for i := 0; i < tokenBankMaxAudiences; i++ {
		if i > 0 {
			hubs.WriteByte(',')
		}
		fmt.Fprintf(&hubs, "hub-%d", i)
	}
	visibility, rows, err := tokenBankAccessBodyMode("private", hubs.String(), "", true)
	if err != nil || visibility != "private" || len(rows) != tokenBankMaxAudiences {
		t.Fatalf("32 hubs should be accepted, rows=%d err=%v", len(rows), err)
	}
}
