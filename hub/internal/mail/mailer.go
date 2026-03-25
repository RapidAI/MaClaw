package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coremail "github.com/RapidAI/CodeClaw/corelib/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
)

const systemKeyMailConfig = "mail_config"

type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
	SendLoginConfirmation(ctx context.Context, to string, confirmURL string) error
}

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type ConfigState struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	SMTPHost   string `json:"smtp_host"`
	SMTPPort   int    `json:"smtp_port"`
	Encryption string `json:"smtp_encryption"`
	Username   string `json:"smtp_username"`
	Password   string `json:"smtp_password"`
	FromName   string `json:"from_name"`
	FromEmail  string `json:"from_email"`
	Tested     bool   `json:"tested"`
	TestedAt   int64  `json:"tested_at"`
}

type Service struct {
	fallback ConfigState
	settings SystemSettingsRepository
}

func New(cfg config.Config, settings SystemSettingsRepository) *Service {
	return &Service{
		fallback: ConfigState{
			Enabled:    cfg.Mail.Enabled,
			Provider:   strings.TrimSpace(cfg.Mail.Provider),
			SMTPHost:   strings.TrimSpace(cfg.Mail.SMTPHost),
			SMTPPort:   cfg.Mail.SMTPPort,
			Encryption: coremail.NormalizeEncryption(cfg.Mail.Encryption),
			Username:   strings.TrimSpace(cfg.Mail.Username),
			Password:   cfg.Mail.Password,
			FromName:   strings.TrimSpace(cfg.Mail.FromName),
			FromEmail:  strings.TrimSpace(cfg.Mail.FromEmail),
		},
		settings: settings,
	}
}

func (s *Service) CurrentConfig(ctx context.Context) (ConfigState, error) {
	if s == nil {
		return ConfigState{}, nil
	}
	if s.settings == nil {
		return normalizeConfig(s.fallback), nil
	}
	raw, err := s.settings.Get(ctx, systemKeyMailConfig)
	if err != nil {
		return ConfigState{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return normalizeConfig(s.fallback), nil
	}
	var cfg ConfigState
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return ConfigState{}, err
	}
	return normalizeConfig(cfg), nil
}

func (s *Service) SaveConfig(ctx context.Context, cfg ConfigState) (ConfigState, error) {
	cfg = normalizeConfig(cfg)
	cfg.Tested = false
	cfg.TestedAt = 0
	if s == nil || s.settings == nil {
		s.fallback = cfg
		return cfg, nil
	}
	if err := s.settings.Set(ctx, systemKeyMailConfig, mustJSON(cfg)); err != nil {
		return ConfigState{}, err
	}
	return cfg, nil
}

func (s *Service) MarkTestSuccess(ctx context.Context) (ConfigState, error) {
	cfg, err := s.CurrentConfig(ctx)
	if err != nil {
		return ConfigState{}, err
	}
	cfg.Tested = true
	cfg.TestedAt = time.Now().Unix()
	if s == nil || s.settings == nil {
		s.fallback = cfg
		return cfg, nil
	}
	if err := s.settings.Set(ctx, systemKeyMailConfig, mustJSON(cfg)); err != nil {
		return ConfigState{}, err
	}
	return cfg, nil
}

func (s *Service) SendLoginConfirmation(ctx context.Context, to string, confirmURL string) error {
	subject := "MaClaw Hub sign-in confirmation"
	body := fmt.Sprintf(
		"Hello,\r\n\r\nUse the link below to sign in to MaClaw Hub:\r\n\r\n%s\r\n\r\nIf this was not you, you can ignore this email.\r\n",
		confirmURL,
	)
	return s.Send(ctx, []string{to}, subject, body)
}

func (s *Service) Send(ctx context.Context, to []string, subject string, body string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, coremail.DefaultSendTimeout)
		defer cancel()
	}
	cfg, err := s.CurrentConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("mail delivery is not configured")
	}
	return coremail.Send(ctx, toCoreConfig(cfg), to, subject, body)
}

func toCoreConfig(cfg ConfigState) coremail.Config {
	return coremail.Config{
		SMTPHost:   cfg.SMTPHost,
		SMTPPort:   cfg.SMTPPort,
		Encryption: cfg.Encryption,
		Username:   cfg.Username,
		Password:   cfg.Password,
		FromName:   cfg.FromName,
		FromEmail:  cfg.FromEmail,
	}
}

func normalizeConfig(cfg ConfigState) ConfigState {
	cfg.Provider = defaultIfEmpty(strings.TrimSpace(cfg.Provider), "custom")
	cfg.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
	if cfg.SMTPPort <= 0 {
		cfg.SMTPPort = 587
	}
	cfg.Encryption = coremail.NormalizeEncryption(cfg.Encryption)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.FromEmail = strings.TrimSpace(cfg.FromEmail)
	return cfg
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}

var _ Mailer = (*Service)(nil)
