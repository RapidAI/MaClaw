package main

import (
	"context"
	"fmt"
	"strings"
)

func (a *App) ensurePassthroughRegistry() *PassthroughRegistry {
	if a.passthroughRegistry == nil {
		a.passthroughRegistry = newPassthroughRegistry("")
	}
	return a.passthroughRegistry
}

func (a *App) ListPassthroughCommands() ([]PassthroughCommand, error) {
	return a.ensurePassthroughRegistry().List()
}

func (a *App) GetPassthroughCommand(name string) (PassthroughCommand, error) {
	cmd, ok, err := a.ensurePassthroughRegistry().Get(strings.TrimSpace(name))
	if err != nil {
		return PassthroughCommand{}, err
	}
	if !ok {
		return PassthroughCommand{}, fmt.Errorf("passthrough command %q not found", name)
	}
	return cmd, nil
}

func (a *App) SavePassthroughCommand(cmd PassthroughCommand) (PassthroughCommand, error) {
	return a.ensurePassthroughRegistry().UpsertWithAudit(cmd, "desktop:monitor")
}

func (a *App) DeletePassthroughCommand(name string) error {
	return a.ensurePassthroughRegistry().DeleteWithAudit(strings.TrimSpace(name), "desktop:monitor")
}

func (a *App) SetPassthroughCommandEnabled(name string, enabled bool) error {
	return a.ensurePassthroughRegistry().SetEnabledWithAudit(strings.TrimSpace(name), enabled, "desktop:monitor")
}

func (a *App) GetPassthroughSettings() (PassthroughSettings, error) {
	return a.ensurePassthroughRegistry().GetSettings()
}

func (a *App) SavePassthroughSettings(settings PassthroughSettings) (PassthroughSettings, error) {
	reg := a.ensurePassthroughRegistry()
	previous, _ := reg.GetSettings()
	saved, err := reg.SaveSettings(settings)
	if err != nil {
		return PassthroughSettings{}, err
	}
	if previous.AllowExec != saved.AllowExec {
		action := passthroughControlActionForEnabled(saved.AllowExec)
		_ = reg.recordControlAudit("settings", "exec "+string(action), "desktop:monitor", passthroughRunStatusSuccess, 0, "")
	}
	return saved, nil
}

func (a *App) ListPassthroughAudit(limit int) ([]PassthroughAuditEntry, error) {
	return a.ensurePassthroughRegistry().ListAudit(limit)
}

func (a *App) RunPassthroughCommand(name string, values map[string]string, confirmed bool) (PassthroughRunResult, error) {
	result, err := a.ensurePassthroughRegistry().RunWithSource(context.Background(), strings.TrimSpace(name), values, confirmed, "desktop:monitor")
	if err != nil && result.CommandName != "" {
		return result, nil
	}
	return result, err
}

func (a *App) PreviewPassthroughCommand(name string, values map[string]string) ([]string, error) {
	cmd, ok, err := a.ensurePassthroughRegistry().Get(strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("passthrough command %q not found", name)
	}
	program, args, _, err := buildPassthroughProcess(cmd, values)
	if err != nil {
		return nil, err
	}
	return append([]string{program}, args...), nil
}

func (a *App) PreviewPassthroughDraftCommand(cmd PassthroughCommand, values map[string]string) ([]string, error) {
	return previewPassthroughProcessArgs(cmd, values)
}

func (a *App) ExportPassthroughCommand(name string) (string, error) {
	cmd, ok, err := a.ensurePassthroughRegistry().Get(strings.TrimSpace(name))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("passthrough command %q not found", name)
	}
	return passthroughRunctlSaveExample(cmd), nil
}

func (a *App) PassthroughRegistryPath() string {
	return a.ensurePassthroughRegistry().Path()
}

func isPassthroughSlashText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "/help" || trimmed == "/run" || strings.HasPrefix(trimmed, "/run ") || trimmed == "/exec" || strings.HasPrefix(trimmed, "/exec ") || trimmed == "/runctl" || strings.HasPrefix(trimmed, "/runctl ")
}

func (a *App) TryHandlePassthroughSlashCommand(text string) (*IMAgentResponse, bool) {
	return a.TryHandlePassthroughSlashCommandWithSource(text, "")
}

func (a *App) TryHandlePassthroughSlashCommandWithSource(text string, source string) (*IMAgentResponse, bool) {
	if !isPassthroughSlashText(text) {
		return nil, false
	}
	return a.handlePassthroughSlashCommand(text, source), true
}

func (a *App) handlePassthroughSlashCommand(text string, source string) *IMAgentResponse {
	if a == nil {
		return &IMAgentResponse{Error: localizedPassthroughExecutorMissingMessage("")}
	}
	lang := a.CurrentLanguage
	trimmed := strings.TrimSpace(text)
	if trimmed == "/help" {
		return &IMAgentResponse{Text: slashHelpText(lang)}
	}
	if trimmed == "/run" {
		return &IMAgentResponse{Text: localizedPassthroughRunUsageText(lang)}
	}
	if trimmed == "/runctl help" {
		return &IMAgentResponse{Text: passthroughHelpText(lang)}
	}
	if strings.HasPrefix(trimmed, "/run ") {
		name, values, confirmed, err := parsePassthroughRunText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		result, err := a.ensurePassthroughRegistry().RunWithSource(context.Background(), name, values, confirmed, source)
		output := formatPassthroughRunResult(result)
		if err != nil {
			if result.CommandName != "" && output != "" {
				return &IMAgentResponse{Text: output}
			}
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: output}
	}
	if trimmed == "/exec" {
		return &IMAgentResponse{Text: localizedPassthroughExecUsageText(lang)}
	}
	if strings.HasPrefix(trimmed, "/exec ") {
		result, err := a.ensurePassthroughRegistry().RunExecWithSource(context.Background(), trimmed, source)
		output := formatPassthroughRunResult(result)
		if err != nil {
			if result.CommandName != "" && output != "" {
				return &IMAgentResponse{Text: output}
			}
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: output}
	}
	if trimmed == "/runctl" || trimmed == "/runctl list" {
		commands, err := a.ensurePassthroughRegistry().List()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		if len(commands) == 0 {
			return &IMAgentResponse{Text: localizedPassthroughNoTasksMessage(lang)}
		}
		return &IMAgentResponse{Text: formatPassthroughCommandList(commands, lang)}
	}
	if trimmed == "/runctl status" || trimmed == "/runctl settings" {
		commands, err := a.ensurePassthroughRegistry().List()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		settings, err := a.ensurePassthroughRegistry().GetSettings()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		audit, err := a.ensurePassthroughRegistry().ListAudit(0)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: formatPassthroughStatusWithLang(a.ensurePassthroughRegistry().Path(), commands, settings, len(audit), lang)}
	}
	if strings.HasPrefix(trimmed, "/runctl save ") {
		cmd, _, previewOnly, err := parsePassthroughSaveText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		previewArgs, err := previewPassthroughProcessArgs(cmd, passthroughPreviewValues(cmd, nil))
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		previewText := formatPassthroughPreviewArgs(previewArgs)
		if previewOnly {
			return &IMAgentResponse{Text: localizedPassthroughSavePreviewMessage(lang, cmd.Name, previewText)}
		}
		saved, err := a.ensurePassthroughRegistry().UpsertWithAudit(cmd, source)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: localizedPassthroughSavedMessage(lang, saved, previewText)}
	}
	if strings.HasPrefix(trimmed, "/runctl exec ") {
		enabled, err := parsePassthroughExecSettingText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		reg := a.ensurePassthroughRegistry()
		settings, err := reg.GetSettings()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		settings.AllowExec = enabled
		if _, err := reg.SaveSettings(settings); err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		action := passthroughControlActionForEnabled(enabled)
		_ = reg.recordControlAudit("runctl", "exec "+string(action), source, passthroughRunStatusSuccess, 0, "")
		return &IMAgentResponse{Text: localizedPassthroughExecStateMessage(lang, enabled)}
	}
	if strings.HasPrefix(trimmed, "/runctl enable ") || strings.HasPrefix(trimmed, "/runctl disable ") {
		action, name, err := parsePassthroughSetEnabledText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		enabled := normalizePassthroughControlAction(action).Enabled()
		reg := a.ensurePassthroughRegistry()
		if err := reg.SetEnabledWithAudit(name, enabled, source); err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: localizedPassthroughTaskEnabledMessage(lang, enabled, name)}
	}
	if strings.HasPrefix(trimmed, "/runctl delete ") {
		name, _, err := parsePassthroughDeleteText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		if err := a.ensurePassthroughRegistry().DeleteWithAudit(name, source); err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: localizedPassthroughTaskDeletedMessage(lang, name)}
	}
	if trimmed == "/runctl audit" || strings.HasPrefix(trimmed, "/runctl audit ") {
		limit, err := parsePassthroughAuditLimit(trimmed, 10, 50)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		entries, err := a.ensurePassthroughRegistry().ListAudit(limit)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: formatPassthroughAuditListWithLang(entries, lang)}
	}
	if strings.HasPrefix(trimmed, "/runctl preview ") {
		name, values, err := parsePassthroughPreviewText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		cmd, ok, err := a.ensurePassthroughRegistry().Get(name)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		if !ok {
			return &IMAgentResponse{Error: localizedPassthroughTaskNotFoundMessage(lang, name)}
		}
		args, err := previewPassthroughProcessArgs(cmd, passthroughPreviewValues(cmd, values))
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: formatPassthroughPreviewArgs(args)}
	}
	if strings.HasPrefix(trimmed, "/runctl export ") {
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "/runctl export "))
		cmd, ok, err := a.ensurePassthroughRegistry().Get(name)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		if !ok {
			return &IMAgentResponse{Error: localizedPassthroughTaskNotFoundMessage(lang, name)}
		}
		return &IMAgentResponse{Text: passthroughRunctlSaveExample(cmd)}
	}
	if strings.HasPrefix(trimmed, "/runctl show ") {
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "/runctl show "))
		cmd, ok, err := a.ensurePassthroughRegistry().Get(name)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		if !ok {
			return &IMAgentResponse{Error: localizedPassthroughTaskNotFoundMessage(lang, name)}
		}
		return &IMAgentResponse{Text: formatPassthroughCommandShowWithLang(cmd, lang)}
	}
	return &IMAgentResponse{Text: localizedPassthroughSupportedCommandsText(lang)}
}

func localizedPassthroughExecutorMissingMessage(lang string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return "Passthrough command executor is not initialized."
	}
	return "直通命令执行器未初始化。"
}

func localizedPassthroughRunUsageText(lang string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return "Usage: /run <name> [--param value] [--confirm]\nSee: /runctl list"
	}
	return "用法：/run <命令名> [--参数 值] [--confirm]\n查看：/runctl list"
}

func localizedPassthroughExecUsageText(lang string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return "Usage: /exec <program> [args...] --confirm\nNote: /exec only runs programs found on PATH or absolute paths. It does not interpret pipes, redirects, &&, or other shell syntax. To pass a literal --confirm to the target program, use: /exec tool --confirm -- --confirm"
	}
	return "用法：/exec <程序> [参数...] --confirm\n说明：/exec 只运行系统 PATH 中可找到的程序或绝对路径，不解释管道、重定向、&& 等 shell 语法。如需把字面量 --confirm 传给目标程序，可使用：/exec tool --confirm -- --confirm"
}

func localizedPassthroughNoTasksMessage(lang string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return "No passthrough tasks are registered yet. Create one in Monitor > Passthrough Tasks, or ask Maclaw to create it for you."
	}
	return "还没有注册直通任务。请在监控 > 直通任务中新建，或让 Maclaw 帮你创建。"
}

func formatPassthroughCommandList(commands []PassthroughCommand, lang string) string {
	var b strings.Builder
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		b.WriteString("Registered passthrough tasks:")
		for _, cmd := range commands {
			status := "enabled"
			if !cmd.Enabled {
				status = "disabled"
			}
			confirm := ""
			if cmd.ConfirmRequired {
				confirm = ", requires --confirm"
			}
			fmt.Fprintf(&b, "\n- %s: %s%s", cmd.Name, status, confirm)
		}
		return b.String()
	}
	b.WriteString("已注册直通任务：")
	for _, cmd := range commands {
		status := "启用"
		if !cmd.Enabled {
			status = "禁用"
		}
		confirm := ""
		if cmd.ConfirmRequired {
			confirm = "，需 --confirm"
		}
		fmt.Fprintf(&b, "\n- %s：%s%s", cmd.Name, status, confirm)
	}
	return b.String()
}

func localizedPassthroughSavePreviewMessage(lang, name, previewText string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return fmt.Sprintf("Preview only; passthrough task %s was not saved.\n%s", name, previewText)
	}
	return fmt.Sprintf("仅预览，未保存直通任务 %s。\n%s", name, previewText)
}

func localizedPassthroughSavedMessage(lang string, saved PassthroughCommand, previewText string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return fmt.Sprintf("Saved passthrough task %s.\n%s\nRun example: %s\nRemote registration command: %s\nPreview: /runctl preview %s\nShow: /runctl show %s", saved.Name, previewText, passthroughCommandRunExample(saved), passthroughRunctlSaveExample(saved), saved.Name, saved.Name)
	}
	return fmt.Sprintf("已保存直通任务 %s。\n%s\n运行示例：%s\n远程注册命令：%s\n预览：/runctl preview %s\n查询：/runctl show %s", saved.Name, previewText, passthroughCommandRunExample(saved), passthroughRunctlSaveExample(saved), saved.Name, saved.Name)
}

func localizedPassthroughExecStateMessage(lang string, enabled bool) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		if enabled {
			return "Enabled one-off /exec system commands."
		}
		return "Disabled one-off /exec system commands."
	}
	state := "已关闭"
	if enabled {
		state = "已开启"
	}
	return fmt.Sprintf("%s /exec 一次性系统命令。", state)
}

func localizedPassthroughTaskEnabledMessage(lang string, enabled bool, name string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		state := "Enabled"
		if !enabled {
			state = "Disabled"
		}
		return fmt.Sprintf("%s passthrough task %s.", state, name)
	}
	state := "已启用"
	if !enabled {
		state = "已禁用"
	}
	return fmt.Sprintf("%s直通任务 %s。", state, name)
}

func localizedPassthroughTaskDeletedMessage(lang, name string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return fmt.Sprintf("Deleted passthrough task %s.", name)
	}
	return fmt.Sprintf("已删除直通任务 %s。", name)
}

func localizedPassthroughTaskNotFoundMessage(lang, name string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return fmt.Sprintf("Passthrough task %q does not exist.", name)
	}
	return fmt.Sprintf("直通任务 %q 不存在。", name)
}

func localizedPassthroughSupportedCommandsText(lang string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return "Supported: /run <name> [--param value] [--confirm], /exec <program> [args...] --confirm, /runctl list, /runctl status, /runctl show <name>, /runctl export <name>, /runctl save <name> --cmd \"command template\" [--param ...|--params-json ...] --confirm, /runctl preview <name> [--param value], /runctl enable <name>, /runctl disable <name>, /runctl delete <name> --confirm, /runctl exec enable, /runctl exec disable, /runctl audit [limit]"
	}
	return "支持：/run <命令名> [--参数 值] [--confirm]、/exec <程序> [参数...] --confirm、/runctl list、/runctl status、/runctl show <命令名>、/runctl export <命令名>、/runctl save <命令名> --cmd \"命令模板\" [--param ...|--params-json ...] --confirm、/runctl preview <命令名> [--参数 值]、/runctl enable <命令名>、/runctl disable <命令名>、/runctl delete <命令名> --confirm、/runctl exec enable、/runctl exec disable、/runctl audit [数量]"
}
