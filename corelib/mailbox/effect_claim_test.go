package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	_ "modernc.org/sqlite"
)

type journalRunner struct {
	db    *sql.DB
	runID string
}

func (j journalRunner) Finished(key string) (string, bool, error) {
	var result string
	var done int
	err := j.db.QueryRow(`SELECT result, done FROM desktop_step_journal WHERE idempotency_key=?`, key).Scan(&result, &done)
	if errors.Is(err, sql.ErrNoRows) || done != 1 {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", false, err
		}
		return "", false, nil
	}
	return result, true, nil
}

func (j journalRunner) Finish(string, string, string) error { return nil }

func (j journalRunner) RunExternalEffect(effect ExternalEffect) (string, bool, error) {
	return desktopbot.RunClaimed(j.db, desktopbot.Step{
		RunID: j.runID,
		Tool:  effect.Tool,
		Key:   effect.Key,
		Bind:  effect.Bind,
	}, "", effect.Run)
}

type gateSender struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	err     error
}

func (s *gateSender) Send(context.Context, Account, string, OutboundMessage) error {
	s.calls.Add(1)
	if s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.release != nil {
		<-s.release
	}
	return s.err
}

func TestApproveClaimsSendBeforeTheProviderAndDoesNotReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "service.db")
	db := openClaimDB(t, path)
	accounts := twoAccounts(t)
	sender := &gateSender{started: make(chan struct{}), release: make(chan struct{})}
	host := claimHost(t, accounts, sender, db)
	const body = "unique-body-not-stored"
	host.Drafts.Put(Draft{
		ID: "draft-mail", Principal: "tenant/alice", Account: accounts[0],
		Message:   OutboundMessage{To: []string{"a@ex.com"}, Subject: "Hi", Text: body},
		ExpiresAt: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = host.Approve(context.Background(), "draft-mail")
	}()
	select {
	case <-sender.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not called")
	}
	if sender.calls.Load() != 1 {
		t.Fatalf("calls=%d", sender.calls.Load())
	}
	var stored string
	if err := db.QueryRow(`SELECT result FROM desktop_step_journal WHERE idempotency_key=? AND done=0`, "draft-mail").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" || containsAny(stored, body, "stored-secret", "a@ex.com") {
		t.Fatalf("journal stored the message: %q", stored)
	}

	other := claimHost(t, accounts, &gateSender{}, openClaimDB(t, path))
	changed := accounts[0]
	changed.Outbound = epOut("smtp.other.example")
	other.Drafts.Put(Draft{
		ID: "draft-mail", Principal: "tenant/alice", Account: changed,
		Message:   OutboundMessage{To: []string{"a@ex.com"}, Subject: "Hi", Text: body},
		ExpiresAt: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	})
	text, class := other.Approve(context.Background(), "draft-mail")
	if class != "uncertain" || text == "" || other.Steps == nil {
		t.Fatalf("stale class=%s text=%q", class, text)
	}
	if gate, ok := other.Sender.(*gateSender); !ok || gate.calls.Load() != 0 {
		t.Fatal("changed host replayed the send")
	}

	close(sender.release)
	<-done
	same := claimHost(t, accounts, &gateSender{}, openClaimDB(t, path))
	same.Drafts.Put(Draft{
		ID: "draft-mail", Principal: "tenant/alice", Account: accounts[0],
		Message:   OutboundMessage{To: []string{"a@ex.com"}, Subject: "Hi", Text: body},
		ExpiresAt: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	})
	text, class = same.Approve(context.Background(), "draft-mail")
	if class != "approved" || text != "邮件已发送" {
		t.Fatalf("settled class=%s text=%q", class, text)
	}
	if gate, ok := same.Sender.(*gateSender); !ok || gate.calls.Load() != 0 {
		t.Fatal("settled claim sent again")
	}
}

func TestApproveKeepsUncertainDelete(t *testing.T) {
	db := openClaimDB(t, filepath.Join(t.TempDir(), "state", "service.db"))
	accounts := twoAccounts(t)
	deleter := &fakeDeleter{applyErr: ErrUncertain}
	host := claimHost(t, accounts, &recordingSender{}, db)
	host.Deleter = deleter
	host.StepKey = "draft-del"
	host.Drafts.Put(Draft{
		ID: "draft-del", Principal: "tenant/alice", Kind: DraftKindDelete, Account: accounts[0],
		Delete:    DeletePlan{Scope: DeleteScopeFolder, Box: "INBOX", UIDs: []uint32{7}},
		ExpiresAt: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	})
	text, class := host.Approve(context.Background(), "draft-del")
	if class != "uncertain" || text != MsgDeleteUncertain || deleter.applyCalls != 1 {
		t.Fatalf("text=%s class=%s applies=%d", text, class, deleter.applyCalls)
	}
	host.Drafts.Put(Draft{
		ID: "draft-del", Principal: "tenant/alice", Kind: DraftKindDelete, Account: accounts[0],
		Delete:    DeletePlan{Scope: DeleteScopeFolder, Box: "INBOX", UIDs: []uint32{7}},
		ExpiresAt: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	})
	text, class = host.Approve(context.Background(), "draft-del")
	if class != "uncertain" || deleter.applyCalls != 1 {
		t.Fatalf("replay text=%s class=%s applies=%d", text, class, deleter.applyCalls)
	}
}

func TestDraftEffectBindHidesSecretAndChangesWithHost(t *testing.T) {
	accounts := twoAccounts(t)
	draft := Draft{
		Principal: "tenant/alice", Account: accounts[0],
		Message: OutboundMessage{To: []string{"a@ex.com"}, Subject: "Hi", Text: "stored-secret-body", Files: []OutboundFile{{Name: "a.txt", Bytes: []byte("stored-secret-file")}}},
	}
	bind := DraftEffectBind(draft)
	if containsAny(bind, "stored-secret-body", "stored-secret-file", "stored-secret") {
		t.Fatalf("bind kept content: %s", bind)
	}
	moved := draft
	moved.Account.Outbound = epOut("smtp.other.example")
	if DraftEffectBind(moved) == bind {
		t.Fatal("host change kept the same consent")
	}
	otherOwner := draft
	otherOwner.Principal = "tenant/bob"
	if DraftEffectBind(otherOwner) == bind {
		t.Fatal("owner change kept the same consent")
	}
}

func claimHost(t *testing.T, accounts []Account, sender MailSender, db *sql.DB) *Host {
	t.Helper()
	host := testHost(accounts, nil, sender)
	host.Steps = journalRunner{db: db, runID: "run-mail"}
	host.StepKey = "draft-mail"
	return host
}

func openClaimDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if err := desktopbot.EnsureEffectJournal(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func containsAny(s string, parts ...string) bool {
	for _, part := range parts {
		if part != "" && len(s) >= len(part) {
			for i := 0; i+len(part) <= len(s); i++ {
				if s[i:i+len(part)] == part {
					return true
				}
			}
		}
	}
	return false
}
