package compute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// TestResult holds the outcome of a provider connectivity test.
type TestResult struct {
	Success bool          `json:"success"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"`
	Model   string        `json:"model,omitempty"`
}

// localStoreFile is the JSON structure persisted to disk.
type localStoreFile struct {
	Providers []ComputeProvider `json:"providers"`
}

// LocalStore manages local LLM provider configurations in a JSON file.
// It is safe for concurrent use.
type LocalStore struct {
	mu        sync.RWMutex
	filePath  string
	providers []ComputeProvider
}

// NewLocalStore creates a LocalStore that reads/writes the given JSON file.
// It loads existing providers from the file if it exists.
func NewLocalStore(filePath string) *LocalStore {
	ls := &LocalStore{filePath: filePath}
	_ = ls.Load() // best-effort load; file may not exist yet
	return ls
}

// Load reads providers from the JSON file. If the file does not exist,
// the provider list is left empty (not an error).
func (ls *LocalStore) Load() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	data, err := os.ReadFile(ls.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			ls.providers = nil
			return nil
		}
		return fmt.Errorf("read local store: %w", err)
	}

	var f localStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("decode local store: %w", err)
	}
	ls.providers = f.Providers
	return nil
}

// Save writes the current provider list to the JSON file.
func (ls *LocalStore) Save() error {
	ls.mu.RLock()
	f := localStoreFile{Providers: ls.providers}
	ls.mu.RUnlock()

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local store: %w", err)
	}
	if err := os.WriteFile(ls.filePath, data, 0644); err != nil {
		return fmt.Errorf("write local store: %w", err)
	}
	return nil
}

// ListProviders returns a copy of all stored providers.
func (ls *LocalStore) ListProviders() []ComputeProvider {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	out := make([]ComputeProvider, len(ls.providers))
	copy(out, ls.providers)
	return out
}

// GetProvider returns a pointer to the provider with the given ID, or nil.
func (ls *LocalStore) GetProvider(id string) *ComputeProvider {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	for i := range ls.providers {
		if ls.providers[i].ID == id {
			cp := ls.providers[i]
			return &cp
		}
	}
	return nil
}

// SaveProvider creates or updates a provider. If p.ID is empty a new ID is
// generated. The change is persisted to disk.
func (ls *LocalStore) SaveProvider(p ComputeProvider) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	if p.ID == "" {
		p.ID = fmt.Sprintf("local-%d", time.Now().UnixNano())
		p.CreatedAt = now
		p.UpdatedAt = now
		ls.providers = append(ls.providers, p)
	} else {
		found := false
		for i := range ls.providers {
			if ls.providers[i].ID == p.ID {
				p.UpdatedAt = now
				if p.CreatedAt == "" {
					p.CreatedAt = ls.providers[i].CreatedAt
				}
				ls.providers[i] = p
				found = true
				break
			}
		}
		if !found {
			p.CreatedAt = now
			p.UpdatedAt = now
			ls.providers = append(ls.providers, p)
		}
	}

	return ls.saveLocked()
}

// DeleteProvider removes the provider with the given ID and persists the change.
func (ls *LocalStore) DeleteProvider(id string) error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	idx := -1
	for i := range ls.providers {
		if ls.providers[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("provider %q not found", id)
	}

	ls.providers = append(ls.providers[:idx], ls.providers[idx+1:]...)
	return ls.saveLocked()
}

// saveLocked writes to disk. Caller must hold ls.mu.
func (ls *LocalStore) saveLocked() error {
	f := localStoreFile{Providers: ls.providers}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local store: %w", err)
	}
	if err := os.WriteFile(ls.filePath, data, 0644); err != nil {
		return fmt.Errorf("write local store: %w", err)
	}
	return nil
}

// ---------- Connectivity tester ----------

// TestComputeProvider sends a simple "Hello" prompt to the provider and
// reports whether it is reachable. The logic mirrors iWorkerCloud's tester.
func TestComputeProvider(p *ComputeProvider) TestResult {
	client := &http.Client{Timeout: 30 * time.Second}

	start := time.Now()
	req, err := buildTestRequest(p)
	if err != nil {
		return TestResult{Error: fmt.Sprintf("build request: %s", err)}
	}

	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return TestResult{Error: fmt.Sprintf("request failed: %s", err), Latency: latency}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return TestResult{
			Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg),
			Latency: latency,
		}
	}

	model := extractModelFromResponse(p.Protocol, body)
	if model == "" {
		model = p.Model
	}

	return TestResult{Success: true, Latency: latency, Model: model}
}

func buildTestRequest(p *ComputeProvider) (*http.Request, error) {
	switch p.Protocol {
	case "openai":
		return buildOpenAITestRequest(p)
	case "anthropic":
		return buildAnthropicTestRequest(p)
	case "gemini":
		return buildGeminiTestRequest(p)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
}

func buildOpenAITestRequest(p *ComputeProvider) (*http.Request, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	model := p.Model
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	payload := map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "Hello"}},
		"max_tokens": 5,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	return req, nil
}

func buildAnthropicTestRequest(p *ComputeProvider) (*http.Request, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/messages"
	model := p.Model
	if model == "" {
		model = "claude-3-haiku-20240307"
	}
	payload := map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "Hello"}},
		"max_tokens": 5,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	return req, nil
}

func buildGeminiTestRequest(p *ComputeProvider) (*http.Request, error) {
	model := p.Model
	if model == "" {
		model = "gemini-pro"
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/models/" + model + ":generateContent?key=" + p.APIKey
	payload := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": "Hello"}}},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	return req, nil
}

func extractModelFromResponse(protocol string, body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	switch protocol {
	case "openai", "anthropic":
		if m, ok := data["model"].(string); ok {
			return m
		}
	case "gemini":
		if m, ok := data["modelVersion"].(string); ok {
			return m
		}
	}
	return ""
}
