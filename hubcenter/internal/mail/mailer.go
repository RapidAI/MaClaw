package mail

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
)

const systemKeyMailConfig = "mail_config"
const defaultSendTimeout = 12 * time.Second

type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
	SendHubRegistrationConfirmation(ctx context.Context, to string, confirmURL string, hubName string) error
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
			Encryption: normalizeEncryption(cfg.Mail.Encryption),
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

func (s *Service) SendHubRegistrationConfirmation(ctx context.Context, to string, confirmURL string, hubName string) error {
	if strings.TrimSpace(hubName) == "" {
		hubName = "MaClaw Hub"
	}
	subject := fmt.Sprintf("%s registration confirmation", hubName)
	body := fmt.Sprintf(
		"Hello,\r\n\r\nA Hub requested registration with MaClaw Hub Center.\r\n\r\nHub: %s\r\nConfirm registration:\r\n%s\r\n\r\nIf you did not request this, you can ignore this email.\r\n",
		hubName,
		confirmURL,
	)
	return s.Send(ctx, []string{to}, subject, body)
}

func (s *Service) Send(ctx context.Context, to []string, subject string, body string) error {
	if len(to) == 0 {
		return fmt.Errorf("mail recipient is required")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSendTimeout)
		defer cancel()
	}
	cfg, err := s.CurrentConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.SMTPHost) == "" {
		return fmt.Errorf("mail delivery is not configured")
	}
	if cfg.SMTPPort <= 0 {
		return fmt.Errorf("smtp port is required")
	}

	fromEmail := cfg.FromEmail
	if fromEmail == "" {
		fromEmail = cfg.Username
	}
	if fromEmail == "" {
		return fmt.Errorf("mail sender is not configured")
	}

	message := buildMessage(cfg.FromName, fromEmail, to, subject, body)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)
	if useImplicitTLS(cfg) {
		return sendWithImplicitTLS(ctx, addr, cfg, fromEmail, to, message)
	}
	return sendWithSMTP(ctx, addr, cfg, fromEmail, to, message)
}

func buildMessage(fromName, fromEmail string, to []string, subject string, body string) []byte {
	headers := []string{
		"From: " + formatFrom(fromName, fromEmail),
		"To: " + strings.Join(to, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
	}
	return []byte(strings.Join(headers, "\r\n") + body)
}

func sendWithSMTP(ctx context.Context, addr string, cfg ConfigState, fromEmail string, to []string, message []byte) error {
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return wrapMailError(err)
	}
	return sendWithConn(ctx, conn, cfg, fromEmail, to, message, false)
}

func sendWithImplicitTLS(ctx context.Context, addr string, cfg ConfigState, fromEmail string, to []string, message []byte) error {
	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return wrapMailError(err)
	}
	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName:         cfg.SMTPHost,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
	})
	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConn.SetDeadline(deadline)
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return wrapMailError(err)
	}
	return sendWithConn(ctx, tlsConn, cfg, fromEmail, to, message, true)
}

func sendWithConn(ctx context.Context, conn net.Conn, cfg ConfigState, fromEmail string, to []string, message []byte, implicitTLS bool) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return wrapMailError(err)
	}
	defer client.Close()

	if !implicitTLS && shouldStartTLS(cfg) {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			if cfg.Encryption == "starttls" {
				return fmt.Errorf("smtp server does not support STARTTLS")
			}
		} else if err := client.StartTLS(&tls.Config{
			ServerName:         cfg.SMTPHost,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false,
		}); err != nil {
			return wrapMailError(err)
		}
	}

	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)); err != nil {
				return wrapMailError(err)
			}
		}
	}
	if err := client.Mail(fromEmail); err != nil {
		return wrapMailError(err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return wrapMailError(err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return wrapMailError(err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return wrapMailError(err)
	}
	if err := writer.Close(); err != nil {
		return wrapMailError(err)
	}
	return wrapMailError(client.Quit())
}

func useImplicitTLS(cfg ConfigState) bool {
	return cfg.Encryption == "ssl" || (cfg.Encryption == "auto" && cfg.SMTPPort == 465)
}

func shouldStartTLS(cfg ConfigState) bool {
	return cfg.Encryption == "starttls" || (cfg.Encryption == "auto" && cfg.SMTPPort != 465)
}

func normalizeConfig(cfg ConfigState) ConfigState {
	cfg.Provider = defaultIfEmpty(strings.TrimSpace(cfg.Provider), "custom")
	cfg.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
	cfg.SMTPPort = defaultPort(cfg.SMTPPort)
	cfg.Encryption = normalizeEncryption(cfg.Encryption)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.FromEmail = strings.TrimSpace(cfg.FromEmail)
	return cfg
}

func normalizeEncryption(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ssl":
		return "ssl"
	case "starttls":
		return "starttls"
	case "plain":
		return "plain"
	default:
		return "auto"
	}
}

func defaultPort(port int) int {
	if port > 0 {
		return port
	}
	return 587
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func formatFrom(name, email string) string {
	if strings.TrimSpace(name) == "" {
		return email
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func wrapMailError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("mail delivery timed out: %w", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("mail delivery timed out: %w", err)
	}
	return err
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return `{}`
	}
	return string(data)
}

var _ Mailer = (*Service)(nil)
