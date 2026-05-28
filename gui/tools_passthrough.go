package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (h *IMMessageHandler) toolPassthroughTask(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return "passthrough registry is not available"
	}
	actionText := strings.ToLower(strings.TrimSpace(stringVal(args, "action")))
	action := normalizePassthroughToolAction(actionText)
	registry := h.app.ensurePassthroughRegistry()
	switch action {
	case passthroughToolActionList:
		commands, err := registry.List()
		if err != nil {
			return fmt.Sprintf("list passthrough tasks failed: %v", err)
		}
		if len(commands) == 0 {
			return "还没有注册直通任务。可以先创建脚本，再用 action=save 注册。"
		}
		out, _ := json.MarshalIndent(commands, "", "  ")
		return string(out)
	case passthroughToolActionStatus:
		commands, err := registry.List()
		if err != nil {
			return fmt.Sprintf("load passthrough tasks failed: %v", err)
		}
		settings, err := registry.GetSettings()
		if err != nil {
			return fmt.Sprintf("load passthrough settings failed: %v", err)
		}
		audit, err := registry.ListAudit(0)
		if err != nil {
			return fmt.Sprintf("load passthrough audit failed: %v", err)
		}
		return formatPassthroughStatus(registry.Path(), commands, settings, len(audit))
	case passthroughToolActionShow:
		name := strings.TrimSpace(stringVal(args, "name"))
		if name == "" {
			return "missing name"
		}
		cmd, ok, err := registry.Get(name)
		if err != nil {
			return fmt.Sprintf("show passthrough task failed: %v", err)
		}
		if !ok {
			return fmt.Sprintf("passthrough task %q not found", name)
		}
		out, _ := json.MarshalIndent(cmd, "", "  ")
		return string(out)
	case passthroughToolActionExport:
		name := strings.TrimSpace(stringVal(args, "name"))
		if name == "" {
			return "missing name"
		}
		cmd, ok, err := registry.Get(name)
		if err != nil {
			return fmt.Sprintf("export passthrough task failed: %v", err)
		}
		if !ok {
			return fmt.Sprintf("passthrough task %q not found", name)
		}
		return passthroughRunctlSaveExample(cmd)
	case passthroughToolActionPreview:
		cmd, err := passthroughPreviewCommandFromToolArgs(registry, args)
		if err != nil {
			return err.Error()
		}
		values := passthroughStringMapArg(args, "values")
		argv, err := previewPassthroughProcessArgs(cmd, passthroughPreviewValues(cmd, values))
		if err != nil {
			return fmt.Sprintf("preview passthrough task failed: %v", err)
		}
		return formatPassthroughPreviewArgs(argv)
	case passthroughToolActionSave:
		cmd, err := passthroughCommandFromToolArgs(args)
		if err != nil {
			return err.Error()
		}
		saved, err := registry.UpsertWithAudit(cmd, "assistant:tool")
		if err != nil {
			return fmt.Sprintf("save passthrough task failed: %v", err)
		}
		return fmt.Sprintf("已保存直通任务 %s。\n运行示例：%s\n远程注册命令：%s\n查询：/runctl show %s\n最近记录：/runctl audit", saved.Name, passthroughCommandRunExample(saved), passthroughRunctlSaveExample(saved), saved.Name)
	case passthroughToolActionDelete:
		name := strings.TrimSpace(stringVal(args, "name"))
		if name == "" {
			return "missing name"
		}
		if err := registry.DeleteWithAudit(name, "assistant:tool"); err != nil {
			return fmt.Sprintf("delete passthrough task failed: %v", err)
		}
		return fmt.Sprintf("已删除直通任务 %s。", name)
	case passthroughToolActionSetEnabled:
		name := strings.TrimSpace(stringVal(args, "name"))
		if name == "" {
			return "missing name"
		}
		enabled := passthroughBoolArg(args, "enabled", normalizePassthroughControlAction(actionText) != passthroughControlActionDisable)
		if err := registry.SetEnabledWithAudit(name, enabled, "assistant:tool"); err != nil {
			return fmt.Sprintf("update passthrough task failed: %v", err)
		}
		state := "启用"
		if !enabled {
			state = "禁用"
		}
		return fmt.Sprintf("已%s直通任务 %s。", state, name)
	case passthroughToolActionAudit:
		limit := passthroughIntArg(args, "limit", 10)
		if limit <= 0 {
			limit = 10
		}
		if limit > 50 {
			limit = 50
		}
		entries, err := registry.ListAudit(limit)
		if err != nil {
			return fmt.Sprintf("list passthrough audit failed: %v", err)
		}
		return formatPassthroughAuditList(entries)
	default:
		return "unsupported action. Use list, status, show, export, preview, save, delete, set_enabled, or audit."
	}
}

func passthroughPreviewCommandFromToolArgs(registry *PassthroughRegistry, args map[string]interface{}) (PassthroughCommand, error) {
	if commandLine := strings.TrimSpace(stringVal(args, "command")); commandLine != "" {
		return passthroughCommandFromToolArgs(args)
	}
	if strings.TrimSpace(stringVal(args, "script_path")) != "" {
		return passthroughCommandFromToolArgs(args)
	}
	name := strings.TrimSpace(stringVal(args, "name"))
	if name == "" {
		return PassthroughCommand{}, fmt.Errorf("missing name or command")
	}
	cmd, ok, err := registry.Get(name)
	if err != nil {
		return PassthroughCommand{}, fmt.Errorf("load passthrough task failed: %w", err)
	}
	if !ok {
		return PassthroughCommand{}, fmt.Errorf("passthrough task %q not found", name)
	}
	return cmd, nil
}

func passthroughCommandFromToolArgs(args map[string]interface{}) (PassthroughCommand, error) {
	name := strings.TrimSpace(stringVal(args, "name"))
	scriptPath := strings.TrimSpace(stringVal(args, "script_path"))
	templateArgs := passthroughStringSliceArg(args, "template_args")
	if commandLine := strings.TrimSpace(stringVal(args, "command")); commandLine != "" {
		fields, err := splitPassthroughFields(commandLine)
		if err != nil {
			return PassthroughCommand{}, fmt.Errorf("invalid command template: %w", err)
		}
		if len(fields) == 0 {
			return PassthroughCommand{}, fmt.Errorf("command template is empty")
		}
		scriptPath = fields[0]
		templateArgs = fields[1:]
	}
	if name == "" {
		return PassthroughCommand{}, fmt.Errorf("missing name")
	}
	if scriptPath == "" {
		return PassthroughCommand{}, fmt.Errorf("missing script_path or command")
	}
	timeout := passthroughIntArg(args, "timeout_seconds", defaultPassthroughTimeoutSeconds)
	cmd := PassthroughCommand{
		Name:            name,
		Title:           strings.TrimSpace(stringVal(args, "title")),
		Description:     strings.TrimSpace(stringVal(args, "description")),
		ScriptPath:      scriptPath,
		TemplateArgs:    templateArgs,
		Runtime:         strings.TrimSpace(stringVal(args, "runtime")),
		Cwd:             strings.TrimSpace(stringVal(args, "cwd")),
		TimeoutSeconds:  timeout,
		ConfirmRequired: passthroughBoolArg(args, "confirm_required", true),
		Enabled:         passthroughBoolArg(args, "enabled", true),
	}
	params, err := passthroughParamsFromToolArgs(args)
	if err != nil {
		return PassthroughCommand{}, err
	}
	cmd.Params = params
	return cmd, nil
}

func passthroughParamsFromToolArgs(args map[string]interface{}) ([]PassthroughParam, error) {
	paramsText := strings.TrimSpace(stringVal(args, "params_text"))
	paramsJSON := strings.TrimSpace(stringVal(args, "params_json"))
	if paramsJSON == "" {
		paramsJSON = strings.TrimSpace(stringVal(args, "param_json"))
	}
	_, hasParams := args["params"]
	formatCount := 0
	if paramsText != "" {
		formatCount++
	}
	if paramsJSON != "" {
		formatCount++
	}
	if hasParams && args["params"] != nil {
		formatCount++
	}
	if formatCount > 1 {
		return nil, fmt.Errorf("use only one of params, params_text, or params_json")
	}
	if paramsText != "" {
		return parsePassthroughParamsText(paramsText)
	}
	if paramsJSON != "" {
		return parsePassthroughParamsJSON(paramsJSON)
	}
	if !hasParams || args["params"] == nil {
		return nil, nil
	}
	raw := args["params"]
	if rawString, ok := raw.(string); ok {
		return parsePassthroughParamsJSON(rawString)
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("params must be an array")
	}
	params := make([]PassthroughParam, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("each params item must be an object")
		}
		name := passthroughMapString(m, "name")
		if name == "" {
			return nil, fmt.Errorf("param name is required")
		}
		params = append(params, PassthroughParam{
			Name:     name,
			Type:     passthroughMapString(m, "type"),
			Required: passthroughBoolAny(m["required"], true),
			Default:  passthroughMapString(m, "default"),
			Example:  passthroughMapString(m, "example"),
		})
	}
	return params, nil
}

func passthroughBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	return passthroughBoolAny(args[key], fallback)
}

func passthroughBoolAny(value interface{}, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		if value, ok := coerceToolBoolToken(v); ok {
			return value
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return fallback
}

func passthroughIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		var out int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &out); err == nil {
			return out
		}
	}
	return fallback
}

func passthroughMapString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func passthroughStringSliceArg(args map[string]interface{}, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	case string:
		fields, err := splitPassthroughFields(v)
		if err == nil {
			return fields
		}
		return []string{strings.TrimSpace(v)}
	default:
		return []string{strings.TrimSpace(fmt.Sprint(v))}
	}
}

func passthroughStringMapArg(args map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	if args == nil {
		return out
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return out
	}
	switch v := raw.(type) {
	case map[string]string:
		for key, value := range v {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	case map[string]interface{}:
		for key, value := range v {
			out[strings.TrimSpace(key)] = strings.TrimSpace(fmt.Sprint(value))
		}
	case string:
		var parsed map[string]string
		if err := json.Unmarshal([]byte(strings.TrimSpace(v)), &parsed); err == nil {
			for key, value := range parsed {
				out[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
	return out
}
