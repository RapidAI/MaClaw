// Model catalog: read via the official CLI's signed contract — the wasm-backed
// COSY bearer plus the /algo-prefixed catalog path on the per-site inference
// host. The response is a map of scene → model entries parsed as plain JSON;
// deployments that instead serve an encrypted payload make the parse fail, and
// callers keep their fallback default.
package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/qoder/cosy"
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

// ListModels fetches the full server catalog through the official CLI's
// signing contract: the wasm COSY context (uid + device token folded in)
// prepares the /algo/api/v2/model/list request on the site inference host.
// Returns the parsed models plus the default model key. The catalog parse is
// tolerant: any set of scene arrays is accepted, scenes are read in a
// deterministic order (device-account scene, then the CLI scene, then the
// rest by name), and the earliest enabled is_default entry wins.
func ListModels(ctx context.Context, profile Profile, uid, accessToken string) ([]Model, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, "", fmt.Errorf("未登录，无法获取 Qoder 模型列表")
	}
	signed, err := cosy.New(MachineID(), ClientVersion, strings.TrimSpace(uid))
	if err != nil {
		return nil, "", fmt.Errorf("Qoder 签名上下文初始化失败: %w", err)
	}
	defer signed.Close()
	if err := signed.RefreshAuthFields(strings.TrimSpace(accessToken)); err != nil {
		return nil, "", fmt.Errorf("Qoder 签名材料注入失败: %w", err)
	}
	prepared, err := signed.PrepareRequest(strings.TrimRight(profile.InferBase, "/"), "/algo/api/v2/model/list", http.MethodGet, "auth", "", "")
	if err != nil {
		return nil, "", fmt.Errorf("Qoder 签名请求构建失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prepared.URL, nil)
	if err != nil {
		return nil, "", err
	}
	for key, value := range prepared.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.DoNoFollow(req)
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
// The earliest enabled is_default entry wins; scenes are visited in a stable
// order so the chosen default does not depend on Go map iteration order.
func parseCatalog(body []byte) ([]Model, string, error) {
	var scenes map[string]json.RawMessage
	if err := json.Unmarshal(body, &scenes); err != nil {
		return nil, "", fmt.Errorf("Qoder 模型列表响应无法解析: %w", err)
	}
	var models []Model
	defaultKey := ""
	fallbackKey := ""
	for _, scene := range sortedScenes(scenes) {
		var entries []json.RawMessage
		if err := json.Unmarshal(scenes[scene], &entries); err != nil {
			continue
		}
		for _, rawEntry := range entries {
			model, ok := parseModel(rawEntry, scene)
			if !ok {
				continue
			}
			models = append(models, model)
			if !model.Enabled {
				continue
			}
			if model.IsDefault && defaultKey == "" {
				defaultKey = model.Key
			}
			if fallbackKey == "" {
				fallbackKey = model.Key
			}
		}
	}
	if len(models) == 0 {
		return nil, "", fmt.Errorf("Qoder 模型列表为空")
	}
	if defaultKey == "" {
		defaultKey = fallbackKey
	}
	return models, defaultKey, nil
}

// sortedScenes orders the catalog scenes deterministically: the device
// account scene first, then the CLI scene, then everything else by name.
func sortedScenes(scenes map[string]json.RawMessage) []string {
	priority := map[string]int{"assistant": 0, "agent": 1, "cli": 2}
	names := make([]string, 0, len(scenes))
	for scene := range scenes {
		names = append(names, scene)
	}
	sort.Slice(names, func(i, j int) bool {
		pi, pj := priority[names[i]], priority[names[j]]
		if pi == pj {
			return names[i] < names[j]
		}
		return pi < pj
	})
	return names
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
