package im

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	ThirdPartyProtocolVersion       = "1.1"
	ThirdPartyProtocolLegacyVersion = "1"
	ThirdPartyMaxTextChars          = 20000
	ThirdPartyMaxURLChars           = 4096
	ThirdPartyMaxMediaCaption       = 2000
	ThirdPartyMaxAttachments        = 10
	ThirdPartyMaxIDChars            = 128
	ThirdPartyMaxDirectBytes        = 256 * 1024
	ThirdPartyMaxTools              = 64
	ThirdPartyMaxToolSteps          = 32
	ThirdPartyMaxToolJSON           = 64 * 1024
	ThirdPartyMaxBodyBytes          = 16 * 1024 * 1024
	ThirdPartyMaxMediaBytes         = 50 * 1024 * 1024
	ThirdPartyMaxAckIDs             = 100
	ThirdPartyPollTimeoutSec        = 30
	ThirdPartyMaxTimeoutSec         = 60
	ThirdPartyMaxBatchSize          = 20
	ThirdPartyMaxPollLimit          = 100
	ThirdPartyGatewayMode           = "maclaw"
)

type ThirdPartyHandshakeRequest struct {
	ClientID           string                     `json:"clientId"`
	ClientName         string                     `json:"clientName,omitempty"`
	ProtocolVersion    string                     `json:"protocolVersion,omitempty"`
	Capabilities       map[string]any             `json:"capabilities,omitempty"` // legacy transport feature map
	ClientCapabilities *agent.ClientCapabilities  `json:"clientCapabilities,omitempty"`
	Tools              []ThirdPartyToolDefinition `json:"tools,omitempty"`
}

type ThirdPartyIncomingRequest struct {
	ClientID       string                   `json:"clientId"`
	EventID        string                   `json:"eventId"`
	MessageID      string                   `json:"messageId,omitempty"`
	ConversationID string                   `json:"conversationId"`
	User           ThirdPartyUserRef        `json:"user"`
	Message        ThirdPartyMessagePayload `json:"message"`
	Metadata       map[string]string        `json:"metadata,omitempty"`
	CreatedAt      int64                    `json:"createdAt,omitempty"`
	Extra          map[string]any           `json:"extra,omitempty"`
}

type ThirdPartyUserRef struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type ThirdPartyMessagePayload struct {
	ID          string                     `json:"id,omitempty"`
	Type        string                     `json:"type,omitempty"`
	Text        string                     `json:"text,omitempty"`
	FileName    string                     `json:"fileName,omitempty"`
	ContentType string                     `json:"contentType,omitempty"`
	MimeType    string                     `json:"mimeType,omitempty"`
	Data        string                     `json:"data,omitempty"`
	URL         string                     `json:"url,omitempty"`
	SizeBytes   int64                      `json:"sizeBytes,omitempty"`
	DurationMs  int64                      `json:"durationMs,omitempty"`
	Attachments []ThirdPartyMediaReference `json:"attachments,omitempty"`
}

type ThirdPartyMediaReference struct {
	ID          string            `json:"id,omitempty"`
	Type        string            `json:"type,omitempty"`
	FileName    string            `json:"fileName,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	MimeType    string            `json:"mimeType,omitempty"`
	Data        string            `json:"data,omitempty"`
	URL         string            `json:"url,omitempty"`
	SizeBytes   int64             `json:"sizeBytes,omitempty"`
	DurationMs  int64             `json:"durationMs,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type ThirdPartyMediaPrepareRequest struct {
	ClientID   string `json:"clientId"`
	Type       string `json:"type,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	MimeType   string `json:"mimeType,omitempty"`
	SizeBytes  int64  `json:"sizeBytes,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

type ThirdPartyMediaPrepareResponse struct {
	OK        bool                     `json:"ok"`
	RequestID string                   `json:"requestId,omitempty"`
	Media     ThirdPartyMediaReference `json:"media"`
	Upload    ThirdPartyMediaUpload    `json:"upload"`
	Download  ThirdPartyMediaDownload  `json:"download"`
	ExpiresAt int64                    `json:"expiresAt,omitempty"`
	Error     map[string]string        `json:"error,omitempty"`
}

type ThirdPartyMediaUpload struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	MaxBytes    int64             `json:"maxBytes,omitempty"`
}

type ThirdPartyMediaDownload struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type ThirdPartyAckRequest struct {
	ClientID   string   `json:"clientId"`
	MessageIDs []string `json:"messageIds"`
	Status     string   `json:"status,omitempty"`
}

type ThirdPartyPollRequest struct {
	ClientID   string
	Cursor     int64
	Limit      int
	TimeoutSec int
	TimeoutSet bool
}

type ThirdPartyToolDefinition struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	InputSchema      map[string]any    `json:"inputSchema,omitempty"`
	OutputSchema     map[string]any    `json:"outputSchema,omitempty"`
	Risk             string            `json:"risk,omitempty"`
	RequiresApproval bool              `json:"requiresApproval,omitempty"`
	TimeoutMs        int64             `json:"timeoutMs,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type ThirdPartyToolCall struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Arguments        map[string]any    `json:"arguments,omitempty"`
	Risk             string            `json:"risk,omitempty"`
	RequiresApproval bool              `json:"requiresApproval,omitempty"`
	IdempotencyKey   string            `json:"idempotencyKey,omitempty"`
	TimeoutMs        int64             `json:"timeoutMs,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type ThirdPartyToolPlan struct {
	ID               string                   `json:"id"`
	Mode             string                   `json:"mode,omitempty"`
	Steps            []ThirdPartyToolPlanStep `json:"steps"`
	RequiresApproval bool                     `json:"requiresApproval,omitempty"`
	Metadata         map[string]string        `json:"metadata,omitempty"`
}

type ThirdPartyToolPlanStep struct {
	ID               string            `json:"id"`
	Tool             string            `json:"tool"`
	Arguments        map[string]any    `json:"arguments,omitempty"`
	DependsOn        []string          `json:"dependsOn,omitempty"`
	Risk             string            `json:"risk,omitempty"`
	RequiresApproval bool              `json:"requiresApproval,omitempty"`
	IdempotencyKey   string            `json:"idempotencyKey,omitempty"`
	TimeoutMs        int64             `json:"timeoutMs,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type ThirdPartyToolCancel struct {
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolPlanID string `json:"toolPlanId,omitempty"`
	StepID     string `json:"stepId,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type ThirdPartyToolResultRequest struct {
	ClientID       string               `json:"clientId"`
	ResultID       string               `json:"resultId,omitempty"`
	ConversationID string               `json:"conversationId,omitempty"`
	ToolCallID     string               `json:"toolCallId,omitempty"`
	ToolPlanID     string               `json:"toolPlanId,omitempty"`
	StepID         string               `json:"stepId,omitempty"`
	Status         string               `json:"status"`
	IdempotencyKey string               `json:"idempotencyKey,omitempty"`
	Result         map[string]any       `json:"result,omitempty"`
	Text           string               `json:"text,omitempty"`
	Error          *ThirdPartyToolError `json:"error,omitempty"`
	Metadata       map[string]string    `json:"metadata,omitempty"`
	CreatedAt      int64                `json:"createdAt,omitempty"`
}

type ThirdPartyToolError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

type ThirdPartyOutgoingMessage struct {
	ID               string                     `json:"id"`
	Seq              int64                      `json:"seq,omitempty"`
	Cursor           string                     `json:"cursor,omitempty"`
	ReplyTo          string                     `json:"replyTo,omitempty"`
	ClientID         string                     `json:"clientId,omitempty"`
	ConversationID   string                     `json:"conversationId,omitempty"`
	ReplyToMessageID string                     `json:"replyToMessageId,omitempty"`
	Type             string                     `json:"type"`
	Text             string                     `json:"text,omitempty"`
	Caption          string                     `json:"caption,omitempty"`
	FileName         string                     `json:"fileName,omitempty"`
	ContentType      string                     `json:"contentType,omitempty"`
	MimeType         string                     `json:"mimeType,omitempty"`
	Data             string                     `json:"data,omitempty"`
	URL              string                     `json:"url,omitempty"`
	SizeBytes        int64                      `json:"sizeBytes,omitempty"`
	DurationMs       int64                      `json:"durationMs,omitempty"`
	Attachments      []ThirdPartyMediaReference `json:"attachments,omitempty"`
	ToolCall         *ThirdPartyToolCall        `json:"toolCall,omitempty"`
	ToolPlan         *ThirdPartyToolPlan        `json:"toolPlan,omitempty"`
	ToolCancel       *ThirdPartyToolCancel      `json:"toolCancel,omitempty"`
	Progress         bool                       `json:"progress,omitempty"`
	Error            string                     `json:"error,omitempty"`
	CreatedAt        int64                      `json:"createdAt"`
	// Glyphs carries the compact 24x24 bitmaps required by constrained ESP
	// displays to render non-ASCII reply text. It deliberately lives beside
	// Text so the device can cache it before drawing the message.
	Glyphs   map[string]string `json:"glyphs,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Extra    map[string]any    `json:"extra,omitempty"`
}

type ThirdPartyNormalizeOptions struct {
	RequireMessageID      bool
	RequireUserID         bool
	DefaultConversationID string
	MaxTextChars          int
}

type ThirdPartyGatewayConfig struct {
	Mode               string
	ChannelID          string
	RequestID          string
	ServerTime         int64
	MaxBodyBytes       int
	MaxMediaBytes      int
	PollTimeoutSec     int
	MaxTimeoutSec      int
	MaxBatchSize       int
	MaxPollLimit       int
	ClientCapabilities *agent.ClientCapabilities
}

type ThirdPartyGatewayHandshakeResponse struct {
	OK                   bool                      `json:"ok"`
	RequestID            string                    `json:"requestId,omitempty"`
	ChannelID            string                    `json:"channelId,omitempty"`
	ProtocolVersion      string                    `json:"protocolVersion"`
	ServerTime           int64                     `json:"serverTime,omitempty"`
	Mode                 string                    `json:"mode,omitempty"`
	Capabilities         []string                  `json:"capabilities"`
	Poll                 map[string]int            `json:"poll"`
	Limits               map[string]int            `json:"limits"`
	Delivery             map[string]string         `json:"delivery"`
	PollTimeoutSec       int                       `json:"pollTimeoutSec"`
	MaxBatchSize         int                       `json:"maxBatchSize"`
	Features             map[string]bool           `json:"features"`
	CapabilitiesAccepted *agent.ClientCapabilities `json:"capabilitiesAccepted,omitempty"`
}

type ThirdPartyGatewayHealthResponse struct {
	OK              bool   `json:"ok"`
	RequestID       string `json:"requestId,omitempty"`
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocolVersion"`
	ServerTime      int64  `json:"serverTime,omitempty"`
}

type ThirdPartyGatewayOKResponse struct {
	OK        bool   `json:"ok"`
	RequestID string `json:"requestId,omitempty"`
}

type ThirdPartyMediaUploadCompleteResponse struct {
	OK        bool   `json:"ok"`
	RequestID string `json:"requestId,omitempty"`
	MediaID   string `json:"mediaId"`
}

type ThirdPartyIncomingAcceptedResponse struct {
	OK              bool   `json:"ok"`
	RequestID       string `json:"requestId,omitempty"`
	Accepted        bool   `json:"accepted"`
	Duplicate       bool   `json:"duplicate"`
	MaclawMessageID string `json:"maclawMessageId"`
}

type ThirdPartyOutgoingPollResponse struct {
	OK         bool                        `json:"ok"`
	RequestID  string                      `json:"requestId,omitempty"`
	Messages   []ThirdPartyOutgoingMessage `json:"messages"`
	NextCursor string                      `json:"nextCursor"`
	HasMore    bool                        `json:"hasMore"`
}

type ThirdPartyGatewayErrorResponse struct {
	OK        bool              `json:"ok"`
	RequestID string            `json:"requestId,omitempty"`
	Error     map[string]string `json:"error"`
}

func ThirdPartyCapabilities() []string {
	return []string{"text", "image", "file", "voice", "audio", "attachments", "server_media_upload", "server_media_download", "long_poll", "ack", "idempotency", "client_tools", "tool_call", "tool_plan", "tool_result", "tool_cancel"}
}

func ThirdPartyCapabilityMap() map[string]any {
	capabilities := map[string]any{}
	for _, capability := range ThirdPartyCapabilities() {
		capabilities[capability] = true
	}
	capabilities["serverMedia"] = true
	capabilities["longPolling"] = true
	return capabilities
}

func ThirdPartyLimits() map[string]int {
	return map[string]int{
		"maxTextChars":       ThirdPartyMaxTextChars,
		"maxURLChars":        ThirdPartyMaxURLChars,
		"maxMediaCaption":    ThirdPartyMaxMediaCaption,
		"maxAttachments":     ThirdPartyMaxAttachments,
		"maxIdentifierChars": ThirdPartyMaxIDChars,
		"maxDirectBytes":     ThirdPartyMaxDirectBytes,
		"maxTools":           ThirdPartyMaxTools,
		"maxToolSteps":       ThirdPartyMaxToolSteps,
		"maxToolJSONBytes":   ThirdPartyMaxToolJSON,
		"maxBodyBytes":       ThirdPartyMaxBodyBytes,
		"maxMediaBytes":      ThirdPartyMaxMediaBytes,
		"maxAckIds":          ThirdPartyMaxAckIDs,
	}
}

func ThirdPartyGatewayFeatures() map[string]bool {
	features := map[string]bool{}
	for key, value := range ThirdPartyCapabilityMap() {
		if enabled, ok := value.(bool); ok && enabled {
			features[key] = true
		}
	}
	return features
}

func ThirdPartyGatewayDelivery() map[string]string {
	return map[string]string{
		"guarantee": "at_least_once_by_cursor",
		"dedupeKey": "message.id",
		"ack":       "delivery_receipt",
	}
}

func DefaultThirdPartyGatewayConfig() ThirdPartyGatewayConfig {
	return ThirdPartyGatewayConfig{
		Mode:           ThirdPartyGatewayMode,
		MaxBodyBytes:   ThirdPartyMaxBodyBytes,
		MaxMediaBytes:  ThirdPartyMaxMediaBytes,
		PollTimeoutSec: ThirdPartyPollTimeoutSec,
		MaxTimeoutSec:  ThirdPartyMaxTimeoutSec,
		MaxBatchSize:   ThirdPartyMaxBatchSize,
		MaxPollLimit:   ThirdPartyMaxPollLimit,
	}
}

func (cfg ThirdPartyGatewayConfig) WithDefaults() ThirdPartyGatewayConfig {
	defaults := DefaultThirdPartyGatewayConfig()
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = defaults.Mode
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaults.MaxBodyBytes
	}
	if cfg.MaxMediaBytes <= 0 {
		cfg.MaxMediaBytes = defaults.MaxMediaBytes
	}
	if cfg.PollTimeoutSec <= 0 {
		cfg.PollTimeoutSec = defaults.PollTimeoutSec
	}
	if cfg.MaxTimeoutSec <= 0 {
		cfg.MaxTimeoutSec = defaults.MaxTimeoutSec
	}
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = defaults.MaxBatchSize
	}
	if cfg.MaxPollLimit <= 0 {
		cfg.MaxPollLimit = defaults.MaxPollLimit
	}
	return cfg
}

func ThirdPartyGatewayLimits(maxBodyBytes, maxMediaBytes int) map[string]int {
	cfg := ThirdPartyGatewayConfig{MaxBodyBytes: maxBodyBytes, MaxMediaBytes: maxMediaBytes}.WithDefaults()
	limits := ThirdPartyLimits()
	limits["maxBodyBytes"] = cfg.MaxBodyBytes
	limits["maxMediaBytes"] = cfg.MaxMediaBytes
	return limits
}

func ThirdPartyGatewayPollSettings(pollTimeoutSec, maxTimeoutSec, maxBatchSize, maxPollLimit int) map[string]int {
	cfg := ThirdPartyGatewayConfig{
		PollTimeoutSec: pollTimeoutSec,
		MaxTimeoutSec:  maxTimeoutSec,
		MaxBatchSize:   maxBatchSize,
		MaxPollLimit:   maxPollLimit,
	}.WithDefaults()
	return map[string]int{
		"recommendedTimeoutSec": cfg.PollTimeoutSec,
		"maxTimeoutSec":         cfg.MaxTimeoutSec,
		"defaultLimit":          cfg.MaxBatchSize,
		"maxLimit":              cfg.MaxPollLimit,
	}
}

func ParseThirdPartyPollQuery(values url.Values, cfg ThirdPartyGatewayConfig) (ThirdPartyPollRequest, error) {
	req := ThirdPartyPollRequest{ClientID: values.Get("clientId")}
	if raw := strings.TrimSpace(values.Get("cursor")); raw != "" {
		cursor, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor < 0 {
			return req, errors.New("cursor must be a non-negative integer")
		}
		req.Cursor = cursor
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			return req, errors.New("limit must be a non-negative integer")
		}
		req.Limit = limit
	}
	if raw := strings.TrimSpace(values.Get("timeout")); raw != "" {
		timeoutSec, err := strconv.Atoi(raw)
		if err != nil || timeoutSec < 0 {
			return req, errors.New("timeout must be a non-negative integer")
		}
		req.TimeoutSec = timeoutSec
		req.TimeoutSet = true
	}
	return req, NormalizeThirdPartyPollRequest(&req, cfg)
}

func NormalizeThirdPartyPollRequest(req *ThirdPartyPollRequest, cfg ThirdPartyGatewayConfig) error {
	cfg = cfg.WithDefaults()
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if req.Cursor < 0 {
		return errors.New("cursor must be a non-negative integer")
	}
	if req.Limit < 0 {
		return errors.New("limit must be a non-negative integer")
	}
	if req.Limit == 0 {
		req.Limit = cfg.MaxBatchSize
	}
	if req.Limit > cfg.MaxPollLimit {
		req.Limit = cfg.MaxPollLimit
	}
	if req.TimeoutSec < 0 {
		return errors.New("timeout must be a non-negative integer")
	}
	if !req.TimeoutSet && req.TimeoutSec == 0 {
		req.TimeoutSec = cfg.PollTimeoutSec
	}
	if req.TimeoutSec > cfg.MaxTimeoutSec {
		req.TimeoutSec = cfg.MaxTimeoutSec
	}
	return nil
}

func NewThirdPartyGatewayHandshakeResponse(cfg ThirdPartyGatewayConfig) ThirdPartyGatewayHandshakeResponse {
	cfg = cfg.WithDefaults()
	return ThirdPartyGatewayHandshakeResponse{
		OK:                   true,
		RequestID:            strings.TrimSpace(cfg.RequestID),
		ChannelID:            strings.TrimSpace(cfg.ChannelID),
		ProtocolVersion:      ThirdPartyProtocolVersion,
		ServerTime:           cfg.ServerTime,
		Mode:                 strings.TrimSpace(cfg.Mode),
		Capabilities:         ThirdPartyCapabilities(),
		Poll:                 ThirdPartyGatewayPollSettings(cfg.PollTimeoutSec, cfg.MaxTimeoutSec, cfg.MaxBatchSize, cfg.MaxPollLimit),
		Limits:               ThirdPartyGatewayLimits(cfg.MaxBodyBytes, cfg.MaxMediaBytes),
		Delivery:             ThirdPartyGatewayDelivery(),
		PollTimeoutSec:       cfg.PollTimeoutSec,
		MaxBatchSize:         cfg.MaxBatchSize,
		Features:             ThirdPartyGatewayFeatures(),
		CapabilitiesAccepted: normalizeThirdPartyClientCapabilities(cfg.ClientCapabilities),
	}
}

func NewThirdPartyGatewayHealthResponse(requestID string, serverTime int64) ThirdPartyGatewayHealthResponse {
	return ThirdPartyGatewayHealthResponse{
		OK:              true,
		RequestID:       strings.TrimSpace(requestID),
		Status:          "connected",
		ProtocolVersion: ThirdPartyProtocolVersion,
		ServerTime:      serverTime,
	}
}

func NewThirdPartyGatewayOKResponse(requestID string) ThirdPartyGatewayOKResponse {
	return ThirdPartyGatewayOKResponse{OK: true, RequestID: strings.TrimSpace(requestID)}
}

func NewThirdPartyMediaUploadCompleteResponse(requestID, mediaID string) ThirdPartyMediaUploadCompleteResponse {
	return ThirdPartyMediaUploadCompleteResponse{OK: true, RequestID: strings.TrimSpace(requestID), MediaID: strings.TrimSpace(mediaID)}
}

func NewThirdPartyIncomingAcceptedResponse(requestID, maclawMessageID string, duplicate bool) ThirdPartyIncomingAcceptedResponse {
	return ThirdPartyIncomingAcceptedResponse{
		OK:              true,
		RequestID:       strings.TrimSpace(requestID),
		Accepted:        true,
		Duplicate:       duplicate,
		MaclawMessageID: strings.TrimSpace(maclawMessageID),
	}
}

func NewThirdPartyOutgoingPollResponse(requestID string, messages []ThirdPartyOutgoingMessage, nextCursor int64, hasMore bool) ThirdPartyOutgoingPollResponse {
	if messages == nil {
		messages = []ThirdPartyOutgoingMessage{}
	}
	return ThirdPartyOutgoingPollResponse{
		OK:         true,
		RequestID:  strings.TrimSpace(requestID),
		Messages:   messages,
		NextCursor: strconv.FormatInt(nextCursor, 10),
		HasMore:    hasMore,
	}
}

func NewThirdPartyGatewayErrorResponse(requestID, code, message string) ThirdPartyGatewayErrorResponse {
	return ThirdPartyGatewayErrorResponse{
		OK:        false,
		RequestID: strings.TrimSpace(requestID),
		Error: map[string]string{
			"code":    strings.TrimSpace(code),
			"message": strings.TrimSpace(message),
		},
	}
}

func DecodeThirdPartyGatewayJSON(w http.ResponseWriter, r *http.Request, out any, maxBodyBytes int64) error {
	if maxBodyBytes <= 0 {
		maxBodyBytes = ThirdPartyMaxBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain a single JSON value")
		}
		return err
	}
	return nil
}

func ThirdPartyServerMediaRequestFromURL(rawURL string) (string, *http.Request, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", nil, errors.New("must be an absolute URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", nil, errors.New("must use http or https")
	}
	const suffix = "/api/im-gateway/v1/media/"
	if !strings.HasPrefix(u.Path, suffix) {
		return "", nil, errors.New("must be a server media URL returned by /media/upload-url")
	}
	id := strings.Trim(strings.TrimPrefix(u.Path, suffix), "/")
	if id == "" || strings.Contains(id, "/") {
		return "", nil, errors.New("must be a server media download URL")
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", nil, err
	}
	return id, req, nil
}

func ThirdPartyMediaTokenOK(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	provided := strings.TrimSpace(r.URL.Query().Get("mediaToken"))
	if provided == "" {
		provided = strings.TrimSpace(r.Header.Get("X-Media-Token"))
	}
	if expected == "" || len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func SafeThirdPartyFileName(value string) string {
	value = strings.TrimSpace(filepath.Base(value))
	if value == "" || value == "." || value == string(filepath.Separator) {
		return "file"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(value)
}

func ValidateThirdPartyID(field, value string) error {
	return validateThirdPartyID(field, value)
}

func NormalizeThirdPartyHandshakeRequest(req *ThirdPartyHandshakeRequest) error {
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	req.ClientName = strings.TrimSpace(req.ClientName)
	req.ProtocolVersion = strings.TrimSpace(req.ProtocolVersion)
	if req.ProtocolVersion != "" && req.ProtocolVersion != ThirdPartyProtocolVersion && req.ProtocolVersion != ThirdPartyProtocolLegacyVersion {
		return fmt.Errorf("protocolVersion must be %s or %s", ThirdPartyProtocolLegacyVersion, ThirdPartyProtocolVersion)
	}
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if len(req.Tools) > ThirdPartyMaxTools {
		return fmt.Errorf("tools exceeds %d items", ThirdPartyMaxTools)
	}
	for i := range req.Tools {
		if err := NormalizeThirdPartyToolDefinition(&req.Tools[i], i); err != nil {
			return err
		}
	}
	if req.ClientCapabilities != nil {
		normalized := agent.NormalizeClientCapabilities(req.ClientCapabilities)
		req.ClientCapabilities = &normalized
	} else if looksLikeStructuredClientCapabilities(req.Capabilities) {
		raw, _ := json.Marshal(req.Capabilities)
		var capabilities agent.ClientCapabilities
		if json.Unmarshal(raw, &capabilities) == nil {
			normalized := agent.NormalizeClientCapabilities(&capabilities)
			req.ClientCapabilities = &normalized
		}
	}
	return nil
}

func normalizeThirdPartyClientCapabilities(capabilities *agent.ClientCapabilities) *agent.ClientCapabilities {
	if capabilities == nil {
		return nil
	}
	normalized := agent.NormalizeClientCapabilities(capabilities)
	return &normalized
}

func looksLikeStructuredClientCapabilities(capabilities map[string]any) bool {
	if capabilities == nil {
		return false
	}
	_, hasInput := capabilities["input"]
	_, hasOutput := capabilities["output"]
	return hasInput || hasOutput
}

func NormalizeThirdPartyMediaPrepareRequest(req *ThirdPartyMediaPrepareRequest, maxMediaBytes int64) error {
	if maxMediaBytes <= 0 {
		maxMediaBytes = ThirdPartyMaxMediaBytes
	}
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	req.Type = NormalizeThirdPartyMessageType(req.Type)
	if req.Type == "" || req.Type == "text" {
		req.Type = "file"
	}
	req.FileName = strings.TrimSpace(req.FileName)
	req.MimeType = strings.TrimSpace(req.MimeType)
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if req.SizeBytes < 0 {
		return errors.New("sizeBytes must be non-negative")
	}
	if req.SizeBytes > maxMediaBytes {
		return fmt.Errorf("sizeBytes exceeds %d bytes", maxMediaBytes)
	}
	if req.DurationMs < 0 {
		return errors.New("durationMs must be non-negative")
	}
	return nil
}

func NormalizeThirdPartyAckRequest(req *ThirdPartyAckRequest, maxAckIDs int) error {
	if maxAckIDs <= 0 {
		maxAckIDs = ThirdPartyMaxAckIDs
	}
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	req.Status = NormalizeThirdPartyAckStatus(req.Status)
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if len(req.MessageIDs) > maxAckIDs {
		return fmt.Errorf("messageIds exceeds %d items", maxAckIDs)
	}
	ids := req.MessageIDs[:0]
	for _, id := range req.MessageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := validateThirdPartyID("messageIds[]", id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	req.MessageIDs = ids
	return nil
}

func NormalizeThirdPartyToolDefinition(tool *ThirdPartyToolDefinition, index int) error {
	tool.Name = NormalizeThirdPartyToolName(tool.Name)
	tool.Description = strings.TrimSpace(tool.Description)
	tool.Risk = NormalizeThirdPartyToolRisk(tool.Risk)
	if tool.Name == "" {
		return fmt.Errorf("tools[%d].name is required", index)
	}
	if err := validateThirdPartyToolName(fmt.Sprintf("tools[%d].name", index), tool.Name); err != nil {
		return err
	}
	if tool.TimeoutMs < 0 {
		return fmt.Errorf("tools[%d].timeoutMs must be non-negative", index)
	}
	if tool.InputSchema != nil {
		if err := validateThirdPartyJSONSize(fmt.Sprintf("tools[%d].inputSchema", index), tool.InputSchema); err != nil {
			return err
		}
	}
	if tool.OutputSchema != nil {
		if err := validateThirdPartyJSONSize(fmt.Sprintf("tools[%d].outputSchema", index), tool.OutputSchema); err != nil {
			return err
		}
	}
	return nil
}

func NormalizeThirdPartyOutgoingMessage(msg *ThirdPartyOutgoingMessage) error {
	msg.ID = strings.TrimSpace(msg.ID)
	msg.ClientID = NormalizeThirdPartyID(msg.ClientID)
	msg.ConversationID = strings.TrimSpace(msg.ConversationID)
	msg.Type = strings.ToLower(strings.TrimSpace(msg.Type))
	if msg.ID == "" {
		return errors.New("id is required")
	}
	if err := validateThirdPartyID("id", msg.ID); err != nil {
		return err
	}
	switch msg.Type {
	case "tool_call":
		if msg.ToolCall == nil {
			return errors.New("toolCall is required for tool_call messages")
		}
		return NormalizeThirdPartyToolCall(msg.ToolCall)
	case "tool_plan":
		if msg.ToolPlan == nil {
			return errors.New("toolPlan is required for tool_plan messages")
		}
		return NormalizeThirdPartyToolPlan(msg.ToolPlan)
	case "tool_cancel":
		if msg.ToolCancel == nil {
			return errors.New("toolCancel is required for tool_cancel messages")
		}
		return NormalizeThirdPartyToolCancel(msg.ToolCancel)
	default:
		return nil
	}
}

func NormalizeThirdPartyToolCall(call *ThirdPartyToolCall) error {
	call.ID = NormalizeThirdPartyID(call.ID)
	call.Name = NormalizeThirdPartyToolName(call.Name)
	call.Risk = NormalizeThirdPartyToolRisk(call.Risk)
	call.IdempotencyKey = strings.TrimSpace(call.IdempotencyKey)
	if call.ID == "" {
		return errors.New("toolCall.id is required")
	}
	if err := validateThirdPartyID("toolCall.id", call.ID); err != nil {
		return err
	}
	if err := validateThirdPartyToolName("toolCall.name", call.Name); err != nil {
		return err
	}
	if call.TimeoutMs < 0 {
		return errors.New("toolCall.timeoutMs must be non-negative")
	}
	if err := validateThirdPartyIdempotencyKey("toolCall.idempotencyKey", call.IdempotencyKey); err != nil {
		return err
	}
	return validateThirdPartyJSONSize("toolCall.arguments", call.Arguments)
}

func NormalizeThirdPartyToolPlan(plan *ThirdPartyToolPlan) error {
	plan.ID = NormalizeThirdPartyID(plan.ID)
	rawMode := strings.TrimSpace(plan.Mode)
	plan.Mode = NormalizeThirdPartyToolPlanMode(plan.Mode)
	if plan.Mode == "" {
		if rawMode == "" {
			plan.Mode = "sequential"
		} else {
			return errors.New("toolPlan.mode must be sequential, parallel, dag, or interactive")
		}
	}
	if plan.ID == "" {
		return errors.New("toolPlan.id is required")
	}
	if err := validateThirdPartyID("toolPlan.id", plan.ID); err != nil {
		return err
	}
	if len(plan.Steps) == 0 {
		return errors.New("toolPlan.steps is required")
	}
	if len(plan.Steps) > ThirdPartyMaxToolSteps {
		return fmt.Errorf("toolPlan.steps exceeds %d items", ThirdPartyMaxToolSteps)
	}
	seen := map[string]bool{}
	for i := range plan.Steps {
		if err := NormalizeThirdPartyToolPlanStep(&plan.Steps[i], i); err != nil {
			return err
		}
		if seen[plan.Steps[i].ID] {
			return fmt.Errorf("toolPlan.steps[%d].id is duplicated", i)
		}
		seen[plan.Steps[i].ID] = true
	}
	for i, step := range plan.Steps {
		for _, dep := range step.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("toolPlan.steps[%d].dependsOn references unknown step %q", i, dep)
			}
		}
	}
	if err := validateThirdPartyToolPlanAcyclic(plan.Steps); err != nil {
		return err
	}
	return nil
}

func NormalizeThirdPartyToolPlanStep(step *ThirdPartyToolPlanStep, index int) error {
	step.ID = NormalizeThirdPartyID(step.ID)
	step.Tool = NormalizeThirdPartyToolName(step.Tool)
	step.Risk = NormalizeThirdPartyToolRisk(step.Risk)
	step.IdempotencyKey = strings.TrimSpace(step.IdempotencyKey)
	for i := range step.DependsOn {
		step.DependsOn[i] = NormalizeThirdPartyID(step.DependsOn[i])
	}
	if step.ID == "" {
		return fmt.Errorf("toolPlan.steps[%d].id is required", index)
	}
	if err := validateThirdPartyID(fmt.Sprintf("toolPlan.steps[%d].id", index), step.ID); err != nil {
		return err
	}
	if err := validateThirdPartyToolName(fmt.Sprintf("toolPlan.steps[%d].tool", index), step.Tool); err != nil {
		return err
	}
	if step.TimeoutMs < 0 {
		return fmt.Errorf("toolPlan.steps[%d].timeoutMs must be non-negative", index)
	}
	if err := validateThirdPartyIdempotencyKey(fmt.Sprintf("toolPlan.steps[%d].idempotencyKey", index), step.IdempotencyKey); err != nil {
		return err
	}
	return validateThirdPartyJSONSize(fmt.Sprintf("toolPlan.steps[%d].arguments", index), step.Arguments)
}

func NormalizeThirdPartyToolCancel(cancel *ThirdPartyToolCancel) error {
	cancel.ToolCallID = NormalizeThirdPartyID(cancel.ToolCallID)
	cancel.ToolPlanID = NormalizeThirdPartyID(cancel.ToolPlanID)
	cancel.StepID = NormalizeThirdPartyID(cancel.StepID)
	cancel.Reason = strings.TrimSpace(cancel.Reason)
	if cancel.ToolCallID == "" && cancel.ToolPlanID == "" {
		return errors.New("toolCancel.toolCallId or toolPlanId is required")
	}
	return nil
}

func NormalizeThirdPartyToolResultRequest(req *ThirdPartyToolResultRequest) error {
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	req.ResultID = NormalizeThirdPartyID(req.ResultID)
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.ToolCallID = NormalizeThirdPartyID(req.ToolCallID)
	req.ToolPlanID = NormalizeThirdPartyID(req.ToolPlanID)
	req.StepID = NormalizeThirdPartyID(req.StepID)
	req.Status = NormalizeThirdPartyToolStatus(req.Status)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.Text = strings.TrimSpace(req.Text)
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if req.ToolCallID == "" && req.ToolPlanID == "" {
		return errors.New("toolCallId or toolPlanId is required")
	}
	if req.Status == "" {
		return errors.New("status must be success, error, rejected, cancelled, or timeout")
	}
	if req.ToolCallID != "" {
		if err := validateThirdPartyID("toolCallId", req.ToolCallID); err != nil {
			return err
		}
	}
	if req.ResultID != "" {
		if err := validateThirdPartyID("resultId", req.ResultID); err != nil {
			return err
		}
	}
	if req.ToolPlanID != "" {
		if err := validateThirdPartyID("toolPlanId", req.ToolPlanID); err != nil {
			return err
		}
	}
	if req.StepID != "" {
		if err := validateThirdPartyID("stepId", req.StepID); err != nil {
			return err
		}
	}
	if err := validateThirdPartyIdempotencyKey("idempotencyKey", req.IdempotencyKey); err != nil {
		return err
	}
	if len(req.Text) > ThirdPartyMaxTextChars {
		return fmt.Errorf("text exceeds %d characters", ThirdPartyMaxTextChars)
	}
	return validateThirdPartyJSONSize("result", req.Result)
}

func NormalizeThirdPartyToolName(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ':', r == '/':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".:-_/")
}

func NormalizeThirdPartyToolRisk(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read", "low", "":
		return "read"
	case "write", "medium":
		return "write"
	case "dangerous", "high":
		return "dangerous"
	default:
		return "dangerous"
	}
}

func NormalizeThirdPartyToolStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ok", "succeeded", "success":
		return "success"
	case "failed", "error":
		return "error"
	case "reject", "rejected":
		return "rejected"
	case "cancel", "cancelled", "canceled":
		return "cancelled"
	case "timeout", "timed_out":
		return "timeout"
	default:
		return ""
	}
}

func NormalizeThirdPartyAckStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "delivered", "delivery", "ok", "success":
		return "delivered"
	case "read":
		return "read"
	case "failed", "error":
		return "failed"
	default:
		return "delivered"
	}
}

func NormalizeThirdPartyToolPlanMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sequential":
		return "sequential"
	case "parallel", "dag", "interactive":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func NormalizeThirdPartyIncomingRequest(req *ThirdPartyIncomingRequest, opts ThirdPartyNormalizeOptions) error {
	req.ClientID = NormalizeThirdPartyID(req.ClientID)
	req.EventID = strings.TrimSpace(req.EventID)
	req.MessageID = strings.TrimSpace(req.MessageID)
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.User.ID = strings.TrimSpace(req.User.ID)
	req.User.Name = strings.TrimSpace(req.User.Name)
	req.User.DisplayName = strings.TrimSpace(req.User.DisplayName)
	req.Message.Type = NormalizeThirdPartyMessageType(req.Message.Type)
	if req.Message.Type == "" {
		req.Message.Type = "text"
	}
	req.Message.Text = strings.TrimSpace(req.Message.Text)
	req.Message.FileName = strings.TrimSpace(req.Message.FileName)
	req.Message.ContentType = strings.TrimSpace(req.Message.ContentType)
	req.Message.MimeType = strings.TrimSpace(req.Message.MimeType)
	req.Message.URL = strings.TrimSpace(req.Message.URL)
	if req.Message.MimeType == "" {
		req.Message.MimeType = req.Message.ContentType
	}
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyID("clientId", req.ClientID); err != nil {
		return err
	}
	if req.EventID == "" {
		return errors.New("eventId is required")
	}
	if err := validateThirdPartyID("eventId", req.EventID); err != nil {
		return err
	}
	if opts.RequireMessageID && req.MessageID == "" {
		return errors.New("messageId is required")
	}
	if req.MessageID != "" {
		if err := validateThirdPartyID("messageId", req.MessageID); err != nil {
			return err
		}
	}
	if req.ConversationID == "" {
		req.ConversationID = strings.TrimSpace(opts.DefaultConversationID)
		if req.ConversationID == "" {
			req.ConversationID = "default"
		}
	}
	if err := validateThirdPartyID("conversationId", req.ConversationID); err != nil {
		return err
	}
	if opts.RequireUserID && req.User.ID == "" {
		return errors.New("user.id is required")
	}
	if !IsSupportedThirdPartyMessageType(req.Message.Type) {
		return errors.New("message.type must be one of text, image, file, voice, or audio")
	}
	maxText := opts.MaxTextChars
	if maxText <= 0 {
		maxText = ThirdPartyMaxTextChars
	}
	if req.Message.Type == "text" && req.Message.Text == "" {
		return errors.New("message.text is required")
	}
	if len(req.Message.Text) > maxText {
		return fmt.Errorf("message.text exceeds %d characters", maxText)
	}
	if req.Message.Type != "text" && len(req.Message.Text) > ThirdPartyMaxMediaCaption {
		return fmt.Errorf("message.text exceeds %d characters for media caption", ThirdPartyMaxMediaCaption)
	}
	attachments := NormalizeThirdPartyMessageAttachments(req.Message)
	if len(attachments) > ThirdPartyMaxAttachments {
		return fmt.Errorf("message.attachments exceeds %d items", ThirdPartyMaxAttachments)
	}
	for i := range attachments {
		if err := NormalizeThirdPartyMediaReference(&attachments[i], req.Message.Type, i); err != nil {
			return err
		}
	}
	if req.Message.Type != "text" && len(attachments) == 0 {
		return errors.New("message attachment data, url, fileName, or attachments is required for media messages")
	}
	req.Message.Attachments = attachments
	return nil
}

func NormalizeThirdPartyID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ':':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".:-_")
}

func NormalizeThirdPartyMessageType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "audio":
		return "voice"
	case "text", "image", "file", "voice":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func IsSupportedThirdPartyMessageType(value string) bool {
	switch NormalizeThirdPartyMessageType(value) {
	case "text", "image", "file", "voice":
		return true
	default:
		return false
	}
}

func NormalizeThirdPartyMessageAttachments(msg ThirdPartyMessagePayload) []ThirdPartyMediaReference {
	attachments := append([]ThirdPartyMediaReference(nil), msg.Attachments...)
	if strings.TrimSpace(msg.ID) != "" || strings.TrimSpace(msg.URL) != "" || strings.TrimSpace(msg.Data) != "" || strings.TrimSpace(msg.FileName) != "" || strings.TrimSpace(msg.MimeType) != "" || strings.TrimSpace(msg.ContentType) != "" || msg.SizeBytes > 0 || msg.DurationMs > 0 {
		attachments = append([]ThirdPartyMediaReference{{
			ID:          msg.ID,
			Type:        msg.Type,
			FileName:    msg.FileName,
			ContentType: msg.ContentType,
			MimeType:    msg.MimeType,
			Data:        msg.Data,
			URL:         msg.URL,
			SizeBytes:   msg.SizeBytes,
			DurationMs:  msg.DurationMs,
		}}, attachments...)
	}
	return attachments
}

func NormalizeThirdPartyMediaReference(att *ThirdPartyMediaReference, fallbackType string, index int) error {
	att.ID = strings.TrimSpace(att.ID)
	att.Type = NormalizeThirdPartyMessageType(defaultString(att.Type, fallbackType))
	att.FileName = strings.TrimSpace(att.FileName)
	att.ContentType = strings.TrimSpace(att.ContentType)
	att.MimeType = strings.TrimSpace(att.MimeType)
	if att.MimeType == "" {
		att.MimeType = att.ContentType
	}
	att.Data = strings.TrimSpace(att.Data)
	att.URL = strings.TrimSpace(att.URL)
	att.SHA256 = strings.TrimSpace(att.SHA256)
	if att.Type == "" || att.Type == "text" {
		return fmt.Errorf("message.attachments[%d].type must be image, file, voice, or audio", index)
	}
	if att.ID != "" && utf8.RuneCountInString(att.ID) > ThirdPartyMaxIDChars {
		return fmt.Errorf("message.attachments[%d].id exceeds %d characters", index, ThirdPartyMaxIDChars)
	}
	if att.ID == "" && att.URL == "" && att.Data == "" {
		return fmt.Errorf("message.attachments[%d] requires id, data, or server media url", index)
	}
	if att.Data != "" {
		decoded, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			return fmt.Errorf("message.attachments[%d].data must be base64", index)
		}
		if len(decoded) > ThirdPartyMaxDirectBytes {
			return fmt.Errorf("message.attachments[%d].data exceeds %d bytes; use server media upload URL", index, ThirdPartyMaxDirectBytes)
		}
		if att.SizeBytes == 0 {
			att.SizeBytes = int64(len(decoded))
		} else if att.SizeBytes != int64(len(decoded)) {
			return fmt.Errorf("message.attachments[%d].sizeBytes mismatch: got %d bytes, want %d", index, len(decoded), att.SizeBytes)
		}
	}
	if utf8.RuneCountInString(att.URL) > ThirdPartyMaxURLChars {
		return fmt.Errorf("message.attachments[%d].url exceeds %d characters", index, ThirdPartyMaxURLChars)
	}
	if att.SizeBytes < 0 {
		return fmt.Errorf("message.attachments[%d].sizeBytes must be non-negative", index)
	}
	if att.DurationMs < 0 {
		return fmt.Errorf("message.attachments[%d].durationMs must be non-negative", index)
	}
	return nil
}

func ThirdPartyIncomingContent(req ThirdPartyIncomingRequest) string {
	if req.Message.Type == "text" {
		return req.Message.Text
	}
	var b strings.Builder
	if req.Message.Text != "" {
		b.WriteString(req.Message.Text)
		b.WriteString("\n\n")
	}
	b.WriteString("[Third-party ")
	b.WriteString(req.Message.Type)
	b.WriteString(" message]")
	for i, att := range req.Message.Attachments {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". type=")
		b.WriteString(att.Type)
		if att.FileName != "" {
			b.WriteString(" fileName=")
			b.WriteString(att.FileName)
		}
		if att.MimeType != "" {
			b.WriteString(" mimeType=")
			b.WriteString(att.MimeType)
		}
		if att.URL != "" {
			b.WriteString(" url=")
			b.WriteString(att.URL)
		}
		if att.SizeBytes > 0 {
			b.WriteString(" sizeBytes=")
			b.WriteString(strconv.FormatInt(att.SizeBytes, 10))
		}
		if att.DurationMs > 0 {
			b.WriteString(" durationMs=")
			b.WriteString(strconv.FormatInt(att.DurationMs, 10))
		}
	}
	return b.String()
}

func ThirdPartyToolResultContent(req ThirdPartyToolResultRequest) string {
	var b strings.Builder
	b.WriteString("[Client tool result]")
	if req.ResultID != "" {
		b.WriteString("\nresultId=")
		b.WriteString(req.ResultID)
	}
	if req.ToolCallID != "" {
		b.WriteString("\ntoolCallId=")
		b.WriteString(req.ToolCallID)
	}
	if req.ToolPlanID != "" {
		b.WriteString("\ntoolPlanId=")
		b.WriteString(req.ToolPlanID)
	}
	if req.StepID != "" {
		b.WriteString("\nstepId=")
		b.WriteString(req.StepID)
	}
	b.WriteString("\nstatus=")
	b.WriteString(req.Status)
	if req.IdempotencyKey != "" {
		b.WriteString("\nidempotencyKey=")
		b.WriteString(req.IdempotencyKey)
	}
	if req.Text != "" {
		b.WriteString("\ntext=")
		b.WriteString(req.Text)
	}
	if req.Error != nil {
		b.WriteString("\nerror=")
		if req.Error.Code != "" {
			b.WriteString(req.Error.Code)
			b.WriteString(": ")
		}
		b.WriteString(req.Error.Message)
	}
	if len(req.Result) > 0 {
		if data, err := json.MarshalIndent(req.Result, "", "  "); err == nil {
			b.WriteString("\nresult=")
			b.Write(data)
		}
	}
	return b.String()
}

func ThirdPartyToolResultEventID(req ThirdPartyToolResultRequest) string {
	if req.ResultID != "" {
		return "tool_result:" + req.ResultID
	}
	if req.IdempotencyKey != "" {
		key := NormalizeThirdPartyID(req.IdempotencyKey)
		if key != "" {
			return "tool_result:" + key
		}
	}
	parts := []string{req.ToolCallID, req.ToolPlanID, req.StepID, req.Status}
	var compact []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			compact = append(compact, NormalizeThirdPartyID(part))
		}
	}
	if len(compact) == 0 {
		return "tool_result:unknown"
	}
	return "tool_result:" + strings.Join(compact, ":")
}

func validateThirdPartyToolPlanAcyclic(steps []ThirdPartyToolPlanStep) error {
	graph := map[string][]string{}
	for _, step := range steps {
		graph[step.ID] = append([]string(nil), step.DependsOn...)
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return false
		}
		if visited[id] {
			return true
		}
		visiting[id] = true
		for _, dep := range graph[id] {
			if !visit(dep) {
				return false
			}
		}
		visiting[id] = false
		visited[id] = true
		return true
	}
	for id := range graph {
		if !visit(id) {
			return errors.New("toolPlan.steps contains a dependency cycle")
		}
	}
	return nil
}

func validateThirdPartyID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(value) > ThirdPartyMaxIDChars {
		return fmt.Errorf("%s exceeds %d characters", field, ThirdPartyMaxIDChars)
	}
	return nil
}

func validateThirdPartyToolName(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(value) > ThirdPartyMaxIDChars {
		return fmt.Errorf("%s exceeds %d characters", field, ThirdPartyMaxIDChars)
	}
	return nil
}

func validateThirdPartyIdempotencyKey(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > ThirdPartyMaxIDChars {
		return fmt.Errorf("%s exceeds %d characters", field, ThirdPartyMaxIDChars)
	}
	if NormalizeThirdPartyID(value) == "" {
		return fmt.Errorf("%s must contain at least one identifier character", field)
	}
	return nil
}

func validateThirdPartyJSONSize(field string, value any) error {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%s must be JSON serializable", field)
	}
	if len(data) > ThirdPartyMaxToolJSON {
		return fmt.Errorf("%s exceeds %d bytes", field, ThirdPartyMaxToolJSON)
	}
	return nil
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
