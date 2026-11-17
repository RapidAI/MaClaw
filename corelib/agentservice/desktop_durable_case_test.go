package agentservice

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotDurableCases(t *testing.T) {
	t.Run("resume_from_step_3", func(t *testing.T) {
		db, path := openDurableDB(t)
		const runID = "run-3"
		if _, _, err := db.RunDesktopStep(desktopbot.Step{RunID: runID, Number: 1, Tool: "email", Key: "mail-1"}, "", func() (string, error) {
			return "邮件已发送", nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := db.RunDesktopStep(desktopbot.Step{RunID: runID, Number: 2, Tool: "install", Key: "inst-1"}, "", func() (string, error) {
			return "已安装", nil
		}); err != nil {
			t.Fatal(err)
		}
		failed := 0
		if _, _, err := db.RunDesktopStep(desktopbot.Step{RunID: runID, Number: 3, Tool: "bash", Key: "push-1"}, "git push", func() (string, error) {
			failed++
			return "", fmt.Errorf("step 3 failed")
		}); err == nil || failed != 1 {
			t.Fatalf("failed=%d err=%v", failed, err)
		}
		reads := 0
		for i := 0; i < 2; i++ {
			if _, replayed, err := db.RunDesktopStep(desktopbot.Step{RunID: runID, Tool: "screenshot", Key: "shot-1"}, "", func() (string, error) {
				reads++
				return "png", nil
			}); err != nil || replayed {
				t.Fatalf("read replayed=%v err=%v", replayed, err)
			}
		}
		if reads != 2 {
			t.Fatalf("reads = %d", reads)
		}

		other, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = other.Close() })
		plan, err := other.ResumeDesktopRound(runID, "再发一次邮件")
		if err != nil {
			t.Fatal(err)
		}
		if plan.Start != 3 || plan.Sentence != "" || plan.Display != "再发一次邮件" || !durableBlocked(plan.Blocked, "mail-1") {
			t.Fatalf("plan = %+v", plan)
		}
		again := 0
		text, replayed, err := other.RunDesktopStep(desktopbot.Step{RunID: runID, Number: 1, Tool: "email", Key: "mail-1"}, "", func() (string, error) {
			again++
			return "第二封", nil
		})
		if err != nil || again != 0 || !replayed || text != "邮件已发送" {
			t.Fatalf("again=%d replayed=%v text=%q err=%v", again, replayed, text, err)
		}
		if _, _, err := other.RunDesktopStep(desktopbot.Step{RunID: runID, Number: 3, Tool: "bash", Key: "push-1"}, "git push", func() (string, error) {
			failed++
			return "", fmt.Errorf("step 3 failed")
		}); err == nil || failed != 2 {
			t.Fatalf("retry failed=%d err=%v", failed, err)
		}
		steps, err := other.ListDesktopSteps(runID)
		if err != nil {
			t.Fatal(err)
		}
		if len(steps) != 2 || steps[0].Number != 1 || steps[1].Number != 2 {
			t.Fatalf("steps = %+v", steps)
		}
		for _, step := range steps {
			if step.Tool == "screenshot" || step.Key == "push-1" || step.Key == "shot-1" {
				t.Fatalf("stored a step that is not done: %+v", step)
			}
		}
		if version := durableSchemaVersion(t, path); version != 2 || sqliteRowsFormatVersion != 2 {
			t.Fatalf("schema = %d constant = %d", version, sqliteRowsFormatVersion)
		}
		durableColumns(t, path)
	})

	t.Run("email_not_sent_twice", func(t *testing.T) {
		db, _ := openDurableDB(t)
		sends := 0
		first, replayed, err := db.RunDesktopStep(desktopbot.Step{RunID: "run-mail", Number: 1, Tool: "email", Key: "mail-1"}, "", func() (string, error) {
			sends++
			return "邮件已发送", nil
		})
		second, replayed2, err2 := db.RunDesktopStep(desktopbot.Step{RunID: "run-mail", Number: 1, Tool: "mailbox_send", Key: "mail-1"}, "", func() (string, error) {
			sends++
			return "第二封", nil
		})
		if err != nil || err2 != nil || sends != 1 || replayed || !replayed2 || first != "邮件已发送" || second != "邮件已发送" {
			t.Fatalf("sends=%d first=%q replayed=%v second=%q replayed2=%v err=%v err2=%v", sends, first, replayed, second, replayed2, err, err2)
		}
	})

	t.Run("deploy_idempotent_key", func(t *testing.T) {
		db, path := openDurableDB(t)
		deploys := 0
		run := func() (string, error) {
			deploys++
			return "已部署", nil
		}
		for _, step := range []desktopbot.Step{
			{RunID: "run-deploy", Number: 1, Tool: "install", Key: "inst-1"},
			{RunID: "run-deploy", Number: 2, Tool: "bash", Key: "push-1"},
			{RunID: "run-deploy", Number: 3, Tool: "bash", Key: "up-1"},
			{RunID: "run-pay", Number: 1, Tool: "charge", Key: "pay-1"},
		} {
			command := ""
			switch step.Key {
			case "push-1":
				command = "git push origin main"
			case "up-1":
				command = "docker compose up -d"
			}
			if _, _, err := db.RunDesktopStep(step, command, run); err != nil {
				t.Fatal(err)
			}
			text, replayed, err := db.RunDesktopStep(step, command, run)
			if err != nil || !replayed || text != "已部署" {
				t.Fatalf("key=%s replayed=%v text=%q err=%v", step.Key, replayed, text, err)
			}
		}
		if deploys != 4 {
			t.Fatalf("deploys = %d", deploys)
		}
		if _, err := db.SaveDesktopStep(desktopbot.Step{RunID: "run-deploy", Tool: "install", Key: "call-install", Result: "已安装", Done: true}); err != nil {
			t.Fatal(err)
		}
		exec := &CoreAgentExecutor{}
		exec.SetDesktopSteps(db)
		got := (&coreAgentCallbacks{executor: exec, runtimeRequest: ExecuteRequest{RunID: "run-deploy"}}).executeToolCallLegacy("desktop", `{"action":"install","packages":["curl"]}`, "call-install")
		if got.Result != "已安装" || got.Outcome != agent.ToolExecutionOutcomeOK {
			t.Fatalf("replay = %+v", got)
		}
		if version := durableSchemaVersion(t, path); version != sqliteRowsFormatVersion {
			t.Fatalf("schema = %d", version)
		}
	})

	t.Run("journal_not_on_vm", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "desktops", "u", "service.db")
		db, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		ran := 0
		_, _, err = db.RunDesktopStep(desktopbot.Step{RunID: "run", Tool: "install", Key: "inst-1"}, "", func() (string, error) {
			ran++
			return "已安装", nil
		})
		if !errors.Is(err, desktopbot.ErrJournalOnVM) || ran != 0 {
			t.Fatalf("ran=%d err=%v", ran, err)
		}
		if _, err := db.SaveDesktopStep(desktopbot.Step{RunID: "run", Tool: "email", Key: "mail-1", Result: "邮件已发送", Done: true}); !errors.Is(err, desktopbot.ErrJournalOnVM) {
			t.Fatal(err)
		}
		if version := durableSchemaVersion(t, path); version != sqliteRowsFormatVersion {
			t.Fatalf("schema = %d", version)
		}
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		var n int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='desktop_step_journal'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("the step journal was created on a virtual machine volume")
		}
	})

	t.Run("resume_stays_quiet", func(t *testing.T) {
		db, _ := openDurableDB(t)
		if _, _, err := db.RunDesktopStep(desktopbot.Step{RunID: "run-quiet", Number: 1, Tool: "email", Key: "mail-1"}, "", func() (string, error) {
			return "邮件已发送", nil
		}); err != nil {
			t.Fatal(err)
		}
		plan, err := db.ResumeDesktopRound("run-quiet", "我接着做了")
		if err != nil {
			t.Fatal(err)
		}
		if plan.Sentence != "" || plan.Start != 2 || plan.Display != "我接着做了" {
			t.Fatalf("plan = %+v", plan)
		}
		empty, err := db.ResumeDesktopRound("nobody", "我接着做了")
		if err != nil || empty.Sentence != "" || empty.Start != 1 || empty.Display != "我接着做了" {
			t.Fatalf("empty = %+v err=%v", empty, err)
		}
	})
}

func TestDesktopEffectClaimSurvivesRestart(t *testing.T) {
	db, path := openDurableDB(t)
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = db.RunDesktopStep(desktopbot.Step{
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
		t.Fatal("the effect ran before its claim was stored")
	}
	restarted, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	var again atomic.Int32
	_, _, err = restarted.RunDesktopStep(desktopbot.Step{
		RunID: "run-mail", Tool: "email", Key: "mail-1",
		Bind: desktopbot.ConsentBind("tenant/bob", "acct", "smtp.example", "a@example.com", "hello"),
	}, "", func() (string, error) {
		again.Add(1)
		return "第二封", nil
	})
	if again.Load() != 0 || !errors.As(err, &desktopbot.ErrConsentStale{}) {
		t.Fatalf("stale calls=%d err=%v", again.Load(), err)
	}
	_, _, err = restarted.RunDesktopStep(desktopbot.Step{
		RunID: "run-mail", Tool: "email", Key: "mail-1",
		Bind: desktopbot.ConsentBind("tenant/alice", "acct", "smtp.example", "a@example.com", "hello"),
	}, "", func() (string, error) {
		again.Add(1)
		return "第二封", nil
	})
	if again.Load() != 0 || !errors.As(err, &desktopbot.ErrEffectUncertain{}) {
		t.Fatalf("restart calls=%d err=%v", again.Load(), err)
	}
	close(release)
	<-done
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	text, replayed, err := restarted.RunDesktopStep(desktopbot.Step{
		RunID: "run-mail", Tool: "email", Key: "mail-1",
		Bind: desktopbot.ConsentBind("tenant/alice", "acct", "smtp.example", "a@example.com", "hello"),
	}, "", func() (string, error) {
		again.Add(1)
		return "第三封", nil
	})
	if err != nil || !replayed || text != "邮件已发送" || again.Load() != 0 {
		t.Fatalf("text=%q replayed=%v again=%d err=%v", text, replayed, again.Load(), err)
	}
}

func openDurableDB(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "service.db")
	db, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func durableSchemaVersion(t *testing.T, path string) int {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var version int
	if err := raw.QueryRow(`SELECT format_version FROM as_meta WHERE id=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func durableColumns(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	rows, err := raw.Query(`PRAGMA table_info(desktop_step_journal)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"idempotency_key", "run_id", "step_no", "tool", "result", "done"} {
		if !got[name] {
			t.Fatalf("missing column %s in %+v", name, got)
		}
		delete(got, name)
	}
	if len(got) != 0 {
		t.Fatalf("the step table grew extra columns: %+v", got)
	}
}

func durableBlocked(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}
