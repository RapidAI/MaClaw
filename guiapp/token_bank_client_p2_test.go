package guiapp

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestTokenBankClientPresentsSessionToItsIssuer(t *testing.T) {
	preferredHits := 0
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredHits++
		http.Error(w, "preferred center", http.StatusInternalServerError)
	}))
	defer preferred.Close()
	issuerHits := 0
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuerHits++
		if r.Header.Get("Authorization") != "Bearer bank-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance":1}`))
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        preferred.URL,
		RemoteHubCenterURLs:       []string{issuer.URL},
		SkillMarketSessionToken:   "bank-token",
		SkillMarketSessionBaseURL: issuer.URL,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := app.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/summary", nil)
	if err != nil {
		t.Fatalf("token bank: %v", err)
	}
	if result["balance"] != float64(1) && result["balance"] != 1 {
		t.Fatalf("summary = %#v", result)
	}
	if preferredHits != 0 || issuerHits != 1 {
		t.Fatalf("preferred hits = %d issuer hits = %d", preferredHits, issuerHits)
	}
}

func TestTokenBankCenterRequestURLKeepsTheCallOnTheIssuer(t *testing.T) {
	got, err := tokenBankCenterRequestURL("https://issuer.example", "/api/v1/token-bank/shares/a%2Fb?range=today")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://issuer.example/api/v1/token-bank/shares/a%2Fb?range=today" {
		t.Fatalf("url = %s", got)
	}
	for _, path := range []string{"https://evil.example/steal", "//evil.example/steal", "api/v1/token-bank/summary"} {
		if _, err := tokenBankCenterRequestURL("https://issuer.example", path); err == nil {
			t.Fatalf("path %q was accepted", path)
		}
	}
	if _, err := tokenBankCenterRequestURL("https://user:pass@issuer.example", "/api/v1/token-bank/summary"); err == nil {
		t.Fatal("userinfo URL was accepted")
	}
}

func TestTokenBankClientStaysOnIssuerWhenRedirected(t *testing.T) {
	preferredHits := 0
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredHits++
		http.Error(w, "preferred center", http.StatusInternalServerError)
	}))
	defer preferred.Close()
	issuerHits := 0
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuerHits++
		http.Redirect(w, r, preferred.URL+"/api/v1/token-bank/summary", http.StatusFound)
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        preferred.URL,
		RemoteHubCenterURLs:       []string{issuer.URL},
		SkillMarketSessionToken:   "bank-token",
		SkillMarketSessionBaseURL: issuer.URL,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/summary", nil); err == nil {
		t.Fatal("redirect was treated as a successful Token Bank response")
	}
	if preferredHits != 0 || issuerHits != 1 {
		t.Fatalf("preferred hits = %d issuer hits = %d", preferredHits, issuerHits)
	}
}

func TestTokenBankPublicKeyUsesSessionIssuer(t *testing.T) {
	restoreTokenBankPubKeyCache(t)
	preferredPEM := testTokenBankPEM(t)
	issuerPEM := testTokenBankPEM(t)
	thirdPEM := testTokenBankPEM(t)

	preferredHits := 0
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredHits++
		_, _ = w.Write(preferredPEM)
	}))
	defer preferred.Close()
	issuerHits := 0
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuerHits++
		if r.URL.Path != "/api/v1/crypto/pubkey" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(issuerPEM)
	}))
	defer issuer.Close()
	thirdHits := 0
	third := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		thirdHits++
		_, _ = w.Write(thirdPEM)
	}))
	defer third.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        preferred.URL,
		RemoteHubCenterURLs:       []string{issuer.URL, third.URL},
		SkillMarketSessionToken:   "bank-token",
		SkillMarketSessionBaseURL: issuer.URL,
	}); err != nil {
		t.Fatal(err)
	}
	// A pem cached before the issuer was recorded has no center and must not
	// be reused for a session that lives on another center.
	if err := os.MkdirAll(app.GetDataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.GetDataDir(), "hubcenter_pubkey.pem"), preferredPEM, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := app.tokenBankPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, issuerPEM) {
		t.Fatal("public key did not come from the session issuer")
	}
	cached, err := os.ReadFile(filepath.Join(app.GetDataDir(), tokenBankPubKeyCacheName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(cached, []byte(tokenBankPubKeyCachePrefix+issuer.URL+"\n")) {
		t.Fatalf("cache record = %q", cached)
	}
	if _, err := app.tokenBankPublicKey(); err != nil {
		t.Fatal(err)
	}
	if preferredHits != 0 || issuerHits != 1 {
		t.Fatalf("preferred hits = %d issuer hits = %d", preferredHits, issuerHits)
	}

	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		storeSkillMarketSession(cfg, "bank-token", third.URL)
	}); err != nil {
		t.Fatal(err)
	}
	switched, err := app.tokenBankPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(switched, thirdPEM) || preferredHits != 0 || issuerHits != 1 || thirdHits != 1 {
		t.Fatalf("switched key mismatch preferred=%d issuer=%d third=%d", preferredHits, issuerHits, thirdHits)
	}
}

func restoreTokenBankPubKeyCache(t *testing.T) {
	t.Helper()
	tokenBankPubKeyMu.Lock()
	var prevKey []byte
	if tokenBankPubKey != nil {
		prevKey = append([]byte(nil), tokenBankPubKey...)
	}
	prevBase := tokenBankPubKeyBase
	tokenBankPubKey = nil
	tokenBankPubKeyBase = ""
	tokenBankPubKeyMu.Unlock()
	t.Cleanup(func() {
		tokenBankPubKeyMu.Lock()
		tokenBankPubKey = prevKey
		tokenBankPubKeyBase = prevBase
		tokenBankPubKeyMu.Unlock()
	})
}

func TestTokenBankPubKeyCacheKeepsCenterAndKeyTogether(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tokenBankPubKeyCacheName)
	issuerPEM := testTokenBankPEM(t)
	otherPEM := testTokenBankPEM(t)
	if err := os.WriteFile(path, otherPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readTokenBankPubKeyCache(path, "https://issuer.example"); got != nil {
		t.Fatal("bare pem was accepted")
	}
	if err := writeTokenBankPubKeyCache(path, "https://issuer.example", issuerPEM); err != nil {
		t.Fatal(err)
	}
	if got := readTokenBankPubKeyCache(path, "https://other.example"); got != nil {
		t.Fatal("other center reused the issuer key")
	}
	if got := readTokenBankPubKeyCache(path, "https://issuer.example"); !bytes.Equal(got, issuerPEM) {
		t.Fatal("issuer pem mismatch")
	}
	if err := writeTokenBankPubKeyCache(path, "https://other.example", otherPEM); err != nil {
		t.Fatal(err)
	}
	if got := readTokenBankPubKeyCache(path, "https://issuer.example"); got != nil {
		t.Fatal("replaced cache still answered for the previous center")
	}
	if got := readTokenBankPubKeyCache(path, "https://other.example"); !bytes.Equal(got, otherPEM) {
		t.Fatal("replaced cache mismatch")
	}
}

func TestGiftClaimURLUsesTheSessionCenter(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        "https://preferred.example",
		RemoteHubCenterURLs:       []string{"https://issuer.example"},
		SkillMarketSessionToken:   "bank-token",
		SkillMarketSessionBaseURL: "https://issuer.example",
	}); err != nil {
		t.Fatal(err)
	}
	result := map[string]interface{}{"code": "AB2CDEFGHJ"}
	app.annotateGiftClaimLinks(result)
	if result["claim_url"] != "https://issuer.example/c/AB2CDEFGHJ" {
		t.Fatalf("claim_url = %#v", result["claim_url"])
	}
	if result["deep_link"] != "maclaw://credit/AB2CDEFGHJ" {
		t.Fatalf("deep_link = %#v", result["deep_link"])
	}

	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteHubCenterURLs = []string{"https://user:pass@issuer.example"}
		cfg.SkillMarketSessionToken = "bank-token"
		cfg.SkillMarketSessionBaseURL = "https://user:pass@issuer.example"
	}); err != nil {
		t.Fatal(err)
	}
	secret := map[string]interface{}{"code": "AB2CDEFGHJ"}
	app.annotateGiftClaimLinks(secret)
	if _, ok := secret["claim_url"]; ok {
		t.Fatalf("claim_url = %#v", secret["claim_url"])
	}
	if secret["deep_link"] != "maclaw://credit/AB2CDEFGHJ" {
		t.Fatalf("deep_link = %#v", secret["deep_link"])
	}
}

func testTokenBankPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

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

func saveTokenBankPrincipal(t *testing.T, app *App, preferred string, centers []string, issuer, token string) {
	t.Helper()
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        preferred,
		RemoteHubCenterURLs:       centers,
		RemoteHubID:               "hub-test",
		RemoteUserID:              "u_current",
		RemoteMobile:              "13301168516",
		RemoteMachineID:           "m_test",
		RemoteViewerToken:         "viewer-token",
		SkillMarketSessionToken:   token,
		SkillMarketSessionBaseURL: issuer,
	}); err != nil {
		t.Fatal(err)
	}
}

func writeTokenBankLogin(w http.ResponseWriter, token string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"session_token": token,
		"user_id":       "u_current",
	})
}

func TestTokenBankDerivesSessionBeforeThePublicKey(t *testing.T) {
	restoreTokenBankPubKeyCache(t)
	issuerPEM := testTokenBankPEM(t)
	minted := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	var preferredHits atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		preferredHits.Add(1)
		if r.URL.Path != "/api/v1/auth/machine-login" {
			t.Errorf("preferred path = %s", r.URL.Path)
		}
		http.Error(w, "preferred down", http.StatusServiceUnavailable)
	}))
	defer preferred.Close()
	var issuerLogin atomic.Int32
	var issuerKey atomic.Int32
	var issuerSummary atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/machine-login":
			issuerLogin.Add(1)
			writeTokenBankLogin(w, minted)
		case "/api/v1/crypto/pubkey":
			issuerKey.Add(1)
			_, _ = w.Write(issuerPEM)
		case "/api/v1/token-bank/summary":
			issuerSummary.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+minted {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"balance":1}`))
		default:
			t.Errorf("issuer path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	saveTokenBankPrincipal(t, app, preferred.URL, []string{issuer.URL}, "", "")
	got, err := app.tokenBankPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, issuerPEM) {
		t.Fatal("public key did not come from the center that minted the session")
	}
	if _, err := app.tokenBankPublicKey(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/summary", nil); err != nil {
		t.Fatal(err)
	}
	if preferredHits.Load() != 1 || issuerLogin.Load() != 1 || issuerKey.Load() != 1 || issuerSummary.Load() != 1 {
		t.Fatalf("preferred=%d login=%d key=%d summary=%d", preferredHits.Load(), issuerLogin.Load(), issuerKey.Load(), issuerSummary.Load())
	}
}

func TestTokenBankRemintsOnceAfterUnauthorized(t *testing.T) {
	stale := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	var preferredLogin atomic.Int32
	var preferredOther atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/machine-login" {
			preferredLogin.Add(1)
			http.Error(w, "preferred down", http.StatusServiceUnavailable)
			return
		}
		preferredOther.Add(1)
		http.Error(w, "preferred center", http.StatusInternalServerError)
	}))
	defer preferred.Close()
	var issuerLogin atomic.Int32
	var auths []string
	var authMu sync.Mutex
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/machine-login":
			issuerLogin.Add(1)
			writeTokenBankLogin(w, "minted-after-401")
		case "/api/v1/token-bank/summary":
			authMu.Lock()
			auths = append(auths, r.Header.Get("Authorization"))
			attempt := len(auths)
			authMu.Unlock()
			if attempt == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"HubCenter sign-in required"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"balance":1}`))
		default:
			t.Errorf("issuer path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	saveTokenBankPrincipal(t, app, preferred.URL, []string{issuer.URL}, issuer.URL, stale)
	result, err := app.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/summary", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["balance"] != float64(1) {
		t.Fatalf("summary = %#v", result)
	}
	authMu.Lock()
	gotAuths := append([]string(nil), auths...)
	authMu.Unlock()
	if len(gotAuths) != 2 || gotAuths[0] != "Bearer "+stale || gotAuths[1] != "Bearer minted-after-401" {
		t.Fatalf("authorizations = %#v", gotAuths)
	}
	if preferredLogin.Load() != 1 || preferredOther.Load() != 0 || issuerLogin.Load() != 1 {
		t.Fatalf("preferred login=%d other=%d issuer login=%d", preferredLogin.Load(), preferredOther.Load(), issuerLogin.Load())
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if saved.SkillMarketSessionToken != "minted-after-401" || saved.SkillMarketSessionBaseURL != issuer.URL {
		t.Fatalf("saved token=%q base=%q", saved.SkillMarketSessionToken, saved.SkillMarketSessionBaseURL)
	}
}

func TestTokenBankDoesNotReplayCenterBoundBodyOnAnotherCenter(t *testing.T) {
	stale := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	var preferredLogin atomic.Int32
	var preferredShare atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/machine-login" {
			preferredLogin.Add(1)
			writeTokenBankLogin(w, "minted-on-preferred")
			return
		}
		preferredShare.Add(1)
		http.Error(w, "ciphertext arrived on the wrong center", http.StatusInternalServerError)
	}))
	defer preferred.Close()
	var issuerShare atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/machine-login":
			t.Errorf("issuer login ran after the preferred center had already minted")
			http.Error(w, "unexpected login", http.StatusInternalServerError)
		case "/api/v1/token-bank/shares":
			issuerShare.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+stale {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"HubCenter sign-in required"}`))
		default:
			t.Errorf("issuer path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	saveTokenBankPrincipal(t, app, preferred.URL, []string{issuer.URL}, issuer.URL, stale)
	_, err := app.tokenBankClientCall(http.MethodPost, "/api/v1/token-bank/shares", map[string]any{
		"encrypted_payload": "cipher-for-issuer",
	}, issuer.URL)
	if !errors.Is(err, errTokenBankCenterMoved) {
		t.Fatalf("err = %v", err)
	}
	if issuerShare.Load() != 1 || preferredShare.Load() != 0 || preferredLogin.Load() != 1 {
		t.Fatalf("issuer share=%d preferred share=%d preferred login=%d", issuerShare.Load(), preferredShare.Load(), preferredLogin.Load())
	}
	saved, loadErr := app.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.SkillMarketSessionToken != "minted-on-preferred" || saved.SkillMarketSessionBaseURL != preferred.URL {
		t.Fatalf("saved token=%q base=%q", saved.SkillMarketSessionToken, saved.SkillMarketSessionBaseURL)
	}
}

func TestTokenBankRetriesCenterBoundBodyOnTheSameCenter(t *testing.T) {
	stale := testSkillMarketSignedToken("u_current", "phone:13301168516", time.Now().Add(time.Hour))
	var logins atomic.Int32
	var bodies [][]byte
	var auths []string
	var mu sync.Mutex
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/machine-login":
			logins.Add(1)
			writeTokenBankLogin(w, "minted-same-center")
		case "/api/v1/token-bank/shares":
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Errorf("read body: %v", readErr)
			}
			mu.Lock()
			bodies = append(bodies, body)
			auths = append(auths, r.Header.Get("Authorization"))
			attempt := len(auths)
			mu.Unlock()
			if attempt == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"HubCenter sign-in required"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"share-1"}`))
		default:
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	saveTokenBankPrincipal(t, app, issuer.URL, nil, issuer.URL, stale)
	result, err := app.tokenBankClientCall(http.MethodPost, "/api/v1/token-bank/shares", map[string]any{
		"encrypted_payload": "cipher-for-issuer",
	}, issuer.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result["id"] != "share-1" {
		t.Fatalf("result = %#v", result)
	}
	mu.Lock()
	gotBodies := append([][]byte(nil), bodies...)
	gotAuths := append([]string(nil), auths...)
	mu.Unlock()
	if logins.Load() != 1 || len(gotBodies) != 2 || !bytes.Equal(gotBodies[0], gotBodies[1]) {
		t.Fatalf("logins=%d bodies=%d", logins.Load(), len(gotBodies))
	}
	if len(gotAuths) != 2 || gotAuths[0] != "Bearer "+stale || gotAuths[1] != "Bearer minted-same-center" {
		t.Fatalf("authorizations = %#v", gotAuths)
	}
}

func TestTokenBankReusesAdoptedSessionForTheKeyAndTheCall(t *testing.T) {
	restoreTokenBankPubKeyCache(t)
	issuerPEM := testTokenBankPEM(t)
	adopted := testSkillMarketSignedToken("u_adopted_market", "phone:13301168516", time.Now().Add(time.Hour))
	var preferredLogin atomic.Int32
	preferred := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/machine-login" {
			t.Errorf("preferred path = %s", r.URL.Path)
		}
		preferredLogin.Add(1)
		http.Error(w, "preferred down", http.StatusServiceUnavailable)
	}))
	defer preferred.Close()
	var issuerLogin atomic.Int32
	var issuerKey atomic.Int32
	var issuerSummary atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/machine-login":
			issuerLogin.Add(1)
			writeTokenBankLogin(w, adopted)
		case "/api/v1/crypto/pubkey":
			issuerKey.Add(1)
			_, _ = w.Write(issuerPEM)
		case "/api/v1/token-bank/summary":
			issuerSummary.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+adopted {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"balance":1}`))
		default:
			t.Errorf("issuer path = %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()

	app := &App{testHomeDir: t.TempDir()}
	saveTokenBankPrincipal(t, app, preferred.URL, []string{issuer.URL}, "", "")
	key, keyCenter, err := app.tokenBankSessionPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, issuerPEM) || keyCenter != issuer.URL {
		t.Fatalf("key center = %s", keyCenter)
	}
	if _, err := app.tokenBankClientCall(http.MethodGet, "/api/v1/token-bank/summary", nil, keyCenter); err != nil {
		t.Fatal(err)
	}
	if _, err := app.tokenBankClientDo(http.MethodGet, "/api/v1/token-bank/summary", nil); err != nil {
		t.Fatal(err)
	}
	if preferredLogin.Load() != 1 || issuerLogin.Load() != 1 || issuerKey.Load() != 1 || issuerSummary.Load() != 2 {
		t.Fatalf("preferred=%d login=%d key=%d summary=%d", preferredLogin.Load(), issuerLogin.Load(), issuerKey.Load(), issuerSummary.Load())
	}
}
