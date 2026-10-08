package qoder

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fastPollWait removes the real one-second sleep from poll-loop tests.
func fastPollWait(t *testing.T) {
	t.Helper()
	previous := pollWait
	pollWait = time.Millisecond
	t.Cleanup(func() { pollWait = previous })
}

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
	cases := []struct {
		name    string
		profile Profile
	}{
		{"cn", CNProfile()},
		{"global", GlobalProfile()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			login, err := StartLogin("machine-1")
			if err != nil {
				t.Fatalf("StartLogin: %v", err)
			}
			raw := login.AuthURL(tc.profile)
			if !strings.HasPrefix(raw, tc.profile.WebOrigin+"/device/selectAccounts?") {
				t.Fatalf("unexpected auth URL %q", raw)
			}
			// The official CLI emits the query as challenge, challenge_method,
			// nonce, machine_id, client_id; qoder.cn rejects device logins that
			// deviate, so both the presence and the order are part of the
			// contract.
			keys := strings.Split(strings.TrimPrefix(raw, tc.profile.WebOrigin+"/device/selectAccounts?"), "&")
			wantOrder := []string{"challenge=", "challenge_method=S256", "nonce=", "machine_id=machine-1", "client_id=" + DeviceClientID}
			if len(keys) != len(wantOrder) {
				t.Fatalf("auth URL has %d params, want %d: %s", len(keys), len(wantOrder), raw)
			}
			for i, prefix := range wantOrder {
				if !strings.HasPrefix(keys[i], prefix) {
					t.Fatalf("auth URL param %d = %s, want prefix %s (full: %s)", i, keys[i], prefix, raw)
				}
			}
		})
	}
}

func TestListModelsRequestContract(t *testing.T) {
	// The catalog request must mirror the official CLI's signed contract: the
	// wasm-prepared /algo/api/v2/model/list path with a COSY bearer plus the
	// Cosy-* family. A plain fetch is rejected upstream, so any drift in this
	// contract breaks the catalog again.
	var gotPath, gotUA, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chat":[{"key":"auto","display_name":"Auto","is_default":true,"enable":true},{"key":"qfmodel","display_name":"Qwen3.8-Flash","enable":true}],"assistant":[]}`))
	}))
	defer server.Close()

	profile := GlobalProfile()
	profile.InferBase = server.URL
	models, defaultKey, err := ListModels(context.Background(), profile, "01a11487-c7fa-7aab-b1b4-0ec70fcd5743", "tok-catalog")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if gotPath != "/algo/api/v2/model/list" {
		t.Fatalf("request path %q, want the signed /algo/api/v2/model/list", gotPath)
	}
	if gotUA != UserAgent {
		t.Fatalf("User-Agent %q", gotUA)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") {
		t.Fatalf("Authorization %q, want the wasm COSY bearer", gotAuth)
	}
	// The whole catalog parses end to end, and the default comes from the
	// enabled is_default entry regardless of scene order.
	if len(models) != 2 || defaultKey != "auto" {
		t.Fatalf("catalog parse: models=%d default=%q", len(models), defaultKey)
	}
}

func TestPollUntilAuthorizedReturnsToken(t *testing.T) {
	fastPollWait(t)
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

func TestPollExpiresInFallsBackToRelativeExpiry(t *testing.T) {
	fastPollWait(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"tok-2","expires_in":3600}`))
	}))
	defer server.Close()

	profile := GlobalProfile()
	profile.OpenAPIBase = server.URL
	login, err := StartLogin("machine-1")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	before := time.Now().Unix()
	token, err := PollUntilAuthorized(context.Background(), profile, login)
	if err != nil {
		t.Fatalf("PollUntilAuthorized: %v", err)
	}
	if token.ExpiresAt < before+3600 || token.ExpiresAt > before+3600+10 {
		t.Fatalf("expected relative expiry ~3600s from now, got %d (base %d)", token.ExpiresAt, before)
	}
}

func TestPollAcceptsNumericExpiresAt(t *testing.T) {
	fastPollWait(t)
	// The CLI's own parser treats expires_at as either a quoted string or a
	// bare number; a number must not break the whole payload decode.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"tok-3","expires_at":2999999999,"refresh_token_expires_at":"2999999999.5"}`))
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
	if token.ExpiresAt != 2999999999 || token.RefreshTokenExpiresAt != 2999999999 {
		t.Fatalf("unexpected expiry parse: %+v", token)
	}
}

func TestPollParsesRFC3339Expiry(t *testing.T) {
	fastPollWait(t)
	// The live upstream answers expires_at as an RFC3339 wall clock
	// ("2026-11-06T13:43:44Z"). Dropping it would leave ExpiresAt at 0, which
	// IsExpired treats as never-expiring — the token would silently stop
	// refreshing until a 401. The RFC3339 spelling must land on real unix
	// seconds.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"tok-4","expires_at":"2026-11-06T13:43:44Z","refresh_token_expires_at":"2027-10-02T13:43:44Z"}`))
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
	wantAccess, _ := time.Parse(time.RFC3339, "2026-11-06T13:43:44Z")
	wantRefresh, _ := time.Parse(time.RFC3339, "2027-10-02T13:43:44Z")
	if token.ExpiresAt != wantAccess.Unix() {
		t.Fatalf("expires_at = %d, want %d", token.ExpiresAt, wantAccess.Unix())
	}
	if token.RefreshTokenExpiresAt != wantRefresh.Unix() {
		t.Fatalf("refresh_token_expires_at = %d, want %d", token.RefreshTokenExpiresAt, wantRefresh.Unix())
	}
}

func TestRefreshParsesRFC3339Expiry(t *testing.T) {
	// The captured live refresh payload: device_token + rotated refresh token
	// with RFC3339 expiry stamps. Both values must survive the refresh so the
	// caller persists the rotation.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] != "drt-old" || body["machine_id"] != "machine-1" {
			t.Errorf("refresh body mismatch: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_token":"dt-new","refresh_token":"drt-new","token_type":"Bearer","expires_at":"2026-11-06T13:43:44Z","refresh_token_expires_at":"2027-10-02T13:43:44Z","created_at":"2026-10-07T13:43:44Z"}`))
	}))
	defer server.Close()

	profile := GlobalProfile()
	profile.OpenAPIBase = server.URL
	token, err := Refresh(context.Background(), profile, "drt-old", "machine-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if token.AccessToken != "dt-new" || token.RefreshToken != "drt-new" {
		t.Fatalf("refresh token pair: %+v", token)
	}
	wantAccess, _ := time.Parse(time.RFC3339, "2026-11-06T13:43:44Z")
	if token.ExpiresAt != wantAccess.Unix() {
		t.Fatalf("expires_at = %d, want %d", token.ExpiresAt, wantAccess.Unix())
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

func TestCatalogPrefersEnabledDefaultAcrossScenes(t *testing.T) {
	// byok sorts first alphabetically, but the default must come from the
	// enabled assistant default regardless of map iteration order.
	body := []byte(`{"byok_enterprise":[{"key":"ent-choice","display_name":"Ent","is_default":true,"enable":false},{"key":"ent-open","display_name":"EntOpen"}],"assistant":[{"key":"a-helper","display_name":"Helper"},{"key":"b-best","display_name":"Best","is_default":true,"enable":true}]}`)
	models, defaultKey, err := parseCatalog(body)
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	if len(models) != 4 {
		t.Fatalf("models=%d", len(models))
	}
	if defaultKey != "b-best" {
		t.Fatalf("default=%q, want b-best", defaultKey)
	}
	for _, m := range models {
		if m.Key == "ent-choice" {
			if m.Enabled {
				t.Fatalf("ent-choice enable=false should stay disabled")
			}
			// And it must never win the default slot.
			if defaultKey == "ent-choice" {
				t.Fatalf("disabled entry must not become the default")
			}
		}
	}
}

func TestCatalogFallsBackToFirstEnabledWhenNoDefault(t *testing.T) {
	body := []byte(`{"assistant":[{"key":"z-late","display_name":"Late"},{"key":"a-first","display_name":"First"}]}`)
	_, defaultKey, err := parseCatalog(body)
	if err != nil {
		t.Fatalf("parseCatalog: %v", err)
	}
	if defaultKey != "z-late" {
		// Catalog order decides the fallback, not the scene sort.
		t.Fatalf("default=%q, want z-late (server order preserved within a scene)", defaultKey)
	}
}

func TestEditionProfiles(t *testing.T) {
	// Both editions share the official device-login client id.
	if p, ok := ProfileByEdition("cn"); !ok || p.ClientID != DeviceClientID || p.WebOrigin != "https://qoder.cn" {
		t.Fatalf("cn profile: %v %v", p, ok)
	}
	if p, ok := ProfileByEdition("GLOBAL"); !ok || p.ClientID != DeviceClientID || p.WebOrigin != "https://qoder.com" {
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

func TestIsChatBaseURLMatchesSharedModelServer(t *testing.T) {
	if !IsChatBaseURL(ChatBase) || !IsChatBaseURL("https://api2-v2.qoder.sh/model/v1/") {
		t.Fatalf("chat base URLs should match")
	}
	if IsChatBaseURL("https://openapi.qoder.sh") || IsChatBaseURL("") {
		t.Fatalf("non-chat hosts must not match")
	}
	if CanonicalChatURL("https://api2-v2.qoder.sh/model/v1") != ChatBase {
		t.Fatalf("CanonicalChatURL should pin the canonical base")
	}
}
