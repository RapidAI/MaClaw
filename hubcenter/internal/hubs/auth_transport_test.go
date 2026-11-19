package hubs

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestSkillMarketAuthClientRejectsCleartextPublicHub(t *testing.T) {
	t.Setenv(remote.HubAllowHTTPEnv, "")
	resp, err := skillMarketAuthHTTPClient.Get("http://192.0.2.1/api/center/skillmarket-authenticate")
	if err == nil {
		resp.Body.Close()
		t.Fatal("public cleartext verification request was attempted")
	}
	if !strings.Contains(err.Error(), remote.HubAllowHTTPEnv) {
		t.Fatalf("error = %v, want cleartext policy rejection", err)
	}
}
