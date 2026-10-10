package trae

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// UpstreamModel is one SOLO catalog entry (config pose + display name).
type UpstreamModel struct {
	ID          string
	DisplayName string
}

// catalogDo posts one catalog request. ListModels walks every chat pool, and
// tests replace this with a scripted round trip.
var catalogDo = oauth.DoNoFollow

// ListModels reads the SOLO model catalog for an authenticated realm. Each
// chat pool has its own get_detail_param list. The pools are asked together
// so the picker gets their union in one round trip, in pool order. A missing
// or empty answer from every pool falls back to the built-in default model.
// The status ping uses ProbeModels; it only needs one reachable list.
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
	outcomes := make([]catalogOutcome, len(chatFunctionOrder))
	var wg sync.WaitGroup
	for i, fn := range chatFunctionOrder {
		wg.Add(1)
		go func(i int, fn string) {
			defer wg.Done()
			outcomes[i].models, outcomes[i].err, outcomes[i].stop = fetchModelCatalog(ctx, profile, accessToken, machineID, deviceID, fn)
		}(i, fn)
	}
	wg.Wait()
	var models []UpstreamModel
	seen := map[string]struct{}{}
	var hard, soft error
	for _, outcome := range outcomes {
		if outcome.err != nil {
			if outcome.stop {
				if hard == nil {
					hard = outcome.err
				}
			} else if soft == nil {
				soft = outcome.err
			}
			continue
		}
		for _, model := range outcome.models {
			if _, ok := seen[model.ID]; ok {
				continue
			}
			seen[model.ID] = struct{}{}
			models = append(models, model)
		}
	}
	if len(models) > 0 {
		return models, profile.DefaultModel, nil
	}
	if hard != nil {
		return nil, profile.DefaultModel, hard
	}
	if soft != nil {
		return nil, profile.DefaultModel, soft
	}
	return nil, profile.DefaultModel, fmt.Errorf("%s 模型目录为空，回退默认模型", profile.Name)
}

// ProbeModels asks chat pools in order until one returns models or a hard
// failure (host, transport, quota, auth). Membership refusals are skipped.
// One successful pool is enough to answer "is the catalog reachable", so the
// status ping does not fan out to every pool.
func ProbeModels(ctx context.Context, profile Profile, accessToken, machineID, deviceID string) ([]UpstreamModel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("%s 模型目录需要已登录的访问令牌", profile.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var soft error
	for _, fn := range chatFunctionOrder {
		if err := ctx.Err(); err != nil {
			if soft != nil {
				return nil, soft
			}
			return nil, err
		}
		batch, err, stop := fetchModelCatalog(ctx, profile, accessToken, machineID, deviceID, fn)
		if err != nil {
			if stop {
				return nil, err
			}
			soft = err
			continue
		}
		if len(batch) > 0 {
			return batch, nil
		}
	}
	if soft != nil {
		return nil, soft
	}
	return nil, fmt.Errorf("%s 模型目录为空，回退默认模型", profile.Name)
}

// catalogOutcome is one pool's catalog read. stop marks a host, transport,
// quota, or auth failure. A membership refusal is not stop: another pool
// may still list models.
type catalogOutcome struct {
	models []UpstreamModel
	err    error
	stop   bool
}

// fetchModelCatalog reads one pool. stop is set for a host, transport, quota,
// or auth failure. A 4001/4023 membership refusal leaves stop false.
func fetchModelCatalog(ctx context.Context, profile Profile, accessToken, machineID, deviceID, fn string) ([]UpstreamModel, error, bool) {
	body, err := json.Marshal(map[string]any{
		"function":            fn,
		"config_names":        nil,
		"need_prompt":         false,
		"current_config_info": nil,
		"poly_prompt":         true,
		"mode_type":           nil,
		"agent_type":          nil,
	})
	if err != nil {
		return nil, err, true
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, profile.ChatHost+ModelsPath, bytes.NewReader(body))
	if err != nil {
		return nil, err, true
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
	resp, err := catalogDo(req)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 模型目录失败: %w", profile.Name, err), true
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 模型目录失败: %w", profile.Name, err), true
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("读取 %s 模型目录失败 (HTTP %d): %s", profile.Name, resp.StatusCode, summarizeBody(raw)), true
	}
	models, err := parseModelCatalog(profile.Name, raw)
	if err == nil {
		return models, nil, false
	}
	// 4001/4023 means this pool does not list the catalog. Quota and auth
	// codes are the account's answer and will not change on the next pool.
	if code, detail, ok := businessEnvelope(raw); ok {
		return nil, fmt.Errorf("%s 模型目录上游错误 code=%s: %s", profile.Name, code, detail), !wrongFunctionCode(code)
	}
	return nil, err, false
}

// businessEnvelope reports a top-level business code other than success.
func businessEnvelope(raw []byte) (code, detail string, ok bool) {
	var envelope struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return "", "", false
	}
	code = strings.TrimSpace(anyTrim(envelope.Code))
	if code == "" || code == "0" {
		return "", "", false
	}
	detail = strings.TrimSpace(envelope.Message)
	if detail == "" {
		detail = strings.TrimSpace(envelope.Msg)
	}
	return code, detail, true
}

// parseModelCatalog lifts chat config_name/display pairs from the catalog payload.
// profileName names the realm in errors. The international realm also drops
// entries the IDE marks invisible.
//
// get_detail_param returns config_info_list as a JSON array. findObject only
// accepts objects, so feeding it that array reports the list as missing.
func parseModelCatalog(profileName string, raw []byte) ([]UpstreamModel, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("%s 模型目录无法解析", profileName)
	}
	configList := findKeyed(payload, "config_info_list", "configInfoList", "ConfigInfoList")
	entries, ok := catalogEntries(configList)
	if !ok {
		return nil, fmt.Errorf("%s 模型目录缺少 config_info_list (%s)", profileName, payloadKeyList(payload))
	}
	hideInvisible := profileName == NameGlobal
	models := make([]UpstreamModel, 0, len(entries))
	for _, entry := range entries {
		if !catalogChatEntry(entry, hideInvisible) {
			continue
		}
		id := strings.TrimSpace(asString(entry["config_name"]))
		name := catalogDisplayName(entry)
		if name == "" {
			name = id
		}
		models = append(models, UpstreamModel{ID: id, DisplayName: name})
	}
	return models, nil
}

// catalogChatEntry reports a config llm_utils_chat can be asked with.
// The same list also carries the IDE's own helpers (summary, fast apply,
// title generation), configs switched off, and custom-model slots. Asking
// one of those by name is a 4001. hideInvisible is the international picker:
// it also drops auto-mode channels and pay-as-you-go twins.
func catalogChatEntry(entry map[string]any, hideInvisible bool) bool {
	id := strings.TrimSpace(asString(entry["config_name"]))
	if id == "" || strings.EqualFold(id, "auto") {
		return false
	}
	if strings.HasPrefix(strings.ToLower(id), "custom_model") {
		return false
	}
	if usage := strings.TrimSpace(asString(entry["usage"])); usage != "" && usage != "chat_completion" {
		return false
	}
	if off, ok := entry["config_switch"].(bool); ok && !off {
		return false
	}
	if catalogCustomModel(entry) {
		return false
	}
	if hideInvisible {
		if hidden, ok := entry["is_invisible_to_user"].(bool); ok && hidden {
			return false
		}
	}
	return true
}

func catalogCustomModel(entry map[string]any) bool {
	if custom, ok := entry["is_custom_model"].(bool); ok && custom {
		return true
	}
	display, ok := entry["display_config"].(map[string]any)
	if !ok {
		return false
	}
	custom, ok := display["is_custom_model"].(bool)
	return ok && custom
}

// findKeyed returns the first value stored under one of keys, including arrays.
// findObject cannot be used here: it drops every non-object value.
func findKeyed(payload any, keys ...string) any {
	switch tree := payload.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := tree[key]; ok && raw != nil {
				return raw
			}
		}
		// Map iteration order is random. A response can carry more than one
		// nested list; walk stable names and prefer the containers the host
		// actually wraps the catalog in.
		names := make([]string, 0, len(tree))
		for name := range tree {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool {
			if rank := catalogContainerRank(names[i]) - catalogContainerRank(names[j]); rank != 0 {
				return rank < 0
			}
			return names[i] < names[j]
		})
		for _, name := range names {
			if nested := findKeyed(tree[name], keys...); nested != nil {
				return nested
			}
		}
	case []any:
		for _, child := range tree {
			if nested := findKeyed(child, keys...); nested != nil {
				return nested
			}
		}
	}
	return nil
}

func catalogContainerRank(name string) int {
	switch name {
	case "Result", "result", "data", "Data", "response", "Response":
		return 0
	default:
		return 1
	}
}

// catalogEntries accepts the live array and a single config object.
func catalogEntries(raw any) ([]map[string]any, bool) {
	return catalogEntriesDepth(raw, 0)
}

func catalogEntriesDepth(raw any, depth int) ([]map[string]any, bool) {
	if depth > 4 {
		return nil, false
	}
	switch value := raw.(type) {
	case []any:
		entries := make([]map[string]any, 0, len(value))
		for _, item := range value {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			entries = append(entries, obj)
		}
		return entries, true
	case map[string]any:
		switch nested := findKeyed(value, "config_info_list", "configInfoList", "ConfigInfoList").(type) {
		case []any:
			return catalogEntriesDepth(nested, depth+1)
		case map[string]any:
			if strings.TrimSpace(asString(nested["config_name"])) != "" {
				return []map[string]any{nested}, true
			}
		}
		if strings.TrimSpace(asString(value["config_name"])) == "" {
			return nil, false
		}
		return []map[string]any{value}, true
	default:
		return nil, false
	}
}

func catalogDisplayName(entry map[string]any) string {
	if display, ok := entry["display_config"].(map[string]any); ok {
		if name := strings.TrimSpace(asString(display["display_name"])); name != "" {
			return name
		}
	}
	return strings.TrimSpace(asString(entry["display_name"]))
}

func payloadKeyList(payload any) string {
	obj, ok := payload.(map[string]any)
	if !ok || len(obj) == 0 {
		return "empty"
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
