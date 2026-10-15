package mailbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestApplyConfigureStoresNoPasswordInTheAccount(t *testing.T) {
	const password = "agent-mailbox-secret"
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	next, saved, secrets, msg, err := ApplyConfigure(nil, map[string]interface{}{
		"action": "save", "name": "Work", "email": "w@ex.com", "username": "w", "password": password,
		"inbound":  map[string]interface{}{"host": "imap.example.com", "port": float64(993), "security": "ssl", "box": "Sent"},
		"outbound": map[string]interface{}{"host": "smtp.example.com", "port": float64(465), "security": "tls"},
	}, now)
	if err != nil || msg != fmtConfigured(saved.Name) || len(next) != 1 || !saved.Default {
		t.Fatalf("msg=%s err=%v saved=%+v", msg, err, saved)
	}
	raw := saved.Name + saved.Address + saved.SecretRefInbound + saved.SecretRefOutbound
	if strings.Contains(raw, password) || strings.Contains(msg, password) || !strings.Contains(msg, "Work") || saved.Name != "Work" || saved.Inbound.Box != "Sent" || saved.Inbound.Security != SecurityTLS || saved.Outbound.FromAddress != "w@ex.com" {
		t.Fatalf("saved leaked or missed fields: msg=%s saved=%+v inbound=%+v outbound=%+v", msg, saved, saved.Inbound, saved.Outbound)
	}
	if len(secrets) != 2 || secrets[0].Secret != password || secrets[1].Secret != password {
		t.Fatalf("secrets=%+v", secrets)
	}
	_, generated, _, generatedMsg, err := ApplyConfigure(nil, map[string]interface{}{
		"action": "save", "email": "w@ex.com", "username": "w",
		"inbound": map[string]interface{}{"host": "imap.example.com", "security": "tls"},
	}, now)
	if err != nil || generated.Name != "w" || !strings.Contains(generatedMsg, "「w」") || strings.Contains(generatedMsg, password) {
		t.Fatalf("generated name=%q msg=%s err=%v", generated.Name, generatedMsg, err)
	}
	if _, _, _, _, err := ApplyConfigure(next, map[string]interface{}{
		"action": "save", "name": "Other", "email": "o@ex.com", "password": "x", "host": "imap.example.com",
	}, now); err == nil {
		t.Fatal("top-level host was accepted")
	}
	_, numbered, _, _, err := ApplyConfigure(nil, map[string]interface{}{
		"action": "save", "name": "custom-port", "email": "c@ex.com",
		"outbound": map[string]interface{}{"host": "smtp.example.com", "port": json.Number("2525"), "security": "tls"},
	}, now)
	if err != nil || numbered.Outbound == nil || numbered.Outbound.Port != 2525 {
		t.Fatalf("canonical port saved=%+v err=%v", numbered.Outbound, err)
	}
}

func TestConfigureShapeReasonRefusesBeforeWrite(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"missing server", map[string]interface{}{"action": "save", "email": "a@ex.com", "name": "a"}, "mailbox_configure_server_required"},
		{"bad address", map[string]interface{}{"action": "save", "email": "not an email", "name": "a", "outbound": map[string]interface{}{"host": "smtp.example.com"}}, "mailbox_configure_identity_invalid"},
		{"bad port", map[string]interface{}{"action": "save", "email": "a@ex.com", "name": "a", "outbound": map[string]interface{}{"host": "smtp.example.com", "port": json.Number("70000"), "security": "tls"}}, "mailbox_configure_server_invalid"},
		{"ready", map[string]interface{}{"action": "save", "email": "a@ex.com", "name": "a", "outbound": map[string]interface{}{"host": "smtp.example.com", "port": json.Number("2525"), "security": "ssl"}}, ""},
	}
	for _, tc := range cases {
		if got := ConfigureShapeReason(tc.args); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func fmtConfigured(name string) string {
	return "邮箱「" + name + "」已保存。密码只写入了系统密钥环，没有留在配置里。"
}
