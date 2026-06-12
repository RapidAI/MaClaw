package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

const (
	defaultBaseURL  = "http://127.0.0.1:18777/api/im-gateway/v1"
	defaultClientID = "maclaw-cli"
	defaultLockWait = 5
)

var cliVersion = "0.1.0"

type config struct {
	BaseURL        string
	Token          string
	ConfigPath     string
	StatePath      string
	ClientID       string
	ClientName     string
	SessionID      string
	ConversationID string
	UserID         string
	UserName       string
	TimeoutSec     int
	LockTimeoutSec int
	Limit          int
	Cursor         string
	Pretty         bool
	AutoAck        bool
	RequireSession bool
	JSONErrors     bool
	Discovered     bool
}

type cli struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	client *http.Client
	now    func() time.Time
}

type stateLock struct {
	path      string
	file      *os.File
	token     string
	heartbeat chan struct{}
}

type gatewayAPIError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type gatewayErrorEnvelope struct {
	OK        bool             `json:"ok"`
	RequestID string           `json:"requestId,omitempty"`
	Error     *gatewayAPIError `json:"error,omitempty"`
}

type askResult struct {
	Incoming       any                                `json:"incoming,omitempty"`
	SessionID      string                             `json:"sessionId"`
	ConversationID string                             `json:"conversationId,omitempty"`
	Messages       []coreim.ThirdPartyOutgoingMessage `json:"messages"`
	NextCursor     string                             `json:"nextCursor"`
	HasMore        bool                               `json:"hasMore"`
}

type invokeRequest struct {
	Action         string         `json:"action"`
	BaseURL        string         `json:"baseUrl,omitempty"`
	Token          string         `json:"token,omitempty"`
	ConfigPath     string         `json:"configPath,omitempty"`
	StatePath      string         `json:"statePath,omitempty"`
	BootstrapHost  string         `json:"bootstrapHost,omitempty"`
	BootstrapPort  *int           `json:"bootstrapPort,omitempty"`
	ForceToken     *bool          `json:"forceToken,omitempty"`
	ClientID       string         `json:"clientId,omitempty"`
	ClientName     string         `json:"clientName,omitempty"`
	SessionID      string         `json:"sessionId,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	UserID         string         `json:"userId,omitempty"`
	UserName       string         `json:"userName,omitempty"`
	Text           string         `json:"text,omitempty"`
	Message        map[string]any `json:"message,omitempty"`
	Attachments    []any          `json:"attachments,omitempty"`
	EventID        string         `json:"eventId,omitempty"`
	MessageID      string         `json:"messageId,omitempty"`
	TimeoutSec     *int           `json:"timeoutSec,omitempty"`
	LockTimeoutSec *int           `json:"lockTimeoutSec,omitempty"`
	Limit          *int           `json:"limit,omitempty"`
	Count          *int           `json:"count,omitempty"`
	Cursor         string         `json:"cursor,omitempty"`
	WaitPolls      *int           `json:"waitPolls,omitempty"`
	Ack            *bool          `json:"ack,omitempty"`
	RequireSession *bool          `json:"requireSession,omitempty"`
	Pretty         *bool          `json:"pretty,omitempty"`
	ToolsPath      string         `json:"toolsPath,omitempty"`
	MessageIDs     []string       `json:"messageIds,omitempty"`
	Status         string         `json:"status,omitempty"`
	ToolCallID     string         `json:"toolCallId,omitempty"`
	ToolPlanID     string         `json:"toolPlanId,omitempty"`
	StepID         string         `json:"stepId,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
	ResultID       string         `json:"resultId,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	ErrorCode      string         `json:"errorCode,omitempty"`
	ErrorMessage   string         `json:"errorMessage,omitempty"`
	ErrorRetryable *bool          `json:"errorRetryable,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type srvConfig struct {
	BaseURL    string
	AuthToken  string
	AdminToken string
	TenantID   string
	UserID     string
	Endpoint   string
	Pretty     bool
	TimeoutSec int
}

type srvUserConfigResponse struct {
	AppConfig corelib.AppConfig `json:"app_config"`
}

type srvUserConfigUpdateRequest struct {
	AppConfig corelib.AppConfig `json:"app_config"`
}

type cliState struct {
	CurrentSession string         `json:"currentSession,omitempty"`
	Sessions       []sessionState `json:"sessions,omitempty"`
}

type sessionState struct {
	ID        string `json:"id"`
	ClientID  string `json:"clientId,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

func main() {
	c := &cli{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		client: &http.Client{},
		now:    time.Now,
	}
	if err := c.run(os.Args[1:]); err != nil {
		writeCLIError(c.stderr, os.Args[1:], err)
		os.Exit(1)
	}
}

func writeCLIError(w io.Writer, args []string, err error) {
	if wantsJSONErrors(args) {
		_ = writeJSON(w, map[string]any{"ok": false, "error": map[string]any{"message": err.Error()}}, false)
		return
	}
	fmt.Fprintln(w, "error:", err)
}

func (c *cli) run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.stdout, usage())
		return nil
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "version", "--version", "-v":
		return writeJSON(c.stdout, map[string]any{"name": "maclaw-cli", "version": cliVersion, "protocolVersion": coreim.ThirdPartyProtocolVersion}, true)
	case "agent-help", "agent-usage":
		fmt.Fprint(c.stdout, agentUsage())
		return nil
	case "agent-spec":
		return writeJSON(c.stdout, agentSpec(), true)
	case "invoke-schema":
		return writeJSON(c.stdout, invokeSchema(), true)
	case "invoke":
		return c.runInvoke(args)
	case "health":
		return c.runHealth(args)
	case "doctor":
		return c.runDoctor(args)
	case "bootstrap", "init":
		return c.runBootstrap(args)
	case "srv", "server":
		return c.runSrv(args)
	case "session":
		return c.runSession(args)
	case "handshake":
		return c.runHandshake(args)
	case "send":
		return c.runSend(args)
	case "poll":
		return c.runPoll(args, false)
	case "watch":
		return c.runWatch(args)
	case "ack":
		return c.runAck(args)
	case "tool-result":
		return c.runToolResult(args)
	case "ask":
		return c.runAsk(args)
	case "continue", "run":
		return c.runAsk(args)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage())
	}
}

func (c *cli) runHealth(args []string) error {
	cfgp, fs := newFlagSet("health")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 10))
	defer cancel()
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/health", "", nil, &out); err != nil {
		return err
	}
	return writeJSON(c.stdout, out, cfg.Pretty)
}

func (c *cli) runInvoke(args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	inputPath := fs.String("input", "", "JSON request file; defaults to stdin")
	rawJSON := fs.String("json", "", "inline JSON request")
	dryRun := fs.Bool("dry-run", false, "parse request and print command mapping without executing")
	fs.Bool("json-errors", true, "accepted for consistency; invoke errors are always JSON at process boundary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var data []byte
	var err error
	switch {
	case strings.TrimSpace(*rawJSON) != "":
		data = []byte(*rawJSON)
	case strings.TrimSpace(*inputPath) != "":
		data, err = os.ReadFile(*inputPath)
	default:
		data, err = io.ReadAll(c.stdin)
	}
	if err != nil {
		return err
	}
	var req invokeRequest
	if err := decodeInvokeRequest(data, &req); err != nil {
		return fmt.Errorf("decode invoke request: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		if req.Action != "" {
			return errors.New("invoke action must not be blank; omit action to default to continue")
		}
		action = "continue"
	}
	action, cmdArgs, err := invokeArgs(req, action)
	if err != nil {
		return err
	}
	if *dryRun {
		return writeJSON(c.stdout, map[string]any{"ok": true, "action": action, "argv": redactedDryRunArgv(append([]string{action}, cmdArgs...))}, true)
	}
	return c.run(append([]string{action}, cmdArgs...))
}

func redactedDryRunArgv(argv []string) []string {
	out := append([]string(nil), argv...)
	for i := 0; i < len(out); i++ {
		if out[i] == "--token" && i+1 < len(out) {
			out[i+1] = "[redacted]"
			i++
			continue
		}
		if strings.HasPrefix(out[i], "--token=") {
			out[i] = "--token=[redacted]"
		}
	}
	return out
}

func decodeInvokeRequest(data []byte, req *invokeRequest) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func invokeArgs(req invokeRequest, action string) (string, []string, error) {
	args := []string{"--json-errors"}
	if unsupported := unsupportedInvokeFields(req, action); len(unsupported) > 0 {
		return "", nil, fmt.Errorf("invoke action %q does not support field(s): %s", action, strings.Join(unsupported, ", "))
	}
	if req.BaseURL != "" {
		args = append(args, "--base", normalizeBaseURL(req.BaseURL))
	}
	if req.Token != "" {
		args = append(args, "--token", req.Token)
	}
	if req.ConfigPath != "" {
		args = append(args, "--config", req.ConfigPath)
	}
	if req.StatePath != "" {
		args = append(args, "--state", req.StatePath)
	}
	if req.ClientID != "" {
		args = append(args, "--client", req.ClientID)
	}
	if req.ClientName != "" {
		args = append(args, "--client-name", req.ClientName)
	}
	if isStatefulInvokeAction(action) {
		if req.SessionID != "" {
			args = append(args, "--session", req.SessionID)
		}
		if req.ConversationID != "" {
			args = append(args, "--conversation", req.ConversationID)
		}
	}
	if req.UserID != "" {
		args = append(args, "--user", req.UserID)
	}
	if req.UserName != "" {
		args = append(args, "--name", req.UserName)
	}
	if req.TimeoutSec != nil {
		if *req.TimeoutSec < 0 {
			return "", nil, errors.New("invoke timeoutSec must be non-negative")
		}
		args = append(args, "--timeout", fmt.Sprintf("%d", *req.TimeoutSec))
	}
	if req.LockTimeoutSec != nil {
		if *req.LockTimeoutSec < 1 {
			return "", nil, errors.New("invoke lockTimeoutSec must be at least 1")
		}
		args = append(args, "--lock-timeout", fmt.Sprintf("%d", *req.LockTimeoutSec))
	}
	if req.Limit != nil {
		if *req.Limit < 1 {
			return "", nil, errors.New("invoke limit must be at least 1")
		}
		args = append(args, "--limit", fmt.Sprintf("%d", *req.Limit))
	}
	if req.Cursor != "" {
		args = append(args, "--cursor", req.Cursor)
	}
	if req.Ack != nil {
		args = append(args, "--ack="+fmt.Sprintf("%t", *req.Ack))
	}
	requireSession := true
	if req.RequireSession != nil {
		requireSession = *req.RequireSession
	}
	if requireSession && isStatefulInvokeAction(action) {
		args = append(args, "--require-session")
	}
	if req.Pretty != nil {
		args = append(args, "--pretty="+fmt.Sprintf("%t", *req.Pretty))
	}
	switch action {
	case "continue", "run", "ask":
		action = "continue"
		if strings.TrimSpace(req.Text) == "" {
			return "", nil, errors.New("invoke continue requires text")
		}
		if req.WaitPolls != nil {
			if *req.WaitPolls < 1 {
				return "", nil, errors.New("invoke waitPolls must be at least 1")
			}
			args = append(args, "--wait-polls", fmt.Sprintf("%d", *req.WaitPolls))
		}
		if len(req.Metadata) > 0 {
			data, err := metadataJSONArg(req.Metadata)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--metadata-json", data)
		}
		if req.Text != "" {
			args = append(args, "--text", req.Text)
		}
	case "send":
		if req.Text == "" && len(req.Message) == 0 {
			return "", nil, errors.New("invoke send requires text or message")
		}
		if req.EventID != "" {
			args = append(args, "--event-id", req.EventID)
		}
		if req.MessageID != "" {
			args = append(args, "--message-id", req.MessageID)
		}
		if len(req.Message) > 0 {
			data, err := messagePayloadJSONArg(req.Message)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--message-json", data)
		}
		if len(req.Attachments) > 0 {
			data, err := attachmentsJSONArg(req.Attachments)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--attachments-json", data)
		}
		if len(req.Metadata) > 0 {
			data, err := metadataJSONArg(req.Metadata)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--metadata-json", data)
		}
		if req.Text != "" {
			args = append(args, "--text", req.Text)
		}
	case "poll", "doctor":
	case "bootstrap":
		if req.BootstrapHost != "" {
			args = append(args, "--host", req.BootstrapHost)
		}
		if req.BootstrapPort != nil {
			if *req.BootstrapPort < 1 || *req.BootstrapPort > 65535 {
				return "", nil, errors.New("invoke bootstrapPort must be between 1 and 65535")
			}
			args = append(args, "--port", fmt.Sprintf("%d", *req.BootstrapPort))
		}
		if req.ForceToken != nil {
			args = append(args, "--force-token="+fmt.Sprintf("%t", *req.ForceToken))
		}
	case "watch":
		if req.Count != nil {
			if *req.Count < 1 {
				return "", nil, errors.New("invoke count must be at least 1")
			}
			args = append(args, "--count", fmt.Sprintf("%d", *req.Count))
		}
	case "handshake":
		if req.ToolsPath != "" {
			args = append(args, "--tools", req.ToolsPath)
		}
	case "ack":
		if len(req.MessageIDs) == 0 {
			return "", nil, errors.New("invoke ack requires messageIds")
		}
		args = append(args, "--ids", strings.Join(req.MessageIDs, ","))
		if req.Status != "" {
			if normalizeInvokeAckStatus(req.Status) == "" {
				return "", nil, errors.New("invoke ack status must be delivered, read, or failed")
			}
			args = append(args, "--status", req.Status)
		}
	case "tool-result":
		if req.ToolCallID == "" && req.ToolPlanID == "" {
			return "", nil, errors.New("invoke tool-result requires toolCallId or toolPlanId")
		}
		if req.Status == "" {
			return "", nil, errors.New("invoke tool-result requires status")
		}
		if coreim.NormalizeThirdPartyToolStatus(req.Status) == "" {
			return "", nil, errors.New("invoke tool-result status must be success, error, rejected, cancelled, or timeout")
		}
		if req.ResultID != "" {
			args = append(args, "--result-id", req.ResultID)
		}
		if req.ToolCallID != "" {
			args = append(args, "--tool-call-id", req.ToolCallID)
		}
		if req.ToolPlanID != "" {
			args = append(args, "--tool-plan-id", req.ToolPlanID)
		}
		if req.StepID != "" {
			args = append(args, "--step-id", req.StepID)
		}
		if req.Status != "" {
			args = append(args, "--status", req.Status)
		}
		if req.IdempotencyKey != "" {
			args = append(args, "--idempotency-key", req.IdempotencyKey)
		}
		if len(req.Result) > 0 {
			data, err := json.Marshal(req.Result)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--result-json", string(data))
		}
		if req.Text != "" {
			args = append(args, "--text", req.Text)
		}
		if req.ErrorCode != "" {
			args = append(args, "--error-code", req.ErrorCode)
		}
		if req.ErrorMessage != "" {
			args = append(args, "--error-message", req.ErrorMessage)
		}
		if req.ErrorRetryable != nil {
			args = append(args, "--error-retryable="+fmt.Sprintf("%t", *req.ErrorRetryable))
		}
		if len(req.Metadata) > 0 {
			data, err := metadataJSONArg(req.Metadata)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--metadata-json", data)
		}
	default:
		return "", nil, fmt.Errorf("unsupported invoke action %q", req.Action)
	}
	if action == "run" || action == "ask" {
		action = "continue"
	}
	return action, args, nil
}

func unsupportedInvokeFields(req invokeRequest, action string) []string {
	var out []string
	add := func(name string, present bool) {
		if present {
			out = append(out, name)
		}
	}
	isContinue := action == "continue" || action == "run" || action == "ask"
	isPolling := isContinue || action == "poll" || action == "watch"
	isSend := action == "send"
	isToolResult := action == "tool-result"

	if action != "bootstrap" {
		add("bootstrapHost", req.BootstrapHost != "")
		add("bootstrapPort", req.BootstrapPort != nil)
		add("forceToken", req.ForceToken != nil)
	}
	if action != "handshake" {
		add("toolsPath", req.ToolsPath != "")
	}
	if action != "ack" {
		add("messageIds", len(req.MessageIDs) > 0)
	}
	if !isContinue {
		add("waitPolls", req.WaitPolls != nil)
	}
	if action != "watch" {
		add("count", req.Count != nil)
	}
	if !isPolling {
		add("limit", req.Limit != nil)
		add("cursor", req.Cursor != "")
		add("ack", req.Ack != nil)
	}
	if !(isContinue || isSend) {
		add("userId", req.UserID != "")
		add("userName", req.UserName != "")
	}
	if !(isContinue || isSend || isToolResult) {
		add("text", req.Text != "")
		add("metadata", len(req.Metadata) > 0)
	}
	if !isSend {
		add("eventId", req.EventID != "")
		add("messageId", req.MessageID != "")
		add("message", len(req.Message) > 0)
		add("attachments", len(req.Attachments) > 0)
	}
	if !isToolResult {
		add("resultId", req.ResultID != "")
		add("toolCallId", req.ToolCallID != "")
		add("toolPlanId", req.ToolPlanID != "")
		add("stepId", req.StepID != "")
		add("idempotencyKey", req.IdempotencyKey != "")
		add("result", len(req.Result) > 0)
		add("errorCode", req.ErrorCode != "")
		add("errorMessage", req.ErrorMessage != "")
		add("errorRetryable", req.ErrorRetryable != nil)
	}
	if action != "ack" && !isToolResult {
		add("status", req.Status != "")
	}
	return out
}

func isStatefulInvokeAction(action string) bool {
	switch action {
	case "continue", "run", "ask", "send", "poll", "watch", "tool-result":
		return true
	default:
		return false
	}
}

func normalizeInvokeAckStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "delivered", "delivery", "ok", "success":
		return "delivered"
	case "read":
		return "read"
	case "failed", "error":
		return "failed"
	default:
		return ""
	}
}

func (c *cli) runDoctor(args []string) error {
	cfgp, fs := newFlagSet("doctor")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	result := map[string]any{
		"ok":           false,
		"configPath":   cfg.ConfigPath,
		"baseUrl":      cfg.BaseURL,
		"discovered":   cfg.Discovered,
		"tokenPresent": strings.TrimSpace(cfg.Token) != "",
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 10))
	defer cancel()
	var health map[string]any
	if err := c.doJSON(ctx, http.MethodGet, cfg.BaseURL+"/health", "", nil, &health); err != nil {
		result["health"] = map[string]any{"ok": false, "error": err.Error()}
		_ = writeJSON(c.stdout, result, cfg.Pretty)
		return err
	}
	result["health"] = health
	if strings.TrimSpace(cfg.Token) == "" {
		result["error"] = "missing gateway token; run maclaw-cli bootstrap, then start or restart MaClaw GUI gateway"
		_ = writeJSON(c.stdout, result, cfg.Pretty)
		return requireToken(cfg)
	}
	var handshake map[string]any
	req := coreim.ThirdPartyHandshakeRequest{
		ClientID:        cfg.ClientID,
		ClientName:      cfg.ClientName,
		ProtocolVersion: coreim.ThirdPartyProtocolVersion,
		Capabilities:    coreim.ThirdPartyCapabilityMap(),
	}
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/handshake", cfg.Token, req, &handshake); err != nil {
		result["handshake"] = map[string]any{"ok": false, "error": err.Error()}
		_ = writeJSON(c.stdout, result, cfg.Pretty)
		return err
	}
	result["handshake"] = handshake
	result["ok"] = true
	return writeJSON(c.stdout, result, cfg.Pretty)
}

func (c *cli) runBootstrap(args []string) error {
	cfgp, fs := newFlagSet("bootstrap")
	host := fs.String("host", "127.0.0.1", "gateway host written to GUI config")
	port := fs.Int("port", 18777, "gateway port written to GUI config")
	forceToken := fs.Bool("force-token", false, "replace existing gateway token")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	appCfg, err := loadAppConfig(cfg.ConfigPath)
	if err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return errors.New("--port must be between 1 and 65535")
	}
	if strings.TrimSpace(appCfg.ThirdPartyGatewayToken) == "" || *forceToken {
		token, err := randomHex(32)
		if err != nil {
			return err
		}
		appCfg.ThirdPartyGatewayToken = token
	}
	appCfg.ThirdPartyGatewayEnabled = true
	appCfg.ThirdPartyGatewayHost = firstNonEmpty(*host, "127.0.0.1")
	appCfg.ThirdPartyGatewayPort = *port
	appCfg.SetThirdPartyGatewayLocal(true)
	if err := saveAppConfig(cfg.ConfigPath, appCfg); err != nil {
		return err
	}
	clientHost := gatewayClientHost(appCfg.ThirdPartyGatewayHost)
	baseURL := fmt.Sprintf("http://%s/api/im-gateway/v1", netJoinHostPortForURL(clientHost, appCfg.ThirdPartyGatewayPort))
	return writeJSON(c.stdout, map[string]any{
		"ok":           true,
		"configPath":   cfg.ConfigPath,
		"baseUrl":      baseURL,
		"enabled":      true,
		"tokenPresent": true,
		"note":         "Config written. If MaClaw GUI is already running and gateway is not connected, restart gateway in GUI or restart MaClaw GUI.",
	}, cfg.Pretty)
}

func (c *cli) runSrv(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.stdout, srvUsage())
		return nil
	}
	area, args := args[0], args[1:]
	switch area {
	case "thirdparty", "third-party", "gateway":
		return c.runSrvThirdParty(args)
	case "tools":
		return c.runSrvTools(args)
	case "setup", "connect", "enable", "show", "info", "set", "disable", "rotate-token", "token", "test", "check":
		return c.runSrvThirdParty(append([]string{area}, args...))
	default:
		return fmt.Errorf("unknown srv area %q\n\n%s", area, srvUsage())
	}
}

func (c *cli) runSrvThirdParty(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.stdout, srvUsage())
		return nil
	}
	sub, args := args[0], args[1:]
	sub = normalizeSrvThirdPartySubcommand(sub)
	cfgp, fs := newSrvFlagSet("srv thirdparty " + sub)
	gatewayToken := fs.String("gateway-token", "", "third-party gateway token; use auto to generate")
	fs.StringVar(gatewayToken, "token", *gatewayToken, "alias for --gateway-token")
	fs.StringVar(&cfgp.AuthToken, "user-token", cfgp.AuthToken, "alias for --auth-token")
	enable := fs.Bool("enable", true, "enable third-party access")
	localMode := fs.Bool("local-mode", false, "write thirdparty_gateway_local_mode")
	setLocalMode := fs.Bool("set-local-mode", false, "persist --local-mode")
	host := fs.String("host", "", "third-party access host stored in user config")
	port := fs.Int("port", 0, "third-party access port stored in user config")
	includeToken := fs.Bool("include-token", false, "print existing token when available; use carefully")
	clientID := fs.String("client", defaultClientID, "test client id")
	clientName := fs.String("client-name", "MaClaw CLI", "test client name")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := applySrvThirdPartyPositionals(cfgp, gatewayToken, sub, fs.Args()); err != nil {
		return err
	}
	if sub == "test" && strings.TrimSpace(*gatewayToken) == "" {
		*gatewayToken = firstNonEmpty(os.Getenv("MACLAWSRV_GATEWAY_TOKEN"), os.Getenv("MACLAW_SRV_GATEWAY_TOKEN"))
	}
	cfg := finalizeSrvConfig(*cfgp)
	if err := validateSrvConfig(cfg); err != nil {
		return err
	}
	if err := validateSrvURLs(cfg); err != nil {
		return err
	}
	switch sub {
	case "show", "info":
		if err := requireSrvAdmin(cfg); err != nil {
			return err
		}
		app, err := c.getSrvUserAppConfig(cfg)
		if err != nil {
			return err
		}
		return writeJSON(c.stdout, srvThirdPartyConfigSummary(cfg, app, *includeToken || sub == "info", ""), cfg.Pretty)
	case "set":
		if err := requireSrvAdmin(cfg); err != nil {
			return err
		}
		if flagWasSet(fs, "port") && (*port < 1 || *port > 65535) {
			return errors.New("--port must be between 1 and 65535")
		}
		app, err := c.getSrvUserAppConfig(cfg)
		if err != nil {
			return err
		}
		newToken := ""
		revealToken := false
		gatewayTokenValue := strings.TrimSpace(*gatewayToken)
		if strings.EqualFold(gatewayTokenValue, "auto") || (gatewayTokenValue == "" && strings.TrimSpace(app.ThirdPartyGatewayToken) == "") {
			newToken, err = randomHex(32)
			if err != nil {
				return err
			}
			app.ThirdPartyGatewayToken = newToken
			revealToken = true
		} else if gatewayTokenValue != "" {
			app.ThirdPartyGatewayToken = gatewayTokenValue
			newToken = app.ThirdPartyGatewayToken
			revealToken = true
		}
		app.ThirdPartyGatewayEnabled = *enable
		applySrvEndpointToApp(&app, cfg.Endpoint)
		if strings.TrimSpace(*host) != "" {
			app.ThirdPartyGatewayHost = strings.TrimSpace(*host)
		}
		if *port > 0 {
			app.ThirdPartyGatewayPort = *port
		}
		if *setLocalMode {
			app.SetThirdPartyGatewayLocal(*localMode)
		} else if app.ThirdPartyGatewayLocalMode == nil {
			app.SetThirdPartyGatewayLocal(false)
		}
		updated, err := c.updateSrvUserAppConfig(cfg, app)
		if err != nil {
			return err
		}
		out := srvThirdPartyConfigSummary(cfg, updated, revealToken || *includeToken, newToken)
		out["changed"] = true
		return writeJSON(c.stdout, out, cfg.Pretty)
	case "disable":
		if err := requireSrvAdmin(cfg); err != nil {
			return err
		}
		app, err := c.getSrvUserAppConfig(cfg)
		if err != nil {
			return err
		}
		app.ThirdPartyGatewayEnabled = false
		updated, err := c.updateSrvUserAppConfig(cfg, app)
		if err != nil {
			return err
		}
		out := srvThirdPartyConfigSummary(cfg, updated, false, "")
		out["changed"] = true
		return writeJSON(c.stdout, out, cfg.Pretty)
	case "rotate-token":
		if err := requireSrvAdmin(cfg); err != nil {
			return err
		}
		app, err := c.getSrvUserAppConfig(cfg)
		if err != nil {
			return err
		}
		newToken, err := randomHex(32)
		if err != nil {
			return err
		}
		app.ThirdPartyGatewayEnabled = true
		app.ThirdPartyGatewayToken = newToken
		applySrvEndpointToApp(&app, cfg.Endpoint)
		if app.ThirdPartyGatewayLocalMode == nil {
			app.SetThirdPartyGatewayLocal(false)
		}
		updated, err := c.updateSrvUserAppConfig(cfg, app)
		if err != nil {
			return err
		}
		out := srvThirdPartyConfigSummary(cfg, updated, true, newToken)
		out["changed"] = true
		out["rotated"] = true
		return writeJSON(c.stdout, out, cfg.Pretty)
	case "test":
		endpoint := normalizeBaseURL(firstNonEmpty(cfg.Endpoint, srvGatewayEndpointFromBase(cfg.BaseURL)))
		token := strings.TrimSpace(*gatewayToken)
		if token == "" {
			return errors.New("missing gateway token for srv test; pass --gateway-token, positional GATEWAY_TOKEN, or MACLAWSRV_GATEWAY_TOKEN")
		}
		if coreim.NormalizeThirdPartyID(*clientID) == "" {
			return errors.New("--client must contain at least one identifier character")
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 10))
		defer cancel()
		req := coreim.ThirdPartyHandshakeRequest{
			ClientID:        *clientID,
			ClientName:      *clientName,
			ProtocolVersion: coreim.ThirdPartyProtocolVersion,
			Capabilities:    coreim.ThirdPartyCapabilityMap(),
		}
		var out map[string]any
		if err := c.doJSON(ctx, http.MethodPost, endpoint+"/handshake", token, req, &out); err != nil {
			return err
		}
		result := map[string]any{"ok": true, "endpoint": endpoint, "handshake": out}
		if ok, _ := out["ok"].(bool); !ok {
			result["ok"] = false
		}
		if status, _ := out["status"].(string); status != "" {
			result["status"] = status
		}
		if channelID, _ := out["channelId"].(string); channelID != "" {
			result["channelId"] = channelID
		}
		return writeJSON(c.stdout, result, cfg.Pretty)
	default:
		return fmt.Errorf("unknown srv thirdparty command %q\n\n%s", sub, srvUsage())
	}
}

func applySrvThirdPartyPositionals(cfg *srvConfig, gatewayToken *string, sub string, args []string) error {
	if len(args) > 2 {
		return fmt.Errorf("too many positional arguments for srv %s", sub)
	}
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		cfg.BaseURL = strings.TrimSpace(args[0])
	}
	if len(args) >= 2 && strings.TrimSpace(args[1]) != "" {
		switch sub {
		case "test":
			if strings.TrimSpace(*gatewayToken) == "" {
				*gatewayToken = strings.TrimSpace(args[1])
			}
		default:
			if strings.TrimSpace(cfg.AuthToken) == "" {
				cfg.AuthToken = strings.TrimSpace(args[1])
			}
		}
	}
	return nil
}

func normalizeSrvThirdPartySubcommand(sub string) string {
	switch strings.ToLower(strings.TrimSpace(sub)) {
	case "setup", "connect", "enable":
		return "set"
	case "token", "new-token":
		return "rotate-token"
	case "check":
		return "test"
	default:
		return sub
	}
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	wasSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			wasSet = true
		}
	})
	return wasSet
}

func (c *cli) runSrvTools(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(c.stdout, srvUsage())
		return nil
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("srv tools "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "tools JSON file")
	pretty := fs.Bool("pretty", true, "pretty-print JSON")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch sub {
	case "validate":
		if strings.TrimSpace(*file) == "" {
			return errors.New("srv tools validate requires --file")
		}
		tools, err := readTools(*file)
		if err != nil {
			return err
		}
		names, err := validateToolDefinitions(tools)
		if err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "count": len(tools), "tools": names}, *pretty)
	default:
		return fmt.Errorf("unknown srv tools command %q\n\n%s", sub, srvUsage())
	}
}

func (c *cli) runHandshake(args []string) error {
	cfgp, fs := newFlagSet("handshake")
	toolsPath := fs.String("tools", "", "JSON file containing []tool definitions")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	if err := requireToken(cfg); err != nil {
		return err
	}
	tools, err := readTools(*toolsPath)
	if err != nil {
		return err
	}
	if _, err := validateToolDefinitions(tools); err != nil {
		return err
	}
	req := coreim.ThirdPartyHandshakeRequest{
		ClientID:        cfg.ClientID,
		ClientName:      cfg.ClientName,
		ProtocolVersion: coreim.ThirdPartyProtocolVersion,
		Capabilities:    coreim.ThirdPartyCapabilityMap(),
		Tools:           tools,
	}
	if err := coreim.NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 20))
	defer cancel()
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/handshake", cfg.Token, req, &out); err != nil {
		return err
	}
	return writeJSON(c.stdout, out, cfg.Pretty)
}

func (c *cli) runSend(args []string) error {
	cfgp, fs := newFlagSet("send")
	text := fs.String("text", "", "text message")
	messageJSON := fs.String("message-json", "", "message payload JSON; overrides --text")
	attachmentsJSON := fs.String("attachments-json", "", "message attachments JSON array")
	metadataJSON := fs.String("metadata-json", "", "JSON object string metadata")
	eventID := fs.String("event-id", "", "stable event id for idempotency")
	messageID := fs.String("message-id", "", "source message id")
	stdin := fs.Bool("stdin", false, "read text from stdin")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	msgText := *text
	if *stdin {
		data, err := io.ReadAll(c.stdin)
		if err != nil {
			return err
		}
		msgText = string(data)
	}
	if strings.TrimSpace(msgText) == "" && len(fs.Args()) > 0 {
		msgText = strings.Join(cleanPromptArgs(fs.Args()), " ")
	}
	if strings.TrimSpace(msgText) == "" && strings.TrimSpace(*messageJSON) == "" {
		return errors.New("send requires --text, --stdin, positional text, or --message-json")
	}
	payload, err := buildMessagePayload(msgText, *messageJSON, *attachmentsJSON)
	if err != nil {
		return err
	}
	metadata, err := readStringMap(*metadataJSON)
	if err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	var runLock *stateLock
	cfg, runLock, err = c.applySessionWithRunLock(cfg, true)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
		return err
	}
	now := c.now().UnixMilli()
	if strings.TrimSpace(*messageID) == "" {
		*messageID = c.newMessageID(now)
	}
	if strings.TrimSpace(*eventID) == "" {
		*eventID = "evt_" + strings.TrimSpace(*messageID)
	}
	req := coreim.ThirdPartyIncomingRequest{
		ClientID:       cfg.ClientID,
		EventID:        *eventID,
		MessageID:      *messageID,
		ConversationID: cfg.ConversationID,
		User:           coreim.ThirdPartyUserRef{ID: cfg.UserID, Name: cfg.UserName},
		Message:        payload,
		Metadata:       metadata,
		CreatedAt:      now,
	}
	if err := coreim.NormalizeThirdPartyIncomingRequest(&req, coreim.ThirdPartyNormalizeOptions{DefaultConversationID: cfg.ConversationID}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 30))
	defer cancel()
	var out coreim.ThirdPartyIncomingAcceptedResponse
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/incoming", cfg.Token, req, &out); err != nil {
		return err
	}
	return writeJSON(c.stdout, out, cfg.Pretty)
}

func (c *cli) runPoll(args []string, silent bool) error {
	cfgp, fs := newFlagSet("poll")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	var err error
	var runLock *stateLock
	cfg, runLock, err = c.applySessionWithRunLock(cfg, true)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
		return err
	}
	out, err := c.poll(cfg)
	if err != nil {
		return err
	}
	if cfg.AutoAck {
		if err := c.ackMessages(cfg, out.Messages); err != nil {
			return err
		}
	}
	if err := c.updateSessionCursor(cfg, out.NextCursor); err != nil {
		return err
	}
	if silent {
		return nil
	}
	return writeJSON(c.stdout, out, cfg.Pretty)
}

func (c *cli) runWatch(args []string) error {
	cfgp, fs := newFlagSet("watch")
	iterations := fs.Int("count", 0, "number of polls before exit; 0 means forever")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *iterations < 0 {
		return errors.New("--count must be non-negative")
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	var err error
	var runLock *stateLock
	cfg, runLock, err = c.applySessionWithRunLock(cfg, true)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
		return err
	}
	cursor := cfg.Cursor
	for i := 0; *iterations == 0 || i < *iterations; i++ {
		cfg.Cursor = cursor
		out, err := c.poll(cfg)
		if err != nil {
			return err
		}
		cursor = out.NextCursor
		for _, msg := range out.Messages {
			if err := writeJSONLine(c.stdout, msg); err != nil {
				return err
			}
		}
		if cfg.AutoAck {
			if err := c.ackMessages(cfg, out.Messages); err != nil {
				return err
			}
		}
		if err := c.updateSessionCursor(cfg, cursor); err != nil {
			return err
		}
	}
	return nil
}

func (c *cli) runAck(args []string) error {
	cfgp, fs := newFlagSet("ack")
	idsCSV := fs.String("ids", "", "comma-separated outgoing message ids")
	status := fs.String("status", "delivered", "delivery status")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ids := splitCSV(*idsCSV)
	if len(ids) == 0 {
		return errors.New("--ids is required")
	}
	if normalizeInvokeAckStatus(*status) == "" {
		return errors.New("--status must be delivered, read, or failed")
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	if err := requireToken(cfg); err != nil {
		return err
	}
	req := coreim.ThirdPartyAckRequest{ClientID: cfg.ClientID, MessageIDs: ids, Status: *status}
	if err := coreim.NormalizeThirdPartyAckRequest(&req, coreim.ThirdPartyMaxAckIDs); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 15))
	defer cancel()
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/ack", cfg.Token, req, &out); err != nil {
		return err
	}
	return writeJSON(c.stdout, out, cfg.Pretty)
}

func (c *cli) runToolResult(args []string) error {
	cfgp, fs := newFlagSet("tool-result")
	resultID := fs.String("result-id", "", "stable result id")
	toolCallID := fs.String("tool-call-id", "", "tool call id")
	toolPlanID := fs.String("tool-plan-id", "", "tool plan id")
	stepID := fs.String("step-id", "", "tool plan step id")
	status := fs.String("status", "success", "success, error, rejected, cancelled, timeout")
	resultJSON := fs.String("result-json", "", "JSON object result")
	text := fs.String("text", "", "plain text result")
	errorCode := fs.String("error-code", "", "tool error code")
	errorMessage := fs.String("error-message", "", "tool error message")
	errorRetryable := fs.Bool("error-retryable", false, "mark tool error as retryable")
	metadataJSON := fs.String("metadata-json", "", "JSON object string metadata")
	idempotencyKey := fs.String("idempotency-key", "", "stable idempotency key")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*toolCallID) == "" && strings.TrimSpace(*toolPlanID) == "" {
		return errors.New("tool-result requires --tool-call-id or --tool-plan-id")
	}
	if coreim.NormalizeThirdPartyToolStatus(*status) == "" {
		return errors.New("--status must be success, error, rejected, cancelled, or timeout")
	}
	result, err := readJSONObject(*resultJSON)
	if err != nil {
		return err
	}
	metadata, err := readStringMap(*metadataJSON)
	if err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	var runLock *stateLock
	cfg, runLock, err = c.applySessionWithRunLock(cfg, true)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
		return err
	}
	req := coreim.ThirdPartyToolResultRequest{
		ClientID:       cfg.ClientID,
		ResultID:       *resultID,
		ConversationID: cfg.ConversationID,
		ToolCallID:     *toolCallID,
		ToolPlanID:     *toolPlanID,
		StepID:         *stepID,
		Status:         *status,
		IdempotencyKey: *idempotencyKey,
		Result:         result,
		Text:           *text,
		Metadata:       metadata,
		CreatedAt:      c.now().UnixMilli(),
	}
	if strings.TrimSpace(*errorCode) != "" || strings.TrimSpace(*errorMessage) != "" {
		req.Error = &coreim.ThirdPartyToolError{Code: *errorCode, Message: *errorMessage, Retryable: *errorRetryable}
	}
	if err := coreim.NormalizeThirdPartyToolResultRequest(&req); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 20))
	defer cancel()
	var out map[string]any
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/tool-result", cfg.Token, req, &out); err != nil {
		return err
	}
	return writeJSON(c.stdout, out, cfg.Pretty)
}

func (c *cli) runAsk(args []string) error {
	cfgp, fs := newFlagSet("ask")
	text := fs.String("text", "", "text message")
	waitPolls := fs.Int("wait-polls", 1, "poll attempts after send")
	metadataJSON := fs.String("metadata-json", "", "JSON object string metadata")
	stdin := fs.Bool("stdin", false, "read text from stdin")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	msgText := *text
	if *stdin {
		data, err := io.ReadAll(c.stdin)
		if err != nil {
			return err
		}
		msgText = string(data)
	}
	if strings.TrimSpace(msgText) == "" && len(fs.Args()) > 0 {
		msgText = strings.Join(cleanPromptArgs(fs.Args()), " ")
	}
	if strings.TrimSpace(msgText) == "" {
		return errors.New("continue requires --text, --stdin, or positional text")
	}
	if *waitPolls < 1 {
		return errors.New("--wait-polls must be at least 1")
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	var err error
	var runLock *stateLock
	cfg, runLock, err = c.applySessionWithRunLock(cfg, true)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
		return err
	}
	now := c.now().UnixMilli()
	messageID := c.newMessageID(now)
	metadata, err := readStringMap(*metadataJSON)
	if err != nil {
		return err
	}
	inReq := coreim.ThirdPartyIncomingRequest{
		ClientID:       cfg.ClientID,
		EventID:        "evt_" + messageID,
		MessageID:      messageID,
		ConversationID: cfg.ConversationID,
		User:           coreim.ThirdPartyUserRef{ID: cfg.UserID, Name: cfg.UserName},
		Message:        coreim.ThirdPartyMessagePayload{Type: "text", Text: msgText},
		Metadata:       metadata,
		CreatedAt:      now,
	}
	if err := coreim.NormalizeThirdPartyIncomingRequest(&inReq, coreim.ThirdPartyNormalizeOptions{DefaultConversationID: cfg.ConversationID}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 30))
	defer cancel()
	var incoming coreim.ThirdPartyIncomingAcceptedResponse
	if err := c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/incoming", cfg.Token, inReq, &incoming); err != nil {
		return err
	}
	var all []coreim.ThirdPartyOutgoingMessage
	next := cfg.Cursor
	hasMore := false
	for i := 0; i < *waitPolls; i++ {
		cfg.Cursor = next
		out, err := c.poll(cfg)
		if err != nil {
			return err
		}
		all = append(all, out.Messages...)
		next = out.NextCursor
		hasMore = out.HasMore
		if cfg.AutoAck {
			if err := c.ackMessages(cfg, out.Messages); err != nil {
				return err
			}
		}
		if len(out.Messages) > 0 && !out.HasMore {
			break
		}
	}
	if err := c.updateSessionCursor(cfg, next); err != nil {
		return err
	}
	return writeJSON(c.stdout, askResult{Incoming: incoming, SessionID: cfg.SessionID, ConversationID: cfg.ConversationID, Messages: all, NextCursor: next, HasMore: hasMore}, cfg.Pretty)
}

func (c *cli) runSession(args []string) error {
	if len(args) == 0 {
		args = []string{"current"}
	}
	sub, args := args[0], args[1:]
	rawArgs := append([]string(nil), args...)
	cfgp, fs := newFlagSet("session")
	id := fs.String("id", "", "session id")
	args = reorderKnownFlags(fs, args)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := validateConfigValues(cfg); err != nil {
		return err
	}
	switch sub {
	case "new":
		sessionID := firstNonEmpty(*id, c.newSessionID())
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, true, func(st *cliState) error {
			if findSessionForClientOrLegacy(*st, sessionID, cfg.ClientID) != nil {
				return fmt.Errorf("session %q already exists", sessionID)
			}
			upsertSession(st, sessionState{ID: sessionID, ClientID: cfg.ClientID, Cursor: "0", CreatedAt: c.now().UnixMilli(), UpdatedAt: c.now().UnixMilli()})
			st.CurrentSession = sessionID
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "currentSession": sessionID, "statePath": cfg.StatePath}, cfg.Pretty)
	case "use":
		sessionID := firstNonEmpty(*id, firstArg(fs.Args()))
		if sessionID == "" {
			return errors.New("session use requires --id or positional id")
		}
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, true, func(st *cliState) error {
			now := c.now().UnixMilli()
			if sess := findSessionForClientOrLegacy(*st, sessionID, cfg.ClientID); sess != nil {
				sess.UpdatedAt = now
			} else {
				upsertSession(st, sessionState{ID: sessionID, ClientID: cfg.ClientID, Cursor: "0", CreatedAt: now, UpdatedAt: now})
			}
			st.CurrentSession = sessionID
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "currentSession": sessionID, "statePath": cfg.StatePath}, cfg.Pretty)
	case "rename":
		from := firstNonEmpty(*id, firstArg(fs.Args()))
		to := ""
		if args := fs.Args(); len(args) > 0 {
			if strings.TrimSpace(*id) != "" {
				to = args[0]
			} else if len(args) > 1 {
				to = args[1]
			}
		}
		if from == "" || to == "" {
			return errors.New("session rename requires old and new ids")
		}
		currentSession := ""
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, true, func(st *cliState) error {
			if findSessionForClientOrLegacy(*st, to, cfg.ClientID) != nil {
				return fmt.Errorf("session %q already exists", to)
			}
			sess := findSessionForClientOrLegacy(*st, from, cfg.ClientID)
			if sess == nil {
				return fmt.Errorf("session %q not found", from)
			}
			sess.ID = to
			sess.UpdatedAt = c.now().UnixMilli()
			if st.CurrentSession == from && !hasSessionID(*st, from) {
				st.CurrentSession = to
			}
			currentSession = st.CurrentSession
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "currentSession": currentSession, "renamed": map[string]string{"from": from, "to": to}}, cfg.Pretty)
	case "delete", "rm":
		sessionID := firstNonEmpty(*id, firstArg(fs.Args()))
		if sessionID == "" {
			return errors.New("session delete requires --id or positional id")
		}
		currentSession := ""
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, true, func(st *cliState) error {
			var removed bool
			st.Sessions, removed = removeSessionForClient(st.Sessions, sessionID, cfg.ClientID)
			if !removed {
				return fmt.Errorf("session %q not found", sessionID)
			}
			if st.CurrentSession == sessionID && !hasSessionID(*st, sessionID) {
				st.CurrentSession = ""
			}
			currentSession = st.CurrentSession
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "deleted": sessionID, "currentSession": currentSession}, cfg.Pretty)
	case "reset-cursor":
		sessionID := firstNonEmpty(*id, cfg.SessionID, cfg.ConversationID)
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, true, func(st *cliState) error {
			if sessionID == "" {
				sessionID = st.CurrentSession
			}
			if sessionID == "" {
				return errors.New("no current session")
			}
			sess := findSessionForClientOrLegacy(*st, sessionID, cfg.ClientID)
			if sess == nil {
				return fmt.Errorf("session %q not found", sessionID)
			}
			sess.Cursor = "0"
			sess.UpdatedAt = c.now().UnixMilli()
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "sessionId": sessionID, "cursor": "0"}, cfg.Pretty)
	case "current":
		cfg, explicit := normalizeSessionIDs(cfg)
		var sess *sessionState
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, false, func(st *cliState) error {
			if !explicit {
				cfg.SessionID = strings.TrimSpace(st.CurrentSession)
				cfg.ConversationID = cfg.SessionID
			}
			if cfg.SessionID == "" {
				return errors.New("no current session")
			}
			found := findSessionForClientOrLegacy(*st, cfg.SessionID, cfg.ClientID)
			if found == nil {
				return fmt.Errorf("session %q not found", cfg.SessionID)
			}
			copied := *found
			sess = &copied
			if cfg.Cursor == "" {
				cfg.Cursor = firstNonEmpty(copied.Cursor, "0")
			}
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "currentSession": cfg.SessionID, "conversationId": cfg.ConversationID, "session": sess, "statePath": cfg.StatePath}, cfg.Pretty)
	case "list":
		var st cliState
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, false, func(loaded *cliState) error {
			st = *loaded
			return nil
		}); err != nil {
			return err
		}
		out := map[string]any{"ok": true, "currentSession": st.CurrentSession, "sessions": st.Sessions, "statePath": cfg.StatePath}
		if hasNamedFlag(rawArgs, "client") {
			filtered := make([]sessionState, 0, len(st.Sessions))
			for _, sess := range st.Sessions {
				if sess.ClientID == cfg.ClientID || sess.ClientID == "" {
					filtered = append(filtered, sess)
				}
			}
			out["sessions"] = filtered
			out["filteredClient"] = cfg.ClientID
		}
		return writeJSON(c.stdout, out, cfg.Pretty)
	default:
		return fmt.Errorf("unknown session command %q", sub)
	}
}

func (c *cli) poll(cfg config) (coreim.ThirdPartyOutgoingPollResponse, error) {
	if cfg.Cursor == "" {
		cfg.Cursor = "0"
	}
	if cfg.TimeoutSec < 0 {
		cfg.TimeoutSec = 0
	}
	if cfg.Limit <= 0 {
		cfg.Limit = coreim.ThirdPartyMaxBatchSize
	}
	u := fmt.Sprintf("%s/outgoing?clientId=%s&cursor=%s&timeout=%d&limit=%d",
		cfg.BaseURL, url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.Cursor), cfg.TimeoutSec, cfg.Limit)
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec+10, 15))
	defer cancel()
	var out coreim.ThirdPartyOutgoingPollResponse
	if err := c.doJSON(ctx, http.MethodGet, u, cfg.Token, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *cli) ackMessages(cfg config, messages []coreim.ThirdPartyOutgoingMessage) error {
	ids := make([]string, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.ID) != "" {
			ids = append(ids, msg.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	req := coreim.ThirdPartyAckRequest{ClientID: cfg.ClientID, MessageIDs: ids, Status: "delivered"}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 15))
	defer cancel()
	var out map[string]any
	return c.doJSON(ctx, http.MethodPost, cfg.BaseURL+"/ack", cfg.Token, req, &out)
}

func (c *cli) doJSON(ctx context.Context, method, endpoint, token string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "maclaw-cli")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		data = []byte("{}")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env gatewayErrorEnvelope
		if err := json.Unmarshal(data, &env); err == nil && env.Error != nil {
			return fmt.Errorf("HTTP %d [%s] %s", resp.StatusCode, env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response HTTP %d: %w: %s", resp.StatusCode, err, string(data))
	}
	return nil
}

func newFlagSet(name string) (*config, *flag.FlagSet) {
	discovered := discoverGatewayConfig("")
	cfg := &config{
		BaseURL:        firstNonEmpty(os.Getenv("MACLAW_GATEWAY_URL"), discovered.BaseURL, defaultBaseURL),
		Token:          firstNonEmpty(os.Getenv("MACLAW_GATEWAY_TOKEN"), discovered.Token),
		ConfigPath:     firstNonEmpty(os.Getenv("MACLAW_CONFIG"), discovered.ConfigPath),
		StatePath:      firstNonEmpty(os.Getenv("MACLAW_CLI_STATE"), defaultStatePath()),
		ClientID:       firstNonEmpty(os.Getenv("MACLAW_CLIENT_ID"), defaultClientID),
		ClientName:     firstNonEmpty(os.Getenv("MACLAW_CLIENT_NAME"), "MaClaw CLI"),
		SessionID:      os.Getenv("MACLAW_SESSION_ID"),
		ConversationID: os.Getenv("MACLAW_CONVERSATION_ID"),
		UserID:         firstNonEmpty(os.Getenv("MACLAW_USER_ID"), "agent"),
		UserName:       firstNonEmpty(os.Getenv("MACLAW_USER_NAME"), "Agent"),
		TimeoutSec:     coreim.ThirdPartyPollTimeoutSec,
		LockTimeoutSec: envInt("MACLAW_CLI_LOCK_TIMEOUT_SEC", defaultLockWait),
		Limit:          coreim.ThirdPartyMaxBatchSize,
		Cursor:         "",
		Pretty:         true,
		AutoAck:        true,
		RequireSession: strings.EqualFold(os.Getenv("MACLAW_REQUIRE_SESSION"), "1") || strings.EqualFold(os.Getenv("MACLAW_REQUIRE_SESSION"), "true"),
		JSONErrors:     wantsJSONErrors(nil),
		Discovered:     discovered.OK,
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.BaseURL, "base", cfg.BaseURL, "gateway base URL")
	fs.StringVar(&cfg.Token, "token", cfg.Token, "gateway bearer token or MACLAW_GATEWAY_TOKEN")
	fs.StringVar(&cfg.ConfigPath, "config", cfg.ConfigPath, "MaClaw GUI config path")
	fs.StringVar(&cfg.StatePath, "state", cfg.StatePath, "maclaw-cli state path")
	fs.StringVar(&cfg.ClientID, "client", cfg.ClientID, "third-party client id")
	fs.StringVar(&cfg.ClientName, "client-name", cfg.ClientName, "third-party client display name")
	fs.StringVar(&cfg.SessionID, "session", cfg.SessionID, "maclaw-cli session id")
	fs.StringVar(&cfg.ConversationID, "conversation", cfg.ConversationID, "conversation id")
	fs.StringVar(&cfg.UserID, "user", cfg.UserID, "external user id")
	fs.StringVar(&cfg.UserName, "name", cfg.UserName, "external user display name")
	fs.IntVar(&cfg.TimeoutSec, "timeout", cfg.TimeoutSec, "request or long-poll timeout seconds")
	fs.IntVar(&cfg.LockTimeoutSec, "lock-timeout", cfg.LockTimeoutSec, "state/run lock wait timeout seconds")
	fs.IntVar(&cfg.Limit, "limit", cfg.Limit, "poll limit")
	fs.StringVar(&cfg.Cursor, "cursor", cfg.Cursor, "outgoing cursor")
	fs.BoolVar(&cfg.Pretty, "pretty", cfg.Pretty, "pretty-print JSON")
	fs.BoolVar(&cfg.AutoAck, "ack", cfg.AutoAck, "ack poll/watch/ask messages after receipt")
	fs.BoolVar(&cfg.RequireSession, "require-session", cfg.RequireSession, "fail unless --session, --conversation, or MACLAW_SESSION_ID is set")
	fs.BoolVar(&cfg.JSONErrors, "json-errors", cfg.JSONErrors, "emit machine-readable error JSON to stderr")
	return cfg, fs
}

func finalizeConfig(cfg config) config {
	discovered := discoverGatewayConfig(cfg.ConfigPath)
	cfg.ConfigPath = firstNonEmpty(cfg.ConfigPath, discovered.ConfigPath)
	if strings.TrimSpace(cfg.Token) == "" {
		cfg.Token = discovered.Token
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || cfg.BaseURL == defaultBaseURL {
		cfg.BaseURL = firstNonEmpty(discovered.BaseURL, cfg.BaseURL, defaultBaseURL)
	}
	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	cfg.Discovered = cfg.Discovered || discovered.OK
	return cfg
}

func validateConfigValues(cfg config) error {
	if coreim.NormalizeThirdPartyID(cfg.ClientID) == "" {
		return errors.New("--client must contain at least one identifier character")
	}
	if cfg.TimeoutSec < 0 {
		return errors.New("--timeout must be non-negative")
	}
	if cfg.LockTimeoutSec < 1 {
		return errors.New("--lock-timeout must be at least 1")
	}
	if cfg.Limit < 1 {
		return errors.New("--limit must be at least 1")
	}
	return nil
}

func newSrvFlagSet(name string) (*srvConfig, *flag.FlagSet) {
	cfg := &srvConfig{
		BaseURL:    firstNonEmpty(os.Getenv("MACLAWSRV_URL"), os.Getenv("MACLAW_SRV_URL"), "http://127.0.0.1:18778"),
		AuthToken:  firstNonEmpty(os.Getenv("MACLAWSRV_AUTH_TOKEN"), os.Getenv("MACLAW_SRV_AUTH_TOKEN")),
		AdminToken: firstNonEmpty(os.Getenv("MACLAWSRV_ADMIN_TOKEN"), os.Getenv("MACLAW_SRV_ADMIN_TOKEN")),
		TenantID:   firstNonEmpty(os.Getenv("MACLAWSRV_TENANT_ID"), os.Getenv("MACLAW_SRV_TENANT_ID")),
		UserID:     firstNonEmpty(os.Getenv("MACLAWSRV_USER_ID"), os.Getenv("MACLAW_SRV_USER_ID")),
		Endpoint:   firstNonEmpty(os.Getenv("MACLAWSRV_GATEWAY_URL"), os.Getenv("MACLAW_SRV_GATEWAY_URL")),
		Pretty:     true,
		TimeoutSec: 15,
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.BaseURL, "srv", cfg.BaseURL, "MaClawSrv base URL")
	fs.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "MaClawSrv user API bearer token")
	fs.StringVar(&cfg.AdminToken, "admin-token", cfg.AdminToken, "MaClawSrv admin bearer token")
	fs.StringVar(&cfg.TenantID, "tenant", cfg.TenantID, "MaClawSrv tenant id")
	fs.StringVar(&cfg.UserID, "user", cfg.UserID, "MaClawSrv user id")
	fs.StringVar(&cfg.Endpoint, "endpoint", cfg.Endpoint, "third-party access endpoint shown to clients")
	fs.IntVar(&cfg.TimeoutSec, "timeout", cfg.TimeoutSec, "request timeout seconds")
	fs.BoolVar(&cfg.Pretty, "pretty", cfg.Pretty, "pretty-print JSON")
	return cfg, fs
}

func finalizeSrvConfig(cfg srvConfig) srvConfig {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Endpoint = normalizeBaseURL(firstNonEmpty(cfg.Endpoint, srvGatewayEndpointFromBase(cfg.BaseURL)))
	cfg.AuthToken = strings.TrimSpace(cfg.AuthToken)
	cfg.AdminToken = strings.TrimSpace(cfg.AdminToken)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.UserID = strings.TrimSpace(cfg.UserID)
	return cfg
}

func validateSrvURLs(cfg srvConfig) error {
	if err := validateHTTPURL(cfg.BaseURL, "--srv"); err != nil {
		return err
	}
	if err := validateHTTPURL(cfg.Endpoint, "--endpoint"); err != nil {
		return err
	}
	return nil
}

func validateSrvConfig(cfg srvConfig) error {
	if cfg.TimeoutSec < 0 {
		return errors.New("--timeout must be non-negative")
	}
	return nil
}

func validateHTTPURL(rawURL, label string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("missing %s URL", label)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid %s URL %q; use absolute http:// or https:// URL", label, rawURL)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("invalid %s URL %q; query and fragment are not supported", label, rawURL)
		}
		return nil
	default:
		return fmt.Errorf("invalid %s URL scheme %q; use http or https", label, u.Scheme)
	}
}

func requireSrvAdmin(cfg srvConfig) error {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return errors.New("missing --srv or MACLAWSRV_URL")
	}
	if strings.TrimSpace(cfg.AuthToken) != "" {
		return nil
	}
	if strings.TrimSpace(cfg.AdminToken) == "" {
		return errors.New("missing user API token for srv command; pass positional USER_API_TOKEN, --auth-token, or MACLAWSRV_AUTH_TOKEN. Use --admin-token --tenant --user only for owner/admin fallback")
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return errors.New("missing --tenant or MACLAWSRV_TENANT_ID")
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		return errors.New("missing --user or MACLAWSRV_USER_ID")
	}
	return nil
}

func (c *cli) getSrvUserAppConfig(cfg srvConfig) (corelib.AppConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 15))
	defer cancel()
	var out srvUserConfigResponse
	endpoint, token := srvConfigEndpointAndToken(cfg)
	if err := c.doJSON(ctx, http.MethodGet, endpoint, token, nil, &out); err != nil {
		return corelib.AppConfig{}, err
	}
	return out.AppConfig, nil
}

func (c *cli) updateSrvUserAppConfig(cfg srvConfig, app corelib.AppConfig) (corelib.AppConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration(cfg.TimeoutSec, 15))
	defer cancel()
	var out srvUserConfigResponse
	endpoint, token := srvConfigEndpointAndToken(cfg)
	if err := c.doJSON(ctx, http.MethodPut, endpoint, token, srvUserConfigUpdateRequest{AppConfig: app}, &out); err != nil {
		return corelib.AppConfig{}, err
	}
	return out.AppConfig, nil
}

func srvConfigEndpointAndToken(cfg srvConfig) (string, string) {
	if strings.TrimSpace(cfg.AuthToken) != "" {
		return cfg.BaseURL + "/api/v1/config", cfg.AuthToken
	}
	return fmt.Sprintf("%s/api/v1/admin/tenants/%s/users/%s/config", cfg.BaseURL, url.PathEscape(cfg.TenantID), url.PathEscape(cfg.UserID)), cfg.AdminToken
}

func srvGatewayEndpointFromBase(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	return base + "/api/im-gateway/v1"
}

func applySrvEndpointToApp(app *corelib.AppConfig, endpoint string) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" {
		return
	}
	host := u.Hostname()
	if host != "" {
		app.ThirdPartyGatewayHost = host
	}
	if port := u.Port(); port != "" {
		if value, err := strconv.Atoi(port); err == nil && value > 0 && value <= 65535 {
			app.ThirdPartyGatewayPort = value
		}
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		app.ThirdPartyGatewayPort = 443
	case "http":
		app.ThirdPartyGatewayPort = 80
	}
}

func srvThirdPartyConfigSummary(cfg srvConfig, app corelib.AppConfig, includeToken bool, tokenValue string) map[string]any {
	endpoint := normalizeBaseURL(firstNonEmpty(cfg.Endpoint, srvGatewayEndpointFromBase(cfg.BaseURL)))
	token := strings.TrimSpace(firstNonEmpty(tokenValue, app.ThirdPartyGatewayToken))
	out := map[string]any{
		"ok":           true,
		"srv":          cfg.BaseURL,
		"endpoint":     endpoint,
		"enabled":      app.ThirdPartyGatewayEnabled,
		"host":         app.ThirdPartyGatewayHost,
		"port":         app.ThirdPartyGatewayPort,
		"localMode":    app.IsThirdPartyGatewayLocalMode(),
		"tokenPresent": token != "",
	}
	if cfg.TenantID != "" {
		out["tenantId"] = cfg.TenantID
	}
	if cfg.UserID != "" {
		out["userId"] = cfg.UserID
	}
	if includeToken && token != "" {
		out["token"] = token
	}
	if token != "" {
		if includeToken {
			out["next"] = "maclaw-cli srv test " + shellQuote(cfg.BaseURL) + " " + shellQuote(token)
		} else {
			out["next"] = "maclaw-cli srv test " + shellQuote(cfg.BaseURL) + " <gateway-token>"
		}
	} else {
		out["next"] = "maclaw-cli srv token " + shellQuote(cfg.BaseURL) + " <user-api-token>"
	}
	out["clientUse"] = map[string]any{
		"baseUrl": endpoint,
		"token":   chooseTokenPlaceholder(includeToken && token != "", token),
		"test":    "maclaw-cli srv test --srv " + shellQuote(cfg.BaseURL) + " --gateway-token " + shellQuote(chooseTokenPlaceholder(includeToken && token != "", token)),
		"testArgv": []string{
			"maclaw-cli",
			"srv",
			"test",
			"--srv",
			cfg.BaseURL,
			"--gateway-token",
			chooseTokenPlaceholder(includeToken && token != "", token),
		},
		"env": map[string]string{
			"MACLAWSRV_URL":           cfg.BaseURL,
			"MACLAWSRV_GATEWAY_URL":   endpoint,
			"MACLAWSRV_GATEWAY_TOKEN": chooseTokenPlaceholder(includeToken && token != "", token),
		},
	}
	return out
}

func chooseTokenPlaceholder(show bool, token string) string {
	if show {
		return token
	}
	return "<token>"
}

func shellQuote(value string) string {
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r <= ' ' || strings.ContainsRune(`"'&|<>^()%!`, r)
	}) == -1 {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func (c *cli) applySessionWithRunLock(cfg config, create bool) (config, *stateLock, error) {
	cfg, explicit := normalizeSessionIDs(cfg)
	if cfg.RequireSession && !explicit {
		return cfg, nil, errors.New("missing explicit session; pass --session <id> for concurrent/agent use")
	}
	if explicit {
		runLock, err := acquireRunLock(cfg)
		if err != nil {
			return cfg, nil, err
		}
		cfg, err = c.applySession(cfg, create)
		if err != nil {
			runLock.Release()
			return cfg, nil, err
		}
		return cfg, runLock, nil
	}
	cursorOverride := strings.TrimSpace(cfg.Cursor) != ""
	var err error
	cfg, err = c.applySession(cfg, create)
	if err != nil {
		return cfg, nil, err
	}
	runLock, err := acquireRunLock(cfg)
	if err != nil {
		return cfg, nil, err
	}
	if !cursorOverride {
		cfg.Cursor = ""
	}
	cfg, err = c.applySession(cfg, create)
	if err != nil {
		runLock.Release()
		return cfg, nil, err
	}
	return cfg, runLock, nil
}

func normalizeSessionIDs(cfg config) (config, bool) {
	cfg.SessionID = strings.TrimSpace(cfg.SessionID)
	cfg.ConversationID = strings.TrimSpace(cfg.ConversationID)
	explicit := cfg.SessionID != "" || cfg.ConversationID != ""
	if cfg.SessionID == "" {
		cfg.SessionID = cfg.ConversationID
	}
	if cfg.ConversationID == "" {
		cfg.ConversationID = cfg.SessionID
	}
	return cfg, explicit
}

func (c *cli) applySession(cfg config, create bool) (config, error) {
	lock, err := acquireStateLockWithTimeout(cfg.StatePath, cfg.LockTimeoutSec)
	if err != nil {
		return cfg, err
	}
	defer lock.Release()
	return c.applySessionLocked(cfg, create)
}

func (c *cli) applySessionLocked(cfg config, create bool) (config, error) {
	cfg, explicitSession := normalizeSessionIDs(cfg)
	if cfg.RequireSession && !explicitSession {
		return cfg, errors.New("missing explicit session; pass --session <id> for concurrent/agent use")
	}
	st, err := loadCLIState(cfg.StatePath)
	if err != nil {
		return cfg, err
	}
	if explicitSession {
		now := c.now().UnixMilli()
		sess := findSessionForClient(st, cfg.SessionID, cfg.ClientID)
		if sess == nil {
			if legacy := findSessionLegacy(st, cfg.SessionID); legacy != nil {
				upsertSession(&st, sessionState{ID: cfg.SessionID, ClientID: cfg.ClientID, Cursor: legacy.Cursor, CreatedAt: legacy.CreatedAt, UpdatedAt: now})
				sess = findSessionForClient(st, cfg.SessionID, cfg.ClientID)
			}
		}
		if sess == nil && create {
			upsertSession(&st, sessionState{ID: cfg.SessionID, ClientID: cfg.ClientID, Cursor: "0", CreatedAt: now, UpdatedAt: now})
			sess = findSessionForClient(st, cfg.SessionID, cfg.ClientID)
		}
		if sess != nil && cfg.Cursor == "" {
			cfg.Cursor = firstNonEmpty(sess.Cursor, "0")
		}
		if cfg.Cursor == "" {
			cfg.Cursor = "0"
		}
		st.CurrentSession = cfg.SessionID
		if create {
			if err := saveCLIState(cfg.StatePath, st); err != nil {
				return cfg, err
			}
		}
		return cfg, nil
	}
	if strings.TrimSpace(st.CurrentSession) == "" && create {
		now := c.now().UnixMilli()
		st.CurrentSession = c.newSessionID()
		upsertSession(&st, sessionState{ID: st.CurrentSession, ClientID: cfg.ClientID, Cursor: "0", CreatedAt: now, UpdatedAt: now})
		if err := saveCLIState(cfg.StatePath, st); err != nil {
			return cfg, err
		}
	}
	cfg.SessionID = st.CurrentSession
	cfg.ConversationID = st.CurrentSession
	if cfg.Cursor == "" {
		sess := findSessionForClient(st, st.CurrentSession, cfg.ClientID)
		if sess == nil {
			if legacy := findSessionLegacy(st, st.CurrentSession); legacy != nil {
				now := c.now().UnixMilli()
				upsertSession(&st, sessionState{ID: st.CurrentSession, ClientID: cfg.ClientID, Cursor: legacy.Cursor, CreatedAt: legacy.CreatedAt, UpdatedAt: now})
				sess = findSessionForClient(st, st.CurrentSession, cfg.ClientID)
				if create {
					if err := saveCLIState(cfg.StatePath, st); err != nil {
						return cfg, err
					}
				}
			}
		}
		if sess != nil && sess.Cursor != "" {
			cfg.Cursor = sess.Cursor
		} else {
			cfg.Cursor = "0"
		}
	}
	return cfg, nil
}

func (c *cli) updateSessionCursor(cfg config, cursor string) error {
	if strings.TrimSpace(cfg.SessionID) == "" || strings.TrimSpace(cursor) == "" {
		return nil
	}
	lock, err := acquireStateLockWithTimeout(cfg.StatePath, cfg.LockTimeoutSec)
	if err != nil {
		return err
	}
	defer lock.Release()
	st, err := loadCLIState(cfg.StatePath)
	if err != nil {
		return err
	}
	now := c.now().UnixMilli()
	existing := findSessionForClient(st, cfg.SessionID, cfg.ClientID)
	created := now
	if existing != nil && existing.CreatedAt != 0 {
		created = existing.CreatedAt
	}
	upsertSession(&st, sessionState{ID: cfg.SessionID, ClientID: cfg.ClientID, Cursor: cursor, CreatedAt: created, UpdatedAt: now})
	st.CurrentSession = cfg.SessionID
	return saveCLIState(cfg.StatePath, st)
}

func (c *cli) newSessionID() string {
	suffix, err := randomHex(4)
	if err != nil {
		suffix = strconv.FormatInt(c.now().UnixNano(), 36)
	}
	return "sess_" + c.now().UTC().Format("20060102_150405") + "_" + suffix
}

func (c *cli) newMessageID(now int64) string {
	suffix, err := randomHex(4)
	if err != nil {
		suffix = strconv.FormatInt(c.now().UnixNano(), 36)
	}
	return fmt.Sprintf("maclaw_cli_%d_%s", now, suffix)
}

type discoveredGateway struct {
	OK         bool
	ConfigPath string
	BaseURL    string
	Token      string
}

func discoverGatewayConfig(path string) discoveredGateway {
	if strings.TrimSpace(path) == "" {
		path = defaultConfigPath()
	}
	out := discoveredGateway{ConfigPath: path}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return out
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return out
	}
	host := gatewayClientHost(cfg.ThirdPartyGatewayHost)
	port := cfg.ThirdPartyGatewayPort
	if port <= 0 {
		port = 18777
	}
	out.BaseURL = fmt.Sprintf("http://%s/api/im-gateway/v1", netJoinHostPortForURL(host, port))
	out.Token = strings.TrimSpace(cfg.ThirdPartyGatewayToken)
	out.OK = cfg.ThirdPartyGatewayEnabled && out.Token != ""
	return out
}

func loadAppConfig(path string) (corelib.AppConfig, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultConfigPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return corelib.AppConfigDefaults(), nil
		}
		return corelib.AppConfig{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) == 0 {
		return corelib.AppConfigDefaults(), nil
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return corelib.AppConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func saveAppConfig(path string, cfg corelib.AppConfig) error {
	if strings.TrimSpace(path) == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".maclaw", "config.json")
	}
	return filepath.Join(home, ".maclaw", "config.json")
}

func defaultStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".maclaw", "maclaw-cli", "state.json")
	}
	return filepath.Join(home, ".maclaw", "maclaw-cli", "state.json")
}

func loadCLIState(path string) (cliState, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultStatePath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cliState{}, nil
		}
		return cliState{}, fmt.Errorf("read state: %w", err)
	}
	if len(data) == 0 {
		return cliState{}, nil
	}
	var st cliState
	if err := json.Unmarshal(data, &st); err != nil {
		return cliState{}, fmt.Errorf("parse state: %w", err)
	}
	return st, nil
}

func saveCLIState(path string, st cliState) error {
	if strings.TrimSpace(path) == "" {
		path = defaultStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func withCLIState(statePath string, lockTimeoutSec int, save bool, fn func(*cliState) error) error {
	lock, err := acquireStateLockWithTimeout(statePath, lockTimeoutSec)
	if err != nil {
		return err
	}
	defer lock.Release()
	st, err := loadCLIState(statePath)
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	if save {
		return saveCLIState(statePath, st)
	}
	return nil
}

func acquireStateLock(statePath string) (*stateLock, error) {
	return acquireStateLockWithTimeout(statePath, defaultLockWait)
}

func acquireStateLockWithTimeout(statePath string, timeoutSec int) (*stateLock, error) {
	if strings.TrimSpace(statePath) == "" {
		statePath = defaultStatePath()
	}
	lockPath := statePath + ".lock"
	return acquireLockFile(lockPath, "state lock", timeoutSec)
}

func acquireRunLock(cfg config) (*stateLock, error) {
	if strings.TrimSpace(cfg.SessionID) == "" {
		return nil, nil
	}
	statePath := cfg.StatePath
	if strings.TrimSpace(statePath) == "" {
		statePath = defaultStatePath()
	}
	lockPath := sessionRunLockPath(statePath, cfg.ClientID, cfg.SessionID)
	lock, err := acquireLockFile(lockPath, "run lock", cfg.LockTimeoutSec)
	if err != nil {
		return nil, fmt.Errorf("%w for clientId=%q sessionId=%q", err, cfg.ClientID, cfg.SessionID)
	}
	return lock, nil
}

func sessionRunLockPath(statePath, clientID, sessionID string) string {
	if strings.TrimSpace(statePath) == "" {
		statePath = defaultStatePath()
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(clientID) + "\x00" + strings.TrimSpace(sessionID)))
	return filepath.Join(filepath.Dir(statePath), "runs", hex.EncodeToString(sum[:])+".lock")
}

func acquireLockFile(lockPath, label string, timeoutSec int) (*stateLock, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s dir: %w", label, err)
	}
	if timeoutSec <= 0 {
		timeoutSec = defaultLockWait
	}
	staleAfter := time.Duration(maxInt(timeoutSec*2, 60)) * time.Second
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	var lastErr error
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			token := newLockToken()
			if _, err := fmt.Fprintf(f, "%d %s\n", os.Getpid(), token); err != nil {
				_ = f.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("write %s: %w", label, err)
			}
			lock := &stateLock{path: lockPath, file: f, token: token, heartbeat: make(chan struct{})}
			lock.startHeartbeat(staleAfter)
			return lock, nil
		}
		lastErr = err
		if !isLockContentionError(err) {
			return nil, fmt.Errorf("acquire %s: %w", label, err)
		}
		if isStaleLock(lockPath, staleAfter) {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return nil, fmt.Errorf("%s busy: %s: %w", label, lockPath, lastErr)
			}
			return nil, fmt.Errorf("%s busy: %s", label, lockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func isLockContentionError(err error) bool {
	return os.IsExist(err) || os.IsPermission(err)
}

func isStaleLock(lockPath string, staleAfter time.Duration) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > staleAfter
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (l *stateLock) Release() {
	if l == nil {
		return
	}
	if l.heartbeat != nil {
		close(l.heartbeat)
		l.heartbeat = nil
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	if l.path != "" {
		if lockFileHasToken(l.path, l.token) {
			_ = os.Remove(l.path)
		}
	}
}

func (l *stateLock) startHeartbeat(staleAfter time.Duration) {
	if l == nil || l.path == "" || l.heartbeat == nil {
		return
	}
	path := l.path
	token := l.token
	stop := l.heartbeat
	interval := staleAfter / 3
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !lockFileHasToken(path, token) {
					return
				}
				now := time.Now()
				_ = os.Chtimes(path, now, now)
			case <-stop:
				return
			}
		}
	}()
}

func newLockToken() string {
	suffix, err := randomHex(8)
	if err != nil {
		suffix = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return fmt.Sprintf("%d-%s", os.Getpid(), suffix)
}

func lockFileHasToken(path, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, field := range strings.Fields(string(data)) {
		if field == token {
			return true
		}
	}
	return false
}

func findSession(st cliState, id string) *sessionState {
	for i := range st.Sessions {
		if st.Sessions[i].ID == id {
			return &st.Sessions[i]
		}
	}
	return nil
}

func findSessionForClient(st cliState, id, clientID string) *sessionState {
	for i := range st.Sessions {
		if st.Sessions[i].ID != id {
			continue
		}
		if st.Sessions[i].ClientID == clientID {
			return &st.Sessions[i]
		}
	}
	return nil
}

func findSessionLegacy(st cliState, id string) *sessionState {
	for i := range st.Sessions {
		if st.Sessions[i].ID == id && st.Sessions[i].ClientID == "" {
			return &st.Sessions[i]
		}
	}
	return nil
}

func hasSessionID(st cliState, id string) bool {
	for _, sess := range st.Sessions {
		if sess.ID == id {
			return true
		}
	}
	return false
}

func findSessionForClientOrLegacy(st cliState, id, clientID string) *sessionState {
	if sess := findSessionForClient(st, id, clientID); sess != nil {
		return sess
	}
	return findSessionLegacy(st, id)
}

func upsertSession(st *cliState, sess sessionState) {
	if strings.TrimSpace(sess.ID) == "" {
		return
	}
	for i := range st.Sessions {
		if st.Sessions[i].ID == sess.ID && st.Sessions[i].ClientID == sess.ClientID {
			if sess.CreatedAt == 0 {
				sess.CreatedAt = st.Sessions[i].CreatedAt
			}
			if sess.Cursor == "" {
				sess.Cursor = st.Sessions[i].Cursor
			}
			st.Sessions[i] = sess
			return
		}
	}
	st.Sessions = append(st.Sessions, sess)
}

func removeSessionForClient(sessions []sessionState, id, clientID string) ([]sessionState, bool) {
	out := sessions[:0]
	removed := false
	for _, sess := range sessions {
		if sess.ID == id && (sess.ClientID == clientID || sess.ClientID == "") {
			removed = true
			continue
		}
		out = append(out, sess)
	}
	return out, removed
}

func netJoinHostPortForURL(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func gatewayClientHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func buildMessagePayload(text, messageJSON, attachmentsJSON string) (coreim.ThirdPartyMessagePayload, error) {
	if strings.TrimSpace(messageJSON) != "" {
		var payload coreim.ThirdPartyMessagePayload
		if err := decodeStrictJSON([]byte(messageJSON), &payload); err != nil {
			return payload, fmt.Errorf("decode --message-json: %w", err)
		}
		if err := validateMessagePayloadType(payload); err != nil {
			return payload, err
		}
		return payload, nil
	}
	var attachments []coreim.ThirdPartyMediaReference
	if strings.TrimSpace(attachmentsJSON) != "" {
		if err := decodeStrictJSON([]byte(attachmentsJSON), &attachments); err != nil {
			return coreim.ThirdPartyMessagePayload{}, fmt.Errorf("decode --attachments-json: %w", err)
		}
		if err := validateAttachmentTypes(attachments); err != nil {
			return coreim.ThirdPartyMessagePayload{}, err
		}
	}
	msgType := "text"
	if len(attachments) > 0 {
		msgType = firstNonEmpty(attachments[0].Type, "file")
	}
	return coreim.ThirdPartyMessagePayload{Type: msgType, Text: text, Attachments: attachments}, nil
}

func messagePayloadJSONArg(raw map[string]any) (string, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	var payload coreim.ThirdPartyMessagePayload
	if err := decodeStrictJSON(data, &payload); err != nil {
		return "", fmt.Errorf("decode message: %w", err)
	}
	if err := validateMessagePayloadType(payload); err != nil {
		return "", err
	}
	return string(data), nil
}

func attachmentsJSONArg(raw []any) (string, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	var attachments []coreim.ThirdPartyMediaReference
	if err := decodeStrictJSON(data, &attachments); err != nil {
		return "", fmt.Errorf("decode attachments: %w", err)
	}
	if err := validateAttachmentTypes(attachments); err != nil {
		return "", err
	}
	return string(data), nil
}

func validateMessagePayloadType(payload coreim.ThirdPartyMessagePayload) error {
	if strings.TrimSpace(payload.Type) == "" {
		return nil
	}
	if coreim.NormalizeThirdPartyMessageType(payload.Type) == "" {
		return errors.New("message.type must be text, image, file, voice, or audio")
	}
	return nil
}

func validateAttachmentTypes(attachments []coreim.ThirdPartyMediaReference) error {
	for i, att := range attachments {
		if strings.TrimSpace(att.Type) == "" {
			continue
		}
		normalized := coreim.NormalizeThirdPartyMessageType(att.Type)
		if normalized == "" || normalized == "text" {
			return fmt.Errorf("attachments[%d].type must be image, file, voice, or audio", i)
		}
	}
	return nil
}

func decodeStrictJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func readTools(path string) ([]coreim.ThirdPartyToolDefinition, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tools []coreim.ThirdPartyToolDefinition
	if err := decodeStrictJSON(data, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func validateToolDefinitions(tools []coreim.ThirdPartyToolDefinition) ([]string, error) {
	if len(tools) > coreim.ThirdPartyMaxTools {
		return nil, fmt.Errorf("tools exceeds %d items", coreim.ThirdPartyMaxTools)
	}
	names := make([]string, 0, len(tools))
	seen := map[string]bool{}
	for i := range tools {
		if err := coreim.NormalizeThirdPartyToolDefinition(&tools[i], i); err != nil {
			return nil, err
		}
		if seen[tools[i].Name] {
			return nil, fmt.Errorf("tools[%d].name duplicate %q", i, tools[i].Name)
		}
		seen[tools[i].Name] = true
		names = append(names, tools[i].Name)
	}
	return names, nil
}

func readJSONObject(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	return out, nil
}

func readStringMap(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decode JSON string object: %w", err)
	}
	return out, nil
}

func metadataJSONArg(raw map[string]any) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		s, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("metadata %q must be a string", key)
		}
		out[key] = s
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func requireToken(cfg config) error {
	if strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("cannot discover MaClaw third-party gateway token; enable GUI IM third-party access, or set MACLAW_GATEWAY_TOKEN (checked config: %s)", cfg.ConfigPath)
	}
	return nil
}

func writeJSON(w io.Writer, v any, pretty bool) error {
	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(v, "", "  ")
	} else {
		data, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func writeJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func wantsJSONErrors(args []string) bool {
	if strings.EqualFold(os.Getenv("MACLAW_JSON_ERRORS"), "1") || strings.EqualFold(os.Getenv("MACLAW_JSON_ERRORS"), "true") {
		return true
	}
	if len(args) > 0 && args[0] == "invoke" {
		return true
	}
	for _, arg := range args {
		if arg == "--json-errors" || arg == "--json-errors=true" {
			return true
		}
	}
	return false
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func cleanPromptArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func reorderKnownFlags(fs *flag.FlagSet, args []string) []string {
	if len(args) == 0 {
		return args
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i:]...)
			break
		}
		name, hasValue := flagName(arg)
		f := fs.Lookup(name)
		if name == "" || f == nil {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		if hasValue || isBoolFlag(f) {
			continue
		}
		if i+1 >= len(args) {
			continue
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, positionals...)
}

func hasNamedFlag(args []string, want string) bool {
	for _, arg := range args {
		name, _ := flagName(arg)
		if name == want {
			return true
		}
	}
	return false
}

func flagName(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
		return "", false
	}
	trimmed := strings.TrimLeft(arg, "-")
	if trimmed == "" {
		return "", false
	}
	name, value, hasValue := strings.Cut(trimmed, "=")
	if name == "" {
		return "", false
	}
	_ = value
	return name, hasValue
}

func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface {
		IsBoolFlag() bool
	}
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

func timeoutDuration(seconds, fallback int) time.Duration {
	if seconds <= 0 {
		seconds = fallback
	}
	return time.Duration(seconds) * time.Second
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func usage() string {
	return `maclaw-cli controls MaClaw through the third-party IM gateway protocol.

Usage:
  maclaw-cli version
  maclaw-cli health [flags]
  maclaw-cli agent-help
  maclaw-cli agent-spec
  maclaw-cli invoke-schema
  maclaw-cli invoke [--dry-run] [--json '{...}' | --input request.json]
  maclaw-cli doctor [flags]
  maclaw-cli bootstrap [--host 127.0.0.1] [--port 18777] [flags]
  maclaw-cli srv setup|show|disable|token|test [flags]
  maclaw-cli srv tools validate --file tools.json
  maclaw-cli session new [--id SESSION]
  maclaw-cli session use SESSION
  maclaw-cli session rename OLD NEW
  maclaw-cli session delete SESSION
  maclaw-cli session reset-cursor [--id SESSION]
  maclaw-cli session current
  maclaw-cli session list
  maclaw-cli handshake [--tools tools.json] [flags]
  maclaw-cli send --text "hello" [flags]
  maclaw-cli ask --text "do this" [--wait-polls 2] [flags]
  maclaw-cli continue "keep going" [flags]
  maclaw-cli poll [--cursor 0] [--timeout 30] [--ack=true] [flags]
  maclaw-cli watch [--count 10] [--ack=true] [flags]
  maclaw-cli ack --ids msg1,msg2 [flags]
  maclaw-cli tool-result --tool-call-id tc_1 --status success --result-json '{"ok":true}' [--metadata-json '{"source":"agent"}'] [flags]

Common flags:
  --base URL             default http://127.0.0.1:18777/api/im-gateway/v1 or MACLAW_GATEWAY_URL
  --token TOKEN          default MACLAW_GATEWAY_TOKEN, otherwise auto-read from ~/.maclaw/config.json
  --config PATH          default ~/.maclaw/config.json or MACLAW_CONFIG
  --state PATH           default ~/.maclaw/maclaw-cli/state.json or MACLAW_CLI_STATE
  --client ID            default maclaw-cli or MACLAW_CLIENT_ID
  --client-name NAME     default MaClaw CLI or MACLAW_CLIENT_NAME
  --session ID           explicit session id; recommended for agents/concurrency
  --conversation ID      protocol conversation id; overrides persisted session
  --require-session      fail unless explicit --session/--conversation/MACLAW_SESSION_ID is set
  --lock-timeout SEC     state/run lock wait timeout; default 5 or MACLAW_CLI_LOCK_TIMEOUT_SEC
  --json-errors          emit machine-readable JSON errors to stderr
  --user ID              default agent or MACLAW_USER_ID
  --name NAME            default Agent or MACLAW_USER_NAME
  --pretty=false         emit compact JSON
`
}

func srvUsage() string {
	return `maclaw-cli srv manages MaClawSrv remote third-party access.

Usage:
  maclaw-cli srv setup URL USER_API_TOKEN
  maclaw-cli srv info URL USER_API_TOKEN
  maclaw-cli srv test URL GATEWAY_TOKEN
  maclaw-cli srv setup --srv URL --auth-token USER_API_TOKEN
  maclaw-cli srv token URL USER_API_TOKEN
  maclaw-cli srv disable --srv URL --auth-token USER_API_TOKEN
  maclaw-cli srv show --srv URL --auth-token USER_API_TOKEN
  maclaw-cli srv tools validate --file tools.json

Remote access flags:
  --srv URL          MaClawSrv base URL; env MACLAWSRV_URL; http/https absolute URL
  --auth-token T     user API bearer token; env MACLAWSRV_AUTH_TOKEN
  --user-token T     alias for --auth-token
  --gateway-token T  third-party gateway token; for test env MACLAWSRV_GATEWAY_TOKEN; use auto to generate
  --endpoint URL     optional http/https gateway URL override; default <srv>/api/im-gateway/v1
  --pretty=false     emit compact JSON

Admin fallback:
  --admin-token T --tenant ID --user ID can configure another user when owner admin access is required.

Notes:
  srv setup is short for srv thirdparty setup. setup/connect/enable are aliases for set. info prints integration credentials. token is an alias for rotate-token. check is an alias for test.
  The gateway endpoint is shared by the server. The bearer token selects the MaClawSrv user.
  set/rotate-token print a newly generated token once. show does not reveal tokens unless --include-token is used.
  Industrial tools are not stored in MaClawSrv user config. The client advertises tools during handshake; validate tools.json before use.
`
}

func agentUsage() string {
	return `# maclaw-cli Agent Usage

Purpose:
  Use maclaw-cli to send work requests to MaClaw GUI on the same machine and
  keep task state across one-shot subprocess calls.

Golden rule for automation:
  Always pass --client, --session, and --require-session.

Recommended call:
  maclaw-cli continue --require-session --client <agent-id> --session <task-id> "<instruction>"

First-time local setup:
  maclaw-cli bootstrap
  maclaw-cli doctor

Remote MaClawSrv user setup:
  maclaw-cli srv setup <maclawsrv-url> <user-api-token>
  maclaw-cli srv test <maclawsrv-url> <new-gateway-token>
  maclaw-cli srv tools validate --file tools.json

State model:
  State file: ~/.maclaw/maclaw-cli/state.json
  State key: clientId + sessionId
  Cursor is saved per clientId + sessionId after ask/continue/poll/watch.
  Do not use "session use" from automation; it mutates global currentSession.

Command choices:
  continue   Send text and wait for replies. Best default.
  send       Send text only. Use when another process will poll.
  poll       Fetch replies for one session.
  watch      Long-running JSONL poll loop.
  doctor     Verify GUI gateway health and auth.
  bootstrap  Write local GUI gateway config if missing.

Examples:
  maclaw-cli continue --require-session --client planner --session task-123 "Plan next step"
  maclaw-cli continue --require-session --client executor --session task-123 "Execute plan"
  maclaw-cli poll --require-session --client planner --session task-123
  echo "Continue from previous result" | maclaw-cli continue --stdin --require-session --client planner --session task-123

Output:
  Normal commands print JSON to stdout.
  watch prints one JSON object per line.
  Errors print to stderr and exit non-zero.
  Use --json-errors or MACLAW_JSON_ERRORS=1 for JSON error envelopes.

Important fields in ask/continue JSON:
  sessionId       Saved state/session key used.
  conversationId  Protocol conversation id used.
  messages        Assistant replies and tool calls.
  nextCursor      Cursor saved for next call.
  hasMore         More replies may be available.

Concurrency:
  Different clientId + sessionId pairs are independent.
  Same clientId + sessionId shares cursor and is serialized by a run lock.
  Stateful commands covered by the run lock:
    continue, ask, send, poll, watch, tool-result.
  A second process for the same key waits, then exits with "run lock busy"
  if the first process does not finish before timeout.

Tool calls:
  Register tools:
    maclaw-cli handshake --tools tools.json --client <agent-id> --client-name "<agent name>"
  Submit result:
    maclaw-cli tool-result --tool-call-id <id> --status success --result-json '{"ok":true}' --require-session --client <agent-id> --session <task-id>

Environment alternatives:
  MACLAW_CLIENT_ID=<agent-id>
  MACLAW_SESSION_ID=<task-id>
  MACLAW_REQUIRE_SESSION=1

MaClawSrv remote environment:
  MACLAWSRV_URL=<server-url>
  MACLAWSRV_AUTH_TOKEN=<user-api-token>
  MACLAWSRV_GATEWAY_TOKEN=<third-party-gateway-token>
  MACLAWSRV_GATEWAY_URL=<optional-public-gateway-url>

Recovery:
  If auth/config fails: maclaw-cli bootstrap, then restart MaClaw GUI gateway.
  If repeated old replies appear: maclaw-cli session reset-cursor --id <task-id> or pass --cursor.
`
}

func agentSpec() map[string]any {
	return map[string]any{
		"name":        "maclaw-cli",
		"version":     cliVersion,
		"purpose":     "Control and schedule MaClaw GUI through the local third-party gateway from one-shot agent subprocesses.",
		"goldenRule":  "Automated agents must pass --require-session, --client, and --session on every stateful call.",
		"recommended": "maclaw-cli continue --require-session --client <agent-id> --session <task-id> \"<instruction>\"",
		"manifest":    "maclaw-cli/manifest.json",
		"install": map[string]string{
			"windows": "maclaw-cli\\install.ps1",
			"unix":    "./maclaw-cli/install.sh",
			"build":   "go build -o maclaw-cli ./maclaw-cli",
		},
		"state": map[string]any{
			"path":           "~/.maclaw/maclaw-cli/state.json",
			"lockPath":       "~/.maclaw/maclaw-cli/state.json.lock",
			"runLockDir":     "~/.maclaw/maclaw-cli/runs/",
			"key":            "clientId + sessionId. If --conversation is provided with --session, --session remains the state key and --conversation is only the protocol id.",
			"cursor":         "Saved per clientId + sessionId after ask/continue/poll/watch.",
			"doNotUse":       "session use in automation",
			"concurrency":    "Different clientId + sessionId pairs are independent. Stateful calls with the same key are serialized by a run lock; a second process waits and then fails with 'run lock busy' after timeout.",
			"lockTimeoutSec": defaultLockWait,
			"lockedCommands": []string{"continue", "ask", "send", "poll", "watch", "tool-result"},
		},
		"setup": []map[string]any{
			{"command": "maclaw-cli bootstrap", "when": "First local setup or missing token/config."},
			{"command": "maclaw-cli doctor", "when": "Check GUI gateway health and auth."},
			{"command": "maclaw-cli srv setup <url> <user-api-token>", "when": "Enable remote MaClawSrv third-party access for the authenticated user. Generates a gateway token if missing."},
		},
		"remoteAccess": map[string]any{
			"purpose": "Configure and verify remote MaClawSrv third-party gateway access for the authenticated user.",
			"simpleFlow": []string{
				"maclaw-cli srv setup <maclawsrv-url> <user-api-token>",
				"maclaw-cli srv info <maclawsrv-url> <user-api-token>",
				"maclaw-cli srv test <maclawsrv-url> <gateway-token>",
			},
			"commands": map[string]any{
				"setup":   "Enable access. Generates and prints a gateway token only when no token exists or --gateway-token is supplied.",
				"info":    "Print existing integration credentials intentionally for handoff to a third-party client.",
				"show":    "Inspect current remote access status without printing the token by default.",
				"token":   "Rotate gateway token and print the new value once.",
				"disable": "Disable third-party gateway access for the authenticated user.",
				"test":    "Handshake against <srv>/api/im-gateway/v1 using the gateway token.",
			},
			"tokens": map[string]string{
				"userApiToken": "Second positional for setup/info/show/token/disable. Authenticates to MaClawSrv and selects the user.",
				"gatewayToken": "Second positional for test, or MACLAWSRV_GATEWAY_TOKEN for test only. Bearer token used by third-party protocol clients.",
			},
			"urlRules": "--srv and --endpoint must be absolute http:// or https:// URLs without query strings or fragments.",
			"output": map[string]string{
				"token":              "Present only when a new token is generated, explicitly set, rotated, or info/include-token intentionally reveals an existing token.",
				"tokenPresent":       "Boolean; true when a gateway token exists even if token is redacted.",
				"next":               "Human-readable next command. Prefer clientUse.testArgv or clientUse.env in automation.",
				"clientUse.baseUrl":  "Gateway protocol base URL for third-party clients.",
				"clientUse.token":    "Gateway bearer token when intentionally revealed; otherwise <token>.",
				"clientUse.testArgv": "Array argv for testing gateway access without shell parsing.",
				"clientUse.env":      "Environment map for testing with maclaw-cli srv test and for passing credentials to another client.",
			},
			"normalMode":    "No tenant id or user id is required; the user API token identifies the MaClawSrv user.",
			"adminFallback": "Use --admin-token --tenant --user only when an owner configures another user.",
		},
		"commands": map[string]any{
			"invoke-schema": map[string]any{
				"use":      "Print JSON Schema for invoke requests.",
				"template": "maclaw-cli invoke-schema",
				"stdout":   "JSON Schema",
			},
			"invoke": map[string]any{
				"use":      "Machine-friendly JSON request wrapper around continue/send/poll/handshake/ack/tool-result/doctor/bootstrap.",
				"template": "echo '{\"action\":\"continue\",\"clientId\":\"planner\",\"sessionId\":\"task-123\",\"text\":\"Continue\"}' | maclaw-cli invoke",
				"stdin":    "JSON invokeRequest",
				"stdout":   "Same as selected action",
				"dryRun":   "echo '{\"action\":\"continue\",\"clientId\":\"planner\",\"sessionId\":\"task-123\",\"text\":\"Continue\"}' | maclaw-cli invoke --dry-run prints parsed argv without calling MaClaw.",
			},
			"continue": map[string]any{
				"use":       "Best default: send text and wait for replies.",
				"template":  "maclaw-cli continue --require-session --client <agent-id> --session <task-id> \"<instruction>\"",
				"stdout":    "JSON askResult",
				"stateful":  true,
				"updates":   "incoming message, poll cursor",
				"stdin":     "echo \"...\" | maclaw-cli continue --stdin --require-session --client <agent-id> --session <task-id>",
				"avoidWhen": "Use send if another process will poll asynchronously.",
			},
			"send": map[string]any{
				"use":      "Send text only; does not wait for assistant replies.",
				"template": "maclaw-cli send --require-session --client <agent-id> --session <task-id> --text \"<instruction>\"",
				"stdout":   "ThirdPartyIncomingAcceptedResponse JSON",
				"stateful": true,
			},
			"poll": map[string]any{
				"use":      "Fetch pending replies and advance saved cursor.",
				"template": "maclaw-cli poll --require-session --client <agent-id> --session <task-id>",
				"stdout":   "ThirdPartyOutgoingPollResponse JSON",
				"stateful": true,
			},
			"watch": map[string]any{
				"use":      "Long-running poll loop. Emits JSONL.",
				"template": "maclaw-cli watch --require-session --client <agent-id> --session <task-id> --count 10",
				"stdout":   "JSONL; one outgoing message per line",
				"stateful": true,
			},
			"handshake": map[string]any{
				"use":      "Authenticate and optionally register client-side tool definitions.",
				"template": "maclaw-cli handshake --tools tools.json --client <agent-id> --client-name \"<agent name>\"",
				"stdout":   "Gateway handshake JSON",
				"stateful": false,
				"note":     "Client-scoped; does not read or update saved session cursor.",
			},
			"tool-result": map[string]any{
				"use":      "Submit result for a tool_call/tool_plan step.",
				"template": "maclaw-cli tool-result --tool-call-id <id> --status success --result-json '{\"ok\":true}' --metadata-json '{\"source\":\"agent\"}' --require-session --client <agent-id> --session <task-id>",
				"statuses": []string{"success", "error", "rejected", "cancelled", "timeout"},
				"stdout":   "Gateway accepted response JSON",
				"stateful": true,
			},
			"doctor": map[string]any{
				"use":      "Diagnose local gateway connectivity and token auth.",
				"template": "maclaw-cli doctor",
				"stdout":   "JSON diagnostic report",
			},
			"bootstrap": map[string]any{
				"use":      "Write local gateway settings into MaClaw GUI config.",
				"template": "maclaw-cli bootstrap",
				"stdout":   "JSON setup report; does not print token",
			},
			"srv thirdparty": map[string]any{
				"use":      "Configure remote MaClawSrv user third-party gateway access address and token through the admin API.",
				"template": "maclaw-cli srv setup <maclawsrv-url> <user-api-token>",
				"actions":  []string{"setup", "connect", "info", "show", "set", "disable", "token", "rotate-token", "test", "check"},
				"stdout":   "JSON remote gateway setup report. setup/token print a new token once when generated; show redacts existing token by default; info prints existing integration credentials intentionally. next contains a human command; clientUse.testArgv and clientUse.env are structured for agents.",
				"stateful": false,
				"note":     "No tenant/user id is needed in normal mode. The user API token configures that user; the gateway token identifies that user for third-party protocol calls. Admin fallback accepts --admin-token --tenant --user.",
			},
			"srv tools validate": map[string]any{
				"use":      "Validate industrial/client tool definitions before handshake registration.",
				"template": "maclaw-cli srv tools validate --file tools.json",
				"stdout":   "JSON validation report with normalized tool names.",
				"stateful": false,
				"note":     "Tools are not stored in MaClawSrv user config; clients advertise them during handshake.",
			},
		},
		"requiredFlagsForAutomation": []string{"--require-session", "--client", "--session"},
		"importantFlags": map[string]string{
			"--client":          "Stable calling agent id. Part of state key.",
			"--client-name":     "Human-readable client display name.",
			"--session":         "Stable task/conversation id. Part of state key and default protocol conversationId.",
			"--conversation":    "Protocol conversation id override. With --session, it does not change the saved state key.",
			"--require-session": "Fail fast if explicit session is missing.",
			"--lock-timeout":    "State/run lock wait timeout seconds. Default 5.",
			"--json-errors":     "Emit machine-readable JSON error envelope to stderr on failure.",
			"--state":           "Override state file path.",
			"--config":          "Override MaClaw GUI config path.",
			"--cursor":          "Override poll cursor for one call.",
			"--ack":             "Ack received messages. Default true.",
			"--metadata-json":   "JSON string object metadata for send/continue/tool-result.",
		},
		"outputFields": map[string]any{
			"askResult":            []string{"incoming", "sessionId", "conversationId", "messages", "nextCursor", "hasMore"},
			"outgoingMessageTypes": []string{"text", "image", "file", "voice", "audio", "tool_call", "tool_plan", "tool_cancel"},
			"toolCallPath":         "messages[].toolCall",
			"toolPlanPath":         "messages[].toolPlan",
		},
		"environment": map[string]string{
			"MACLAW_CLIENT_ID":            "Default --client.",
			"MACLAW_CLIENT_NAME":          "Default --client-name.",
			"MACLAW_SESSION_ID":           "Default --session.",
			"MACLAW_CONVERSATION_ID":      "Default --conversation protocol override.",
			"MACLAW_REQUIRE_SESSION":      "Set 1/true to require explicit sessions.",
			"MACLAW_JSON_ERRORS":          "Set 1/true to emit machine-readable error JSON to stderr.",
			"MACLAW_CLI_LOCK_TIMEOUT_SEC": "Default --lock-timeout.",
			"MACLAW_CLI_STATE":            "State file path.",
			"MACLAW_CONFIG":               "MaClaw GUI config path.",
			"MACLAW_GATEWAY_URL":          "Gateway base URL override.",
			"MACLAW_GATEWAY_TOKEN":        "Bearer token override.",
			"MACLAW_USER_ID":              "Default --user.",
			"MACLAW_USER_NAME":            "Default --name.",
			"MACLAWSRV_URL":               "Default --srv for remote MaClawSrv admin commands.",
			"MACLAWSRV_AUTH_TOKEN":        "Default --auth-token/--user-token user API token for remote MaClawSrv commands.",
			"MACLAWSRV_GATEWAY_TOKEN":     "Default --gateway-token for remote MaClawSrv gateway test calls.",
			"MACLAWSRV_ADMIN_TOKEN":       "Default --admin-token for admin fallback.",
			"MACLAWSRV_TENANT_ID":         "Default --tenant for admin fallback.",
			"MACLAWSRV_USER_ID":           "Default --user for admin fallback.",
			"MACLAWSRV_GATEWAY_URL":       "Default --endpoint for remote MaClawSrv third-party gateway URL.",
		},
		"antiPatterns": []string{
			"Do not use 'session use' in automation.",
			"Do not intentionally run two long polls for the same clientId + sessionId; the second command will wait on the run lock.",
			"Do not omit --client in multi-agent systems.",
			"Do not submit tool-result without toolCallId or toolPlanId.",
			"Do not parse stderr as data; use stdout JSON only.",
		},
	}
}

func invokeSchema() map[string]any {
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://rapidai.local/maclaw-cli/invoke.schema.json",
		"title":                "maclaw-cli invoke request",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Command action. Defaults to continue.",
				"enum":        []string{"continue", "ask", "run", "send", "poll", "watch", "handshake", "ack", "tool-result", "doctor", "bootstrap"},
			},
			"baseUrl":        map[string]any{"type": "string", "description": "Gateway base URL override."},
			"token":          map[string]any{"type": "string", "description": "Gateway bearer token override."},
			"configPath":     map[string]any{"type": "string", "description": "MaClaw GUI config path override."},
			"statePath":      map[string]any{"type": "string", "description": "maclaw-cli state file path override."},
			"bootstrapHost":  map[string]any{"type": "string", "description": "Host written by bootstrap."},
			"bootstrapPort":  map[string]any{"type": "integer", "minimum": 1, "maximum": 65535, "description": "Port written by bootstrap."},
			"forceToken":     map[string]any{"type": "boolean", "description": "Replace existing gateway token during bootstrap."},
			"clientId":       map[string]any{"type": "string", "description": "Stable calling agent id; part of state key."},
			"clientName":     map[string]any{"type": "string", "description": "Human-readable client display name."},
			"sessionId":      map[string]any{"type": "string", "description": "Stable task/session id; part of state key."},
			"conversationId": map[string]any{"type": "string", "description": "Protocol conversation id override. With sessionId, it does not change the saved state key."},
			"userId":         map[string]any{"type": "string", "description": "External user id for the incoming message."},
			"userName":       map[string]any{"type": "string", "description": "External user display name for the incoming message."},
			"text":           map[string]any{"type": "string", "description": "Prompt text or tool-result text."},
			"message":        messagePayloadSchema(),
			"attachments":    map[string]any{"type": "array", "description": "Strict ThirdPartyMediaReference array for send.", "items": mediaReferenceSchema()},
			"eventId":        map[string]any{"type": "string", "description": "Stable incoming event id for send idempotency."},
			"messageId":      map[string]any{"type": "string", "description": "Stable source message id for send idempotency."},
			"timeoutSec":     map[string]any{"type": "integer", "minimum": 0},
			"lockTimeoutSec": map[string]any{"type": "integer", "minimum": 1, "description": "State/run lock wait timeout seconds."},
			"limit":          map[string]any{"type": "integer", "minimum": 1},
			"count":          map[string]any{"type": "integer", "minimum": 1, "description": "watch poll count; omit for endless watch."},
			"cursor":         map[string]any{"type": "string"},
			"waitPolls":      map[string]any{"type": "integer", "minimum": 1},
			"ack":            map[string]any{"type": "boolean"},
			"requireSession": map[string]any{"type": "boolean", "default": true, "description": "Add --require-session for stateful actions."},
			"pretty":         map[string]any{"type": "boolean"},
			"toolsPath":      map[string]any{"type": "string"},
			"messageIds":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"status":         map[string]any{"type": "string", "description": "Action-specific status. ack: delivered/read/failed. tool-result: success/error/rejected/cancelled/timeout."},
			"toolCallId":     map[string]any{"type": "string"},
			"toolPlanId":     map[string]any{"type": "string"},
			"stepId":         map[string]any{"type": "string"},
			"resultId":       map[string]any{"type": "string"},
			"idempotencyKey": map[string]any{"type": "string", "description": "Stable idempotency key for tool-result."},
			"result":         map[string]any{"type": "object", "additionalProperties": true},
			"errorCode":      map[string]any{"type": "string"},
			"errorMessage":   map[string]any{"type": "string"},
			"errorRetryable": map[string]any{"type": "boolean", "description": "Set tool-result error.retryable."},
			"metadata":       map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "String metadata map for send, continue, or tool-result."},
		},
		"allOf": []map[string]any{
			{
				"if":   map[string]any{"not": map[string]any{"required": []string{"action"}}},
				"then": map[string]any{"required": []string{"text"}},
			},
			{
				"if": map[string]any{"properties": map[string]any{"action": map[string]any{"const": "ack"}}, "required": []string{"action"}},
				"then": map[string]any{
					"required": []string{"messageIds"},
					"properties": map[string]any{
						"status": map[string]any{"enum": []string{"delivered", "delivery", "read", "failed", "error", "ok", "success"}},
					},
				},
			},
			{
				"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"enum": []string{"continue", "ask", "run"}}}, "required": []string{"action"}},
				"then": map[string]any{"required": []string{"text"}},
			},
			{
				"if": map[string]any{"properties": map[string]any{"action": map[string]any{"const": "send"}}, "required": []string{"action"}},
				"then": map[string]any{
					"anyOf": []map[string]any{
						{"required": []string{"text"}},
						{"required": []string{"message"}},
					},
				},
			},
			{
				"if": map[string]any{"properties": map[string]any{"action": map[string]any{"const": "tool-result"}}, "required": []string{"action"}},
				"then": map[string]any{
					"required": []string{"status"},
					"properties": map[string]any{
						"status": map[string]any{"enum": []string{"success", "error", "rejected", "cancelled", "canceled", "timeout"}},
					},
					"anyOf": []map[string]any{
						{"required": []string{"toolCallId"}},
						{"required": []string{"toolPlanId"}},
					},
				},
			},
		},
		"examples": []map[string]any{
			{"action": "continue", "clientId": "planner", "sessionId": "task-123", "text": "Continue the task", "requireSession": true},
			{"action": "poll", "clientId": "planner", "sessionId": "task-123"},
			{"action": "tool-result", "clientId": "desktop-agent", "sessionId": "task-123", "toolCallId": "tc_001", "status": "success", "result": map[string]any{"ok": true}},
		},
	}
}

func messagePayloadSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          "Strict ThirdPartyMessagePayload for send.",
		"additionalProperties": false,
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"type":        map[string]any{"type": "string", "enum": []string{"text", "image", "file", "voice", "audio"}},
			"text":        map[string]any{"type": "string"},
			"fileName":    map[string]any{"type": "string"},
			"contentType": map[string]any{"type": "string"},
			"mimeType":    map[string]any{"type": "string"},
			"data":        map[string]any{"type": "string", "description": "Base64 direct media data."},
			"url":         map[string]any{"type": "string"},
			"sizeBytes":   map[string]any{"type": "integer", "minimum": 0},
			"durationMs":  map[string]any{"type": "integer", "minimum": 0},
			"attachments": map[string]any{
				"type":  "array",
				"items": mediaReferenceSchema(),
			},
		},
	}
}

func mediaReferenceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"type":        map[string]any{"type": "string", "enum": []string{"image", "file", "voice", "audio"}},
			"fileName":    map[string]any{"type": "string"},
			"contentType": map[string]any{"type": "string"},
			"mimeType":    map[string]any{"type": "string"},
			"data":        map[string]any{"type": "string", "description": "Base64 direct media data."},
			"url":         map[string]any{"type": "string"},
			"sizeBytes":   map[string]any{"type": "integer", "minimum": 0},
			"durationMs":  map[string]any{"type": "integer", "minimum": 0},
			"sha256":      map[string]any{"type": "string"},
			"metadata":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		},
	}
}
