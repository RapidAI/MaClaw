package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAppRunctlDeleteRemovesCommandAndAuditsSource(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:           "repair-env",
		ScriptPath:     "git",
		Runtime:        "direct",
		TimeoutSeconds: 5,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource("/runctl delete repair-env --confirm", "weixin:user1")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Deleted passthrough task repair-env") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if _, ok, err := reg.Get("repair-env"); err != nil || ok {
		t.Fatalf("expected command deleted: ok=%v err=%v", ok, err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "registry" || audit[0].CommandName != "delete repair-env" || audit[0].Source != "weixin:user1" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestAppRunctlDeleteRequiresConfirm(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource("/runctl delete repair-env", "telegram:42")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || !strings.Contains(resp.Error, "requires --confirm") {
		t.Fatalf("expected confirmation error, got %+v", resp)
	}
}

func TestAppRunctlSaveRegistersCommandAndAuditsSource(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource(`/runctl save git-status --cmd "git -C ${target} status --short" --param "target:path:required::D:\workprj\aicoder" --confirm`, "telegram:42")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Saved passthrough task git-status") || !strings.Contains(resp.Text, "argv: git -C D:\\workprj\\aicoder status --short") || !strings.Contains(resp.Text, "Remote registration command: /runctl save git-status") || !strings.Contains(resp.Text, "--params-json") || !strings.Contains(resp.Text, "/runctl preview git-status") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	cmd, ok, err := reg.Get("git-status")
	if err != nil || !ok {
		t.Fatalf("Get failed: ok=%v err=%v", ok, err)
	}
	if cmd.ScriptPath != "git" || strings.Join(cmd.TemplateArgs, " ") != "-C ${target} status --short" || len(cmd.Params) != 1 {
		t.Fatalf("unexpected command: %+v", cmd)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "registry" || audit[0].CommandName != "save git-status" || audit[0].Source != "telegram:42" {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestAppRunctlShowIncludesRemoteRegistrationCommand(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}
	if _, err := reg.Upsert(PassthroughCommand{
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
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource("/runctl show git-status", "weixin:user1")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Run example: /run git-status") || !strings.Contains(resp.Text, "Remote registration command: /runctl save git-status") || !strings.Contains(resp.Text, "--params-json") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAppPassthroughSlashCommandsUseEnglishLanguage(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg, CurrentLanguage: "en"}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource("/run", "desktop:test")
	if !handled || resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Usage: /run <name>") {
		t.Fatalf("unexpected /run response: handled=%v resp=%+v", handled, resp)
	}

	resp, handled = app.TryHandlePassthroughSlashCommandWithSource("/exec", "desktop:test")
	if !handled || resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Usage: /exec <program>") {
		t.Fatalf("unexpected /exec response: handled=%v resp=%+v", handled, resp)
	}

	resp, handled = app.TryHandlePassthroughSlashCommandWithSource("/runctl list", "desktop:test")
	if !handled || resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "No passthrough tasks are registered yet") {
		t.Fatalf("unexpected /runctl list response: handled=%v resp=%+v", handled, resp)
	}

	if _, err := reg.Upsert(PassthroughCommand{Name: "git-status", ScriptPath: "git", Runtime: "direct", TimeoutSeconds: 60, Enabled: true}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	resp, handled = app.TryHandlePassthroughSlashCommandWithSource("/runctl show git-status", "desktop:test")
	if !handled || resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Command: git-status") || strings.Contains(resp.Text, "命令：") {
		t.Fatalf("unexpected /runctl show response: handled=%v resp=%+v", handled, resp)
	}
}

func TestAppRunctlExportReturnsOnlyRemoteRegistrationCommand(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}
	if _, err := reg.Upsert(PassthroughCommand{
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
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource("/runctl export git-status", "telegram:42")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.HasPrefix(resp.Text, "/runctl save git-status") || !strings.Contains(resp.Text, "--params-json") || strings.Contains(resp.Text, "Run example") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAppRunctlSaveRegistersCommandWithParamsJSON(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource(`/runctl save git-status --cmd "git -C ${target} status --short" --params-json '[{"name":"target","type":"path","required":true,"example":"D:\\workprj\\aicoder"}]' --confirm`, "lansenger:user2")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Saved passthrough task git-status") || !strings.Contains(resp.Text, "argv: git -C D:\\workprj\\aicoder status --short") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	cmd, ok, err := reg.Get("git-status")
	if err != nil || !ok {
		t.Fatalf("Get failed: ok=%v err=%v", ok, err)
	}
	if len(cmd.Params) != 1 || cmd.Params[0].Name != "target" || cmd.Params[0].Type != "path" || cmd.Params[0].Example != `D:\workprj\aicoder` {
		t.Fatalf("unexpected params: %+v", cmd.Params)
	}
}

func TestAppExportPassthroughCommandReturnsRemoteRegistrationCommand(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}
	if _, err := reg.Upsert(PassthroughCommand{
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
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	got, err := app.ExportPassthroughCommand("git-status")
	if err != nil {
		t.Fatalf("ExportPassthroughCommand failed: %v", err)
	}
	if !strings.HasPrefix(got, "/runctl save git-status") || !strings.Contains(got, "--params-json") {
		t.Fatalf("unexpected export: %s", got)
	}
}

func TestAppRunPassthroughCommandAuditsDesktopMonitorSource(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo-args.cmd")
	if err := os.WriteFile(script, []byte("@echo off\r\necho ok %*\r\n"), 0o644); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:            "echo-args",
		ScriptPath:      script,
		Runtime:         "cmd",
		TimeoutSeconds:  10,
		ConfirmRequired: true,
		Enabled:         true,
		Params: []PassthroughParam{
			{Name: "target", Type: "text", Required: true},
		},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	result, err := app.RunPassthroughCommand("echo-args", map[string]string{"target": "sample"}, true)
	if err != nil {
		t.Fatalf("RunPassthroughCommand failed: %v result=%+v", err, result)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "run" || audit[0].Source != "desktop:monitor" || audit[0].CommandName != "echo-args" {
		t.Fatalf("unexpected audit: %+v", audit)
	}
}

func TestSendAIAssistantMessageHandlesPassthroughBeforeLLM(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	script := filepath.Join(dir, "echo-args")
	runtimeName := "bash"
	content := "#!/bin/sh\necho assistant=$2\n"
	if runtime.GOOS == "windows" {
		script += ".cmd"
		runtimeName = "cmd"
		content = "@echo off\r\necho assistant=%2\r\n"
	} else {
		script += ".sh"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write script failed: %v", err)
	}
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{ctx: context.Background(), passthroughRegistry: reg}
	if _, err := reg.Upsert(PassthroughCommand{
		Name:            "assistant-echo",
		ScriptPath:      script,
		Runtime:         runtimeName,
		TimeoutSeconds:  10,
		ConfirmRequired: true,
		Enabled:         true,
		Params: []PassthroughParam{
			{Name: "target", Type: "text", Required: true},
		},
	}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	resp, err := app.SendAIAssistantMessage(AIAssistantSendRequest{Text: "/run assistant-echo --target sample --confirm", RequestID: "req-passthrough"})
	if err != nil {
		t.Fatalf("SendAIAssistantMessage failed: %v", err)
	}
	expectedText := "assistant=sample\n"
	if runtime.GOOS == "windows" {
		expectedText = "assistant=sample\r\n"
	}
	if resp == nil || resp.Error != "" || resp.Text != expectedText || resp.RequestID != "req-passthrough" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "run" || audit[0].Source != "desktop:ai-assistant" || audit[0].CommandName != "assistant-echo" {
		t.Fatalf("unexpected audit: %+v", audit)
	}
}

func TestSendAIAssistantMessageHandlesExecBeforeLLM(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	if _, err := reg.SaveSettings(PassthroughSettings{AllowExec: true}); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}
	app := &App{ctx: context.Background(), passthroughRegistry: reg}

	resp, err := app.SendAIAssistantMessage(AIAssistantSendRequest{Text: "/exec definitely-not-a-real-maclaw-command --confirm", RequestID: "req-exec"})
	if err != nil {
		t.Fatalf("SendAIAssistantMessage failed: %v", err)
	}
	if resp == nil || !strings.Contains(resp.Error, "executable not found") || resp.RequestID != "req-exec" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 1 || audit[0].Kind != "exec" || audit[0].Source != "desktop:ai-assistant" || audit[0].Status != "failed" {
		t.Fatalf("unexpected audit: %+v", audit)
	}
}

func TestAppRunctlSavePreviewDoesNotPersistOrAudit(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource(`/runctl save git-status --cmd "git status --short" --preview`, "weixin:user1")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "Preview only; passthrough task git-status was not saved") || !strings.Contains(resp.Text, "argv: git status --short") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if _, ok, err := reg.Get("git-status"); err != nil || ok {
		t.Fatalf("preview should not persist command: ok=%v err=%v", ok, err)
	}
	audit, err := reg.ListAudit(10)
	if err != nil {
		t.Fatalf("ListAudit failed: %v", err)
	}
	if len(audit) != 0 {
		t.Fatalf("preview should not audit registry changes: %+v", audit)
	}
}

func TestAppRunctlSavePreviewAllowsMissingRequiredParamExample(t *testing.T) {
	dir := t.TempDir()
	reg := newPassthroughRegistry(filepath.Join(dir, "commands.json"))
	app := &App{passthroughRegistry: reg}

	resp, handled := app.TryHandlePassthroughSlashCommandWithSource(`/runctl save git-status --cmd "git -C ${target} status --short" --param "target:path:required" --preview`, "weixin:user1")
	if !handled {
		t.Fatal("expected passthrough command to be handled")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "argv: git -C . status --short") {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if _, ok, err := reg.Get("git-status"); err != nil || ok {
		t.Fatalf("preview should not persist command: ok=%v err=%v", ok, err)
	}
}
