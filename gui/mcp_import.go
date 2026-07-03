package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type mcpImportTarget string

const (
	mcpImportTargetAuto   mcpImportTarget = "auto"
	mcpImportTargetLocal  mcpImportTarget = "local"
	mcpImportTargetRemote mcpImportTarget = "remote"
)

var mcpImportServerKeys = []string{"mcpServers", "mcp_servers", "mcpservers"}

type mcpImportSummary struct {
	Local  []string
	Remote []string
}

func (s mcpImportSummary) total() int {
	return len(s.Local) + len(s.Remote)
}

func parseMCPImportTarget(raw string) (mcpImportTarget, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return mcpImportTargetAuto, nil
	case "local", "stdio":
		return mcpImportTargetLocal, nil
	case "remote", "http":
		return mcpImportTargetRemote, nil
	default:
		return "", fmt.Errorf("target must be auto, local, or remote")
	}
}

func parseMCPImportConfig(raw string, target mcpImportTarget) ([]corelib.LocalMCPServerEntry, []corelib.MCPServerEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, fmt.Errorf("json_config is required")
	}
	raw = stripMCPImportCodeFence(raw)
	raw = normalizeMCPImportJSONFragment(raw)
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON: %w", err)
	}
	serverRaw := mcpImportServerRaw(parsed, []byte(raw))
	var servers map[string]map[string]interface{}
	if err := json.Unmarshal(serverRaw, &servers); err != nil || len(servers) == 0 {
		return nil, nil, fmt.Errorf("invalid MCP JSON structure: expected {\"mcpServers\":{\"name\":{\"command\"...}}}")
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	localEntries := make([]corelib.LocalMCPServerEntry, 0, len(servers))
	remoteEntries := make([]corelib.MCPServerEntry, 0, len(servers))
	for _, name := range names {
		cfg := servers[name]
		displayName := firstNonEmpty(strings.TrimSpace(name), mcpImportStringField(cfg, "name"))
		if strings.TrimSpace(displayName) == "" {
			return nil, nil, fmt.Errorf("MCP server name is required")
		}
		command := mcpImportStringField(cfg, "command")
		endpointURL := mcpImportFirstStringField(cfg, "endpoint_url", "url")
		entryTarget := target
		if entryTarget == mcpImportTargetAuto {
			if endpointURL != "" && command == "" {
				entryTarget = mcpImportTargetRemote
			} else {
				entryTarget = mcpImportTargetLocal
			}
		}
		switch entryTarget {
		case mcpImportTargetLocal:
			if command == "" {
				return nil, nil, fmt.Errorf("local MCP server %q is missing command", name)
			}
			localEntries = append(localEntries, corelib.LocalMCPServerEntry{
				ID:        mcpImportStringField(cfg, "id"),
				Name:      displayName,
				Command:   command,
				Args:      mcpImportStringSliceField(cfg, "args"),
				Env:       mcpImportStringMapField(cfg, "env"),
				Disabled:  mcpImportBoolField(cfg, "disabled"),
				AutoStart: mcpImportBoolField(cfg, "auto_start"),
				Source:    corelib.MCPSourceManual,
			})
		case mcpImportTargetRemote:
			if endpointURL == "" {
				return nil, nil, fmt.Errorf("remote MCP server %q is missing url or endpoint_url", name)
			}
			headers := mcpImportStringMapField(cfg, "headers")
			authType := mcpImportStringField(cfg, "auth_type")
			authSecret := mcpImportStringField(cfg, "auth_secret")
			if authType == "" {
				authType = "none"
			}
			if authSecret == "" {
				if value, ok := mcpImportFindHeader(headers, "authorization"); ok {
					if strings.HasPrefix(strings.ToLower(value), "bearer ") {
						authType = "bearer"
						authSecret = strings.TrimSpace(value[7:])
					} else {
						authType = "api_key"
						authSecret = value
					}
				}
			}
			if authSecret != "" {
				headers = mcpImportWithoutHeader(headers, "authorization")
			}
			remoteEntries = append(remoteEntries, corelib.MCPServerEntry{
				ID:          mcpImportStringField(cfg, "id"),
				Name:        displayName,
				EndpointURL: endpointURL,
				AuthType:    authType,
				AuthSecret:  authSecret,
				Headers:     headers,
				Source:      corelib.MCPSourceManual,
			})
		}
	}
	return localEntries, remoteEntries, nil
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
	if !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		return raw
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func normalizeMCPImportJSONFragment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || json.Valid([]byte(raw)) {
		return raw
	}
	if wrapped := "{" + raw + "}"; json.Valid([]byte(wrapped)) {
		return wrapped
	}
	if wrapped, ok := wrapMCPImportServerFragment(raw); ok {
		return wrapped
	}
	return raw
}

func wrapMCPImportServerFragment(raw string) (string, bool) {
	colon := strings.Index(raw, ":")
	if colon <= 0 {
		return "", false
	}
	key := strings.TrimSpace(raw[:colon])
	key = strings.Trim(key, `"'`)
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	if normalized != "mcpservers" {
		return "", false
	}
	rest := strings.TrimSpace(raw[colon:])
	for _, key := range mcpImportServerKeys {
		wrapped := "{\"" + key + "\"" + rest + "}"
		if json.Valid([]byte(wrapped)) {
			return wrapped, true
		}
	}
	return "", false
}

func mcpImportServerRaw(parsed map[string]json.RawMessage, fallback []byte) []byte {
	for _, key := range mcpImportServerKeys {
		if nested, ok := parsed[key]; ok {
			return nested
		}
	}
	for key, nested := range parsed {
		normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
		if normalized == "mcpservers" {
			return nested
		}
	}
	return fallback
}

func (a *App) ImportMCPServersFromJSON(jsonConfig, targetRaw string) (mcpImportSummary, error) {
	target, err := parseMCPImportTarget(targetRaw)
	if err != nil {
		return mcpImportSummary{}, err
	}
	localEntries, remoteEntries, err := parseMCPImportConfig(jsonConfig, target)
	if err != nil {
		return mcpImportSummary{}, err
	}
	if a == nil || a.mcpRegistry == nil {
		return mcpImportSummary{}, fmt.Errorf("MCP registry not initialized")
	}
	prepareLocalMCPImportIDs(localEntries)
	prepareRemoteMCPImportIDs(remoteEntries)
	if err := a.validateMCPImportConflicts(localEntries, remoteEntries); err != nil {
		return mcpImportSummary{}, err
	}
	if err := a.validateMCPImportAllowed(localEntries, remoteEntries); err != nil {
		return mcpImportSummary{}, err
	}
	summary := mcpImportSummary{}
	importedLocalIDs := make([]string, 0, len(localEntries))
	importedRemoteIDs := make([]string, 0, len(remoteEntries))
	fail := func(err error) (mcpImportSummary, error) {
		a.rollbackMCPImport(importedLocalIDs, importedRemoteIDs)
		a.invalidateIMToolCaches()
		return mcpImportSummary{}, err
	}
	for _, entry := range localEntries {
		if err := a.RegisterLocalMCPServer(entry); err != nil {
			return fail(fmt.Errorf("import local MCP %q failed: %w", entry.Name, err))
		}
		importedLocalIDs = append(importedLocalIDs, entry.ID)
		summary.Local = append(summary.Local, entry.Name)
	}
	for _, entry := range remoteEntries {
		if err := a.importRemoteMCPServer(entry); err != nil {
			return fail(fmt.Errorf("import remote MCP %q failed: %w", entry.Name, err))
		}
		importedRemoteIDs = append(importedRemoteIDs, entry.ID)
		summary.Remote = append(summary.Remote, entry.Name)
	}
	if len(localEntries) > 0 {
		_ = a.SyncLocalMCPServers()
	}
	a.invalidateIMToolCaches()
	a.warmImportedRemoteMCPServers(importedRemoteIDs)
	return summary, nil
}

func (a *App) validateMCPImportAllowed(localEntries []corelib.LocalMCPServerEntry, remoteEntries []corelib.MCPServerEntry) error {
	if a == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	for _, entry := range localEntries {
		args := map[string]interface{}{"command": localMCPImportCommand(entry), "action": "import_local_mcp", "server_id": entry.ID}
		if err := a.ensureWorkflowAllowsRemoteToolCall("bash", args); err != nil {
			return fmt.Errorf("local MCP %q rejected by workflow policy: %w", entry.Name, err)
		}
		if ok, reason := a.enforceHubSecurityAppPolicy("bash", args); !ok {
			return fmt.Errorf("local MCP %q rejected by security policy: %s", entry.Name, reason)
		}
	}
	for _, entry := range remoteEntries {
		args := map[string]interface{}{"action": "import_remote_mcp", "server_id": entry.ID, "endpoint_url": entry.EndpointURL}
		if err := a.ensureWorkflowAllowsRemoteToolCall("call_mcp_tool", args); err != nil {
			return fmt.Errorf("remote MCP %q rejected by workflow policy: %w", entry.Name, err)
		}
		if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": entry.EndpointURL}); !ok {
			return fmt.Errorf("remote MCP %q rejected by security policy: %s", entry.Name, reason)
		}
	}
	return nil
}

func (a *App) rollbackMCPImport(localIDs, remoteIDs []string) {
	if a == nil || a.mcpRegistry == nil {
		return
	}
	for i := len(remoteIDs) - 1; i >= 0; i-- {
		id := strings.TrimSpace(remoteIDs[i])
		if id == "" {
			continue
		}
		if err := a.mcpRegistry.Unregister(id); err != nil {
			log.Printf("[MCPImport] rollback remote MCP %s failed: %v", id, err)
		}
	}
	for i := len(localIDs) - 1; i >= 0; i-- {
		id := strings.TrimSpace(localIDs[i])
		if id == "" {
			continue
		}
		if err := a.mcpRegistry.UnregisterLocal(id); err != nil {
			log.Printf("[MCPImport] rollback local MCP %s failed: %v", id, err)
		}
	}
}

func (a *App) warmImportedRemoteMCPServers(serverIDs []string) {
	if a == nil || a.mcpRegistry == nil {
		return
	}
	for _, serverID := range serverIDs {
		serverID := strings.TrimSpace(serverID)
		if serverID == "" {
			continue
		}
		a.mcpRegistry.warmServerToolsAsync(serverID, func(err error) {
			logMCPToolSyncResult("background tool sync after import", serverID, err)
			a.invalidateIMToolCaches()
		})
	}
}

func localMCPImportCommand(entry corelib.LocalMCPServerEntry) string {
	return strings.Join(append([]string{entry.Command}, entry.Args...), " ")
}

func (a *App) importRemoteMCPServer(server corelib.MCPServerEntry) error {
	if a == nil || a.mcpRegistry == nil {
		return fmt.Errorf("MCP registry not initialized")
	}
	if err := a.ensureWorkflowAllowsRemoteToolCall("call_mcp_tool", map[string]interface{}{"action": "import_remote_mcp", "server_id": server.ID, "endpoint_url": server.EndpointURL}); err != nil {
		return err
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": server.EndpointURL}); !ok {
		return fmt.Errorf("%s", reason)
	}
	_, err := a.mcpRegistry.register(server, false)
	if err != nil {
		return err
	}
	a.invalidateIMToolCaches()
	return nil
}

func prepareLocalMCPImportIDs(entries []corelib.LocalMCPServerEntry) {
	if len(entries) == 0 {
		return
	}
	base := time.Now().UnixNano()
	seen := make(map[string]bool, len(entries))
	for i := range entries {
		id := strings.TrimSpace(entries[i].ID)
		if id == "" {
			id = fmt.Sprintf("local-%d", base+int64(i))
		}
		for seen[id] {
			id = fmt.Sprintf("local-%d", base+int64(len(seen)+1))
		}
		entries[i].ID = id
		seen[id] = true
	}
}

func prepareRemoteMCPImportIDs(entries []corelib.MCPServerEntry) {
	if len(entries) == 0 {
		return
	}
	base := time.Now().UnixMilli()
	seen := make(map[string]bool, len(entries))
	for i := range entries {
		id := strings.TrimSpace(entries[i].ID)
		if id == "" {
			id = fmt.Sprintf("%s-%d", sanitizeMCPID(entries[i].Name), base+int64(i))
		}
		for seen[id] {
			id = fmt.Sprintf("%s-%d", sanitizeMCPID(entries[i].Name), base+int64(len(seen)+1))
		}
		entries[i].ID = id
		seen[id] = true
	}
}

func (a *App) validateMCPImportConflicts(localEntries []corelib.LocalMCPServerEntry, remoteEntries []corelib.MCPServerEntry) error {
	if a == nil || a.mcpRegistry == nil {
		return nil
	}
	localExisting := make(map[string]string, len(localEntries))
	for _, existing := range a.mcpRegistry.ListLocalServers() {
		id := strings.TrimSpace(existing.ID)
		if id != "" {
			localExisting[id] = strings.TrimSpace(existing.Name)
		}
	}
	if err := validateMCPImportIDs("local", localMCPImportIDItems(localEntries), localExisting); err != nil {
		return err
	}

	remoteExisting := make(map[string]string, len(remoteEntries))
	for _, existing := range a.mcpRegistry.ListServers() {
		id := strings.TrimSpace(existing.ID)
		if id != "" {
			remoteExisting[id] = strings.TrimSpace(existing.Name)
		}
	}
	if err := validateMCPImportIDs("remote", remoteMCPImportIDItems(remoteEntries), remoteExisting); err != nil {
		return err
	}
	return nil
}

type mcpImportIDItem struct {
	ID string
}

func validateMCPImportIDs(kind string, entries []mcpImportIDItem, existing map[string]string) error {
	importSeen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if importSeen[id] {
			return fmt.Errorf("duplicate %s MCP server id %q in import JSON", kind, id)
		}
		importSeen[id] = true
		if existingName, ok := existing[id]; ok {
			if existingName == "" {
				existingName = id
			}
			return fmt.Errorf("%s MCP server id %q already exists (%s)", kind, id, existingName)
		}
	}
	return nil
}

func localMCPImportIDItems(entries []corelib.LocalMCPServerEntry) []mcpImportIDItem {
	items := make([]mcpImportIDItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, mcpImportIDItem{ID: entry.ID})
	}
	return items
}

func remoteMCPImportIDItems(entries []corelib.MCPServerEntry) []mcpImportIDItem {
	items := make([]mcpImportIDItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, mcpImportIDItem{ID: entry.ID})
	}
	return items
}

func (h *IMMessageHandler) toolImportMCPServers(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return "import_mcp_servers failed: app is unavailable"
	}
	jsonConfig := mcpImportStringArg(args, "json_config")
	if jsonConfig == "" {
		jsonConfig = mcpImportStringArg(args, "config")
	}
	target := mcpImportStringArg(args, "target")
	summary, err := h.app.ImportMCPServersFromJSON(jsonConfig, target)
	if err != nil {
		return "import_mcp_servers failed: " + err.Error()
	}
	if summary.total() == 0 {
		return "No MCP servers imported."
	}
	parts := make([]string, 0, 2)
	if len(summary.Local) > 0 {
		parts = append(parts, fmt.Sprintf("local: %s", strings.Join(summary.Local, ", ")))
	}
	if len(summary.Remote) > 0 {
		parts = append(parts, fmt.Sprintf("remote: %s", strings.Join(summary.Remote, ", ")))
	}
	return fmt.Sprintf("Imported %d MCP server(s): %s", summary.total(), strings.Join(parts, "; "))
}

func mcpImportStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case map[string]interface{}, []interface{}:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
	case map[string]string:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
	case json.RawMessage:
		return strings.TrimSpace(string(v))
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mcpImportStringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func mcpImportFirstStringField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v := mcpImportStringField(m, key); v != "" {
			return v
		}
	}
	return ""
}

func mcpImportBoolField(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func mcpImportStringSliceField(m map[string]interface{}, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return values
	default:
		return nil
	}
}

func mcpImportStringMapField(m map[string]interface{}, key string) map[string]string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	values, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mcpImportFindHeader(headers map[string]string, name string) (string, bool) {
	for key, value := range headers {
		if strings.EqualFold(key, name) && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func mcpImportWithoutHeader(headers map[string]string, name string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
