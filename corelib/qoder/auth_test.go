package qoder

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPKCEChallengeMatchesVerifier(t *testing.T) {
	verifier, err := pkceVerifier()
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	if len(verifier) < 43 || len(verifier) > 129 {
		t.Fatalf("verifier length %d outside RFC window", len(verifier))
	}
	for _, r := range verifier {
		if !strings.ContainsRune(pkceAlphabet, r) {
			t.Fatalf("verifier contains non-alphabet rune %q", r)
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != pkceChallenge(verifier) {
		t.Fatalf("challenge mismatch")
	}
}

func TestAuthURLCarriesPKCEAndClientID(t *testing.T) {
	login, err := StartLogin("machine-1")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	raw := login.AuthURL(CNProfile())
	if !strings.HasPrefix(raw, "https://qoder.cn/device/selectAccounts?") {
		t.Fatalf("unexpected auth URL %q", raw)
	}
	for _, key := range []string{"challenge", "challenge_method=S256", "nonce", "machine_id=machine-1", "client_id=" + ClientIDCN} {
		if !strings.Contains(raw, key) {
			t.Fatalf("auth URL missing %s: %s", key, raw)
		}
	}
}

func TestPollUntilAuthorizedReturnsToken(t *testing.T) {
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		nonce := r.URL.Query().Get("nonce")
		verifier := r.URL.Query().Get("verifier")
		if nonce == "" || verifier == "" {
			t.Errorf("poll missing pkce params")
		}
		if polls < 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"tok-1","user_id":"u1","user_name":"n1","expires_at":"2999999999"}`))
	}))
	defer server.Close()

	profile := CNProfile()
	profile.OpenAPIBase = server.URL
	login, err := StartLogin("machine-1")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	token, err := PollUntilAuthorized(context.Background(), profile, login)
	if err != nil {
		t.Fatalf("PollUntilAuthorized: %v", err)
	}
	if token.AccessToken != "tok-1" || token.UserID != "u1" || token.ExpiresAt != 2999999999 {
		t.Fatalf("unexpected token %+v", token)
	}
	if polls != 3 {
		t.Fatalf("expected 3 polls, got %d", polls)
	}
}

func TestCatalogParse(t *testing.T) {
	body := []byte(`{"assistant":[{"key":"qwen3.8-max","display_name":"Qwen 3.8 Max","is_default":true,"enable":1},{"key":"other","display_name":"Other"}],"byok_enterprise":[]}`)
	models, defaultKey, err := parseCatalog(body)
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	if len(models) != 2 || defaultKey != "qwen3.8-max" {
		t.Fatalf("unexpected parse: models=%d default=%q", len(models), defaultKey)
	}
}

func TestEditionProfiles(t *testing.T) {
	if p, ok := ProfileByEdition("cn"); !ok || p.ClientID != ClientIDCN || p.WebOrigin != "https://qoder.cn" {
		t.Fatalf("cn profile: %v %v", p, ok)
	}
	if p, ok := ProfileByEdition("GLOBAL"); !ok || p.ClientID != ClientIDGlobal || p.WebOrigin != "https://qoder.com" {
		t.Fatalf("global profile: %v %v", p, ok)
	}
	if p, ok := ProfileByEdition("nope"); ok {
		t.Fatalf("unknown edition resolved: %v", p)
	}
	if p, ok := ProfileByName("Qoder 国内版"); !ok || p.StoreID != StoreCN {
		t.Fatalf("ProfileByName cn")
	}
	if p, ok := ProfileByName("Qoder 国际版"); !ok || p.StoreID != StoreGlobal {
		t.Fatalf("ProfileByName global")
	}
}
