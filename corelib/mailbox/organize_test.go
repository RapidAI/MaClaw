package mailbox

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

type fakeOrganizer struct {
	*fakeMail
	got []OrganizeRequest
}

func (f *fakeOrganizer) Organize(_ context.Context, _ Account, secret string, req OrganizeRequest) (int, error) {
	if secret != "stored-secret" {
		return 0, ErrAuth
	}
	f.got = append(f.got, req)
	return len(req.UIDs), nil
}

func TestOrganizeHostValidatesAndDispatches(t *testing.T) {
	org := &fakeOrganizer{fakeMail: &fakeMail{}}
	host := testHost(twoAccounts(t), org, nil)
	ctx := context.Background()
	for _, bad := range []map[string]interface{}{
		{"action": "delete", "uids": []interface{}{1.0}},
		{"action": "move", "uids": []interface{}{1.0}},
		{"action": "flag"},
		{"action": "add_labels", "uids": []interface{}{1.0}},
		{"action": "flag", "uids": []interface{}{1.0}, "password": "x"},
	} {
		if _, class := host.organize(ctx, bad, new(string)); class == "ok" {
			t.Fatalf("accepted %v", bad)
		}
	}
	out, class := host.organize(ctx, map[string]interface{}{"action": "move", "uids": []interface{}{3.0, 4.0}, "to_box": "Archive", "box": "INBOX"}, new(string))
	if class != "ok" || !strings.Contains(out, `"count":2`) {
		t.Fatalf("move %s %s", class, out)
	}
	if len(org.got) != 1 || org.got[0].Dest != "Archive" || org.got[0].Box != "INBOX" {
		t.Fatalf("dispatch %+v", org.got)
	}
	host.organize(ctx, map[string]interface{}{"action": "add_labels", "uids": []interface{}{5.0}, "labels": []interface{}{"发票", " "}}, new(string))
	if got := org.got[len(org.got)-1]; len(got.Labels) != 1 || got.Labels[0] != "发票" {
		t.Fatalf("labels %+v", got)
	}
}

func TestOrganizeHostReaderWithoutOrganizer(t *testing.T) {
	host := testHost(twoAccounts(t), &fakeMail{}, nil)
	if _, class := host.organize(context.Background(), map[string]interface{}{"action": "flag", "uids": []interface{}{1.0}}, new(string)); class != "connect" {
		t.Fatalf("class %s", class)
	}
}

func TestOrganizeStoreItems(t *testing.T) {
	gmail := Account{Provider: ProviderGmail}
	other := Account{Inbound: &Endpoint{Host: "imap.163.com"}}
	item, vals := organizeStore(other, OrganizeRequest{Action: "mark_read"})
	if item != imap.FormatFlagsOp(imap.AddFlags, true) || vals[0] != imap.SeenFlag {
		t.Fatalf("mark_read %v %v", item, vals)
	}
	item, vals = organizeStore(other, OrganizeRequest{Action: "unflag"})
	if item != imap.FormatFlagsOp(imap.RemoveFlags, true) || vals[0] != imap.FlaggedFlag {
		t.Fatalf("unflag %v %v", item, vals)
	}
	item, vals = organizeStore(gmail, OrganizeRequest{Action: "add_labels", Labels: []string{"Work Stuff"}})
	if item != "+X-GM-LABELS.SILENT" || vals[0] != "Work Stuff" {
		t.Fatalf("gmail label %v %v", item, vals)
	}
	item, vals = organizeStore(other, OrganizeRequest{Action: "remove_labels", Labels: []string{"a b(c)"}})
	if item != imap.FormatFlagsOp(imap.RemoveFlags, true) || vals[0] != "a_b_c_" {
		t.Fatalf("keyword %v %v", item, vals)
	}
}

func TestOrganizeOnIMAPSendsStore(t *testing.T) {
	c, seen := scriptIMAP(t, "IMAP4rev1 AUTH=PLAIN")
	defer c.Terminate()
	if err := imapLogin(c, "u", "p"); err != nil {
		t.Fatal(err)
	}
	n, err := organizeOn(c, Account{Provider: ProviderGmail}, OrganizeRequest{Action: "add_labels", Box: "INBOX", UIDs: []uint32{9, 10}, Labels: []string{"Receipts"}})
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	var sel, store bool
	for _, s := range seen() {
		if strings.HasPrefix(s, "SELECT INBOX") {
			sel = true
		}
		if strings.HasPrefix(s, "UID STORE 9:10 +X-GM-LABELS.SILENT") && strings.Contains(s, "Receipts") {
			store = true
		}
	}
	if !sel || !store {
		t.Fatalf("commands %v", seen())
	}
}

func TestOrganizeRefusedOnPOP3(t *testing.T) {
	acc := Account{Name: "p", Inbound: &Endpoint{Host: "127.0.0.1", Protocol: ProtocolPOP3}}
	if _, err := NewNetDialer().Organize(context.Background(), acc, "x", OrganizeRequest{Action: "flag", UIDs: []uint32{1}}); err == nil {
		t.Fatal("POP3 organize must be refused")
	}
}
