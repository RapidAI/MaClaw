package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParsePassthroughRunText(t *testing.T) {
	name, values, confirmed, err := parsePassthroughRunText(`/run repair-env --target "D:\work prj" --deep true --confirm`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if name != "repair-env" || !confirmed {
		t.Fatalf("name=%q confirmed=%v", name, confirmed)
	}
	if values["target"] != `D:\work prj` || values["deep"] != "true" {
		t.Fatalf("values=%#v", values)
	}
}

func TestParsePassthroughExecText(t *testing.T) {
	program, args, confirmed, err := parsePassthroughExecText(`/exec git status --short --confirm`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if program != "git" || !confirmed || strings.Join(args, " ") != "status --short" {
		t.Fatalf("program=%q args=%#v confirmed=%v", program, args, confirmed)
	}
}

func TestParsePassthroughExecTextSupportsLiteralArgsAfterDelimiter(t *testing.T) {
	program, args, confirmed, err := parsePassthroughExecText(`/exec tool --confirm -- --confirm --mode safe`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if program != "tool" || !confirmed || strings.Join(args, " ") != "--confirm --mode safe" {
		t.Fatalf("program=%q args=%#v confirmed=%v", program, args, confirmed)
	}
}

func TestSplitPassthroughFieldsPreservesEmptyQuotedArgs(t *testing.T) {
	fields, err := splitPassthroughFields(`tool "" '' "non empty"`)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if len(fields) != 4 || fields[0] != "tool" || fields[1] != "" || fields[2] != "" || fields[3] != "non empty" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
}

func TestSlashHelpIncludesDetailedPassthroughUsage(t *testing.T) {
	help := slashHelpText()
	for _, want := range []string{
		"/run <任务名> [--参数 值] [--confirm]",
		"/runctl list",
		"/runctl show <任务名>",
		"/runctl save <任务名>",
		"/runctl status",
		"/runctl delete <任务名> --confirm",
		"/runctl audit [数量]",
		"/exec <程序> [参数...] --confirm",
		"-- --confirm",
		"stdout/stderr",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q:\n%s", want, help)
		}
	}
}

func TestRunctlHelpIncludesPassthroughOnly(t *testing.T) {
	help := passthroughHelpText()
	if !strings.Contains(help, "/runctl help") || !strings.Contains(help, "/runctl audit [数量]") || !strings.Contains(help, "/runctl preview <任务名>") || !strings.Contains(help, "布尔参数可简写为 --deep") || !strings.Contains(help, "/runctl export <任务名>") || !strings.Contains(help, "/runctl save <任务名>") || !strings.Contains(help, "--cmd '命令模板'") || !strings.Contains(help, "--params-json") || !strings.Contains(help, `D:\\workprj\\aicoder`) || !strings.Contains(help, "/runctl enable <任务名>") || !strings.Contains(help, "/runctl delete <任务名> --confirm") || !strings.Contains(help, "/runctl exec enable") || !strings.Contains(help, "/runctl status") || !strings.Contains(help, "耗时和 argv") || !strings.Contains(help, "<redacted>") || strings.Contains(help, "/new /reset") {
		t.Fatalf("unexpected passthrough help:\n%s", help)
	}
}

func TestSlashAndRunctlHelpEnglish(t *testing.T) {
	help := slashHelpText("en")
	for _, want := range []string{"Available commands:", "/new /reset /clear - reset conversation", "Passthrough tasks:", "/runctl audit [limit]"} {
		if !strings.Contains(help, want) {
			t.Fatalf("English slash help missing %q:\n%s", want, help)
		}
	}
	runctl := passthroughHelpText("en")
	if !strings.Contains(runctl, "Passthrough tasks:") || strings.Contains(runctl, "直通任务") || strings.Contains(runctl, "/new /reset") {
		t.Fatalf("unexpected English runctl help:\n%s", runctl)
	}
}

func TestParsePassthroughAuditLimit(t *testing.T) {
	limit, err := parsePassthroughAuditLimit("/runctl audit", 10, 50)
	if err != nil || limit != 10 {
		t.Fatalf("limit=%d err=%v", limit, err)
	}
	limit, err = parsePassthroughAuditLimit("/runctl audit 20", 10, 50)
	if err != nil || limit != 20 {
		t.Fatalf("limit=%d err=%v", limit, err)
	}
	limit, err = parsePassthroughAuditLimit("/runctl audit 200", 10, 50)
	if err != nil || limit != 50 {
		t.Fatalf("limit=%d err=%v", limit, err)
	}
	if _, err := parsePassthroughAuditLimit("/runctl audit abc", 10, 50); err == nil {
		t.Fatal("expected invalid limit error")
	}
}

func TestFormatPassthroughStatus(t *testing.T) {
	got := formatPassthroughStatus("commands.json", []PassthroughCommand{
		{Name: "repair", Enabled: true, ConfirmRequired: true},
		{Name: "disabled", Enabled: false},
	}, PassthroughSettings{AllowExec: true}, 3)
	for _, want := range []string{"任务：2 个", "启用 1 个", "/exec：开启", "审计记录：3 条", "commands.json"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status missing %q:\n%s", want, got)
		}
	}
}

func TestPassthroughFormattersEnglish(t *testing.T) {
	commands := []PassthroughCommand{{Name: "repair", Enabled: true, ConfirmRequired: true}}
	status := formatPassthroughStatusWithLang("commands.json", commands, PassthroughSettings{AllowExec: true}, 3, "en")
	for _, want := range []string{"Passthrough task status", "Tasks: 1 total", "/exec: on", "Audit records: 3"} {
		if !strings.Contains(status, want) {
			t.Fatalf("English status missing %q:\n%s", want, status)
		}
	}
	list := formatPassthroughCommandList(commands, "en")
	if !strings.Contains(list, "Registered passthrough tasks:") || !strings.Contains(list, "requires --confirm") {
		t.Fatalf("unexpected English list:\n%s", list)
	}
	show := formatPassthroughCommandShowWithLang(PassthroughCommand{Name: "repair", ScriptPath: "repair.ps1", Runtime: "powershell", TimeoutSeconds: 60, Enabled: true}, "en")
	if !strings.Contains(show, "Command: repair") || !strings.Contains(show, "Timeout limit: 60s") || !strings.Contains(show, "Run example:") || strings.Contains(show, "命令：") {
		t.Fatalf("unexpected English show:\n%s", show)
	}
	help := passthroughHelpText("en")
	if !strings.Contains(help, "timeout limit") {
		t.Fatalf("English help should describe timeout as a limit:\n%s", help)
	}
}

func TestFormatPassthroughAuditList(t *testing.T) {
	got := formatPassthroughAuditList([]PassthroughAuditEntry{{
		Kind:        "exec",
		CommandName: "git",
		Source:      "weixin:user1",
		Status:      "success",
		ExitCode:    0,
		DurationMs:  12,
		StartedAt:   "2026-05-09T10:11:12+08:00",
	}})
	for _, want := range []string{"exec git", "source=weixin:user1", "status=success", "exit=0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("audit list missing %q:\n%s", want, got)
		}
	}
}

func TestFormatPassthroughCommandShowIncludesTemplateAndRunExample(t *testing.T) {
	got := formatPassthroughCommandShow(PassthroughCommand{
		Name:            "git-status",
		Title:           "Git status",
		ScriptPath:      "git",
		Runtime:         "direct",
		TimeoutSeconds:  120,
		ConfirmRequired: true,
		Enabled:         true,
		TemplateArgs:    []string{"-C", "${target}", "status", "--message", "hello world", ""},
		Params:          []PassthroughParam{{Name: "target", Type: "path", Required: true, Example: `D:\workprj\aicoder`}},
	})
	for _, want := range []string{
		"git-status",
		"超时上限：120s",
		`-C ${target} status --message "hello world" ""`,
		"--target path",
		"/run git-status --target D:\\workprj\\aicoder --confirm",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("show missing %q:\n%s", want, got)
		}
	}
}

func TestPassthroughCommandRunExampleQuotesParamValues(t *testing.T) {
	got := passthroughCommandRunExample(PassthroughCommand{
		Name:            "repair-env",
		ConfirmRequired: true,
		Params: []PassthroughParam{
			{Name: "target", Type: "path", Example: `D:\ops dir\`},
			{Name: "message", Type: "text", Example: `Bob's laptop`},
			{Name: "literal", Type: "text", Example: `--force`},
		},
	})
	for _, want := range []string{`--target "D:\\ops dir\\"`, `--message "Bob's laptop"`, `--literal=--force`, `--confirm`} {
		if !strings.Contains(got, want) {
			t.Fatalf("run example missing %q:\n%s", want, got)
		}
	}
	name, values, confirmed, err := parsePassthroughRunText(got)
	if err != nil {
		t.Fatalf("parse run example failed: %v\n%s", err, got)
	}
	if name != "repair-env" || !confirmed || values["target"] != `D:\ops dir\` || values["message"] != `Bob's laptop` || values["literal"] != "--force" {
		t.Fatalf("unexpected parsed example: name=%s confirmed=%v values=%+v", name, confirmed, values)
	}
}

func TestParsePassthroughRunTextSupportsEqualsValues(t *testing.T) {
	name, values, confirmed, err := parsePassthroughRunText(`/run repair-env --message=--force --target="D:\work prj" --confirm`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if name != "repair-env" || !confirmed || values["message"] != "--force" || values["target"] != `D:\work prj` {
		t.Fatalf("name=%q confirmed=%v values=%#v", name, confirmed, values)
	}
}

func TestParsePassthroughPreviewText(t *testing.T) {
	name, values, err := parsePassthroughPreviewText(`/runctl preview git-status --target "D:\work prj"`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if name != "git-status" || values["target"] != `D:\work prj` {
		t.Fatalf("name=%q values=%#v", name, values)
	}
}

func TestParsePassthroughPreviewTextSupportsEqualsValues(t *testing.T) {
	name, values, err := parsePassthroughPreviewText(`/runctl preview repair-env --message=--force`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if name != "repair-env" || values["message"] != "--force" {
		t.Fatalf("name=%q values=%#v", name, values)
	}
}

func TestParsePassthroughPreviewTextSupportsBooleanFlagShorthand(t *testing.T) {
	name, values, err := parsePassthroughPreviewText(`/runctl preview repair-env --deep --target workspace`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if name != "repair-env" || values["deep"] != "true" || values["target"] != "workspace" {
		t.Fatalf("name=%q values=%#v", name, values)
	}
}

func TestParsePassthroughSaveText(t *testing.T) {
	cmd, confirmed, previewOnly, err := parsePassthroughSaveText(`/runctl save git-status --cmd "git -C ${target} status --short" --runtime direct --param "target:path:required::D:\workprj\aicoder" --timeout 30 --confirm`)
	if err != nil {
		t.Fatalf("parse save failed: %v", err)
	}
	if !confirmed || previewOnly || cmd.Name != "git-status" || cmd.ScriptPath != "git" || strings.Join(cmd.TemplateArgs, " ") != "-C ${target} status --short" {
		t.Fatalf("cmd=%+v confirmed=%v previewOnly=%v", cmd, confirmed, previewOnly)
	}
	if cmd.TimeoutSeconds != 30 || len(cmd.Params) != 1 || cmd.Params[0].Name != "target" || cmd.Params[0].Example != `D:\workprj\aicoder` {
		t.Fatalf("unexpected command: %+v", cmd)
	}
}

func TestParsePassthroughSaveTextSupportsInlineFlagValues(t *testing.T) {
	cmd, confirmed, previewOnly, err := parsePassthroughSaveText(`/runctl save flaggy --cmd=tool --title=--repair --desc=--safe --param=mode:text:required::--force --confirm`)
	if err != nil {
		t.Fatalf("parse save failed: %v", err)
	}
	if !confirmed || previewOnly || cmd.Name != "flaggy" || cmd.Title != "--repair" || cmd.Description != "--safe" {
		t.Fatalf("cmd=%+v confirmed=%v previewOnly=%v", cmd, confirmed, previewOnly)
	}
	if len(cmd.Params) != 1 || cmd.Params[0].Name != "mode" || cmd.Params[0].Example != "--force" {
		t.Fatalf("unexpected params: %+v", cmd.Params)
	}
}

func TestParsePassthroughSaveTextPreviewDoesNotRequireConfirm(t *testing.T) {
	cmd, confirmed, previewOnly, err := parsePassthroughSaveText(`/runctl save git-status --cmd "git status" --preview`)
	if err != nil {
		t.Fatalf("parse save preview failed: %v", err)
	}
	if confirmed || !previewOnly || cmd.Name != "git-status" || cmd.ScriptPath != "git" {
		t.Fatalf("cmd=%+v confirmed=%v previewOnly=%v", cmd, confirmed, previewOnly)
	}
}

func TestParsePassthroughSaveTextRequiresConfirmAndCommand(t *testing.T) {
	if _, _, _, err := parsePassthroughSaveText(`/runctl save git-status --cmd "git status"`); err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("expected confirm error, got %v", err)
	}
	if _, _, _, err := parsePassthroughSaveText(`/runctl save git-status --confirm`); err == nil || !strings.Contains(err.Error(), "missing --cmd") {
		t.Fatalf("expected missing command error, got %v", err)
	}
}

func TestPassthroughPreviewUsesExampleValues(t *testing.T) {
	cmd := PassthroughCommand{
		Name:           "git-status",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 120,
		Enabled:        true,
		TemplateArgs:   []string{"-C", "${target}", "status", "--short"},
		Params:         []PassthroughParam{{Name: "target", Type: "path", Required: true, Example: `D:\workprj\aicoder`}},
	}
	args, err := previewPassthroughProcessArgs(cmd, passthroughPreviewValues(cmd, nil))
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	got := formatPassthroughPreviewArgs(args)
	if !strings.Contains(got, `argv: git -C D:\workprj\aicoder status --short`) {
		t.Fatalf("preview=%q args=%#v", got, args)
	}
}

func TestPassthroughPreviewUsesSampleValuesForMissingRequiredParams(t *testing.T) {
	cmd := PassthroughCommand{
		Name:           "git-status",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 120,
		Enabled:        true,
		TemplateArgs:   []string{"-C", "${target}", "status", "--short"},
		Params:         []PassthroughParam{{Name: "target", Type: "path", Required: true}},
	}
	args, err := previewPassthroughProcessArgs(cmd, passthroughPreviewValues(cmd, nil))
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	got := formatPassthroughPreviewArgs(args)
	if !strings.Contains(got, `argv: git -C . status --short`) {
		t.Fatalf("preview=%q args=%#v", got, args)
	}
}

func TestParsePassthroughSetEnabledText(t *testing.T) {
	action, name, err := parsePassthroughSetEnabledText("/runctl enable repair-env")
	if err != nil {
		t.Fatalf("parse enable failed: %v", err)
	}
	if action != "enable" || name != "repair-env" {
		t.Fatalf("action=%q name=%q", action, name)
	}
	action, name, err = parsePassthroughSetEnabledText("/runctl disable git.status")
	if err != nil {
		t.Fatalf("parse disable failed: %v", err)
	}
	if action != "disable" || name != "git.status" {
		t.Fatalf("action=%q name=%q", action, name)
	}
	if _, _, err := parsePassthroughSetEnabledText("/runctl enable bad name"); err == nil {
		t.Fatal("expected invalid usage error")
	}
}

func TestParsePassthroughDeleteText(t *testing.T) {
	name, confirmed, err := parsePassthroughDeleteText("/runctl delete repair-env --confirm")
	if err != nil {
		t.Fatalf("parse delete failed: %v", err)
	}
	if name != "repair-env" || !confirmed {
		t.Fatalf("name=%q confirmed=%v", name, confirmed)
	}
	if _, _, err := parsePassthroughDeleteText("/runctl delete repair-env"); err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("expected confirm error, got %v", err)
	}
	if _, _, err := parsePassthroughDeleteText("/runctl delete bad name --confirm"); err == nil {
		t.Fatal("expected invalid usage error")
	}
}

func TestParsePassthroughExecSettingText(t *testing.T) {
	enabled, err := parsePassthroughExecSettingText("/runctl exec enable")
	if err != nil || !enabled {
		t.Fatalf("enable=%v err=%v", enabled, err)
	}
	enabled, err = parsePassthroughExecSettingText("/runctl exec off")
	if err != nil || enabled {
		t.Fatalf("enable=%v err=%v", enabled, err)
	}
	if _, err := parsePassthroughExecSettingText("/runctl exec maybe"); err == nil {
		t.Fatal("expected invalid exec setting error")
	}
}

func TestPassthroughRegistryRecordControlAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	if err := reg.recordControlAudit("runctl", "exec enable", "weixin:user1", passthroughRunStatusSuccess, 0, ""); err != nil {
		t.Fatalf("recordControlAudit failed: %v", err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "runctl" || audit[0].CommandName != "exec enable" || audit[0].Source != "weixin:user1" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestPassthroughRegistrySetEnabledWithAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	if _, err := reg.Upsert(PassthroughCommand{
		Name:           "repair-env",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 5,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := reg.SetEnabledWithAudit("repair-env", false, "weixin:user1"); err != nil {
		t.Fatalf("SetEnabledWithAudit failed: %v", err)
	}
	cmd, ok, err := reg.Get("repair-env")
	if err != nil || !ok {
		t.Fatalf("Get failed: ok=%v err=%v", ok, err)
	}
	if cmd.Enabled {
		t.Fatalf("expected disabled command: %+v", cmd)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "runctl" || audit[0].CommandName != "disable repair-env" || audit[0].Source != "weixin:user1" || audit[0].Status != "success" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestPassthroughRegistrySaveAndDeleteWithAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	if _, err := reg.UpsertWithAudit(PassthroughCommand{
		Name:           "repair-env",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 5,
		Enabled:        true,
	}, "desktop:monitor"); err != nil {
		t.Fatalf("UpsertWithAudit failed: %v", err)
	}
	if err := reg.DeleteWithAudit("repair-env", "assistant:tool"); err != nil {
		t.Fatalf("DeleteWithAudit failed: %v", err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 2 {
		t.Fatalf("audit=%+v", audit)
	}
	if audit[0].Kind != "registry" || audit[0].CommandName != "delete repair-env" || audit[0].Source != "assistant:tool" || audit[0].Status != "success" {
		t.Fatalf("delete audit=%+v", audit[0])
	}
	if audit[1].Kind != "registry" || audit[1].CommandName != "save repair-env" || audit[1].Source != "desktop:monitor" || audit[1].Status != "success" {
		t.Fatalf("save audit=%+v", audit[1])
	}
}

func TestPassthroughRegistryRunRejectedAttemptsRecordAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:            "repair-env",
		ScriptPath:      exe,
		Runtime:         "direct",
		TimeoutSeconds:  5,
		ConfirmRequired: true,
		Enabled:         true,
		TemplateArgs:    []string{"-C", "${target}", "status"},
		Params:          []PassthroughParam{{Name: "target", Type: "path", Required: true}},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if _, err := reg.RunWithSource(context.Background(), "missing-env", nil, true, "weixin:user1"); err == nil {
		t.Fatal("expected missing command error")
	}
	if _, err := reg.RunWithSource(context.Background(), "repair-env", nil, false, "telegram:42"); err == nil || !strings.Contains(err.Error(), "requires confirmation") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if _, err := reg.RunWithSource(context.Background(), "repair-env", map[string]string{"token": "super-secret-value"}, true, "lansenger:user2"); err == nil || !strings.Contains(err.Error(), "missing required parameter") {
		t.Fatalf("expected missing parameter error, got %v", err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 3 {
		t.Fatalf("audit=%+v", audit)
	}
	if audit[0].Kind != "run" || audit[0].CommandName != "repair-env" || audit[0].Source != "lansenger:user2" || !strings.Contains(audit[0].Error, "missing required parameter") {
		t.Fatalf("missing-param audit=%+v", audit[0])
	}
	if strings.Join(audit[0].Args, " ") != "--token <redacted>" {
		t.Fatalf("expected redacted args, got %#v", audit[0].Args)
	}
	if audit[1].Kind != "run" || audit[1].CommandName != "repair-env" || audit[1].Source != "telegram:42" || !strings.Contains(audit[1].Error, "requires confirmation") {
		t.Fatalf("confirm audit=%+v", audit[1])
	}
	if audit[2].Kind != "run" || audit[2].CommandName != "missing-env" || audit[2].Source != "weixin:user1" || !strings.Contains(audit[2].Error, "not found") {
		t.Fatalf("missing audit=%+v", audit[2])
	}
}

func TestFormatPassthroughAuditListShowsArgs(t *testing.T) {
	got := formatPassthroughAuditList([]PassthroughAuditEntry{{
		Kind:        "run",
		CommandName: "repair-env",
		Source:      "weixin:user1",
		Args:        []string{"--target", `D:\workprj\aicoder`, "--message", "hello world", "--empty", ""},
		Status:      "failed",
		ExitCode:    -1,
		StartedAt:   "2026-05-09T10:11:12+08:00",
		Error:       "missing required parameter --mode",
	}})
	for _, want := range []string{"run repair-env", `args=--target D:\workprj\aicoder --message "hello world" --empty ""`, "error=missing required parameter --mode"} {
		if !strings.Contains(got, want) {
			t.Fatalf("audit text missing %q:\n%s", want, got)
		}
	}
}

func TestPassthroughRegistryRunExecDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	if _, err := reg.RunExec(context.Background(), `/exec git status --confirm`); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "exec" || audit[0].CommandName != "disabled" || audit[0].Status != "failed" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestPassthroughRegistryRunExecRecordsRejectedAttempts(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	if _, err := reg.SaveSettings(PassthroughSettings{AllowExec: true}); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	if _, err := reg.RunExecWithSource(context.Background(), `/exec definitely-not-a-real-maclaw-command --api_key secret-value --confirm`, "telegram:42"); err == nil || !strings.Contains(err.Error(), "executable not found") {
		t.Fatalf("expected executable error, got %v", err)
	}
	if _, err := reg.RunExecWithSource(context.Background(), `/exec git status --token secret-value`, "weixin:user1"); err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 2 {
		t.Fatalf("audit=%+v", audit)
	}
	if audit[0].Kind != "exec" || audit[0].CommandName != "git" || audit[0].Source != "weixin:user1" || audit[0].Status != "failed" {
		t.Fatalf("confirm audit=%+v", audit[0])
	}
	if got := strings.Join(audit[0].Args, " "); strings.Contains(got, "secret-value") || !strings.Contains(got, "--token <redacted>") {
		t.Fatalf("confirm audit args not redacted: %#v", audit[0].Args)
	}
	if audit[1].Kind != "exec" || audit[1].CommandName != "definitely-not-a-real-maclaw-command" || audit[1].Source != "telegram:42" || audit[1].Status != "failed" {
		t.Fatalf("lookpath audit=%+v", audit[1])
	}
	if got := strings.Join(audit[1].Args, " "); strings.Contains(got, "secret-value") || !strings.Contains(got, "--api_key <redacted>") {
		t.Fatalf("lookpath audit args not redacted: %#v", audit[1].Args)
	}
}

func TestPassthroughRegistryRun(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	script := filepath.Join(dir, "echo")
	runtimeName := "bash"
	content := "#!/bin/sh\necho target=$2\n"
	if runtime.GOOS == "windows" {
		script += ".cmd"
		runtimeName = "cmd"
		content = "@echo off\necho target=%2\n"
	} else {
		script += ".sh"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Upsert(PassthroughCommand{
		Name:            "repair-env",
		ScriptPath:      script,
		Runtime:         runtimeName,
		TimeoutSeconds:  5,
		ConfirmRequired: true,
		Enabled:         true,
		Params:          []PassthroughParam{{Name: "target", Type: "text", Required: true}},
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if _, err := reg.Run(context.Background(), "repair-env", map[string]string{"target": "workspace"}, false); err == nil {
		t.Fatal("expected confirmation error")
	}
	result, err := reg.Run(context.Background(), "repair-env", map[string]string{"target": "workspace"}, true)
	if err != nil {
		t.Fatalf("run failed: %v result=%+v", err, result)
	}
	if result.Status != "success" || !strings.Contains(result.Output, "target=workspace") {
		t.Fatalf("result=%+v", result)
	}
}

func TestPassthroughRegistryRunWithTemplateArgs(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	script := filepath.Join(dir, "echo")
	runtimeName := "bash"
	content := "#!/bin/sh\necho target=$2 mode=$4\n"
	if runtime.GOOS == "windows" {
		script += ".cmd"
		runtimeName = "cmd"
		content = "@echo off\necho target=%2 mode=%4\n"
	} else {
		script += ".sh"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Upsert(PassthroughCommand{
		Name:           "template-env",
		ScriptPath:     script,
		Runtime:        runtimeName,
		TimeoutSeconds: 5,
		Enabled:        true,
		TemplateArgs:   []string{"--target", "${target}", "--mode", "deep"},
		Params:         []PassthroughParam{{Name: "target", Type: "text", Required: true}},
	})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	result, err := reg.Run(context.Background(), "template-env", map[string]string{"target": "workspace"}, true)
	if err != nil {
		t.Fatalf("run failed: %v result=%+v", err, result)
	}
	if !strings.Contains(result.Output, "target=workspace mode=deep") {
		t.Fatalf("result=%+v", result)
	}
}

func TestBuildPassthroughArgsPreservesExplicitWhitespaceAndEmptyValues(t *testing.T) {
	args, err := buildPassthroughArgs([]PassthroughParam{
		{Name: "message", Type: "text", Required: true},
		{Name: "empty", Type: "text"},
		{Name: "fallback", Type: "text", Default: "  default  "},
	}, map[string]string{"message": "  hello  ", "empty": ""})
	if err != nil {
		t.Fatalf("build args failed: %v", err)
	}
	got := strings.Join(args, "\x00")
	want := "--message\x00  hello  \x00--empty\x00\x00--fallback\x00  default  "
	if got != want {
		t.Fatalf("args=%#v want joined %q", args, want)
	}
}

func TestPassthroughPreviewValuesPreservesExplicitEmptyValue(t *testing.T) {
	cmd := PassthroughCommand{
		Params: []PassthroughParam{{Name: "message", Type: "text", Example: "sample"}},
	}
	values := passthroughPreviewValues(cmd, map[string]string{"message": ""})
	if value, ok := values["message"]; !ok || value != "" {
		t.Fatalf("values=%#v", values)
	}
}

func TestPassthroughRegistryRunAuditRedactsSensitiveArgs(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	script := filepath.Join(dir, "echo")
	runtimeName := "bash"
	content := "#!/bin/sh\necho token=$2\n"
	if runtime.GOOS == "windows" {
		script += ".cmd"
		runtimeName = "cmd"
		content = "@echo off\necho token=%2\n"
	} else {
		script += ".sh"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:           "secret-env",
		ScriptPath:     script,
		Runtime:        runtimeName,
		TimeoutSeconds: 5,
		Enabled:        true,
		Params:         []PassthroughParam{{Name: "token", Type: "text", Required: true}},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	result, err := reg.RunWithSource(context.Background(), "secret-env", map[string]string{"token": "secret-value"}, true, "weixin:user1")
	if err != nil {
		t.Fatalf("run failed: %v result=%+v", err, result)
	}
	if !strings.Contains(result.Output, "secret-value") {
		t.Fatalf("raw output should be preserved, got %+v", result)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 {
		t.Fatalf("audit=%+v", audit)
	}
	joined := strings.Join(audit[0].Args, " ")
	if strings.Contains(joined, "secret-value") || !strings.Contains(joined, "--token <redacted>") {
		t.Fatalf("expected redacted audit args, got %#v", audit[0].Args)
	}
}

func TestRedactPassthroughCLIArgsRedactsInlineSensitiveFlags(t *testing.T) {
	got := strings.Join(redactPassthroughCLIArgs([]string{"tool", "--api_key=secret", "--mode", "ok", "--password", "p@ss"}), " ")
	for _, want := range []string{"--api_key=<redacted>", "--password <redacted>", "--mode ok"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted args missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "p@ss") {
		t.Fatalf("sensitive values leaked: %s", got)
	}
}

func TestPreviewPassthroughProcessArgsDoesNotRequireScriptFile(t *testing.T) {
	got, err := previewPassthroughProcessArgs(PassthroughCommand{
		Name:           "draft",
		ScriptPath:     `D:\ops\repair.ps1`,
		Runtime:        "powershell",
		TimeoutSeconds: 5,
		Enabled:        true,
		TemplateArgs:   []string{"--target", "${target}"},
		Params:         []PassthroughParam{{Name: "target", Type: "path", Required: true}},
	}, map[string]string{"target": `D:\workprj\aicoder`})
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-File D:\\ops\\repair.ps1 --target D:\\workprj\\aicoder") {
		t.Fatalf("preview args=%#v", got)
	}
}

func TestRenderPassthroughTemplateArgsRejectsUndefinedParam(t *testing.T) {
	_, err := renderPassthroughTemplateArgs([]string{"${missing}"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "undefined parameter") {
		t.Fatalf("expected undefined parameter error, got %v", err)
	}
}

func TestValidatePassthroughCommandRejectsUndefinedTemplateParam(t *testing.T) {
	err := validatePassthroughCommand(&PassthroughCommand{
		Name:           "template-env",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 5,
		Enabled:        true,
		TemplateArgs:   []string{"-C", "${target}", "status"},
	})
	if err == nil || !strings.Contains(err.Error(), "undefined parameter") {
		t.Fatalf("expected undefined template parameter error, got %v", err)
	}
}

func TestValidatePassthroughCommandRejectsInvalidTemplateParam(t *testing.T) {
	err := validatePassthroughCommand(&PassthroughCommand{
		Name:           "template-env",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 5,
		Enabled:        true,
		TemplateArgs:   []string{"-C", "${bad name}", "status"},
		Params:         []PassthroughParam{{Name: "target", Type: "path"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid parameter placeholder") {
		t.Fatalf("expected invalid template parameter error, got %v", err)
	}
}

func TestValidatePassthroughCommandRejectsUnsupportedRuntime(t *testing.T) {
	err := validatePassthroughCommand(&PassthroughCommand{
		Name:           "runtime-env",
		ScriptPath:     "git",
		Runtime:        "zsh",
		TimeoutSeconds: 5,
		Enabled:        true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported runtime") {
		t.Fatalf("expected unsupported runtime error, got %v", err)
	}
}

func TestValidatePassthroughCommandRejectsInvalidParamDefaultsAndExamples(t *testing.T) {
	for _, tc := range []struct {
		name  string
		param PassthroughParam
		want  string
	}{
		{name: "bad-number-default", param: PassthroughParam{Name: "count", Type: "number", Default: "many"}, want: "invalid default"},
		{name: "bad-boolean-example", param: PassthroughParam{Name: "deep", Type: "boolean", Example: "maybe"}, want: "invalid example"},
		{name: "bad-path-default", param: PassthroughParam{Name: "target", Type: "path", Default: `D:\bad*path`}, want: "invalid default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePassthroughCommand(&PassthroughCommand{
				Name:           tc.name,
				ScriptPath:     "git",
				Runtime:        "direct",
				TimeoutSeconds: 5,
				Enabled:        true,
				Params:         []PassthroughParam{tc.param},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestPassthroughRegistryRunRecordsAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	script := filepath.Join(dir, "echo")
	runtimeName := "bash"
	content := "#!/bin/sh\necho ok\n"
	if runtime.GOOS == "windows" {
		script += ".cmd"
		runtimeName = "cmd"
		content = "@echo off\necho ok\n"
	} else {
		script += ".sh"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:           "audit-test",
		ScriptPath:     script,
		Runtime:        runtimeName,
		TimeoutSeconds: 5,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if _, err := reg.RunWithSource(context.Background(), "audit-test", nil, true, "weixin:user1"); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "run" || audit[0].Source != "weixin:user1" || audit[0].ExitCode != 0 {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestPassthroughRegistryRunFailureReturnsOutputAndRecordsAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	script := filepath.Join(dir, "fail")
	runtimeName := "bash"
	content := "#!/bin/sh\necho failed-output\nexit 7\n"
	if runtime.GOOS == "windows" {
		script += ".cmd"
		runtimeName = "cmd"
		content = "@echo off\necho failed-output\nexit /b 7\n"
	} else {
		script += ".sh"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:           "fail-test",
		ScriptPath:     script,
		Runtime:        runtimeName,
		TimeoutSeconds: 5,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	result, err := reg.RunWithSource(context.Background(), "fail-test", nil, true, "telegram:42")
	if err == nil {
		t.Fatal("expected run error")
	}
	if result.Status != "failed" || result.ExitCode != 7 || !strings.Contains(result.Output, "failed-output") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got := formatPassthroughRunResult(result); got != result.Output {
		t.Fatalf("formatted output=%q want %q", got, result.Output)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Source != "telegram:42" || audit[0].Status != "failed" || audit[0].ExitCode != 7 || audit[0].Error == "" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestFormatPassthroughRunResultReturnsRawOutput(t *testing.T) {
	result := PassthroughRunResult{
		CommandName: "repair-env",
		Status:      "failed",
		ExitCode:    2,
		Output:      "line 1\nline 2\n",
	}
	if got := formatPassthroughRunResult(result); got != result.Output {
		t.Fatalf("formatPassthroughRunResult()=%q, want raw output %q", got, result.Output)
	}
}

func TestTruncatePassthroughOutputPreservesWhitespaceWhenNotTruncated(t *testing.T) {
	output := "  line 1\nline 2\n\n"
	if got := truncatePassthroughOutput(output, 100); got != output {
		t.Fatalf("truncatePassthroughOutput()=%q, want raw output %q", got, output)
	}
}

func TestTruncatePassthroughOutputMarksTruncatedOutput(t *testing.T) {
	got := truncatePassthroughOutput("0123456789abcdefghijklmnopqrstuvwxyz", 35)
	if !strings.HasSuffix(got, "\n... output truncated ...") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if len(got) > 35 {
		t.Fatalf("truncated output length=%d, want <=35: %q", len(got), got)
	}
}

func TestTruncatePassthroughOutputKeepsValidUTF8(t *testing.T) {
	got := truncatePassthroughOutput("修复完成修复完成修复完成", 35)
	if !strings.HasSuffix(got, "\n... output truncated ...") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if !strings.Contains(got, "修") {
		t.Fatalf("expected valid UTF-8 prefix, got %q", got)
	}
}

func TestValidatePassthroughCommandRejectsDuplicateParams(t *testing.T) {
	err := validatePassthroughCommand(&PassthroughCommand{
		Name:           "repair-env",
		ScriptPath:     "repair.ps1",
		Runtime:        "powershell",
		TimeoutSeconds: 5,
		Enabled:        true,
		Params: []PassthroughParam{
			{Name: "target", Type: "path"},
			{Name: "target", Type: "text"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate param") {
		t.Fatalf("expected duplicate param error, got %v", err)
	}
}

func TestParsePassthroughSaveTextSupportsParamsJSON(t *testing.T) {
	cmd, confirmed, previewOnly, err := parsePassthroughSaveText(`/runctl save git-status --cmd "git -C ${target} status --short" --params-json '[{"name":"target","type":"path","required":true,"example":"D:\\workprj\\aicoder"}]' --confirm`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !confirmed || previewOnly {
		t.Fatalf("confirmed=%v previewOnly=%v", confirmed, previewOnly)
	}
	if cmd.Name != "git-status" || cmd.ScriptPath != "git" || len(cmd.Params) != 1 {
		t.Fatalf("unexpected command: %+v", cmd)
	}
	if param := cmd.Params[0]; param.Name != "target" || param.Type != "path" || !param.Required || param.Example != `D:\workprj\aicoder` {
		t.Fatalf("unexpected param: %+v", param)
	}
}

func TestParsePassthroughSaveTextRejectsMixedParamFormats(t *testing.T) {
	_, _, _, err := parsePassthroughSaveText(`/runctl save git-status --cmd "git status" --param "target:path:required" --params-json '[{"name":"target"}]' --confirm`)
	if err == nil || !strings.Contains(err.Error(), "either --param or --params-json") {
		t.Fatalf("expected mixed param format error, got %v", err)
	}
}

func TestPassthroughRunctlSaveExampleRoundTripsParamsJSON(t *testing.T) {
	original := PassthroughCommand{
		Name:            "git-status",
		ScriptPath:      "git",
		TemplateArgs:    []string{"-C", "${target}", "status", "--short"},
		Runtime:         "direct",
		TimeoutSeconds:  120,
		ConfirmRequired: true,
		Enabled:         true,
		Params: []PassthroughParam{
			{Name: "target", Type: "path", Required: true, Example: `D:\workprj\aicoder`},
		},
	}
	example := passthroughRunctlSaveExample(original)
	parsed, confirmed, previewOnly, err := parsePassthroughSaveText(example)
	if err != nil {
		t.Fatalf("parse generated example failed: %v\n%s", err, example)
	}
	if !confirmed || previewOnly {
		t.Fatalf("confirmed=%v previewOnly=%v", confirmed, previewOnly)
	}
	if parsed.Name != original.Name || parsed.ScriptPath != original.ScriptPath || strings.Join(parsed.TemplateArgs, " ") != strings.Join(original.TemplateArgs, " ") {
		t.Fatalf("parsed mismatch:\nexample=%s\nparsed=%+v", example, parsed)
	}
	if len(parsed.Params) != 1 || parsed.Params[0].Example != `D:\workprj\aicoder` || parsed.Params[0].Type != "path" {
		t.Fatalf("parsed params mismatch:\nexample=%s\nparams=%+v", example, parsed.Params)
	}
}

func TestPassthroughRunctlSaveExampleRoundTripsQuotedArgs(t *testing.T) {
	original := PassthroughCommand{
		Name:            "quoted-args",
		ScriptPath:      "tool",
		TemplateArgs:    []string{`--message`, `Bob's`, `say "hello"`, `C:\ops\repair.ps1`, `C:\ops\`, ``},
		Runtime:         "direct",
		TimeoutSeconds:  120,
		ConfirmRequired: true,
		Enabled:         true,
	}
	example := passthroughRunctlSaveExample(original)
	parsed, confirmed, previewOnly, err := parsePassthroughSaveText(example)
	if err != nil {
		t.Fatalf("parse generated example failed: %v\n%s", err, example)
	}
	if !confirmed || previewOnly {
		t.Fatalf("confirmed=%v previewOnly=%v", confirmed, previewOnly)
	}
	if parsed.ScriptPath != original.ScriptPath || strings.Join(parsed.TemplateArgs, "\x00") != strings.Join(original.TemplateArgs, "\x00") {
		t.Fatalf("parsed mismatch:\nexample=%s\nparsed=%+v", example, parsed)
	}
}

func TestQuotePassthroughSingleArgRoundTripsEmbeddedSingleQuote(t *testing.T) {
	fields, err := splitPassthroughFields("/runctl save quoted --cmd " + quotePassthroughSingleArg("tool --message Bob's") + " --confirm")
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if len(fields) != 6 || fields[4] != "tool --message Bob's" {
		t.Fatalf("unexpected fields: %#v", fields)
	}
}
