package cloudagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	officialChatPath = "/api/internal/cloud-agents/llm/chat"
	officialKey      = "cloud-official"
)

type instanceCtxKey struct{}

func withInstance(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, instanceCtxKey{}, id)
}

func instanceFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(instanceCtxKey{}).(string)
	return id
}

// CustomLLM is stored only in llm.json on this machine.
type CustomLLM struct {
	URL   string `json:"url"`
	Key   string `json:"key"`
	Model string `json:"model"`
	Label string `json:"label"`
}

func (rt *Runtime) newHTTPClient() *http.Client {
	return &http.Client{Transport: &hubTransport{rt: rt, next: http.DefaultTransport}}
}

type hubTransport struct {
	rt   *Runtime
	next http.RoundTripper
}

func (t *hubTransport) base() http.RoundTripper {
	if t.next != nil {
		return t.next
	}
	return http.DefaultTransport
}

// RoundTrip sends an official turn to this Hub's loopback chat route and
// strips the placeholder bearer token. A custom endpoint is left unchanged.
func (t *hubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id := instanceFrom(req.Context())
	if id == "" {
		return t.base().RoundTrip(req)
	}
	inst, err := t.rt.instance(id)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	if clone == nil {
		return nil, errors.New("云端模型请求无效")
	}
	if inst.meta.LLMMode == llmCustom {
		clone.Header.Del("X-MaClaw-Admin-Secret")
		return t.base().RoundTrip(clone)
	}
	if err := officialReady(inst.meta); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(strings.TrimRight(inst.meta.HubLoopback, "/") + officialChatPath)
	if err != nil {
		return nil, err
	}
	clone.URL = endpoint
	clone.Host = endpoint.Host
	clone.Header.Del("Authorization")
	clone.Header.Set("X-MaClaw-Admin-Secret", t.rt.adminSecret)
	clone.Header.Set("X-MaClaw-Cloud-Agent", inst.meta.ID)
	return t.base().RoundTrip(clone)
}

func officialReady(meta Spec) error {
	if !sameOrigin(os.Getenv("MACLAW_HUB_URL"), meta.HubOrigin) || !loopbackURL(meta.HubLoopback) {
		return errors.New("云端官方模型未接通")
	}
	return nil
}

func sameOrigin(a, b string) bool {
	pa, errA := url.Parse(strings.TrimSpace(a))
	pb, errB := url.Parse(strings.TrimSpace(b))
	if errA != nil || errB != nil || pa.Host == "" || pb.Host == "" {
		return false
	}
	return strings.EqualFold(pa.Scheme, pb.Scheme) && strings.EqualFold(pa.Host, pb.Host)
}

func loopbackURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.Trim(parsed.Hostname(), "."))
	return host == "127.0.0.1" || host == "::1"
}

// LoopbackRemote reports whether a request came from the loopback interface.
// An empty address is not loopback.
func LoopbackRemote(remote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "127.0.0.1" || host == "::1"
}

func (rt *Runtime) llmConfig(inst *instance) (corelib.AppConfig, error) {
	if inst.meta.LLMMode == llmCustom {
		secret, err := readLLM(inst.dir)
		if err != nil {
			return corelib.AppConfig{}, err
		}
		if rejectsHubLLM(secret.URL, inst.meta) {
			return corelib.AppConfig{}, errors.New("自定义模型不能指向本 Hub 的官方接口")
		}
		model := strings.TrimSpace(secret.Model)
		if model == "" {
			model = strings.TrimSpace(inst.meta.LLMModel)
		}
		if model == "" {
			model = "auto"
		}
		return corelib.AppConfig{
			MaclawLLMUrl:      secret.URL,
			MaclawLLMKey:      secret.Key,
			MaclawLLMModel:    model,
			MaclawLLMProtocol: "openai",
		}, nil
	}
	if err := officialReady(inst.meta); err != nil {
		return corelib.AppConfig{}, err
	}
	model := strings.TrimSpace(inst.meta.LLMModel)
	if model == "" {
		model = "auto"
	}
	// Official turns speak the Responses API. Hub's loopback chat route
	// reuses LLMV1ResponsesHandler, which rejects a chat-completions body.
	return corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "hub-llm",
			URL:      strings.TrimRight(inst.meta.HubLoopback, "/"),
			Key:      officialKey,
			Model:    model,
			Protocol: "openai",
			WireAPI:  "responses",
		}},
	}, nil
}

func readLLM(dir string) (CustomLLM, error) {
	raw, err := os.ReadFile(filepathJoinLLM(dir))
	if err != nil {
		return CustomLLM{}, errors.New("还没有配置自定义模型")
	}
	var secret CustomLLM
	if err := json.Unmarshal(raw, &secret); err != nil {
		return CustomLLM{}, err
	}
	if strings.TrimSpace(secret.URL) == "" || strings.TrimSpace(secret.Key) == "" {
		return CustomLLM{}, errors.New("自定义模型不完整")
	}
	return secret, nil
}

func filepathJoinLLM(dir string) string {
	return filepath.Join(dir, "llm.json")
}

// SaveLLM writes llm.json and rejects a URL that points at this Hub.
func (rt *Runtime) SaveLLM(id string, in CustomLLM) error {
	inst, err := rt.instance(id)
	if err != nil {
		return err
	}
	in.URL = strings.TrimSpace(in.URL)
	in.Key = strings.TrimSpace(in.Key)
	in.Model = strings.TrimSpace(in.Model)
	if in.URL == "" || in.Key == "" {
		return errors.New("请填写模型地址和密钥")
	}
	parsed, err := url.Parse(in.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("模型地址无效")
	}
	if rejectsHubLLM(in.URL, inst.meta) {
		return errors.New("自定义模型不能指向本 Hub 的官方接口")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if err := writeFile(filepathJoinLLM(inst.dir), raw, 0o600); err != nil {
		return err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	inst.meta.LLMMode = llmCustom
	if in.Model != "" {
		inst.meta.LLMModel = in.Model
	}
	return writeMeta(inst.dir, inst.meta)
}

func rejectsHubLLM(raw string, meta Spec) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if path != "/api/llm/v1" && !strings.HasPrefix(path, "/api/llm/v1/") {
		return false
	}
	host := strings.ToLower(strings.Trim(parsed.Hostname(), "."))
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	return host != "" && (host == hostOnly(meta.HubOrigin) || host == hostOnly(meta.HubLoopback))
}

func hostOnly(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil {
			value = parsed.Host
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.ToLower(strings.Trim(value, "[]"))
}

var heartbeatHTTP = &http.Client{Timeout: 5 * time.Second}

// postHeartbeat reports whether Hub accepted the beat and the feature is still on.
// A failed POST, a non-loopback hub, or a body without enabled does not count as a beat.
func (rt *Runtime) postHeartbeat(ctx context.Context, inst *instance, taskID, requestID string, finished bool) (enabled bool, ok bool) {
	if inst == nil || !loopbackURL(inst.meta.HubLoopback) {
		return false, false
	}
	payload, err := json.Marshal(map[string]any{
		"instance_id": inst.meta.ID,
		"task_id":     taskID,
		"request_id":  requestID,
		"finished":    finished,
	})
	if err != nil {
		return false, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(inst.meta.HubLoopback, "/")+"/api/internal/cloud-agents/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return false, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", rt.adminSecret)
	resp, err := heartbeatHTTP.Do(req)
	if err != nil {
		return false, false
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var body struct {
		OK      bool  `json:"ok"`
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || !body.OK || body.Enabled == nil {
		return false, false
	}
	return *body.Enabled, true
}
