package mailbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSignatureDefaultAndMailboxOverride(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sigs, first, err := UpsertSignature(nil, SignatureSave{
		Name: "公共", Body: "{{邮箱名}} <{{邮箱}}>",
	}, now)
	if err != nil || !first.Default {
		t.Fatalf("first default: %+v %v", first, err)
	}
	sigs, personal, err := UpsertSignature(sigs, SignatureSave{
		Name: "个人", Body: "来自 {{from}}",
	}, now.Add(time.Second))
	if err != nil || personal.Default {
		t.Fatalf("second must not steal default: %+v %v", personal, err)
	}
	if _, _, err := UpsertSignature(sigs, SignatureSave{Name: "公共", Body: "x"}, now); err == nil {
		t.Fatal("duplicate name")
	}

	accounts := twoAccounts(t)
	work := accounts[0]
	home := accounts[1]
	resolved, ok, err := ResolveSignature(sigs, work)
	if err != nil || !ok || resolved.ID != first.ID {
		t.Fatalf("public default: %+v ok=%v err=%v", resolved, ok, err)
	}
	home.SignatureID = personal.ID
	resolved, ok, err = ResolveSignature(sigs, home)
	if err != nil || !ok || resolved.Name != "个人" {
		t.Fatalf("mailbox signature: %+v %v", resolved, err)
	}
	home.SignatureID = "missing"
	if _, _, err := ResolveSignature(sigs, home); err == nil || !strings.Contains(err.Error(), "Home") || !strings.Contains(err.Error(), "指定的签名不存在") {
		t.Fatal(err)
	}

	yes := true
	sigs, personal, err = UpsertSignature(sigs, SignatureSave{
		ID: personal.ID, Name: "个人", Body: personal.Body, MakeDefault: &yes,
	}, now.Add(2*time.Second))
	if err != nil || !personal.Default {
		t.Fatalf("explicit default: %+v %v", personal, err)
	}
	no := false
	sigs, personal, err = UpsertSignature(sigs, SignatureSave{
		ID: personal.ID, Name: "个人", Body: personal.Body, MakeDefault: &no,
	}, now.Add(3*time.Second))
	if err != nil || personal.Default {
		t.Fatalf("uncheck must clear a newer default: %+v %v", personal, err)
	}
	promoted := false
	for _, sig := range sigs {
		if sig.ID == first.ID && sig.Default {
			promoted = true
		}
	}
	if !promoted {
		t.Fatalf("oldest signature was not promoted: %+v", sigs)
	}

	sigs, accounts, err = DeleteSignature(sigs, []Account{work, home}, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs) != 1 || !sigs[0].Default || sigs[0].ID != personal.ID {
		t.Fatalf("promote oldest remaining: %+v", sigs)
	}
}

func TestSendFreezesExpandedSignature(t *testing.T) {
	accounts := twoAccounts(t)
	for i := range accounts {
		if accounts[i].Name == "Home" {
			accounts[i].SignatureID = "personal"
		}
	}
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sigs, _, err := UpsertSignature(nil, SignatureSave{
		Name: "公共", Body: "此致\n{{邮箱名}} / {{邮箱}}",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	sigs, _, err = UpsertSignature(sigs, SignatureSave{
		ID: "personal", Name: "个人", Body: "From {{from}}",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	host := testHost(accounts, &fakeMail{}, sender)
	host.Signatures = sigs

	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "Hello", "text": "body",
	})
	if sender.calls != 0 {
		t.Fatal("signature must not dial SMTP")
	}
	if !strings.Contains(card, "body") || !strings.Contains(card, "Work") || !strings.Contains(card, "w@ex.com") {
		t.Fatalf("default signature not expanded: %s", card)
	}
	if strings.Contains(card, "{{") {
		t.Fatalf("macro left in card: %s", card)
	}
	if _, class := host.Approve(context.Background(), "draft-1"); class != "approved" {
		t.Fatal(class)
	}
	if !strings.Contains(sender.last.Text, "Work / w@ex.com") || !strings.Contains(sender.last.Text, "\n\n-- \n") {
		t.Fatalf("frozen body: %q", sender.last.Text)
	}

	host = testHost(accounts, &fakeMail{}, sender)
	host.Signatures = sigs
	host.NewDraftID = func() string { return "draft-1" }
	card = host.Send(context.Background(), map[string]interface{}{
		"action": "send", "account": "Home", "to": []interface{}{"a@ex.com"}, "subject": "H", "text": "t",
	})
	if !strings.Contains(card, "from@ex.com") || strings.Contains(card, "此致") {
		t.Fatalf("mailbox signature: %s", card)
	}
	if _, class := host.Approve(context.Background(), "draft-1"); class != "approved" {
		t.Fatal(class)
	}
	if !strings.Contains(sender.last.Text, "From from@ex.com") {
		t.Fatalf("home body: %q", sender.last.Text)
	}

	broken := accounts
	for i := range broken {
		if broken[i].Name == "Work" {
			broken[i].SignatureID = "gone"
		}
	}
	host = testHost(broken, &fakeMail{}, sender)
	host.Signatures = sigs
	before := sender.calls
	text := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "Hello", "text": "body",
	})
	if sender.calls != before || !strings.Contains(text, "指定的签名不存在") {
		t.Fatalf("calls=%d text=%s", sender.calls, text)
	}
}
