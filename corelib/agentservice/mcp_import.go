package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type mcpJSONImporter interface {
	ImportMCPServers(ctx context.Context, p Principal, inputs []MCPServerCreateInput) ([]string, error)
}

func mcpJSONConfigArg(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	for _, key := range []string{"json_config", "config"} {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		case json.RawMessage:
			if s := strings.TrimSpace(string(v)); s != "" {
				return s
			}
		case map[string]interface{}, []interface{}, map[string]string:
			data, err := json.Marshal(v)
			if err == nil && len(data) > 0 {
				return string(data)
			}
		}
	}
	return ""
}

func stripMCPImportCodeFence(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	lines := strings.Split(raw, "\n")
	if len(lines) < 2 {
		return raw
	}
	end := len(lines)
	if strings.HasPrefix(strings.TrimSpace(lines[end-1]), "```") {
		end--
	}
	stripped := strings.TrimSpace(strings.Join(lines[1:end], "\n"))
	if stripped == "" {
		return raw
	}
	if json.Valid([]byte(stripped)) || json.Valid([]byte("{"+stripped+"}")) {
		return stripped
	}
	return raw
}

func normalizeMCPImportJSON(raw string) string {
	raw = stripMCPImportCodeFence(strings.TrimSpace(raw))
	if raw == "" || json.Valid([]byte(raw)) {
		return raw
	}
	if wrapped := "{" + raw + "}"; json.Valid([]byte(wrapped)) {
		return wrapped
	}
	return raw
}

func parseMCPServerCreateInputs(raw, target string) ([]MCPServerCreateInput, error) {
	raw = normalizeMCPImportJSON(raw)
	if raw == "" {
		return nil, fmt.Errorf("json_config is required")
	}
	kindHint := strings.ToLower(strings.TrimSpace(target))
	switch kindHint {
	case "", "auto", "local", "stdio", "remote", "http":
	default:
		return nil, fmt.Errorf("target must be auto, local, or remote")
	}
	if kindHint == "stdio" {
		kindHint = "local"
	}
	if kindHint == "http" {
		kindHint = "remote"
	}

	var top interface{}
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch typed := top.(type) {
	case []interface{}:
		return parseMCPServerCreateInputList(typed, kindHint)
	case map[string]interface{}:
		if list, ok := mcpServersList(typed); ok {
			return parseMCPServerCreateInputList(list, kindHint)
		}
		servers := mcpServersMap(typed)
		if servers == nil {
			in, err := mcpCreateInputFromObject("", typed, kindHint)
			if err != nil {
				return nil, err
			}
			return []MCPServerCreateInput{in}, nil
		}
		names := make([]string, 0, len(servers))
		for name := range servers {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]MCPServerCreateInput, 0, len(names))
		for _, name := range names {
			obj, ok := servers[name].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("MCP server %q is not an object", name)
			}
			in, err := mcpCreateInputFromObject(name, obj, kindHint)
			if err != nil {
				return nil, err
			}
			out = append(out, in)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("json_config contains no MCP servers")
		}
		return out, nil
	default:
		return nil, fmt.Errorf("invalid MCP JSON structure")
	}
}

func parseMCPServerCreateInputList(typed []interface{}, kindHint string) ([]MCPServerCreateInput, error) {
	out := make([]MCPServerCreateInput, 0, len(typed))
	for i, item := range typed {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("entry %d is not an object", i+1)
		}
		in, err := mcpCreateInputFromObject("", obj, kindHint)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		out = append(out, in)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("json_config contains no MCP servers")
	}
	return out, nil
}

func mcpServersList(parsed map[string]interface{}) ([]interface{}, bool) {
	for _, key := range []string{"mcpServers", "mcp_servers", "mcpservers", "servers"} {
		if nested, ok := parsed[key].([]interface{}); ok {
			return nested, true
		}
	}
	for key, nested := range parsed {
		normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
		if normalized == "mcpservers" || normalized == "servers" {
			if arr, ok := nested.([]interface{}); ok {
				return arr, true
			}
		}
	}
	return nil, false
}

func mcpServersMap(parsed map[string]interface{}) map[string]interface{} {
	for _, key := range []string{"mcpServers", "mcp_servers", "mcpservers", "servers"} {
		if nested, ok := parsed[key].(map[string]interface{}); ok {
			return nested
		}
	}
	for key, nested := range parsed {
		normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
		if normalized == "mcpservers" || normalized == "servers" {
			if obj, ok := nested.(map[string]interface{}); ok {
				return obj
			}
		}
	}
	if _, hasCommand := parsed["command"]; hasCommand {
		return nil
	}
	if _, hasCmd := parsed["cmd"]; hasCmd {
		return nil
	}
	if _, hasURL := parsed["url"]; hasURL {
		return nil
	}
	if _, hasEndpoint := parsed["endpoint_url"]; hasEndpoint {
		return nil
	}
	if _, hasServerURL := parsed["serverUrl"]; hasServerURL {
		return nil
	}
	if _, hasServerURL := parsed["server_url"]; hasServerURL {
		return nil
	}
	if _, hasName := parsed["name"]; hasName {
		return nil
	}
	return parsed
}

func mcpCreateInputFromObject(name string, obj map[string]interface{}, kindHint string) (MCPServerCreateInput, error) {
	display := firstNonEmpty(strings.TrimSpace(name), mcpJSONString(obj, "name"))
	if display == "" {
		return MCPServerCreateInput{}, fmt.Errorf("MCP server name is required")
	}
	command, args := mcpCommandAndArgs(obj)
	endpoint := firstNonEmpty(
		mcpJSONString(obj, "endpoint_url"),
		mcpJSONString(obj, "url"),
		mcpJSONString(obj, "serverUrl"),
		mcpJSONString(obj, "server_url"),
	)
	kind := kindHint
	if kind == "" || kind == "auto" {
		switch mcpTransportKind(obj) {
		case "remote":
			kind = "remote"
		case "local":
			kind = "local"
		default:
			if endpoint != "" && command == "" {
				kind = "remote"
			} else {
				kind = "local"
			}
		}
	}
	headers := mcpJSONHeaderMap(obj["headers"])
	authType := firstNonEmpty(mcpJSONString(obj, "auth_type"), mcpJSONString(obj, "authType"))
	authSecret := firstNonEmpty(
		mcpJSONString(obj, "auth_secret"),
		mcpJSONString(obj, "auth_token"),
		mcpJSONString(obj, "api_key"),
		mcpJSONString(obj, "apiKey"),
	)
	if authSecret == "" {
		if value, ok := mcpAuthorizationHeader(headers); ok {
			if strings.HasPrefix(strings.ToLower(value), "bearer ") {
				authType = "bearer"
				authSecret = strings.TrimSpace(value[7:])
			} else if authType == "" {
				authType = "api_key"
				authSecret = value
			}
			headers = mcpWithoutAuthorizationHeader(headers)
		}
	}
	in := MCPServerCreateInput{
		Kind:        kind,
		Name:        display,
		EndpointURL: endpoint,
		AuthType:    authType,
		AuthSecret:  authSecret,
		Headers:     headers,
		Command:     command,
		Args:        args,
		Env:         mcpJSONEnvMap(obj),
		Disabled:    mcpJSONBool(obj["disabled"]),
		AutoStart:   mcpJSONBool(obj["auto_start"]) || mcpJSONBool(obj["autoStart"]) || mcpJSONBool(obj["autostart"]),
	}
	if in.Kind == "remote" {
		if strings.TrimSpace(in.EndpointURL) == "" {
			return MCPServerCreateInput{}, fmt.Errorf("remote MCP server %q is missing url or endpoint_url", display)
		}
		if strings.TrimSpace(in.AuthType) == "" {
			if strings.TrimSpace(in.AuthSecret) != "" {
				in.AuthType = "api_key"
			} else {
				in.AuthType = "none"
			}
		}
	} else {
		if strings.TrimSpace(in.Command) == "" {
			return MCPServerCreateInput{}, fmt.Errorf("local MCP server %q is missing command", display)
		}
	}
	return in, nil
}

func mcpAuthorizationHeader(headers map[string]string) (string, bool) {
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), "authorization") && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func mcpWithoutAuthorizationHeader(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), "authorization") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mcpCommandAndArgs(obj map[string]interface{}) (string, []string) {
	if obj == nil {
		return "", nil
	}
	extra := mcpJSONStringSlice(obj["args"])
	if len(extra) == 0 {
		extra = mcpJSONStringSlice(obj["arguments"])
	}
	raw, ok := obj["command"]
	if !ok || raw == nil {
		raw = obj["cmd"]
	}
	switch raw.(type) {
	case []interface{}, []string:
		parts := mcpJSONStringSlice(raw)
		if len(parts) == 0 {
			return "", extra
		}
		return parts[0], append(append([]string{}, parts[1:]...), extra...)
	default:
		cmd := strings.TrimSpace(firstNonEmpty(mcpJSONString(obj, "command"), mcpJSONString(obj, "cmd")))
		if strings.HasPrefix(cmd, "[") {
			if parts := mcpJSONStringSlice(cmd); len(parts) > 0 {
				return parts[0], append(append([]string{}, parts[1:]...), extra...)
			}
		}
		return cmd, extra
	}
}

func mcpTransportKind(obj map[string]interface{}) string {
	raw := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		mcpJSONString(obj, "type"),
		mcpJSONString(obj, "transport"),
	)))
	switch raw {
	case "sse", "http", "https", "remote", "streamable-http", "streamablehttp", "streamable_http":
		return "remote"
	case "stdio", "local", "std-io":
		return "local"
	default:
		return ""
	}
}

func mcpJSONString(obj map[string]interface{}, key string) string {
	if obj == nil {
		return ""
	}
	s, ok := mcpJSONScalarString(obj[key])
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func mcpJSONBool(v interface{}) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case int:
		return n != 0
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i != 0
		}
		if f, err := n.Float64(); err == nil {
			return f != 0
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "true", "1", "yes":
			return true
		}
	}
	return false
}

func mcpJSONScalarString(v interface{}) (string, bool) {
	switch n := v.(type) {
	case string:
		return n, true
	case json.Number:
		return n.String(), true
	case float64:
		if n == float64(int64(n)) {
			return strconv.FormatInt(int64(n), 10), true
		}
		return strconv.FormatFloat(n, 'g', -1, 64), true
	case int:
		return strconv.Itoa(n), true
	case bool:
		if n {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func mcpJSONEnvMap(obj map[string]interface{}) map[string]string {
	if obj == nil {
		return nil
	}
	if env := mcpJSONStringMap(obj["env"]); len(env) > 0 {
		return env
	}
	return mcpJSONStringMap(obj["environment"])
}

func mcpJSONHeaderMap(v interface{}) map[string]string {
	switch typed := v.(type) {
	case map[string]interface{}, map[string]string:
		return mcpJSONStringMap(typed)
	case []interface{}, []string:
		out := map[string]string{}
		for _, item := range mcpJSONStringSlice(typed) {
			key, value, ok := strings.Cut(item, ":")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				continue
			}
			out[key] = strings.TrimSpace(value)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return mcpJSONStringMap(v)
	}
}

func mcpJSONStringMap(v interface{}) map[string]string {
	switch typed := v.(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, val := range typed {
			if key := strings.TrimSpace(k); key != "" {
				out[key] = val
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]interface{}:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, val := range typed {
			key := strings.TrimSpace(k)
			s, ok := mcpJSONScalarString(val)
			if key == "" || !ok {
				continue
			}
			out[key] = s
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []interface{}, []string:
		out := map[string]string{}
		for _, item := range mcpJSONStringSlice(typed) {
			key, value, ok := strings.Cut(item, "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				continue
			}
			out[key] = value
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return nil
		}
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			if _, isString := parsed.(string); !isString {
				return mcpJSONStringMap(parsed)
			}
		}
		lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' })
		items := make([]interface{}, 0, len(lines))
		for _, line := range lines {
			if s := strings.TrimSpace(line); s != "" {
				items = append(items, s)
			}
		}
		return mcpJSONStringMap(items)
	default:
		return nil
	}
}

func mcpJSONStringSlice(v interface{}) []string {
	switch typed := v.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := mcpJSONScalarString(item)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return nil
		}
		var parsed interface{}
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			if _, isString := parsed.(string); !isString {
				return mcpJSONStringSlice(parsed)
			}
		}
		return strings.Fields(raw)
	default:
		return nil
	}
}

func (b *MCPToolBridge) ImportMCPServers(ctx context.Context, p Principal, inputs []MCPServerCreateInput) ([]string, error) {
	if b == nil || b.svc == nil {
		return nil, fmt.Errorf("MCP service is not initialized")
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("json_config contains no MCP servers")
	}
	existing, err := b.svc.ListMCPServers(ctx, p)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing)+len(inputs))
	for _, server := range existing {
		seen[mcpImportIdentity(server.Name, server.Kind)] = true
	}
	for _, in := range inputs {
		key := mcpImportIdentity(in.Name, in.Kind)
		if seen[key] {
			return nil, fmt.Errorf("MCP server %q already exists", in.Name)
		}
		seen[key] = true
	}
	created := make([]string, 0, len(inputs))
	createdIDs := make([]string, 0, len(inputs))
	rollback := func() {
		for i := len(createdIDs) - 1; i >= 0; i-- {
			_ = b.svc.DeleteMCPServer(ctx, p, createdIDs[i])
		}
	}
	for _, in := range inputs {
		view, err := b.svc.CreateMCPServer(ctx, p, in)
		if err != nil {
			rollback()
			return nil, err
		}
		label := in.Name
		if view != nil && strings.TrimSpace(view.Name) != "" {
			label = view.Name
		}
		if view != nil && strings.TrimSpace(view.ID) != "" {
			createdIDs = append(createdIDs, view.ID)
		}
		created = append(created, label)
	}
	return created, nil
}

func mcpImportIdentity(name, kind string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" + strings.ToLower(strings.TrimSpace(kind))
}
