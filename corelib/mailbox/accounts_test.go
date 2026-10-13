package mailbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func epIn(host string) *Endpoint {
	return &Endpoint{Host: host, Security: SecurityTLS, Username: "u"}
}

func epOut(host string) *Endpoint {
	return &Endpoint{Host: host, Security: SecuritySTARTTLS, Username: "u", FromAddress: "from@ex.com"}
}

func TestUpsertNamesDefaultPauseDeleteAndSecrets(t *testing.T) {
	now := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	password := "app-password-should-not-leak"
	if err := RejectSecretPayload([]string{"password"}); err == nil {
		t.Fatal("password key accepted")
	}
	first, saved, err := UpsertAccount(nil, SaveRequest{
		Name: " Work ", Address: "work@ex.com", Inbound: epIn("imap.ex.com"), Outbound: epOut("smtp.ex.com"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Enabled || !saved.Default {
		t.Fatalf("first account enabled=%v default=%v", saved.Enabled, saved.Default)
	}
	if saved.Name != "Work" || saved.Inbound.Port != 993 || saved.Outbound.Port != 587 {
		t.Fatalf("normalized = %+v inbound=%+v outbound=%+v", saved, saved.Inbound, saved.Outbound)
	}
	if saved.SecretRefInbound != SecretRef(saved.ID, SideInbound) || strings.Contains(saved.SecretRefInbound, password) {
		t.Fatal(saved.SecretRefInbound)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), password) {
		t.Fatal("password landed in the account config")
	}
	summary := Summary(saved, true, false)
	blob, _ := json.Marshal(summary)
	for _, banned := range []string{password, "secret_ref"} {
		if strings.Contains(string(blob), banned) {
			t.Fatalf("summary contains %s: %s", banned, blob)
		}
	}
	in, _ := summary["inbound"].(map[string]interface{})
	outEP, _ := summary["outbound"].(map[string]interface{})
	if in["host"] != "imap.ex.com" || in["port"] != 993 || outEP["host"] != "smtp.ex.com" || outEP["from_address"] != "from@ex.com" {
		t.Fatalf("settings summary dropped the servers: %#v", summary)
	}
	if summary["has_inbound_secret"] != true || summary["has_outbound_secret"] != false {
		t.Fatalf("summary = %#v", summary)
	}

	later := now.Add(time.Minute)
	second, _, err := UpsertAccount(first, SaveRequest{
		Name: "work", Address: "other@ex.com", Inbound: epIn("imap.ex.com"),
	}, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].Default != true || second[1].Default != false || !second[1].Enabled {
		t.Fatalf("second = %+v %+v", second[0], second[1])
	}
	if _, _, err := UpsertAccount(second, SaveRequest{Name: "Work", Address: "x@ex.com", Inbound: epIn("imap.ex.com")}, later); err == nil || !strings.Contains(err.Error(), "Work") {
		t.Fatal(err)
	}

	marked, err := SetDefault(second, second[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if marked[0].Default || !marked[1].Default {
		t.Fatalf("default not moved: %+v %+v", marked[0], marked[1])
	}
	paused, err := SetEnabled(marked, second[1].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if paused[1].Enabled || paused[1].Default || !paused[0].Default {
		t.Fatalf("pause did not promote the older mailbox: %+v %+v", paused[0], paused[1])
	}
	deleted, removed, err := DeleteAccount(marked, second[1].ID)
	if err != nil || removed.ID != second[1].ID || len(deleted) != 1 || !deleted[0].Default {
		t.Fatalf("deleted=%+v removed=%+v err=%v", deleted, removed, err)
	}

	allPaused, err := SetEnabled(first, saved.ID, false)
	if err != nil || allPaused[0].Default {
		t.Fatal(allPaused, err)
	}
	reenabled, err := SetEnabled(allPaused, saved.ID, true)
	if err != nil || !reenabled[0].Default {
		t.Fatalf("re-enable = %+v %v", reenabled, err)
	}
}

func TestOldestPromotionUsesCreatedAtThenID(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	accounts, a, err := UpsertAccount(nil, SaveRequest{ID: "b", Name: "B", Address: "b@ex.com", Inbound: epIn("imap.ex.com")}, t1)
	if err != nil {
		t.Fatal(err)
	}
	accounts, _, err = UpsertAccount(accounts, SaveRequest{ID: "a", Name: "A", Address: "a@ex.com", Inbound: epIn("imap.ex.com")}, t1)
	if err != nil {
		t.Fatal(err)
	}
	accounts, _, err = UpsertAccount(accounts, SaveRequest{ID: "c", Name: "C", Address: "c@ex.com", Inbound: epIn("imap.ex.com"), MakeDefault: boolPtr(true)}, t1.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	left, err := SetEnabled(accounts, "c", false)
	if err != nil {
		t.Fatal(err)
	}
	def := ""
	for _, account := range left {
		if account.Default {
			def = account.ID
		}
	}
	if def != "a" {
		t.Fatalf("promoted %s, want a (same created_at, lexicographic id); first=%s", def, a.ID)
	}
}

func TestResolveNameOrDefault(t *testing.T) {
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	accounts, _, err := UpsertAccount(nil, SaveRequest{Name: "Work", Address: "w@ex.com", Inbound: epIn("imap.ex.com"), Outbound: epOut("smtp.ex.com")}, now)
	if err != nil {
		t.Fatal(err)
	}
	accounts, _, err = UpsertAccount(accounts, SaveRequest{Name: "Home", Address: "h@ex.com", Outbound: epOut("smtp.ex.com")}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(accounts, "", SideInbound)
	if err != nil || got.Name != "Work" {
		t.Fatalf("default = %+v %v", got, err)
	}
	got, err = Resolve(accounts, "Home", SideOutbound)
	if err != nil || got.Name != "Home" {
		t.Fatalf("named = %+v %v", got, err)
	}
	if _, err := Resolve(accounts, "Home", SideInbound); err == nil || !strings.Contains(err.Error(), "没有配置收件") {
		t.Fatal(err)
	}
	if _, err := Resolve(accounts, "missing", ""); err == nil || !strings.Contains(err.Error(), "没有名为「missing」") {
		t.Fatal(err)
	}
	if _, err := Resolve(accounts, "w@ex.com", ""); err == nil {
		t.Fatal("address must not select an account")
	}
	paused, err := SetEnabled(accounts, accounts[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(paused, "Work", ""); err == nil || !strings.Contains(err.Error(), "已停用") {
		t.Fatal(err)
	}
	if _, err := Resolve(paused, "", SideOutbound); err != nil && !strings.Contains(err.Error(), "Home") && !strings.Contains(err.Error(), "没有可用的默认") {
		t.Fatal(err)
	}
	both, err := SetEnabled(paused, accounts[1].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(both, "", ""); err == nil || !strings.Contains(err.Error(), "没有可用的默认邮箱") {
		t.Fatal(err)
	}
	if _, err := Resolve(nil, "", ""); err == nil || !strings.Contains(err.Error(), "尚未配置邮箱") {
		t.Fatal(err)
	}
}

func TestNoneSecurityOnlyOnLoopbackAndCap(t *testing.T) {
	now := time.Now()
	if _, _, err := UpsertAccount(nil, SaveRequest{
		Name: "Open", Address: "a@ex.com", Inbound: &Endpoint{Host: "imap.ex.com", Security: SecurityNone},
	}, now); err == nil {
		t.Fatal("cleartext remote host accepted")
	}
	if _, _, err := UpsertAccount(nil, SaveRequest{
		Name: "Local", Address: "a@ex.com", Inbound: &Endpoint{Host: "localhost", Security: SecurityNone, Port: 1143},
	}, now); err != nil {
		t.Fatal(err)
	}
	var accounts []Account
	for i := 0; i < MaxAccounts; i++ {
		var err error
		accounts, _, err = UpsertAccount(accounts, SaveRequest{
			Name: "N" + jsonNumber(uint64(i)), Address: "a@ex.com", Inbound: epIn("imap.ex.com"),
		}, now.Add(time.Duration(i)))
		if err != nil {
			t.Fatal(i, err)
		}
	}
	if _, _, err := UpsertAccount(accounts, SaveRequest{Name: "overflow", Address: "a@ex.com", Inbound: epIn("imap.ex.com")}, now); err == nil {
		t.Fatal("cap not enforced")
	}
}

func boolPtr(v bool) *bool { return &v }
