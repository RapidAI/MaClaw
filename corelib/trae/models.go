package trae

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// UpstreamModel is one SOLO catalog entry (config pose + display name).
type UpstreamModel struct {
	ID          string
	DisplayName string
}

// ListModels reads the SOLO model catalog for an authenticated realm. The
// catalog rides the chat host's get_detail_param endpoint; a missing or empty
// API answer falls back to the built-in default model so callers always have
// one usable entry.
func ListModels(ctx context.Context, profile Profile, accessToken, machineID, deviceID string) ([]UpstreamModel, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, profile.DefaultModel, fmt.Errorf("%s 模型目录需要已登录的访问令牌", profile.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, err := json.Marshal(map[string]any{
		"function":            "solo_work_lite",
		"config_names":        nil,
		"need_prompt":         false,
		"current_config_info": nil,
		"poly_prompt":         true,
		"mode_type":           nil,
		"agent_type":          nil,
	})
	if err != nil {
		return nil, profile.DefaultModel, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, profile.ChatHost+ModelsPath, bytes.NewReader(body))
	if err != nil {
		return nil, profile.DefaultModel, err
	}
	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	sessionKey := soloSessionKey("Bearer " + accessToken)
	h.Set("Authorization", "Cloud-IDE-JWT "+sessionKey)
	h.Set("X-Cloudide-Token", sessionKey)
	stampSoloAccountFamily(profile, h, sessionKey)
	// The stored login pair, when known, wins over the claims-derived one.
	if id := strings.TrimSpace(machineID); id != "" {
		h.Set("X-Machine-Id", id)
	}
	if id := strings.TrimSpace(deviceID); id != "" {
		h.Set("X-Device-Id", id)
	}
	// The catalog rides the shared proxy-aware OAuth client so the MaClaw
	// LLM-scope proxy config applies here too.
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return nil, profile.DefaultModel, fmt.Errorf("读取 %s 模型目录失败: %w", profile.Name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, profile.DefaultModel, fmt.Errorf("读取 %s 模型目录失败: %w", profile.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, profile.DefaultModel, fmt.Errorf("读取 %s 模型目录失败 (HTTP %d): %s", profile.Name, resp.StatusCode, summarizeBody(raw))
	}
	models, err := parseModelCatalog(profile.Name, raw)
	if err != nil {
		return nil, profile.DefaultModel, err
	}
	if len(models) == 0 {
		return nil, profile.DefaultModel, fmt.Errorf("%s 模型目录为空，回退默认模型", profile.Name)
	}
	return models, profile.DefaultModel, nil
}

// parseModelCatalog lifts config_name/display pairs from the catalog payload.
// profileName only feeds the error text so the message names the realm.
func parseModelCatalog(profileName string, raw []byte) ([]UpstreamModel, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("%s 模型目录无法解析", profileName)
	}
	configList := findObject(payload, "config_info_list", "configInfoList")
	if configList == nil {
		return nil, fmt.Errorf("%s 模型目录缺少 config_info_list", profileName)
	}
	var entries []map[string]any
	if err := json.Unmarshal(mustJSON(configList), &entries); err != nil {
		return nil, fmt.Errorf("%s 模型目录形态无法解析", profileName)
	}
	models := make([]UpstreamModel, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(asString(entry["config_name"]))
		if id == "" || strings.EqualFold(id, "auto") {
			continue
		}
		name := ""
		if display, ok := entry["display_config"].(map[string]any); ok {
			name = strings.TrimSpace(asString(display["display_name"]))
		}
		if name == "" {
			name = id
		}
		models = append(models, UpstreamModel{ID: id, DisplayName: name})
	}
	return models, nil
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("[]")
	}
	return raw
}
