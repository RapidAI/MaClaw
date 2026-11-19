package httpapi

import (
	"context"
	"testing"
)

func trustCompanionOriginsForTest(t *testing.T) {
	t.Helper()
	previous := companionOriginTrusted
	companionOriginTrusted = func(context.Context, string) bool { return true }
	t.Cleanup(func() { companionOriginTrusted = previous })
}

func TestCompanionOriginWithoutTrustAuthorityFailsClosed(t *testing.T) {
	companionFederationClear()
	t.Cleanup(companionFederationClear)
	if companionOriginIsTrusted(context.Background(), "https://unknown-hub.example") {
		t.Fatal("origin was trusted without HubCenter or an explicit test override")
	}
	if !companionOriginIsTrusted(context.Background(), "http://127.0.0.1:9399") {
		t.Fatal("loopback development peer was rejected without HubCenter")
	}
}

func TestCompanionFederationEndpointRequiresHTTPSOffLoopback(t *testing.T) {
	t.Setenv(companionFederationAllowHTTPEnv, "")
	for _, test := range []struct {
		endpoint string
		allowed  bool
	}{
		{endpoint: "https://hub.example/api/relay", allowed: true},
		{endpoint: "http://127.0.0.1:9399/api/relay", allowed: true},
		{endpoint: "http://[::1]:9399/api/relay", allowed: true},
		{endpoint: "http://localhost:9399/api/relay", allowed: true},
		{endpoint: "http://hub.example/api/relay"},
		{endpoint: "ftp://hub.example/api/relay"},
	} {
		err := companionFederationEndpoint(test.endpoint)
		if (err == nil) != test.allowed {
			t.Errorf("endpoint %q error = %v, allowed = %t", test.endpoint, err, test.allowed)
		}
	}

	t.Setenv(companionFederationAllowHTTPEnv, "1")
	if err := companionFederationEndpoint("http://hub.example/api/relay"); err != nil {
		t.Fatalf("explicit legacy HTTP opt-in was rejected: %v", err)
	}
}
