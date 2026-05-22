package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const bridgeChannelsConfigKey = "openclaw_bridge_channels"

// KnownChannel describes a supported OpenClaw channel plugin.
type KnownChannel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	NameZH      string  `json:"name_zh"`
	Package     string  `json:"package"`     // npm package name
	AltPackage  string  `json:"alt_package"` // fallback package name
	Fields      []Field `json:"fields"`
	Description string  `json:"description"`
	DescZH      string  `json:"desc_zh"`
}

type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	LabelZH     string `json:"label_zh"`
	Type        string `json:"type"` // "text" | "password"
	Placeholder string `json:"placeholder"`
}

var knownChannels = []KnownChannel{
	{
		ID: "telegram", Name: "Telegram", NameZH: "Telegram",
		Package: "@openclaw/telegram", AltPackage: "openclaw-channel-telegram",
		Description: "Connect a Telegram bot to receive and send messages.",
		DescZH:      "\u8fde\u63a5 Telegram \u673a\u5668\u4eba\u6536\u53d1\u6d88\u606f\u3002",
		Fields: []Field{
			{Key: "botToken", Label: "Bot Token", LabelZH: "Bot Token", Type: "password", Placeholder: "123456:ABC-DEF..."},
		},
	},
	{
		ID: "discord", Name: "Discord", NameZH: "Discord",
		Package: "@openclaw/discord", AltPackage: "openclaw-channel-discord",
		Description: "Connect a Discord bot.",
		DescZH:      "\u8fde\u63a5 Discord \u673a\u5668\u4eba\u3002",
		Fields: []Field{
			{Key: "botToken", Label: "Bot Token", LabelZH: "Bot Token", Type: "password", Placeholder: ""},
			{Key: "applicationId", Label: "Application ID", LabelZH: "Application ID", Type: "text", Placeholder: ""},
		},
	},
	{
		ID: "slack", Name: "Slack", NameZH: "Slack",
		Package: "@openclaw/slack", AltPackage: "openclaw-channel-slack",
		Description: "Connect a Slack bot.",
		DescZH:      "\u8fde\u63a5 Slack \u673a\u5668\u4eba\u3002",
		Fields: []Field{
			{Key: "botToken", Label: "Bot Token", LabelZH: "Bot Token", Type: "password", Placeholder: "xoxb-..."},
			{Key: "appToken", Label: "App Token", LabelZH: "App Token", Type: "password", Placeholder: "xapp-..."},
		},
	},
	{
		ID: "wechatwork", Name: "WeChat Work", NameZH: "\u4f01\u4e1a\u5fae\u4fe1",
		Package: "@openclaw/wechatwork", AltPackage: "openclaw-channel-wechatwork",
		Description: "Connect WeChat Work bot.",
		DescZH:      "\u8fde\u63a5\u4f01\u4e1a\u5fae\u4fe1\u673a\u5668\u4eba\u3002",
		Fields: []Field{
			{Key: "corpId", Label: "Corp ID", LabelZH: "\u4f01\u4e1a ID", Type: "text", Placeholder: ""},
			{Key: "agentId", Label: "Agent ID", LabelZH: "\u5e94\u7528 ID", Type: "text", Placeholder: ""},
			{Key: "secret", Label: "Secret", LabelZH: "\u5e94\u7528\u5bc6\u94a5", Type: "password", Placeholder: ""},
			{Key: "token", Label: "Token", LabelZH: "Token", Type: "password", Placeholder: ""},
			{Key: "encodingAESKey", Label: "Encoding AES Key", LabelZH: "EncodingAESKey", Type: "password", Placeholder: ""},
		},
	},
	{
		ID: "dingtalk", Name: "DingTalk", NameZH: "\u9489\u9489",
		Package: "@openclaw/dingtalk", AltPackage: "openclaw-channel-dingtalk",
		Description: "Connect DingTalk bot.",
		DescZH:      "\u8fde\u63a5\u9489\u9489\u673a\u5668\u4eba\u3002",
		Fields: []Field{
			{Key: "appKey", Label: "App Key", LabelZH: "App Key", Type: "text", Placeholder: ""},
			{Key: "appSecret", Label: "App Secret", LabelZH: "App Secret", Type: "password", Placeholder: ""},
		},
	},
}

// ChannelState is the per-channel config stored in the DB and written to bridge config.json.
type ChannelState struct {
	Enabled bool              `json:"enabled"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// BridgeConfigJSON mirrors the bridge's config.json structure.
type BridgeConfigJSON struct {
	Hub struct {
		WebhookURL string `json:"webhookUrl"`
		Secret     string `json:"secret"`
	} `json:"hub"`
	Bridge struct {
		Port int    `json:"port"`
		Host string `json:"host"`
	} `json:"bridge"`
	Channels map[string]map[string]interface{} `json:"channels"`
}

// bridgeInstallMu serializes npm install operations.
var bridgeInstallMu sync.Mutex

// GetBridgeChannelsHandler returns the list of known channels and their current config.
func GetBridgeChannelsHandler(system store.SystemSettingsRepository, bridgeDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		saved := loadChannelStates(r, system)
		type channelResp struct {
			KnownChannel
			Enabled   bool              `json:"enabled"`
			Config    map[string]string `json:"config"`
			Installed bool              `json:"installed"`
		}
		result := make([]channelResp, 0, len(knownChannels))
		for _, ch := range knownChannels {
			cr := channelResp{KnownChannel: ch}
			if st, ok := saved[ch.ID]; ok {
				cr.Enabled = st.Enabled
				cr.Config = st.Fields
			}
			if cr.Config == nil {
				cr.Config = map[string]string{}
			}
			cr.Installed = isNpmPackageInstalled(bridgeDir, ch.Package) || (ch.AltPackage != "" && isNpmPackageInstalled(bridgeDir, ch.AltPackage))
			result = append(result, cr)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"channels": result})
	}
}

// SaveBridgeChannelHandler saves a single channel's config, installs the npm
// package if needed, and regenerates the bridge config.json.
func SaveBridgeChannelHandler(system store.SystemSettingsRepository, bridgeDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		var req struct {
			ID      string            `json:"id"`
			Enabled bool              `json:"enabled"`
			Fields  map[string]string `json:"fields"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "MISSING_ID", "Channel ID is required")
			return
		}

		// Validate channel ID
		var known *KnownChannel
		for i := range knownChannels {
			if knownChannels[i].ID == req.ID {
				known = &knownChannels[i]
				break
			}
		}
		if known == nil {
			writeError(w, http.StatusBadRequest, "UNKNOWN_CHANNEL", "Unknown channel: "+req.ID)
			return
		}

		// Load existing states and only accept known field keys.
		validKeys := make(map[string]bool, len(known.Fields))
		for _, f := range known.Fields {
			validKeys[f.Key] = true
		}
		cleanFields := make(map[string]string, len(req.Fields))
		for k, v := range req.Fields {
			if validKeys[k] {
				cleanFields[k] = v
			}
		}
		saved := loadChannelStates(r, system)
		saved[req.ID] = ChannelState{Enabled: req.Enabled, Fields: cleanFields}

		// Persist to DB
		data, _ := json.Marshal(saved)
		if err := system.Set(r.Context(), bridgeChannelsConfigKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", err.Error())
			return
		}

		// Auto-install npm package if enabling and not yet installed
		installMsg := ""
		if req.Enabled && bridgeDir != "" {
			if !isNpmPackageInstalled(bridgeDir, known.Package) {
				if err := npmInstallPackage(bridgeDir, known.Package); err != nil {
					// Try fallback package name
					if known.AltPackage != "" {
						if err2 := npmInstallPackage(bridgeDir, known.AltPackage); err2 != nil {
							installMsg = fmt.Sprintf("npm install failed for both %s and %s", known.Package, known.AltPackage)
						} else {
							installMsg = fmt.Sprintf("npm install %s succeeded (fallback)", known.AltPackage)
						}
					} else {
						installMsg = fmt.Sprintf("npm install %s failed: %v", known.Package, err)
					}
				} else {
					installMsg = fmt.Sprintf("npm install %s succeeded", known.Package)
				}
			}
		}

		// Regenerate bridge config.json
		configErr := ""
		if shouldWriteSharedBridgeConfig(r, bridgeDir) {
			if err := writeBridgeConfig(r, system, bridgeDir, saved); err != nil {
				configErr = err.Error()
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":          true,
			"install_msg": installMsg,
			"config_err":  configErr,
		})
	}
}

// BridgeStatusHandler checks if the bridge is reachable and returns its status.
func BridgeStatusHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system = scopedSystemSettingsForRequest(r, system)
		cfg := loadOpenclawIMConfig(r, system)
		webhookURL := cfg.WebhookURL
		if webhookURL == "" {
			webhookURL = "http://127.0.0.1:3210/outbound"
		}
		// Derive health URL from webhook URL
		healthURL := strings.TrimSuffix(webhookURL, "/outbound") + "/health"

		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, healthURL, nil)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"running":  false,
				"error":    err.Error(),
				"channels": []string{},
			})
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"running":  false,
				"error":    err.Error(),
				"channels": []string{},
			})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var result map[string]interface{}
		if json.Unmarshal(body, &result) != nil {
			result = map[string]interface{}{}
		}
		result["running"] = resp.StatusCode == 200
		writeJSON(w, http.StatusOK, result)
	}
}

// InstallBridgeDepsHandler runs npm install in the bridge directory.
func InstallBridgeDepsHandler(bridgeDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bridgeDir == "" {
			writeError(w, http.StatusBadRequest, "NO_BRIDGE_DIR", "Bridge directory not configured")
			return
		}
		bridgeInstallMu.Lock()
		defer bridgeInstallMu.Unlock()

		npmCmd := "npm"
		if runtime.GOOS == "windows" {
			npmCmd = "npm.cmd"
		}
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, npmCmd, "install")
		cmd.Dir = bridgeDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":     false,
				"output": string(out),
				"error":  err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":     true,
			"output": string(out),
		})
	}
}

// --- helpers ---

func loadChannelStates(r *http.Request, system store.SystemSettingsRepository) map[string]ChannelState {
	raw, err := system.Get(r.Context(), bridgeChannelsConfigKey)
	if err != nil || raw == "" {
		return map[string]ChannelState{}
	}
	var states map[string]ChannelState
	if json.Unmarshal([]byte(raw), &states) != nil {
		return map[string]ChannelState{}
	}
	return states
}

func isNpmPackageInstalled(bridgeDir, pkg string) bool {
	if bridgeDir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(bridgeDir, "node_modules", pkg))
	return err == nil && info.IsDir()
}

func npmInstallPackage(bridgeDir, pkg string) error {
	bridgeInstallMu.Lock()
	defer bridgeInstallMu.Unlock()

	npmCmd := "npm"
	if runtime.GOOS == "windows" {
		npmCmd = "npm.cmd"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, npmCmd, "install", pkg)
	cmd.Dir = bridgeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}

func shouldWriteSharedBridgeConfig(r *http.Request, bridgeDir string) bool {
	return strings.TrimSpace(bridgeDir) != "" && !isTenantScopedAdminRequest(r)
}
func writeBridgeConfig(r *http.Request, system store.SystemSettingsRepository, bridgeDir string, channels map[string]ChannelState) error {
	// Load the openclaw IM config for hub webhook URL and secret
	imCfg := loadOpenclawIMConfig(r, system)
	secret := imCfg.Secret
	if secret == "" {
		secret = DefaultOpenclawIMSecret
	}

	bcfg := BridgeConfigJSON{}
	bcfg.Hub.WebhookURL = "http://127.0.0.1:9399/api/openclaw_im/webhook"
	bcfg.Hub.Secret = secret
	bcfg.Bridge.Port = 3210
	bcfg.Bridge.Host = "127.0.0.1"
	bcfg.Channels = make(map[string]map[string]interface{})

	for id, st := range channels {
		ch := map[string]interface{}{
			"enabled": st.Enabled,
		}
		for k, v := range st.Fields {
			ch[k] = v
		}
		bcfg.Channels[id] = ch
	}

	data, err := json.MarshalIndent(bcfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bridgeDir, "config.json"), data, 0644)
}
