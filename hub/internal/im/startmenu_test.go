package im

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartMenuWizardSelectsEditsConfirmsAndLaunches(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_email", "person@example.com")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"templates":[{"title":"发布项目","body":"发布项目\n项目名称：[名称]\n环境：[测试 / 生产]\n说明：[可空]"}]}`
	if err := os.WriteFile(filepath.Join(dir, "document.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newStartMenuService(NewStartMenuTemplateStore(root, func(context.Context, string, string) (string, error) {
		return "person@example.com", nil
	}))
	ctx := context.Background()

	result := service.handle(ctx, "tenant-a", "user-a", "/startmenu")
	if !result.Handled || result.Response == nil || !strings.Contains(result.Response.Body, "1. 发布项目") {
		t.Fatalf("list response = %#v", result)
	}
	result = service.handle(ctx, "tenant-a", "user-a", "1")
	if result.Response == nil || !strings.Contains(result.Response.Body, "项目名称：未填写（必填）") {
		t.Fatalf("select response = %#v body=%q", result, result.Response.Body)
	}
	result = service.handle(ctx, "tenant-a", "user-a", "项目名称 官网")
	if result.Response == nil || !strings.Contains(result.Response.Body, "项目名称 = 官网") {
		t.Fatalf("name response = %#v", result)
	}
	result = service.handle(ctx, "tenant-a", "user-a", "2 生产")
	if result.Response == nil || !strings.Contains(result.Response.Body, "环境 = 生产") {
		t.Fatalf("environment response = %#v", result)
	}
	result = service.handle(ctx, "tenant-a", "user-a", "/run")
	if result.Response == nil || !strings.Contains(result.Response.Body, "/confirm") {
		t.Fatalf("confirm screen = %#v", result)
	}
	result = service.handle(ctx, "tenant-a", "user-a", "/confirm")
	if !result.Handled || !result.LaunchConfirmed || result.Launch != "任务内容：\n发布项目\n项目名称：官网\n环境：生产\n说明：" || result.TaskText != "发布项目\n项目名称：官网\n环境：生产\n说明：" {
		t.Fatalf("launch = %#v", result)
	}
}

func TestStartMenuWizardKeepsSlashPrefixedTemplateOnTaskRoute(t *testing.T) {
	service := newStartMenuService(&StartMenuTemplateStore{})
	key := tenantUserRuntimeKey("", "u")
	service.states[key] = &startMenuState{
		Templates: []startMenuTemplate{{Title: "T", Body: "/cancel"}},
		Selected:  0,
		Confirm:   true,
		UpdatedAt: time.Now(),
	}
	result := service.handle(context.Background(), "", "u", "/confirm")
	if !result.LaunchConfirmed || result.Launch != "任务内容：\n/cancel" {
		t.Fatalf("launch = %#v", result)
	}
}

func TestStartMenuWizardRejectsEmptyRenderedTemplate(t *testing.T) {
	service := newStartMenuService(&StartMenuTemplateStore{})
	key := tenantUserRuntimeKey("", "u")
	service.states[key] = &startMenuState{
		Templates: []startMenuTemplate{{Title: "T", Body: "[可空]"}},
		Selected:  0,
		Fields:    []startMenuField{{Name: "可空", Required: false, Start: 0, End: len("[可空]")}},
		Values:    []string{""},
		Confirm:   true,
		UpdatedAt: time.Now(),
	}
	result := service.handle(context.Background(), "", "u", "/confirm")
	if result.Response == nil || result.Response.StatusCode != 400 || result.Launch != "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestStartMenuWizardExpiresStaleState(t *testing.T) {
	service := newStartMenuService(&StartMenuTemplateStore{})
	service.states[tenantUserRuntimeKey("", "u")] = &startMenuState{
		Templates: []startMenuTemplate{{Title: "T", Body: "任务"}},
		Selected:  0,
		UpdatedAt: time.Now().Add(-startMenuStateTTL - time.Second),
	}
	result := service.handle(context.Background(), "", "u", "/run")
	if result.Response == nil || result.Response.StatusCode != 400 || !strings.Contains(result.Response.Title, "超时") {
		t.Fatalf("expired response = %#v", result)
	}
	if _, ok := service.states[tenantUserRuntimeKey("", "u")]; ok {
		t.Fatal("expired state was not removed")
	}
}

func TestStartMenuWizardRejectsMissingRequiredAndCancels(t *testing.T) {
	service := newStartMenuService(&StartMenuTemplateStore{})
	service.states[tenantUserRuntimeKey("", "u")] = &startMenuState{
		Templates: []startMenuTemplate{{Title: "T", Body: "字段：[值]"}},
		Selected:  0,
		Fields:    []startMenuField{{Name: "字段", Required: true}},
		Values:    []string{""},
		UpdatedAt: time.Now(),
	}
	result := service.handle(context.Background(), "", "u", "/run")
	if result.Response == nil || result.Response.StatusCode != 400 || !strings.Contains(result.Response.Body, "字段") {
		t.Fatalf("missing response = %#v", result)
	}
	result = service.handle(context.Background(), "", "u", "/cancel")
	if result.Response == nil || result.Response.Title != "已取消" {
		t.Fatalf("cancel response = %#v", result)
	}
	if service.states[tenantUserRuntimeKey("", "u")] != nil {
		t.Fatal("state was not removed after cancellation")
	}
}

func TestParseStartMenuFieldsAndAssignment(t *testing.T) {
	fields := parseStartMenuFields("名称：[项目名]\n说明：[可空]\n范围：[optional]")
	if len(fields) != 3 || !fields[0].Required || fields[1].Required || fields[2].Required {
		t.Fatalf("fields = %#v", fields)
	}
	idx, value, ok := parseStartMenuAssignment("名称 我的项目", fields)
	if !ok || idx != 0 || value != "我的项目" {
		t.Fatalf("assignment = %d %q %v fields=%#v", idx, value, ok, fields)
	}
}

func TestParseStartMenuAssignmentAcceptsChineseFieldSeparator(t *testing.T) {
	fields := []startMenuField{{Name: "项目名称"}, {Name: "部署环境"}}
	idx, value, ok := parseStartMenuAssignment("项目名称：官网重构", fields)
	if !ok || idx != 0 || value != "官网重构" {
		t.Fatalf("assignment = %d %q %v", idx, value, ok)
	}
}

func TestParseStartMenuAssignmentDoesNotMatchFieldNamePrefix(t *testing.T) {
	fields := []startMenuField{{Name: "项目"}, {Name: "项目名称"}}
	idx, value, ok := parseStartMenuAssignment("项目名称 官网重构", fields)
	if !ok || idx != 1 || value != "官网重构" {
		t.Fatalf("assignment = %d %q %v", idx, value, ok)
	}
}

func TestParseStartMenuFieldsMatchesWelcomeTemplateRules(t *testing.T) {
	fields := parseStartMenuFields("说明 [普通文本]\n项目：[名称]\n嵌套 [[不应解析]]")
	if len(fields) != 3 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields[0].Name != "普通文本" || fields[1].Name != "项目" || fields[2].Name != "不应解析" {
		t.Fatalf("field names = %#v", fields)
	}
}

func TestStartMenuTemplateStoreRejectsOversizedDocument(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_email", "person@example.com")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "document.json"), make([]byte, maxStartMenuDocumentBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStartMenuTemplateStore(root, func(context.Context, string, string) (string, error) {
		return "person@example.com", nil
	})
	if _, err := store.List(context.Background(), "", "u"); err == nil {
		t.Fatal("expected oversized document error")
	}
}

func TestStartMenuEnvironmentInstructionCarriesRemoteContextWithoutPassword(t *testing.T) {
	got := startMenuEnvironmentInstruction(startMenuTemplate{
		AgentMode: "remote_coding_dev",
		CodingEnv: &startMenuCodingEnv{Remote: &startMenuRemoteEnv{
			Host: "dev.example", Port: 2222, User: "builder", WorkDir: "/srv/app",
		}},
	})
	for _, want := range []string{"dev.example", "2222", "builder", "/srv/app", "不要求用户"} {
		if !strings.Contains(got, want) {
			t.Fatalf("environment instruction missing %q: %q", want, got)
		}
	}
}
