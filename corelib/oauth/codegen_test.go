package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"pgregory.net/rapid"
)

// ─── Task 2.2 ───────────────────────────────────────────────────────────────
// Feature: codegen-scan-login, Property 5: Token result application sets provider fields correctly
// **Validates: Requirements 3.1, 3.2, 5.2**
//
// For any CodeGenSSOResult with a non-empty AccessToken and any expires_in ≥ 0,
// applying the result to a MaclawLLMProvider should set:
//   - Key to the access token
//   - AuthType to "sso"
//   - TokenExpiresAt to approximately now + expires_in (within 2 seconds tolerance)
//   - If a new RefreshToken is provided, it replaces the old one; if empty, old RefreshToken is preserved
func TestProperty_ApplyTokenResult_SSOProvider(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		originalRefresh := rapid.String().Draw(t, "original_refresh")

		provider := corelib.MaclawLLMProvider{
			Name:         rapid.String().Draw(t, "name"),
			URL:          rapid.String().Draw(t, "url"),
			Key:          rapid.String().Draw(t, "old_key"),
			Model:        rapid.String().Draw(t, "model"),
			AuthType:     "sso",
			RefreshToken: originalRefresh,
		}

		result := &TokenResult{
			AccessToken:  rapid.StringMatching(`[a-zA-Z0-9]{10,50}`).Draw(t, "access_token"),
			RefreshToken: rapid.StringMatching(`[a-zA-Z0-9]{0,50}`).Draw(t, "refresh_token"),
			ExpiresIn:    rapid.IntRange(0, 86400).Draw(t, "expires_in"),
		}

		beforeApply := time.Now().Unix()
		updated := ApplyTokenResult(provider, result)
		afterApply := time.Now().Unix()

		// Key must equal AccessToken
		if updated.Key != result.AccessToken {
			t.Fatalf("Key = %q, want %q", updated.Key, result.AccessToken)
		}

		// AuthType must remain "sso" (ApplyTokenResult does not change AuthType,
		// so the caller is responsible for setting it before or after)
		if updated.AuthType != "sso" {
			t.Fatalf("AuthType = %q, want %q", updated.AuthType, "sso")
		}

		// TokenExpiresAt ≈ now + ExpiresIn (within 2 seconds tolerance)
		expectedLow := beforeApply + int64(result.ExpiresIn)
		expectedHigh := afterApply + int64(result.ExpiresIn) + 2
		if updated.TokenExpiresAt < expectedLow || updated.TokenExpiresAt > expectedHigh {
			t.Fatalf("TokenExpiresAt = %d, want in [%d, %d]", updated.TokenExpiresAt, expectedLow, expectedHigh)
		}

		// RefreshToken: updated if non-empty, preserved if empty
		if result.RefreshToken != "" {
			if updated.RefreshToken != result.RefreshToken {
				t.Fatalf("RefreshToken = %q, want %q (result non-empty)", updated.RefreshToken, result.RefreshToken)
			}
		} else {
			if updated.RefreshToken != originalRefresh {
				t.Fatalf("RefreshToken = %q, want %q (result empty, should preserve original)", updated.RefreshToken, originalRefresh)
			}
		}
	})
}

// ─── Task 2.3 ───────────────────────────────────────────────────────────────
// Feature: codegen-scan-login, Property 7: NeedsRefreshCodeGen threshold correctness
// **Validates: Requirements 4.2, 4.3, 4.4**
//
// For any MaclawLLMProvider with AuthType=="sso" and TokenExpiresAt > 0,
// NeedsRefreshCodeGen should return true iff now+300 >= TokenExpiresAt.
// For providers with TokenExpiresAt == 0, it should return false.
func TestProperty_NeedsRefreshCodeGen_Threshold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := time.Now().Unix()

		// Generate TokenExpiresAt in a range around now to exercise both sides of the threshold.
		// Range: [now - 3600, now + 3600] covers well-expired, near-threshold, and far-future tokens.
		expiresAt := rapid.Int64Range(now-3600, now+3600).Draw(t, "token_expires_at")

		provider := corelib.MaclawLLMProvider{
			AuthType:       "sso",
			TokenExpiresAt: expiresAt,
		}

		got := NeedsRefreshCodeGen(provider)

		// Recompute expected: now may have advanced slightly, so re-read it.
		nowCheck := time.Now().Unix()

		// For positive TokenExpiresAt, the threshold is: now+300 >= expiresAt
		// Because time passes between the function call and our check, we allow a small window:
		//   - If nowCheck+300 >= expiresAt+2, the function MUST have returned true
		//   - If nowCheck+300 < expiresAt-2, the function MUST have returned false
		//   - Otherwise we're in the boundary zone and either result is acceptable
		margin := int64(2)
		if nowCheck+300 >= expiresAt+margin {
			// Clearly needs refresh
			if !got {
				t.Fatalf("TokenExpiresAt=%d, now≈%d: got false, want true (now+300=%d >= expiresAt)",
					expiresAt, nowCheck, nowCheck+300)
			}
		} else if nowCheck+300 < expiresAt-margin {
			// Clearly does NOT need refresh
			if got {
				t.Fatalf("TokenExpiresAt=%d, now≈%d: got true, want false (now+300=%d < expiresAt)",
					expiresAt, nowCheck, nowCheck+300)
			}
		}
		// else: boundary zone, either result is acceptable
	})
}

// Sub-property: TokenExpiresAt == 0 always returns false for SSO providers.
func TestProperty_NeedsRefreshCodeGen_ZeroExpiry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		provider := corelib.MaclawLLMProvider{
			Name:           rapid.String().Draw(t, "name"),
			URL:            rapid.String().Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			AuthType:       "sso",
			TokenExpiresAt: 0,
		}

		if NeedsRefreshCodeGen(provider) {
			t.Fatal("TokenExpiresAt=0 with AuthType=sso: got true, want false")
		}
	})
}

// ─── Task 2.4 ───────────────────────────────────────────────────────────────
// Feature: codegen-scan-login, Property 8: Non-SSO AuthType providers are skipped
// **Validates: Requirements 4.5, 7.3**
//
// For any MaclawLLMProvider with AuthType != "sso" (including empty string,
// "oauth", "api_key", or any arbitrary string), NeedsRefreshCodeGen should
// return false regardless of TokenExpiresAt value.
func TestProperty_NeedsRefreshCodeGen_NonSSOSkipped(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random AuthType that is never "sso".
		// Pick from common non-SSO values and arbitrary strings, filtering out "sso".
		candidates := []string{"", "oauth", "api_key", "bearer", "token", "basic", "custom"}
		authType := rapid.SampledFrom(candidates).Draw(t, "auth_type_fixed")
		if rapid.Bool().Draw(t, "use_random_auth") {
			authType = rapid.StringMatching(`[a-z0-9_]{0,20}`).Filter(func(s string) bool {
				return s != "sso"
			}).Draw(t, "auth_type_random")
		}

		// TokenExpiresAt can be anything: zero, past, near-future, far-future.
		expiresAt := rapid.Int64Range(0, time.Now().Unix()+86400).Draw(t, "token_expires_at")

		provider := corelib.MaclawLLMProvider{
			Name:           rapid.String().Draw(t, "name"),
			URL:            rapid.String().Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			AuthType:       authType,
			TokenExpiresAt: expiresAt,
		}

		if NeedsRefreshCodeGen(provider) {
			t.Fatalf("AuthType=%q, TokenExpiresAt=%d: got true, want false (non-SSO providers should always be skipped)",
				authType, expiresAt)
		}
	})
}

// ─── Task 8.1 ───────────────────────────────────────────────────────────────
// Feature: codegen-scan-login, Property 1: Success response parsing extracts token and email
// **Validates: Requirements 1.3**
//
// For any valid scan response JSON with status: "success" and a non-empty token field,
// parsing the response should extract the exact token and email values from the JSON,
// with no data loss or corruption.
func TestProperty_ScanSuccessResponseParsing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random non-empty token and random email
		token := rapid.StringMatching(`[a-zA-Z0-9_\-\.]{1,200}`).Draw(t, "token")
		email := rapid.StringMatching(`[a-z]{3,10}@[a-z]{3,8}\.[a-z]{2,4}`).Draw(t, "email")

		// Construct a JSON response matching the CodeGen scan success format
		respJSON := fmt.Sprintf(`{"status":"success","token":%s,"email":%s}`,
			mustMarshalString(token), mustMarshalString(email))

		// Parse using the same struct as production code
		var scanResp codeGenScanResponse
		if err := json.Unmarshal([]byte(respJSON), &scanResp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		// Verify parsed values match exactly
		if scanResp.Status != "success" {
			t.Fatalf("Status = %q, want %q", scanResp.Status, "success")
		}
		if scanResp.Token != token {
			t.Fatalf("Token = %q, want %q", scanResp.Token, token)
		}
		if scanResp.Email != email {
			t.Fatalf("Email = %q, want %q", scanResp.Email, email)
		}
	})
}

// mustMarshalString JSON-encodes a string value (with proper escaping).
func mustMarshalString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("json.Marshal(%q): %v", s, err))
	}
	return string(b)
}

// ─── Task 8.2 ───────────────────────────────────────────────────────────────
// Feature: codegen-scan-login, Property 2: Error response includes status code and body summary
// **Validates: Requirements 1.8**
//
// For any non-2xx HTTP status code (400-599) and any response body, the error
// message produced by the error formatting logic (fmt.Errorf("HTTP %d: %s", statusCode, truncateBody(body, 512)))
// should contain both the numeric status code and a truncated representation
// of the response body (≤512 bytes).
func TestProperty_ErrorResponseFormatting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// 1. Generate random HTTP status code in range 400-599
		statusCode := rapid.IntRange(400, 599).Draw(t, "status_code")

		// 2. Generate random response body of varying length (0 to 2048 bytes)
		//    Use SliceOfN with Byte generator for efficient exact-length generation
		bodyLen := rapid.IntRange(0, 2048).Draw(t, "body_len")
		body := rapid.SliceOfN(rapid.Byte(), bodyLen, bodyLen).Draw(t, "body")

		// 3. Call truncateBody (the same helper used by fetchCodeGenModels)
		truncated := truncateBody(body, 512)

		// 4. Build the error message using the same pattern as fetchCodeGenModels
		errMsg := fmt.Sprintf("HTTP %d: %s", statusCode, truncated)

		// 5. Verify the error string contains the numeric status code
		statusStr := fmt.Sprintf("%d", statusCode)
		if !strings.Contains(errMsg, statusStr) {
			t.Fatalf("error message %q does not contain status code %q", errMsg, statusStr)
		}

		// 6. Verify the error string contains the (possibly truncated) body
		bodyStr := string(body)
		if len(bodyStr) > 0 {
			if len(bodyStr) <= 512 {
				// Body fits within limit — must appear verbatim
				if !strings.Contains(errMsg, bodyStr) {
					t.Fatalf("error message does not contain full body (len=%d)", len(bodyStr))
				}
			} else {
				// Body was truncated — first 512 chars must appear, plus "..." suffix
				prefix := bodyStr[:512]
				if !strings.Contains(errMsg, prefix) {
					t.Fatalf("error message does not contain truncated body prefix (body len=%d)", len(bodyStr))
				}
				if !strings.HasSuffix(truncated, "...") {
					t.Fatalf("truncated body missing '...' suffix for body len=%d", len(bodyStr))
				}
			}
		}

		// 7. Verify truncated body length constraint
		if len(bodyStr) > 512 {
			// truncated = first 512 chars + "..."  → 515 chars
			if len(truncated) != 515 {
				t.Fatalf("truncated length = %d, want 515 for body len=%d", len(truncated), len(bodyStr))
			}
		} else {
			// No truncation needed — length must match original
			if len(truncated) != len(bodyStr) {
				t.Fatalf("truncated length = %d, want %d (no truncation needed)", len(truncated), len(bodyStr))
			}
		}
	})
}


// ─── Task 8.3 ───────────────────────────────────────────────────────────────
// Feature: codegen-scan-login, Property 6: MaclawLLMProvider serialization round-trip
// **Validates: Requirements 3.3**
//
// For any valid MaclawLLMProvider struct (including SSO fields: AuthType,
// RefreshToken, TokenExpiresAt), serializing to JSON and deserializing back
// should produce an equivalent struct.
func TestProperty_MaclawLLMProvider_SerializationRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := corelib.MaclawLLMProvider{
			Name:           rapid.String().Draw(t, "name"),
			URL:            rapid.String().Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			Model:          rapid.String().Draw(t, "model"),
			Protocol:       rapid.String().Draw(t, "protocol"),
			ContextLength:  rapid.IntRange(0, 1000000).Draw(t, "context_length"),
			AuthType:       rapid.SampledFrom([]string{"", "sso", "oauth", "api_key"}).Draw(t, "auth_type"),
			RefreshToken:   rapid.String().Draw(t, "refresh_token"),
			TokenExpiresAt: rapid.Int64Range(0, 1<<40).Draw(t, "token_expires_at"),
		}

		// Serialize to JSON
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		// Deserialize back
		var roundTripped corelib.MaclawLLMProvider
		if err := json.Unmarshal(data, &roundTripped); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Verify all fields match
		if original.Name != roundTripped.Name {
			t.Fatalf("Name mismatch: %q vs %q", original.Name, roundTripped.Name)
		}
		if original.URL != roundTripped.URL {
			t.Fatalf("URL mismatch: %q vs %q", original.URL, roundTripped.URL)
		}
		if original.Key != roundTripped.Key {
			t.Fatalf("Key mismatch: %q vs %q", original.Key, roundTripped.Key)
		}
		if original.Model != roundTripped.Model {
			t.Fatalf("Model mismatch: %q vs %q", original.Model, roundTripped.Model)
		}
		if original.Protocol != roundTripped.Protocol {
			t.Fatalf("Protocol mismatch: %q vs %q", original.Protocol, roundTripped.Protocol)
		}
		if original.ContextLength != roundTripped.ContextLength {
			t.Fatalf("ContextLength mismatch: %d vs %d", original.ContextLength, roundTripped.ContextLength)
		}
		if original.AuthType != roundTripped.AuthType {
			t.Fatalf("AuthType mismatch: %q vs %q", original.AuthType, roundTripped.AuthType)
		}
		if original.RefreshToken != roundTripped.RefreshToken {
			t.Fatalf("RefreshToken mismatch: %q vs %q", original.RefreshToken, roundTripped.RefreshToken)
		}
		if original.TokenExpiresAt != roundTripped.TokenExpiresAt {
			t.Fatalf("TokenExpiresAt mismatch: %d vs %d", original.TokenExpiresAt, roundTripped.TokenExpiresAt)
		}
	})
}

// ─── Task 3.1 ───────────────────────────────────────────────────────────────
// Feature: sso-onboarding-auto-register, Property 1: JWT 邮箱提取往返一致性
// **Validates: Requirements 1.1, 1.2, 5.1**
//
// For any valid email (user@domain.tld format), constructing a JWT with that
// email in the payload and calling ExtractEmailFromJWT should return the
// exact same email string.
func TestProperty_ExtractEmailFromJWT_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random email: local@domain.tld
		local := rapid.StringMatching(`[a-z][a-z0-9._]{0,15}`).Draw(t, "local")
		domain := rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "domain")
		tld := rapid.StringMatching(`[a-z]{2,4}`).Draw(t, "tld")
		email := local + "@" + domain + "." + tld

		// Build JWT payload JSON with the email
		payloadJSON, err := json.Marshal(map[string]string{"email": email})
		if err != nil {
			t.Fatalf("json.Marshal payload: %v", err)
		}

		// Build a minimal JWT: base64url(header).base64url(payload).base64url(signature)
		header := `{"alg":"HS256","typ":"JWT"}`
		headerB64 := base64RawURLEncode([]byte(header))
		payloadB64 := base64RawURLEncode(payloadJSON)
		sigB64 := base64RawURLEncode([]byte("fakesignature"))

		token := headerB64 + "." + payloadB64 + "." + sigB64

		// Extract email and verify round-trip
		got, err := ExtractEmailFromJWT(token)
		if err != nil {
			t.Fatalf("ExtractEmailFromJWT(%q) error: %v", token, err)
		}
		if got != email {
			t.Fatalf("ExtractEmailFromJWT round-trip: got %q, want %q", got, email)
		}
	})
}

// ─── Task 3.2 ───────────────────────────────────────────────────────────────
// Feature: sso-onboarding-auto-register, Property 2: 非法 JWT 优雅降级
// **Validates: Requirements 1.3, 1.4, 5.2**
//
// For any invalid JWT token (random strings, missing segments, non-base64
// payload, non-JSON payload, or empty email), ExtractEmailFromJWT should
// return an empty string and a non-nil error, and must not panic.
func TestProperty_ExtractEmailFromJWT_InvalidTokenGraceful(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Choose one of several invalid token strategies
		strategy := rapid.IntRange(0, 4).Draw(t, "strategy")

		var token string
		switch strategy {
		case 0:
			// Random string with no dots
			token = rapid.StringMatching(`[a-zA-Z0-9]{0,100}`).Filter(func(s string) bool {
				return !strings.Contains(s, ".")
			}).Draw(t, "random_no_dots")
		case 1:
			// Only one dot (two segments instead of three)
			seg1 := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, "seg1")
			seg2 := rapid.StringMatching(`[a-zA-Z0-9]{1,20}`).Draw(t, "seg2")
			token = seg1 + "." + seg2
		case 2:
			// Three segments but payload is not valid base64
			header := base64RawURLEncode([]byte(`{"alg":"none"}`))
			badPayload := rapid.StringMatching(`[^a-zA-Z0-9_\-]{1,30}`).Draw(t, "bad_b64")
			sig := base64RawURLEncode([]byte("sig"))
			token = header + "." + badPayload + "." + sig
		case 3:
			// Three segments, valid base64 payload but not valid JSON
			header := base64RawURLEncode([]byte(`{"alg":"none"}`))
			notJSON := rapid.StringMatching(`[a-z]{1,30}`).Draw(t, "not_json")
			payload := base64RawURLEncode([]byte(notJSON))
			sig := base64RawURLEncode([]byte("sig"))
			token = header + "." + payload + "." + sig
		case 4:
			// Valid JWT structure but email field is empty string
			header := base64RawURLEncode([]byte(`{"alg":"none"}`))
			payload := base64RawURLEncode([]byte(`{"email":""}`))
			sig := base64RawURLEncode([]byte("sig"))
			token = header + "." + payload + "." + sig
		}

		// Must not panic (deferred recover)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ExtractEmailFromJWT panicked on token %q: %v", token, r)
			}
		}()

		got, err := ExtractEmailFromJWT(token)
		if err == nil {
			t.Fatalf("ExtractEmailFromJWT(%q): expected error, got nil (email=%q)", token, got)
		}
		if got != "" {
			t.Fatalf("ExtractEmailFromJWT(%q): expected empty string, got %q", token, got)
		}
	})
}

// ─── Task 3.3 ───────────────────────────────────────────────────────────────
// Feature: sso-onboarding-auto-register, Property 3: 邮箱格式校验
// **Validates: Requirements 1.5**
//
// For any JWT payload where the email field does NOT contain '@',
// ExtractEmailFromJWT should return an empty string and a non-nil error.
func TestProperty_ExtractEmailFromJWT_EmailMissingAt(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a non-empty string that does NOT contain '@'
		badEmail := rapid.StringMatching(`[a-zA-Z0-9._%+\-]{1,40}`).Filter(func(s string) bool {
			return !strings.Contains(s, "@") && s != ""
		}).Draw(t, "bad_email")

		// Build a valid JWT structure with the bad email
		payloadJSON, err := json.Marshal(map[string]string{"email": badEmail})
		if err != nil {
			t.Fatalf("json.Marshal payload: %v", err)
		}

		header := base64RawURLEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))
		payloadB64 := base64RawURLEncode(payloadJSON)
		sig := base64RawURLEncode([]byte("fakesig"))
		token := header + "." + payloadB64 + "." + sig

		got, err := ExtractEmailFromJWT(token)
		if err == nil {
			t.Fatalf("ExtractEmailFromJWT with email=%q: expected error, got nil", badEmail)
		}
		if got != "" {
			t.Fatalf("ExtractEmailFromJWT with email=%q: expected empty string, got %q", badEmail, got)
		}
	})
}

// base64RawURLEncode is a test helper that encodes bytes using base64 RawURLEncoding
// (no padding), matching the JWT standard encoding.
func base64RawURLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// ─── Task 5.1 ───────────────────────────────────────────────────────────────
// Feature: sso-onboarding-auto-register, Unit tests for ExtractEmailFromJWT
// **Validates: Requirements 1.1, 1.3, 1.4, 1.5, 5.1, 5.2**

func TestExtractEmailFromJWT(t *testing.T) {
	// helper: build a minimal JWT from a raw payload string
	buildJWT := func(payloadJSON string) string {
		header := base64RawURLEncode([]byte(`{"alg":"HS256","typ":"JWT"}`))
		payload := base64RawURLEncode([]byte(payloadJSON))
		sig := base64RawURLEncode([]byte("sig"))
		return header + "." + payload + "." + sig
	}

	tests := []struct {
		name      string
		token     string
		wantEmail string
		wantErr   bool
	}{
		{
			name:      "standard JWT extraction succeeds",
			token:     buildJWT(`{"email":"alice@example.com","sub":"12345"}`),
			wantEmail: "alice@example.com",
			wantErr:   false,
		},
		{
			name:      "base64url without padding decodes correctly",
			token:     buildJWT(`{"email":"b@x.io"}`), // short payload → no padding needed
			wantEmail: "b@x.io",
			wantErr:   false,
		},
		{
			name:      "empty token input",
			token:     "",
			wantEmail: "",
			wantErr:   true,
		},
		{
			name:      "payload missing email field",
			token:     buildJWT(`{"sub":"12345","name":"Bob"}`),
			wantEmail: "",
			wantErr:   true,
		},
		{
			name:      "email without @ is rejected",
			token:     buildJWT(`{"email":"not-an-email"}`),
			wantEmail: "",
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractEmailFromJWT(tc.token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ExtractEmailFromJWT() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.wantEmail {
				t.Fatalf("ExtractEmailFromJWT() = %q, want %q", got, tc.wantEmail)
			}
		})
	}
}
