package mailbox

import (
	"strings"
	"testing"
	"time"
)

func TestPresetForAddress(t *testing.T) {
	cases := map[string][2]string{
		"znsoft@163.com": {"imap.163.com", "smtp.163.com"},
		"a@126.com":      {"imap.126.com", "smtp.126.com"},
		"a@YEAH.net":     {"imap.yeah.net", "smtp.yeah.net"},
		"a@qq.com":       {"imap.qq.com", "smtp.qq.com"},
		"a@gmail.com":    {"imap.gmail.com", "smtp.gmail.com"},
		"a@hotmail.com":  {"outlook.office365.com", "smtp-mail.outlook.com"},
		"a@outlook.com":  {"outlook.office365.com", "smtp-mail.outlook.com"},
	}
	for addr, want := range cases {
		p, ok := PresetForAddress(addr)
		if !ok || p.Inbound.Host != want[0] || p.Outbound.Host != want[1] || p.Hint == "" {
			t.Errorf("%s: got %+v", addr, p)
		}
	}
	if _, ok := PresetForAddress("a@example.org"); ok {
		t.Fatal("unknown domain got a preset")
	}
	if !strings.Contains(ProviderHint("a@163.com"), "授权码") || !strings.Contains(ProviderHint("a@qq.com"), "授权码") {
		t.Fatal("netease/qq hint must mention 授权码")
	}
}

func TestConfigureFillsPresetAndKeepsExplicit(t *testing.T) {
	_, saved, _, msg, err := ApplyConfigure(nil, map[string]interface{}{
		"action": "save", "name": "163", "email": "znsoft@163.com",
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Inbound == nil || saved.Inbound.Host != "imap.163.com" || saved.Inbound.Username != "znsoft@163.com" {
		t.Fatalf("inbound %+v", saved.Inbound)
	}
	if saved.Outbound == nil || saved.Outbound.Host != "smtp.163.com" || saved.Outbound.Port != 465 {
		t.Fatalf("outbound %+v", saved.Outbound)
	}
	if !strings.Contains(msg, "授权码") {
		t.Fatalf("hint missing: %s", msg)
	}
	if r := ConfigureShapeReason(map[string]interface{}{"action": "save", "name": "q", "email": "a@qq.com"}); r != "" {
		t.Fatalf("preset address rejected: %s", r)
	}
	_, saved, _, _, err = ApplyConfigure(nil, map[string]interface{}{
		"action": "save", "name": "x", "email": "a@163.com",
		"inbound": map[string]interface{}{"host": "mail.corp.local", "port": 993},
	}, time.Unix(1, 0))
	if err != nil || saved.Inbound.Host != "mail.corp.local" || saved.Outbound.Host != "smtp.163.com" {
		t.Fatalf("explicit endpoint overridden: %+v %+v %v", saved.Inbound, saved.Outbound, err)
	}
	if r := ConfigureShapeReason(map[string]interface{}{"action": "save", "name": "q", "email": "a@example.org"}); r != "mailbox_configure_server_required" {
		t.Fatalf("unknown domain without servers: %q", r)
	}
}
