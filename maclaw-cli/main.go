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
	cliVersion      = "0.1.0"
)

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
	Incoming   any                                `json:"incoming,omitempty"`
	SessionID  string                             `json:"sessionId"`
	Messages   []coreim.ThirdPartyOutgoingMessage `json:"messages"`
	NextCursor string                             `json:"nextCursor"`
	HasMore    bool                               `json:"hasMore"`
}

type invokeRequest struct {
	Action         string         `json:"action"`
	ClientID       string         `json:"clientId,omitempty"`
	SessionID      string         `json:"sessionId,omitempty"`
	ConversationID string         `json:"conversationId,omitempty"`
	Text           string         `json:"text,omitempty"`
	Message        map[string]any `json:"message,omitempty"`
	Attachments    []any          `json:"attachments,omitempty"`
	EventID        string         `json:"eventId,omitempty"`
	MessageID      string         `json:"messageId,omitempty"`
	TimeoutSec     int            `json:"timeoutSec,omitempty"`
	LockTimeoutSec int            `json:"lockTimeoutSec,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	Count          int            `json:"count,omitempty"`
	Cursor         string         `json:"cursor,omitempty"`
	WaitPolls      int            `json:"waitPolls,omitempty"`
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
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
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("decode invoke request: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "continue"
	}
	action, cmdArgs, err := invokeArgs(req, action)
	if err != nil {
		return err
	}
	if *dryRun {
		return writeJSON(c.stdout, map[string]any{"ok": true, "action": action, "argv": append([]string{action}, cmdArgs...)}, true)
	}
	return c.run(append([]string{action}, cmdArgs...))
}

func invokeArgs(req invokeRequest, action string) (string, []string, error) {
	args := []string{"--json-errors"}
	if req.ClientID != "" {
		args = append(args, "--client", req.ClientID)
	}
	if req.SessionID != "" {
		args = append(args, "--session", req.SessionID)
	}
	if req.ConversationID != "" {
		args = append(args, "--conversation", req.ConversationID)
	}
	if req.TimeoutSec > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", req.TimeoutSec))
	}
	if req.LockTimeoutSec > 0 {
		args = append(args, "--lock-timeout", fmt.Sprintf("%d", req.LockTimeoutSec))
	}
	if req.Limit > 0 {
		args = append(args, "--limit", fmt.Sprintf("%d", req.Limit))
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
	if requireSession {
		args = append(args, "--require-session")
	}
	if req.Pretty != nil {
		args = append(args, "--pretty="+fmt.Sprintf("%t", *req.Pretty))
	}
	switch action {
	case "continue", "run", "ask":
		action = "continue"
		if req.WaitPolls > 0 {
			args = append(args, "--wait-polls", fmt.Sprintf("%d", req.WaitPolls))
		}
		if req.Text != "" {
			args = append(args, "--text", req.Text)
		}
	case "send":
		if req.EventID != "" {
			args = append(args, "--event-id", req.EventID)
		}
		if req.MessageID != "" {
			args = append(args, "--message-id", req.MessageID)
		}
		if len(req.Message) > 0 {
			data, err := json.Marshal(req.Message)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--message-json", string(data))
		}
		if len(req.Attachments) > 0 {
			data, err := json.Marshal(req.Attachments)
			if err != nil {
				return "", nil, err
			}
			args = append(args, "--attachments-json", string(data))
		}
		if req.Text != "" {
			args = append(args, "--text", req.Text)
		}
	case "poll", "doctor", "bootstrap":
	case "watch":
		if req.Count > 0 {
			args = append(args, "--count", fmt.Sprintf("%d", req.Count))
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
			args = append(args, "--status", req.Status)
		}
	case "tool-result":
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
	default:
		return "", nil, fmt.Errorf("unsupported invoke action %q", req.Action)
	}
	if action == "run" || action == "ask" {
		action = "continue"
	}
	return action, args, nil
}

func (c *cli) runDoctor(args []string) error {
	cfgp, fs := newFlagSet("doctor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
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
	baseURL := fmt.Sprintf("http://%s/api/im-gateway/v1", netJoinHostPortForURL(appCfg.ThirdPartyGatewayHost, appCfg.ThirdPartyGatewayPort))
	return writeJSON(c.stdout, map[string]any{
		"ok":           true,
		"configPath":   cfg.ConfigPath,
		"baseUrl":      baseURL,
		"enabled":      true,
		"tokenPresent": true,
		"note":         "Config written. If MaClaw GUI is already running and gateway is not connected, restart gateway in GUI or restart MaClaw GUI.",
	}, cfg.Pretty)
}

func (c *cli) runHandshake(args []string) error {
	cfgp, fs := newFlagSet("handshake")
	toolsPath := fs.String("tools", "", "JSON file containing []tool definitions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := requireToken(cfg); err != nil {
		return err
	}
	tools, err := readTools(*toolsPath)
	if err != nil {
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
	eventID := fs.String("event-id", "", "stable event id for idempotency")
	messageID := fs.String("message-id", "", "source message id")
	stdin := fs.Bool("stdin", false, "read text from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	var err error
	cfg, err = c.applySession(cfg, true)
	if err != nil {
		return err
	}
	runLock, err := acquireRunLock(cfg)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
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
	now := c.now().UnixMilli()
	if strings.TrimSpace(*messageID) == "" {
		*messageID = fmt.Sprintf("maclaw_cli_%d", now)
	}
	if strings.TrimSpace(*eventID) == "" {
		*eventID = "evt_" + strings.TrimSpace(*messageID)
	}
	payload, err := buildMessagePayload(msgText, *messageJSON, *attachmentsJSON)
	if err != nil {
		return err
	}
	req := coreim.ThirdPartyIncomingRequest{
		ClientID:       cfg.ClientID,
		EventID:        *eventID,
		MessageID:      *messageID,
		ConversationID: cfg.ConversationID,
		User:           coreim.ThirdPartyUserRef{ID: cfg.UserID, Name: cfg.UserName},
		Message:        payload,
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	var err error
	cfg, err = c.applySession(cfg, true)
	if err != nil {
		return err
	}
	runLock, err := acquireRunLock(cfg)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	var err error
	cfg, err = c.applySession(cfg, true)
	if err != nil {
		return err
	}
	runLock, err := acquireRunLock(cfg)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	if err := requireToken(cfg); err != nil {
		return err
	}
	ids := splitCSV(*idsCSV)
	if len(ids) == 0 {
		return errors.New("--ids is required")
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
	idempotencyKey := fs.String("idempotency-key", "", "stable idempotency key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	var err error
	cfg, err = c.applySession(cfg, true)
	if err != nil {
		return err
	}
	runLock, err := acquireRunLock(cfg)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
		return err
	}
	result, err := readJSONObject(*resultJSON)
	if err != nil {
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
		CreatedAt:      c.now().UnixMilli(),
	}
	if strings.TrimSpace(*errorCode) != "" || strings.TrimSpace(*errorMessage) != "" {
		req.Error = &coreim.ThirdPartyToolError{Code: *errorCode, Message: *errorMessage}
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
	stdin := fs.Bool("stdin", false, "read text from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	var err error
	cfg, err = c.applySession(cfg, true)
	if err != nil {
		return err
	}
	runLock, err := acquireRunLock(cfg)
	if err != nil {
		return err
	}
	defer runLock.Release()
	if err := requireToken(cfg); err != nil {
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
	now := c.now().UnixMilli()
	inReq := coreim.ThirdPartyIncomingRequest{
		ClientID:       cfg.ClientID,
		EventID:        fmt.Sprintf("evt_maclaw_cli_%d", now),
		MessageID:      fmt.Sprintf("maclaw_cli_%d", now),
		ConversationID: cfg.ConversationID,
		User:           coreim.ThirdPartyUserRef{ID: cfg.UserID, Name: cfg.UserName},
		Message:        coreim.ThirdPartyMessagePayload{Type: "text", Text: msgText},
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
	return writeJSON(c.stdout, askResult{Incoming: incoming, SessionID: cfg.ConversationID, Messages: all, NextCursor: next, HasMore: hasMore}, cfg.Pretty)
}

func (c *cli) runSession(args []string) error {
	if len(args) == 0 {
		args = []string{"current"}
	}
	sub, args := args[0], args[1:]
	cfgp, fs := newFlagSet("session")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg := finalizeConfig(*cfgp)
	switch sub {
	case "new":
		sessionID := firstNonEmpty(*id, c.newSessionID())
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, true, func(st *cliState) error {
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
			upsertSession(st, sessionState{ID: sessionID, ClientID: cfg.ClientID, Cursor: "0", CreatedAt: now, UpdatedAt: now})
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
			if st.CurrentSession == from {
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
			st.Sessions = removeSessionForClient(st.Sessions, sessionID, cfg.ClientID)
			if st.CurrentSession == sessionID {
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
		cfg, err := c.applySession(cfg, true)
		if err != nil {
			return err
		}
		var sess *sessionState
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, false, func(st *cliState) error {
			if found := findSessionForClient(*st, cfg.ConversationID, cfg.ClientID); found != nil {
				copied := *found
				sess = &copied
			}
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "currentSession": cfg.ConversationID, "session": sess, "statePath": cfg.StatePath}, cfg.Pretty)
	case "list":
		var st cliState
		if err := withCLIState(cfg.StatePath, cfg.LockTimeoutSec, false, func(loaded *cliState) error {
			st = *loaded
			return nil
		}); err != nil {
			return err
		}
		return writeJSON(c.stdout, map[string]any{"ok": true, "currentSession": st.CurrentSession, "sessions": st.Sessions, "statePath": cfg.StatePath}, cfg.Pretty)
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
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response HTTP %d: %w: %s", resp.StatusCode, err, string(data))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env gatewayErrorEnvelope
		_ = json.Unmarshal(data, &env)
		if env.Error != nil {
			return fmt.Errorf("HTTP %d [%s] %s", resp.StatusCode, env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
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
	cfg.Discovered = cfg.Discovered || discovered.OK
	return cfg
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
	explicitSession := false
	if strings.TrimSpace(cfg.SessionID) != "" {
		cfg.ConversationID = strings.TrimSpace(cfg.SessionID)
		explicitSession = true
	}
	if strings.TrimSpace(cfg.ConversationID) != "" {
		cfg.SessionID = cfg.ConversationID
		explicitSession = true
	}
	if cfg.RequireSession && !explicitSession {
		return cfg, errors.New("missing explicit session; pass --session <id> for concurrent/agent use")
	}
	st, err := loadCLIState(cfg.StatePath)
	if err != nil {
		return cfg, err
	}
	if explicitSession {
		now := c.now().UnixMilli()
		sess := findSessionForClient(st, cfg.ConversationID, cfg.ClientID)
		if sess == nil {
			if legacy := findSessionLegacy(st, cfg.ConversationID); legacy != nil {
				upsertSession(&st, sessionState{ID: cfg.ConversationID, ClientID: cfg.ClientID, Cursor: legacy.Cursor, CreatedAt: legacy.CreatedAt, UpdatedAt: now})
				sess = findSessionForClient(st, cfg.ConversationID, cfg.ClientID)
			}
		}
		if sess == nil && create {
			upsertSession(&st, sessionState{ID: cfg.ConversationID, ClientID: cfg.ClientID, Cursor: "0", CreatedAt: now, UpdatedAt: now})
			sess = findSessionForClient(st, cfg.ConversationID, cfg.ClientID)
		}
		if sess != nil && cfg.Cursor == "" {
			cfg.Cursor = firstNonEmpty(sess.Cursor, "0")
		}
		if cfg.Cursor == "" {
			cfg.Cursor = "0"
		}
		st.CurrentSession = cfg.ConversationID
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
	if strings.TrimSpace(cfg.ConversationID) == "" || strings.TrimSpace(cursor) == "" {
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
	existing := findSessionForClient(st, cfg.ConversationID, cfg.ClientID)
	created := now
	if existing != nil && existing.CreatedAt != 0 {
		created = existing.CreatedAt
	}
	upsertSession(&st, sessionState{ID: cfg.ConversationID, ClientID: cfg.ClientID, Cursor: cursor, CreatedAt: created, UpdatedAt: now})
	st.CurrentSession = cfg.ConversationID
	return saveCLIState(cfg.StatePath, st)
}

func (c *cli) newSessionID() string {
	return "sess_" + c.now().UTC().Format("20060102_150405")
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
	host := strings.TrimSpace(cfg.ThirdPartyGatewayHost)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
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
	if strings.TrimSpace(cfg.ConversationID) == "" {
		return nil, nil
	}
	statePath := cfg.StatePath
	if strings.TrimSpace(statePath) == "" {
		statePath = defaultStatePath()
	}
	lockPath := sessionRunLockPath(statePath, cfg.ClientID, cfg.ConversationID)
	lock, err := acquireLockFile(lockPath, "run lock", cfg.LockTimeoutSec)
	if err != nil {
		return nil, fmt.Errorf("%w for clientId=%q sessionId=%q", err, cfg.ClientID, cfg.ConversationID)
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
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			lock := &stateLock{path: lockPath, file: f, heartbeat: make(chan struct{})}
			lock.startHeartbeat(staleAfter)
			return lock, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire %s: %w", label, err)
		}
		if isStaleLock(lockPath, staleAfter) {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s busy: %s", label, lockPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
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
		_ = os.Remove(l.path)
	}
}

func (l *stateLock) startHeartbeat(staleAfter time.Duration) {
	if l == nil || l.path == "" || l.heartbeat == nil {
		return
	}
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
				now := time.Now()
				_ = os.Chtimes(l.path, now, now)
			case <-l.heartbeat:
				return
			}
		}
	}()
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

func removeSession(sessions []sessionState, id string) []sessionState {
	out := sessions[:0]
	for _, sess := range sessions {
		if sess.ID != id {
			out = append(out, sess)
		}
	}
	return out
}

func removeSessionForClient(sessions []sessionState, id, clientID string) []sessionState {
	out := sessions[:0]
	for _, sess := range sessions {
		if sess.ID == id && (sess.ClientID == clientID || sess.ClientID == "") {
			continue
		}
		out = append(out, sess)
	}
	return out
}

func netJoinHostPortForURL(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func buildMessagePayload(text, messageJSON, attachmentsJSON string) (coreim.ThirdPartyMessagePayload, error) {
	if strings.TrimSpace(messageJSON) != "" {
		var payload coreim.ThirdPartyMessagePayload
		if err := json.Unmarshal([]byte(messageJSON), &payload); err != nil {
			return payload, fmt.Errorf("decode --message-json: %w", err)
		}
		return payload, nil
	}
	var attachments []coreim.ThirdPartyMediaReference
	if strings.TrimSpace(attachmentsJSON) != "" {
		if err := json.Unmarshal([]byte(attachmentsJSON), &attachments); err != nil {
			return coreim.ThirdPartyMessagePayload{}, fmt.Errorf("decode --attachments-json: %w", err)
		}
	}
	msgType := "text"
	if len(attachments) > 0 {
		msgType = firstNonEmpty(attachments[0].Type, "file")
	}
	return coreim.ThirdPartyMessagePayload{Type: msgType, Text: text, Attachments: attachments}, nil
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
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	return tools, nil
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
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "--") {
			if strings.Contains(arg, "=") {
				continue
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				skipNext = true
			}
			continue
		}
		out = append(out, arg)
	}
	return out
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
  maclaw-cli tool-result --tool-call-id tc_1 --status success --result-json '{"ok":true}' [flags]

Common flags:
  --base URL             default http://127.0.0.1:18777/api/im-gateway/v1 or MACLAW_GATEWAY_URL
  --token TOKEN          default MACLAW_GATEWAY_TOKEN, otherwise auto-read from ~/.maclaw/config.json
  --config PATH          default ~/.maclaw/config.json or MACLAW_CONFIG
  --state PATH           default ~/.maclaw/maclaw-cli/state.json or MACLAW_CLI_STATE
  --client ID            default maclaw-cli or MACLAW_CLIENT_ID
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
  sessionId   Protocol conversation/session id used.
  messages    Assistant replies and tool calls.
  nextCursor  Cursor saved for next call.
  hasMore     More replies may be available.

Concurrency:
  Different clientId + sessionId pairs are independent.
  Same clientId + sessionId shares cursor and is serialized by a run lock.
  Stateful commands covered by the run lock:
    continue, ask, send, poll, watch, tool-result.
  A second process for the same key waits, then exits with "run lock busy"
  if the first process does not finish before timeout.

Tool calls:
  Register tools:
    maclaw-cli handshake --tools tools.json --require-session --client <agent-id> --session <task-id>
  Submit result:
    maclaw-cli tool-result --tool-call-id <id> --status success --result-json '{"ok":true}' --require-session --client <agent-id> --session <task-id>

Environment alternatives:
  MACLAW_CLIENT_ID=<agent-id>
  MACLAW_SESSION_ID=<task-id>
  MACLAW_REQUIRE_SESSION=1

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
			"key":            "clientId + sessionId",
			"cursor":         "Saved per clientId + sessionId after ask/continue/poll/watch.",
			"doNotUse":       "session use in automation",
			"concurrency":    "Different clientId + sessionId pairs are independent. Stateful calls with the same key are serialized by a run lock; a second process waits and then fails with 'run lock busy' after timeout.",
			"lockTimeoutSec": defaultLockWait,
			"lockedCommands": []string{"continue", "ask", "send", "poll", "watch", "tool-result"},
		},
		"setup": []map[string]any{
			{"command": "maclaw-cli bootstrap", "when": "First local setup or missing token/config."},
			{"command": "maclaw-cli doctor", "when": "Check GUI gateway health and auth."},
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
				"template": "maclaw-cli watch --require-session --client <agent-id> --session <task-id>",
				"stdout":   "JSONL; one outgoing message per line",
				"stateful": true,
			},
			"handshake": map[string]any{
				"use":      "Authenticate and optionally register client-side tool definitions.",
				"template": "maclaw-cli handshake --tools tools.json --require-session --client <agent-id> --session <task-id>",
				"stdout":   "Gateway handshake JSON",
			},
			"tool-result": map[string]any{
				"use":      "Submit result for a tool_call/tool_plan step.",
				"template": "maclaw-cli tool-result --tool-call-id <id> --status success --result-json '{\"ok\":true}' --require-session --client <agent-id> --session <task-id>",
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
		},
		"requiredFlagsForAutomation": []string{"--require-session", "--client", "--session"},
		"importantFlags": map[string]string{
			"--client":          "Stable calling agent id. Part of state key.",
			"--session":         "Stable task/conversation id. Part of state key and default protocol conversationId.",
			"--conversation":    "Protocol conversation id override. Use only when exact external conversation id is required.",
			"--require-session": "Fail fast if explicit session is missing.",
			"--lock-timeout":    "State/run lock wait timeout seconds. Default 5.",
			"--json-errors":     "Emit machine-readable JSON error envelope to stderr on failure.",
			"--state":           "Override state file path.",
			"--config":          "Override MaClaw GUI config path.",
			"--cursor":          "Override poll cursor for one call.",
			"--ack":             "Ack received messages. Default true.",
		},
		"outputFields": map[string]any{
			"askResult":            []string{"incoming", "sessionId", "messages", "nextCursor", "hasMore"},
			"outgoingMessageTypes": []string{"text", "image", "file", "voice", "audio", "tool_call", "tool_plan", "tool_cancel"},
			"toolCallPath":         "messages[].toolCall",
			"toolPlanPath":         "messages[].toolPlan",
		},
		"environment": map[string]string{
			"MACLAW_CLIENT_ID":            "Default --client.",
			"MACLAW_SESSION_ID":           "Default --session.",
			"MACLAW_REQUIRE_SESSION":      "Set 1/true to require explicit sessions.",
			"MACLAW_JSON_ERRORS":          "Set 1/true to emit machine-readable error JSON to stderr.",
			"MACLAW_CLI_LOCK_TIMEOUT_SEC": "Default --lock-timeout.",
			"MACLAW_CLI_STATE":            "State file path.",
			"MACLAW_CONFIG":               "MaClaw GUI config path.",
			"MACLAW_GATEWAY_URL":          "Gateway base URL override.",
			"MACLAW_GATEWAY_TOKEN":        "Bearer token override.",
		},
		"antiPatterns": []string{
			"Do not use 'session use' in automation.",
			"Do not intentionally run two long polls for the same clientId + sessionId; the second command will wait on the run lock.",
			"Do not omit --client in multi-agent systems.",
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
			"clientId":       map[string]any{"type": "string", "description": "Stable calling agent id; part of state key."},
			"sessionId":      map[string]any{"type": "string", "description": "Stable task/session id; part of state key."},
			"conversationId": map[string]any{"type": "string", "description": "Protocol conversation id override."},
			"text":           map[string]any{"type": "string", "description": "Prompt text or tool-result text."},
			"message":        map[string]any{"type": "object", "description": "Full ThirdPartyMessagePayload for send.", "additionalProperties": true},
			"attachments":    map[string]any{"type": "array", "description": "ThirdPartyMediaReference array for send.", "items": map[string]any{"type": "object", "additionalProperties": true}},
			"eventId":        map[string]any{"type": "string", "description": "Stable incoming event id for send idempotency."},
			"messageId":      map[string]any{"type": "string", "description": "Stable source message id for send idempotency."},
			"timeoutSec":     map[string]any{"type": "integer", "minimum": 0},
			"lockTimeoutSec": map[string]any{"type": "integer", "minimum": 1, "description": "State/run lock wait timeout seconds."},
			"limit":          map[string]any{"type": "integer", "minimum": 1},
			"count":          map[string]any{"type": "integer", "minimum": 1, "description": "watch poll count; omit for endless watch."},
			"cursor":         map[string]any{"type": "string"},
			"waitPolls":      map[string]any{"type": "integer", "minimum": 1},
			"ack":            map[string]any{"type": "boolean"},
			"requireSession": map[string]any{"type": "boolean", "default": true},
			"pretty":         map[string]any{"type": "boolean"},
			"toolsPath":      map[string]any{"type": "string"},
			"messageIds":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"status":         map[string]any{"type": "string", "enum": []string{"success", "error", "rejected", "cancelled", "canceled", "timeout", "delivered"}},
			"toolCallId":     map[string]any{"type": "string"},
			"toolPlanId":     map[string]any{"type": "string"},
			"stepId":         map[string]any{"type": "string"},
			"resultId":       map[string]any{"type": "string"},
			"idempotencyKey": map[string]any{"type": "string", "description": "Stable idempotency key for tool-result."},
			"result":         map[string]any{"type": "object", "additionalProperties": true},
			"errorCode":      map[string]any{"type": "string"},
			"errorMessage":   map[string]any{"type": "string"},
		},
		"allOf": []map[string]any{
			{
				"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"const": "ack"}}, "required": []string{"action"}},
				"then": map[string]any{"required": []string{"messageIds"}},
			},
			{
				"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"const": "tool-result"}}, "required": []string{"action"}},
				"then": map[string]any{"required": []string{"status"}},
			},
		},
		"examples": []map[string]any{
			{"action": "continue", "clientId": "planner", "sessionId": "task-123", "text": "Continue the task", "requireSession": true},
			{"action": "poll", "clientId": "planner", "sessionId": "task-123"},
			{"action": "tool-result", "clientId": "desktop-agent", "sessionId": "task-123", "toolCallId": "tc_001", "status": "success", "result": map[string]any{"ok": true}},
		},
	}
}
