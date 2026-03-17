// Package im — WebhookIMPlugin implements IMPlugin for external IM adapters
// that communicate with Hub via HTTP webhooks (OpenClaw IM protocol).
//
// Outbound: Hub POSTs OutgoingMessage to the adapter's webhook URL.
// Inbound:  The adapter POSTs IncomingMessage to Hub's /api/openclaw_im/webhook endpoint,
//           which calls InjectMessage to feed it into the IM Adapter pipeline.
package im

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookConfig holds the configuration for a webhook-based IM plugin.
type WebhookConfig struct {
	WebhookURL string // Adapter's endpoint for outbound messages
	Secret     string // Shared HMAC secret
}

// WebhookConfigProvider supplies the current webhook config (read from DB).
type WebhookConfigProvider func() WebhookConfig

// WebhookIMPlugin implements IMPlugin for remote IM adapters connected via
// the OpenClaw IM webhook protocol.
type WebhookIMPlugin struct {
	platformName   string
	configProvider WebhookConfigProvider
	client         *http.Client

	mu             sync.Mutex
	messageHandler func(msg IncomingMessage)
}

// NewWebhookIMPlugin creates a plugin for the given platform name.
// configProvider is called on each outbound send to get the latest config.
func NewWebhookIMPlugin(platformName string, configProvider WebhookConfigProvider) *WebhookIMPlugin {
	return &WebhookIMPlugin{
		platformName:   platformName,
		configProvider: configProvider,
		client:         &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *WebhookIMPlugin) Name() string { return p.platformName }

func (p *WebhookIMPlugin) ReceiveMessage(handler func(msg IncomingMessage)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messageHandler = handler
}

// InjectMessage is called by the HTTP handler when an external adapter POSTs
// a message to Hub. It feeds the message into the IM Adapter pipeline via
// the registered messageHandler callback.
func (p *WebhookIMPlugin) InjectMessage(msg IncomingMessage) error {
	p.mu.Lock()
	handler := p.messageHandler
	p.mu.Unlock()

	if handler == nil {
		return fmt.Errorf("openclaw_im: no message handler registered for plugin %q", p.platformName)
	}

	// Ensure platform name is set correctly.
	msg.PlatformName = p.platformName
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	handler(msg)
	return nil
}

// SendText sends a plain text message to the adapter via webhook POST.
func (p *WebhookIMPlugin) SendText(ctx context.Context, target UserTarget, text string) error {
	payload := webhookOutPayload{
		Type:   "text",
		Target: target,
		Text:   text,
	}
	return p.postToAdapter(ctx, "message", payload)
}

// SendCard sends a rich card (OutgoingMessage) to the adapter via webhook POST.
func (p *WebhookIMPlugin) SendCard(ctx context.Context, target UserTarget, card OutgoingMessage) error {
	payload := webhookOutPayload{
		Type:    "card",
		Target:  target,
		Message: &card,
	}
	return p.postToAdapter(ctx, "message", payload)
}

// SendImage sends an image reference to the adapter via webhook POST.
func (p *WebhookIMPlugin) SendImage(ctx context.Context, target UserTarget, imageKey string, caption string) error {
	payload := webhookOutPayload{
		Type:     "image",
		Target:   target,
		ImageKey: imageKey,
		Caption:  caption,
	}
	return p.postToAdapter(ctx, "message", payload)
}

// ResolveUser is a no-op for webhook plugins — identity resolution is handled
// by the IM Adapter core using the IdentityResolver, not by the plugin itself.
// External adapters include platform_uid in their inbound messages.
func (p *WebhookIMPlugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	return "", fmt.Errorf("openclaw_im: ResolveUser not supported for webhook plugin %q; use IdentityResolver", p.platformName)
}

// Capabilities returns conservative defaults. External adapters can override
// by including a capabilities declaration in their registration handshake.
func (p *WebhookIMPlugin) Capabilities() CapabilityDeclaration {
	return CapabilityDeclaration{
		SupportsRichCard:    true,
		SupportsMarkdown:    true,
		SupportsImage:       true,
		SupportsButton:      true,
		SupportsMessageEdit: false,
		MaxTextLength:       4000,
	}
}

func (p *WebhookIMPlugin) Start(ctx context.Context) error {
	log.Printf("[openclaw_im/%s] webhook plugin started", p.platformName)
	return nil
}

func (p *WebhookIMPlugin) Stop(ctx context.Context) error {
	log.Printf("[openclaw_im/%s] webhook plugin stopped", p.platformName)
	return nil
}

// ---------------------------------------------------------------------------
// Outbound webhook POST
// ---------------------------------------------------------------------------

type webhookOutPayload struct {
	Type     string          `json:"type"`               // "text", "card", "image"
	Target   UserTarget      `json:"target"`
	Text     string          `json:"text,omitempty"`
	Message  *OutgoingMessage `json:"message,omitempty"`
	ImageKey string          `json:"image_key,omitempty"`
	Caption  string          `json:"caption,omitempty"`
}

func (p *WebhookIMPlugin) postToAdapter(ctx context.Context, event string, payload any) error {
	cfg := p.configProvider()
	if cfg.WebhookURL == "" {
		return fmt.Errorf("openclaw_im: webhook URL not configured for plugin %q", p.platformName)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("openclaw_im: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("openclaw_im: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Event", event)

	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-OpenClaw-Signature", "sha256="+sig)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("[openclaw_im/%s] POST to adapter failed: %v", p.platformName, err)
		return fmt.Errorf("openclaw_im: post to adapter: %w", err)
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		log.Printf("[openclaw_im/%s] adapter returned HTTP %d", p.platformName, resp.StatusCode)
		return fmt.Errorf("openclaw_im: adapter returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// VerifySignature validates the HMAC-SHA256 signature of an inbound request body.
// Returns true if the signature is valid or if no secret is configured.
func VerifySignature(body []byte, signature, secret string) bool {
	if secret == "" {
		return true // no secret configured, skip verification
	}
	if signature == "" {
		return false
	}
	// Strip "sha256=" prefix if present.
	const prefix = "sha256="
	if strings.HasPrefix(signature, prefix) {
		signature = signature[len(prefix):]
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
