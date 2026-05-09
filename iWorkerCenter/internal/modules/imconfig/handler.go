package imconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// IMConfig holds configuration for all IM gateways.
type IMConfig struct {
	Feishu   *FeishuConfig   `json:"feishu,omitempty"`
	DingTalk *DingTalkConfig `json:"dingtalk,omitempty"`
	WeCom    *WeComConfig    `json:"wecom,omitempty"`
}

// FeishuConfig holds Feishu bot credentials.
type FeishuConfig struct {
	AppID             string `json:"app_id"`
	AppSecret         string `json:"app_secret"`
	VerificationToken string `json:"verification_token"`
	EncryptKey        string `json:"encrypt_key"`
	Enabled           bool   `json:"enabled"`
}

// DingTalkConfig holds DingTalk bot credentials.
type DingTalkConfig struct {
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret"`
	RobotCode string `json:"robot_code"`
	Enabled   bool   `json:"enabled"`
}

// WeComConfig holds WeCom bot credentials.
type WeComConfig struct {
	CorpID     string `json:"corp_id"`
	CorpSecret string `json:"corp_secret"`
	AgentID    int    `json:"agent_id"`
	Token      string `json:"token"`
	AESKey     string `json:"aes_key"`
	Enabled    bool   `json:"enabled"`
}

// Handler provides HTTP endpoints for IM configuration.
type Handler struct{}

// NewHandler creates an IM config Handler.
func NewHandler() *Handler { return &Handler{} }

const maxIMConfigJSONBodyBytes = 64 << 10

func decodeIMConfigJSON(body io.Reader, dst any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxIMConfigJSONBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxIMConfigJSONBodyBytes {
		return errors.New("im config json body exceeds size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("im config json body contains trailing data")
		}
		return err
	}
	return nil
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/im-config", h.handle)
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := Load()
		response.OK(w, cfg)
	case http.MethodPut:
		var cfg IMConfig
		if err := decodeIMConfigJSON(r.Body, &cfg); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		if err := Save(cfg); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "ok"})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".iworkercenter", "im_config.json")
}

// Load reads the IM config from disk.
func Load() IMConfig {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return IMConfig{}
	}
	var cfg IMConfig
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// Save writes the IM config to disk.
func Save(cfg IMConfig) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
