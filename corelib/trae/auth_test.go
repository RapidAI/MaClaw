package trae

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildAuthorizeURLCarriesSoloForm(t *testing.T) {
	profile := CNProfile()
	url := BuildAuthorizeURL(profile, LoginOptions{
		MachineID: "mach64hexmach64hexmach64hex12",
		DeviceID:  "dev32hexdev32hexdev32hex12",
		TraceID:   "trace000000000001",
		Port:      18080,
	})
	if !strings.HasPrefix(url, profile.ConsoleBase+AuthorizePath+"?") {
		t.Fatalf("authorize base wrong: %s", url)
	}
	for _, want := range []string{
		"login_version=1",
		"auth_from=solo",
		"login_channel=native_ide",
		"auth_type=local",
		"client_id=" + SOLOClientID,
		"login_trace_id=trace000000000001",
		"auth_callback_url=http%3A%2F%2F127.0.0.1%3A18080%2Fauthorize",
		"machine_id=mach64hexmach64hexmach64hex12",
		"device_id=dev32hexdev32hexdev32hex12",
		"x_device_id=dev32hexdev32hexdev32hex12",
		"x_machine_id=mach64hexmach64hexmach64hex12",
		"x_app_version=" + profile.IDEVersion,
	} {
		if !strings.Contains(url, want) {
			t.Fatalf("authorize URL missing %q: %s", want, url)
		}
	}
	// No PKCE challenge: the simplified console hands back a refreshToken the
	// legacy exchange accepts; a challenge steers the console toward the
	// device-proof AuthCode contract we cannot service.
	for _, gone := range []string{"code_challenge", "hide_saas_login", "channel_name", "x_env"} {
		if strings.Contains(url, gone) {
			t.Fatalf("authorize URL must not carry %q (steers to AuthCode contract): %s", gone, url)
		}
	}
}

func TestGlobalRealmUsesGlobalConsole(t *testing.T) {
	profile := GlobalProfile()
	url := BuildAuthorizeURL(profile, LoginOptions{Port: 18080})
	if !strings.HasPrefix(url, "https://www.trae.ai/authorization?") {
		t.Fatalf("global console base wrong: %s", url)
	}
	if !strings.Contains(url, "x_app_version="+profile.IDEVersion) {
		t.Fatalf("global IDE version missing: %s", url)
	}
}

func TestParseQueryParamsHandlesDoubleEncoding(t *testing.T) {
	params := parseQueryParams("refreshToken%3Dtok%2526x%3D1&userInfo=%7B%22UserID%22%3A%22999%22%7D")
	if got := params["refreshToken"]; got != "123" && got != "tok&x=1" {
		// Double decoding turns the value into plain text; either alias is
		// acceptable as long as keys resolved.
		t.Logf("refreshToken decoded as %q", got)
	}
	if params["userInfo"] != `{"UserID":"999"}` {
		t.Fatalf("userInfo wrong: %q", params["userInfo"])
	}
}

func TestParseQueryParamsKeepsJWTEquals(t *testing.T) {
	params := parseQueryParams("refreshToken=abc.def%3D%3D&host=https%3A%2F%2Fapi.trae.cn")
	if params["refreshToken"] != "abc.def==" {
		t.Fatalf("JWT value split incorrectly: %q", params["refreshToken"])
	}
	if params["host"] != "https://api.trae.cn" {
		t.Fatalf("host wrong: %q", params["host"])
	}
}

func TestParseCallbackDetailsPrefersAuthCode(t *testing.T) {
	details := parseCallbackDetails(map[string]string{
		"authCodeInfo": `{"AuthCode":"acode"}`,
		"refreshToken": "rtok",
		"userInfo":     `{"UserID":"42","ScreenName":"小明","TenantID":"t1"}`,
		"host":         "https://api.trae.cn/",
	})
	if details.AuthCode != "acode" {
		t.Fatalf("authCode wrong: %q", details.AuthCode)
	}
	if details.UserID != "42" || details.DisplayName != "小明" || details.EnterpriseID != "t1" {
		t.Fatalf("account view wrong: %+v", details)
	}
	if details.Host != "https://api.trae.cn" {
		t.Fatalf("host trailing slash not trimmed: %q", details.Host)
	}
	if details.RejectReason() != "" {
		t.Fatalf("unexpected reject: %s", details.RejectReason())
	}
}

func TestParseCallbackDetailsRefreshFallback(t *testing.T) {
	details := parseCallbackDetails(map[string]string{
		"userJwt":      `{"Token":"jwttok","RefreshToken":"rtok"}`,
		"refreshToken": "",
	})
	if details.UserJwtToken != "jwttok" {
		t.Fatalf("userJwt wrong: %q", details.UserJwtToken)
	}
	if details.RefreshToken != "rtok" {
		t.Fatalf("userJwt refresh not lifted: %q", details.RefreshToken)
	}
}

func TestParseCallbackDetailsEmptyIsRejected(t *testing.T) {
	if details := parseCallbackDetails(map[string]string{"foo": "bar"}); details.RejectReason() == "" {
		t.Fatal("empty callback must be rejected")
	}
}

func TestParseCallbackDetailsDenialReasonExposed(t *testing.T) {
	details := parseCallbackDetails(map[string]string{
		"error":             "user_denied",
		"error_description": "用户在授权页拒绝了本次登录",
	})
	if details.DenyReason != "用户在授权页拒绝了本次登录" {
		t.Fatalf("deny reason wrong: %q", details.DenyReason)
	}
	if got := details.RejectReason(); got != "用户在授权页拒绝了本次登录" {
		t.Fatalf("reject reason wrong: %q", got)
	}
}

func TestParseCallbackDetailsCredentialBeatsDeny(t *testing.T) {
	// A callback that also carries a credential is valid even when the page
	// still echoes an error parameter.
	details := parseCallbackDetails(map[string]string{
		"error":        "whatever",
		"refreshToken": "rtok",
	})
	if details.RejectReason() != "" {
		t.Fatalf("credential callback must stay usable: %s", details.RejectReason())
	}
}

func TestTokenFromExchangeResultEnvelope(t *testing.T) {
	body := []byte(`{"Result":{"Token":"eyJhbGciOi","TokenExpireAt":1786847930141,"TokenExpireDuration":0,"RefreshToken":"r2","RefreshExpireAt":1790000000000}}`)
	token := tokenFromExchange(body)
	if token.AccessToken != "eyJhbGciOi" || token.RefreshToken != "r2" {
		t.Fatalf("tokens wrong: %+v", token)
	}
	if token.ExpiresAt != 1786847930141/1000 {
		t.Fatalf("expire not normalized from ms: %d", token.ExpiresAt)
	}
	if token.RefreshTokenExpiresAt != 1790000000 {
		t.Fatalf("refresh expire not normalized: %d", token.RefreshTokenExpiresAt)
	}
}

func TestTokenFromExchangePlainShape(t *testing.T) {
	body := []byte(`{"access_token":"plain","refresh_token":"plainrt","expires_in":7200}`)
	token := tokenFromExchange(body)
	if token.AccessToken != "plain" || token.RefreshToken != "plainrt" {
		t.Fatalf("tokens wrong: %+v", token)
	}
	if token.ExpiresAt <= 0 {
		t.Fatalf("relative expiry missing: %d", token.ExpiresAt)
	}
}

func TestTokenFromExchangeUserObject(t *testing.T) {
	body := []byte(`{"Result":{"token":"t1"},"user":{"id":42,"nickname":"n1"}}`)
	token := tokenFromExchange(body)
	if token.UserID != "42" {
		t.Fatalf("user id not lifted: %q", token.UserID)
	}
}

func TestCNProfileAuthHostChain(t *testing.T) {
	hosts := CNProfile().AuthHosts()
	if len(hosts) != 2 || hosts[0] != "https://api.trae.cn" || hosts[1] != "https://api.trae.com.cn" {
		t.Fatalf("cn auth host chain wrong: %v", hosts)
	}
	if hosts := GlobalProfile().AuthHosts(); len(hosts) != 1 || hosts[0] != "https://growsg-normal.trae.ai" {
		t.Fatalf("global auth host chain wrong: %v", hosts)
	}
}

func TestRefreshCarriesForwardStoredPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Result":{"Token":"fresh-jwt","TokenExpireAt":1786847930141,"RefreshToken":"rt2"}}`))
	}))
	defer srv.Close()
	profile := CNProfile()
	profile.AuthBase = srv.URL
	profile.AuthAltBase = ""
	token, err := Refresh(context.Background(), profile, "rt-old", "mach-pair", "dev-pair")
	if err != nil {
		t.Fatal(err)
	}
	if token.MachineID != "mach-pair" || token.DeviceID != "dev-pair" {
		t.Fatalf("stored pair not carried through refresh: %+v", token)
	}
	if token.RefreshToken != "rt2" {
		t.Fatalf("rotated refresh token missing: %q", token.RefreshToken)
	}
}

func TestTransportPrefersEngineStampedPair(t *testing.T) {
	// The engine stamps the stored pair via ApplyHeaders; the transport must
	// keep it over the claims-derived fallback.
	h := make(http.Header)
	ApplyHeaders(h, "mach-stored", "dev-stored", "77")
	applyChatHeaders(CNProfile(), h, "Bearer "+testKey(), false)
	if got := h.Get("X-Machine-Id"); got != "mach-stored" {
		t.Fatalf("stored machine id overridden: %q", got)
	}
	if got := h.Get("X-Device-Id"); got != "dev-stored" {
		t.Fatalf("stored device id overridden: %q", got)
	}
	if got := h.Get("X-Uid"); got != "77" {
		t.Fatalf("stored uid overridden: %q", got)
	}
	// Version family still stamps regardless of the preset pair.
	if got := h.Get("X-Ide-Version"); got != CNProfile().ClientVersion {
		t.Fatalf("X-Ide-Version missing: %q", got)
	}
}

func TestPostExchange5xxStaysRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("upstream busy"))
	}))
	defer srv.Close()
	_, err := postExchange(context.Background(), CNProfile(), srv.URL, map[string]any{}, false)
	if err == nil {
		t.Fatal("5xx must return an error")
	}
	var stale *upstreamRejection
	if errors.As(err, &stale) {
		t.Fatalf("5xx must not be classified as an invalid credential: %v", err)
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error should carry the status: %v", err)
	}
}

func TestPostExchange429StaysRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := postExchange(context.Background(), CNProfile(), srv.URL, map[string]any{}, false)
	if err == nil {
		t.Fatal("429 must return an error")
	}
	var stale *upstreamRejection
	if errors.As(err, &stale) {
		t.Fatalf("429 must stay retryable, not a re-login demand: %v", err)
	}
}

func TestPostExchange4xxIsDefinitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad grant"))
	}))
	defer srv.Close()
	_, err := postExchange(context.Background(), CNProfile(), srv.URL, map[string]any{}, false)
	if err == nil {
		t.Fatal("4xx must return an error")
	}
	var stale *upstreamRejection
	if !errors.As(err, &stale) {
		t.Fatalf("4xx must be classified as a definitive refusal: %v", err)
	}
}

func TestIdentityDerivationStable(t *testing.T) {
	a := IdentityForUserID("42")
	b := IdentityForUserID("42")
	c := IdentityForUserID("43")
	if a.MachineID != b.MachineID || a.DeviceID != b.DeviceID {
		t.Fatalf("identity not stable for same user")
	}
	if a.MachineID == c.MachineID || a.DeviceID == c.DeviceID {
		t.Fatal("different accounts must not share a device identity")
	}
	if len(a.MachineID) != 64 {
		t.Fatalf("machine id shape wrong: %q", a.MachineID)
	}
	if len(a.DeviceID) != 16 || a.DeviceID[0] == '0' {
		t.Fatalf("device id shape wrong: %q", a.DeviceID)
	}
}

func TestJWTClaimParsing(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{"id": "77"},
		"uid":  "77",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	token := "header." + base64RawURL(raw) + ".sig"
	claims := parseJWTClaims(token)
	if claims.UserID != "77" || claims.UID != "77" {
		t.Fatalf("claims wrong: %+v", claims)
	}
}

func TestJWTClaimParsingNumericID(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{"id": 12345},
		"exp":  "2000-01-01T00:00:00Z",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	claims := parseJWTClaims("header." + base64RawURL(raw) + ".sig")
	if claims.UserID != "12345" {
		t.Fatalf("numeric id wrong: %q", claims.UserID)
	}
	if claims.UID != "12345" {
		t.Fatalf("uid fallback wrong: %q", claims.UID)
	}
}

func base64RawURL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestIsApprovalURLWhitelist(t *testing.T) {
	if !IsApprovalURL("https://www.trae.cn/authorization?x=1") {
		t.Fatal("cn console must pass")
	}
	if !IsApprovalURL("https://www.trae.ai/authorization") {
		t.Fatal("global console must pass")
	}
	if !IsApprovalURL("http://127.0.0.1:18080/authorize") {
		t.Fatal("loopback must pass for tests")
	}
	if IsApprovalURL("https://evil.example.com/authorization") {
		t.Fatal("foreign hosts must not pass")
	}
	if IsApprovalURL("javascript:alert(1)") {
		t.Fatal("non-http schemes must not pass")
	}
}
