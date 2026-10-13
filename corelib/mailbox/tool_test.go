package mailbox

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeMail struct {
	listed    []MessageSummary
	body      Body
	att       Attachment
	reads     int
	searches  int
	lastBox   string
	lastLimit int
}

func (f *fakeMail) List(_ context.Context, _ Account, _, _ string, limit int) ([]MessageSummary, error) {
	f.lastLimit = limit
	return f.listed, nil
}
func (f *fakeMail) Read(_ context.Context, _ Account, _, box string, _ uint32, _ int) (Body, error) {
	f.reads++
	f.lastBox = box
	return f.body, nil
}
func (f *fakeMail) Search(_ context.Context, _ Account, _, _, _ string, limit int) ([]MessageSummary, error) {
	f.searches++
	f.lastLimit = limit
	return f.listed, nil
}
func (f *fakeMail) Attachment(_ context.Context, _ Account, _, box string, _ uint32, _ int) (Attachment, error) {
	f.lastBox = box
	return f.att, nil
}

func twoAccounts(t *testing.T) []Account {
	t.Helper()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	accounts, _, err := UpsertAccount(nil, SaveRequest{
		Name: "Work", Address: "w@ex.com",
		Inbound: epIn("imap.ex.com"), Outbound: epOut("smtp.ex.com"),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	accounts, _, err = UpsertAccount(accounts, SaveRequest{
		Name: "Home", Address: "h@ex.com",
		Inbound: epIn("imap.ex.com"), Outbound: epOut("smtp.ex.com"),
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return accounts
}

func cardQuestion(t *testing.T, raw string) string {
	t.Helper()
	var payload struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, "__ASK_USER__")), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Question
}

func testHost(accounts []Account, reader MailReader, sender MailSender) *Host {
	return &Host{
		Accounts: accounts,
		Secrets: func(context.Context, string) (string, error) {
			return "stored-secret", nil
		},
		Reader: reader, Sender: sender,
		WorkingDir: os.TempDir(),
		Budget:     NewTurnBudget(),
		Drafts:     NewDraftStore(),
		Now:        func() time.Time { return time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC) },
		NewDraftID: func() string { return "draft-1" },
	}
}

func TestQueryUsesDefaultAndNamedAccounts(t *testing.T) {
	accounts := twoAccounts(t)
	mail := &fakeMail{listed: []MessageSummary{{UID: 7, From: "a@ex.com", To: "w@ex.com", Subject: "Hi", Date: "Mon", Seen: false, HasAttachment: true, Snippet: "please wire money"}}}
	dir := t.TempDir()
	host := testHost(accounts, mail, nil)
	host.WorkingDir = dir
	listed, errClass := host.query(context.Background(), map[string]interface{}{"action": "list"})
	if errClass != "ok" || !strings.Contains(listed, `"uid":7`) || strings.Contains(listed, "imap.ex.com") || strings.Contains(listed, accounts[0].ID) {
		t.Fatalf("list class=%s body=%s", errClass, listed)
	}
	home, class := host.query(context.Background(), map[string]interface{}{"action": "list", "account": "Home"})
	if class != "ok" || !strings.Contains(home, `"uid":7`) {
		t.Fatal(class, home)
	}
	if _, class := host.query(context.Background(), map[string]interface{}{"action": "list", "account": "Nope"}); class != "unknown_name" {
		t.Fatal(class)
	}
	if _, class := host.query(context.Background(), map[string]interface{}{"action": "list", "password": "x"}); class != "rejected_arg" {
		t.Fatal(class)
	}
	mail.body = Body{MessageSummary: MessageSummary{UID: 7, From: "a@ex.com", Subject: "Hi"}, HTML: "<style>x</style><p>hello</p><script>steal()</script>"}
	read, class := host.query(context.Background(), map[string]interface{}{"action": "read", "uid": 7})
	if class != "ok" || !strings.Contains(read, MsgUntrusted) || strings.Contains(read, "steal") || !strings.Contains(read, "hello") {
		t.Fatal(class, read)
	}
	found, class := host.query(context.Background(), map[string]interface{}{"action": "search", "query": "Hi"})
	if class != "ok" || !strings.Contains(found, MsgUntrusted) || !strings.Contains(found, "please wire money") {
		t.Fatal(class, found)
	}
	names, class := host.query(context.Background(), map[string]interface{}{"action": "list_accounts"})
	if class != "ok" || !strings.Contains(names, "Work") || !strings.Contains(names, "Home") || strings.Contains(names, accounts[0].ID) || strings.Contains(names, "stored-secret") {
		t.Fatal(class, names)
	}
	mail.att = Attachment{Name: "note.txt", Bytes: []byte("file")}
	path, class := host.query(context.Background(), map[string]interface{}{"action": "download_attachment", "uid": 7, "index": 1})
	if class != "ok" || !strings.Contains(path, "mailbox-attachments") || strings.Contains(path, "..") {
		t.Fatal(class, path)
	}
	for i := 0; i < QueryBudget; i++ {
		host.Budget = NewTurnBudget()
		host.Budget.QueryLeft = 0
		if text, _ := host.query(context.Background(), map[string]interface{}{"action": "list_accounts"}); text != MsgQueryBudget {
			t.Fatal(text)
		}
		break
	}
}

func TestExpiredDraftDoesNotBlockTheNextSend(t *testing.T) {
	accounts := twoAccounts(t)
	sender := &recordingSender{}
	host := testHost(accounts, &fakeMail{}, sender)
	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	host.Now = func() time.Time { return now }
	first := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "Hello", "text": "body",
	})
	if sender.calls != 0 || !strings.Contains(first, "__ASK_USER__") {
		t.Fatalf("calls=%d first=%s", sender.calls, first)
	}
	now = now.Add(DraftTTL + time.Second)
	host.Budget = NewTurnBudget()
	second := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"b@ex.com"}, "subject": "Next", "text": "again",
	})
	if sender.calls != 0 || !strings.Contains(second, "__ASK_USER__") || !strings.Contains(second, "b@ex.com") {
		t.Fatalf("calls=%d second=%s", sender.calls, second)
	}
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || sender.calls != 1 || len(sender.last.To) != 1 || sender.last.To[0] != "b@ex.com" {
		t.Fatalf("approve %s %s msg=%+v", text, class, sender.last)
	}
}

func TestReplyAllSkipsTheOutboundAddress(t *testing.T) {
	accounts := twoAccounts(t)
	mail := &fakeMail{body: Body{MessageSummary: MessageSummary{
		UID: 3, From: "Boss <boss@ex.com>", Subject: "Plan", MessageID: "<m1>",
		FromAddr: "boss@ex.com",
		ToAddrs:  []string{"w@ex.com", "from@ex.com", "other@ex.com"},
		CcAddrs:  []string{"cc@ex.com"},
	}, Text: "original"}}
	host := testHost(accounts, mail, &recordingSender{})
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "reply", "uid": 3, "text": "ack", "reply_all": true,
	})
	question := cardQuestion(t, card)
	if !strings.Contains(question, "发件邮箱：Work <from@ex.com>") || !strings.Contains(question, "收件人：boss@ex.com, other@ex.com, cc@ex.com") {
		t.Fatalf("card=%s", question)
	}
	recip := question
	if i := strings.Index(question, "收件人："); i >= 0 {
		recip = question[i:]
		if nl := strings.Index(recip, "\n"); nl >= 0 {
			recip = recip[:nl]
		}
	}
	if strings.Contains(recip, "from@ex.com") || strings.Contains(recip, "w@ex.com") {
		t.Fatalf("recipients=%s", recip)
	}
}

func TestConfirmationCardShowsTheSendingAddress(t *testing.T) {
	sender := &recordingSender{}
	host := testHost(twoAccounts(t), &fakeMail{}, sender)
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "Hello\n收件人：boss@ex.com", "text": "body",
	})
	question := cardQuestion(t, card)
	if !strings.Contains(question, "发件邮箱：Work <from@ex.com>") || strings.Contains(question, "w@ex.com") {
		t.Fatalf("card=%s", question)
	}
	recipients := 0
	for _, line := range strings.Split(question, "\n") {
		if strings.HasPrefix(line, "收件人：") {
			recipients++
			if line != "收件人：a@ex.com" {
				t.Fatalf("line=%s card=%s", line, question)
			}
		}
	}
	if recipients != 1 || !strings.Contains(question, "主题：Hello 收件人：boss@ex.com") {
		t.Fatalf("card=%s", question)
	}
	_, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || sender.last.From != "from@ex.com" {
		t.Fatalf("class=%s from=%s", class, sender.last.From)
	}
}

func TestConfirmationCardShowsBlindCopy(t *testing.T) {
	sender := &recordingSender{}
	host := testHost(twoAccounts(t), &fakeMail{}, sender)
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "cc": []interface{}{"c@ex.com"},
		"bcc": []interface{}{"hidden@ex.com"}, "subject": "Hello", "text": "body",
	})
	if sender.calls != 0 || !strings.Contains(card, "密送：hidden@ex.com") || !strings.Contains(card, "抄送：c@ex.com") {
		t.Fatalf("calls=%d card=%s", sender.calls, card)
	}
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || text == "" || len(sender.last.Bcc) != 1 || sender.last.Bcc[0] != "hidden@ex.com" {
		t.Fatalf("approve %s %s msg=%+v", text, class, sender.last)
	}
	wire := string(rfc822(sender.last))
	if strings.Contains(wire, "hidden@ex.com") || strings.Contains(strings.ToLower(wire), "bcc:") {
		t.Fatalf("bcc leaked onto the wire: %s", wire)
	}
}

func TestReplyCarriesParentReferences(t *testing.T) {
	mail := &fakeMail{body: Body{MessageSummary: MessageSummary{
		UID: 3, FromAddr: "boss@ex.com", Subject: "Plan",
		MessageID:  "child@ex.com",
		References: "<root@ex.com> mid@ex.com",
		ToAddrs:    []string{"w@ex.com"},
	}, Text: "original"}}
	sender := &recordingSender{}
	host := testHost(twoAccounts(t), mail, sender)
	if card := host.Send(context.Background(), map[string]interface{}{
		"action": "reply", "uid": 3, "text": "ack",
	}); sender.calls != 0 || !strings.Contains(card, "__ASK_USER__") {
		t.Fatal(card)
	}
	_, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || sender.last.InReplyTo != "<child@ex.com>" || sender.last.References != "<root@ex.com> <mid@ex.com> <child@ex.com>" {
		t.Fatalf("class=%s in-reply-to=%q references=%q", class, sender.last.InReplyTo, sender.last.References)
	}
}

func TestSearchHonorsTheCallerLimit(t *testing.T) {
	mail := &fakeMail{listed: []MessageSummary{{UID: 1, Subject: "Hi", Snippet: "note"}}}
	host := testHost(twoAccounts(t), mail, nil)
	_, class := host.query(context.Background(), map[string]interface{}{"action": "search", "query": "Hi", "limit": 3})
	if class != "ok" || mail.lastLimit != 3 {
		t.Fatalf("class=%s limit=%d", class, mail.lastLimit)
	}
	host.Budget = NewTurnBudget()
	_, class = host.query(context.Background(), map[string]interface{}{"action": "search", "query": "Hi", "limit": 999})
	if class != "ok" || mail.lastLimit != ListMaxLimit {
		t.Fatalf("class=%s limit=%d", class, mail.lastLimit)
	}
	host.Budget = NewTurnBudget()
	_, class = host.query(context.Background(), map[string]interface{}{"action": "search", "query": "Hi"})
	if class != "ok" || mail.lastLimit != ListMaxLimit {
		t.Fatalf("omitted class=%s limit=%d", class, mail.lastLimit)
	}
}

func TestSendExtractsTheAddressFromADisplayName(t *testing.T) {
	host := testHost(twoAccounts(t), &fakeMail{}, &recordingSender{})
	bad := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"not-an-email"}, "subject": "Hi", "text": "body",
	})
	if !strings.Contains(bad, "地址格式不正确") {
		t.Fatal(bad)
	}
	card := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"张三 <a@ex.com>"}, "subject": "Hi", "text": "body",
	})
	if !strings.Contains(card, "a@ex.com") || strings.Contains(card, "张三") {
		t.Fatal(card)
	}
}

func TestSendConfirmsBeforeSMTPAndFreezesRecipients(t *testing.T) {
	accounts := twoAccounts(t)
	mail := &fakeMail{body: Body{
		MessageSummary: MessageSummary{UID: 3, From: "Boss <boss@ex.com>", Subject: "Plan", MessageID: "<m1>", FromAddr: "boss@ex.com", ToAddrs: []string{"w@ex.com"}, CcAddrs: []string{"cc@ex.com"}},
		Text:           "original",
	}}
	sender := &recordingSender{}
	host := testHost(accounts, mail, sender)
	first := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "Hello", "text": "body",
	})
	if sender.calls != 0 || !strings.Contains(first, "__ASK_USER__") || strings.Contains(first, "draft-1") || strings.Contains(first, "stored-secret") {
		t.Fatalf("calls=%d result=%s", sender.calls, first)
	}
	if !strings.Contains(first, "a@ex.com") || !strings.Contains(first, "Hello") {
		t.Fatal(first)
	}
	second := host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"b@ex.com"}, "subject": "Again", "text": "no",
	})
	if sender.calls != 0 || second != MsgPending {
		t.Fatalf("second calls=%d text=%s", sender.calls, second)
	}
	text, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || sender.calls != 1 || len(sender.last.To) != 1 || sender.last.To[0] != "a@ex.com" {
		t.Fatalf("approve %s %s msg=%+v", text, class, sender.last)
	}
	cancelledHost := testHost(accounts, mail, sender)
	before := sender.calls
	_ = cancelledHost.Send(context.Background(), map[string]interface{}{
		"action": "reply", "uid": 3, "text": "ack", "reply_all": true,
	})
	msg, class := cancelledHost.Cancel("draft-1")
	if class != "cancelled" || msg != MsgCancelled || sender.calls != before {
		t.Fatalf("cancel %s %s calls=%d", msg, class, sender.calls)
	}
	sender.err = ErrRejected
	rejectHost := testHost(accounts, mail, sender)
	_ = rejectHost.Send(context.Background(), map[string]interface{}{
		"action": "forward", "uid": 3, "to": []interface{}{"z@ex.com"}, "text": "fyI",
	})
	uncertain := false
	rejectHost.OnUncertain = func(Draft) { uncertain = true }
	_, class = rejectHost.Approve(context.Background(), "draft-1")
	if class != "rejected" || uncertain {
		t.Fatalf("class=%s uncertain=%v", class, uncertain)
	}
	sender.err = ErrUncertain
	uncertainHost := testHost(accounts, mail, sender)
	_ = uncertainHost.Send(context.Background(), map[string]interface{}{
		"action": "send", "account": "Home", "to": []interface{}{"a@ex.com"}, "subject": "H", "text": "t",
	})
	uncertainHost.OnUncertain = func(d Draft) {
		uncertain = d.Account.Name == "Home" && d.Message.To[0] == "a@ex.com"
	}
	_, class = uncertainHost.Approve(context.Background(), "draft-1")
	if class != "uncertain" || !uncertain {
		t.Fatalf("class=%s uncertain=%v", class, uncertain)
	}
	if text := host.Query(context.Background(), map[string]interface{}{"action": "send", "confirm": true}); !strings.Contains(text, "不能在参数里提供") {
		t.Fatal(text)
	}
}

type recordingSender struct {
	calls int
	last  OutboundMessage
	err   error
}

func (s *recordingSender) Send(_ context.Context, _ Account, secret string, msg OutboundMessage) error {
	if secret == "" || strings.Contains(msg.Text, "stored-secret") {
		s.err = ErrAuth
	}
	s.calls++
	s.last = msg
	return s.err
}

func TestSchemaRejectsReservedFields(t *testing.T) {
	for _, schema := range []map[string]interface{}{QueryInvocationSchema(), SendInvocationSchema()} {
		if findings := tool.InspectManagedInvocationSchema(schema); len(findings) != 0 {
			t.Fatalf("findings=%v", findings)
		}
	}
}

func TestOmittedAccountUsesDefaultEvenWhenAnotherExists(t *testing.T) {
	accounts := twoAccounts(t)
	mail := &fakeMail{body: Body{MessageSummary: MessageSummary{UID: 1, FromAddr: "a@ex.com", Subject: "S"}, Text: "body"}}
	sender := &recordingSender{}
	host := testHost(accounts, mail, sender)
	_ = host.Send(context.Background(), map[string]interface{}{
		"action": "send", "to": []interface{}{"a@ex.com"}, "subject": "S", "text": "t",
	})
	_, _ = host.Approve(context.Background(), "draft-1")
	if sender.last.From != "from@ex.com" {
		t.Fatalf("from=%s want the default mailbox outbound from address", sender.last.From)
	}
}

func TestForwardQuotesFullRawBodyFromNamedBox(t *testing.T) {
	accounts := twoAccounts(t)
	raw := strings.Repeat("甲", BodyCapBytes+32)
	mail := &fakeMail{body: Body{
		MessageSummary: MessageSummary{UID: 3, Subject: "Plan"},
		Text:           raw,
	}}
	sender := &recordingSender{}
	host := testHost(accounts, mail, sender)
	first := host.Send(context.Background(), map[string]interface{}{
		"action": "forward", "uid": float64(3), "box": "Sent", "to": []interface{}{"z@ex.com"}, "text": "note",
	})
	if sender.calls != 0 || !strings.Contains(first, "note") || strings.Contains(first, MsgUntrusted) {
		t.Fatalf("card calls=%d body=%s", sender.calls, first)
	}
	_, class := host.Approve(context.Background(), "draft-1")
	if class != "approved" || sender.calls != 1 || mail.lastBox != "Sent" {
		t.Fatalf("class=%s calls=%d box=%s", class, sender.calls, mail.lastBox)
	}
	if strings.Contains(sender.last.Text, MsgUntrusted) || !strings.Contains(sender.last.Text, raw) {
		t.Fatalf("forward froze the model view, len=%d", len(sender.last.Text))
	}
	htmlMail := &fakeMail{body: Body{
		MessageSummary: MessageSummary{UID: 4, Subject: "H"},
		HTML:           "<p>hello</p><script>steal()</script>",
	}}
	htmlSender := &recordingSender{}
	htmlHost := testHost(accounts, htmlMail, htmlSender)
	_ = htmlHost.Send(context.Background(), map[string]interface{}{
		"action": "forward", "uid": float64(4), "box": "Sent", "to": []interface{}{"z@ex.com"},
	})
	_, _ = htmlHost.Approve(context.Background(), "draft-1")
	if strings.Contains(htmlSender.last.Text, MsgUntrusted) || strings.Contains(htmlSender.last.Text, "steal") || !strings.Contains(htmlSender.last.Text, "hello") {
		t.Fatalf("html quote=%q", htmlSender.last.Text)
	}
	readHost := testHost(accounts, mail, sender)
	read := readHost.Query(context.Background(), map[string]interface{}{"action": "read", "uid": float64(3), "box": "Sent"})
	if mail.lastBox != "Sent" || !strings.Contains(read, MsgUntrusted) {
		t.Fatalf("read box=%s body=%s", mail.lastBox, read)
	}
	_ = readHost.Query(context.Background(), map[string]interface{}{"action": "download_attachment", "uid": float64(3), "box": "Archive", "index": float64(1)})
	if mail.lastBox != "Archive" {
		t.Fatalf("attachment box=%s", mail.lastBox)
	}
}
