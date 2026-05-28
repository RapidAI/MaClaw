package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type MCPCapabilitySummary struct {
	External          bool   `json:"external,omitempty"`
	ID                string `json:"id"`
	CapabilityType    string `json:"capability_type,omitempty"`
	CapabilityID      string `json:"capability_id,omitempty"`
	DisplayName       string `json:"display_name,omitempty"`
	Description       string `json:"description,omitempty"`
	Source            string `json:"source,omitempty"`
	Status            string `json:"status,omitempty"`
	GlobalKey         string `json:"global_key,omitempty"`
	CurrentVersionKey string `json:"current_version_key,omitempty"`
	MetadataJSON      string `json:"metadata_json,omitempty"`
}

func (s *Service) SearchMCPMarket(ctx context.Context, p Principal, query string) ([]MCPCapabilitySummary, error) {
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	items := []MCPCapabilitySummary{}
	if strings.TrimSpace(cfg.AppConfig.RemoteHubURL) != "" && strings.TrimSpace(cfg.AppConfig.RemoteViewerToken) != "" {
		if hubItems, err := listHubMCPCapabilities(ctx, client, cfg.AppConfig.RemoteHubURL, cfg.AppConfig.RemoteViewerToken, query); err == nil {
			items = append(items, hubItems...)
		}
	}
	if shouldSearchHubCenterMCP(cfg.AppConfig.CapabilityMarketPolicy) {
		for _, base := range cfg.AppConfig.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs) {
			extItems, err := listHubCenterMCPCapabilities(ctx, client, base, query)
			if err == nil {
				items = mergeMCPCapabilities(items, extItems)
				break
			}
		}
	}
	return items, nil
}

func (s *Service) InstallMCPMarketCapability(ctx context.Context, p Principal, in MCPCapabilitySummary) (*MCPServerView, error) {
	cfg, err := s.requireUserConfigForMCP(p)
	if err != nil {
		return nil, err
	}
	item, err := s.resolveMCPMarketCapability(ctx, cfg, in)
	if err != nil {
		return nil, err
	}
	if item.CapabilityType != "" && !strings.EqualFold(item.CapabilityType, corelib.CapabilityTypeMCP) {
		return nil, fmt.Errorf("capability %s is not an MCP capability", firstMCPNonEmpty(item.ID, item.CapabilityID))
	}
	metadata := capabilityMetadataMap(item.MetadataJSON)
	if nested, ok := metadata["mcp"].(map[string]any); ok {
		for k, v := range nested {
			if _, exists := metadata[k]; !exists {
				metadata[k] = v
			}
		}
	}
	name := firstMCPNonEmpty(stringFromAny(metadata["name"]), item.DisplayName, item.CapabilityID, item.ID)
	if name == "" {
		return nil, fmt.Errorf("MCP capability name is required")
	}
	capRef := &corelib.MCPServerCapabilityRef{CapabilityID: firstMCPNonEmpty(item.CapabilityID, item.ID), VersionKey: firstMCPNonEmpty(item.CurrentVersionKey, stringFromAny(metadata["version_key"]), stringFromAny(metadata["version"])), Source: item.Source, GlobalKey: item.GlobalKey}
	now := s.now().UTC().Format(time.RFC3339)
	if command := strings.TrimSpace(stringFromAny(metadata["command"])); command != "" {
		entry := corelib.LocalMCPServerEntry{ID: firstMCPNonEmpty(stringFromAny(metadata["server_id"]), item.ID, NewID("mcp_market")), Name: name, Command: command, Args: stringSliceFromAny(metadata["args"]), Env: stringMapFromAny(metadata["env"]), AutoStart: true, CreatedAt: now, Source: corelib.MCPSourceMarket, Capability: capRef}
		upserted := upsertLocalMCP(&cfg.AppConfig, entry)
		if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
			return nil, err
		}
		if upserted.AutoStart && !upserted.Disabled {
			_, _ = s.StartMCPServer(ctx, p, upserted.ID)
		}
		return s.GetMCPServer(ctx, p, upserted.ID)
	}
	endpoint := strings.TrimSpace(firstMCPNonEmpty(stringFromAny(metadata["endpoint_url"]), stringFromAny(metadata["url"])))
	if endpoint == "" {
		return nil, fmt.Errorf("MCP capability %s has neither command nor endpoint_url", firstMCPNonEmpty(item.ID, item.CapabilityID))
	}
	authType := normalizeMCPAuthType(firstMCPNonEmpty(stringFromAny(metadata["auth_type"]), "none"))
	if authType == "" {
		return nil, fmt.Errorf("invalid MCP capability auth_type")
	}
	entry := corelib.MCPServerEntry{ID: firstMCPNonEmpty(stringFromAny(metadata["server_id"]), item.ID, NewID("mcp_market")), Name: name, EndpointURL: endpoint, AuthType: authType, Headers: stringMapFromAny(metadata["headers"]), CreatedAt: now, Source: corelib.MCPSourceMarket, Capability: capRef}
	upserted := upsertRemoteMCP(&cfg.AppConfig, entry)
	if err := s.saveRawUserConfig(p, cfg.AppConfig); err != nil {
		return nil, err
	}
	return s.GetMCPServer(ctx, p, upserted.ID)
}

func (s *Service) resolveMCPMarketCapability(ctx context.Context, cfg UserConfig, in MCPCapabilitySummary) (MCPCapabilitySummary, error) {
	item := in
	client := &http.Client{Timeout: 15 * time.Second}
	source := corelib.NormalizeCapabilitySource(item.Source)
	if item.External || source == corelib.CapabilitySourceHubCenter {
		query := firstMCPNonEmpty(item.GlobalKey, item.CapabilityID, item.ID, item.DisplayName)
		for _, base := range cfg.AppConfig.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs) {
			items, err := listHubCenterMCPCapabilities(ctx, client, base, query)
			if err != nil {
				continue
			}
			for _, candidate := range items {
				if sameMCPCapability(candidate, item) {
					return candidate, nil
				}
			}
		}
		return MCPCapabilitySummary{}, fmt.Errorf("MCP capability %s was not found in capability market", query)
	}
	if strings.TrimSpace(cfg.AppConfig.RemoteHubURL) != "" && strings.TrimSpace(cfg.AppConfig.RemoteViewerToken) != "" && strings.TrimSpace(item.ID) != "" {
		fetched, err := getHubMCPCapability(ctx, client, cfg.AppConfig.RemoteHubURL, cfg.AppConfig.RemoteViewerToken, item.ID)
		if err == nil {
			return fetched, nil
		}
	}
	if source == "" || source == corelib.CapabilitySourceEnterpriseHub {
		return MCPCapabilitySummary{}, fmt.Errorf("MCP capability %s must be resolved from enterprise hub before install", firstMCPNonEmpty(item.ID, item.CapabilityID))
	}
	if strings.TrimSpace(item.MetadataJSON) == "" {
		return MCPCapabilitySummary{}, fmt.Errorf("MCP capability metadata is required")
	}
	return item, nil
}

func listHubMCPCapabilities(ctx context.Context, client *http.Client, baseURL, token, query string) ([]MCPCapabilitySummary, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/capabilities?type=" + url.QueryEscape(corelib.CapabilityTypeMCP)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	var payload struct {
		Items []MCPCapabilitySummary `json:"items"`
	}
	if err := doMCPMarketJSON(client, req, &payload); err != nil {
		return nil, err
	}
	return filterMCPCapabilities(payload.Items, query), nil
}

func getHubMCPCapability(ctx context.Context, client *http.Client, baseURL, token, id string) (MCPCapabilitySummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(baseURL), "/")+"/api/capabilities/"+url.PathEscape(id), nil)
	if err != nil {
		return MCPCapabilitySummary{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	var item MCPCapabilitySummary
	err = doMCPMarketJSON(client, req, &item)
	return item, err
}

func listHubCenterMCPCapabilities(ctx context.Context, client *http.Client, baseURL, query string) ([]MCPCapabilitySummary, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/api/capability-market/mcp"
	if strings.TrimSpace(query) != "" {
		endpoint += "?q=" + url.QueryEscape(strings.TrimSpace(query))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := doMCPMarketJSON(client, req, &payload); err != nil {
		return nil, err
	}
	out := make([]MCPCapabilitySummary, 0, len(payload.Items))
	for _, raw := range payload.Items {
		id := firstMCPNonEmpty(stringFromAny(raw["id"]), stringFromAny(raw["capability_id"]), stringFromAny(raw["name"]), stringFromAny(raw["display_name"]))
		if id == "" {
			continue
		}
		metadata := map[string]any{"external": true}
		for _, key := range []string{"pricing", "license", "mcp", "secret_requirements", "version", "version_key"} {
			if v, ok := raw[key]; ok && v != nil {
				metadata[key] = v
			}
		}
		if m, ok := raw["mcp"].(map[string]any); ok {
			for _, key := range []string{"id", "name", "command", "args", "env", "endpoint_url", "url", "auth_type", "headers"} {
				if v, ok := m[key]; ok && v != nil {
					metadata[key] = v
				}
			}
		}
		out = append(out, MCPCapabilitySummary{External: true, ID: id, CapabilityType: corelib.CapabilityTypeMCP, CapabilityID: firstMCPNonEmpty(stringFromAny(raw["capability_id"]), id), DisplayName: firstMCPNonEmpty(stringFromAny(raw["display_name"]), stringFromAny(raw["name"]), id), Description: stringFromAny(raw["description"]), Source: corelib.CapabilitySourceHubCenter, Status: "external", GlobalKey: stringFromAny(raw["global_key"]), CurrentVersionKey: firstMCPNonEmpty(stringFromAny(raw["version_key"]), stringFromAny(raw["version"])), MetadataJSON: mustJSON(metadata)})
	}
	return filterMCPCapabilities(out, query), nil
}

func doMCPMarketJSON(client *http.Client, req *http.Request, dest any) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MCP marketplace request failed: status=%s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func shouldSearchHubCenterMCP(policy corelib.CapabilityMarketPolicy) bool {
	policy = policy.WithDefaults()
	return !policy.EffectiveEnterpriseOnlySearch()
}

func capabilityMetadataMap(raw string) map[string]any {
	var out map[string]any
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}
func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
func firstMCPNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
func stringSliceFromAny(v any) []string {
	out := []string{}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if s := stringFromAny(x); s != "" {
				out = append(out, s)
			}
		}
	}
	if a, ok := v.([]string); ok {
		return append(out, a...)
	}
	return out
}
func stringMapFromAny(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, x := range m {
			if s := stringFromAny(x); strings.TrimSpace(k) != "" && s != "" {
				out[strings.TrimSpace(k)] = s
			}
		}
	}
	if m, ok := v.(map[string]string); ok {
		return cleanStringMap(m)
	}
	return out
}

func filterMCPCapabilities(items []MCPCapabilitySummary, query string) []MCPCapabilitySummary {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return items
	}
	out := []MCPCapabilitySummary{}
	for _, item := range items {
		if strings.Contains(strings.ToLower(strings.Join([]string{item.ID, item.CapabilityID, item.DisplayName, item.Description, item.Source, item.GlobalKey}, " ")), needle) {
			out = append(out, item)
		}
	}
	return out
}

func sameMCPCapability(a, b MCPCapabilitySummary) bool {
	keys := map[string]bool{}
	for _, key := range []string{a.GlobalKey, a.CapabilityID, a.ID} {
		if strings.TrimSpace(key) != "" {
			keys[strings.ToLower(strings.TrimSpace(key))] = true
		}
	}
	for _, key := range []string{b.GlobalKey, b.CapabilityID, b.ID} {
		if keys[strings.ToLower(strings.TrimSpace(key))] {
			return true
		}
	}
	return false
}

func mergeMCPCapabilities(primary, extra []MCPCapabilitySummary) []MCPCapabilitySummary {
	seen := map[string]bool{}
	for _, item := range primary {
		for _, key := range []string{item.ID, item.CapabilityID, item.GlobalKey} {
			if strings.TrimSpace(key) != "" {
				seen[strings.ToLower(strings.TrimSpace(key))] = true
			}
		}
	}
	out := append([]MCPCapabilitySummary{}, primary...)
	for _, item := range extra {
		key := strings.ToLower(firstMCPNonEmpty(item.GlobalKey, item.CapabilityID, item.ID))
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	return out
}

func upsertRemoteMCP(cfg *corelib.AppConfig, entry corelib.MCPServerEntry) corelib.MCPServerEntry {
	local := make([]corelib.LocalMCPServerEntry, 0, len(cfg.LocalMCPServers))
	for _, existing := range cfg.LocalMCPServers {
		if !sameMCPInstallRef(existing.ID, existing.Capability, entry.ID, entry.Capability) {
			local = append(local, existing)
		}
	}
	cfg.LocalMCPServers = local
	for i := range cfg.MCPServers {
		if sameMCPInstallRef(cfg.MCPServers[i].ID, cfg.MCPServers[i].Capability, entry.ID, entry.Capability) {
			entry.AuthSecret = cfg.MCPServers[i].AuthSecret
			if cfg.MCPServers[i].CreatedAt != "" {
				entry.CreatedAt = cfg.MCPServers[i].CreatedAt
			}
			cfg.MCPServers[i] = entry
			return entry
		}
	}
	cfg.MCPServers = append(cfg.MCPServers, entry)
	return entry
}
func upsertLocalMCP(cfg *corelib.AppConfig, entry corelib.LocalMCPServerEntry) corelib.LocalMCPServerEntry {
	remote := make([]corelib.MCPServerEntry, 0, len(cfg.MCPServers))
	for _, existing := range cfg.MCPServers {
		if !sameMCPInstallRef(existing.ID, existing.Capability, entry.ID, entry.Capability) {
			remote = append(remote, existing)
		}
	}
	cfg.MCPServers = remote
	for i := range cfg.LocalMCPServers {
		if sameMCPInstallRef(cfg.LocalMCPServers[i].ID, cfg.LocalMCPServers[i].Capability, entry.ID, entry.Capability) {
			entry.Disabled = cfg.LocalMCPServers[i].Disabled
			if cfg.LocalMCPServers[i].CreatedAt != "" {
				entry.CreatedAt = cfg.LocalMCPServers[i].CreatedAt
			}
			cfg.LocalMCPServers[i] = entry
			return entry
		}
	}
	cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
	return entry
}

func sameMCPInstallRef(existingID string, existingCap *corelib.MCPServerCapabilityRef, nextID string, nextCap *corelib.MCPServerCapabilityRef) bool {
	if strings.TrimSpace(existingID) != "" && strings.TrimSpace(existingID) == strings.TrimSpace(nextID) {
		return true
	}
	if existingCap == nil || nextCap == nil {
		return false
	}
	for _, existing := range []string{existingCap.GlobalKey, existingCap.CapabilityID} {
		for _, next := range []string{nextCap.GlobalKey, nextCap.CapabilityID} {
			if strings.TrimSpace(existing) != "" && strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(next)) {
				return true
			}
		}
	}
	return false
}
