package guiapp

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
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": strings.TrimSpace(cmd.Name), "passthrough": true, "action": "save"}); err != nil {
		return PassthroughCommand{}, err
	}
	return a.ensurePassthroughRegistry().UpsertWithAudit(cmd, "desktop:monitor")
}

func (a *App) DeletePassthroughCommand(name string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": strings.TrimSpace(name), "passthrough": true, "action": "delete"}); err != nil {
		return err
	}
	return a.ensurePassthroughRegistry().DeleteWithAudit(strings.TrimSpace(name), "desktop:monitor")
}

func (a *App) SetPassthroughCommandEnabled(name string, enabled bool) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": strings.TrimSpace(name), "passthrough": true, "action": "set_enabled", "enabled": enabled}); err != nil {
		return err
	}
	return a.ensurePassthroughRegistry().SetEnabledWithAudit(strings.TrimSpace(name), enabled, "desktop:monitor")
}

func (a *App) GetPassthroughSettings() (PassthroughSettings, error) {
	return a.ensurePassthroughRegistry().GetSettings()
}

func (a *App) SavePassthroughSettings(settings PassthroughSettings) (PassthroughSettings, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "update passthrough settings", "passthrough": true, "action": "settings"}); err != nil {
		return PassthroughSettings{}, err
	}
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
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": strings.TrimSpace(name), "passthrough": true}); err != nil {
		return PassthroughRunResult{}, err
	}
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
		output := formatPassthroughRunResultWithLang(result, lang)
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
		output := formatPassthroughRunResultWithLang(result, lang)
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
	return pickUI(lang, "Passthrough command executor is not initialized.", "直通命令执行器未初始化。", "直通命令执行器未初始化。")
}

func localizedPassthroughRunUsageText(lang string) string {
	return pickUI(lang, "Usage: /run <name> [--param value] [--confirm]\nSee: /runctl list", "用法：/run <命令名> [--参数 值] [--confirm]\n查看：/runctl list", "用法：/run <命令名> [--参数 值] [--confirm]\n查看：/runctl list")
}

func localizedPassthroughExecUsageText(lang string) string {
	return pickUI(lang, "Usage: /exec <program> [args...] --confirm\nNote: /exec only runs programs found on PATH or absolute paths. It does not interpret pipes, redirects, &&, or other shell syntax. To pass a literal --confirm to the target program, use: /exec tool --confirm -- --confirm", "用法：/exec <程序> [参数...] --confirm\n说明：/exec 只运行系统 PATH 中可找到的程序或绝对路径，不解释管道、重定向、&& 等 shell 语法。如需把字面量 --confirm 传给目标程序，可使用：/exec tool --confirm -- --confirm", "用法：/exec <程序> [参数...] --confirm\n说明：/exec 只运行系统 PATH 中可找到的程序或绝对路径，不解释管道、重定向、&& 等 shell 语法。如需把字面量 --confirm 传给目标程序，可使用：/exec tool --confirm -- --confirm")
}

func localizedPassthroughNoTasksMessage(lang string) string {
	return pickUI(lang, "No passthrough tasks are registered yet. Create one in Monitor > Passthrough Tasks, or ask Maclaw to create it for you.", "还没有注册直通任务。请在监控 > 直通任务中新建，或让 Maclaw 帮你创建。", "还没有注册直通任务。请在监控 > 直通任务中新建，或让 Maclaw 帮你创建。")
}

func formatPassthroughCommandList(commands []PassthroughCommand, lang string) string {
	var b strings.Builder
	b.WriteString(pickUI(lang, "Registered passthrough tasks:", "已注册直通任务：", "已注册直通任务："))
	enabledLabel := pickUI(lang, "enabled", "启用", "启用")
	disabledLabel := pickUI(lang, "disabled", "禁用", "禁用")
	confirmLabel := pickUI(lang, ", requires --confirm", "，需 --confirm", "，需 --confirm")
	lineFmt := pickUI(lang, "\n- %s: %s%s", "\n- %s：%s%s", "\n- %s：%s%s")
	for _, cmd := range commands {
		status := enabledLabel
		if !cmd.Enabled {
			status = disabledLabel
		}
		confirm := ""
		if cmd.ConfirmRequired {
			confirm = confirmLabel
		}
		fmt.Fprintf(&b, lineFmt, cmd.Name, status, confirm)
	}
	return b.String()
}

func localizedPassthroughSavePreviewMessage(lang, name, previewText string) string {
	return fmt.Sprintf(pickUI(lang, "Preview only; passthrough task %s was not saved.\n%s", "仅预览，未保存直通任务 %s。\n%s", "仅预览，未保存直通任务 %s。\n%s"), name, previewText)
}

func localizedPassthroughSavedMessage(lang string, saved PassthroughCommand, previewText string) string {
	return fmt.Sprintf(pickUI(lang, "Saved passthrough task %s.\n%s\nRun example: %s\nRemote registration command: %s\nPreview: /runctl preview %s\nShow: /runctl show %s", "已保存直通任务 %s。\n%s\n运行示例：%s\n远程注册命令：%s\n预览：/runctl preview %s\n查询：/runctl show %s", "已保存直通任务 %s。\n%s\n运行示例：%s\n远程注册命令：%s\n预览：/runctl preview %s\n查询：/runctl show %s"), saved.Name, previewText, passthroughCommandRunExample(saved), passthroughRunctlSaveExample(saved), saved.Name, saved.Name)
}

func localizedPassthroughExecStateMessage(lang string, enabled bool) string {
	if enabled {
		return pickUI(lang, "Enabled one-off /exec system commands.", "已开启 /exec 一次性系统命令。", "已开启 /exec 一次性系统命令。")
	}
	return pickUI(lang, "Disabled one-off /exec system commands.", "已关闭 /exec 一次性系统命令。", "已关闭 /exec 一次性系统命令。")
}

func localizedPassthroughTaskEnabledMessage(lang string, enabled bool, name string) string {
	if enabled {
		return fmt.Sprintf(pickUI(lang, "Enabled passthrough task %s.", "已启用直通任务 %s。", "已启用直通任务 %s。"), name)
	}
	return fmt.Sprintf(pickUI(lang, "Disabled passthrough task %s.", "已禁用直通任务 %s。", "已禁用直通任务 %s。"), name)
}

func localizedPassthroughTaskDeletedMessage(lang, name string) string {
	return fmt.Sprintf(pickUI(lang, "Deleted passthrough task %s.", "已删除直通任务 %s。", "已删除直通任务 %s。"), name)
}

func localizedPassthroughTaskNotFoundMessage(lang, name string) string {
	return fmt.Sprintf(pickUI(lang, "Passthrough task %q does not exist.", "直通任务 %q 不存在。", "直通任务 %q 不存在。"), name)
}

func localizedPassthroughSupportedCommandsText(lang string) string {
	return pickUI(lang, "Supported: /run <name> [--param value] [--confirm], /exec <program> [args...] --confirm, /runctl list, /runctl status, /runctl show <name>, /runctl export <name>, /runctl save <name> --cmd \"command template\" [--param ...|--params-json ...] --confirm, /runctl preview <name> [--param value], /runctl enable <name>, /runctl disable <name>, /runctl delete <name> --confirm, /runctl exec enable, /runctl exec disable, /runctl audit [limit]", "支持：/run <命令名> [--参数 值] [--confirm]、/exec <程序> [参数...] --confirm、/runctl list、/runctl status、/runctl show <命令名>、/runctl export <命令名>、/runctl save <命令名> --cmd \"命令模板\" [--param ...|--params-json ...] --confirm、/runctl preview <命令名> [--参数 值]、/runctl enable <命令名>、/runctl disable <命令名>、/runctl delete <命令名> --confirm、/runctl exec enable、/runctl exec disable、/runctl audit [数量]", "支持：/run <命令名> [--参数 值] [--confirm]、/exec <程序> [参数...] --confirm、/runctl list、/runctl status、/runctl show <命令名>、/runctl export <命令名>、/runctl save <命令名> --cmd \"命令模板\" [--param ...|--params-json ...] --confirm、/runctl preview <命令名> [--参数 值]、/runctl enable <命令名>、/runctl disable <命令名>、/runctl delete <命令名> --confirm、/runctl exec enable、/runctl exec disable、/runctl audit [数量]")
}
