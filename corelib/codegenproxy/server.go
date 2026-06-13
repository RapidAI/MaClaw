// Package codegenproxy implements a local HTTP proxy that accepts
// Anthropic Messages API requests and converts them to OpenAI chat
// completions format before forwarding to the upstream CodeGen service.
// This allows Claude Code (which speaks Anthropic protocol) to work
// with CodeGen (which only speaks OpenAI protocol).
package codegenproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	llmcompat "github.com/RapidAI/CodeClaw/corelib/llm"
)

const codeGenQwenFlashMaxTokens = 8192
const codeGenQwenFlashToolDescriptionMax = 512
const codeGenQwenFlashSchemaDescriptionMax = 256
const codeGenQwenFlashToolLoopAfterToolResults = 32
const codeGenQwenFlashToolLoopRepeatedToolCalls = 8
const codeGenQwenFlashSystemPrompt = "You are TigerClaw Code, a helpful coding assistant. Follow the user's instructions, use available tools when needed, and report outcomes clearly."
const codeGenStreamMaxEventBytes = 32 * 1024 * 1024
const codeGenContentToolScanFlushBytes = 4096
const codeGenContentToolScanRetainBytes = 128

func setCodeGenUpstreamHeaders(req *http.Request, clientName string) {
	if req != nil {
		normalized := corelib.NormalizeCodeGenClientName(clientName)
		req.Header.Set("User-Agent", normalized)
		req.Header.Set(corelib.CodeGenClientNameHeader, normalized)
	}
}

func codeGenProxyOpenAIBaseURL(upURL, clientName string) string {
	return corelib.NormalizeGLMCodingPlanOpenAIBaseURL(strings.TrimRight(strings.TrimSpace(upURL), "/"), clientName)
}

func codeGenProxyChatCompletionsEndpoint(upURL, clientName string) string {
	baseURL := codeGenProxyOpenAIBaseURL(upURL, clientName)
	if codeGenProxyHasEmptyURLPath(baseURL) {
		return strings.TrimRight(baseURL, "/") + "/chat/completions"
	}
	return llmcompat.BuildOpenAIChatCompletionsEndpoint(baseURL)
}

func codeGenProxyModelsEndpoint(upURL, clientName, protocol string) string {
	baseURL := codeGenProxyOpenAIBaseURL(upURL, clientName)
	if codeGenProxyHasEmptyURLPath(baseURL) {
		return strings.TrimRight(baseURL, "/") + "/models"
	}
	candidates := llmcompat.BuildOpenAIModelsEndpointCandidates(baseURL, protocol)
	if len(candidates) == 0 {
		return strings.TrimRight(strings.TrimSpace(upURL), "/") + "/models"
	}
	return candidates[0]
}

func codeGenProxyHasEmptyURLPath(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return false
	}
	return parsed.Scheme != "" && parsed.Host != "" && (parsed.Path == "" || parsed.Path == "/")
}

// Server is the local Anthropic→OpenAI protocol conversion proxy.
type Server struct {
	addr     string
	listener net.Listener
	srv      *http.Server
	client   *http.Client // reused for upstream requests
	upstream OpenAIUpstreamClient

	mu             sync.RWMutex
	upstreamURL    string // CodeGen OpenAI-compatible base URL
	apiKey         string // CodeGen access token
	clientName     string // upstream CodeGen client identity for User-Agent and X-Codegen-Client-Name
	clientKey      string // optional local proxy API key for OpenAI/Anthropic clients
	modelAliasByID map[string]string
}

// NewServer creates a new codegen proxy server.
func NewServer(addr string) *Server {
	client := &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true, // SSE must not be compressed
		},
	}
	return &Server{
		addr:     addr,
		client:   client,
		upstream: NewOpenAISDKUpstreamClient(client),
	}
}

// SetOpenAIUpstreamClient replaces the upstream OpenAI-compatible client.
// Passing nil restores the default openai-go backed implementation.
func (s *Server) SetOpenAIUpstreamClient(client OpenAIUpstreamClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client == nil {
		client = NewOpenAISDKUpstreamClient(s.client)
	}
	s.upstream = client
}

// SetUpstream configures the upstream CodeGen endpoint and API key.
func (s *Server) SetUpstream(baseURL, apiKey string) {
	s.SetUpstreamWithClientName(baseURL, apiKey, corelib.CodeGenClientName)
}

// SetUpstreamWithClientName configures the upstream CodeGen endpoint, API key,
// and CodeGen client identity used for both User-Agent and
// X-Codegen-Client-Name on upstream requests.
func (s *Server) SetUpstreamWithClientName(baseURL, apiKey, clientName string) {
	normalizedClient := corelib.NormalizeCodeGenClientName(clientName)
	s.mu.Lock()
	s.upstreamURL = strings.TrimRight(baseURL, "/")
	s.apiKey = apiKey
	s.clientName = normalizedClient
	s.modelAliasByID = nil
	s.mu.Unlock()
	log.Printf("[codegenproxy] upstream configured base=%q client=%q key_present=%v", strings.TrimRight(baseURL, "/"), normalizedClient, strings.TrimSpace(apiKey) != "")
}

// SetClientAPIKey configures the optional API key that local OpenAI and
// Anthropic clients must send to the proxy. When this key is set, incoming
// credentials are validated locally and are not forwarded to CodeGen; the
// SSO access token configured by SetUpstream is used for upstream requests.
func (s *Server) SetClientAPIKey(apiKey string) {
	s.mu.Lock()
	s.clientKey = strings.TrimSpace(apiKey)
	s.mu.Unlock()
}

func (s *Server) getConfig() (string, string, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.upstreamURL, s.apiKey, s.clientName, s.clientKey
}

func (s *Server) getUpstreamClient() OpenAIUpstreamClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.upstream != nil {
		return s.upstream
	}
	return NewOpenAISDKUpstreamClient(s.client)
}

func (s *Server) getModelAlias(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.modelAliasByID == nil {
		return ""
	}
	return s.modelAliasByID[key]
}

func (s *Server) setModelAliases(aliases map[string]string) {
	if len(aliases) == 0 {
		return
	}
	s.mu.Lock()
	s.modelAliasByID = aliases
	s.mu.Unlock()
}

// resolveAPIKey determines the API key to use for the upstream request.
// Priority: incoming request header > server-configured key.
// Claude Code sets ANTHROPIC_AUTH_TOKEN which the SDK sends as
// x-api-key and/or Authorization: Bearer in the request headers.
func resolveAPIKey(r *http.Request, fallback string) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return fallback
}

func requestAPIKey(r *http.Request) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (s *Server) authorizeClient(r *http.Request, configuredClientKey string) bool {
	if strings.TrimSpace(configuredClientKey) == "" {
		return true
	}
	return requestAPIKey(r) == configuredClientKey
}

// Start starts the HTTP server. It blocks until the server is stopped.
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	return s.Serve(ctx, listener)
}

// Serve starts the HTTP server on a listener created by the caller. This lets
// desktop wrappers bind synchronously, report failures immediately, and then
// hand the already-bound socket to the proxy.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	shutdownDone := make(chan struct{})
	defer close(shutdownDone)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleOpenAIChatCompletions)
	mux.HandleFunc("/v1/responses", s.handleOpenAIResponses)
	mux.HandleFunc("/v1/models", s.handleOpenAIModels)
	mux.HandleFunc("/v1/models/", s.handleOpenAIModel)
	mux.HandleFunc("/models", s.handleOpenAIModels)
	mux.HandleFunc("/anthropic/v1/messages", s.handleMessages)
	mux.HandleFunc("/anthropic/v1/messages/count_tokens", s.handleAnthropicCountTokens)
	mux.HandleFunc("/anthropic/v1/models", s.handleModels)
	mux.HandleFunc("/anthropic/v1/models/", s.handleAnthropicModel)
	mux.HandleFunc("/health", s.handleHealth)

	server := &http.Server{Handler: mux}
	s.mu.Lock()
	s.listener = listener
	s.srv = server
	s.mu.Unlock()
	log.Printf("[codegenproxy] listening on %s (Anthropic→OpenAI adapter)", listener.Addr())

	go func() {
		select {
		case <-ctx.Done():
			_ = server.Shutdown(context.Background())
		case <-shutdownDone:
		}
	}()

	if err := server.Serve(listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	s.mu.RLock()
	server := s.srv
	s.mu.RUnlock()
	if server != nil {
		_ = server.Shutdown(context.Background())
	}
}

// Addr returns the listener address. Only valid after Start has bound.
func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	listener := s.listener
	s.mu.RUnlock()
	if listener != nil {
		return listener.Addr()
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	upURL, _, _, _ := s.getConfig()
	status := "ok"
	if upURL == "" {
		status = "not_configured"
	}
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	s.handleModelsForProtocol(w, r, "anthropic")
}

func (s *Server) handleAnthropicModel(w http.ResponseWriter, r *http.Request) {
	s.handleModelForProtocol(w, r, "anthropic", strings.TrimPrefix(r.URL.Path, "/anthropic/v1/models/"))
}

func (s *Server) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	s.handleModelsForProtocol(w, r, "openai")
}

func (s *Server) handleOpenAIModel(w http.ResponseWriter, r *http.Request) {
	s.handleModelForProtocol(w, r, "openai", strings.TrimPrefix(r.URL.Path, "/v1/models/"))
}

func (s *Server) handleAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	reqID := newLogRequestID()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	_, _, _, clientKey := s.getConfig()
	if !s.authorizeClient(r, clientKey) {
		log.Printf("[codegenproxy] anthropic count_tokens request id=%s rejected: invalid proxy api key", reqID)
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		log.Printf("[codegenproxy] anthropic count_tokens read body error id=%s err=%v", reqID, err)
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[codegenproxy] anthropic count_tokens parse error id=%s err=%v body=%s", reqID, err, truncateForLog(body, 4096))
		writeError(w, http.StatusBadRequest, "parse body: "+err.Error())
		return
	}
	inputTokens := estimateAnthropicCountTokens(payload)
	log.Printf("[codegenproxy] anthropic count_tokens response id=%s model=%q input_tokens=%d messages=%d tools=%d",
		reqID, logScalar(payload["model"]), inputTokens, logArrayLen(payload["messages"]), logArrayLen(payload["tools"]))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": inputTokens})
}

func (s *Server) handleModelsForProtocol(w http.ResponseWriter, r *http.Request, protocol string) {
	reqID := newLogRequestID()
	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		log.Printf("[codegenproxy] models request id=%s protocol=%s rejected: upstream not configured", reqID, protocol)
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		log.Printf("[codegenproxy] models request id=%s protocol=%s rejected: invalid proxy api key", reqID, protocol)
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}
	upEndpoint := codeGenProxyModelsEndpoint(upURL, clientName, protocol)
	normalizedClient := corelib.NormalizeCodeGenClientName(clientName)
	log.Printf("[codegenproxy] models upstream request id=%s protocol=%s endpoint=%q client=%q user_agent=%q codegen_client=%q accept=%q",
		reqID, protocol, upEndpoint, clientName, normalizedClient, normalizedClient, r.Header.Get("Accept"))
	resp, err := s.getUpstreamClient().DoModels(r.Context(), upEndpoint, apiKey, clientName)
	if err != nil {
		log.Printf("[codegenproxy] models upstream failed id=%s protocol=%s endpoint=%q err=%v", reqID, protocol, upEndpoint, err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		log.Printf("[codegenproxy] models upstream read failed id=%s protocol=%s endpoint=%q status=%d err=%v", reqID, protocol, upEndpoint, resp.StatusCode, err)
		writeError(w, http.StatusBadGateway, "read upstream models: "+err.Error())
		return
	}
	log.Printf("[codegenproxy] models upstream response id=%s protocol=%s status=%d raw_models=%s",
		reqID, protocol, resp.StatusCode, strings.Join(extractModelIDsFromModelsBody(body), ","))
	s.setModelAliases(buildModelAliasMapFromModelsBody(body))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[codegenproxy] models upstream error id=%s protocol=%s status=%d body=%s",
			reqID, protocol, resp.StatusCode, truncateForLog(body, 4096))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	normalized, err := normalizeModelsResponse(body, protocol)
	if err != nil {
		log.Printf("[codegenproxy] models normalize failed id=%s protocol=%s err=%v body=%s", reqID, protocol, err, truncateForLog(body, 4096))
		writeError(w, http.StatusBadGateway, "parse upstream models: "+err.Error())
		return
	}
	log.Printf("[codegenproxy] models proxy response id=%s protocol=%s normalized_models=%s",
		reqID, protocol, strings.Join(extractModelIDsFromModelsBody(normalized), ","))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(normalized)
}

func (s *Server) handleModelForProtocol(w http.ResponseWriter, r *http.Request, protocol, modelID string) {
	reqID := newLogRequestID()
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	modelID, err := url.PathUnescape(strings.TrimSpace(modelID))
	if err != nil || modelID == "" {
		writeError(w, http.StatusBadRequest, "model id required")
		return
	}
	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		log.Printf("[codegenproxy] model request id=%s protocol=%s model=%q rejected: upstream not configured", reqID, protocol, modelID)
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		log.Printf("[codegenproxy] model request id=%s protocol=%s model=%q rejected: invalid proxy api key", reqID, protocol, modelID)
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}
	upEndpoint := codeGenProxyModelsEndpoint(upURL, clientName, protocol)
	resp, err := s.getUpstreamClient().DoModels(r.Context(), upEndpoint, apiKey, clientName)
	if err != nil {
		log.Printf("[codegenproxy] model upstream failed id=%s protocol=%s model=%q endpoint=%q err=%v", reqID, protocol, modelID, upEndpoint, err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		log.Printf("[codegenproxy] model upstream read failed id=%s protocol=%s model=%q endpoint=%q status=%d err=%v", reqID, protocol, modelID, upEndpoint, resp.StatusCode, err)
		writeError(w, http.StatusBadGateway, "read upstream models: "+err.Error())
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	normalized, err := normalizeSingleModelResponse(body, protocol, modelID)
	if err != nil {
		log.Printf("[codegenproxy] model normalize failed id=%s protocol=%s model=%q err=%v body=%s", reqID, protocol, modelID, err, truncateForLog(body, 4096))
		writeError(w, http.StatusBadGateway, "parse upstream models: "+err.Error())
		return
	}
	if len(normalized) == 0 {
		log.Printf("[codegenproxy] model not found id=%s protocol=%s model=%q upstream_models=%s", reqID, protocol, modelID, strings.Join(extractModelIDsFromModelsBody(body), ","))
		writeError(w, http.StatusNotFound, "model not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(normalized)
}

// handleOpenAIChatCompletions forwards OpenAI-compatible chat completion
// requests directly to CodeGen. The request body, including model name, is
// passed through unchanged so agent-side /model commands can select models
// without TigerProxy rewriting them.
func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	reqID := newLogRequestID()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		log.Printf("[codegenproxy] openai chat request id=%s rejected: upstream not configured", reqID)
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		log.Printf("[codegenproxy] openai chat request id=%s rejected: invalid proxy api key", reqID)
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		log.Printf("[codegenproxy] openai chat read body error id=%s err=%v", reqID, err)
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	originalModel := extractJSONFieldString(body, "model")
	body = normalizeOpenAIModelInBody(body)
	normalizedModel := extractJSONFieldString(body, "model")
	if resolvedModel := s.resolveCodeGenModelAlias(r.Context(), upURL, apiKey, clientName, normalizedModel); resolvedModel != "" && resolvedModel != normalizedModel {
		body = setOpenAIModelInBody(body, resolvedModel)
		normalizedModel = resolvedModel
	}
	body, compatibilityNotes := applyCodeGenOpenAICompatibility(body)
	requestSummary := summarizeOpenAIRequest(body)

	upEndpoint := codeGenProxyChatCompletionsEndpoint(upURL, clientName)
	normalizedClient := corelib.NormalizeCodeGenClientName(clientName)
	log.Printf("[codegenproxy] openai chat upstream request id=%s endpoint=%q client=%q user_agent=%q codegen_client=%q accept=%q original_model=%q normalized_model=%q summary=%s compatibility=%s",
		reqID, upEndpoint, clientName, normalizedClient, normalizedClient, r.Header.Get("Accept"), originalModel, normalizedModel, requestSummary, strings.Join(compatibilityNotes, ","))

	upResp, err := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, body, r.Header.Get("Accept"), logBoolFromBody(body, "stream"))
	if err != nil {
		log.Printf("[codegenproxy] openai chat upstream failed id=%s endpoint=%q model=%q err=%v", reqID, upEndpoint, normalizedModel, err)
		writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer upResp.Body.Close()

	if upResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
		if retryBody, ok := prepareQwenFlashChatOnlyRetryBody(normalizedModel, body, upResp.StatusCode); ok {
			log.Printf("[codegenproxy] openai chat retry without tools id=%s model=%q original_status=%d original_response=%s retry_summary=%s",
				reqID, normalizedModel, upResp.StatusCode, truncateForLog(respBody, 2048), summarizeOpenAIRequest(retryBody))
			retryResp, retryErr := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, retryBody, r.Header.Get("Accept"), logBoolFromBody(retryBody, "stream"))
			if retryErr != nil {
				log.Printf("[codegenproxy] openai chat retry without tools failed id=%s model=%q err=%v", reqID, normalizedModel, retryErr)
			} else {
				upResp = retryResp
				defer upResp.Body.Close()
				body = retryBody
				if upResp.StatusCode == http.StatusOK {
					log.Printf("[codegenproxy] openai chat retry without tools succeeded id=%s model=%q status=%d content_type=%q",
						reqID, normalizedModel, upResp.StatusCode, upResp.Header.Get("Content-Type"))
					respBody = nil
				} else {
					respBody, _ = io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
					log.Printf("[codegenproxy] openai chat retry without tools error id=%s model=%q status=%d content_type=%q response=%s",
						reqID, normalizedModel, upResp.StatusCode, upResp.Header.Get("Content-Type"), truncateForLog(respBody, 2048))
				}
			}
		}
		if compactBody, ok := prepareCodeGenCompactChatRetryBody(upURL, normalizedModel, body, upResp.StatusCode); ok {
			log.Printf("[codegenproxy] openai chat compact retry id=%s model=%q original_status=%d original_response=%s retry_summary=%s",
				reqID, normalizedModel, upResp.StatusCode, truncateForLog(respBody, 2048), summarizeOpenAIRequest(compactBody))
			compactResp, compactErr := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, compactBody, r.Header.Get("Accept"), logBoolFromBody(compactBody, "stream"))
			if compactErr != nil {
				log.Printf("[codegenproxy] openai chat compact retry failed id=%s model=%q err=%v", reqID, normalizedModel, compactErr)
			} else {
				upResp = compactResp
				defer upResp.Body.Close()
				body = compactBody
				if upResp.StatusCode == http.StatusOK {
					log.Printf("[codegenproxy] openai chat compact retry succeeded id=%s model=%q status=%d content_type=%q",
						reqID, normalizedModel, upResp.StatusCode, upResp.Header.Get("Content-Type"))
					respBody = nil
				} else {
					respBody, _ = io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
					log.Printf("[codegenproxy] openai chat compact retry error id=%s model=%q status=%d content_type=%q response=%s",
						reqID, normalizedModel, upResp.StatusCode, upResp.Header.Get("Content-Type"), truncateForLog(respBody, 2048))
				}
			}
		}
		if upResp.StatusCode != http.StatusOK {
			log.Printf("[codegenproxy] openai chat upstream error id=%s endpoint=%q status=%d model=%q content_type=%q request=%s response=%s",
				reqID, upEndpoint, upResp.StatusCode, normalizedModel, upResp.Header.Get("Content-Type"), truncateForLog(body, 4096), truncateForLog(respBody, 4096))
			upResp.Body = io.NopCloser(bytes.NewReader(respBody))
		}
	} else {
		log.Printf("[codegenproxy] openai chat upstream response id=%s status=%d model=%q content_type=%q",
			reqID, upResp.StatusCode, normalizedModel, upResp.Header.Get("Content-Type"))
	}

	if ct := upResp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(upResp.StatusCode)
	_, _ = io.Copy(w, upResp.Body)
}

// handleOpenAIResponses accepts the OpenAI Responses API shape used by newer
// OpenAI/Codex clients and bridges non-streaming calls to CodeGen chat
// completions.
func (s *Server) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	reqID := newLogRequestID()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		log.Printf("[codegenproxy] openai responses request id=%s rejected: upstream not configured", reqID)
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		log.Printf("[codegenproxy] openai responses request id=%s rejected: invalid proxy api key", reqID)
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		log.Printf("[codegenproxy] openai responses read body error id=%s err=%v", reqID, err)
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	chatBody, originalModel, err := convertOpenAIResponsesRequestToChat(body)
	if err != nil {
		log.Printf("[codegenproxy] openai responses convert request failed id=%s err=%v body=%s", reqID, err, truncateForLog(body, 4096))
		writeError(w, http.StatusBadRequest, "convert responses request: "+err.Error())
		return
	}
	chatBody = normalizeOpenAIModelInBody(chatBody)
	normalizedModel := extractJSONFieldString(chatBody, "model")
	if resolvedModel := s.resolveCodeGenModelAlias(r.Context(), upURL, apiKey, clientName, normalizedModel); resolvedModel != "" && resolvedModel != normalizedModel {
		chatBody = setOpenAIModelInBody(chatBody, resolvedModel)
		normalizedModel = resolvedModel
	}
	chatBody, compatibilityNotes := applyCodeGenOpenAICompatibility(chatBody)
	stream := logBoolFromBody(chatBody, "stream")

	upEndpoint := codeGenProxyChatCompletionsEndpoint(upURL, clientName)
	log.Printf("[codegenproxy] openai responses upstream request id=%s endpoint=%q original_model=%q normalized_model=%q stream=%v summary=%s compatibility=%s",
		reqID, upEndpoint, originalModel, normalizedModel, stream, summarizeOpenAIRequest(chatBody), strings.Join(compatibilityNotes, ","))

	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	upResp, err := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, chatBody, accept, stream)
	if err != nil {
		log.Printf("[codegenproxy] openai responses upstream failed id=%s endpoint=%q model=%q err=%v", reqID, upEndpoint, normalizedModel, err)
		writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer upResp.Body.Close()
	if stream {
		s.handleOpenAIResponsesStream(w, upResp, normalizedModel, reqID)
		return
	}
	respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 10*1024*1024))
	if upResp.StatusCode != http.StatusOK {
		log.Printf("[codegenproxy] openai responses upstream error id=%s endpoint=%q status=%d model=%q response=%s",
			reqID, upEndpoint, upResp.StatusCode, normalizedModel, truncateForLog(respBody, 4096))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	responsesBody, err := convertOpenAIChatResponseToResponses(respBody, normalizedModel)
	if err != nil {
		log.Printf("[codegenproxy] openai responses convert response failed id=%s model=%q err=%v body=%s", reqID, normalizedModel, err, truncateForLog(respBody, 4096))
		writeError(w, http.StatusBadGateway, "convert upstream response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(responsesBody)
}

func (s *Server) handleOpenAIResponsesStream(w http.ResponseWriter, upResp *http.Response, model, reqID string) {
	if upResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	respID := "resp_" + shortSHA256(reqID+":"+model)
	msgID := "msg_" + shortSHA256(respID+":message")
	seq := 1
	nextOutputIndex := 0
	textOutputIndex := -1
	var text strings.Builder
	textStarted := false
	toolCalls := map[int]*responsesStreamToolCallAccum{}
	var toolOrder []int
	var payloadErr error
	writeResponsesSSE(w, "response.created", map[string]interface{}{
		"type":            "response.created",
		"sequence_number": seq,
		"response":        responsesStreamResponseObject(respID, model, "", false, -1, nil, nil),
	})
	seq++
	flusher.Flush()

	handlePayload := func(payload string) bool {
		if strings.TrimSpace(payload) == "[DONE]" {
			return false
		}
		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			log.Printf("[codegenproxy] responses stream parse chunk failed id=%s model=%q err=%v payload=%s", reqID, model, err, truncateForLog([]byte(payload), 2048))
			payloadErr = err
			return false
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if !textStarted {
					textOutputIndex = nextOutputIndex
					nextOutputIndex++
					writeResponsesSSE(w, "response.output_item.added", map[string]interface{}{
						"type":            "response.output_item.added",
						"sequence_number": seq,
						"output_index":    textOutputIndex,
						"item": map[string]interface{}{
							"id":      msgID,
							"type":    "message",
							"status":  "in_progress",
							"role":    "assistant",
							"content": []interface{}{},
						},
					})
					seq++
					writeResponsesSSE(w, "response.content_part.added", map[string]interface{}{
						"type":            "response.content_part.added",
						"sequence_number": seq,
						"item_id":         msgID,
						"output_index":    textOutputIndex,
						"content_index":   0,
						"part": map[string]interface{}{
							"type":        "output_text",
							"text":        "",
							"annotations": []interface{}{},
						},
					})
					seq++
					textStarted = true
				}
				text.WriteString(choice.Delta.Content)
				writeResponsesSSE(w, "response.output_text.delta", map[string]interface{}{
					"type":            "response.output_text.delta",
					"sequence_number": seq,
					"item_id":         msgID,
					"output_index":    textOutputIndex,
					"content_index":   0,
					"delta":           choice.Delta.Content,
					"logprobs":        []interface{}{},
				})
				seq++
				flusher.Flush()
			}
			for _, tc := range choice.Delta.ToolCalls {
				acc := toolCalls[tc.Index]
				if acc == nil {
					acc = &responsesStreamToolCallAccum{
						Index:       tc.Index,
						OutputIndex: -1,
					}
					toolCalls[tc.Index] = acc
					toolOrder = append(toolOrder, tc.Index)
				}
				if tc.ID != "" {
					acc.ID = tc.ID
				}
				if tc.Function.Name != "" {
					acc.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.Arguments += tc.Function.Arguments
					acc.PendingArguments += tc.Function.Arguments
				}
				if acc.Name != "" && acc.ID != "" && !acc.Added {
					if acc.OutputIndex < 0 {
						acc.OutputIndex = nextOutputIndex
						nextOutputIndex++
					}
					acc.ItemID = "fc_" + shortSHA256(acc.ID)
					writeResponsesSSE(w, "response.output_item.added", map[string]interface{}{
						"type":            "response.output_item.added",
						"sequence_number": seq,
						"output_index":    acc.OutputIndex,
						"item": map[string]interface{}{
							"id":        acc.ItemID,
							"type":      "function_call",
							"status":    "in_progress",
							"call_id":   acc.ID,
							"name":      acc.Name,
							"arguments": "",
						},
					})
					seq++
					acc.Added = true
				}
				if acc.Added && acc.PendingArguments != "" {
					writeResponsesSSE(w, "response.function_call_arguments.delta", map[string]interface{}{
						"type":            "response.function_call_arguments.delta",
						"sequence_number": seq,
						"item_id":         acc.ItemID,
						"output_index":    acc.OutputIndex,
						"delta":           acc.PendingArguments,
					})
					seq++
					acc.PendingArguments = ""
					flusher.Flush()
				}
			}
		}
		return true
	}
	streamErr := readOpenAIStreamEvents(upResp.Body, handlePayload)
	if streamErr == nil && payloadErr != nil {
		streamErr = payloadErr
	}
	if streamErr != nil {
		log.Printf("[codegenproxy] responses stream read failed id=%s model=%q err=%v", reqID, model, streamErr)
		writeResponsesSSE(w, "error", map[string]interface{}{
			"type":    "error",
			"message": "upstream stream read failed",
		})
		flusher.Flush()
		return
	}

	outputText := text.String()
	if textStarted {
		writeResponsesSSE(w, "response.output_text.done", map[string]interface{}{
			"type":            "response.output_text.done",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"text":            outputText,
			"logprobs":        []interface{}{},
		})
		seq++
		writeResponsesSSE(w, "response.content_part.done", map[string]interface{}{
			"type":            "response.content_part.done",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"part": map[string]interface{}{
				"type":        "output_text",
				"text":        outputText,
				"annotations": []interface{}{},
			},
		})
		seq++
		writeResponsesSSE(w, "response.output_item.done", map[string]interface{}{
			"type":            "response.output_item.done",
			"sequence_number": seq,
			"output_index":    textOutputIndex,
			"item": map[string]interface{}{
				"id":     msgID,
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []interface{}{map[string]interface{}{
					"type":        "output_text",
					"text":        outputText,
					"annotations": []interface{}{},
				}},
			},
		})
		seq++
	}
	for _, idx := range toolOrder {
		acc := toolCalls[idx]
		if acc == nil || acc.Name == "" {
			continue
		}
		if acc.ID == "" {
			acc.ID = fmt.Sprintf("call_%s_%d", shortSHA256(respID), idx)
		}
		if acc.ItemID == "" {
			acc.ItemID = "fc_" + shortSHA256(acc.ID)
		}
		if acc.OutputIndex < 0 {
			acc.OutputIndex = nextOutputIndex
			nextOutputIndex++
		}
		if !acc.Added {
			writeResponsesSSE(w, "response.output_item.added", map[string]interface{}{
				"type":            "response.output_item.added",
				"sequence_number": seq,
				"output_index":    acc.OutputIndex,
				"item": map[string]interface{}{
					"id":        acc.ItemID,
					"type":      "function_call",
					"status":    "in_progress",
					"call_id":   acc.ID,
					"name":      acc.Name,
					"arguments": "",
				},
			})
			seq++
		}
		if acc.PendingArguments != "" {
			writeResponsesSSE(w, "response.function_call_arguments.delta", map[string]interface{}{
				"type":            "response.function_call_arguments.delta",
				"sequence_number": seq,
				"item_id":         acc.ItemID,
				"output_index":    acc.OutputIndex,
				"delta":           acc.PendingArguments,
			})
			seq++
			acc.PendingArguments = ""
		}
		writeResponsesSSE(w, "response.function_call_arguments.done", map[string]interface{}{
			"type":            "response.function_call_arguments.done",
			"sequence_number": seq,
			"item_id":         acc.ItemID,
			"output_index":    acc.OutputIndex,
			"arguments":       acc.Arguments,
		})
		seq++
		writeResponsesSSE(w, "response.output_item.done", map[string]interface{}{
			"type":            "response.output_item.done",
			"sequence_number": seq,
			"output_index":    acc.OutputIndex,
			"item": map[string]interface{}{
				"id":        acc.ItemID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   acc.ID,
				"name":      acc.Name,
				"arguments": acc.Arguments,
			},
		})
		seq++
	}
	writeResponsesSSE(w, "response.completed", map[string]interface{}{
		"type":            "response.completed",
		"sequence_number": seq,
		"response":        responsesStreamResponseObject(respID, model, outputText, true, textOutputIndex, toolOrder, toolCalls),
	})
	flusher.Flush()
}

type responsesStreamToolCallAccum struct {
	Index            int
	OutputIndex      int
	ID               string
	ItemID           string
	Name             string
	Arguments        string
	PendingArguments string
	Added            bool
}

func responsesStreamResponseObject(id, model, text string, completed bool, textOutputIndex int, toolOrder []int, toolCalls map[int]*responsesStreamToolCallAccum) map[string]interface{} {
	status := "in_progress"
	output := []interface{}{}
	if completed {
		status = "completed"
		type outputItem struct {
			index int
			item  interface{}
		}
		var items []outputItem
		hasTools := len(toolOrder) > 0
		if text != "" || !hasTools {
			if textOutputIndex < 0 {
				textOutputIndex = 0
			}
			items = append(items, outputItem{index: textOutputIndex, item: map[string]interface{}{
				"id":     "msg_" + shortSHA256(id+":message"),
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []interface{}{map[string]interface{}{
					"type":        "output_text",
					"text":        text,
					"annotations": []interface{}{},
				}},
			}})
		}
		for _, idx := range toolOrder {
			acc := toolCalls[idx]
			if acc == nil || acc.Name == "" {
				continue
			}
			items = append(items, outputItem{index: acc.OutputIndex, item: map[string]interface{}{
				"id":        firstNonEmptyCodegenProxy(acc.ItemID, "fc_"+shortSHA256(acc.ID)),
				"type":      "function_call",
				"status":    "completed",
				"call_id":   acc.ID,
				"name":      acc.Name,
				"arguments": acc.Arguments,
			}})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].index < items[j].index })
		for _, item := range items {
			output = append(output, item.item)
		}
	}
	return map[string]interface{}{
		"id":                  id,
		"object":              "response",
		"created_at":          float64(time.Now().Unix()),
		"status":              status,
		"model":               model,
		"output":              output,
		"parallel_tool_calls": false,
		"tools":               []interface{}{},
		"tool_choice":         "auto",
		"temperature":         0,
		"top_p":               0,
		"metadata":            map[string]interface{}{},
		"instructions":        nil,
		"incomplete_details":  nil,
		"error":               nil,
		"usage":               map[string]interface{}{},
	}
}

// handleMessages receives Anthropic Messages API requests, converts to
// OpenAI chat completions, forwards to upstream CodeGen, converts back.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	reqID := newLogRequestID()
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	upURL, fallbackKey, clientName, clientKey := s.getConfig()
	if upURL == "" {
		log.Printf("[codegenproxy] anthropic request id=%s rejected: upstream not configured", reqID)
		writeError(w, http.StatusServiceUnavailable, "upstream not configured")
		return
	}
	if !s.authorizeClient(r, clientKey) {
		log.Printf("[codegenproxy] anthropic request id=%s rejected: invalid proxy api key", reqID)
		writeError(w, http.StatusUnauthorized, "invalid proxy api key")
		return
	}

	// Claude Code sends token via x-api-key / Authorization: Bearer.
	// Fall back to the server-configured key (from SSO) if absent.
	apiKey := fallbackKey
	if clientKey == "" {
		apiKey = resolveAPIKey(r, fallbackKey)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		log.Printf("[codegenproxy] anthropic read body error id=%s err=%v", reqID, err)
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var anthReq anthropicRequest
	if err := json.Unmarshal(body, &anthReq); err != nil {
		log.Printf("[codegenproxy] anthropic parse error id=%s err=%v body=%s", reqID, err, truncateForLog(body, 4096))
		writeError(w, http.StatusBadRequest, "parse body: "+err.Error())
		return
	}
	originalModel := anthReq.Model
	anthReq.Model = normalizeModelIdentifier(anthReq.Model)
	if resolvedModel := s.resolveCodeGenModelAlias(r.Context(), upURL, apiKey, clientName, anthReq.Model); resolvedModel != "" && resolvedModel != anthReq.Model {
		log.Printf("[codegenproxy] anthropic model alias resolved id=%s original_model=%q resolved_model=%q", reqID, anthReq.Model, resolvedModel)
		anthReq.Model = resolvedModel
	}
	log.Printf("[codegenproxy] anthropic request id=%s original_model=%q normalized_model=%q stream=%v messages=%d tools=%d system=%t accept=%q anthropic_version=%q beta=%q",
		reqID, originalModel, anthReq.Model, anthReq.Stream, len(anthReq.Messages), len(anthReq.Tools), anthReq.System != nil,
		r.Header.Get("Accept"), r.Header.Get("anthropic-version"), r.Header.Get("anthropic-beta"))

	// Convert Anthropic → OpenAI
	openaiReq := convertAnthropicToOpenAI(anthReq)
	compatibilityNotes := applyCodeGenOpenAIRequestCompatibility(&openaiReq)
	toolSchemas := summarizeOpenAIToolSchemas(openaiReq.Tools)

	reqData, _ := json.Marshal(openaiReq)
	requestSummary := summarizeOpenAIRequest(reqData)

	// Forward to upstream CodeGen using standard OpenAI Bearer auth.
	upEndpoint := codeGenProxyChatCompletionsEndpoint(upURL, clientName)
	normalizedClient := corelib.NormalizeCodeGenClientName(clientName)
	log.Printf("[codegenproxy] anthropic upstream request id=%s endpoint=%q client=%q user_agent=%q codegen_client=%q model=%q summary=%s compatibility=%s",
		reqID, upEndpoint, clientName, normalizedClient, normalizedClient, anthReq.Model, requestSummary, strings.Join(compatibilityNotes, ","))

	upResp, err := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, reqData, r.Header.Get("Accept"), anthReq.Stream)
	if err != nil {
		log.Printf("[codegenproxy] upstream request failed id=%s model=%q stream=%v client=%q endpoint=%q request=%s err=%v",
			reqID, anthReq.Model, anthReq.Stream, clientName, upEndpoint, truncateForLog(reqData, 4096), err)
		writeError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer upResp.Body.Close()

	if upResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
		if retryBody, ok := prepareQwenFlashChatOnlyRetryBody(anthReq.Model, reqData, upResp.StatusCode); ok {
			log.Printf("[codegenproxy] anthropic retry without tools id=%s model=%q stream=%v original_status=%d original_response=%s retry_summary=%s",
				reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, truncateForLog(respBody, 2048), summarizeOpenAIRequest(retryBody))
			retryResp, retryErr := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, retryBody, r.Header.Get("Accept"), anthReq.Stream)
			if retryErr != nil {
				log.Printf("[codegenproxy] anthropic retry without tools failed id=%s model=%q stream=%v err=%v", reqID, anthReq.Model, anthReq.Stream, retryErr)
			} else {
				upResp = retryResp
				defer upResp.Body.Close()
				reqData = retryBody
				if upResp.StatusCode == http.StatusOK {
					log.Printf("[codegenproxy] anthropic retry without tools succeeded id=%s model=%q stream=%v status=%d content_type=%q",
						reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, upResp.Header.Get("Content-Type"))
					respBody = nil
				} else {
					respBody, _ = io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
					log.Printf("[codegenproxy] anthropic retry without tools error id=%s model=%q stream=%v status=%d content_type=%q response=%s",
						reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, upResp.Header.Get("Content-Type"), truncateForLog(respBody, 2048))
				}
			}
		}
		if compactBody, ok := prepareCodeGenCompactChatRetryBody(upURL, anthReq.Model, reqData, upResp.StatusCode); ok {
			log.Printf("[codegenproxy] anthropic compact retry id=%s model=%q stream=%v original_status=%d original_response=%s retry_summary=%s",
				reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, truncateForLog(respBody, 2048), summarizeOpenAIRequest(compactBody))
			compactResp, compactErr := s.getUpstreamClient().DoChatCompletions(r.Context(), upEndpoint, apiKey, clientName, compactBody, r.Header.Get("Accept"), anthReq.Stream)
			if compactErr != nil {
				log.Printf("[codegenproxy] anthropic compact retry failed id=%s model=%q stream=%v err=%v", reqID, anthReq.Model, anthReq.Stream, compactErr)
			} else {
				upResp = compactResp
				defer upResp.Body.Close()
				reqData = compactBody
				if upResp.StatusCode == http.StatusOK {
					log.Printf("[codegenproxy] anthropic compact retry succeeded id=%s model=%q stream=%v status=%d content_type=%q",
						reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, upResp.Header.Get("Content-Type"))
					respBody = nil
				} else {
					respBody, _ = io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
					log.Printf("[codegenproxy] anthropic compact retry error id=%s model=%q stream=%v status=%d content_type=%q response=%s",
						reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, upResp.Header.Get("Content-Type"), truncateForLog(respBody, 2048))
				}
			}
		}
		if upResp.StatusCode != http.StatusOK {
			log.Printf("[codegenproxy] upstream error id=%s model=%q stream=%v client=%q endpoint=%q status=%d content_type=%q request=%s response=%s",
				reqID, anthReq.Model, anthReq.Stream, clientName, upEndpoint, upResp.StatusCode, upResp.Header.Get("Content-Type"), truncateForLog(reqData, 4096), truncateForLog(respBody, 4096))
			upResp.Body = io.NopCloser(bytes.NewReader(respBody))
		}
	} else {
		log.Printf("[codegenproxy] anthropic upstream response id=%s model=%q stream=%v status=%d content_type=%q",
			reqID, anthReq.Model, anthReq.Stream, upResp.StatusCode, upResp.Header.Get("Content-Type"))
	}

	if anthReq.Stream {
		s.handleStreamResponse(w, upResp, anthReq.Model, reqID, toolSchemas)
	} else {
		s.handleNonStreamResponse(w, upResp, anthReq.Model, reqID, toolSchemas)
	}
}

// handleNonStreamResponse converts an OpenAI non-streaming response to Anthropic format.
func (s *Server) handleNonStreamResponse(w http.ResponseWriter, upResp *http.Response, model, reqID string, toolSchemas map[string]toolSchemaSummary) {
	respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 10*1024*1024))

	if upResp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": truncate(string(respBody), 1024),
			},
		})
		return
	}

	var openaiResp openaiChatResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		writeError(w, http.StatusBadGateway, "parse upstream: "+err.Error())
		return
	}
	if err := validateOpenAIResponseToolUses(openaiResp); err != nil {
		log.Printf("[codegenproxy] non-stream tool call invalid id=%s model=%q err=%v", reqID, model, err)
		writeError(w, http.StatusBadGateway, "upstream response produced invalid tool call")
		return
	}

	anthResp := convertOpenAIToAnthropic(openaiResp, model)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthResp)
}

// handleStreamResponse converts an OpenAI SSE stream to Anthropic SSE stream.
func (s *Server) handleStreamResponse(w http.ResponseWriter, upResp *http.Response, model, reqID string, toolSchemas map[string]toolSchemaSummary) {
	if upResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 256*1024))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upResp.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": truncate(string(respBody), 1024),
			},
		})
		return
	}

	// Some gateways return non-SSE content-type even for stream=true.
	// If the body is clearly not SSE, fall back to non-stream parsing.
	ct := upResp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		s.handleNonStreamResponse(w, upResp, model, reqID, toolSchemas)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Anthropic SSE: message_start → content_block_start/delta/stop → message_delta → message_stop
	msgID := fmt.Sprintf("msg_proxy_%d", time.Now().UnixNano())
	writeSSE(w, "message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id": msgID, "type": "message", "role": "assistant",
			"model": model, "content": []interface{}{},
			"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	})
	flusher.Flush()

	blockIdx := 0
	textStarted := false
	var stopReason string
	textBytes := 0
	var textBuf strings.Builder
	bufferTextForToolCalls := len(toolSchemas) > 0
	toolCalls := make(map[int]*streamToolCallAccum)
	var toolOrder []int
	var legacyFunction *streamToolCallAccum
	var payloadErr error

	handlePayload := func(payload string) bool {
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			return false
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			log.Printf("[codegenproxy] stream chunk parse failed id=%s model=%q payload_bytes=%d payload_sha256=%s err=%v",
				reqID, model, len(payload), shortSHA256(payload), err)
			payloadErr = fmt.Errorf("parse upstream stream chunk: %w", err)
			return false
		}
		if len(chunk.Choices) == 0 {
			return true
		}
		delta := chunk.Choices[0].Delta
		finish := chunk.Choices[0].FinishReason

		// ── text content ──
		if delta.Content != "" {
			textBytes += len(delta.Content)
			if bufferTextForToolCalls {
				textBuf.WriteString(delta.Content)
				maybeFlushStreamTextBuffer(w, flusher, blockIdx, &textStarted, &textBuf)
			} else {
				writeStreamTextDelta(w, flusher, blockIdx, &textStarted, delta.Content)
			}
		}

		// Buffer tool calls until finish_reason. Some OpenAI-compatible
		// streams send name/id/arguments across separate deltas; emitting a
		// tool_use before the name or full JSON arrives makes Claude Code reject
		// it as invalid parameters.
		for _, tc := range delta.ToolCalls {
			acc := toolCalls[tc.Index]
			if acc == nil {
				acc = &streamToolCallAccum{Index: tc.Index}
				toolCalls[tc.Index] = acc
				toolOrder = append(toolOrder, tc.Index)
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			acc.Arguments += tc.Function.Arguments
		}

		if delta.FunctionCall != nil {
			if legacyFunction == nil {
				legacyFunction = &streamToolCallAccum{
					Index: 0,
					ID:    "call_legacy_function",
					Name:  "legacy_function",
				}
			}
			if delta.FunctionCall.Name != "" {
				legacyFunction.Name = delta.FunctionCall.Name
			}
			legacyFunction.Arguments += delta.FunctionCall.Arguments
		}

		if finish != "" {
			stopReason = finish
		}
		return true
	}

	streamErr := readOpenAIStreamEvents(upResp.Body, handlePayload)
	if streamErr == nil && payloadErr != nil {
		streamErr = payloadErr
	}
	if streamErr != nil {
		log.Printf("[codegenproxy] stream read failed id=%s model=%q max_event_bytes=%d err=%v",
			reqID, model, codeGenStreamMaxEventBytes, streamErr)
	}

	if textStarted {
		writeSSE(w, "content_block_stop", map[string]interface{}{
			"type": "content_block_stop", "index": blockIdx,
		})
		blockIdx++
		flusher.Flush()
	}

	if streamErr != nil {
		log.Printf("[codegenproxy] stream abort id=%s model=%q text_bytes=%d tool_calls=%d legacy_function=%t tool_summary=%s",
			reqID, model, textBytes, len(toolOrder), legacyFunction != nil, summarizeStreamTools(toolOrder, toolCalls, legacyFunction, toolSchemas))
		writeStreamError(w, "upstream stream read failed")
		flusher.Flush()
		return
	}

	if err := validateBufferedToolUses(toolOrder, toolCalls, legacyFunction); err != nil {
		log.Printf("[codegenproxy] stream tool call invalid id=%s model=%q err=%v tool_summary=%s",
			reqID, model, err, summarizeStreamTools(toolOrder, toolCalls, legacyFunction, toolSchemas))
		writeStreamError(w, "upstream stream produced invalid tool call")
		flusher.Flush()
		return
	}

	var contentToolCalls []*streamToolCallAccum
	hasBufferedToolCalls := len(toolOrder) > 0 || legacyFunction != nil
	if textBuf.Len() > 0 {
		if hasBufferedToolCalls {
			if blocks, malformed := contentToolCallsToStreamAccums(textBuf.String()); len(blocks) == 0 && !malformed {
				blockIdx = writeBufferedText(w, flusher, blockIdx, textBuf.String())
			}
		} else {
			var malformedContentToolCall bool
			contentToolCalls, malformedContentToolCall = contentToolCallsToStreamAccums(textBuf.String())
			if malformedContentToolCall {
				log.Printf("[codegenproxy] stream content tool call malformed id=%s model=%q text_bytes=%d text_sha256=%s",
					reqID, model, textBytes, shortSHA256(textBuf.String()))
				writeStreamError(w, "upstream stream produced malformed content tool call")
				flusher.Flush()
				return
			}
			if len(contentToolCalls) > 0 {
				for _, acc := range contentToolCalls {
					blockIdx = writeBufferedToolUse(w, flusher, blockIdx, acc)
				}
			} else {
				blockIdx = writeBufferedText(w, flusher, blockIdx, textBuf.String())
			}
		}
	}

	if len(toolOrder) > 0 {
		for _, idx := range toolOrder {
			acc := toolCalls[idx]
			if acc == nil {
				continue
			}
			blockIdx = writeBufferedToolUse(w, flusher, blockIdx, acc)
		}
	}
	if legacyFunction != nil {
		blockIdx = writeBufferedToolUse(w, flusher, blockIdx, legacyFunction)
	}

	anthStop := "end_turn"
	switch stopReason {
	case "tool_calls", "function_call":
		anthStop = "tool_use"
	case "length":
		anthStop = "max_tokens"
	}
	if len(contentToolCalls) > 0 {
		anthStop = "tool_use"
	}
	log.Printf("[codegenproxy] stream complete id=%s model=%q openai_finish=%q anthropic_stop=%q text_bytes=%d tool_calls=%d legacy_function=%t content_tool_calls=%d tool_summary=%s content_tool_summary=%s",
		reqID, model, stopReason, anthStop, textBytes, len(toolOrder), legacyFunction != nil, len(contentToolCalls),
		summarizeStreamTools(toolOrder, toolCalls, legacyFunction, toolSchemas),
		summarizeStreamToolList(contentToolCalls, toolSchemas))

	writeSSE(w, "message_delta", map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": anthStop, "stop_sequence": nil},
		"usage": map[string]int{"output_tokens": 0},
	})
	flusher.Flush()

	writeSSE(w, "message_stop", map[string]interface{}{"type": "message_stop"})
	flusher.Flush()
}

type streamToolCallAccum struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

func contentToolCallsToStreamAccums(content string) ([]*streamToolCallAccum, bool) {
	if !mayContainContentToolCall(content) {
		return nil, false
	}
	calls, malformed := llmcompat.ParseContentToolCallsDetailed(content)
	if len(calls) == 0 {
		return nil, malformed
	}
	accs := make([]*streamToolCallAccum, 0, len(calls))
	skippedMalformed := false
	for i, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			skippedMalformed = true
			continue
		}
		args, ok := normalizeOpenAIToolArguments(call.Function.Arguments)
		if !ok {
			skippedMalformed = true
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = fmt.Sprintf("call_content_%d_%d", time.Now().UnixNano(), i)
		}
		accs = append(accs, &streamToolCallAccum{
			Index:     i,
			ID:        id,
			Name:      name,
			Arguments: args,
		})
	}
	return accs, malformed || skippedMalformed
}

func maybeFlushStreamTextBuffer(w http.ResponseWriter, flusher http.Flusher, blockIdx int, textStarted *bool, buf *strings.Builder) {
	if buf == nil || buf.Len() <= codeGenContentToolScanFlushBytes {
		return
	}
	text := buf.String()
	if mayContainContentToolCall(text) {
		return
	}
	retain := codeGenContentToolScanRetainBytes
	if retain > len(text) {
		retain = len(text)
	}
	flushText := text[:len(text)-retain]
	if flushText == "" {
		return
	}
	writeStreamTextDelta(w, flusher, blockIdx, textStarted, flushText)
	buf.Reset()
	buf.WriteString(text[len(text)-retain:])
}

func writeStreamTextDelta(w http.ResponseWriter, flusher http.Flusher, blockIdx int, textStarted *bool, text string) {
	if text == "" {
		return
	}
	if textStarted != nil && !*textStarted {
		writeSSE(w, "content_block_start", map[string]interface{}{
			"type": "content_block_start", "index": blockIdx,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		})
		flusher.Flush()
		*textStarted = true
	}
	writeSSE(w, "content_block_delta", map[string]interface{}{
		"type": "content_block_delta", "index": blockIdx,
		"delta": map[string]interface{}{"type": "text_delta", "text": text},
	})
	flusher.Flush()
}

func readOpenAIStreamEvents(r io.Reader, handle func(string) bool) error {
	reader := bufio.NewReaderSize(r, 64*1024)
	var data strings.Builder
	flush := func() bool {
		if data.Len() == 0 {
			return true
		}
		payload := data.String()
		data.Reset()
		return handle(payload)
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				if !flush() {
					return nil
				}
			case strings.HasPrefix(line, ":"):
				// SSE comment/heartbeat.
			case strings.HasPrefix(line, "data:"):
				part := strings.TrimPrefix(line, "data:")
				if strings.HasPrefix(part, " ") {
					part = strings.TrimPrefix(part, " ")
				}
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(part)
				if data.Len() > codeGenStreamMaxEventBytes {
					return fmt.Errorf("stream event exceeded %d bytes", codeGenStreamMaxEventBytes)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				if !flush() {
					return nil
				}
				return nil
			}
			return err
		}
	}
}

func shortSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", sum[:8])
}

type toolSchemaSummary struct {
	Required map[string]struct{}
	Props    map[string]struct{}
}

func summarizeOpenAIToolSchemas(tools []openaiTool) map[string]toolSchemaSummary {
	if len(tools) == 0 {
		return nil
	}
	result := make(map[string]toolSchemaSummary, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		summary := toolSchemaSummary{
			Required: map[string]struct{}{},
			Props:    map[string]struct{}{},
		}
		if schema, ok := tool.Function.Parameters.(map[string]interface{}); ok {
			if props, ok := schema["properties"].(map[string]interface{}); ok {
				for key := range props {
					summary.Props[key] = struct{}{}
				}
			}
			if required, ok := schema["required"].([]interface{}); ok {
				for _, item := range required {
					if key, ok := item.(string); ok && key != "" {
						summary.Required[key] = struct{}{}
					}
				}
			}
		}
		result[name] = summary
	}
	return result
}

func summarizeStreamTools(order []int, calls map[int]*streamToolCallAccum, legacy *streamToolCallAccum, schemas map[string]toolSchemaSummary) string {
	var parts []string
	for _, idx := range order {
		if acc := calls[idx]; acc != nil {
			parts = append(parts, summarizeStreamTool(acc, schemas))
		}
	}
	if legacy != nil {
		parts = append(parts, "legacy:"+summarizeStreamTool(legacy, schemas))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

func summarizeStreamToolList(calls []*streamToolCallAccum, schemas map[string]toolSchemaSummary) string {
	if len(calls) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(calls))
	for _, acc := range calls {
		if acc != nil {
			parts = append(parts, summarizeStreamTool(acc, schemas))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

func summarizeStreamTool(acc *streamToolCallAccum, schemas map[string]toolSchemaSummary) string {
	name := strings.TrimSpace(acc.Name)
	if name == "" {
		name = "unknown_tool"
	}
	args := strings.TrimSpace(acc.Arguments)
	keys := topLevelJSONKeys(args)
	schemaNote := summarizeToolSchemaMatch(name, keys, schemas)
	return fmt.Sprintf("%s(args_bytes=%d,args_kind=%s,args_keys=%s%s)", name, len(args), classifyToolArguments(args), strings.Join(keys, "|"), schemaNote)
}

func topLevelJSONKeys(raw string) []string {
	normalized, ok := normalizeOpenAIToolArguments(raw)
	if !ok {
		return nil
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &input); err != nil {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func summarizeToolSchemaMatch(name string, keys []string, schemas map[string]toolSchemaSummary) string {
	if len(schemas) == 0 {
		return ""
	}
	schema, ok := schemas[name]
	if !ok {
		return ",schema=missing_tool"
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	missing := make([]string, 0)
	for key := range schema.Required {
		if _, ok := keySet[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return ",schema_missing=" + strings.Join(missing, "|")
	}
	return ",schema=ok"
}

func validateBufferedToolUses(order []int, calls map[int]*streamToolCallAccum, legacy *streamToolCallAccum) error {
	for _, idx := range order {
		if acc := calls[idx]; acc != nil {
			if err := validateBufferedToolUse(acc); err != nil {
				return err
			}
		}
	}
	if legacy != nil {
		if err := validateBufferedToolUse(legacy); err != nil {
			return err
		}
	}
	return nil
}

func validateBufferedToolUse(acc *streamToolCallAccum) error {
	name := strings.TrimSpace(acc.Name)
	if name == "" {
		return fmt.Errorf("tool call index=%d missing name", acc.Index)
	}
	args := strings.TrimSpace(acc.Arguments)
	if args == "" {
		acc.Arguments = "{}"
		return nil
	}
	normalized, ok := normalizeOpenAIToolArguments(args)
	if !ok {
		return fmt.Errorf("tool call index=%d name=%q has invalid tool arguments kind=%s bytes=%d args_sha256=%s", acc.Index, name, classifyToolArguments(args), len(args), shortSHA256(args))
	}
	acc.Arguments = normalized
	return nil
}

func validateOpenAIResponseToolUses(resp openaiChatResponse) error {
	for choiceIdx, choice := range resp.Choices {
		for toolIdx, tc := range choice.Message.ToolCalls {
			name := strings.TrimSpace(tc.Function.Name)
			if name == "" {
				return fmt.Errorf("choice=%d tool_call=%d missing name", choiceIdx, toolIdx)
			}
			args := strings.TrimSpace(tc.Function.Arguments)
			if _, ok := normalizeOpenAIToolArguments(args); !ok {
				return fmt.Errorf("choice=%d tool_call=%d name=%q has invalid tool arguments kind=%s bytes=%d args_sha256=%s", choiceIdx, toolIdx, name, classifyToolArguments(args), len(args), shortSHA256(args))
			}
		}
		if fc := choice.Message.FunctionCall; fc != nil {
			name := strings.TrimSpace(fc.Name)
			if name == "" {
				return fmt.Errorf("choice=%d legacy function missing name", choiceIdx)
			}
			args := strings.TrimSpace(fc.Arguments)
			if _, ok := normalizeOpenAIToolArguments(args); !ok {
				return fmt.Errorf("choice=%d legacy function name=%q has invalid tool arguments kind=%s bytes=%d args_sha256=%s", choiceIdx, name, classifyToolArguments(args), len(args), shortSHA256(args))
			}
		}
	}
	return nil
}

func classifyToolArguments(raw string) string {
	args := strings.TrimSpace(raw)
	if args == "" {
		return "empty"
	}
	var object map[string]interface{}
	if err := json.Unmarshal([]byte(args), &object); err == nil && object != nil {
		return "object"
	}
	var encoded string
	if err := json.Unmarshal([]byte(args), &encoded); err == nil {
		if strings.EqualFold(args, "null") {
			return "null"
		}
		encoded = strings.TrimSpace(encoded)
		var encodedObject map[string]interface{}
		if err := json.Unmarshal([]byte(encoded), &encodedObject); err == nil && encodedObject != nil {
			return "encoded_object"
		}
		return "string"
	}
	var array []interface{}
	if err := json.Unmarshal([]byte(args), &array); err == nil {
		return "array"
	}
	return "invalid_json"
}

func writeBufferedToolUse(w http.ResponseWriter, flusher http.Flusher, blockIdx int, acc *streamToolCallAccum) int {
	if acc == nil {
		return blockIdx
	}
	id := strings.TrimSpace(acc.ID)
	if id == "" {
		id = fmt.Sprintf("call_%d", time.Now().UnixNano())
	}
	name := strings.TrimSpace(acc.Name)
	if name == "" {
		name = "unknown_tool"
	}
	args := strings.TrimSpace(acc.Arguments)
	if args == "" {
		args = "{}"
	}
	writeSSE(w, "content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": blockIdx,
		"content_block": map[string]interface{}{
			"type": "tool_use", "id": id,
			"name": name, "input": map[string]interface{}{},
		},
	})
	flusher.Flush()
	writeSSE(w, "content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": blockIdx,
		"delta": map[string]interface{}{
			"type": "input_json_delta", "partial_json": args,
		},
	})
	flusher.Flush()
	writeSSE(w, "content_block_stop", map[string]interface{}{
		"type": "content_block_stop", "index": blockIdx,
	})
	flusher.Flush()
	return blockIdx + 1
}

func writeBufferedText(w http.ResponseWriter, flusher http.Flusher, blockIdx int, text string) int {
	writeSSE(w, "content_block_start", map[string]interface{}{
		"type": "content_block_start", "index": blockIdx,
		"content_block": map[string]interface{}{"type": "text", "text": ""},
	})
	flusher.Flush()
	writeSSE(w, "content_block_delta", map[string]interface{}{
		"type": "content_block_delta", "index": blockIdx,
		"delta": map[string]interface{}{"type": "text_delta", "text": text},
	})
	flusher.Flush()
	writeSSE(w, "content_block_stop", map[string]interface{}{
		"type": "content_block_stop", "index": blockIdx,
	})
	flusher.Flush()
	return blockIdx + 1
}

// writeSSE writes a single Anthropic SSE event.
func writeSSE(w http.ResponseWriter, event string, data interface{}) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
}

func writeResponsesSSE(w http.ResponseWriter, event string, data interface{}) {
	writeSSE(w, event, data)
}

func writeStreamError(w http.ResponseWriter, message string) {
	writeSSE(w, "error", map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": message,
		},
	})
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    "api_error",
			"message": msg,
		},
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func truncateForLog(body []byte, n int) string {
	if len(body) == 0 {
		return ""
	}
	return truncate(redactLogBody(body), n)
}

func newLogRequestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func extractJSONFieldString(body []byte, key string) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func extractModelIDsFromModelsBody(body []byte) []string {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	entries, ok := modelEntries(raw)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		if id := modelField(m, "id", "name", "display_name"); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func applyCodeGenOpenAICompatibility(body []byte) ([]byte, []string) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}
	model, _ := payload["model"].(string)
	notes := applyCodeGenOpenAIMapCompatibility(payload, model)
	if len(notes) == 0 {
		return body, nil
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return body, nil
	}
	return out, notes
}

func applyCodeGenOpenAIMapCompatibility(payload map[string]interface{}, model string) []string {
	if payload == nil {
		return nil
	}
	var notes []string
	if messages := codeGenProxySliceFromAny(payload["messages"]); len(messages) > 0 {
		sanitized := llmcompat.SanitizeOpenAICompatRequestMessages(messages, true)
		if !reflect.DeepEqual(sanitized, messages) {
			payload["messages"] = sanitized
			notes = append(notes, fmt.Sprintf("codegen_sanitize_messages:%d", len(sanitized)))
		}
	}
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
		if _, ok := payload[key]; ok {
			delete(payload, key)
			notes = append(notes, "codegen_drop_"+key)
		}
	}
	if tools, ok := payload["tools"]; ok {
		sanitized := corelib.SanitizeCodeGenOpenAIChatToolsValue(tools)
		if !reflect.DeepEqual(sanitized, tools) {
			payload["tools"] = sanitized
			notes = append(notes, fmt.Sprintf("codegen_sanitize_tools:%d", logArrayLen(sanitized)))
		}
	}
	if functions, ok := payload["functions"]; ok {
		sanitized := corelib.SanitizeCodeGenOpenAIFunctionsValue(functions)
		if !reflect.DeepEqual(sanitized, functions) {
			payload["functions"] = sanitized
			notes = append(notes, fmt.Sprintf("codegen_sanitize_functions:%d", logArrayLen(sanitized)))
		}
	}
	if !isQwenFlashModel(model) {
		return notes
	}
	if before, after, changed := sanitizeQwenFlashSystemMessages(payload); changed {
		notes = append(notes, fmt.Sprintf("qwen_flash_sanitize_system:%d->%d", before, after))
	}
	if before, after, changed := mergeQwenFlashAdjacentMessages(payload); changed {
		notes = append(notes, fmt.Sprintf("qwen_flash_merge_messages:%d->%d", before, after))
	}
	maxTokens, ok := numericJSONInt(payload["max_tokens"])
	if ok && maxTokens > codeGenQwenFlashMaxTokens {
		payload["max_tokens"] = codeGenQwenFlashMaxTokens
		notes = append(notes, fmt.Sprintf("qwen_flash_max_tokens:%d->%d", maxTokens, codeGenQwenFlashMaxTokens))
	}
	if tools := codeGenProxySliceFromAny(payload["tools"]); len(tools) > 0 {
		sanitized := 0
		for _, tool := range tools {
			toolMap := codeGenProxyMapFromAny(tool)
			if toolMap == nil {
				continue
			}
			if fn := codeGenProxyMapFromAny(toolMap["function"]); fn != nil {
				if sanitizeQwenFlashToolFunctionMap(fn) {
					sanitized++
				}
			}
		}
		if sanitized > 0 {
			notes = append(notes, fmt.Sprintf("qwen_flash_sanitize_tools:%d", sanitized))
		}
	}
	return notes
}

func applyCodeGenOpenAIRequestCompatibility(req *openaiChatRequest) []string {
	if req == nil {
		return nil
	}
	var notes []string
	if sanitized := sanitizeCodeGenOpenAITypedFunctions(req.Tools, req.Functions); sanitized > 0 {
		notes = append(notes, fmt.Sprintf("codegen_sanitize_tool_schemas:%d", sanitized))
	}
	if isQwenFlashModel(req.Model) {
		if beforeMessages, afterMessages, beforeBytes, afterBytes, changed := normalizeQwenFlashAnthropicSystemMessages(&req.Messages); changed {
			notes = append(notes, fmt.Sprintf("qwen_flash_normalize_system:messages:%d->%d,bytes:%d->%d", beforeMessages, afterMessages, beforeBytes, afterBytes))
		}
		if before, after, changed := mergeQwenFlashOpenAIAdjacentMessages(&req.Messages); changed {
			notes = append(notes, fmt.Sprintf("qwen_flash_merge_messages:%d->%d", before, after))
		}
		if toolResults, repeatedToolCalls, ok := detectQwenFlashRepeatedToolLoop(req.Messages); ok && len(req.Tools) > 0 {
			notes = append(notes, fmt.Sprintf("qwen_flash_tool_loop_suspected:tool_results:%d,repeated_tool_calls:%d", toolResults, repeatedToolCalls))
		}
	}
	if isQwenFlashModel(req.Model) && req.MaxTokens > codeGenQwenFlashMaxTokens {
		notes = append(notes, fmt.Sprintf("qwen_flash_max_tokens:%d->%d", req.MaxTokens, codeGenQwenFlashMaxTokens))
		req.MaxTokens = codeGenQwenFlashMaxTokens
	}
	if isQwenFlashModel(req.Model) && len(req.Tools) > 0 {
		sanitized := 0
		for i := range req.Tools {
			if sanitizeQwenFlashOpenAIFunction(&req.Tools[i].Function) {
				sanitized++
			}
		}
		if sanitized > 0 {
			notes = append(notes, fmt.Sprintf("qwen_flash_sanitize_tools:%d", sanitized))
		}
	}
	return notes
}

func sanitizeCodeGenOpenAITypedFunctions(tools []openaiTool, functions []openaiFunction) int {
	sanitized := 0
	for i := range tools {
		before := tools[i].Function.Parameters
		after := corelib.SanitizeCodeGenOpenAIToolSchemaValue(before)
		if !reflect.DeepEqual(after, before) {
			tools[i].Function.Parameters = after
			sanitized++
		}
	}
	for i := range functions {
		before := functions[i].Parameters
		after := corelib.SanitizeCodeGenOpenAIToolSchemaValue(before)
		if !reflect.DeepEqual(after, before) {
			functions[i].Parameters = after
			sanitized++
		}
	}
	return sanitized
}

func detectQwenFlashRepeatedToolLoop(messages []openaiMessage) (int, int, bool) {
	toolResults := countOpenAIToolResultMessages(messages)
	if toolResults < codeGenQwenFlashToolLoopAfterToolResults {
		return toolResults, 0, false
	}
	repeatedToolCalls := maxRepeatedOpenAIToolCallSignature(messages)
	return toolResults, repeatedToolCalls, repeatedToolCalls >= codeGenQwenFlashToolLoopRepeatedToolCalls
}

func maxRepeatedOpenAIToolCallSignature(messages []openaiMessage) int {
	counts := make(map[string]int)
	maxCount := 0
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			name := strings.TrimSpace(tc.Function.Name)
			args := strings.TrimSpace(tc.Function.Arguments)
			if name == "" && args == "" {
				continue
			}
			key := name + "\x00" + normalizeJSONForSignature(args)
			counts[key]++
			if counts[key] > maxCount {
				maxCount = counts[key]
			}
		}
		if msg.FunctionCall != nil {
			name := strings.TrimSpace(msg.FunctionCall.Name)
			args := strings.TrimSpace(msg.FunctionCall.Arguments)
			if name == "" && args == "" {
				continue
			}
			key := name + "\x00" + normalizeJSONForSignature(args)
			counts[key]++
			if counts[key] > maxCount {
				maxCount = counts[key]
			}
		}
	}
	return maxCount
}

func normalizeJSONForSignature(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return string(normalized)
}

func countOpenAIToolResultMessages(messages []openaiMessage) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == "tool" {
			count++
		}
	}
	return count
}

func mergeQwenFlashOpenAIAdjacentMessages(messages *[]openaiMessage) (int, int, bool) {
	if messages == nil || len(*messages) < 2 {
		return lenValue(messages), lenValue(messages), false
	}
	before := len(*messages)
	merged := make([]openaiMessage, 0, len(*messages))
	for _, msg := range *messages {
		if len(merged) > 0 && canMergeQwenFlashMessages(merged[len(merged)-1].Role, msg.Role) {
			prev := &merged[len(merged)-1]
			prev.Content = joinOpenAIContent(prev.Content, msg.Content)
			continue
		}
		merged = append(merged, msg)
	}
	if len(merged) == before {
		return before, before, false
	}
	*messages = merged
	return before, len(merged), true
}

func mergeQwenFlashAdjacentMessages(payload map[string]interface{}) (int, int, bool) {
	messages := codeGenProxySliceFromAny(payload["messages"])
	if len(messages) < 2 {
		return len(messages), len(messages), false
	}
	merged := make([]interface{}, 0, len(messages))
	for _, item := range messages {
		msg := codeGenProxyMapFromAny(item)
		if msg == nil {
			merged = append(merged, item)
			continue
		}
		if len(merged) > 0 {
			prev := codeGenProxyMapFromAny(merged[len(merged)-1])
			if prev != nil {
				prevRole, _ := prev["role"].(string)
				role, _ := msg["role"].(string)
				if canMergeQwenFlashMessages(prevRole, role) {
					prev["content"] = joinOpenAIContent(prev["content"], msg["content"])
					continue
				}
			}
		}
		merged = append(merged, item)
	}
	if len(merged) == len(messages) {
		return len(messages), len(messages), false
	}
	payload["messages"] = merged
	return len(messages), len(merged), true
}

func canMergeQwenFlashMessages(prevRole, role string) bool {
	if prevRole == "" || prevRole != role {
		return false
	}
	switch role {
	case "system", "user":
		return true
	default:
		return false
	}
}

func joinOpenAIContent(a, b interface{}) string {
	left := strings.TrimSpace(logContentText(a))
	right := strings.TrimSpace(logContentText(b))
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + "\n\n" + right
	}
}

func logContentText(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
			}
			parts = append(parts, logScalar(item))
		}
		return strings.Join(parts, "\n")
	default:
		return logScalar(value)
	}
}

func lenValue(messages *[]openaiMessage) int {
	if messages == nil {
		return 0
	}
	return len(*messages)
}

func normalizeQwenFlashAnthropicSystemMessages(messages *[]openaiMessage) (int, int, int, int, bool) {
	if messages == nil || len(*messages) == 0 {
		return 0, 0, 0, 0, false
	}
	beforeMessages := len(*messages)
	beforeBytes := 0
	changed := false
	normalized := make([]openaiMessage, 0, len(*messages))
	systemWritten := false
	for _, msg := range *messages {
		if msg.Role != "system" {
			normalized = append(normalized, msg)
			continue
		}
		beforeBytes += len(logContentText(msg.Content))
		if systemWritten {
			changed = true
			continue
		}
		if text := logContentText(msg.Content); text != codeGenQwenFlashSystemPrompt {
			changed = true
		}
		msg.Content = codeGenQwenFlashSystemPrompt
		normalized = append(normalized, msg)
		systemWritten = true
	}
	if !changed {
		return beforeMessages, beforeMessages, beforeBytes, beforeBytes, false
	}
	*messages = normalized
	afterBytes := 0
	for _, msg := range normalized {
		if msg.Role == "system" {
			afterBytes += len(logContentText(msg.Content))
		}
	}
	return beforeMessages, len(normalized), beforeBytes, afterBytes, true
}

func sanitizeQwenFlashOpenAISystemMessages(messages []openaiMessage) (int, int, bool) {
	totalBefore := 0
	totalAfter := 0
	changed := false
	for i := range messages {
		if messages[i].Role != "system" {
			continue
		}
		text, ok := messages[i].Content.(string)
		if !ok {
			continue
		}
		totalBefore += len(text)
		if shouldReplaceQwenFlashSystemPrompt(text) {
			messages[i].Content = codeGenQwenFlashSystemPrompt
			totalAfter += len(codeGenQwenFlashSystemPrompt)
			changed = true
			continue
		}
		totalAfter += len(text)
	}
	return totalBefore, totalAfter, changed
}

func sanitizeQwenFlashSystemMessages(payload map[string]interface{}) (int, int, bool) {
	messages := codeGenProxySliceFromAny(payload["messages"])
	if len(messages) == 0 {
		return 0, 0, false
	}
	totalBefore := 0
	totalAfter := 0
	changed := false
	for _, item := range messages {
		msg := codeGenProxyMapFromAny(item)
		if msg == nil {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		text, ok := msg["content"].(string)
		if !ok {
			continue
		}
		totalBefore += len(text)
		if shouldReplaceQwenFlashSystemPrompt(text) {
			msg["content"] = codeGenQwenFlashSystemPrompt
			totalAfter += len(codeGenQwenFlashSystemPrompt)
			changed = true
			continue
		}
		totalAfter += len(text)
	}
	if changed {
		payload["messages"] = messages
	}
	return totalBefore, totalAfter, changed
}

func shouldReplaceQwenFlashSystemPrompt(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "x-anthropic-billing-header") ||
		strings.Contains(lower, "you are claude code") ||
		strings.Contains(lower, "anthropic's official cli") ||
		strings.Contains(lower, "anthropic-version") ||
		strings.Contains(lower, "anthropic-beta")
}

func sanitizeQwenFlashOpenAIFunction(fn *openaiFunction) bool {
	if fn == nil {
		return false
	}
	changed := false
	if trimmed := truncate(fn.Description, codeGenQwenFlashToolDescriptionMax); trimmed != fn.Description {
		fn.Description = trimmed
		changed = true
	}
	if sanitizeQwenFlashSchemaValue(&fn.Parameters) {
		changed = true
	}
	return changed
}

func sanitizeQwenFlashToolFunctionMap(fn map[string]interface{}) bool {
	if fn == nil {
		return false
	}
	changed := false
	if desc, ok := fn["description"].(string); ok {
		trimmed := truncate(desc, codeGenQwenFlashToolDescriptionMax)
		if trimmed != desc {
			fn["description"] = trimmed
			changed = true
		}
	}
	if parameters, ok := fn["parameters"]; ok {
		if sanitizeQwenFlashSchemaValue(&parameters) {
			fn["parameters"] = parameters
			changed = true
		}
	}
	return changed
}

func sanitizeQwenFlashSchemaValue(value *interface{}) bool {
	if value == nil {
		return false
	}
	switch v := (*value).(type) {
	case map[string]interface{}:
		return sanitizeQwenFlashSchemaMap(v)
	case []interface{}:
		changed := false
		for i := range v {
			if sanitizeQwenFlashSchemaValue(&v[i]) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func sanitizeQwenFlashSchemaMap(schema map[string]interface{}) bool {
	changed := false
	for _, key := range []string{
		"$schema",
		"$id",
		"$defs",
		"$ref",
		"definitions",
		"unevaluatedProperties",
		"dependentSchemas",
		"dependentRequired",
		"patternProperties",
		"propertyNames",
		"oneOf",
		"anyOf",
		"allOf",
		"not",
		"const",
		"default",
		"examples",
		"format",
		"readOnly",
		"writeOnly",
		"nullable",
		"deprecated",
	} {
		if _, ok := schema[key]; ok {
			delete(schema, key)
			changed = true
		}
	}
	if _, ok := schema["properties"].(map[string]interface{}); ok {
		if _, hasType := schema["type"]; !hasType {
			schema["type"] = "object"
			changed = true
		}
	}
	if desc, ok := schema["description"].(string); ok {
		trimmed := truncate(desc, codeGenQwenFlashSchemaDescriptionMax)
		if trimmed != desc {
			schema["description"] = trimmed
			changed = true
		}
	}
	if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		delete(schema, "additionalProperties")
		changed = true
	}
	for key, child := range schema {
		if sanitizeQwenFlashSchemaValue(&child) {
			schema[key] = child
			changed = true
		}
	}
	return changed
}

func numericJSONInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func summarizeOpenAIRequest(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Sprintf("body_bytes=%d json=false", len(body))
	}
	schemaStats := summarizeToolSchemaStats(payload)
	systemBytes, systemClaudeMarkers := summarizeSystemMessages(payload["messages"])
	return fmt.Sprintf("body_bytes=%d max_tokens=%s messages=%d roles=%s system_bytes=%d system_claude_markers=%d tools=%d functions=%d stream=%t tool_choice=%s function_call=%s tool_schema=%s",
		len(body),
		logScalar(payload["max_tokens"]),
		logArrayLen(payload["messages"]),
		summarizeMessageRoles(payload["messages"]),
		systemBytes,
		systemClaudeMarkers,
		logArrayLen(payload["tools"]),
		logArrayLen(payload["functions"]),
		logBool(payload["stream"]),
		logScalar(payload["tool_choice"]),
		logScalar(payload["function_call"]),
		schemaStats)
}

func summarizeToolSchemaStats(payload map[string]interface{}) string {
	stats := toolSchemaStats{}
	countToolSchemaStats(payload["tools"], &stats)
	countToolSchemaStats(payload["functions"], &stats)
	return fmt.Sprintf("schemas=%d risky=%d desc_bytes=%d max_depth=%d keys=%s",
		stats.Schemas, stats.RiskyKeys, stats.DescriptionBytes, stats.MaxDepth, strings.Join(stats.RiskyKeyNames(), "|"))
}

type toolSchemaStats struct {
	Schemas          int
	RiskyKeys        int
	DescriptionBytes int
	MaxDepth         int
	keyNames         map[string]bool
}

func (s *toolSchemaStats) RiskyKeyNames() []string {
	if len(s.keyNames) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.keyNames))
	for name := range s.keyNames {
		names = append(names, name)
	}
	return names
}

func countToolSchemaStats(value interface{}, stats *toolSchemaStats) {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			countToolSchemaStats(item, stats)
		}
	case []openaiTool:
		for _, item := range v {
			countToolSchemaStats(item.Function.Parameters, stats)
		}
	case []openaiFunction:
		for _, item := range v {
			countToolSchemaStats(item.Parameters, stats)
		}
	case map[string]interface{}:
		if fn, ok := v["function"]; ok {
			countToolSchemaStats(fn, stats)
			return
		}
		if parameters, ok := v["parameters"]; ok {
			countSchemaMap(parameters, stats, 1)
			return
		}
		countSchemaMap(v, stats, 1)
	}
}

func countSchemaMap(value interface{}, stats *toolSchemaStats, depth int) {
	if stats == nil {
		return
	}
	if depth > stats.MaxDepth {
		stats.MaxDepth = depth
	}
	switch v := value.(type) {
	case map[string]interface{}:
		stats.Schemas++
		for key, child := range v {
			if key == "description" {
				if desc, ok := child.(string); ok {
					stats.DescriptionBytes += len(desc)
				}
			}
			if isRiskyQwenFlashSchemaKey(key) {
				stats.RiskyKeys++
				if stats.keyNames == nil {
					stats.keyNames = make(map[string]bool)
				}
				stats.keyNames[key] = true
			}
			countSchemaMap(child, stats, depth+1)
		}
	case []interface{}:
		for _, child := range v {
			countSchemaMap(child, stats, depth+1)
		}
	}
}

func summarizeMessageRoles(value interface{}) string {
	messages := codeGenProxySliceFromAny(value)
	if len(messages) == 0 {
		return "-"
	}
	roles := make([]string, 0, len(messages))
	for _, item := range messages {
		msg := codeGenProxyMapFromAny(item)
		if msg == nil {
			roles = append(roles, "?")
			continue
		}
		role, _ := msg["role"].(string)
		if role == "" {
			role = "?"
		}
		roles = append(roles, role)
	}
	return strings.Join(roles, ">")
}

func summarizeSystemMessages(value interface{}) (int, int) {
	messages := codeGenProxySliceFromAny(value)
	if len(messages) == 0 {
		return 0, 0
	}
	totalBytes := 0
	claudeMarkers := 0
	for _, item := range messages {
		msg := codeGenProxyMapFromAny(item)
		if msg == nil {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		text, ok := msg["content"].(string)
		if !ok {
			continue
		}
		totalBytes += len(text)
		if shouldReplaceQwenFlashSystemPrompt(text) {
			claudeMarkers++
		}
	}
	return totalBytes, claudeMarkers
}

func isRiskyQwenFlashSchemaKey(key string) bool {
	switch key {
	case "$schema", "$id", "$defs", "$ref", "definitions", "additionalProperties",
		"unevaluatedProperties", "dependentSchemas", "dependentRequired",
		"patternProperties", "propertyNames", "oneOf", "anyOf", "allOf",
		"not", "const", "default", "examples", "format", "readOnly",
		"writeOnly", "nullable", "deprecated":
		return true
	default:
		return false
	}
}

func prepareQwenFlashChatOnlyRetryBody(_ string, body []byte, status int) ([]byte, bool) {
	if status != http.StatusBadRequest {
		return nil, false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	hadTools := logArrayLen(payload["tools"]) > 0 || logArrayLen(payload["functions"]) > 0
	if !hadTools {
		return nil, false
	}
	delete(payload, "tools")
	delete(payload, "functions")
	delete(payload, "tool_choice")
	delete(payload, "function_call")
	sanitizeChatOnlyMessages(payload)
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return out, true
}

func prepareCodeGenCompactChatRetryBody(upURL, model string, body []byte, status int) ([]byte, bool) {
	if status != http.StatusBadRequest {
		return nil, false
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	messages := codeGenProxySliceFromAny(payload["messages"])
	if len(messages) == 0 {
		return nil, false
	}
	cfg := corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: model, ProviderName: "CodeGen"}
	compactMessages := llmcompat.CompactOpenAICompatMessagesForToollessRetry(cfg, messages)
	if len(compactMessages) == 0 {
		return nil, false
	}
	if reflect.DeepEqual(messages, compactMessages) && logArrayLen(payload["tools"]) == 0 && logArrayLen(payload["functions"]) == 0 {
		return nil, false
	}
	outPayload := map[string]interface{}{
		"model":    model,
		"messages": compactMessages,
	}
	for _, key := range []string{"stream", "max_tokens", "temperature", "top_p"} {
		if value, ok := payload[key]; ok {
			outPayload[key] = value
		}
	}
	notes := applyCodeGenOpenAIMapCompatibility(outPayload, model)
	if len(notes) > 0 {
		log.Printf("[codegenproxy] compact retry compatibility model=%q compatibility=%s", model, strings.Join(notes, ","))
	}
	out, err := json.Marshal(outPayload)
	if err != nil {
		return nil, false
	}
	return out, true
}

func sanitizeChatOnlyMessages(payload map[string]interface{}) {
	messages := codeGenProxySliceFromAny(payload["messages"])
	if len(messages) == 0 {
		return
	}
	changed := false
	for _, item := range messages {
		msg := codeGenProxyMapFromAny(item)
		if msg == nil {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "tool" {
			msg["role"] = "user"
			changed = true
			if id, _ := msg["tool_call_id"].(string); id != "" {
				msg["content"] = fmt.Sprintf("Tool result (%s): %s", id, logScalar(msg["content"]))
			}
		}
		for _, key := range []string{"tool_calls", "tool_call_id", "function_call"} {
			if _, ok := msg[key]; ok {
				delete(msg, key)
				changed = true
			}
		}
	}
	if changed {
		payload["messages"] = messages
	}
}

func logArrayLen(value interface{}) int {
	return len(codeGenProxySliceFromAny(value))
}

func firstNonEmptyCodegenProxy(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func codeGenProxyMapFromAny(value interface{}) map[string]interface{} {
	switch m := value.(type) {
	case map[string]interface{}:
		return m
	case map[string]string:
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	default:
		data, err := json.Marshal(value)
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

func codeGenProxySliceFromAny(value interface{}) []interface{} {
	switch items := value.(type) {
	case nil:
		return nil
	case []interface{}:
		return items
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
	case []map[string]string:
		out := make([]interface{}, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
	default:
		data, err := json.Marshal(value)
		if err != nil || len(data) == 0 || string(data) == "null" {
			return nil
		}
		var out []interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func logBool(value interface{}) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func logBoolFromBody(body []byte, key string) bool {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return logBool(payload[key])
}

func logScalar(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func shouldRefreshCodeGenModelAlias(model string) bool {
	if strings.Contains(model, "/") {
		return false
	}
	normalized := normalizeModelNameForMatch(model)
	return strings.HasPrefix(normalized, "qwen")
}

func isQwenFlashModel(model string) bool {
	return strings.Contains(normalizeModelNameForMatch(model), "qwen-flash")
}

func normalizeModelNameForMatch(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	model = strings.ReplaceAll(model, "_", "-")
	model = strings.ReplaceAll(model, " ", "-")
	return model
}

func estimateAnthropicCountTokens(payload map[string]interface{}) int {
	total := 0
	for _, key := range []string{"system", "messages", "tools"} {
		total += estimateAnthropicTokenValue(payload[key])
	}
	if total <= 0 {
		total = 1
	}
	return total
}

func estimateAnthropicTokenValue(value interface{}) int {
	switch v := value.(type) {
	case nil:
		return 0
	case string:
		return corelib.EstimateTextTokens(v)
	case []interface{}:
		total := 0
		for _, item := range v {
			total += estimateAnthropicTokenValue(item)
		}
		return total
	case map[string]interface{}:
		total := 0
		for key, child := range v {
			if key == "cache_control" {
				continue
			}
			if s, ok := child.(string); ok {
				total += corelib.EstimateTextTokens(s)
				continue
			}
			total += estimateAnthropicTokenValue(child)
		}
		if total == 0 && len(v) > 0 {
			if data, err := json.Marshal(v); err == nil {
				total = corelib.EstimateTextTokens(string(data))
			}
		}
		return total
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		return corelib.EstimateTextTokens(string(data))
	}
}

func (s *Server) resolveCodeGenModelAlias(ctx context.Context, upURL, apiKey, clientName, model string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.Contains(model, "/") {
		return model
	}
	if cached := s.getModelAlias(model); cached != "" {
		return cached
	}
	if !shouldRefreshCodeGenModelAlias(model) {
		return model
	}
	upEndpoint := codeGenProxyModelsEndpoint(upURL, clientName, "openai")
	resp, err := s.getUpstreamClient().DoModels(ctx, upEndpoint, apiKey, clientName)
	if err != nil {
		log.Printf("[codegenproxy] model alias refresh failed model=%q endpoint=%q err=%v", model, upEndpoint, err)
		return model
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[codegenproxy] model alias refresh unusable model=%q endpoint=%q status=%d err=%v body=%s",
			model, upEndpoint, resp.StatusCode, err, truncateForLog(body, 1024))
		return model
	}
	aliases := buildModelAliasMapFromModelsBody(body)
	s.setModelAliases(aliases)
	if resolved := aliases[strings.ToLower(model)]; resolved != "" {
		log.Printf("[codegenproxy] model alias refreshed model=%q resolved=%q", model, resolved)
		return resolved
	}
	log.Printf("[codegenproxy] model alias not found model=%q available=%s", model, strings.Join(extractModelIDsFromModelsBody(body), ","))
	return model
}

func buildModelAliasMapFromModelsBody(body []byte) map[string]string {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	entries, ok := modelEntries(raw)
	if !ok {
		return nil
	}
	aliases := make(map[string]string, len(entries)*3)
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id := normalizeModelIdentifier(modelField(m, "id"))
		if id == "" {
			id = normalizeModelIdentifier(modelField(m, "name"))
		}
		if id == "" {
			continue
		}
		addModelAlias(aliases, id, id)
		addModelAlias(aliases, modelField(m, "name"), id)
		addModelAlias(aliases, modelField(m, "display_name"), id)
		if slash := strings.LastIndex(id, "/"); slash >= 0 && slash+1 < len(id) {
			addModelAlias(aliases, id[slash+1:], id)
		}
	}
	return aliases
}

func addModelAlias(aliases map[string]string, alias string, id string) {
	alias = strings.TrimSpace(alias)
	id = strings.TrimSpace(id)
	if alias == "" || id == "" {
		return
	}
	key := strings.ToLower(alias)
	if _, exists := aliases[key]; !exists {
		aliases[key] = id
	}
}

func redactLogBody(body []byte) string {
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return string(body)
	}
	redactLogValue(value)
	if out, err := json.Marshal(value); err == nil {
		return string(out)
	}
	return string(body)
}

func redactLogValue(value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			if isSensitiveLogKey(key) {
				v[key] = "[REDACTED]"
				continue
			}
			redactLogValue(child)
		}
	case []interface{}:
		for _, child := range v {
			redactLogValue(child)
		}
	}
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return normalized == "authorization" ||
		normalized == "api_key" ||
		normalized == "apikey" ||
		normalized == "access_token" ||
		normalized == "refresh_token" ||
		normalized == "token" ||
		normalized == "x-api-key"
}

func normalizeOpenAIModelInBody(body []byte) []byte {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if model, ok := payload["model"].(string); ok {
		payload["model"] = normalizeModelIdentifier(model)
		if out, err := json.Marshal(payload); err == nil {
			return out
		}
	}
	return body
}

func setOpenAIModelInBody(body []byte, model string) []byte {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = strings.TrimSpace(model)
	if out, err := json.Marshal(payload); err == nil {
		return out
	}
	return body
}

func normalizeModelIdentifier(model string) string {
	return strings.TrimSpace(model)
}

func normalizeModelsResponse(body []byte, protocol string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	entries, ok := modelEntries(raw)
	if !ok {
		return body, nil
	}
	if protocol == "anthropic" {
		return json.Marshal(map[string]interface{}{"data": normalizeAnthropicModels(entries)})
	}
	out := make(map[string]interface{}, len(raw)+1)
	for k, v := range raw {
		out[k] = v
	}
	out["object"] = "list"
	out["data"] = normalizeOpenAIModels(entries)
	delete(out, "models")
	return json.Marshal(out)
}

func normalizeSingleModelResponse(body []byte, protocol, modelID string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	entries, ok := modelEntries(raw)
	if !ok {
		id := normalizeModelIdentifier(modelField(raw, "id", "name"))
		if sameModelID(id, modelID) {
			return normalizeSingleModelMap(raw, protocol), nil
		}
		return nil, nil
	}
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id := normalizeModelIdentifier(modelField(m, "id", "name"))
		name := normalizeModelIdentifier(modelField(m, "name", "display_name"))
		if sameModelID(id, modelID) || sameModelID(name, modelID) {
			return normalizeSingleModelMap(m, protocol), nil
		}
	}
	return nil, nil
}

func normalizeSingleModelMap(m map[string]interface{}, protocol string) []byte {
	if protocol == "anthropic" {
		out := normalizeAnthropicModelMap(m)
		data, _ := json.Marshal(out)
		return data
	}
	id := normalizeModelIdentifier(modelField(m, "id", "name"))
	out := map[string]interface{}{
		"id":       id,
		"object":   "model",
		"created":  0,
		"owned_by": modelField(m, "provider"),
	}
	data, _ := json.Marshal(out)
	return data
}

func sameModelID(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	return a != "" && strings.EqualFold(a, b)
}

func modelEntries(raw map[string]interface{}) ([]interface{}, bool) {
	if data, ok := raw["data"].([]interface{}); ok {
		return data, true
	}
	if models, ok := raw["models"].([]interface{}); ok {
		return models, true
	}
	return nil, false
}

func normalizeOpenAIModels(entries []interface{}) []map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id := normalizeModelIdentifier(modelField(m, "id", "name"))
		if id == "" {
			continue
		}
		models = append(models, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  0,
			"owned_by": modelField(m, "provider"),
		})
	}
	return models
}

func normalizeAnthropicModels(entries []interface{}) []map[string]interface{} {
	models := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id := normalizeModelIdentifier(modelField(m, "id", "name"))
		if id == "" {
			continue
		}
		models = append(models, normalizeAnthropicModelMap(m))
	}
	return models
}

func normalizeAnthropicModelMap(m map[string]interface{}) map[string]interface{} {
	id := normalizeModelIdentifier(modelField(m, "id", "name"))
	display := modelField(m, "display_name", "name")
	if display == "" {
		display = id
	}
	return map[string]interface{}{
		"id":               id,
		"display_name":     display,
		"type":             "model",
		"created_at":       "1970-01-01T00:00:00Z",
		"max_input_tokens": 200000,
		"max_tokens":       codeGenQwenFlashMaxTokens,
		"capabilities": map[string]interface{}{
			"input_modalities":  []string{"text"},
			"output_modalities": []string{"text"},
			"thinking": map[string]interface{}{
				"supported": false,
				"types": map[string]interface{}{
					"enabled":  map[string]interface{}{"supported": false},
					"adaptive": map[string]interface{}{"supported": false},
				},
			},
		},
	}
}

func modelField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
