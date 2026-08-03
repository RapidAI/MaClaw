package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartMenuTaskCreatedReplyReflectsRemoteAutoStart(t *testing.T) {
	remote := startMenuTaskCreatedReply(true)
	if !strings.Contains(remote, "自动连接 SSH") || !strings.Contains(remote, "无需手动操作") {
		t.Fatalf("remote launch reply must describe automatic startup, got %q", remote)
	}
	if strings.Contains(remote, "重新连接 SSH 后启动") {
		t.Fatalf("remote launch reply must not instruct obsolete manual reconnect, got %q", remote)
	}
	if got := startMenuTaskCreatedReply(false); strings.Contains(got, "SSH") {
		t.Fatalf("local launch reply must not mention SSH, got %q", got)
	}
}

func TestLocalStartMenuWizardFillsAndConfirms(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = []LocalStartMenuTemplate{{
		Title: "发布说明",
		Body:  "为项目： [项目名]\n生成发布说明",
	}}
	service.loaded = true

	key := "lansenger:group-1:user-1"
	if result := service.handle(key, "/startmenu"); !result.Handled || !strings.Contains(result.Reply, "1. 发布说明") {
		t.Fatalf("menu result = %#v", result)
	}
	if result := service.handle(key, "1"); !result.Handled || !strings.Contains(result.Reply, "项目") {
		t.Fatalf("selection result = %#v", result)
	}
	if result := service.handle(key, "1 MaClaw"); !result.Handled || !strings.Contains(result.Reply, "MaClaw") {
		t.Fatalf("assignment result = %#v", result)
	}
	if result := service.handle(key, "/run"); !result.Handled || !strings.Contains(result.Reply, "/confirm") {
		t.Fatalf("run result = %#v", result)
	}
	result := service.handle(key, "/confirm")
	if !result.Handled || !result.Confirmed || result.TaskTitle != "发布说明" || !strings.Contains(result.TaskText, "MaClaw") {
		t.Fatalf("confirm result = %#v", result)
	}
}

func TestLocalStartMenuRetainsRemoteDiagnosisSafety(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = sanitizeLocalStartMenuTemplates([]LocalStartMenuTemplate{{
		Title:        "Diagnose remote service",
		Body:         "Inspect [service] startup failure",
		AgentMode:    "remote_coding_dev",
		RemoteSafety: "diagnosis",
		CodingEnv: &localStartMenuCodingEnv{Remote: &localStartMenuRemoteEnv{
			Host: "ops.example.test", User: "ops", WorkDir: "/srv/service", Port: 22,
		}},
	}})
	service.loaded = true

	key := "lansenger:user"
	_ = service.handle(key, "/startmenu")
	_ = service.handle(key, "1")
	_ = service.handle(key, "service api")
	_ = service.handle(key, "/run")
	result := service.handle(key, "/confirm")
	if !result.Confirmed || result.RemoteSafety != "diagnosis" {
		t.Fatalf("diagnosis safety was not retained: %#v", result)
	}

	local := sanitizeLocalStartMenuTemplates([]LocalStartMenuTemplate{{
		Title: "local", Body: "task", AgentMode: "coding_dev", RemoteSafety: "diagnosis",
	}})
	if local[0].RemoteSafety != "" {
		t.Fatalf("local template retained remote safety: %#v", local[0])
	}
}

func TestLocalStartMenuListsSavedCommonTasksOnly(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = []LocalStartMenuTemplate{
		{Title: "远程开发新项目", Body: "在远程环境中创建 [项目名]"},
		{Title: "代码审查与改进", Body: "审查 [仓库] 并给出改进建议"},
	}
	service.loaded = true

	result := service.handle("lansenger:user", "/startmenu")
	if !result.Handled || !strings.Contains(result.Reply, "已保存的常用任务") {
		t.Fatalf("menu result = %#v", result)
	}
	for _, title := range []string{"1. 远程开发新项目", "2. 代码审查与改进"} {
		if !strings.Contains(result.Reply, title) {
			t.Fatalf("saved common task %q missing from menu: %s", title, result.Reply)
		}
	}
	if !strings.Contains(result.Reply, "不包含最近任务或场景推荐") {
		t.Fatalf("menu must describe its saved-task-only scope: %s", result.Reply)
	}
}

func TestLocalStartMenuListNormalizesTitleAndBoundsPreview(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	longFirstLine := strings.Repeat("甲", localStartMenuMenuPreviewRunes+1)
	service.templates = sanitizeLocalStartMenuTemplates([]LocalStartMenuTemplate{{
		Title: "  常用\n  任务  ",
		Body:  longFirstLine + "\n第二行不应显示",
	}})
	service.loaded = true

	result := service.handle("lansenger:user", "/startmenu")
	if !strings.Contains(result.Reply, "1. 常用 任务\n") {
		t.Fatalf("title was not normalized to one line: %q", result.Reply)
	}
	if !strings.Contains(result.Reply, strings.Repeat("甲", localStartMenuMenuPreviewRunes)+"…") {
		t.Fatalf("long first line was not truncated: %q", result.Reply)
	}
	if strings.Contains(result.Reply, "第二行不应显示") {
		t.Fatalf("preview must only show the first body line: %q", result.Reply)
	}
}

func TestLocalStartMenuRunRequiresRequiredFields(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = []LocalStartMenuTemplate{{Title: "任务", Body: "目标：[必填目标]"}}
	service.loaded = true
	key := "lansenger:user"
	_ = service.handle(key, "/startmenu")
	_ = service.handle(key, "1")
	result := service.handle(key, "/run")
	if !result.Handled || !strings.Contains(result.Reply, "仍缺少必填项") {
		t.Fatalf("missing field result = %#v", result)
	}
}

func TestLocalStartMenuBoundsParameterAndExpandedTaskLength(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = []LocalStartMenuTemplate{{Title: "任务", Body: "目标：[目标]"}}
	service.loaded = true
	key := "lansenger:user"
	_ = service.handle(key, "/startmenu")
	_ = service.handle(key, "1")

	result := service.handle(key, "目标 "+strings.Repeat("甲", localStartMenuMaxValueRunes+1))
	if !result.Handled || !strings.Contains(result.Reply, "参数值过长") {
		t.Fatalf("oversized parameter result = %#v", result)
	}
	if got := service.states[key].Values[0]; got != "" {
		t.Fatalf("oversized parameter was retained: %d runes", len([]rune(got)))
	}

	service.states[key].Values[0] = strings.Repeat("乙", localStartMenuMaxTaskRunes+1)
	_ = service.handle(key, "/run")
	result = service.handle(key, "/confirm")
	if !result.Handled || result.Confirmed || !strings.Contains(result.Reply, "任务内容过长") {
		t.Fatalf("oversized expanded task result = %#v", result)
	}
}

func TestLocalStartMenuKeepsOtherSlashCommandsInsideWizard(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = []LocalStartMenuTemplate{{Title: "任务", Body: "目标：[目标]"}}
	service.loaded = true
	key := "lansenger:user"
	_ = service.handle(key, "/startmenu")
	_ = service.handle(key, "1")

	result := service.handle(key, "/run deploy --confirm")
	if !result.Handled || !strings.Contains(result.Reply, "/cancel") {
		t.Fatalf("other slash command result = %#v", result)
	}
	if !service.active(key) {
		t.Fatal("other slash command must not discard the active wizard")
	}

	result = service.handle(key, "/CANCEL")
	if !result.Handled || !strings.Contains(result.Reply, "已退出") || service.active(key) {
		t.Fatalf("case-insensitive cancel result = %#v", result)
	}
}

func TestLocalStartMenuFillsRepeatedNamedField(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	service.templates = []LocalStartMenuTemplate{{
		Title: "重复参数",
		Body:  "项目：[项目名]\n为 [项目名] 生成发布说明",
	}}
	service.loaded = true
	key := "lansenger:user"
	_ = service.handle(key, "/startmenu")
	_ = service.handle(key, "1")
	_ = service.handle(key, "项目名 MaClaw")
	if result := service.handle(key, "/run"); !result.Handled || !strings.Contains(result.Reply, "/confirm") {
		t.Fatalf("run result = %#v", result)
	}
	result := service.handle(key, "/confirm")
	if !result.Confirmed || strings.Count(result.TaskText, "MaClaw") != 2 {
		t.Fatalf("confirm result = %#v", result)
	}
}

func TestParseLocalStartMenuFieldsUsesHintForProsePrefix(t *testing.T) {
	fields := parseLocalStartMenuFields("为 [项目名] 生成发布说明\n标题：[标题]")
	if len(fields) != 2 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].Name != "项目名" || fields[1].Name != "标题" {
		t.Fatalf("field names = %#v", fields)
	}
}

func TestLocalStartMenuActiveIsScopedAndExpires(t *testing.T) {
	app := NewApp()
	service := app.localStartMenuService()
	key := "lansenger:g1:user-a"
	other := "lansenger:g1:user-b"
	service.states[key] = &localStartMenuState{UpdatedAt: time.Now()}
	if !service.active(key) || service.active(other) {
		t.Fatalf("active state must be scoped to the starter")
	}
	service.states[key].UpdatedAt = time.Now().Add(-localStartMenuStateTTL - time.Second)
	if service.active(key) {
		t.Fatal("expired wizard must not bypass group mention gate")
	}
}

func TestUpdateLocalStartMenuTemplatesPersistsSanitizedSnapshot(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	if err := app.UpdateLocalStartMenuTemplates([]LocalStartMenuTemplate{{
		Title:     "  本地编码  ",
		Body:      "  修复 [模块]  ",
		AgentMode: "coding_dev",
		CodingEnv: &localStartMenuCodingEnv{WorkingDir: "  C:/work  "},
	}}); err != nil {
		t.Fatalf("UpdateLocalStartMenuTemplates: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(app.GetDataDir(), "local-startmenu-templates.json"))
	if err != nil {
		t.Fatalf("read persisted snapshot: %v", err)
	}
	var doc localStartMenuDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode persisted snapshot: %v", err)
	}
	if len(doc.Templates) != 1 || doc.Templates[0].Title != "本地编码" || doc.Templates[0].CodingEnv.WorkingDir != "C:/work" {
		t.Fatalf("persisted snapshot = %#v", doc)
	}
}

func TestUpdateLocalStartMenuTemplatesDoesNotMutateOrRetainInputPointers(t *testing.T) {
	app := NewApp()
	app.testHomeDir = t.TempDir()
	input := []LocalStartMenuTemplate{{
		Title: "  常用任务  ",
		Body:  "  完成 [目标]  ",
		CodingEnv: &localStartMenuCodingEnv{
			WorkingDir: "  C:/work  ",
			Remote:     &localStartMenuRemoteEnv{Host: "  host-a  ", User: "  alice  ", WorkDir: "  /repo  "},
		},
	}}
	if err := app.UpdateLocalStartMenuTemplates(input); err != nil {
		t.Fatalf("UpdateLocalStartMenuTemplates: %v", err)
	}
	if input[0].Title != "  常用任务  " || input[0].CodingEnv.WorkingDir != "  C:/work  " || input[0].CodingEnv.Remote.Host != "  host-a  " {
		t.Fatalf("input was mutated: %#v", input[0])
	}
	input[0].Title = "已被调用方修改"
	input[0].CodingEnv.Remote.Host = "host-b"

	service := app.localStartMenuService()
	result := service.handle("lansenger:user", "/startmenu")
	if !strings.Contains(result.Reply, "常用任务") || strings.Contains(result.Reply, "已被调用方修改") {
		t.Fatalf("service retained caller-owned template: %s", result.Reply)
	}
	selected := service.handle("lansenger:user", "1")
	if !selected.Handled || service.states["lansenger:user"].Templates[0].CodingEnv.Remote.Host != "host-a" {
		t.Fatalf("wizard did not retain sanitized snapshot: %#v", selected)
	}
}
