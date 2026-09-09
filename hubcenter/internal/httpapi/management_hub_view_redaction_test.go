package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestAdminHubViewNeverLeaksHubSecretHash(t *testing.T) {
	v := adminHubView{HubInstance: &store.HubInstance{ID: "hub-1", BaseURL: "https://x", HubSecretHash: "SECRET-HASH"}}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET-HASH") || strings.Contains(string(b), "hub_secret_hash") {
		t.Fatalf("secret leaked: %s", b)
	}
	if !strings.Contains(string(b), `"base_url":"https://x"`) {
		t.Fatalf("legitimate fields lost: %s", b)
	}
}
