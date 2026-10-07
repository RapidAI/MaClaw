// Model catalog: the official CLI reads GET /api/v2/model/list?Encode=1 on the
// per-site inference host. The response is a map of scene → model entries; the
// served payload may be encrypted upstream, in which case the fetch fails and
// callers keep their fallback default.
package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model is one catalog entry.
type Model struct {
	Key         string   `json:"key"`
	DisplayName string   `json:"display_name"`
	IsDefault   bool     `json:"is_default"`
	Enabled     bool     `json:"enabled"`
	Reasoning   bool     `json:"is_reasoning"`
	Vision      bool     `json:"is_vl"`
	MaxInput    int      `json:"max_input_tokens"`
	MaxOutput   int      `json:"max_output_tokens"`
	Tags        []string `json:"tags"`
	Scene       string   `json:"scene"`
}

// ListModels fetches the catalog. Scenes are parsed in the order the server
// sent them; the device account scene ("assistant") and any other arrays the
// server includes are all accepted, mirroring the CLI's tolerant parse.
func ListModels(ctx context.Context, profile Profile, accessToken string) ([]Model, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, "", fmt.Errorf("未登录，无法获取 Qoder 模型列表")
	}
	endpoint := strings.TrimRight(profile.InferBase, "/") + "/api/v2/model/list?Encode=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("获取 Qoder 模型列表失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", fmt.Errorf("读取 Qoder 模型列表响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("获取 Qoder 模型列表失败 (HTTP %d): %s", resp.StatusCode, truncateForError(body))
	}
	return parseCatalog(body)
}

// parseCatalog accepts {scene: [entry...]} where every scene name is allowed.
// Entries carry their server scene for debugging; the earliest is_default
// entry wins (the CLI only trusts the configured scene's default).
func parseCatalog(body []byte) ([]Model, string, error) {
	var scenes map[string]json.RawMessage
	if err := json.Unmarshal(body, &scenes); err != nil {
		return nil, "", fmt.Errorf("Qoder 模型列表响应无法解析: %w", err)
	}
	var models []Model
	defaultKey := ""
	for scene, raw := range scenes {
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			continue
		}
		for _, rawEntry := range entries {
			model, ok := parseModel(rawEntry, scene)
			if !ok {
				continue
			}
			models = append(models, model)
			if model.IsDefault && defaultKey == "" {
				defaultKey = model.Key
			}
		}
	}
	if len(models) == 0 {
		return nil, "", fmt.Errorf("Qoder 模型列表为空")
	}
	return models, defaultKey, nil
}

type rawModel struct {
	Key         string   `json:"key"`
	ModelKey    string   `json:"model_key"`
	DisplayName string   `json:"display_name"`
	Name        string   `json:"name"`
	IsDefault   bool     `json:"is_default"`
	Enable      any      `json:"enable"` // bool or 0/1 in past server versions
	IsReasoning bool     `json:"is_reasoning"`
	IsVL        bool     `json:"is_vl"`
	MaxInput    int      `json:"max_input_tokens"`
	MaxOutput   int      `json:"max_output_tokens"`
	Tags        []string `json:"tags"`
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return true
	}
}

func parseModel(raw json.RawMessage, scene string) (Model, bool) {
	if len(raw) == 0 {
		return Model{}, false
	}
	var entry rawModel
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Model{}, false
	}
	key := strings.TrimSpace(entry.Key)
	if key == "" {
		key = strings.TrimSpace(entry.ModelKey)
	}
	if key == "" {
		return Model{}, false
	}
	display := strings.TrimSpace(entry.DisplayName)
	if display == "" {
		display = strings.TrimSpace(entry.Name)
	}
	enabled := truthy(entry.Enable)
	return Model{
		Key:         key,
		DisplayName: display,
		IsDefault:   entry.IsDefault,
		Enabled:     enabled,
		Reasoning:   entry.IsReasoning,
		Vision:      entry.IsVL,
		MaxInput:    entry.MaxInput,
		MaxOutput:   entry.MaxOutput,
		Tags:        entry.Tags,
		Scene:       scene,
	}, true
}
