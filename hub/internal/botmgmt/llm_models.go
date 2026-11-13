package botmgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corelibllm "github.com/RapidAI/CodeClaw/corelib/llm"
)

// llmModelFetchLimit caps one models response at 128 KiB. A provider catalog
// is metadata, and anything past that is either abuse or a misrouted page.
const llmModelFetchLimit = 128 << 10

// llmModelFetchTimeout bounds one upstream /models call. Model discovery runs
// on an admin's click, so a slow provider must fail visibly, not hang the tab.
const llmModelFetchTimeout = 20 * time.Second

// llmModelFetchClient is swappable so tests point the fetcher at httptest
// servers instead of the network.
var llmModelFetchClient = &http.Client{Timeout: llmModelFetchTimeout}

// LLMModelsInput names the provider whose models an administrator wants to
// pick from. ProviderID resolves protocol/url/key from the stored settings;
// an unknown provider must carry all three itself (the card being edited has
// not been saved yet).
type LLMModelsInput struct {
	ProviderID string `json:"provider_id"`
	Protocol   string `json:"protocol"`
	URL        string `json:"url"`
	Key        string `json:"key"`
}

// LLMModelItem is one upstream model in an admin picker.
type LLMModelItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FetchLLMProviderModels asks an added provider for its model catalog.
// A blank key on a stored provider reuses the stored key, and a blank
// protocol or url falls back to the stored values, so the administrator does
// not have to retype credentials that are already saved.
func (s *Service) FetchLLMProviderModels(ctx context.Context, tenantID string, in LLMModelsInput) ([]LLMModelItem, error) {
	if s == nil || s.System == nil {
		return nil, ErrSettingsUnavailable
	}
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	if in.ProviderID != "" {
		protocol, url, key, err := s.storedLLMProviderCreds(ctx, tenantID, in.ProviderID)
		if err != nil {
			return nil, err
		}
		if in.Protocol == "" {
			in.Protocol = protocol
		}
		if in.URL == "" {
			in.URL = url
		}
		if in.Key == "" {
			in.Key = key
		}
	}
	in.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	in.Key = strings.TrimSpace(in.Key)
	if in.Protocol != "openai" && in.Protocol != "anthropic" {
		return nil, fmt.Errorf("%w: llm provider protocol must be openai or anthropic", ErrInvalidInput)
	}
	if err := validateBaseURL(in.URL); err != nil {
		return nil, fmt.Errorf("%w: llm provider url is invalid", ErrInvalidInput)
	}
	if in.Key == "" {
		return nil, fmt.Errorf("%w: llm provider key is required", ErrInvalidInput)
	}
	if strings.ContainsAny(in.Key, "\r\n") || len(in.Key) > 4096 {
		return nil, fmt.Errorf("%w: llm provider key is invalid", ErrInvalidInput)
	}
	return fetchLLMModelList(ctx, in.Protocol, in.URL, in.Key)
}

// storedLLMProviderCreds reads one stored provider's credentials. The network
// call happens without the settings lock, so only what the fetch needs leaves
// the critical section.
func (s *Service) storedLLMProviderCreds(ctx context.Context, tenantID, providerID string) (protocol, url, key string, err error) {
	s.mu.Lock()
	rec, loadErr := s.load(ctx, tenantID)
	if loadErr == nil {
		for _, item := range rec.LLM.Providers {
			if item.ID == providerID {
				protocol, url, key = item.Protocol, item.URL, item.Key
				break
			}
		}
	}
	s.mu.Unlock()
	if loadErr != nil {
		return "", "", "", loadErr
	}
	if key == "" && url == "" {
		return "", "", "", fmt.Errorf("%w: llm models provider was not found", ErrInvalidInput)
	}
	return protocol, url, key, nil
}

// fetchLLMModelList lists models over the OpenAI or Anthropic wire. Anthropic
// goes through the shared SDK client, the same one the desktop app uses, so a
// gateway that only accepts one auth header shape behaves the same here.
// OpenAI walks the endpoint candidates until one answers, because deployments
// disagree about whether the base URL already carries /v1.
func fetchLLMModelList(ctx context.Context, protocol, rawURL, key string) ([]LLMModelItem, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	// CodeGen is a hybrid gateway: it speaks Anthropic wire protocol for chat,
	// but its /v1/models endpoint only accepts OpenAI-style Bearer auth and
	// rejects the x-api-key + anthropic-version headers the SDK sends. Model
	// discovery overrides to OpenAI, the same rule the desktop app applies.
	if strings.EqualFold(protocol, "anthropic") && corelib.IsCodeGenURL(baseURL) {
		protocol = "openai"
	}
	if strings.EqualFold(protocol, "anthropic") {
		ctx, cancel := context.WithTimeout(ctx, llmModelFetchTimeout)
		defer cancel()
		models, err := corelibllm.ListAnthropicModelsWithSDK(ctx, corelib.MaclawLLMConfig{
			URL:      baseURL,
			Key:      key,
			Protocol: "anthropic",
		}, llmModelFetchClient)
		if err != nil {
			return nil, llmModelListError(err)
		}
		items := make([]LLMModelItem, 0, len(models))
		for _, model := range models {
			if id := strings.TrimSpace(model.ID); id != "" {
				items = append(items, LLMModelItem{ID: id, Name: firstNonEmpty(model.DisplayName, id)})
			}
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("服务商返回了空的模型列表")
		}
		return normalizeLLMModelItems(items), nil
	}

	candidates := corelibllm.BuildOpenAIModelsEndpointCandidates(baseURL, "openai")
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: llm provider url is invalid", ErrInvalidInput)
	}
	var lastErr error
	for _, endpoint := range candidates {
		items, status, err := llmModelListRequest(ctx, endpoint, key)
		if err != nil {
			lastErr = err
			continue
		}
		if status == http.StatusNotFound {
			lastErr = fmt.Errorf("模型列表端点不存在 (HTTP 404)，请检查接口地址是否正确")
			continue
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return nil, fmt.Errorf("%w: llm provider key is invalid (HTTP %d)", ErrInvalidInput, status)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("服务商返回 HTTP %d", status)
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("服务商返回了空的模型列表")
		}
		return normalizeLLMModelItems(items), nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("获取模型列表失败: %w", lastErr)
	}
	return nil, fmt.Errorf("获取模型列表失败")
}

// llmModelListError keeps the provider's own status line when it carried one,
// so an administrator can tell a rejected key from an unreachable host. A
// 401/403 from the provider means the credentials were refused, and the
// messageOf translation on the admin page keys off the "key is invalid" phrase.
func llmModelListError(err error) error {
	raw := strings.TrimSpace(err.Error())
	if raw == "" {
		return fmt.Errorf("获取模型列表失败")
	}
	if strings.Contains(raw, "HTTP 401") || strings.Contains(raw, "HTTP 403") {
		return fmt.Errorf("%w: llm provider key is invalid", ErrInvalidInput)
	}
	if strings.Contains(raw, "HTTP 404") {
		return fmt.Errorf("模型列表端点不存在 (HTTP 404)，请检查接口地址是否正确")
	}
	return fmt.Errorf("获取模型列表失败: %s", raw)
}

// llmModelListRequest performs one GET {endpoint} and parses the body. The
// status code travels with the parsed items because 404 is a signal to try
// the next endpoint candidate, not a terminal error.
func llmModelListRequest(ctx context.Context, endpoint, key string) ([]LLMModelItem, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "maclaw-hub")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := llmModelFetchClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, llmModelFetchLimit))
	if readErr != nil && len(body) == 0 {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", readErr)
	}
	return parseLLMModelListBody(body), resp.StatusCode, nil
}

// llmModelEntry tolerates the field spellings catalogs use: OpenAI sends id,
// Anthropic adds display_name, and some gateways use name.
type llmModelEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// parseLLMModelListBody accepts the three catalog shapes seen in the wild:
// {"data":[...]}, {"models":[...]}, and a bare array.
func parseLLMModelListBody(body []byte) []LLMModelItem {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] == '<' {
		return nil
	}
	var obj struct {
		Data   []llmModelEntry `json:"data"`
		Models []llmModelEntry `json:"models"`
	}
	if err := json.Unmarshal(trimmed, &obj); err == nil {
		if len(obj.Models) > 0 {
			return llmModelItemsFromEntries(obj.Models)
		}
		if len(obj.Data) > 0 {
			return llmModelItemsFromEntries(obj.Data)
		}
	}
	var arr []llmModelEntry
	if err := json.Unmarshal(trimmed, &arr); err == nil {
		return llmModelItemsFromEntries(arr)
	}
	return nil
}

func llmModelItemsFromEntries(entries []llmModelEntry) []LLMModelItem {
	items := make([]LLMModelItem, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" || strings.ContainsAny(id, " \r\n") {
			continue
		}
		items = append(items, LLMModelItem{ID: id, Name: firstNonEmpty(strings.TrimSpace(entry.DisplayName), strings.TrimSpace(entry.Name), id)})
	}
	return items
}

// normalizeLLMModelItems sorts by id and drops duplicates so a picker reads
// predictably instead of in provider-returned order. It returns a fresh
// slice; the caller's order and backing array stay untouched.
func normalizeLLMModelItems(items []LLMModelItem) []LLMModelItem {
	out := make([]LLMModelItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// firstNonEmpty returns the first value that is non-blank once trimmed.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
