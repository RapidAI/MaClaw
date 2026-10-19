package mailbox

import (
	"context"
	"testing"
	"time"
)

type memorySteps struct {
	result   string
	done     bool
	finishes int
}

func (m *memorySteps) Finished(string) (string, bool, error) {
	return m.result, m.done, nil
}

func (m *memorySteps) Finish(_, _, result string) error {
	m.finishes++
	m.result = result
	m.done = true
	return nil
}

func TestDesktopBotDurableCases(t *testing.T) {
	t.Run("email_not_sent_twice", func(t *testing.T) {
		accounts := twoAccounts(t)
		sender := &recordingSender{}
		host := testHost(accounts, nil, sender)
		host.Drafts.Put(Draft{
			ID:        "draft-mail",
			Account:   accounts[0],
			Message:   OutboundMessage{To: []string{"a@ex.com"}, Subject: "Hi", Text: "hello"},
			ExpiresAt: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
		})
		journal := &memorySteps{}
		host.Steps = journal
		host.StepKey = "draft-mail"
		text, class := host.Approve(context.Background(), "draft-mail")
		if class != "approved" || text != "邮件已发送" || sender.calls != 1 || journal.finishes != 1 {
			t.Fatalf("first class=%s text=%q calls=%d finishes=%d", class, text, sender.calls, journal.finishes)
		}
		text, class = host.Approve(context.Background(), "draft-mail")
		if class != "approved" || text != "邮件已发送" || sender.calls != 1 || journal.finishes != 1 {
			t.Fatalf("second class=%s text=%q calls=%d finishes=%d", class, text, sender.calls, journal.finishes)
		}
	})
}
