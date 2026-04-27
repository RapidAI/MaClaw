package corelib

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/agentnet"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

var logDetailEnabled atomic.Bool

func init() {
	logDetailEnabled.Store(false)
}

// SetLogDetailEnabled updates the in-process detailed log gate.
func SetLogDetailEnabled(enabled bool) {
	logDetailEnabled.Store(enabled)
	tool.SetLogDetailEnabled(enabled)
	weixin.SetLogDetailEnabled(enabled)
	agentnet.SetLogDetailEnabled(enabled)
}

// IsLogDetailEnabled reports whether detailed logs may be written.
func IsLogDetailEnabled() bool {
	return logDetailEnabled.Load()
}

// SyncLogDetailEnabledFromDefaultConfig refreshes the gate from ~/.maclaw/config.json.
// Missing or unreadable config keeps the default disabled state.
func SyncLogDetailEnabledFromDefaultConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		SetLogDetailEnabled(false)
		return
	}
	SyncLogDetailEnabledFromConfigPath(filepath.Join(home, ".maclaw", "config.json"))
}

// SyncLogDetailEnabledFromConfigPath refreshes the gate from a specific config path.
func SyncLogDetailEnabledFromConfigPath(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		SetLogDetailEnabled(false)
		return
	}
	var cfg struct {
		LogDetailEnabled bool `json:"log_detail_enabled"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		SetLogDetailEnabled(false)
		return
	}
	SetLogDetailEnabled(cfg.LogDetailEnabled)
}
