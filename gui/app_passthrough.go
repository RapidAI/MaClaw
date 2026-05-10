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
		_ = reg.recordControlAudit("settings", "exec "+string(action), "desktop:monitor", string(passthroughRunStatusSuccess), 0, "")
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
		return &IMAgentResponse{Error: "直通命令执行器未初始化。"}
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "/help" {
		return &IMAgentResponse{Text: slashHelpText()}
	}
	if trimmed == "/run" {
		return &IMAgentResponse{Text: "用法：/run <命令名> [--参数 值] [--confirm]\n查看：/runctl list"}
	}
	if trimmed == "/runctl help" {
		return &IMAgentResponse{Text: passthroughHelpText()}
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
		return &IMAgentResponse{Text: "用法：/exec <程序> [参数...] --confirm\n说明：/exec 只运行系统 PATH 中可找到的程序或绝对路径，不解释管道、重定向、&& 等 shell 语法。如需把字面量 --confirm 传给目标程序，可使用：/exec tool --confirm -- --confirm"}
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
			return &IMAgentResponse{Text: "还没有注册直通任务。请在监控 > 直通任务中新建，或让 Maclaw 帮你创建。"}
		}
		var b strings.Builder
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
		return &IMAgentResponse{Text: b.String()}
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
		return &IMAgentResponse{Text: formatPassthroughStatus(a.ensurePassthroughRegistry().Path(), commands, settings, len(audit))}
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
			return &IMAgentResponse{Text: fmt.Sprintf("仅预览，未保存直通任务 %s。\n%s", cmd.Name, previewText)}
		}
		saved, err := a.ensurePassthroughRegistry().UpsertWithAudit(cmd, source)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: fmt.Sprintf("已保存直通任务 %s。\n%s\n运行示例：%s\n远程注册命令：%s\n预览：/runctl preview %s\n查询：/runctl show %s", saved.Name, previewText, passthroughCommandRunExample(saved), passthroughRunctlSaveExample(saved), saved.Name, saved.Name)}
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
		state := "已关闭"
		if enabled {
			state = "已开启"
		}
		_ = reg.recordControlAudit("runctl", "exec "+string(action), source, string(passthroughRunStatusSuccess), 0, "")
		return &IMAgentResponse{Text: fmt.Sprintf("%s /exec 一次性系统命令。", state)}
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
		state := "已启用"
		if !enabled {
			state = "已禁用"
		}
		return &IMAgentResponse{Text: fmt.Sprintf("%s直通任务 %s。", state, name)}
	}
	if strings.HasPrefix(trimmed, "/runctl delete ") {
		name, _, err := parsePassthroughDeleteText(trimmed)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		if err := a.ensurePassthroughRegistry().DeleteWithAudit(name, source); err != nil {
			return &IMAgentResponse{Error: err.Error()}
		}
		return &IMAgentResponse{Text: fmt.Sprintf("已删除直通任务 %s。", name)}
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
		return &IMAgentResponse{Text: formatPassthroughAuditList(entries)}
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
			return &IMAgentResponse{Error: fmt.Sprintf("直通任务 %q 不存在。", name)}
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
			return &IMAgentResponse{Error: fmt.Sprintf("直通任务 %q 不存在。", name)}
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
			return &IMAgentResponse{Error: fmt.Sprintf("直通任务 %q 不存在。", name)}
		}
		return &IMAgentResponse{Text: formatPassthroughCommandShow(cmd)}
	}
	return &IMAgentResponse{Text: "支持：/run <命令名> [--参数 值] [--confirm]、/exec <程序> [参数...] --confirm、/runctl list、/runctl status、/runctl show <命令名>、/runctl export <命令名>、/runctl save <命令名> --cmd \"命令模板\" [--param ...|--params-json ...] --confirm、/runctl preview <命令名> [--参数 值]、/runctl enable <命令名>、/runctl disable <命令名>、/runctl delete <命令名> --confirm、/runctl exec enable、/runctl exec disable、/runctl audit [数量]"}
}
