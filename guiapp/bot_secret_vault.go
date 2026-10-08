package guiapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/zalando/go-keyring"
)

// botSecretKeyringService is the OS credential for a desktop user's site
// secrets. It is not the remote-SSH vault, and the key is the user plus the
// secret name, shared by that user's bots.
const botSecretKeyringService = "MaClaw Bot Secret"

func botSecretKey(userID, name string) (string, bool) {
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	if userID == "" || strings.Contains(userID, "/") || !agent.ValidSecretName(name) {
		return "", false
	}
	return userID + "/" + name, true
}

func saveBotSecret(userID, name, value string) error {
	key, ok := botSecretKey(userID, name)
	if !ok || strings.TrimSpace(value) == "" {
		return fmt.Errorf("unavailable")
	}
	if err := keyring.Set(botSecretKeyringService, key, value); err != nil {
		return fmt.Errorf("unavailable")
	}
	return nil
}

// recallBotSecret reports only whether the name is stored.
func recallBotSecret(userID, name string) bool {
	value, ok := loadBotSecret(userID, name)
	return ok && strings.TrimSpace(value) != ""
}

func loadBotSecret(userID, name string) (string, bool) {
	key, ok := botSecretKey(userID, name)
	if !ok {
		return "", false
	}
	value, err := keyring.Get(botSecretKeyringService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false
		}
		return "", false
	}
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// SaveBotSecret stores a site secret for this desktop user.
func (a *App) SaveBotSecret(name, value string) error {
	if a == nil {
		return fmt.Errorf("AI assistant backend is unavailable")
	}
	return saveBotSecret(a.desktopBotAccountUserID(), name, value)
}

// RecallBotSecret reports whether this desktop user has the named secret.
// It does not return the value.
func (a *App) RecallBotSecret(name string) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("AI assistant backend is unavailable")
	}
	return recallBotSecret(a.desktopBotAccountUserID(), name), nil
}

// FillBotSecret reads the named secret on this computer and asks Hub to
// insert it into the focused password field. The value is not logged.
func (a *App) FillBotSecret(botID, name string) error {
	if a == nil {
		return fmt.Errorf("AI assistant backend is unavailable")
	}
	botID = strings.TrimSpace(botID)
	name = strings.TrimSpace(name)
	if botID == "" || !agent.ValidSecretName(name) {
		return fmt.Errorf("unavailable")
	}
	value, ok := loadBotSecret(a.desktopBotAccountUserID(), name)
	if !ok {
		return fmt.Errorf("unavailable")
	}
	var out struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	err := a.desktopBotCall(context.Background(), http.MethodPost, "/api/v1/bots/"+url.PathEscape(botID)+"/secret-fill", map[string]string{
		"name":  name,
		"value": value,
	}, &out, 20*time.Second)
	if err != nil {
		return secretFillClientError(err, value)
	}
	if code := strings.TrimSpace(out.Error); code != "" {
		return secretFillClientError(fmt.Errorf("%s", code), value)
	}
	return nil
}

func secretFillClientError(err error, value string) error {
	if err == nil {
		return nil
	}
	code := strings.TrimSpace(err.Error())
	switch code {
	case "not_password_field", "no_focus", "unavailable":
	default:
		code = "unavailable"
	}
	if value != "" && strings.Contains(code, value) {
		code = "unavailable"
	}
	return fmt.Errorf("%s", code)
}
