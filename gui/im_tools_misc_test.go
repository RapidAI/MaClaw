package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestFindSkillDefinitionFileSupportsYAMLVariantsOnly(t *testing.T) {
	root := t.TempDir()

	ymlDir := filepath.Join(root, "yml-skill")
	if err := os.MkdirAll(ymlDir, 0755); err != nil {
		t.Fatalf("MkdirAll(ymlDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ymlDir, "skill.yml"), []byte("name: yml-skill\n"), 0644); err != nil {
		t.Fatalf("WriteFile(skill.yml) error = %v", err)
	}
	path, format := findSkillDefinitionFile(ymlDir)
	if filepath.Base(path) != "skill.yml" || format != "yaml" {
		t.Fatalf("findSkillDefinitionFile(skill.yml) = (%q, %q), want skill.yml/yaml", path, format)
	}
	jsonDir := filepath.Join(root, "json-skill")
	if err := os.MkdirAll(jsonDir, 0755); err != nil {
		t.Fatalf("MkdirAll(jsonDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "skill.json"), []byte(`{"name":"json-skill"}`), 0644); err != nil {
		t.Fatalf("WriteFile(skill.json) error = %v", err)
	}
	path, format = findSkillDefinitionFile(jsonDir)
	if path != "" || format != "" {
		t.Fatalf("findSkillDefinitionFile(skill.json) = (%q, %q), want none", path, format)
	}
}

func TestValidateSkillFileContentRejectsJSONFormat(t *testing.T) {
	data := []byte(`{"name":"compat","steps":[{"run":"echo hi"}]}`)
	if got := validateSkillFileContent(data, "json"); got == "" {
		t.Fatal("validateSkillFileContent(json) should reject retired JSON skill definitions")
	}
}

func TestToolListScheduledTasksInitializesSharedAppManager(t *testing.T) {
	baseDir := t.TempDir()
	app := &App{testHomeDir: baseDir}
	manager, err := scheduler.NewManager(filepath.Join(baseDir, ".maclaw", "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)
	if _, err := manager.Add(scheduler.ScheduledTask{
		Name:       "蓝信日报",
		Action:     "发送日报",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	app.scheduledTaskManager = manager

	handler := &IMMessageHandler{app: app}
	got := handler.toolListScheduledTasks()
	if !strings.Contains(got, "蓝信日报") {
		t.Fatalf("toolListScheduledTasks() = %q, want shared task", got)
	}
	if handler.scheduledTaskManager != manager {
		t.Fatal("toolListScheduledTasks() did not bind the App scheduler to the handler")
	}
}

func TestToolManageScheduleRunTriggersTask(t *testing.T) {
	manager, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)

	executed := make(chan *scheduler.ScheduledTask, 1)
	manager.SetExecutor(func(_ context.Context, task *scheduler.ScheduledTask) (string, error) {
		executed <- task
		return "done", nil
	})
	id, err := manager.Add(scheduler.ScheduledTask{
		Name:       "蓝信日报",
		Action:     "发送日报",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	handler := &IMMessageHandler{scheduledTaskManager: manager}
	got := handler.toolManageSchedule(map[string]interface{}{"action": "run", "id": id})
	if !strings.Contains(got, "已启动定时任务") || !strings.Contains(got, id) {
		t.Fatalf("toolManageSchedule(run) = %q, want launch confirmation with ID", got)
	}

	select {
	case task := <-executed:
		if task.ID != id {
			t.Fatalf("executor task ID = %q, want %q", task.ID, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled task was not executed")
	}
}

func TestToolManageSchedulePauseAndResume(t *testing.T) {
	manager, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(manager.Stop)
	id, err := manager.Add(scheduler.ScheduledTask{
		Name:       "微信提醒",
		Action:     "发送提醒",
		Hour:       9,
		Minute:     0,
		DayOfWeek:  -1,
		DayOfMonth: -1,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	handler := &IMMessageHandler{scheduledTaskManager: manager}

	if got := handler.toolManageSchedule(map[string]interface{}{"action": "pause", "id": id}); !strings.Contains(got, "已暂停") {
		t.Fatalf("toolManageSchedule(pause) = %q", got)
	}
	if task := manager.Get(id); task == nil || task.Status != "paused" {
		t.Fatalf("task after pause = %#v, want paused", task)
	}
	if got := handler.toolManageSchedule(map[string]interface{}{"action": "resume", "id": id}); !strings.Contains(got, "已恢复") {
		t.Fatalf("toolManageSchedule(resume) = %q", got)
	}
	if task := manager.Get(id); task == nil || task.Status != "active" {
		t.Fatalf("task after resume = %#v, want active", task)
	}
}

func TestParseScheduleDeliveryArgsDefaultsToRuntimePlatform(t *testing.T) {
	args := map[string]interface{}{
		"user_id":                          "self",
		registeredToolRuntimePlatformField: "weixin",
	}
	delivery, err := parseScheduleDeliveryArgs(args)
	if err != nil {
		t.Fatalf("parseScheduleDeliveryArgs() error = %v", err)
	}
	if delivery == nil || delivery.Channel != scheduler.DeliveryChannelWeixin {
		t.Fatalf("delivery = %#v, want weixin channel", delivery)
	}
	if _, ok := args[registeredToolRuntimePlatformField]; ok {
		t.Fatal("runtime platform must not leak into persisted delivery args")
	}
}

func TestParseScheduleDeliveryArgsCanonicalizesLocalGatewayPlatform(t *testing.T) {
	for platform, want := range map[string]string{
		"lansenger_local": scheduler.DeliveryChannelLansenger,
		"weixin_local":    scheduler.DeliveryChannelWeixin,
		"telegram_local":  scheduler.DeliveryChannelTelegram,
		"qqbot_local":     scheduler.DeliveryChannelQQ,
	} {
		args := map[string]interface{}{
			"user_id":                          "self",
			registeredToolRuntimePlatformField: platform,
		}
		delivery, err := parseScheduleDeliveryArgs(args)
		if err != nil {
			t.Fatalf("platform %q: parseScheduleDeliveryArgs() error = %v", platform, err)
		}
		if delivery == nil || delivery.Channel != want {
			t.Fatalf("platform %q: delivery = %#v, want channel %q", platform, delivery, want)
		}
		if _, ok := args[registeredToolRuntimePlatformField]; ok {
			t.Fatalf("platform %q: runtime platform metadata should be consumed", platform)
		}
	}
}

func TestScheduledTaskManagerForToolConcurrentSharedBinding(t *testing.T) {
	manager, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Stop()

	app := &App{scheduledTaskManager: manager}
	handler := &IMMessageHandler{app: app}
	const workers = 32
	results := make(chan *scheduler.Manager, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- handler.scheduledTaskManagerForTool()
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got != manager {
			t.Fatalf("shared manager = %p, want %p", got, manager)
		}
	}
}
