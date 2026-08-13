package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Hot-path field peeks — lock-free via PeekConfig / publishedConfig.
// Prefer these over LoadConfig when only a few fields are needed.

func (a *App) peekConfigOrEmpty() corelib.AppConfig {
	if p := a.PeekConfig(); p != nil {
		return *p
	}
	if cfg, ok := a.publishedConfig(); ok {
		return cfg
	}
	return corelib.AppConfig{}
}

// PeekRemoteHubURL returns the configured Hub WebSocket/base URL.
func (a *App) PeekRemoteHubURL() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RemoteHubURL)
}

// PeekRemoteHubURLTrimmed returns Hub URL without trailing slash.
func (a *App) PeekRemoteHubURLTrimmed() string {
	return strings.TrimRight(a.PeekRemoteHubURL(), "/")
}

// PeekRemoteMachineToken returns the machine auth token (sensitive).
func (a *App) PeekRemoteMachineToken() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RemoteMachineToken)
}

// PeekRemoteMachineID returns the local machine id.
func (a *App) PeekRemoteMachineID() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RemoteMachineID)
}

// PeekRemoteClientID returns the viewer/client id.
func (a *App) PeekRemoteClientID() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RemoteClientID)
}

// PeekRemoteEmail returns the registration email.
func (a *App) PeekRemoteEmail() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.RemoteEmail)
}

// PeekMaclawLLMCurrentProvider returns the selected LLM provider name.
func (a *App) PeekMaclawLLMCurrentProvider() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.MaclawLLMCurrentProvider)
}

// PeekLanguage returns the UI language code.
func (a *App) PeekLanguage() string {
	p := a.PeekConfig()
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Language)
}

// PeekExternalSkillDirs returns configured external skill directories (shared slice).
func (a *App) PeekExternalSkillDirs() []string {
	p := a.PeekConfig()
	if p == nil {
		return nil
	}
	return p.ExternalSkillDirs
}

// UIShellConfig is a compact DTO for shell chrome (sidebar, zoom, language).
type UIShellConfig struct {
	Language             string  `json:"language"`
	ActiveTool           string  `json:"active_tool"`
	DefaultLaunchMode    string  `json:"default_launch_mode"`
	RemoteEnabled        bool    `json:"remote_enabled"`
	PauseEnvCheck        bool    `json:"pause_env_check"`
	CurrentProject       string  `json:"current_project"`
	UIZoomFactor         float64 `json:"ui_zoom_factor,omitempty"`
	ChatFontSize         int     `json:"chat_font_size,omitempty"`
	ShowAppEntry         *bool   `json:"show_app_entry,omitempty"`
	ShowCodingToolEntry  *bool   `json:"show_coding_tool_entry,omitempty"`
	ShowUtilitiesEntry   *bool   `json:"show_utilities_entry,omitempty"`
	MaclawLLMCurrentProv string  `json:"maclaw_llm_current_provider,omitempty"`
}

// GetUIShellConfig returns a small config slice for the app shell.
// Prefer this over LoadConfig when only chrome/settings chrome fields are needed.
func (a *App) GetUIShellConfig() UIShellConfig {
	cfg, err := a.LoadConfig()
	if err != nil {
		return UIShellConfig{}
	}
	return UIShellConfig{
		Language:             cfg.Language,
		ActiveTool:           cfg.ActiveTool,
		DefaultLaunchMode:    cfg.DefaultLaunchMode,
		RemoteEnabled:        cfg.RemoteEnabled,
		PauseEnvCheck:        cfg.PauseEnvCheck,
		CurrentProject:       cfg.CurrentProject,
		ShowAppEntry:         cfg.ShowAppEntry,
		ShowCodingToolEntry:  &cfg.ShowCodingToolEntry,
		ShowUtilitiesEntry:   cfg.ShowUtilitiesEntry,
		MaclawLLMCurrentProv: cfg.MaclawLLMCurrentProvider,
	}
}

// ensurePeekOrLoad returns PeekConfig or loads once on cold start.
func (a *App) ensurePeekOrLoad() *corelib.AppConfig {
	if p := a.PeekConfig(); p != nil {
		return p
	}
	if _, err := a.LoadConfig(); err != nil {
		return nil
	}
	return a.PeekConfig()
}
