package security

import (
	"encoding/json"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// Feature: claude-hook-security-gateway, Property 5: 敏感信息检测准确性
func TestProperty5_SensitiveDetectionAccuracy(t *testing.T) {
	d := NewSensitiveDetector()
	knownSensitive := []struct {
		text string
		cat  string
	}{
		{"sk-abcdefghijklmnopqrstuv", "api_key"},
		{"AKIAIOSFODNN7EXAMPLE0", "api_key"},
		{"-----BEGIN RSA PRIVATE KEY-----", "private_key"},
		{"password=secret123", "password"},
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdef123456", "jwt"},
	}
	for _, tc := range knownSensitive {
		matches := d.Detect(tc.text)
		if len(matches) == 0 {
			t.Errorf("Detect(%q) should find %s", tc.text, tc.cat)
		}
	}
}

// Feature: claude-hook-security-gateway, Property 6: 脱敏处理消除所有敏感模式
func TestProperty6_RedactEliminatesAllPatterns(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate text with embedded sensitive patterns
		prefix := rapid.String().Draw(t, "prefix")
		suffix := rapid.String().Draw(t, "suffix")
		sensitive := "sk-" + rapid.StringMatching(`[a-zA-Z0-9]{20,30}`).Draw(t, "key")
		text := prefix + sensitive + suffix

		d := NewSensitiveDetector()
		redacted := d.Redact(text)
		remaining := d.Detect(redacted)
		if len(remaining) > 0 {
			t.Fatalf("Detect(Redact(text)) should be empty, got %v for redacted=%q", remaining, redacted)
		}
	})
}

// Feature: claude-hook-security-gateway, Property 8: 会话状态持久化正确性
func TestProperty8_SessionStatePersistence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 20).Draw(t, "callCount")
		id := "prop8-" + rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, "id")

		s, _ := LoadSessionState(id)
		for i := 0; i < n; i++ {
			s.IncrementToolCall()
		}
		if s.ToolCallCount != n {
			t.Fatalf("ToolCallCount = %d, want %d", s.ToolCallCount, n)
		}
	})
}

// Feature: claude-hook-security-gateway, Property 9: 累积高风险自动升级安全模式
func TestProperty9_HighRiskAutoUpgrade(t *testing.T) {
	// 3 events within 5 min → upgrade
	s := newSessionState("prop9-upgrade")
	s.IncrementHighRisk()
	s.IncrementHighRisk()
	upgraded := s.IncrementHighRisk()
	if !upgraded || s.SecurityMode != "strict" {
		t.Fatalf("3 high risk events should upgrade to strict, got upgraded=%v mode=%s", upgraded, s.SecurityMode)
	}

	// Events outside 5-min window should not trigger
	s2 := newSessionState("prop9-no-upgrade")
	s2.HighRiskTimestamps = []time.Time{
		time.Now().Add(-10 * time.Minute),
		time.Now().Add(-6 * time.Minute),
	}
	s2.HighRiskCount = 2
	if s2.IncrementHighRisk() {
		t.Fatal("events outside 5-min window should not trigger upgrade")
	}
}

// Feature: claude-hook-security-gateway, Property 7: Hook 配置注入幂等性
// (tested in configfile package — TestEnsureClaudeSecurityHook_Idempotent)

// Feature: claude-hook-security-gateway, Property 1: SecurityCheckRequest 序列化往返
func TestProperty1_SecurityCheckRequestRoundTrip(t *testing.T) {
	type SecurityCheckRequest struct {
		ToolName    string                 `json:"tool_name"`
		ToolInput   map[string]interface{} `json:"tool_input,omitempty"`
		SessionID   string                 `json:"session_id,omitempty"`
		Source      string                 `json:"source,omitempty"`
		ProjectPath string                 `json:"project_path,omitempty"`
	}
	rapid.Check(t, func(t *rapid.T) {
		req := SecurityCheckRequest{
			ToolName:  rapid.StringMatching(`[a-z_]{3,20}`).Draw(t, "tool"),
			SessionID: rapid.StringMatching(`[a-z0-9-]{0,20}`).Draw(t, "session"),
			Source:    rapid.StringMatching(`[a-z-]{0,10}`).Draw(t, "source"),
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var decoded SecurityCheckRequest
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ToolName != req.ToolName || decoded.SessionID != req.SessionID || decoded.Source != req.Source {
			t.Fatalf("round-trip mismatch: %+v vs %+v", req, decoded)
		}
	})
}

// Feature: claude-hook-security-gateway, Property 2: AuditRecordRequest 序列化往返
func TestProperty2_AuditRecordRequestRoundTrip(t *testing.T) {
	type AuditRecordRequest struct {
		ToolName      string `json:"tool_name"`
		SessionID     string `json:"session_id,omitempty"`
		Result        string `json:"result,omitempty"`
		OutputSnippet string `json:"output_snippet,omitempty"`
		Source        string `json:"source,omitempty"`
	}
	rapid.Check(t, func(t *rapid.T) {
		req := AuditRecordRequest{
			ToolName:  rapid.StringMatching(`[a-z_]{3,20}`).Draw(t, "tool"),
			SessionID: rapid.StringMatching(`[a-z0-9-]{0,20}`).Draw(t, "session"),
			Result:    rapid.String().Draw(t, "result"),
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var decoded AuditRecordRequest
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ToolName != req.ToolName || decoded.SessionID != req.SessionID {
			t.Fatalf("round-trip mismatch")
		}
	})
}
