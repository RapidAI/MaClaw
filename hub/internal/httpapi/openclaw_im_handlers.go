package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const openclawIMConfigKey = "openclaw_im_config"

// DefaultOpenclawIMSecret is the pre-shared HMAC secret used by both Hub and
// the openclaw-bridge when running on the same machine (127.0.0.1).  Users may
// change it via the admin UI, but the default lets everything work out of the
// box without manual configuration.
const DefaultOpenclawIMSecret = "maclaw-openclaw-local-secret"

type OpenclawIMConfigState struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret"`
}

func GetOpenclawIMConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := system.Get(r.Context(), openclawIMConfigKey)
		if err != nil || raw == "" {
			// Return default config with localhost bridge address for same-machine deployment.
			writeJSON(w, http.StatusOK, OpenclawIMConfigState{
				WebhookURL: "http://127.0.0.1:3210/outbound",
				Secret:     maskSecret(DefaultOpenclawIMSecret),
			})
			return
		}
		var cfg OpenclawIMConfigState
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeJSON(w, http.StatusOK, OpenclawIMConfigState{
				WebhookURL: "http://127.0.0.1:3210/outbound",
			})
			return
		}
		if cfg.Secret != "" {
			cfg.Secret = maskSecret(cfg.Secret)
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateOpenclawIMConfigHandler(system store.SystemSettingsRepository, bridgeDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg OpenclawIMConfigState
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if cfg.WebhookURL != "" {
			u, err := url.Parse(cfg.WebhookURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK_URL", "Webhook URL must be a valid HTTP(S) URL")
				return
			}
		}
		if isMasked(cfg.Secret) {
			old := loadOpenclawIMConfig(r, system)
			cfg.Secret = old.Secret
		}
		if cfg.Secret == "" {
			cfg.Secret = DefaultOpenclawIMSecret
		}
		data, _ := json.Marshal(cfg)
		if err := system.Set(r.Context(), openclawIMConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "OPENCLAW_IM_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		// Sync secret to bridge config.json if bridge directory exists
		if bridgeDir != "" {
			channels := loadChannelStates(r, system)
			_ = writeBridgeConfig(r, system, bridgeDir, channels)
		}
		resp := cfg
		if resp.Secret != "" {
			resp.Secret = maskSecret(resp.Secret)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func loadOpenclawIMConfig(r *http.Request, system store.SystemSettingsRepository) OpenclawIMConfigState {
	raw, err := system.Get(r.Context(), openclawIMConfigKey)
	if err != nil || raw == "" {
		return OpenclawIMConfigState{Secret: DefaultOpenclawIMSecret}
	}
	var cfg OpenclawIMConfigState
	_ = json.Unmarshal([]byte(raw), &cfg)
	if cfg.Secret == "" {
		cfg.Secret = DefaultOpenclawIMSecret
	}
	return cfg
}

// TestOpenclawIMWebhookHandler sends a ping request to the configured webhook URL
// to verify connectivity and HMAC signature compatibility.
func TestOpenclawIMWebhookHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := loadOpenclawIMConfig(r, system)
		if cfg.WebhookURL == "" {
			writeError(w, http.StatusBadRequest, "NO_WEBHOOK_URL", "Webhook URL is not configured")
			return
		}

		payload := map[string]any{
			"type":      "ping",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"hub":       "maclaw-hub",
		}
		body, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_WEBHOOK_URL", fmt.Sprintf("Cannot create request: %v", err))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-OpenClaw-Event", "ping")

		if cfg.Secret != "" {
			mac := hmac.New(sha256.New, []byte(cfg.Secret))
			mac.Write(body)
			sig := hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-OpenClaw-Signature", "sha256="+sig)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      false,
				"status":  0,
				"message": fmt.Sprintf("Connection failed: %v", err),
			})
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      resp.StatusCode >= 200 && resp.StatusCode < 300,
			"status":  resp.StatusCode,
			"message": fmt.Sprintf("HTTP %d", resp.StatusCode),
			"body":    string(respBody),
		})
	}
}

// OpenclawIMWebhookHandler receives inbound messages from external IM adapters
// via the OpenClaw IM protocol. It validates the HMAC signature, parses the
// incoming message, and injects it into the IM Adapter pipeline via the
// WebhookIMPlugin.
//
// Protocol:
//   POST /api/openclaw_im/webhook
//   Headers:
//     Content-Type: application/json
//     X-OpenClaw-Signature: sha256=<hex HMAC-SHA256>
//   Body: { "platform_uid": "...", "text": "...", "message_type": "text" }
func OpenclawIMWebhookHandler(system store.SystemSettingsRepository, plugin *im.WebhookIMPlugin) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if plugin == nil {
			writeError(w, http.StatusServiceUnavailable, "PLUGIN_NOT_CONFIGURED", "OpenClaw IM plugin is not initialized")
			return
		}

		cfg := loadOpenclawIMConfig(r, system)
		if !cfg.Enabled {
			writeError(w, http.StatusForbidden, "OPENCLAW_IM_DISABLED", "OpenClaw IM integration is disabled")
			return
		}

		// Read body with size limit (64 KB).
		body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
		if err != nil {
			writeError(w, http.StatusBadRequest, "READ_BODY_FAILED", "Failed to read request body")
			return
		}

		// Verify HMAC signature.
		signature := r.Header.Get("X-OpenClaw-Signature")
		if !im.VerifySignature(body, signature, cfg.Secret) {
			writeError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "HMAC signature verification failed")
			return
		}

		// Parse incoming message.
		var msg im.IncomingMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Failed to parse message body")
			return
		}

		// Basic validation.
		if msg.PlatformUID == "" {
			writeError(w, http.StatusBadRequest, "MISSING_PLATFORM_UID", "platform_uid is required")
			return
		}
		if msg.Text == "" && msg.MessageType != "image" {
			writeError(w, http.StatusBadRequest, "MISSING_TEXT", "text is required for non-image messages")
			return
		}

		// Inject into the IM Adapter pipeline.
		if err := plugin.InjectMessage(msg); err != nil {
			writeError(w, http.StatusInternalServerError, "INJECT_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
