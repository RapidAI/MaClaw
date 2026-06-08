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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const codeGenQwenFlashMaxTokens = 8192
const codeGenQwenFlashToolDescriptionMax = 512
const codeGenQwenFlashSchemaDescriptionMax = 256
const codeGenQwenFlashToolLoopAfterToolResults = 32
const codeGenQwenFlashToolLoopRepeatedToolCalls = 8
const codeGenQwenFlashSystemPrompt = "You are TigerClaw Code, a helpful coding assistant. Follow the user's instructions, use available tools when needed, and report outcomes clearly."
const codeGenStreamScannerMaxBuffer = 8 * 1024 * 1024

func setCodeGenUpstreamHeaders(req *http.Request, clientName string) {
	if req != nil {
		normalized := corelib.NormalizeCodeGenClientName(clientName)
		req.Header.Set("User-Agent", normalized)
		req.Header.Set(corelib.CodeGenClientNameHeader, normalized)
	}
}

// Server is the local Anthropic→OpenAI protocol conversion proxy.
type Server struct {
	addr     string
	listener net.Listener
	srv      *http.Server
	client   *http.Client // reused for upstream requests

	mu             sync.RWMutex
	upstreamURL    string // CodeGen OpenAI-compatible base URL
	apiKey         string // CodeGen access token
	clientName     string // upstream CodeGen client identity for User-Agent and X-Codegen-Client-Name
	clientKey      string // optional local proxy API key for OpenAI/Anthropic clients
	modelAliasByID map[string]string
}

// NewServer creates a new codegen proxy server.
func NewServer(addr string) *Server {
	return &Server{
		addr: addr,
		client: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: true, // SSE must not be compressed
			},
		},
	}
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
	mux.HandleFunc("/v1/models", s.handleOpenAIModels)
	mux.HandleFunc("/models", s.handleOpenAIModels)
	mux.HandleFunc("/anthropic/v1/messages", s.handleMessages)
	mux.HandleFunc("/anthropic/v1/models", s.handleModels)
	mux.HandleFunc("/health", s.handleHealth)

	s.listener = listener

	s.srv = &http.Server{Handler: mux}
	log.Printf("[codegenproxy] listening on %s (Anthropic→OpenAI adapter)", s.listener.Addr())

	go func() {
		select {
		case <-ctx.Done():
			_ = s.srv.Shutdown(context.Background())
		case <-shutdownDone:
		}
	}()

	if err := s.srv.Serve(s.listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.srv != nil {
		s.srv.Shutdown(context.Background())
	}
}

// Addr returns the listener address. Only valid after Start has bound.
func (s *Server) Addr() net.Addr {
	if s.listener != nil {
		return s.listener.Addr()
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

func (s *Server) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	s.handleModelsForProtocol(w, r, "openai")
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
	upEndpoint := upURL + "/models"
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, upEndpoint, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(req, clientName)
	log.Printf("[codegenproxy] models upstream request id=%s protocol=%s endpoint=%q client=%q user_agent=%q codegen_client=%q accept=%q",
		reqID, protocol, upEndpoint, clientName, req.Header.Get("User-Agent"), req.Header.Get(corelib.CodeGenClientNameHeader), r.Header.Get("Accept"))
	resp, err := s.client.Do(req)
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

	upEndpoint := upURL + "/chat/completions"
	upReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upEndpoint, bytes.NewReader(body))
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", r.Header.Get("Accept"))
	upReq.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(upReq, clientName)
	log.Printf("[codegenproxy] openai chat upstream request id=%s endpoint=%q client=%q user_agent=%q codegen_client=%q accept=%q original_model=%q normalized_model=%q summary=%s compatibility=%s",
		reqID, upEndpoint, clientName, upReq.Header.Get("User-Agent"), upReq.Header.Get(corelib.CodeGenClientNameHeader), r.Header.Get("Accept"), originalModel, normalizedModel, requestSummary, strings.Join(compatibilityNotes, ","))

	upResp, err := s.client.Do(upReq)
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
			retryReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upEndpoint, bytes.NewReader(retryBody))
			retryReq.Header.Set("Content-Type", "application/json")
			retryReq.Header.Set("Accept", r.Header.Get("Accept"))
			retryReq.Header.Set("Authorization", "Bearer "+apiKey)
			setCodeGenUpstreamHeaders(retryReq, clientName)
			retryResp, retryErr := s.client.Do(retryReq)
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

	reqData, _ := json.Marshal(openaiReq)
	requestSummary := summarizeOpenAIRequest(reqData)

	// Forward to upstream CodeGen using standard OpenAI Bearer auth.
	upEndpoint := upURL + "/chat/completions"
	upReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upEndpoint, bytes.NewReader(reqData))
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(upReq, clientName)
	log.Printf("[codegenproxy] anthropic upstream request id=%s endpoint=%q client=%q user_agent=%q codegen_client=%q model=%q summary=%s compatibility=%s",
		reqID, upEndpoint, clientName, upReq.Header.Get("User-Agent"), upReq.Header.Get(corelib.CodeGenClientNameHeader), anthReq.Model, requestSummary, strings.Join(compatibilityNotes, ","))

	upResp, err := s.client.Do(upReq)
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
			retryReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, upEndpoint, bytes.NewReader(retryBody))
			retryReq.Header.Set("Content-Type", "application/json")
			retryReq.Header.Set("Authorization", "Bearer "+apiKey)
			setCodeGenUpstreamHeaders(retryReq, clientName)
			retryResp, retryErr := s.client.Do(retryReq)
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
		s.handleStreamResponse(w, upResp, anthReq.Model, reqID)
	} else {
		s.handleNonStreamResponse(w, upResp, anthReq.Model)
	}
}

// handleNonStreamResponse converts an OpenAI non-streaming response to Anthropic format.
func (s *Server) handleNonStreamResponse(w http.ResponseWriter, upResp *http.Response, model string) {
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

	anthResp := convertOpenAIToAnthropic(openaiResp, model)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(anthResp)
}

// handleStreamResponse converts an OpenAI SSE stream to Anthropic SSE stream.
func (s *Server) handleStreamResponse(w http.ResponseWriter, upResp *http.Response, model, reqID string) {
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
		s.handleNonStreamResponse(w, upResp, model)
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

	scanner := bufio.NewScanner(upResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), codeGenStreamScannerMaxBuffer)

	blockIdx := 0
	textStarted := false
	var stopReason string
	textBytes := 0
	toolCalls := make(map[int]*streamToolCallAccum)
	var toolOrder []int
	var legacyFunction *streamToolCallAccum

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			log.Printf("[codegenproxy] stream chunk parse failed id=%s model=%q payload_bytes=%d err=%v payload=%s",
				reqID, model, len(payload), err, truncate(payload, 512))
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		finish := chunk.Choices[0].FinishReason

		// ── text content ──
		if delta.Content != "" {
			textBytes += len(delta.Content)
			if !textStarted {
				writeSSE(w, "content_block_start", map[string]interface{}{
					"type": "content_block_start", "index": blockIdx,
					"content_block": map[string]interface{}{"type": "text", "text": ""},
				})
				flusher.Flush()
				textStarted = true
			}
			writeSSE(w, "content_block_delta", map[string]interface{}{
				"type": "content_block_delta", "index": blockIdx,
				"delta": map[string]interface{}{"type": "text_delta", "text": delta.Content},
			})
			flusher.Flush()
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
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[codegenproxy] stream scan failed id=%s model=%q max_buffer=%d err=%v",
			reqID, model, codeGenStreamScannerMaxBuffer, err)
	}

	if textStarted {
		writeSSE(w, "content_block_stop", map[string]interface{}{
			"type": "content_block_stop", "index": blockIdx,
		})
		blockIdx++
		flusher.Flush()
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
	log.Printf("[codegenproxy] stream complete id=%s model=%q openai_finish=%q anthropic_stop=%q text_bytes=%d tool_calls=%d legacy_function=%t tool_summary=%s",
		reqID, model, stopReason, anthStop, textBytes, len(toolOrder), legacyFunction != nil, summarizeStreamTools(toolOrder, toolCalls, legacyFunction))

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

func summarizeStreamTools(order []int, calls map[int]*streamToolCallAccum, legacy *streamToolCallAccum) string {
	var parts []string
	for _, idx := range order {
		if acc := calls[idx]; acc != nil {
			parts = append(parts, summarizeStreamTool(acc))
		}
	}
	if legacy != nil {
		parts = append(parts, "legacy:"+summarizeStreamTool(legacy))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

func summarizeStreamTool(acc *streamToolCallAccum) string {
	name := strings.TrimSpace(acc.Name)
	if name == "" {
		name = "unknown_tool"
	}
	args := strings.TrimSpace(acc.Arguments)
	valid := args == "" || json.Valid([]byte(args))
	return fmt.Sprintf("%s(args_bytes=%d,json=%t)", name, len(args), valid)
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
	if !json.Valid([]byte(args)) {
		log.Printf("[codegenproxy] stream tool call has invalid JSON args index=%d name=%q args=%s", acc.Index, name, truncate(args, 512))
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

// writeSSE writes a single Anthropic SSE event.
func writeSSE(w http.ResponseWriter, event string, data interface{}) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
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
	if !isQwenFlashModel(model) {
		return nil
	}
	var notes []string
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
	if tools, ok := payload["tools"].([]interface{}); ok && len(tools) > 0 {
		sanitized := 0
		for _, tool := range tools {
			toolMap, ok := tool.(map[string]interface{})
			if !ok {
				continue
			}
			if fn, ok := toolMap["function"].(map[string]interface{}); ok {
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
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) < 2 {
		return len(messages), len(messages), false
	}
	merged := make([]interface{}, 0, len(messages))
	for _, item := range messages {
		msg, ok := item.(map[string]interface{})
		if !ok {
			merged = append(merged, item)
			continue
		}
		if len(merged) > 0 {
			prev, ok := merged[len(merged)-1].(map[string]interface{})
			if ok {
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
	messages, ok := payload["messages"].([]interface{})
	if !ok {
		return 0, 0, false
	}
	totalBefore := 0
	totalAfter := 0
	changed := false
	for _, item := range messages {
		msg, ok := item.(map[string]interface{})
		if !ok {
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
	messages, ok := value.([]interface{})
	if !ok {
		return "-"
	}
	roles := make([]string, 0, len(messages))
	for _, item := range messages {
		msg, ok := item.(map[string]interface{})
		if !ok {
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
	messages, ok := value.([]interface{})
	if !ok {
		return 0, 0
	}
	totalBytes := 0
	claudeMarkers := 0
	for _, item := range messages {
		msg, ok := item.(map[string]interface{})
		if !ok {
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

func prepareQwenFlashChatOnlyRetryBody(model string, body []byte, status int) ([]byte, bool) {
	if status != http.StatusBadRequest || !isQwenFlashModel(model) {
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

func sanitizeChatOnlyMessages(payload map[string]interface{}) {
	messages, ok := payload["messages"].([]interface{})
	if !ok {
		return
	}
	for _, item := range messages {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "tool" {
			msg["role"] = "user"
			if id, _ := msg["tool_call_id"].(string); id != "" {
				msg["content"] = fmt.Sprintf("Tool result (%s): %s", id, logScalar(msg["content"]))
			}
		}
		delete(msg, "tool_calls")
		delete(msg, "tool_call_id")
		delete(msg, "function_call")
	}
}

func logArrayLen(value interface{}) int {
	if items, ok := value.([]interface{}); ok {
		return len(items)
	}
	return 0
}

func logBool(value interface{}) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
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
	upEndpoint := strings.TrimRight(upURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upEndpoint, nil)
	if err != nil {
		return model
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	setCodeGenUpstreamHeaders(req, clientName)
	resp, err := s.client.Do(req)
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
		display := modelField(m, "display_name", "name")
		if display == "" {
			display = id
		}
		models = append(models, map[string]interface{}{
			"id":           id,
			"display_name": display,
			"type":         "model",
		})
	}
	return models
}

func modelField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
