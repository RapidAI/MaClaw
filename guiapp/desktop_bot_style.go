package guiapp

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DesktopBotStyleVersion is one saved state of a bot's character.
type DesktopBotStyleVersion struct {
	Version   int               `json:"version"`
	Soul      string            `json:"soul,omitempty"`
	Memory    string            `json:"memory,omitempty"`
	Parts     map[string]string `json:"parts,omitempty"`
	BaseHash  string            `json:"base_hash,omitempty"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// DesktopBotStyle is a bot's character, memory and style-part overrides as
// the Hub keeps them (GET /api/v1/bots/{id}/style), with the defaults.
type DesktopBotStyle struct {
	BotID        string                   `json:"bot_id"`
	Current      DesktopBotStyleVersion   `json:"current"`
	History      []DesktopBotStyleVersion `json:"history,omitempty"`
	DefaultSoul  string                   `json:"default_soul"`
	DefaultParts map[string]string        `json:"default_parts"`
	BaseChanged  bool                     `json:"base_changed"`
	Enabled      bool                     `json:"enabled"`
}

func desktopBotStylePath(botID string) string {
	return "/api/v1/bots/" + url.PathEscape(strings.TrimSpace(botID)) + "/style"
}

// GetDesktopBotStyle reads one bot's style from the Hub.
func (a *App) GetDesktopBotStyle(botID string) (*DesktopBotStyle, error) {
	var out DesktopBotStyle
	if err := a.desktopBotCall(context.Background(), http.MethodGet, desktopBotStylePath(botID), nil, &out, 30*time.Second); err != nil {
		return nil, err
	}
	return &out, nil
}

// SaveDesktopBotStyle stores a new version (soul, memory and the whole
// part-override map). The next turn of that bot uses it.
func (a *App) SaveDesktopBotStyle(botID, soul, memory string, parts map[string]string) (*DesktopBotStyle, error) {
	if parts == nil {
		parts = map[string]string{}
	}
	var out DesktopBotStyle
	body := map[string]any{"soul": soul, "memory": memory, "parts": parts}
	if err := a.desktopBotCall(context.Background(), http.MethodPut, desktopBotStylePath(botID), body, &out, 30*time.Second); err != nil {
		return nil, err
	}
	return &out, nil
}

// RollbackDesktopBotStyle makes an earlier version current again.
func (a *App) RollbackDesktopBotStyle(botID string, version int) (*DesktopBotStyle, error) {
	var out DesktopBotStyle
	if err := a.desktopBotCall(context.Background(), http.MethodPost, desktopBotStylePath(botID)+"/rollback", map[string]int{"version": version}, &out, 30*time.Second); err != nil {
		return nil, err
	}
	return &out, nil
}
