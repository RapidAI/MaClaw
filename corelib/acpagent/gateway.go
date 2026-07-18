package acpagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

// GatewayClient talks to MaClaw GUI third-party IM Gateway (loopback HTTP).
type GatewayClient struct {
	BaseURL    string
	Token      string
	ClientID   string
	ClientName string
	UserID     string
	UserName   string
	HTTP       *http.Client
}

func NewGatewayClient(ep GatewayEndpoint) *GatewayClient {
	return &GatewayClient{
		BaseURL:    strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/"),
		Token:      strings.TrimSpace(ep.Token),
		ClientID:   DefaultClientID,
		ClientName: "MaClaw ACP Bridge",
		UserID:     "vscode",
		UserName:   "VS Code",
		HTTP:       &http.Client{Timeout: 0}, // per-request timeouts via context
	}
}

func (g *GatewayClient) Health(ctx context.Context) (*coreim.ThirdPartyGatewayHealthResponse, error) {
	var out coreim.ThirdPartyGatewayHealthResponse
	if err := g.doJSON(ctx, http.MethodGet, g.BaseURL+"/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GatewayClient) Handshake(ctx context.Context) (*coreim.ThirdPartyGatewayHandshakeResponse, error) {
	req := coreim.ThirdPartyHandshakeRequest{
		ClientID:        g.ClientID,
		ClientName:      g.ClientName,
		ProtocolVersion: coreim.ThirdPartyProtocolVersion,
		Capabilities:    coreim.ThirdPartyCapabilityMap(),
	}
	if err := coreim.NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		return nil, err
	}
	var out coreim.ThirdPartyGatewayHandshakeResponse
	if err := g.doJSON(ctx, http.MethodPost, g.BaseURL+"/handshake", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GatewayClient) SendText(ctx context.Context, conversationID, eventID, messageID, text string) (*coreim.ThirdPartyIncomingAcceptedResponse, error) {
	req := coreim.ThirdPartyIncomingRequest{
		ClientID:       g.ClientID,
		EventID:        eventID,
		MessageID:      messageID,
		ConversationID: conversationID,
		User:           coreim.ThirdPartyUserRef{ID: g.UserID, Name: g.UserName},
		Message:        coreim.ThirdPartyMessagePayload{Type: "text", Text: text},
		CreatedAt:      time.Now().UnixMilli(),
		Metadata: map[string]string{
			"source":   "acp-bridge",
			"protocol": "agent-client-protocol",
		},
	}
	if err := coreim.NormalizeThirdPartyIncomingRequest(&req, coreim.ThirdPartyNormalizeOptions{
		DefaultConversationID: conversationID,
	}); err != nil {
		return nil, err
	}
	var out coreim.ThirdPartyIncomingAcceptedResponse
	if err := g.doJSON(ctx, http.MethodPost, g.BaseURL+"/incoming", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GatewayClient) Poll(ctx context.Context, cursor string, timeoutSec, limit int) (*coreim.ThirdPartyOutgoingPollResponse, error) {
	if cursor == "" {
		cursor = "0"
	}
	if timeoutSec < 0 {
		timeoutSec = 0
	}
	if limit <= 0 {
		limit = coreim.ThirdPartyMaxBatchSize
	}
	u := fmt.Sprintf("%s/outgoing?clientId=%s&cursor=%s&timeout=%d&limit=%d",
		g.BaseURL,
		url.QueryEscape(g.ClientID),
		url.QueryEscape(cursor),
		timeoutSec,
		limit,
	)
	var out coreim.ThirdPartyOutgoingPollResponse
	if err := g.doJSON(ctx, http.MethodGet, u, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GatewayClient) Ack(ctx context.Context, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}
	req := coreim.ThirdPartyAckRequest{
		ClientID:   g.ClientID,
		MessageIDs: messageIDs,
		Status:     "delivered",
	}
	if err := coreim.NormalizeThirdPartyAckRequest(&req, coreim.ThirdPartyMaxAckIDs); err != nil {
		return err
	}
	var out map[string]any
	return g.doJSON(ctx, http.MethodPost, g.BaseURL+"/ack", req, &out)
}

type gatewayErrorEnvelope struct {
	OK    bool `json:"ok"`
	Error *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

func (g *GatewayClient) doJSON(ctx context.Context, method, endpoint string, body any, out any) error {
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
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "maclaw-acp-bridge")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		data = []byte("{}")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env gatewayErrorEnvelope
		if json.Unmarshal(data, &env) == nil && env.Error != nil {
			return fmt.Errorf("gateway HTTP %d [%s] %s", resp.StatusCode, env.Error.Code, env.Error.Message)
		}
		return fmt.Errorf("gateway HTTP %d: %s", resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode gateway response: %w: %s", err, string(data))
	}
	return nil
}

// FormatCursor returns a decimal cursor string.
func FormatCursor(n int64) string {
	return strconv.FormatInt(n, 10)
}
