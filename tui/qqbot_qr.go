package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

var tuiQQBotQR *qqbot.QRClient

func tuiQQBotQRClient() *qqbot.QRClient {
	if tuiQQBotQR != nil {
		return tuiQQBotQR
	}
	return qqbot.DefaultQRClient()
}

func applyQQBotQRCredentials(cfg *corelib.AppConfig, creds *qqbot.QRCredentials) error {
	if cfg == nil {
		return nil
	}
	if creds == nil || strings.TrimSpace(creds.AppID) == "" || strings.TrimSpace(creds.AppSecret) == "" {
		return qqbot.ErrQRBindIncomplete
	}
	cfg.QQBotEnabled = true
	cfg.QQBotAppID = strings.TrimSpace(creds.AppID)
	cfg.QQBotAppSecret = creds.AppSecret
	if strings.TrimSpace(cfg.QQBotOwnerOpenID) == "" && strings.TrimSpace(creds.UserOpenID) != "" {
		cfg.QQBotOwnerOpenID = strings.TrimSpace(creds.UserOpenID)
	}
	if cfg.QQBotLocalMode == nil {
		local := true
		cfg.QQBotLocalMode = &local
	}
	return nil
}

var errQQBotQRConfigLoad = errors.New("load qqbot config")

func persistQQBotQRCredentials(store *commands.FileConfigStore, creds *qqbot.QRCredentials) error {
	if store == nil {
		return errors.New("config store is nil")
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("%w: %v", errQQBotQRConfigLoad, err)
	}
	if err := applyQQBotQRCredentials(&cfg, creds); err != nil {
		return err
	}
	return store.SaveConfig(cfg)
}

func startQQBotQRLoginCmd(lang string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		taskID, qrURL, err := tuiQQBotQRClient().CreateBindTask(ctx)
		if err != nil {
			return views.ConfigQQBotQRMsg{Success: false, Message: err.Error()}
		}
		qrURL = strings.TrimSpace(qrURL)
		taskID = strings.TrimSpace(taskID)
		if qrURL == "" || taskID == "" {
			if taskID != "" {
				tuiQQBotQRClient().CancelBindTask(taskID)
			}
			return views.ConfigQQBotQRMsg{Success: false, Message: tuiText(lang, "qqbotQREmpty")}
		}
		return views.ConfigQQBotQRMsg{Success: true, QR: qrURL, Token: taskID}
	}
}

func pollQQBotQRLoginCmd(lang, token string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(token) == "" {
			return views.ConfigQQBotPollResultMsg{Status: "error", Message: tuiText(lang, "qqbotQREmpty"), Completed: true}
		}
		time.Sleep(1 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status, creds, err := tuiQQBotQRClient().PollBindStatus(ctx, token)
		if err != nil {
			if errors.Is(err, qqbot.ErrQRSessionNotFound) {
				return views.ConfigQQBotPollResultMsg{
					Token:     token,
					Status:    qqbot.QRLoginStatusExpired.String(),
					Message:   tuiQQBotQRStatusMessage(lang, qqbot.QRLoginStatusExpired),
					Completed: true,
				}
			}
			retryable := !errors.Is(err, qqbot.ErrQRCodeTokenEmpty)
			return views.ConfigQQBotPollResultMsg{
				Token:     token,
				Status:    "error",
				Message:   err.Error(),
				Completed: !retryable,
			}
		}
		msg := tuiQQBotQRStatusMessage(lang, status)
		if status == qqbot.QRLoginStatusConfirmed {
			if creds == nil || strings.TrimSpace(creds.AppID) == "" || strings.TrimSpace(creds.AppSecret) == "" {
				return views.ConfigQQBotPollResultMsg{Token: token, Status: "error", Message: tuiText(lang, "qqbotQRFailed"), Completed: false}
			}
			store := commands.NewFileConfigStore(commands.ResolveDataDir())
			if err := persistQQBotQRCredentials(store, creds); err != nil {
				key := "saveConfigFailed"
				if errors.Is(err, errQQBotQRConfigLoad) {
					key = "loadConfigFailed"
				}
				return views.ConfigQQBotPollResultMsg{Token: token, Status: "error", Message: tuiFormat(lang, key, err.Error()), Completed: false}
			}
			tuiQQBotQRClient().CancelBindTask(token)
			return views.ConfigQQBotPollResultMsg{
				Token:     token,
				Status:    status.String(),
				Message:   tuiText(lang, "qqbotBound"),
				Success:   true,
				Completed: true,
				AppID:     creds.AppID,
			}
		}
		if status == qqbot.QRLoginStatusExpired {
			return views.ConfigQQBotPollResultMsg{Token: token, Status: status.String(), Message: msg, Completed: true}
		}
		return views.ConfigQQBotPollResultMsg{Token: token, Status: status.String(), Message: msg, Completed: false}
	}
}

func tuiQQBotQRStatusMessage(lang string, status qqbot.QRLoginStatus) string {
	switch status {
	case qqbot.QRLoginStatusWait:
		return tuiText(lang, "qqbotWaitingScan")
	case qqbot.QRLoginStatusPending:
		return tuiText(lang, "qqbotScannedConfirm")
	case qqbot.QRLoginStatusConfirmed:
		return tuiText(lang, "qqbotBound")
	case qqbot.QRLoginStatusExpired:
		return tuiText(lang, "qqbotQRExpired")
	}
	return status.String()
}
