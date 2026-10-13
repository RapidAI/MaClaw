package mailbox

import (
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
}

func fmtConfigured(name string) string {
	return "邮箱「" + name + "」已保存。密码只写入了系统密钥环，没有留在配置里。"
}
