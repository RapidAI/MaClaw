package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestDesktopBotDurableCases(t *testing.T) {
	t.Run("deploy_idempotent_key", func(t *testing.T) {
		t.Setenv(desktopHubURLEnv, "")
		t.Setenv(desktopHubTokenEnv, "")
		db := openSrvDurableDB(t)
		if _, err := db.SaveDesktopStep(desktopbot.Step{
			RunID: "run-1", Number: 1, Tool: "install", Key: "run-1:call-install", Result: "已安装", Done: true,
		}); err != nil {
			t.Fatal(err)
		}
		bindDesktopSteps(db)
		t.Cleanup(func() { bindDesktopSteps(nil) })
		opened := 0
		prev := desktopRemoteSession
		desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
			opened++
			return desktopEndpoint{}, context.Canceled
		}
		t.Cleanup(func() { desktopRemoteSession = prev })

		ctx := agentruntime.WithToolCallID(context.Background(), "call-install")
		text, err := operateDesktop(ctx, agentruntime.Scope{TenantID: "tenant", UserID: "alice", RunID: "run-1"}, map[string]any{
			"action": "install", "packages": []any{"curl"},
		})
		if err != nil || text != "已安装" || opened != 0 {
			t.Fatalf("text=%q err=%v opened=%d", text, err, opened)
		}
	})

	t.Run("email_not_sent_twice", func(t *testing.T) {
		db := openSrvDurableDB(t)
		if _, _, err := db.RunDesktopStep(desktopbot.Step{RunID: "run-mail", Tool: "delivery", Key: "delivery:run-mail"}, "", func() (string, error) {
			return "首页开了", nil
		}); err != nil {
			t.Fatal(err)
		}
		bindDesktopSteps(db)
		t.Cleanup(func() { bindDesktopSteps(nil) })
		task := durableDeliveryTask()
		if err := deliverScheduledTaskResultForRun(nil, agentservice.Principal{}, task, "首页开了", nil, "run-mail"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("resume_stays_quiet", func(t *testing.T) {
		db := openSrvDurableDB(t)
		bindDesktopSteps(db)
		t.Cleanup(func() { bindDesktopSteps(nil) })
		task := durableDeliveryTask()
		if err := deliverScheduledTaskResultForRun(nil, agentservice.Principal{}, task, "无变化", nil, "run-quiet"); err != nil {
			t.Fatal(err)
		}
		steps, err := db.ListDesktopSteps("run-quiet")
		if err != nil || len(steps) != 0 {
			t.Fatalf("steps=%+v err=%v", steps, err)
		}
		if err := deliverScheduledTaskResultForRun(nil, agentservice.Principal{}, task, "首页开了", nil, "run-quiet"); err == nil {
			t.Fatal("a quiet round was recorded as a delivery")
		}
		plan, err := db.ResumeDesktopRound("run-quiet", "我接着做了")
		if err != nil || plan.Sentence != "" || plan.Start != 1 || plan.Display != "我接着做了" {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
	})
}

func openSrvDurableDB(t *testing.T) *agentservice.SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "service.db")
	db, err := agentservice.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func durableDeliveryTask() *scheduler.ScheduledTask {
	return &scheduler.ScheduledTask{
		ID: "morning", Name: "morning",
		Delivery: &scheduler.TaskDelivery{
			Enabled: true,
			Channel: scheduler.DeliveryChannelLansenger,
			On:      scheduler.DeliveryOnAlways,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "alice"}},
		},
	}
}
