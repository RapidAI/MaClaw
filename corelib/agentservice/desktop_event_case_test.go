package agentservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// quietMemoryExecutor is one round that only reads memory. It does not call Session.
type quietMemoryExecutor struct {
	histories [][]Message
}

func (e *quietMemoryExecutor) Execute(_ context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	e.histories = append(e.histories, append([]Message(nil), req.History...))
	return &ExecuteResult{Content: "无变化", OutputType: "text/plain"}, nil
}

func TestDesktopBotEventCases(t *testing.T) {
	t.Run("message_loads_db_no_ensure", func(t *testing.T) {
		exec := &quietMemoryExecutor{}
		svc, principal, inst := setupCaptureAgentService(t, exec)
		if svc.ScheduledTaskStore() != nil {
			t.Fatal("a memory service opened a second schedule database")
		}
		var finished int
		previous := FinishDesktopRound
		FinishDesktopRound = func(tenantID, userID, runID string) {
			if tenantID != principal.TenantID || userID != principal.UserID || runID == "" {
				t.Errorf("finish tenant=%s user=%s run=%s", tenantID, userID, runID)
			}
			finished++
		}
		t.Cleanup(func() { FinishDesktopRound = previous })

		sess, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{Content: "查一下记忆"})
		if err != nil || sess == nil || finished != 1 {
			t.Fatalf("first send err=%v finished=%d", err, finished)
		}
		if _, _, _, err = svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{SessionID: sess.ID, Content: "再查一次"}); err != nil || finished != 2 {
			t.Fatalf("second send err=%v finished=%d", err, finished)
		}
		if len(exec.histories) != 2 {
			t.Fatalf("rounds = %d", len(exec.histories))
		}
		if !historyHas(exec.histories[1], "查一下记忆") || !historyHas(exec.histories[1], "再查一次") {
			t.Fatalf("history = %+v", exec.histories[1])
		}
	})

	t.Run("schedule_rows_in_service_db", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "state", "service.db")
		store, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		task := scheduler.ScheduledTask{
			ID: "morning", Name: "morning", Action: "read memory",
			Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
		}
		if err := store.SaveScheduledTasks([]scheduler.ScheduledTask{task}); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		again, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = again.Close() })
		loaded, err := again.LoadScheduledTasks()
		if err != nil || len(loaded) != 1 || loaded[0].ID != "morning" || loaded[0].Action != "read memory" || loaded[0].Hour != 8 {
			t.Fatalf("loaded=%+v err=%v", loaded, err)
		}
		svc, err := NewService(Config{DataRoot: dir, TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, again, &captureExecutor{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		if svc.ScheduledTaskStore() == nil {
			t.Fatal("the worker database has no schedule rows")
		}
	})
}

func TestServiceScheduleImportOnceLeavesClearedTasks(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "scheduled_tasks.json")
	raw, err := json.Marshal([]scheduler.ScheduledTask{{
		ID: "legacy-1", Name: "morning", Action: "read memory",
		Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "state", "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	n, err := scheduler.ImportLegacyTasksOnce(jsonPath, store)
	if err != nil || n != 1 {
		t.Fatalf("import n=%d err=%v", n, err)
	}
	if err := store.SaveScheduledTasks(nil); err != nil {
		t.Fatal(err)
	}
	again, err := scheduler.ImportLegacyTasksOnce(jsonPath, store)
	if err != nil || again != 0 {
		t.Fatalf("second import n=%d err=%v", again, err)
	}
	loaded, err := store.LoadScheduledTasks()
	if err != nil || len(loaded) != 0 {
		t.Fatalf("cleared tasks came back: %+v err=%v", loaded, err)
	}
	kept, err := os.ReadFile(jsonPath)
	if err != nil || string(kept) != string(raw) {
		t.Fatal("scheduled_tasks.json was rewritten or removed")
	}
}

func historyHas(messages []Message, content string) bool {
	for _, msg := range messages {
		if msg.Role == MessageRoleUser && msg.Content == content {
			return true
		}
	}
	return false
}
