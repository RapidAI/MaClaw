package corelib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OpenAIProxyConfig holds the upstream LLM configuration for the proxy.
type OpenAIProxyConfig struct {
	URL       string // upstream base URL (e.g. "https://open.bigmodel.cn/api/anthropic")
	Key       string // upstream API key
	Model     string // model name to use
	Protocol  string // "" or "openai" or "anthropic"
	WireAPI   string // "" or "chat" or "responses" or "responses-ws"
	AgentType string // optional User-Agent/client identity
	AuthType  string // optional auth kind from provider config
	// ThinkingMode and ReasoningEffort are copied from the selected LLM config
	// so the proxy cannot bypass the user's global reasoning preference.
	ThinkingMode    string
	ReasoningEffort string
	// UsageCallback receives successful request token usage for local accounting.
	UsageCallback func(OpenAIProxyUsage)
	// UsageCallbackSync runs UsageCallback on the request path. The default
	// async mode keeps skill proxy responses independent from statistics I/O.
	UsageCallbackSync bool
}

// OpenAIProxyUsage is the token usage observed by the skill-local OpenAI proxy.
type OpenAIProxyUsage struct {
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	CacheWriteTokens  int
	Estimated         bool
}

// OpenAIProxy is a local HTTP proxy that provides an OpenAI-compatible
// /v1/chat/completions endpoint, forwarding requests to the configured
// upstream LLM provider with protocol conversion as needed.
type OpenAIProxy struct {
	config   OpenAIProxyConfig
	server   *http.Server
	listener net.Listener
	port     int
	client   *http.Client
}

// NewOpenAIProxy creates a new proxy instance with the given config.
func NewOpenAIProxy(cfg OpenAIProxyConfig) *OpenAIProxy {
	return &OpenAIProxy{
		config: cfg,
		client: &http.Client{
			Timeout: time.Duration(DefaultLLMTimeoutSec) * time.Second,
		},
	}
}

// ValidateOpenAIProxyUpstreamConfig reports whether a skill-local proxy can
// safely start with the resolved upstream provider. Remote authenticated
// providers need a usable key/token; explicit no-auth or loopback providers are
// allowed for local model servers.
func ValidateOpenAIProxyUpstreamConfig(cfg OpenAIProxyConfig) error {
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("no LLM provider URL/model is configured")
	}
	if strings.TrimSpace(cfg.Key) != "" {
		return nil
	}
	authType := strings.ToLower(strings.TrimSpace(cfg.AuthType))
	if authType == "none" {
		return nil
	}
	if isLoopbackOpenAIProxyURL(cfg.URL) {
		return nil
	}
	switch authType {
	case "oauth", "sso", "api_key", "bearer":
		return fmt.Errorf("selected LLM provider requires authentication but no API key/token is available")
	}
	return fmt.Errorf("selected remote LLM provider has no API key/token; set auth_type=none only for no-auth local providers")
}

func isLoopbackOpenAIProxyURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Start binds to a random port on 127.0.0.1 and begins serving.
// Returns the allocated port number or an error.
func (p *OpenAIProxy) Start() (int, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleChatCompletions)

	var err error
	p.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("openai proxy: listen: %w", err)
	}

	p.port = p.listener.Addr().(*net.TCPAddr).Port
	p.server = &http.Server{Handler: mux}

	log.Printf("[openai-proxy] listening on 127.0.0.1:%d (upstream: %s, protocol: %s, model: %s)",
		p.port, p.config.URL, p.config.Protocol, p.config.Model)

	go func() {
		if err := p.server.Serve(p.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[openai-proxy] serve error: %v", err)
		}
	}()

	return p.port, nil
}

// Stop gracefully shuts down the proxy server with a 5-second deadline.
func (p *OpenAIProxy) Stop() error {
	if p.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log.Printf("[openai-proxy] shutting down on port %d", p.port)
	return p.server.Shutdown(ctx)
}

// Port returns the port the proxy is listening on.
func (p *OpenAIProxy) Port() int {
	return p.port
}

// NeedsOpenAIProxy determines if a skill needs the local OpenAI proxy.
// Returns true if required_env contains OPENAI_API_KEY and the user
// has not provided OPENAI_API_KEY via extra_env.
func NeedsOpenAIProxy(requiredEnv []string, extraEnv map[string]string) bool {
	if !declaresOpenAIAPIKey(requiredEnv) {
		return false
	}

	return !hasUserProvidedOpenAIAPIKey(extraEnv)
}

// NeedsOpenAIProxyAuto is like NeedsOpenAIProxy but also auto-detects
// OpenAI env var usage from skill step commands and script files when
// RequiredEnv is not explicitly declared. This handles skills downloaded
// from ClawHub or other sources that use OPENAI_API_KEY in their scripts
// but don't declare requires_env in their metadata.
//
// Detection order:
//  1. RequiredEnv explicitly declares OPENAI_API_KEY/OPENAI_BASE_URL.
//  2. Step commands reference OPENAI_API_KEY/OPENAI_BASE_URL.
//  3. Script files (.py, .js, .ts, .sh) in skillDir reference them.
//
// Explicit required_env entries are satisfied per variable. Passive script
// detection remains conservative: any user-provided OpenAI env disables the
// proxy so caller-supplied endpoints are not overwritten.
func NeedsOpenAIProxyAuto(requiredEnv []string, extraEnv map[string]string, steps []NLSkillStep, skillDir string) bool {
	// Check if the skill or any executable step explicitly declares
	// OPENAI env requirements. Step-level required_env is common in imported skills.
	// Checked before the process-level env so that a stale
	// "sk-maclaw-local-proxy" sentinel in os env doesn't prevent
	// proxy startup for skills that genuinely need it.
	explicitUsage := openAIEnvUsage{
		apiKey:  declaresOpenAIAPIKey(requiredEnv) || stepsDeclareOpenAIAPIKey(steps),
		baseURL: declaresOpenAIBaseURL(requiredEnv) || stepsDeclareOpenAIBaseURL(steps),
	}
	if explicitUsage.any() {
		apiKeySatisfied := !explicitUsage.apiKey || hasUserProvidedOpenAIAPIKey(extraEnv) || hasProcessOpenAIAPIKey(extraEnv)
		baseURLSatisfied := !explicitUsage.baseURL || hasUserProvidedOpenAIBaseURL(extraEnv) || hasProcessOpenAIBaseURL(extraEnv)
		return !(apiKeySatisfied && baseURLSatisfied)
	}

	var detectedUsage openAIEnvUsage
	// Layer 2: scan executable step command/code fields for env var references.
	for _, step := range steps {
		if !isOpenAIProbeCommandAction(step.Action) {
			continue
		}
		for _, text := range openAIProbeStepTexts(step.Params) {
			detected := detectOpenAIEnvUsage(text)
			if detected.any() {
				log.Printf("[openai-proxy] auto-detected OPENAI env usage in step command")
				detectedUsage = detectedUsage.merge(detected)
			}
		}
	}

	// Layer 3: scan script files in skillDir
	if skillDir != "" {
		detected := scanSkillDirForOpenAIEnvUsage(skillDir)
		if detected.any() {
			log.Printf("[openai-proxy] auto-detected OPENAI env usage in skill scripts at %s", skillDir)
			detectedUsage = detectedUsage.merge(detected)
		}
	}

	if !detectedUsage.any() {
		return false
	}
	if hasUserProvidedOpenAIEnv(extraEnv) || hasProcessOpenAIAPIKey(extraEnv) || hasProcessOpenAIBaseURL(extraEnv) {
		return false
	}
	return true
}

func declaresOpenAIAPIKey(requiredEnv []string) bool {
	for _, env := range requiredEnv {
		if isOpenAIAPIKeyEnvName(env) {
			return true
		}
	}
	return false
}

func stepsDeclareOpenAIAPIKey(steps []NLSkillStep) bool {
	for _, step := range steps {
		for _, env := range stepRequiredEnvNames(step.Params) {
			if isOpenAIAPIKeyEnvName(env) {
				return true
			}
		}
	}
	return false
}

func declaresOpenAIBaseURL(requiredEnv []string) bool {
	for _, env := range requiredEnv {
		if isOpenAIBaseURLEnvName(env) {
			return true
		}
	}
	return false
}

func stepsDeclareOpenAIBaseURL(steps []NLSkillStep) bool {
	for _, step := range steps {
		for _, env := range stepRequiredEnvNames(step.Params) {
			if isOpenAIBaseURLEnvName(env) {
				return true
			}
		}
	}
	return false
}

func isOpenAIAPIKeyEnvName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "OPENAI_API_KEY")
}

func isOpenAIBaseURLEnvName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "OPENAI_BASE_URL")
}

func stepRequiredEnvNames(params map[string]interface{}) []string {
	for _, key := range []string{"required_env", "requires_env", "required_environment"} {
		if raw, ok := params[key]; ok {
			return stringListFromAny(raw)
		}
	}
	return nil
}

func stringListFromAny(raw interface{}) []string {
	var result []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			add(fmt.Sprintf("%v", item))
		}
	case []string:
		for _, item := range v {
			add(item)
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			add(item)
		}
	}
	return result
}

func isOpenAIProbeCommandAction(action string) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "-", "_")
	action = strings.ReplaceAll(action, " ", "_")
	switch action {
	case "", "bash", "run", "exec", "execute", "command", "shell", "sh", "cmd", "script",
		"python", "python3", "node", "js", "javascript", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func openAIProbeStepTexts(params map[string]interface{}) []string {
	var texts []string
	for _, key := range []string{"command", "cmd", "run", "script", "shell_command", "code", "source"} {
		if text, ok := params[key].(string); ok && strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

type openAIEnvUsage struct {
	apiKey  bool
	baseURL bool
}

func (u openAIEnvUsage) any() bool {
	return u.apiKey || u.baseURL
}

func (u openAIEnvUsage) merge(other openAIEnvUsage) openAIEnvUsage {
	u.apiKey = u.apiKey || other.apiKey
	u.baseURL = u.baseURL || other.baseURL
	return u
}

// hasUserProvidedOpenAIEnv checks if the user has provided OPENAI_API_KEY
// or OPENAI_BASE_URL via extraEnv with non-empty values.
func hasUserProvidedOpenAIEnv(extraEnv map[string]string) bool {
	return hasUserProvidedOpenAIAPIKey(extraEnv) || hasUserProvidedOpenAIBaseURL(extraEnv)
}

func hasUserProvidedOpenAIAPIKey(extraEnv map[string]string) bool {
	if v, ok := lookupExtraEnvName(extraEnv, "OPENAI_API_KEY"); ok && strings.TrimSpace(v) != "" {
		return true
	}
	return false
}

func hasUserProvidedOpenAIBaseURL(extraEnv map[string]string) bool {
	if v, ok := lookupExtraEnvName(extraEnv, "OPENAI_BASE_URL"); ok && strings.TrimSpace(v) != "" {
		return true
	}
	return false
}

func hasProcessOpenAIAPIKey(extraEnv map[string]string) bool {
	if _, explicitlyOverridden := lookupExtraEnvName(extraEnv, "OPENAI_API_KEY"); explicitlyOverridden {
		return false
	}
	v := os.Getenv("OPENAI_API_KEY")
	if v == "" {
		return false
	}
	if v == "sk-maclaw-local-proxy" {
		log.Printf("[openai-proxy] ignoring stale process env OPENAI_API_KEY=sk-maclaw-local-proxy")
		return false
	}
	return true
}

func hasProcessOpenAIBaseURL(extraEnv map[string]string) bool {
	if _, explicitlyOverridden := lookupExtraEnvName(extraEnv, "OPENAI_BASE_URL"); explicitlyOverridden {
		return false
	}
	return strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")) != ""
}

func lookupExtraEnvName(extraEnv map[string]string, name string) (string, bool) {
	for key, value := range extraEnv {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return value, true
		}
	}
	return "", false
}

// containsOpenAIEnvRef checks if text contains references to OpenAI env vars.
func containsOpenAIEnvRef(text string) bool {
	return detectOpenAIEnvUsage(text).any()
}

func detectOpenAIEnvUsage(text string) openAIEnvUsage {
	upper := strings.ToUpper(text)
	return openAIEnvUsage{
		apiKey:  strings.Contains(upper, "OPENAI_API_KEY"),
		baseURL: strings.Contains(upper, "OPENAI_BASE_URL"),
	}
}

// scriptExtensions are file extensions that may contain env var references.
var scriptExtensions = map[string]bool{
	".py": true, ".js": true, ".ts": true, ".sh": true,
	".rb": true, ".pl": true, ".php": true, ".go": true,
}

// scanSkillDirForOpenAIEnv scans script files in skillDir for OPENAI_API_KEY
// or OPENAI_BASE_URL references. Scans the top-level directory and one level
// of common subdirectories (scripts/, src/, lib/). Safety limits: max 30 files,
// max 64KB each.
func scanSkillDirForOpenAIEnv(skillDir string) bool {
	return scanSkillDirForOpenAIEnvUsage(skillDir).any()
}

func scanSkillDirForOpenAIEnvUsage(skillDir string) openAIEnvUsage {
	// Directories to scan: top-level + common script subdirectories
	dirsToScan := []string{skillDir}
	for _, sub := range []string{"scripts", "src", "lib"} {
		subDir := filepath.Join(skillDir, sub)
		if info, err := os.Stat(subDir); err == nil && info.IsDir() {
			dirsToScan = append(dirsToScan, subDir)
		}
	}

	scanned := 0
	var usage openAIEnvUsage
	for _, dir := range dirsToScan {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if !scriptExtensions[ext] {
				continue
			}
			scanned++
			if scanned > 30 {
				return usage // safety limit
			}

			info, err := entry.Info()
			if err != nil || info.Size() > 64*1024 {
				continue // skip large files
			}

			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			usage = usage.merge(detectOpenAIEnvUsage(string(data)))
		}
	}
	return usage
}

// routeProtocol determines which upstream protocol to use based on config.
// Returns "anthropic", "responses", or "openai" (default).
func (p *OpenAIProxy) routeProtocol() string {
	if strings.EqualFold(p.config.Protocol, "anthropic") {
		return "anthropic"
	}
	w := strings.ToLower(strings.TrimSpace(p.config.WireAPI))
	if w == "responses" || w == "responses-ws" {
		return "responses"
	}
	return "openai"
}

// handleChatCompletions is the main HTTP handler for the proxy.
// It validates the request path and method, parses the JSON body,
// and routes to the appropriate forward function based on protocol.
func (p *OpenAIProxy) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if isOpenAIProxyResponsesPath(r.URL.Path) {
		p.handleResponses(w, r)
		return
	}

	// 1. Validate path. Accept both OpenAI's canonical /v1 path and the
	// suffix used by simple scripts that append to OPENAI_BASE_URL themselves.
	if !isOpenAIProxyChatCompletionsPath(r.URL.Path) {
		if p.handleModels(w, r) {
			return
		}
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Not Found",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 2. Validate method
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Method Not Allowed",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 3. Read and parse body (limit to 10MB to prevent OOM from malicious input)
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("invalid JSON: %s", err.Error()),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("invalid JSON: %s", err.Error()),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 4. Route based on protocol
	protocol := p.routeProtocol()
	originalModel, _ := body["model"].(string) // capture before forward mutates body
	var respBody []byte
	var statusCode int
	var forwardErr error

	switch protocol {
	case "anthropic":
		respBody, statusCode, forwardErr = p.forwardAnthropic(body)
	case "responses":
		respBody, statusCode, forwardErr = p.forwardResponses(body)
	default:
		respBody, statusCode, forwardErr = p.forwardOpenAI(body)
	}

	// 5. Handle forward error
	if forwardErr != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream provider unreachable: %s", forwardErr.Error()),
				"type":    "server_error",
			},
		})
		return
	}

	// 6. Write response
	w.WriteHeader(statusCode)
	w.Write(respBody)
	p.recordUsage(body, respBody, statusCode)

	// 7. Log at debug level
	log.Printf("[openai-proxy] protocol=%s url=%s model=%s status=%d", protocol, p.config.URL, originalModel, statusCode)
}

func isOpenAIProxyChatCompletionsPath(path string) bool {
	path = strings.TrimRight(path, "/")
	return path == "/v1/chat/completions" || path == "/chat/completions"
}

func isOpenAIProxyResponsesPath(path string) bool {
	path = strings.TrimRight(path, "/")
	return path == "/v1/responses" || path == "/responses"
}

func (p *OpenAIProxy) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Method Not Allowed",
				"type":    "invalid_request_error",
			},
		})
		return
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("invalid JSON: %s", err.Error()),
				"type":    "invalid_request_error",
			},
		})
		return
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("invalid JSON: %s", err.Error()),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	var respBody []byte
	var statusCode int
	var forwardErr error
	if p.routeProtocol() == "responses" {
		respBody, statusCode, forwardErr = p.forwardResponsesRaw(body)
	} else {
		chatBody, model, err := openAIProxyResponsesRequestToChat(body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "convert responses request: " + err.Error(),
					"type":    "invalid_request_error",
				},
			})
			return
		}
		switch p.routeProtocol() {
		case "anthropic":
			respBody, statusCode, forwardErr = p.forwardAnthropic(chatBody)
		default:
			respBody, statusCode, forwardErr = p.forwardOpenAI(chatBody)
		}
		if forwardErr == nil && statusCode < 400 {
			respBody, forwardErr = openAIProxyChatResponseToResponses(respBody, model)
		}
	}
	if forwardErr != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream provider unreachable: %s", forwardErr.Error()),
				"type":    "server_error",
			},
		})
		return
	}
	w.WriteHeader(statusCode)
	w.Write(respBody)
	p.recordUsage(body, respBody, statusCode)
}

func (p *OpenAIProxy) recordUsage(reqBody map[string]interface{}, respBody []byte, statusCode int) {
	if p == nil || p.config.UsageCallback == nil || statusCode >= http.StatusBadRequest {
		return
	}
	usage := openAIProxyUsageFromResponse(reqBody, respBody)
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CachedInputTokens == 0 && usage.CacheWriteTokens == 0 {
		return
	}
	callback := p.config.UsageCallback
	if p.config.UsageCallbackSync {
		callOpenAIProxyUsageCallback(callback, usage)
		return
	}
	go callOpenAIProxyUsageCallback(callback, usage)
}

func callOpenAIProxyUsageCallback(callback func(OpenAIProxyUsage), usage OpenAIProxyUsage) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[openai-proxy] usage callback panic: %v", rec)
		}
	}()
	callback(usage)
}

func openAIProxyUsageFromResponse(reqBody map[string]interface{}, respBody []byte) OpenAIProxyUsage {
	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return OpenAIProxyUsage{
			InputTokens:  EstimateTextTokens(openAIProxyFlattenText(reqBody)),
			OutputTokens: EstimateTextTokens(string(respBody)),
			Estimated:    true,
		}
	}
	usage := mapFromAny(payload["usage"])
	var out OpenAIProxyUsage
	derivedFromTotalOnly := false
	if usage != nil {
		out.InputTokens = int(firstPositiveInt64(
			numberToInt64(usage["prompt_tokens"]),
			numberToInt64(usage["input_tokens"]),
		))
		out.OutputTokens = int(firstPositiveInt64(
			numberToInt64(usage["completion_tokens"]),
			numberToInt64(usage["output_tokens"]),
		))
		totalTokens := numberToInt64(usage["total_tokens"])
		if totalTokens > 0 {
			switch {
			case out.InputTokens == 0 && out.OutputTokens == 0:
				out.InputTokens = int(totalTokens)
				derivedFromTotalOnly = true
			case out.InputTokens == 0 && totalTokens > int64(out.OutputTokens):
				out.InputTokens = int(totalTokens) - out.OutputTokens
			case out.OutputTokens == 0 && totalTokens > int64(out.InputTokens):
				out.OutputTokens = int(totalTokens) - out.InputTokens
			}
		}
		out.CachedInputTokens = openAIProxyCachedInputTokens(usage)
		out.CacheWriteTokens = openAIProxyCacheWriteTokens(usage)
	}
	if !derivedFromTotalOnly && (out.InputTokens == 0 || out.OutputTokens == 0) {
		estimatedInput := EstimateTextTokens(openAIProxyFlattenText(reqBody))
		estimatedOutput := EstimateTextTokens(openAIProxyResponseText(payload))
		if out.InputTokens == 0 {
			out.InputTokens = estimatedInput
			out.Estimated = out.Estimated || estimatedInput > 0
		}
		if out.OutputTokens == 0 {
			out.OutputTokens = estimatedOutput
			out.Estimated = out.Estimated || estimatedOutput > 0
		}
	}
	return out
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func openAIProxyCachedInputTokens(usage map[string]interface{}) int {
	if usage == nil {
		return 0
	}
	if v := firstPositiveInt64(
		numberToInt64(usage["cached_input_tokens"]),
		numberToInt64(usage["cache_read_input_tokens"]),
	); v > 0 {
		return int(v)
	}
	for _, key := range []string{"prompt_tokens_details", "input_tokens_details"} {
		if details := mapFromAny(usage[key]); details != nil {
			if v := firstPositiveInt64(
				numberToInt64(details["cached_tokens"]),
				numberToInt64(details["cached_input_tokens"]),
			); v > 0 {
				return int(v)
			}
		}
	}
	return 0
}

func openAIProxyCacheWriteTokens(usage map[string]interface{}) int {
	if usage == nil {
		return 0
	}
	if v := firstPositiveInt64(
		numberToInt64(usage["cache_write_tokens"]),
		numberToInt64(usage["cache_creation_input_tokens"]),
		numberToInt64(usage["cache_creation_tokens"]),
	); v > 0 {
		return int(v)
	}
	for _, key := range []string{"input_tokens_details", "prompt_tokens_details"} {
		if details := mapFromAny(usage[key]); details != nil {
			if v := firstPositiveInt64(
				numberToInt64(details["cache_write_tokens"]),
				numberToInt64(details["cache_creation_input_tokens"]),
				numberToInt64(details["cache_creation_tokens"]),
			); v > 0 {
				return int(v)
			}
		}
	}
	return 0
}

func openAIProxyFlattenText(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case []interface{}:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, openAIProxyFlattenText(item))
		}
		return strings.Join(parts, " ")
	case map[string]interface{}:
		parts := make([]string, 0, len(val))
		for _, key := range []string{"messages", "input", "instructions", "tools", "tool_choice", "response_format", "text", "content", "output", "arguments", "name", "description", "function", "parameters", "schema"} {
			parts = append(parts, openAIProxyFlattenText(val[key]))
		}
		joined := strings.Join(parts, " ")
		if strings.TrimSpace(joined) == "" {
			data, _ := json.Marshal(val)
			return string(data)
		}
		return joined
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

func openAIProxyResponseText(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	var parts []string
	for _, choice := range openAICompatForwardSlice(payload["choices"]) {
		m := mapFromAny(choice)
		if m == nil {
			continue
		}
		if message := mapFromAny(m["message"]); message != nil {
			parts = append(parts, openAIProxyFlattenText(message["content"]))
			parts = append(parts, openAIProxyFlattenText(message["tool_calls"]))
			parts = append(parts, openAIProxyFlattenText(message["function_call"]))
		}
		parts = append(parts, openAIProxyFlattenText(m["text"]))
	}
	parts = append(parts, openAIProxyFlattenText(payload["output"]))
	return strings.Join(parts, " ")
}

func (p *OpenAIProxy) handleModels(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimRight(r.URL.Path, "/")
	if path != "/v1/models" && path != "/models" && !strings.HasPrefix(path, "/v1/models/") && !strings.HasPrefix(path, "/models/") {
		return false
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Method Not Allowed",
				"type":    "invalid_request_error",
			},
		})
		return true
	}
	model := strings.TrimSpace(p.config.Model)
	if model == "" {
		model = "maclaw-local-proxy"
	}
	if strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/models/") {
		requested := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(path, "/v1/models/"), "/models/"))
		if requested != "" {
			model = requested
		}
		json.NewEncoder(w).Encode(openAIProxyModelObject(model))
		return true
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   []interface{}{openAIProxyModelObject(model)},
	})
	return true
}

func openAIProxyModelObject(model string) map[string]interface{} {
	return map[string]interface{}{
		"id":       model,
		"object":   "model",
		"created":  int64(0),
		"owned_by": "maclaw-local-proxy",
	}
}

// forwardOpenAI forwards the request directly to an OpenAI-compatible upstream.
// It replaces the model field, forces stream: false, and forwards the response as-is.
func (p *OpenAIProxy) forwardOpenAI(body map[string]interface{}) ([]byte, int, error) {
	// Work on a shallow copy to avoid mutating the caller's map
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}

	cfg := p.maclawLLMConfig()
	respBody, statusCode, err := ForwardOpenAICompatRequest(context.Background(), cfg, fwd, p.client, "")
	if err != nil && openAICompatSDKError(err) != nil {
		respBody = normalizeOpenAIProxyErrorBody(respBody)
		return respBody, statusCode, nil
	}
	return respBody, statusCode, err
}

func normalizeOpenAIProxyErrorBody(body []byte) []byte {
	var payload map[string]interface{}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return body
	}
	if _, ok := payload["error"]; ok {
		return body
	}
	message, _ := payload["message"].(string)
	if strings.TrimSpace(message) == "" {
		return body
	}
	errType, _ := payload["type"].(string)
	if strings.TrimSpace(errType) == "" {
		errType = "server_error"
	}
	normalized, err := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
		},
	})
	if err != nil {
		return body
	}
	return normalized
}

// openaiToAnthropic converts an OpenAI Chat Completions request body
// to an Anthropic Messages API request body.
func openaiToAnthropic(body map[string]interface{}, model string) map[string]interface{} {
	body = cloneOpenAICompatBody(body)
	sanitizeOpenAICompatForwardBodyWithOptions(MaclawLLMConfig{}, body, false, false)

	anthropicReq := map[string]interface{}{
		"model":  model,
		"stream": false,
	}

	// Extract max_tokens from body, default to a larger budget for tool loops.
	if mt, ok := body["max_tokens"]; ok && mt != nil {
		anthropicReq["max_tokens"] = mt
	} else if len(openAICompatForwardSlice(body["tools"])) > 0 {
		anthropicReq["max_tokens"] = 8192
	} else {
		anthropicReq["max_tokens"] = 4096
	}

	messages := openAICompatForwardMessageSlice(body["messages"])
	converted := convertOpenAIToAnthropicMessages(messages)
	if converted.SystemText != "" {
		anthropicReq["system"] = converted.SystemText
	}
	if converted.Messages == nil {
		converted.Messages = []interface{}{}
	}
	anthropicReq["messages"] = converted.Messages
	if tools := openAICompatForwardSlice(body["tools"]); len(tools) > 0 {
		anthropicReq["tools"] = convertAnthropicToolsAny(tools)
	}
	if thinking := mapFromAny(body["thinking"]); thinking != nil && strings.EqualFold(strings.TrimSpace(fmt.Sprint(thinking["type"])), "enabled") {
		// Forwarded requests that select the Anthropic protocol need the native
		// extended-thinking block too. The plain converter has no access to a
		// provider config, so carry through the already-normalized intent here.
		budget := 4096
		if raw, ok := thinking["budget_tokens"].(int); ok && raw > 0 {
			budget = raw
		}
		anthropicReq["thinking"] = map[string]interface{}{"type": "enabled", "budget_tokens": budget}
		if maxTokens, ok := anthropicReq["max_tokens"].(int); ok && maxTokens > 1 && budget >= maxTokens {
			thinkingReq := anthropicReq["thinking"].(map[string]interface{})
			thinkingReq["budget_tokens"] = maxTokens - 1
		}
	}

	return anthropicReq
}

func convertAnthropicToolsAny(tools []interface{}) []map[string]interface{} {
	typed := make([]map[string]interface{}, 0, len(tools))
	for _, item := range tools {
		if m := mapFromAny(item); m != nil {
			typed = append(typed, m)
		}
	}
	return convertAnthropicTools(typed)
}

type anthropicConvertedMessages struct {
	SystemText string
	Messages   []interface{}
}

func convertOpenAIToAnthropicMessages(messages []interface{}) anthropicConvertedMessages {
	var result anthropicConvertedMessages
	for _, m := range messages {
		mm := mapFromAny(m)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		switch role {
		case "system", "developer":
			if content := openAICompatForwardTextContent(mm["content"]); content != "" {
				if result.SystemText != "" {
					result.SystemText += "\n"
				}
				result.SystemText += content
			}
		case "assistant":
			var blocks []interface{}
			if text := openAICompatForwardTextContent(mm["content"]); text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
			}
			for _, tc := range extractOpenAIToolCalls(mm) {
				var input interface{}
				_ = json.Unmarshal([]byte(tc.Arguments), &input)
				if input == nil {
					input = map[string]interface{}{}
				}
				blocks = append(blocks, map[string]interface{}{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input})
			}
			if len(blocks) > 0 {
				result.Messages = append(result.Messages, map[string]interface{}{"role": "assistant", "content": blocks})
			}
		case "tool":
			callID, _ := mm["tool_call_id"].(string)
			content := stringifyOpenAICompatForwardToolOutput(mm["content"])
			block := map[string]interface{}{"type": "tool_result", "tool_use_id": callID, "content": content}
			if callID != "" {
				block["id"] = "toolrslt_" + callID
			}
			if len(result.Messages) > 0 {
				if last, ok := result.Messages[len(result.Messages)-1].(map[string]interface{}); ok {
					if role, _ := last["role"].(string); role == "user" {
						if blocks, ok := last["content"].([]interface{}); ok && len(blocks) > 0 {
							if first, ok := blocks[0].(map[string]interface{}); ok && first["type"] == "tool_result" {
								last["content"] = append(blocks, block)
								continue
							}
						}
					}
				}
			}
			result.Messages = append(result.Messages, map[string]interface{}{"role": "user", "content": []interface{}{block}})
		default:
			result.Messages = append(result.Messages, map[string]interface{}{"role": role, "content": mm["content"]})
		}
	}
	return result
}

func convertAnthropicTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn := mapFromAny(t["function"])
		if fn == nil && strings.TrimSpace(fmt.Sprint(t["type"])) == "function" {
			fn = t
		}
		if fn == nil {
			continue
		}
		tool := map[string]interface{}{"name": fn["name"]}
		if desc, ok := fn["description"]; ok {
			tool["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			tool["input_schema"] = params
		}
		out = append(out, tool)
	}
	return out
}

type openAIToolCallParts struct {
	ID        string
	Name      string
	Arguments string
}

func extractOpenAIToolCalls(mm map[string]interface{}) []openAIToolCallParts {
	raw := mm["tool_calls"]
	items := openAICompatForwardSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]openAIToolCallParts, 0, len(items))
	for _, item := range items {
		call := mapFromAny(item)
		if call == nil {
			continue
		}
		fn := mapFromAny(call["function"])
		if fn == nil {
			continue
		}
		out = append(out, openAIToolCallParts{
			ID:        stringFromMap(call, "id"),
			Name:      stringFromMap(fn, "name"),
			Arguments: openAIToolCallArgumentsString(fn["arguments"]),
		})
	}
	return out
}

func openAIToolCallArgumentsString(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return "{}"
	case string:
		if strings.TrimSpace(v) == "" {
			return "{}"
		}
		return v
	case json.RawMessage:
		if len(v) == 0 || strings.TrimSpace(string(v)) == "" || strings.TrimSpace(string(v)) == "null" {
			return "{}"
		}
		return string(v)
	default:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 || strings.TrimSpace(string(data)) == "" || strings.TrimSpace(string(data)) == "null" {
			return "{}"
		}
		return string(data)
	}
}

func mapFromAny(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	case map[string]string:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[k] = val
		}
		return out
	default:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 || string(data) == "null" {
			return nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
			return nil
		}
		return out
	}
}

func stringFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// anthropicToOpenAI converts an Anthropic Messages API response body
// to an OpenAI Chat Completions response body.
func anthropicToOpenAI(resp map[string]interface{}, model string) map[string]interface{} {
	// Extract and concatenate text content blocks
	var contentBuilder strings.Builder
	var toolCalls []interface{}
	for _, block := range openAICompatForwardSlice(resp["content"]) {
		blockMap := mapFromAny(block)
		if blockMap == nil {
			continue
		}
		blockType, _ := blockMap["type"].(string)
		switch blockType {
		case "text":
			text, _ := blockMap["text"].(string)
			contentBuilder.WriteString(text)
		case "tool_use":
			id, _ := blockMap["id"].(string)
			name, _ := blockMap["name"].(string)
			input := blockMap["input"]
			argsBytes, _ := json.Marshal(input)
			if string(argsBytes) == "null" || len(argsBytes) == 0 {
				argsBytes = []byte("{}")
			}
			if id != "" && name != "" {
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   id,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": string(argsBytes),
					},
				})
			}
		}
	}
	contentText := contentBuilder.String()
	message := map[string]interface{}{
		"role":    "assistant",
		"content": contentText,
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	// Map stop_reason to finish_reason
	stopReason, _ := resp["stop_reason"].(string)
	var finishReason string
	switch stopReason {
	case "tool_use":
		finishReason = "tool_calls"
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	default:
		finishReason = "stop"
	}

	// Map usage fields
	var promptTokens, completionTokens float64
	if usage := mapFromAny(resp["usage"]); usage != nil {
		promptTokens = float64(numberToInt64(usage["input_tokens"]))
		completionTokens = float64(numberToInt64(usage["output_tokens"]))
	}
	totalTokens := promptTokens + completionTokens

	// Extract ID or use default
	id, _ := resp["id"].(string)
	if id == "" {
		id = "chatcmpl-proxy"
	}

	// Build OpenAI response
	return map[string]interface{}{
		"id":     id,
		"object": "chat.completion",
		"model":  model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
}

// forwardAnthropic converts OpenAI request to Anthropic format, sends it,
// and converts the response back to OpenAI format.
func (p *OpenAIProxy) forwardAnthropic(body map[string]interface{}) ([]byte, int, error) {
	// 1. Convert request to Anthropic format
	cfg := p.maclawLLMConfig()
	ApplyReasoningControls(cfg, body, ReasoningAPIAnthropic)
	anthropicReq := openaiToAnthropic(body, cfg.UpstreamModel())

	respBody, statusCode, err := forwardAnthropicMessageWithSDK(context.Background(), cfg, anthropicReq, p.client)
	if err != nil {
		if statusCode >= 400 {
			return openAICompatAnthropicUpstreamError(statusCode, respBody)
		}
		return nil, statusCode, err
	}
	if statusCode >= 400 {
		return openAICompatAnthropicUpstreamError(statusCode, respBody)
	}

	// 9. Parse Anthropic response and convert to OpenAI format
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse anthropic response: %w", err)
	}

	openaiResp := anthropicToOpenAI(respMap, cfg.UpstreamModel())
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}

	return data, http.StatusOK, nil
}

// forwardResponses converts OpenAI request to Responses API format, sends it,
// and converts the response back to OpenAI format.
func (p *OpenAIProxy) forwardResponses(body map[string]interface{}) ([]byte, int, error) {
	// 1. Convert request to Responses API format
	cfg := p.maclawLLMConfig()
	fwd := cloneOpenAICompatBody(body)
	sanitizeOpenAICompatForwardBodyForResponses(cfg, fwd)
	responsesReq := openaiToResponses(fwd, cfg.UpstreamModel())
	ApplyReasoningControls(cfg, responsesReq, ReasoningAPIResponses)

	// 2. Marshal to JSON
	jsonBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal responses request: %w", err)
	}

	// 3. Construct URL
	upstreamURL := openAIResponsesEndpoint(p.config.URL)

	// 4. Create POST request
	req, err := http.NewRequest("POST", upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	// 5. Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.Key)
	req.Header.Set("User-Agent", cfg.UserAgent())
	SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())

	// 6. Execute request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// 7. Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}

	// 8. On upstream 4xx/5xx: wrap error in OpenAI format
	if resp.StatusCode >= 400 {
		errResp := map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream error (HTTP %d): body_len=%d", resp.StatusCode, len(respBody)),
				"type":    "server_error",
			},
		}
		data, _ := json.Marshal(errResp)
		return data, resp.StatusCode, nil
	}

	// 9. Parse Responses API response and convert to OpenAI format
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse responses api response: %w", err)
	}

	openaiResp := responsesToOpenAI(respMap, cfg.UpstreamModel())
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}

	return data, http.StatusOK, nil
}

func (p *OpenAIProxy) forwardResponsesRaw(body map[string]interface{}) ([]byte, int, error) {
	fwd := cloneOpenAICompatBody(body)
	cfg := p.maclawLLMConfig()
	return ForwardOpenAIResponsesRawRequest(context.Background(), cfg, fwd, p.client)
}

func ForwardOpenAIResponsesRawRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client) ([]byte, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	fwd := cloneOpenAICompatBody(body)
	if model := strings.TrimSpace(cfg.UpstreamModel()); model != "" {
		fwd["model"] = model
	}
	ApplyReasoningControls(cfg, fwd, ReasoningAPIResponses)
	jsonBody, err := json.Marshal(fwd)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal responses request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesEndpoint(cfg.URL), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	req.Header.Set("User-Agent", cfg.UserAgent())
	SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

func openAIProxyResponsesRequestToChat(payload map[string]interface{}) (map[string]interface{}, string, error) {
	model := strings.TrimSpace(fmt.Sprint(payload["model"]))
	if model == "" || model == "<nil>" {
		return nil, "", fmt.Errorf("model is required")
	}
	messages := openAIProxyResponsesInputToChatMessages(payload["input"])
	if instructions := strings.TrimSpace(fmt.Sprint(payload["instructions"])); instructions != "" && instructions != "<nil>" {
		messages = append([]interface{}{map[string]interface{}{"role": "system", "content": instructions}}, messages...)
	}
	if len(messages) == 0 {
		return nil, model, fmt.Errorf("input is required")
	}
	chat := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}
	if tools := openAIProxyResponsesToolsToChatTools(payload["tools"]); len(tools) > 0 {
		chat["tools"] = tools
	}
	if choice := openAIProxyResponsesToolChoiceToChat(payload["tool_choice"]); choice != nil {
		chat["tool_choice"] = choice
	}
	if v, ok := payload["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	} else if v, ok := payload["max_tokens"]; ok {
		chat["max_tokens"] = v
	}
	for _, key := range []string{"temperature", "top_p", "user"} {
		if v, ok := payload[key]; ok {
			chat[key] = v
		}
	}
	if responseFormat := openAIProxyResponsesTextToChatResponseFormat(payload["text"]); responseFormat != nil {
		chat["response_format"] = responseFormat
	}
	return chat, model, nil
}

// OpenAICompatResponsesRequestToChat converts an OpenAI Responses API request
// into the Chat Completions shape used by the shared compatibility forwarder.
func OpenAICompatResponsesRequestToChat(payload map[string]interface{}) (map[string]interface{}, string, error) {
	return openAIProxyResponsesRequestToChat(payload)
}

func openAIProxyResponsesInputToChatMessages(input interface{}) []interface{} {
	switch v := input.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []interface{}{map[string]interface{}{"role": "user", "content": v}}
	default:
		items := openAICompatForwardSlice(v)
		if len(items) == 0 {
			if text := strings.TrimSpace(fmt.Sprint(input)); text != "" && text != "<nil>" {
				return []interface{}{map[string]interface{}{"role": "user", "content": text}}
			}
			return nil
		}
		out := make([]interface{}, 0, len(items))
		for _, item := range items {
			if msg := openAIProxyResponsesInputItemToChatMessage(item); msg != nil {
				out = append(out, msg)
			}
		}
		return out
	}
}

func openAIProxyResponsesInputItemToChatMessage(item interface{}) map[string]interface{} {
	m := mapFromAny(item)
	if m == nil {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text == "" || text == "<nil>" {
			return nil
		}
		return map[string]interface{}{"role": "user", "content": text}
	}
	typ := strings.TrimSpace(fmt.Sprint(m["type"]))
	switch typ {
	case "message", "":
		role := strings.TrimSpace(fmt.Sprint(m["role"]))
		if role == "" || role == "<nil>" {
			role = "user"
		}
		contentRaw := firstOpenAICompatNonNil(m["content"], m["text"], m["input"])
		content := openAIProxyResponsesContentToChatContent(contentRaw, role)
		return map[string]interface{}{"role": role, "content": content}
	case "function_call_output":
		callID := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["call_id"], m["id"])))
		if callID == "" || callID == "<nil>" {
			return nil
		}
		return map[string]interface{}{"role": "tool", "tool_call_id": callID, "content": openAIProxyResponsesContentToText(firstOpenAICompatNonNil(m["output"], m["content"]))}
	case "function_call":
		name := strings.TrimSpace(fmt.Sprint(m["name"]))
		if name == "" || name == "<nil>" {
			return nil
		}
		callID := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["call_id"], m["id"])))
		if callID == "" || callID == "<nil>" {
			callID = randomOpenAICompatForwardToolCallID()
		}
		return map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   callID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": openAIToolCallArgumentsString(m["arguments"]),
				},
			}},
		}
	default:
		return map[string]interface{}{"role": "user", "content": openAIProxyResponsesContentToText(m)}
	}
}

func openAIProxyResponsesContentToText(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	items := openAICompatForwardSlice(value)
	if len(items) == 0 {
		if m := mapFromAny(value); m != nil {
			if text := openAIProxyResponsesSingleContentBlockToText(m); text != "" {
				return text
			}
		}
		data, err := json.Marshal(value)
		if err == nil && string(data) != "null" {
			return string(data)
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
	var parts []string
	for _, item := range items {
		m := mapFromAny(item)
		if m == nil {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				parts = append(parts, text)
			}
			continue
		}
		if text := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["text"], m["content"], m["input_text"], m["output_text"]))); text != "" && text != "<nil>" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func openAIProxyResponsesContentToChatContent(value interface{}, role string) interface{} {
	if role != "user" {
		return openAIProxyResponsesContentToText(value)
	}
	items := openAICompatForwardSlice(value)
	if len(items) == 0 {
		if m := mapFromAny(value); m != nil {
			if block := openAIProxyResponsesSingleContentBlockToChat(m); block != nil {
				return []interface{}{block}
			}
		}
		return openAIProxyResponsesContentToText(value)
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m := mapFromAny(item)
		if m == nil {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				out = append(out, map[string]interface{}{"type": "text", "text": text})
			}
			continue
		}
		switch strings.TrimSpace(fmt.Sprint(m["type"])) {
		case "text", "input_text", "output_text":
			if text := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["text"], m["content"], m["input_text"], m["output_text"]))); text != "" && text != "<nil>" {
				out = append(out, map[string]interface{}{"type": "text", "text": text})
			}
		case "input_image", "image_url":
			if image := openAIProxyResponsesImageBlockToChat(m); image != nil {
				out = append(out, image)
			}
		case "input_file", "file":
			if file := openAIProxyResponsesFileBlockToChat(m); file != nil {
				out = append(out, file)
			}
		}
	}
	if len(out) == 0 {
		return openAIProxyResponsesContentToText(value)
	}
	return out
}

func openAIProxyResponsesSingleContentBlockToText(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	if text := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["text"], m["content"], m["input_text"], m["output_text"]))); text != "" && text != "<nil>" {
		return text
	}
	return ""
}

func openAIProxyResponsesSingleContentBlockToChat(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	switch strings.TrimSpace(fmt.Sprint(m["type"])) {
	case "text", "input_text", "output_text":
		if text := openAIProxyResponsesSingleContentBlockToText(m); text != "" {
			return map[string]interface{}{"type": "text", "text": text}
		}
	case "input_image", "image_url":
		return openAIProxyResponsesImageBlockToChat(m)
	case "input_file", "file":
		return openAIProxyResponsesFileBlockToChat(m)
	}
	return nil
}

func openAIProxyResponsesImageBlockToChat(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	url := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["image_url"], m["url"])))
	if nested := mapFromAny(m["image_url"]); nested != nil {
		url = strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(nested["url"], nested["image_url"])))
	}
	if url == "" || url == "<nil>" {
		return nil
	}
	image := map[string]interface{}{"url": url}
	if detail := strings.TrimSpace(fmt.Sprint(m["detail"])); detail != "" && detail != "<nil>" {
		image["detail"] = detail
	}
	return map[string]interface{}{"type": "image_url", "image_url": image}
}

func openAIProxyResponsesFileBlockToChat(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	file := map[string]interface{}{}
	for _, key := range []string{"file_id", "filename", "file_data"} {
		if value := strings.TrimSpace(fmt.Sprint(m[key])); value != "" && value != "<nil>" {
			file[key] = value
		}
	}
	if nested := mapFromAny(m["file"]); nested != nil {
		for _, key := range []string{"file_id", "filename", "file_data"} {
			if value := strings.TrimSpace(fmt.Sprint(nested[key])); value != "" && value != "<nil>" {
				file[key] = value
			}
		}
	}
	if len(file) == 0 {
		return nil
	}
	return map[string]interface{}{"type": "file", "file": file}
}

func openAIProxyResponsesToolsToChatTools(value interface{}) []interface{} {
	items := openAICompatForwardSlice(value)
	if len(items) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m := mapFromAny(item)
		if m == nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(m["type"])) != "" && strings.TrimSpace(fmt.Sprint(m["type"])) != "function" {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(m["name"]))
		if name == "" || name == "<nil>" {
			if fn := mapFromAny(m["function"]); fn != nil {
				name = strings.TrimSpace(fmt.Sprint(fn["name"]))
				m = fn
			}
		}
		if name == "" || name == "<nil>" {
			continue
		}
		fn := map[string]interface{}{"name": name}
		if desc := strings.TrimSpace(fmt.Sprint(m["description"])); desc != "" && desc != "<nil>" {
			fn["description"] = desc
		}
		if params, ok := m["parameters"]; ok {
			fn["parameters"] = params
		}
		if strict, ok := m["strict"].(bool); ok {
			fn["strict"] = strict
		}
		out = append(out, map[string]interface{}{"type": "function", "function": fn})
	}
	return out
}

func openAIProxyResponsesToolChoiceToChat(value interface{}) interface{} {
	if s, ok := value.(string); ok {
		switch strings.TrimSpace(s) {
		case "auto", "none", "required":
			return strings.TrimSpace(s)
		}
		return nil
	}
	m := mapFromAny(value)
	if m == nil {
		return nil
	}
	if strings.TrimSpace(fmt.Sprint(m["type"])) != "function" {
		return nil
	}
	name := strings.TrimSpace(fmt.Sprint(m["name"]))
	if name == "" || name == "<nil>" {
		if fn := mapFromAny(m["function"]); fn != nil {
			name = strings.TrimSpace(fmt.Sprint(fn["name"]))
		}
	}
	if name == "" || name == "<nil>" {
		return nil
	}
	return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}}
}

func openAIProxyResponsesTextToChatResponseFormat(value interface{}) interface{} {
	text := mapFromAny(value)
	if text == nil {
		return nil
	}
	format := mapFromAny(text["format"])
	if format == nil {
		return nil
	}
	typ := strings.TrimSpace(fmt.Sprint(format["type"]))
	switch typ {
	case "text", "json_object":
		return map[string]interface{}{"type": typ}
	case "json_schema":
		name := strings.TrimSpace(fmt.Sprint(format["name"]))
		schema, ok := format["schema"]
		if name == "" || name == "<nil>" || !ok || schema == nil {
			return nil
		}
		js := map[string]interface{}{"name": name, "schema": schema}
		if desc := strings.TrimSpace(fmt.Sprint(format["description"])); desc != "" && desc != "<nil>" {
			js["description"] = desc
		}
		if strict, ok := format["strict"].(bool); ok {
			js["strict"] = strict
		}
		return map[string]interface{}{"type": "json_schema", "json_schema": js}
	default:
		return nil
	}
}

func openAIProxyChatResponseToResponses(body []byte, model string) ([]byte, error) {
	var chat map[string]interface{}
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, fmt.Errorf("parse chat response: %w", err)
	}
	if m := strings.TrimSpace(fmt.Sprint(chat["model"])); m != "" && m != "<nil>" {
		model = m
	}
	output := []interface{}{}
	choices := openAICompatForwardSlice(chat["choices"])
	if len(choices) > 0 {
		choice := mapFromAny(choices[0])
		msg := mapFromAny(choice["message"])
		if msg != nil {
			if text := openAIProxyResponsesContentToText(msg["content"]); strings.TrimSpace(text) != "" {
				output = append(output, map[string]interface{}{
					"type":    "message",
					"id":      "msg_0",
					"role":    "assistant",
					"status":  "completed",
					"content": []interface{}{map[string]interface{}{"type": "output_text", "text": text}},
				})
			}
			for i, raw := range openAICompatForwardSlice(msg["tool_calls"]) {
				call := mapFromAny(raw)
				fn := mapFromAny(call["function"])
				name := strings.TrimSpace(fmt.Sprint(fn["name"]))
				if name == "" || name == "<nil>" {
					continue
				}
				callID := strings.TrimSpace(fmt.Sprint(call["id"]))
				if callID == "" || callID == "<nil>" {
					callID = fmt.Sprintf("call_%d", i)
				}
				output = append(output, map[string]interface{}{
					"type":      "function_call",
					"id":        callID,
					"call_id":   callID,
					"name":      name,
					"arguments": openAIToolCallArgumentsString(fn["arguments"]),
					"status":    "completed",
				})
			}
			if legacy := mapFromAny(msg["function_call"]); legacy != nil {
				name := strings.TrimSpace(fmt.Sprint(legacy["name"]))
				if name != "" && name != "<nil>" {
					callID := "call_legacy_function"
					output = append(output, map[string]interface{}{
						"type":      "function_call",
						"id":        "fc_legacy_function",
						"call_id":   callID,
						"name":      name,
						"arguments": openAIToolCallArgumentsString(legacy["arguments"]),
						"status":    "completed",
					})
				}
			}
		}
	}
	createdAt := time.Now().Unix()
	resp := map[string]interface{}{
		"id":         fmt.Sprint(firstOpenAICompatNonNil(chat["id"], "resp_maclaw_proxy")),
		"object":     "response",
		"model":      model,
		"status":     "completed",
		"output":     output,
		"usage":      openAIProxyChatUsageToResponsesUsage(chat["usage"]),
		"created":    createdAt,
		"created_at": float64(createdAt),
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal responses response: %w", err)
	}
	return data, nil
}

func openAIProxyChatUsageToResponsesUsage(raw interface{}) interface{} {
	usage := mapFromAny(raw)
	if usage == nil {
		return raw
	}
	inputTokens := numberToInt64(firstOpenAICompatNonNil(usage["input_tokens"], usage["prompt_tokens"]))
	outputTokens := numberToInt64(firstOpenAICompatNonNil(usage["output_tokens"], usage["completion_tokens"]))
	totalTokens := numberToInt64(usage["total_tokens"])
	out := make(map[string]interface{}, len(usage)+2)
	for k, v := range usage {
		out[k] = v
	}
	out["input_tokens"] = inputTokens
	out["output_tokens"] = outputTokens
	out["total_tokens"] = totalTokens
	if details := mapFromAny(firstOpenAICompatNonNil(usage["input_tokens_details"], usage["prompt_tokens_details"])); details != nil {
		out["input_tokens_details"] = details
	}
	return out
}

// OpenAICompatChatResponseToResponses converts an OpenAI-compatible Chat
// Completions response into an OpenAI Responses API envelope.
func OpenAICompatChatResponseToResponses(body []byte, model string) ([]byte, error) {
	return openAIProxyChatResponseToResponses(body, model)
}

func (p *OpenAIProxy) maclawLLMConfig() MaclawLLMConfig {
	if p == nil {
		return MaclawLLMConfig{}
	}
	return MaclawLLMConfig{
		URL:             p.config.URL,
		Key:             p.config.Key,
		Model:           p.config.Model,
		Protocol:        p.config.Protocol,
		WireAPI:         p.config.WireAPI,
		AgentType:       p.config.AgentType,
		ThinkingMode:    p.config.ThinkingMode,
		ReasoningEffort: p.config.ReasoningEffort,
	}
}

func cloneOpenAICompatBody(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	out := make(map[string]interface{}, len(body))
	for k, v := range body {
		out[k] = v
	}
	return out
}
