package desktopbot_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	_ "modernc.org/sqlite"
)

func TestEffectClaimCommittedBeforeRunSurvivesRestart(t *testing.T) {
	path := effectDBPath(t)
	db := openEffectDB(t, path)
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = desktopbot.RunClaimed(db, desktopbot.Step{
			RunID: "run-mail", Tool: "email", Key: "mail-1",
			Bind: desktopbot.ConsentBind("tenant/alice", "acct", "smtp.example", "a@example.com", "hello"),
		}, "", func() (string, error) {
			calls.Add(1)
			close(started)
			<-release
			return "邮件已发送", nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("effect ran before the claim was visible")
	}
	restarted := openEffectDB(t, path)
	var again atomic.Int32
	_, _, err := desktopbot.RunClaimed(restarted, desktopbot.Step{
		RunID: "run-mail", Tool: "email", Key: "mail-1",
		Bind: desktopbot.ConsentBind("tenant/alice", "acct", "smtp.example", "a@example.com", "hello"),
	}, "", func() (string, error) {
		again.Add(1)
		return "第二封", nil
	})
	if again.Load() != 0 || !errors.As(err, &desktopbot.ErrEffectUncertain{}) {
		t.Fatalf("restart calls=%d err=%v", again.Load(), err)
	}
	var result string
	var doneFlag int
	if err := restarted.QueryRow(`SELECT result, done FROM desktop_step_journal WHERE idempotency_key=?`, "mail-1").Scan(&result, &doneFlag); err != nil {
		t.Fatal(err)
	}
	if doneFlag != 0 || result == "" || strings.Contains(result, "hello") || strings.Contains(result, "alice") || strings.Contains(result, "smtp.example") {
		t.Fatalf("stored claim result=%q done=%d", result, doneFlag)
	}
	close(release)
	<-done
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestEffectClaimRejectsChangedConsentWithoutReplay(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _, _ = desktopbot.RunClaimed(db, desktopbot.Step{
			RunID: "run", Tool: "charge", Key: "pay-1",
			Bind: desktopbot.ConsentBind("tenant/alice", "acct", "pay.example", "order-1", "10"),
		}, "", func() (string, error) {
			close(started)
			<-release
			return "已扣款", nil
		})
	}()
	<-started
	var calls atomic.Int32
	_, _, err := desktopbot.RunClaimed(db, desktopbot.Step{
		RunID: "run", Tool: "charge", Key: "pay-1",
		Bind: desktopbot.ConsentBind("tenant/bob", "acct", "pay.example", "order-1", "10"),
	}, "", func() (string, error) {
		calls.Add(1)
		return "另一笔", nil
	})
	if calls.Load() != 0 || !errors.As(err, &desktopbot.ErrConsentStale{}) {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	close(release)
}

func TestEffectClaimDefiniteFailureCanBeRetried(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	var calls atomic.Int32
	_, _, err := desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "bash", Key: "push-1"}, "git push", func() (string, error) {
		calls.Add(1)
		return "", errors.New("rejected")
	})
	if err == nil || err.Error() != "rejected" || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	text, replayed, err := desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "bash", Key: "push-1"}, "git push", func() (string, error) {
		calls.Add(1)
		return "已推送", nil
	})
	if err != nil || replayed || text != "已推送" || calls.Load() != 2 {
		t.Fatalf("text=%q replayed=%v calls=%d err=%v", text, replayed, calls.Load(), err)
	}
	text, replayed, err = desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "bash", Key: "push-1"}, "git push", func() (string, error) {
		calls.Add(1)
		return "第二次", nil
	})
	if err != nil || !replayed || text != "已推送" || calls.Load() != 2 {
		t.Fatalf("replay text=%q replayed=%v calls=%d err=%v", text, replayed, calls.Load(), err)
	}
}

func TestEffectClaimKeepsUncertainAndCanceledOutcomes(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	var calls atomic.Int32
	_, _, err := desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "delivery", Key: "del-1"}, "", func() (string, error) {
		calls.Add(1)
		return "", uncertainTestError{}
	})
	if calls.Load() != 1 || !errors.As(err, new(interface{ UncertainExternalEffect() })) {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	_, _, err = desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "delivery", Key: "del-1"}, "", func() (string, error) {
		calls.Add(1)
		return "又发了", nil
	})
	if calls.Load() != 1 || !errors.As(err, &desktopbot.ErrEffectUncertain{}) {
		t.Fatalf("replay calls=%d err=%v", calls.Load(), err)
	}

	_, _, err = desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "email", Key: "mail-cancel"}, "", func() (string, error) {
		calls.Add(1)
		return "", context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, _, err = desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "email", Key: "mail-cancel"}, "", func() (string, error) {
		calls.Add(1)
		return "又发了", nil
	})
	if calls.Load() != 2 || !errors.As(err, &desktopbot.ErrEffectUncertain{}) {
		t.Fatalf("cancel replay calls=%d err=%v", calls.Load(), err)
	}
}

func TestEffectClaimAllowsOneOwner(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var finished atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = desktopbot.RunClaimed(db, desktopbot.Step{RunID: "run", Tool: "install", Key: "inst-1"}, "", func() (string, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return "已安装", nil
			})
			finished.Add(1)
		}()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("neither owner started")
	}
	deadline := time.Now().Add(5 * time.Second)
	for finished.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 || finished.Load() != 1 {
		t.Fatalf("owners=%d finished=%d", calls.Load(), finished.Load())
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("owners after settle=%d", calls.Load())
	}
}

func TestResumeBlocksClaimedEffect(t *testing.T) {
	plan := desktopbot.ResumeExecution("", desktopbot.Sections{NextStep: "再发一次邮件"}, []desktopbot.Step{
		{Number: 1, Tool: "email", Key: "mail-1", Result: desktopbot.MarkUncertainResult("abc"), Done: false},
		{Number: 2, Tool: "install", Key: "inst-1", Result: "已安装", Done: true},
	})
	if plan.Sentence != "" || plan.Start != 3 || !blockedKey(plan.Blocked, "mail-1") || blockedKey(plan.Blocked, "inst-1") {
		t.Fatalf("plan=%+v", plan)
	}
}

type uncertainTestError struct{}

func (uncertainTestError) Error() string            { return "ambiguous" }
func (uncertainTestError) UncertainExternalEffect() {}

func effectDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state", "service.db")
}

func openEffectDB(t *testing.T, path string) *sql.DB {
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
